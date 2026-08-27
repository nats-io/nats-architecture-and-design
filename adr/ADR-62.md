# JetStream desired state reconciliation

| Metadata | Value                   |
|----------|-------------------------|
| Date     | 2026-08-26              |
| Author   | @MauriceVanVeen         |
| Status   | Implemented             |
| Tags     | server, jetstream, 2.15 |

| Revision | Date       | Author          | Info           |
|----------|------------|-----------------|----------------|
| 1        | 2026-08-26 | @MauriceVanVeen | Initial design |

## Context and Problem Statement

A stream's or consumer's placement and replication is expressed as a `raftGroup`
(`Peers`, `Cluster`, `Storage`, ...) held on the `streamAssignment` /
`consumerAssignment` that lives in the JetStream meta Raft log. Scaling, moving, or
peer-removing a stream or consumer ultimately means changing that peer set, but a Raft
group's membership can't just be swapped for a new one in a single step: adding or
removing several peers at once can produce two quorums that never overlap, which risks
electing two independent leaders over the same data. Every membership change has to be
applied as a sequence of safe, single-peer adds/removes, each one only proposed once the
previous one is durable.

The server's Raft layer automatically adds peers that it hears from, this is fine for the
meta layer where new nodes joining automatically is a great feature, but it's arguably
broken when also applied on the stream and consumer layer. During a scale-down, move, or
peer-remove it means a peer that was meant to be evicted can silently reappear.

Prior to this design, each kind of reconfiguration (move, scale up, scale down, retention
change) was driven by its own bespoke, imperatively coded state machine layered directly
on top of the Raft group's peer list, with progress tracked only implicitly in the shape
of that peer list (e.g. "more peers than configured replicas" meant "a move is in
progress") and with no record of which peers were deliberately evicted versus merely
lagging. This made it hard to combine operations (e.g. scale while a move is pending), to
recover cleanly from a meta or group leader change mid-operation, and to report to a user
or operator what a pending reconfiguration was actually doing or waiting on.

The intention is for the meta leader to propose desired state, but to not itself control
which peers are allowed to be added or removed from the stream/consumer Raft group. The
meta layer is in control of the desired state in the assignment, but the group leader is
in control of the assignment's peer set. The group leader adds and removes peers through
the log safely, and informs the meta leader about updating the assignment based on the
reconciliation steps for the given desired state.

## Design

### Desired state encoding

A `raftGroup` gets an optional `Desired` field. `Desired` is unset when the group is
stable (running exactly as configured); any in-flight scale, move, or retention change is
expressed by setting `Desired` to the peer set, cluster, and flags the group must converge
to. For example, a stream moving from a 3-peer group in `us-east` to a 3-peer group in
`us-west`:

```json
{
  "created": "1970-01-01T00:00:00Z",
  "id": "d1JAf92k",
  "term": 7,
  "peers": [
    "S4",
    "S5",
    "S6"
  ],
  "cluster": "us-west",
  "move": true,
  "origin": {
    "peers": [
      "S1",
      "S2",
      "S3"
    ],
    "cluster": "us-east",
    "replicas": 3,
    "placement": {"cluster": "us-east"}
  }
}
```

`id` guarantees the changes that the group leader is proposing are only made if the meta
leader can converge that it was made against the latest assignment the meta leader knows
(i.e. the `id` matches, otherwise the meta leader ignores it). `term` tells the meta
leader which stream/consumer leader is meant to be driving reconciliation right now, so a
proposal from a leader that has since lost that role is ignored.

`move` indicates the desired state change is about moving the stream between clusters or
to other nodes in general. `scale_down` (not shown above) marks `peers` as the total peer
set to scale down from rather than the final set. The stream/consumer leader selects the
final set from this list, based on the desired replication count and what it knows to be
the most preferable peer set in terms of up-to-date and online servers. `removed` (also
not shown) lists peers an operator explicitly peer-removed (used for determining it's safe
to evict peers, if the group is leaderless without quorum, and evicting peers would help
regain leadership).

`origin` records the pre-change replicas, placement, and (for retention changes)
retention policy, captured once and never overwritten by later desired state updates, so a
pending or in-progress reconfiguration can always be rolled back or canceled back to where
it started. A stream keeps running at its origin configuration (placement, retention)
until desired state is reached.

Legacy stream moves, a peer set with more members than configured replicas, are recognized
and converted into desired state. Ensuring upgraded clusters can benefit from the new
desired state format, and such legacy stream moves don't stall during or after upgrade.

### Desired state on stream/consumer info

`STREAM.INFO` / `CONSUMER.INFO` expose an in-flight desired state as `cluster.desired`.
Unlike the internal encoding above, `replicas` is a peer info list (name, current,
lag, ...) mirroring the existing `cluster.replicas` shape, rather than a bare peer name
list, and only reflects the desired peers once they're settled (while scaling down, the
final peer set hasn't been selected yet, so it's omitted until the final peer set is
known). For example, the same move from `us-east` to `us-west`, reported partway through
catch-up:

