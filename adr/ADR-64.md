# JetStream server cordoning

| Metadata | Value                   |
|----------|-------------------------|
| Date     | 2026-11-19              |
| Author   | @MauriceVanVeen         |
| Status   | Proposed                |
| Tags     | server, jetstream, 2.16 |

| Revision | Date       | Author          | Info           | Server Version | API Level |
|----------|------------|-----------------|----------------|----------------|-----------|
| 1        | 2026-11-19 | @MauriceVanVeen | Initial design | 2.16.0         | 6         |

## Context and Problem Statement

Every time a stream or consumer needs a peer, the server picks from the JetStream servers
it knows about. Operators need to influence that choice in three situations:

1. **Decommissioning a server permanently.** No new streams or consumers should be
   assigned to it, and everything on it should be moved off before it's removed from the
   cluster.
2. **Temporarily cordoning a server that's expensive to restart.** For example, before a
   restart, R1 consumers are moved off so they stay available, and the server shouldn't
   receive new assets in the meantime. This should clear by itself after the restart.
3. **A server starting up**, for example after lame duck mode. While it's still recovering
   and catching up its streams and consumers, it should only be assigned new streams and
   consumers if there's no other choice.

For (1) the `!jetstream` server tag has existed since 2.9, introduced together with
server-scoped stream moves. It was only honored when selecting new stream peers. Consumer
placement and scale-down ignored it, so a "decommissioned" server could still receive new
consumers. Stepdown placement also didn't support the `!tag` exclusion syntax that stream
placement does: a stepdown with `"tags": ["!x"]` matched servers with the literal tag
`!x`.

There was no mechanism for (2) or (3).

## References

- [ADR-62](ADR-62.md) defines how a peer set change (scale, move, evacuate, peer-remove)
  converges. This ADR defines which servers are eligible to be chosen. The operations in
  ADR-62 are what moves existing assets off a server, this ADR does not add new ones.
- [ADR-5](ADR-5.md) describes lame duck mode. Peers mark a server in lame duck mode as
  offline, and it stops broadcasting its `STATSZ` until it shuts down.
- Placement tags on stream config and stepdown requests, and the `server_tags` option.
- [ADR-65](ADR-65.md) adds filters and limits to `$JS.API.SERVER.EVACUATE`, for example to
  only move "all R1 memory consumers of account X", or a few streams at a time.

## Design

### Placement states

A server is in one of three placement states. A cordon is recorded in the meta layer, so
every server agrees on it. The recovering state is advertised by the server itself through
`STATSZ`. Whether a server is offline is each server's own view.

| State | Set by | Cleared by | Effect |
|-------|--------|------------|--------|
| **Eligible** | Default | — | — |
| **Deprioritized** | JetStream starting up, or the server being offline, for example in lame duck mode | `/healthz` has passed after starting, and it's online | Soft: only chosen if no eligible server is available. |
| **Excluded** | `$JS.API.SERVER.CORDON` with `"cordon": true` | `"cordon": false`, or a restart if made with `server_id` | Hard: never chosen for new placement, unless there's no alternative for an asset that already lives there. |

Cordoning is the recommended way to exclude a server. The `!jetstream` server tag predates
it, and has the same effect, so existing deployments that use it keep working. It's set in
the config and only known to servers that have received its `STATSZ`, which makes it less
reliable.

Excluding a server never moves the assets it already hosts. Existing peers are kept, even
when a group is scaled up or another peer is replaced. Moving them off is an explicit
operation through the evacuate and remove APIs described in [ADR-62](ADR-62.md).

### Excluded servers

A server is excluded from placement when it's cordoned, or has the `!jetstream` tag. This
applies everywhere a server is chosen:

