# Reconfiguring the JetStream meta peer set for disaster recovery

| Metadata | Value             |
|----------|-------------------|
| Date     | 2026-07-07        |
| Author   | @MauriceVanVeen   |
| Status   | Proposed          |
| Tags     | server, jetstream |

| Revision | Date       | Author          | Info           |
|----------|------------|-----------------|----------------|
| 1        | 2026-07-07 | @MauriceVanVeen | Initial design |

## Context and Problem Statement

The JetStream meta group is the Raft group that owns stream and consumer assignments
across a cluster. Like any Raft group, its quorum is computed from the configured peer
set, not from the set of currently live peers. A peer that is shut down and intended to
not come back, without being explicitly peer-removed still counts toward the quorum
requirement.

This creates a disaster recovery trap when a cluster is expanded and then shrunk
informally. Consider a 3-node cluster grown to 5 nodes for a migration or capacity test.
If the two added servers are later turned off without peer-removing them, the meta group
still believes its size is 5 and needs 3 votes for quorum. If a single one of the three
originally live servers then fails, only 2 peers remain reachable, the metagroup loses
quorum, and the meta layer becomes unavailable. With no quorum there is no leader to
process a peer-remove either, so the meta layer stalls.

The currently supported remedy is to bring every previously configured peer back online,
under the same server name, at the same time, so that quorum can be re-formed and a real
peer-remove (`$JS.API.SERVER.REMOVE`) can run. In real disaster recovery scenarios that is
frequently hard to achieve: hosts are gone, the previous server names are unknown, or the
lost peers are simply unrecoverable. An otherwise healthy deployment is left stuck.

This ADR introduces an explicitly unsafe operator-only API that temporarily lowers the
meta group's quorum requirement on the surviving servers, allowing them to re-form a
working meta layer and use the normal peer-remove path to permanently drop the lost peers.

## Design

### Endpoint

A new subject is added:

```
$JS.API.META.RESCUE
```

The subscription for this subject lives only on the system account. This is a broadcast
subject where every server subscribes to this same subject and each online server that
receives the request evaluates and applies it locally. This lets an operator drive the
rescue with a single publish rather than a coordinated per-server sequence, which is
important in the disaster recovery scenario where the full set of surviving server ids may
not be known up-front and the meta layer cannot be queried through its normal APIs.

### Request

```json
{
  "quorum_needed": 3
}
```

`quorum_needed` is the new, temporarily lowered, quorum size the receiving servers should
apply to the meta group. It must be a positive integer and must not be larger than the
receiving server's current view of the meta peer set size. Setting it to a value at or
above the current natural quorum is a no-op on that server.

The request carries no peer list. The peer set is not rewritten by this API. The rescue
only changes how many votes the meta Raft group counts as a majority; removing dead peers
permanently is still done afterwards through the existing `$JS.API.SERVER.REMOVE` path,
now unblocked because a leader can be elected.

### Discovering the current state

Before issuing a rescue request, an operator needs the current configured peer set on the
surviving servers, so that a sensible `quorum_needed` can be chosen based on how many
peers are actually reachable.

The configured peer set is read from `JSZ`. Today `meta_cluster.replicas` is only
populated on the server that is the meta leader. In the disaster recovery scenario this
API targets there is, by definition, no meta leader, so that field is empty on exactly the
servers an operator needs to inspect. This ADR therefore requires `meta_cluster.replicas`
to be populated on every server, reflecting that server's own view of the configured peer
set, regardless of whether it is the leader.

### Server behavior

On receiving a valid request, a server:

1. Verifies the request was received on the system account.
2. Verifies that `quorum_needed` is at least 1 and no larger than its current view of the
   meta peer set size.
3. Verifies that this server is a voting member of the meta group.
4. Verifies that, from its own perspective, the meta group currently has no leader. If
   this server knows of a current meta leader, the request is rejected: a healthy meta
   layer must not be reconfigured through this API.