```json
{
  "config": {
    "name": "move-stream",
    "placement": {"cluster": "us-west"}
  },
  "cluster": {
    "name": "us-east",
    "leader": "S1",
    "replicas": [
      {"name": "S2", "current": true, "active": 0, "peer": "..."},
      {"name": "S3", "current": true, "active": 0, "peer": "..."},
      {"name": "S4", "current": false, "active": 0, "peer": "...", "pending": true}
    ],
    "desired": {
      "name": "us-west",
      "replicas": [
        {"name": "S4", "current": false, "active": 0, "peer": "..."},
        {"name": "S5", "current": false, "active": 0, "peer": "..."},
        {"name": "S6", "current": false, "active": 0, "peer": "..."}
      ],
      "origin": {
        "replicas": 3,
        "placement": {"cluster": "us-east"}
      },
      "status": {
        "description": "waiting for peers to catch up",
        "type": "catchup"
      }
    }
  }
}
```

`desired.name` is the target cluster. `desired.origin` is what the reconfiguration can be
rolled back to if canceled. `desired.status` is the same status described below, so a
client watching `STREAM.INFO`/`CONSUMER.INFO` doesn't need a separate API call to see what
a pending reconfiguration is doing or waiting on.

`desired.replicas` shows the replicas that are chosen to be the final peer set, and
`cluster.replicas` shows which peers currently host the stream/consumer.
`cluster.replicas` will therefore see peer additions and removals while the scale/move
operations are ongoing. A new `pending` field is also added to show a pending Raft peer
addition or removal, specifically: the meta assignment contains that specific peer, but
the peer isn't added through the Raft log yet (if about peer addition), or the peer has
been removed through the Raft log but is not yet removed from the meta assignment (if
about peer removal).

### Status reporting

Each cycle's action (or reason for not acting) is exposed as a status on `STREAM.INFO` /
`CONSUMER.INFO`.

| Type | Meaning | Example description |
|------|---------|------------------------|
| `meta` | The meta leader must record or advance desired state. | `requesting desired state from meta leader`<br/>`selecting peers to scale down to`<br/>`expanding assignment with desired peers` |
| `membership` | A proposed membership change must commit. | `adding peer X`<br/>`removing peer X`<br/>`stepping down to X` |
| `snapshot` | A snapshot must be installed. | `installing snapshot` |
| `catchup` | More peers must catch up before removing one. | `waiting for peers to catch up` |
| `quorum` | More peers must come online, adding a peer would risk losing quorum. | `waiting for quorum to add peer` |
| `blocked` | Another asset must move first: a stream/consumer must migrate to or off a peer first. | `waiting for consumer 'X' to migrate` (stream)<br/>`waiting for stream to migrate first` (consumer) |
| `unavailable` | Nothing to do: shutting down, or the assignment is gone. | `shutting down`<br/>`no stream assignment` |