| Where | Behavior |
|-------|----------|
| Stream peer selection (create, scale up, move, replacing a peer) | Never selected. Fails with `no suitable peers for placement, peer cordoned` or `..., exclude tag set` if there aren't enough other servers. |
| Consumer peer selection (create, remap/evacuate) | Only chosen if the stream has no other peer to offer. A consumer has to live on the stream's peers, so an asset that still lives on an excluded server keeps working. |
| Scale down (stream and consumer) | Excluded peers are the first to be dropped, even the current leader, in which case leadership is transferred first. |

Leader elections themselves are not affected. Excluding a server doesn't change which
server is preferred as leader, and an excluded server that still hosts a group can become
its leader, through an election or a stepdown. Only lame duck mode and being offline make
a server ineligible for leadership. Evacuating a server moves its assets off, and their
leadership along with them.

### Placement tags in stepdown requests

The `tags` in a stepdown request's `placement` now follow the same rules as stream
placement tags: a tag must be present on the server, unless it's prefixed with `!`, in
which case it must be absent.

```json
{"placement": {"tags": ["!az:2"]}}
```

### Cordon

A cordon is recorded in the meta layer, keyed by the server's peer ID, which stays the
same across restarts. Every server applies it from the meta log, so all servers agree on
which servers are cordoned, and a new meta leader already knows them.

Cordoning is a system account request to the meta leader. It identifies the server the
same way `$JS.API.SERVER.EVACUATE` and `$JS.API.SERVER.REMOVE` do.

```
$JS.API.SERVER.CORDON
```

Request:

```json
{"peer": "S1", "cordon": true, "server_id": "<server ID>"}
```

| Field | Description |
|-------|-------------|
| `peer` | Server name of the server to (un)cordon. |
| `peer_id` | Peer ID of the server, used instead of `peer` if set. |
| `cordon` | `true` to cordon, `false` to uncordon. |
| `server_id` | Optional. The server ID the server is running as, which changes on every restart. When cordoning, the cordon ends once the server restarts. When uncordoning, only a cordon with this server ID is removed. |

Response:

```json
{
  "type": "io.nats.jetstream.api.v1.meta_server_cordon_response",
  "success": true
}
```

The meta leader resolves `peer` to the server's peer ID, which is derived from the server
name. It accepts any member of the meta group, also one it has never heard from, for
example a server that was already down when this meta leader started. Servers that aren't
a member are rejected. It then proposes a cordon entry to the meta log, and responds once
the entry is applied. Requests are ordered by the meta log, so concurrent requests for the
same server are applied in that order and the last one wins.

Once the response is received, every placement decision the meta leader makes afterward
excludes the server, so an evacuate sent afterward can't move assets back onto it.

A **permanent cordon**, without `server_id`, stays in effect until the server is
uncordoned. It survives restarts of the server and meta leader changes, and applies while
the server is offline. An offline server can be cordoned too, as long as the meta layer
has quorum.

A **temporary cordon**, with `server_id`, stays in effect until that server restarts. The
client gets the server ID from the server itself, for example through `/varz` or
`$SYS.REQ.SERVER.PING`. The meta leader rejects the request if it knows a different server
ID for that peer, as the server has then already restarted. That prevents a cordon that's
sent too late from staying in effect until the next restart. If the meta leader doesn't
know a server ID for it, for example because the server is offline, the request is
accepted. A meta leader that hasn't heard from a restarted server yet still knows its old
server ID, so a request with the new one is rejected until it has, and can be retried.

A temporary cordon is in effect as long as the server ID known for that peer is unknown,
or equal to the recorded one:

- Once the server restarts, other servers learn its new server ID as soon as they hear
  from it again. The cordon no longer applies, and the meta leader cleans it up with an
  uncordon that includes the recorded server ID, so a cordon made after the restart isn't
  removed by it.
- Every meta leader checks the temporary cordons against the server IDs it knows, so a
  cleanup also happens after a meta leader change.
- A network partition or lame duck mode doesn't change the server ID, so the cordon stays
  until an actual restart.

The server ID known for a peer is each server's own view, it isn't replicated. Servers can
therefore briefly disagree whether a temporary cordon still applies, until the cleanup is
applied.

