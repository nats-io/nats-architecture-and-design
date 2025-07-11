# JetStream Read-after-Write

| Metadata | Value                                                        |
|----------|--------------------------------------------------------------|
| Date     | 2025-07-11                                                   |
| Author   | @MauriceVanVeen                                              |
| Status   | Proposed                                                     |
| Tags     | jetstream, kv, objectstore, server, client, refinement, 2.12 |
| Updates  | ADR-8, ADR-17, ADR-20, ADR-31, ADR-37                        |

| Revision | Date       | Author          | Info           |
|----------|------------|-----------------|----------------|
| 1        | 2025-07-11 | @MauriceVanVeen | Initial design |

## Problem Statement

JetStream does NOT support read-after-write or monotonic reads. This can be especially problematic when
using [ADR-8 JetStream based Key-Value Stores](ADR-8.md), primarily but not limited to the use of _Direct Get_.

Specifically, we have no way to guarantee a write like `kv.Put` can be observed by a subsequent `kv.Get` or `kv.Watch`,
especially when the KV/stream is replicated or mirrored.

## Context

The topic of immediate consistency within NATS JetStream can sometimes be a bit confusing. On our docs we claim we
maintain immediate consistency (as opposed to eventual consistency) even in the face of failures. Which is true, but
as with anything, it depends.

- **Monotonic writes**, all writes to a single stream (replicated or not) are monotonic. It's ordered regardless of
  publisher by the stream sequence.
- **Monotonic reads**, if you're using consumers. All reads for a consumer (replicated or not) are monotonic. It's
  ordered by consumer delivery sequence. (Messages can be redelivered on failure, but this also depends on which
  settings are used)

Those paths are immediately consistent, but they are not immediately consistent with respect to each other. This is no
problem for publishers and consumers of a stream, because they observe all operations to be monotonic.
But, if you use the KV abstraction for example, you're more often going to use single message gets through `kv.Get`.
Since those rely on `DirectGet`, even followers can answer, which means we (by default) can't guarantee read-after-write
or even monotonic reads. Such message GET requests get served randomly by all servers within the peer group (or even
mirrors if enabled). Those obviously can't be made immediately consistent, since both replication and mirroring are
async.

Also, when following up a `kv.Create` with `kv.Keys`, you might expect read-after-write such that the returned keys
contains the key you've just written to. This also requires read-after-write.

## Design

Before sharing the proposed design, let's look at an alternative. Read-after-write could be achieved by having reads (on
an opt-in basis) go through Raft replication first. This has several disadvantages:

- Reads will become significantly slower, due to requiring replication first.
- Reads require quorum, due to replication, disallowing any reads when there's downtime or temporarily no leader.
- Only the stream leader can answer reads, as it is the first one to know that it can answer the request. (Followers
  replicate asynchronously, so letting them answer would make the response take even longer to return.)
- Mirrors can still answer `DirectGet` requests, the transparency of mirrors answering read requests will violate any
  read-after-write guarantees (as the client will not know). This would mean mirrors must not be enabled if this
  guarantee should be kept.
- Read-after-write guarantees could temporarily be violated when scaling streams up or down.
- This is not a compatible approach for consumers, meaning they could not have these guarantees based on this approach.
  It would require limiting consumer creation to R1 on the stream leader, which is not possible since the assignment is
  done by the meta leader that has no knowledge about the stream leader. A replicated consumer could violate the
  requirement if the consumer leader changes to an outdated follower in between. And would not work at all when creating
  a consumer on a mirrored stream.

Although having reads be served through Raft does (mostly) offer a strong guarantee of read-after-write and monotonic
reads, the disadvantages outway the advantages. Ideally, the solution has the following advantages:

- It's explicitly defined, either in configuration or in code.
- Works for both replicated and non-replicated streams. (Scale up/down has no influence, and implementation is not
  replication-specific)
- Incurs no slowdown, just as fast as reads that don't guarantee read-after-write (no prior replication required).
- Let followers, and even mirrors, answer read requests as long as they can make the guarantee.
- Let followers, and mirrors, inform the client when they can't make the guarantee. The guarantee is always kept, but
  an error is returned that can be retried (to get a successful read). This can be tuned by disabling reads on mirrors
  or followers.

Now, on to the proposed design which has the above advantages.