### Meta leader suggests, group leader reconciles

#### Meta: desired state reconciliation

The meta leader is the only writer of assignments; it applies updates proposed by
stream/consumer group leaders rather than deciding on its own what an asset's next step
should be.

| Step | Description |
|------|-------------|
| 1 | Receives a request to establish or update an asset's desired state from the asset's group leader. |
| 2 | Validates the request against the live assignment and the requester's leadership term; stale or out-of-date requests are dropped. |
| 3 | Records the new/updated desired state on the assignment. |
| 4 | If a stream's desired state changed, checks whether its consumers still line up with the new target peers; if not, updates their assignments too. |
| 5 | Commits the assignment change(s) to the meta log, making them visible to the relevant group leader(s). |

#### Meta: peer/assignment reconciliation

Separate from the desired state loop above: triggered by cluster membership changes (a
peer joining or leaving), not by user requests. This is what backfills a replacement peer
after a peer-remove, or picks up a newly available peer to heal an under-replicated group.

| Step | Description |
|------|-------------|
| 1 | A peer joins or leaves the meta group. |
| 2a | On join: the peer is held aside until it's confirmed reachable/usable, then becomes eligible for placement. |
| 2b | On leave: every assignment that references the removed peer is found. |
| 3 | For each affected assignment, a replacement peer set is computed (drawing only from currently eligible peers), respecting the stream's placement constraints. |
| 4 | The recomputed assignment is committed, handing the affected stream (and then its consumers) back into the normal desired state loop above to actually converge onto the new peers. |

#### Stream: group leader reconciliation

Runs on the stream's own group leader, repeating on a cycle while desired state is set.

| Step | Description |
|------|-------------|
| 1 | Checks whether desired state exists and is current for its own leadership term; if not, asks the meta leader to (re-)establish it and waits. |
| 2 | If a Raft snapshot is needed before growing the group, installs one first. For example, when scaling up from R1 to R3, the snapshot is what makes sure that the new peers require catchup. |
| 3 | Drops any peers that are no longer part of the cluster at all. |
| 4 | Adds any desired peers not yet part of the group, and waits for them to catch up. The meta leader is asked to add them to the assignment first, only after that can the peer be added through the Raft log. |
| 5 | If scaling down, selects which peers to drop and asks the meta leader to record that choice. |
| 6 | Once the actual peer set exactly matches the desired one, removes one remaining old peer (checking quorum first, and that no consumer is still relying on that peer). The peer is first removed from the Raft log, only after that has been committed then the meta leader is asked to record that change. |
| 7 | Once fully converged, finalizes the assignment and reports done. |
| — | Throughout, the cluster info contains a status describing what it's currently doing or waiting on. |

#### Consumer: group leader reconciliation

Same shape as the stream loop, with one key difference.

| Step | Description |
|------|-------------|
| 1–7 | Same steps as the stream loop: request desired state, snapshot, drop/add peers, scale-down selection, remove old peer, finalize, report status. |
| — | Difference: a consumer may only add a peer if it's already in the stream's peer set. |

### Operations, endpoints, and side effects