An uncordon without `server_id` removes any cordon for that server. A cordon replaces an
existing one for that server, and repeating the same request is a no-op. A peer-remove
also removes the server's cordon. Changes are logged.

#### Meta snapshot

Cordons are part of the meta snapshot. The snapshot is currently a list of stream
assignments. While no server is cordoned the snapshot keeps that format. Once a server is
cordoned, the snapshot becomes an object with the list of stream assignments and the
cordoned servers:

```json
{
  "streams": [{"...": "..."}],
  "cordoned": [
    {"peer": "<peer ID>"},
    {"peer": "<peer ID>", "server_id": "<server ID>"}
  ]
}
```

A server reads both formats.

A server takes a meta snapshot right after applying the entry that removes the last
cordon, regardless of the meta compaction settings. That compacts every cordon entry out
of its meta log, and the snapshot is back in the existing format. If the snapshot fails,
it's retried like other failed meta snapshots. A server that was offline does the same
once it catches up through the meta log. If it catches up through the meta leader's
snapshot instead, that snapshot is already in the existing format.

### Recovering

A server sets itself as recovering when JetStream is enabled, before its `STATSZ` includes
JetStream. It clears it, and immediately sends a `STATSZ` update, the first time
`/healthz` passes, which checks its meta layer and every stream and consumer it hosts. The
server runs that check itself while it's recovering, and a `/healthz` request that passes
clears it as well. A startup probe on `/healthz` therefore never passes while the server
is still recovering. A `/healthz` request with options that narrow the check, such as
`js-meta-only` or `js-server-only`, doesn't clear it.

Clearing it is a one-time latch. The server doesn't set itself as recovering again until
it restarts, even if `/healthz` fails afterward, for example when a replica falls behind
under load. Otherwise placement would flap with every transient failure.

There's no time limit. A server that keeps failing the check, for example because its meta
layer is busy or an asset doesn't recover, stays recovering. That's harmless, as
recovering only deprioritizes a server, and it's visible in `/varz`.

Recovering servers are ranked below other online servers, and offline servers last:

| Rank | Server |
|------|--------|
| 0 | Online, caught up on the meta layer |
| 1 | Online, lagging on the meta layer |
| 2 | Online, recovering |
| 3 | Offline |

Within a rank, servers keep their existing order, for example by available storage or by
the number of HA assets.

This applies to stream peer selection, and consumer peer selection (create and remap). A
consumer with fewer replicas than its stream is therefore kept off a recovering server
whenever the stream has another peer, without having to cordon it. Recovering doesn't
exclude a server, as that would fail placements that need every server, for example an R3
stream in a cluster of three servers, during every restart.

When the meta layer picks a preferred leader for a new or moved group, it prefers a server
that isn't recovering. Elections in existing groups are unchanged, so a server can still
become a consumer's leader through an election while its replica of the stream is catching
up. The consumer then delivers once that replica has caught up.

### Offline servers

An offline server is deprioritized, like a recovering one. A server is seen as offline
right away when it shuts down or enters lame duck mode, but after a crash or network
partition only once nothing has been heard from it for long enough. It's online again with
its next `STATSZ`. A server whose JetStream stats were never received, for example by a
meta leader that started while it was down, isn't considered for placement at all.

An offline server that's still known is ranked last, but it isn't excluded:

- A stream's online peers must form a quorum on their own, so an offline server is never
  picked for an R1 or R2 stream. For R3 and up it can be picked for the remaining peers,
  for example as the third peer of an R3 stream when only two servers are online. This
  keeps streams creatable while a server is down.
- A consumer with fewer replicas than its stream is only placed on online peers. A
  consumer that spans all of its stream's peers includes an offline one, and replacing a
  consumer's peer falls back to an offline peer if there's no online one.
- The preferred leader is always an online server, if there is one.

So being offline only keeps new assets away from a server if there's an alternative, a
cordon keeps them away regardless, including one made while the server is offline. A
server that isn't coming back should be peer-removed, which takes it out of placement
entirely. Assets placed on a server while it was offline stay there until they're
evacuated.