The write and read paths remain eventually consistent as it is now. But one can opt-in for immediate consistency to
guarantee read-after-write and monotonic reads, for both direct/msg read requests as well as consumers.

- **Read-after-write** is achieved because all writes through `js.Publish`, `kv.Put`, etc. return the sequence
  (inherently last sequence) of the stream. In `DirectGet` requests those observed last sequences can be used for read
  requests.
- **Monotonic reads** is achieved by collecting the highest sequence seen in read requests and using that sequence for
  subsequent read requests.

This can be implemented with an additional `MinLastSeq` field in `JSApiMsgGetRequest` and `ConsumerConfig`.

- This ensures the server only replies with data if it can actually 100% guarantee immediate consistency. This is done
  by confirming the `LastSeq` it has for its local stream, is at least the `MinLastSeq` specified.
- Side-note: although `MsgGet` is only answered by the leader, technically an old leader could still respond and serve
  stale reads. Although this shouldn't happen often in practice, until now we couldn't guarantee it. The error can be
  detected on the old leader, and it can delay the error response, allowing for the real leader to send the actual
  answer.
- Followers that can't satisfy the `MinLastSeq` redirect the request to the leader for it to answer instead. This allows
  followers to still serve reads and share the load if they can, but if they can't, they defer to the leader to not
  require a client to retry on what would otherwise be an error.
- Mirrors reject the read request if they can't satisfy the `MinLastSeq`. But can serve reads and share the load
  otherwise. Mirrors don't redirect requests to a leader, not even to the stream leader if the mirror is replicated.
- Leaders/followers/mirrors don't reject a request immediately, but delay this error response to make sure clients don't
  spam these requests while allowing the underlying resources to try and become up-to-date enough in the meantime.
- Rejected read requests have the error code returned as a header, e.g. `NATS/1.0 412 Min Last Sequence`.
- Consumers don't start delivering messages until the `MinLastSeq` is reached, and don't reject the consumer creation.
  This allows consumers to be created successfully, even on outdated followers or mirrors, while waiting to ensure
  `pending` counts are correct when following up `kv.Create` with `kv.Keys` for example.

In terms of API, it can look like this:

```go
// Write
r, err := kv.Put(ctx, "key", []byte("value"))

// Read request
kve, err := kv.Get(ctx, "key", jetstream.MinLastRevision(r))

// Watch/consumer
kl, err := kv.ListKeys(ctx, jetstream.MinLastRevision(r))
```

By specifying the `MinLastRevision` (or `MinLastSequence` when using a stream normally), you can be sure your read
request will be rejected if it can't be satisfied, or the follower/mirror will wait to deliver you messages from
the consumer until it's up-to-date. Followers redirect requests, that would otherwise error, to the leader to not
require the client to retry in these cases.

This satisfies read-after-write and monotonic reads when combining the write and read paths, as well as when only
preforming reads.

### A note about message deletion and purges

JetStream allows in-place deletion of messages through a "message delete" or "purge" request. These don't write new
messages, and thus don't increase the last sequence. This means there are no read-after-write or monotonic reads after a
message is deleted or purged. For example, after deleting a message or purging the stream, multiple requests can flip
between returning the original messages and returning them as deleted.

Although a downside of this approach, it can only be supported when using a replicated stream that's not mirrored, which
would be too restrictive. Whereas with the proposed approach, all followers and mirrors can contribute to providing the
guarantee, regardless of replication or topology (which is valued more highly).

When deleting or purging messages is still desired AND you want to rely on read-after-write or monotonic reads, rollups
can be used instead. The `Nats-Rollup` header can be used to purge messages where the subject equals, or purge the whole
stream. Because a rollup message increases the last sequence, these guarantees can be relied upon again. However, the
client application will need to interpret this rollup message as a "delete/purge" similar to how KV uses delete and
purge markers. Therefore, the KV abstraction still has these guarantees since it places a new message for its
`kv.Delete` and uses a rollup message for its `kv.Purge`.

## Consequences

Since this is an opt-in on a read request or consumer create basis, this is not a breaking change. Depending on client
implementation, this could be harder to implement. But given it's just another field in the `JSApiMsgGetRequest` and
`ConsumerConfig`, each client should have no trouble supporting it.