| Operation | API Endpoint(s) | Description and side effects |
|-----------|-----------------|------------------------------|
| **Leader step-down** | `$JS.API.STREAM.LEADER.STEPDOWN.<stream>`<br/>`$JS.API.CONSUMER.LEADER.STEPDOWN.<stream>.<consumer>`<br/>`$JS.API.META.LEADER.STEPDOWN` | - Does not change the stream/consumer peer set, only transfers leadership to a pre-existing peer. |
| **Scale up a stream** | `$JS.API.STREAM.UPDATE.<stream>` | - Adds peers and catches them up.<br/>- Any consumers without an explicit replica count are scaled up as well.<br/>- Interest/WorkQueue consumers are also scaled up to match the stream's replica count. |
| **Scale down a stream** | `$JS.API.STREAM.UPDATE.<stream>` | - Picks which peers to drop and removes them.<br/>- Prefers keeping the stream leader and online and caught up peers.<br/>- Consumers with a higher replica count than the new value will scale down as well.<br/>- Consumers, for example R1, that are hosted on a peer that is about to be removed will scale up and be moved to a peer that will remain first before the stream scale down continues. |
| **Scale up a consumer** | `$JS.API.CONSUMER.CREATE.<stream>.<consumer>` | - Adds peers, but only from peers the stream itself is already on. |
| **Scale down a consumer** | `$JS.API.CONSUMER.CREATE.<stream>.<consumer>` | - Picks which peers to drop and removes them.<br/>- Prefers keeping the consumer leader and online and caught up peers. |
| **Change stream retention policy** | `$JS.API.STREAM.UPDATE.<stream>` | - When changing stream retention policy from Limits to Interest on a replicated stream, the consumers will need to scale up first since their replica count needs to match that of the stream if it's Interest or WorkQueue.<br/>- Consumers will be scaled up first, and only after that's done will the stream change to Interest retention.<br/>- Changing the stream back to Limits retention similarly allows to scale down consumers. |
| **Move a stream** | `$JS.API.STREAM.UPDATE.<stream>`<br/>`$JS.API.ACCOUNT.STREAM.MOVE.<account>.<stream>`<br/>(system account) | - Adds the new placement's peers, catches them up, then drops all old peers.<br/>- Consumers perform the same move to its new peers. |
| **Evacuate a stream peer** | `$JS.API.STREAM.PEER.EVACUATE.<stream>`<br/>`$JS.API.SERVER.EVACUATE`<br/>(system account, evacuates all assets from a server) | - Marks the peer as evacuating, and assigns a new peer to catch up.<br/>- The evacuating peer can still help to catch up the new peer, for example when evacuating the last peer for a R1 stream, which moves the stream to a new peer. |
| **Remove a stream peer** | `$JS.API.STREAM.PEER.REMOVE.<stream>`<br/>`$JS.API.SERVER.REMOVE`<br/>(system account, peer-remove a server and remove from all assets) | - Removes the peer from the stream immediately, and assigns a new peer if available.<br/>- If no new peer is available the group will run under-replicated (if R>1) until a new peer is added to the cluster that satisfies the stream placement, at which point it's automatically added to the stream peer set.<br/>- Peer-removing the last peer, for example if R1, of a stream is allowed but moves the stream to a new peer without preserving the data (since the last copy is removed). Prefer evacuating a stream peer if it's still able to catch up other peers.<br/>- Removing a peer is a valid strategy to get out of "no quorum" when no leader can be elected if a quorum of peers can't come back. Make sure to scale down the stream to the appropriate replica count afterward. For example, peer-removing 2 dead nodes such that it runs as a R1 stream, you can then scale down the stream config to R1 as well such that it matches its peer set. Be careful though with peer-removing liberally when peers are planned to come back, if a peer can't be replaced its slot will remain empty, so when the peer does come back it does not get re-added to the peer set (since it was peer-removed), scale down the stream, wait for it to settle, then scale it back up. |
| **Cancel a pending stream move, scale, or retention change** | `$JS.API.STREAM.CANCEL_MOVE.<stream>`<br/>`$JS.API.ACCOUNT.STREAM.CANCEL_MOVE.<account>.<stream>`<br/>(system account) | - Any reconfiguration can be rolled back to its origin, i.e. what it was before any change started.<br/>- The stream peers will always return to their original peer set. However, consumers may pick different peer sets depending on how far along their scale/move actions were. |
| **Overlapping stream operations** | - | - Scale or retention changes while other scale or retention changes are still inflight are accepted.<br/>- A move change while another move is in progress is not allowed, wait for the move to complete first.<br/>- Scale or retention changes while a move is in progress is not allowed, wait for the move to complete first. |
| **Overlapping consumer operations** | - | - Consumers can be independently and freely scaled, even if stream operations are ongoing.<br/>- Keep in mind that if stream operations are ongoing, that the consumer may still move around after your operation completes until the stream operations are completed. |