5. Otherwise, the server lowers its meta group's effective quorum to `quorum_needed`,
   starts a 5 minute rescue timeout, and resumes Raft. It logs a `WARN` and emits an
   advisory (see below) describing the change, making clear that an unsafe rescue has been
   applied.

Checks 3 and 4 are the safety gate for this API: together they require that the server
believes the meta layer is genuinely stuck, no leader is known and the server itself is
eligible to participate in an election, before it will change the quorum.

Once a majority of the online servers under the new lowered quorum have applied the
change, the meta group can elect a leader and the meta layer becomes available again. The
operator can then peer-remove the lost peers through the existing `$JS.API.SERVER.REMOVE`
path.

### Quorum during the rescue timeout

Normally, whenever the meta group's peer set changes (for example after a peer-remove),
each server recomputes its effective quorum from the new peer set size. While a server is
inside a rescue timeout this recalculation is suppressed, with one exception:

- If a peer set change would recompute effective quorum to a value **lower** than the
  current rescued value, the recalculation is applied and the rescue timeout is canceled
  immediately. The natural, unrescued quorum is now at least as safe as what the rescue
  provided, so there is no reason to keep the rescue active.
- Otherwise, the rescued quorum is kept until the timeout expires. In particular, a
  peer-remove that would recompute quorum to a value equal to or higher than the current
  rescued value does not disturb the rescued value and does not extend the timeout.

When the 5 minute timeout expires without being canceled, the server recomputes its
effective quorum from its current peer set as usual. If the operator has by then
peer-removed the lost peers, the recomputed quorum reflects only the surviving peers and
the meta group operates normally. If some lost peers still remain in the peer set at
expiry, quorum returns to the pre-rescue value and the operator may need to issue another
rescue.

A subsequent rescue request while a rescue is already active is treated the same way as a
first request: it is only accepted on servers that still see no meta leader and it must
still specify a `quorum_needed` at or below the server's current view of the peer set
size. An accepted subsequent request lowers the effective quorum to the new value (if
lower than the current one) and resets the timeout.

### Advisory

Whenever a server actually applies a rescue, it publishes an advisory in addition to
logging the `WARN`.

The advisory is published on:

```
subject: $JS.EVENT.ADVISORY.SERVER.META_RESCUE
type:    io.nats.jetstream.advisory.v1.meta_rescue
```

The body reports the server id and name that applied the change, its previous effective
quorum, the new effective quorum, and the client information of whoever issued the
request. The latter is carried in a `client` field of type `ClientInfo`.

Advisories are ordinary published messages and do not depend on the meta leader, so they
are delivered even while the meta layer has no quorum.

### Response

The response follows the standard JetStream API envelope, with `type`:

```
io.nats.jetstream.api.v1.meta_rescue_response
```

Because the request is a broadcast, each online server that evaluates the request responds
independently. The body reports the server id and name that evaluated the request, its
previous and new effective quorum, and whether the request was applied, was a no-op, or
was rejected. Errors are returned using the standard JS API error format.

## Consequences

- Operators gain a supported path out of a wedged meta layer without needing to resurrect
  dead hosts under their original names. The rescue plus the existing peer-remove API
  together replace the previous "bring every peer back at once" remedy.
- The API is intentionally unsafe. Lowering the meta quorum weakens the guarantees Raft
  relies on to prevent divergent logs; misuse against a not-actually-wedged meta layer can
  split-brain the meta group. The release notes and operator documentation for this
  feature must clearly mark it as a last-resort disaster recovery tool.
- The 5 minute timeout bounds the exposure of a rescued cluster. Even if the operator
  forgets to peer-remove the lost peers, quorum returns to its natural value once the
  timeout expires, so the cluster does not silently keep running with a permanently
  weakened quorum requirement.
- The endpoint is operator tooling. No NATS client library changes are required; it is
  expected to be invoked via the `nats` CLI or equivalent against the system account.