### Monitoring

Cordons are listed in the meta section of `/jsz` (`meta_cluster`) on every server, as
every server applies them from the meta log. The meta leader has the most recent view,
other servers can briefly lag behind:

```json
{
  "meta_cluster": {
    "name": "C1",
    "leader": "S2",
    "cordoned": [
      {"name": "S1", "peer": "<peer ID>"},
      {"name": "S3", "peer": "<peer ID>", "server_id": "<server ID>"}
    ]
  }
}
```

The JetStream section of `STATSZ` (`$SYS.SERVER.<id>.STATSZ`) and `/varz` shows a server's
own state. The fields are omitted when false:

```json
{
  "jetstream": {
    "config": {"...": "..."},
    "stats": {"...": "..."},
    "cordoned": true,
    "recovering": true
  }
}
```

`cordoned` is the server's own cordon, taken from the meta state. It's only informational,
placement uses the meta state directly. The `!jetstream` tag is visible through the
existing `tags`.

### Operational flows

**Decommissioning a server**

1. Cordon the server.
2. `$JS.API.SERVER.EVACUATE`: every stream (and its consumers) is migrated off, keeping
   its state ([ADR-62](ADR-62.md)).
3. Wait until the server hosts no streams or consumers.
4. Shut the server down, using lame duck mode.
5. Once it's offline, and isn't coming back, `$JS.API.SERVER.REMOVE` it. Nothing is left
   on it, so the peer-remove doesn't lose any state.

**Restarting a server that's expensive to restart**

1. Cordon the server with its current `server_id`.
2. Move its consumers off with `$JS.API.SERVER.EVACUATE` and the `consumers_only` filter
   ([ADR-65](ADR-65.md)), keeping their state. This only works for consumers with fewer
   replicas than their stream.
3. Wait until those consumers have left the server.
4. Restart the server. The cordon is cleared, and the server is deprioritized until
   `/healthz` has passed.

**Restarting after lame duck mode**

No action is needed. Peers mark the server offline when it enters lame duck mode, which
deprioritizes it. Once it restarts it advertises itself as recovering, and it only becomes
a preferred target again once `/healthz` has passed.

## Decision

- A cordon means "don't place any streams or consumers on this server" everywhere a server
  is chosen. It's managed through the JetStream API and replicated through the meta log,
  keyed by peer ID. A cordon made with a `server_id` ends when that server restarts. It
  does not move existing assets, and doesn't affect leader elections.
- The `!jetstream` tag has the same effect, and is kept for existing deployments.
- A server that's starting up (or offline) is deprioritized, not excluded, until
  `/healthz` has passed once (and it's online).

## Consequences

- Stepdown placement tags prefixed with `!` are now exclusions. Previously they could only
  match servers with a literal `!`-prefixed tag, which isn't expected to be used in
  practice.
- Scaling down can now transfer leadership, if the current leader is excluded.
- Cordoning and uncordoning need the meta layer to have quorum.
- Mixed versions: placement is decided by the meta leader, and scale down by the group
  leader. A server that doesn't know about `recovering` ignores it, and a server that
  doesn't send it is treated as not recovering. The `!jetstream` tag keeps its previous,
  partial meaning on older servers. For cordons, see below.

## Upgrade/downgrade considerations

A cordon adds a new meta log entry, and changes the meta snapshot format while any server
is cordoned. A server that doesn't understand them stops processing the meta layer.

- Only cordon a server once every server in the cluster runs a version that supports it.
  Clusters that never cordon are unaffected, as the snapshot keeps its existing format.
- Before downgrading, uncordon all servers, and wait until every server has applied that,
  for example until all meta peers are current. A server replays its own latest meta
  snapshot and the meta log after it on startup, which must not contain any cordon. Each
  server takes a meta snapshot right after the last cordon is removed, which ensures that.