## Decision

Represent all in-flight stream/consumer reconfiguration (scale, move, retention change)
uniformly as desired state on the asset's Raft group, rather than as separate,
special-cased mechanisms per kind of change. The group leader is the only one that decides
the next safe step toward that desired state and proposes it; the meta leader is the only
one that commits it to the assignment, and every proposal is fenced by
`id` and `term` so a stale or superseded proposal can never be applied. Progress is
reported through a stable, typed status on cluster info rather than inferred from the
shape of the peer set.

## Consequences

- Scaling, moving, and retention changes now share one mechanism and one piece of state on
  the assignment, instead of several independent, bespoke mechanisms inferred from
  peer-set shape; this made stacking operations (e.g. scale while a move is pending) and
  safely canceling them tractable.
- `STREAM.INFO`/`CONSUMER.INFO` gains a typed migration status, giving operators and
  client tooling visibility into what a pending reconfiguration is actually blocked on
  (membership, quorum, catch-up, snapshot, another asset) instead of just "still moving".
- Every reconciliation step is fenced by `id` and `term`, so a group leader that loses and
  regains leadership, or a meta leader failover mid-reconciliation, resumes correctly
  rather than replaying or losing progress.
- The added indirection (origin, target, desired state, fencing by `id` and `term`) is
  meaningfully more state to reason about than the prior peer-list-shape encoding, but is
  necessary to make reconfiguration composable and observable rather than a set of special
  cases. Through this indirection, we ensure that the stream/consumer config always equals
  exactly what the user specified last. A server version that doesn't know the desired
  state would degrade to exactly only what the user specified last, albeit it will not
  continue the desired state reconciliation.
- A cluster can't reconcile desired state until every server in it understands the
  encoding, which constrains how a cluster may be upgraded or downgraded across this
  change; see below.

## Upgrade/downgrade considerations

Upgrading directly to 2.15 is **not recommended**. A server that doesn't understand the
desired state has no way to represent an in-flight scale, move, or retention change; any
such operation can be lost in translation while the cluster is on mixed versions, leaving
it stuck rather than converging.

A compatibility release for the 2.14 line will ship first, adding the desired state models
(encoding and recognition, without changing runtime behavior). This makes the upgrade path
two clean steps instead of one risky one:

- **2.14 → 2.14-compat**: no riskier than any other 2.14 patch upgrade. The compatibility
  release only adds understanding of the desired state encoding, so it behaves exactly
  like 2.14 otherwise.
- **2.14-compat → 2.15**: safe, but any in-flight operation stalls while the cluster is on
  mixed versions, since only the fully upgraded servers can drive reconciliation for it.
  Once every server in the cluster is on 2.15, normal desired state reconciliation picks
  up any remaining stalled operations and resolves them without operator intervention.

Downgrading, although not advisable in general, functions in a different way. Downgrading
to anything below the 2.14-compat version is **not recommended**, as it has the same
downsides of a direct upgrade. The compatibility release makes downgrading safer by
preserving the desired state encoding; however, it does not reconcile the desired state
itself. Prefer letting pending scale/move/retention changes settle before downgrading to
the compatibility release. Any desired state left around will still report in stream info
and consumer info as-if you're running on 2.15, but without the reconciliation. If you
upgrade back to 2.15 without updating the stream/consumer assignment, the desired state
reconciliation would continue as normal.

If a direct upgrade/downgrade was performed anyway and the cluster ended up in an
inconsistent state, it may be (partially) repaired by performing a rolling restart of all
nodes while on the same version.
