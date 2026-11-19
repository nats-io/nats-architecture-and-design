# JetStream partial server evacuation

| Metadata | Value                               |
|----------|-------------------------------------|
| Date     | 2026-11-19                          |
| Author   | @MauriceVanVeen                     |
| Status   | Proposed                            |
| Tags     | server, jetstream, 2.16, refinement |
| Updates  | ADR-62                              |

| Revision | Date       | Author          | Info           | Server Version | API Level |
|----------|------------|-----------------|----------------|----------------|-----------|
| 1        | 2026-11-19 | @MauriceVanVeen | Initial design | 2.16.0         | 6         |

## Context and Problem Statement

`$JS.API.SERVER.EVACUATE` ([ADR-62](ADR-62.md)) migrates every stream off a server, and
its consumers with it, all in one go. That's a problem in two cases:

- **Large servers.** Starting the migration of hundreds or thousands of streams at once
  means just as many catch-ups at the same time, competing for disk, network, and the
  catch-up limits of the servers involved.
- **Targeted evacuations.** An operator may only want to move part of what a server hosts.
  For example, before restarting a server that's expensive to restart, all R1 in-memory
  consumers of one account should move to another server, without moving any streams.
  Today that means calling `$JS.API.CONSUMER.PEER.EVACUATE.<stream>.<consumer>`
  for each consumer, after first finding out which consumers live on that server.

## References

- [ADR-62](ADR-62.md) defines the evacuate operations, and how a migration converges while
  keeping its state.
- [ADR-64](ADR-64.md) adds cordoning, which keeps new assets off a server, permanently or
  until it restarts. That's useful before evacuating a server to decommission or restart
  it, but not needed when evacuating to rebalance.

## Design

### Request

`$JS.API.SERVER.EVACUATE` gets optional `filter` and `limit` fields. Without them, it
behaves as it does today.

```json
{
  "peer": "S1",
  "filter": {
    "account": "ACME",
    "storage": "memory",
    "replicas": 1,
    "consumers_only": true
  },
  "limit": 10
}
```

| Field | Description |
|-------|-------------|
| `peer` | Server name of the server to evacuate (existing). |
| `peer_id` | Peer ID of the server, used instead of `peer` if set (existing). |
| `filter` | Only evacuate the assets matching all of its fields. |
| `limit` | The maximum number of assets this request starts to evacuate. Zero or unset means no limit. |

| Filter field | Description |
|--------------|-------------|
| `account` | Account name. |
| `stream` | Stream name. Requires `account`. With `consumers_only`, this evacuates the consumers of that stream. |
| `consumer` | Consumer name. Requires `stream`. Only that consumer is evacuated, `consumers_only` is ignored. |
| `storage` | `file` or `memory`. For consumers this is their effective storage, which is `memory` if the stream or the consumer (`mem_storage`) is. |
| `replicas` | Replica count. For consumers this is their effective replica count, which is inherited from the stream if not set. |
| `consumers_only` | `false` (default): streams are evacuated, their consumers move along with them as they do today.<br/>`true`: only consumers are evacuated, they move to another of their stream's peers. The streams stay where they are. Only consumers with fewer replicas than their stream can move this way, the others are skipped. |

Names are matched exactly, without wildcards or lists. Evacuating several accounts or
streams takes a request for each. An invalid filter, for example `stream` without
`account`, is rejected as a bad request.

`$JS.API.STREAM.PEER.EVACUATE.<stream>` and
`$JS.API.CONSUMER.PEER.EVACUATE.<stream>.<consumer>` are only available in the stream's
own account. The `stream` and `consumer` filters let an operator do the same from the
system account.

### Limit

`limit` only bounds how many evacuations this request starts. The server doesn't track
evacuations across requests, and doesn't limit how many run concurrently. That's left to
the client: it chooses a `limit`, waits for those evacuations to finish, then sends the
same request again.

For that loop to work, an asset that's already being evacuated off the server is skipped,
and doesn't count toward the limit. Repeating the request then only starts new
evacuations. Without `consumers_only`, each stream counts as one, and its consumers move
along with it without counting toward the limit. With `consumers_only`, each consumer
counts as one. `started` and `remaining` count the same way. Which assets are started
first is unspecified, filters are how an operator controls that.

### Response

```json
{
  "type": "io.nats.jetstream.api.v1.meta_server_evacuate_response",
  "success": true,
  "started": 10,
  "remaining": 32
}
```

| Field | Description |
|-------|-------------|
| `started` | Evacuations this request started. |
| `remaining` | Matching assets this request didn't get to because of `limit`. The client can repeat the request until this is zero. |

`remaining` doesn't include assets that are already being evacuated, nor assets that can't
be moved because there's nowhere to move them to.

Running evacuations aren't reported, the client follows them through the evacuated
server's `$SYS.REQ.SERVER.<id>.JSZ` request (or its `/jsz` endpoint), which needs the
system account. It lists the streams and consumers the server still hosts, for example
with `{"account": "ACME", "streams": true, "consumer": true}`. A stream or consumer that's
being migrated reports `cluster.desired`, with its migration status from
[ADR-62](ADR-62.md). Once the migration has finished, it no longer reports it, and an
asset that moved off the server is no longer listed. Consumer details aren't paged, so an
account with many consumers on the server returns them all in one response.

### Example: moving R1 memory consumers of an account

1. Optionally, cordon the server ([ADR-64](ADR-64.md)), for example with its `server_id`
   before restarting it.
2. Send the request above. It starts up to 10 consumer evacuations.
3. Wait until no consumer of the account in the server's `JSZ` reports `cluster.desired`.
4. Repeat from step 2 until `remaining` is zero.

### Mixed versions

A meta leader that doesn't know `filter` or `limit` ignores them and evacuates everything.
Clients that set either field should send the `Nats-Required-Api-Level` header with the
API level that introduced them, so an older meta leader rejects the request instead.

## Decision

- `$JS.API.SERVER.EVACUATE` can be narrowed with a filter, including to consumers only.
- A request starts at most `limit` evacuations. Pacing is up to the client, the server
  doesn't track evacuations across requests.

## Consequences

- No new state is kept on the server or in the meta log. All progress lives in the
  assignments' desired state ([ADR-62](ADR-62.md)), so a meta leader change in between
  requests doesn't lose anything.
- Without `filter` and `limit` the request behaves as before, so existing clients are
  unaffected.
- A client that doesn't wait between requests can still start everything at once.
