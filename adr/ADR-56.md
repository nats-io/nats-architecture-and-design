# JetStream Consistency Models

| Metadata | Value                       |
|----------|-----------------------------|
| Date     | 2025-09-12                  |
| Author   | @ripienaar, @MauriceVanVeen |
| Status   | Approved                    |
| Tags     | server, 2.12                |

| Revision | Date       | Author                      | Info                                              |
|----------|------------|-----------------------------|---------------------------------------------------|
| 1        | 2025-09-12 | @ripienaar, @MauriceVanVeen | Initial document for R1 `async` persistence model |
| 2        | 2025-10-28 | @MauriceVanVeen             | Add read consistencies                            |

## Context and Problem Statement

JetStream is a distributed message persistence system and delivers certain promises when handling user data.

This document intends to document the models it support, the promises it makes and how to configure the different models.

> [!NOTE]  
> This document is a living document; at present we will only cover the `async` persistence model with an aim to expand in time
> 

## R1 `async` Persistence Mode

The `async` persistence mode of a stream will result in asynchronous flushing of data to disk, this result in a significant speed-up as each message will not be written to disk but at the expense of data loss during severe disruptions in power, server or disk subsystems.

If the server is running with `sync: always` set then that setting will be overridden by this setting for the specific stream. It would not be in `sync: always` mode anymore despite the system wide setting.

At the moment this mode cannot support batch publishing at all and any attempt to start a batch against a stream in this mode must fail.

This setting will require API Level 2.

The interactions between `PersistMode:async` and `sync:always` are as follows:

 * `PersistMode:default`, `sync:always` - all writes are flushed (default) and synced
 * `PersistMode:default`, not `sync:always` - all writes are flushed (default), but synced only per sync interval
 * `PersistMode:async` - PubAck is essentially returned first, writes are batched in-memory, and the write happens asynchronously in the background

### Implications:

 * The Publish Ack will be sent before the data is known to be written to disk
 * An increased chance of data loss during any disruption to the server

### Configuration:

 * The `PersistMode` key should be unset or `default` for the default strongest possible consistency level
 * Setting it on anything other than a R1 stream will result in an error
 * Scaling a R1 stream up to greater resiliency levels will fail if the `PersistMode` is not set to `async`
 * When the user provides no value for `PersistMode` the implied default is `default` but the server will not set this in the configuration, result of INFO requests will also have it unset
 * Setting `PersistMode` to anything other than empty/absent will require API Level 2

## Read Consistencies

The table below describes the current read consistencies supported by the JetStream API, from the highest consistency
level to lowest.

| Stream configuration                     | JetStream API                     | Description                                                                                                                                     | Level of consistency                                                                                                                                                                                                                         |
|:-----------------------------------------|:----------------------------------|:------------------------------------------------------------------------------------------------------------------------------------------------|:---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `AllowDirect` disabled.                  | `$JS.API.STREAM.MSG.GET.<stream>` | An API meant only for management outside of the hot path. The request goes to every server, but is normally only answered by the stream leader. | Current highest level of read consistency. Only the stream leader answers, but stale reads are technically possible after leader changes or during network partitions since an old leader could still answer before the current leader does. |
| `AllowDirect` enabled.                   | `$JS.API.DIRECT.GET.<stream>`     | If the stream is replicated, the followers will also answer read requests.                                                                      | Higher availability read responses but with lower consistency. A read request will be randomly served by a server hosting the stream. Recently written data is not guaranteed to be returned on a subsequent read request.                   |
| `MirrorDirect` enabled on mirror stream. | `$JS.API.DIRECT.GET.<stream>`     | If the stream is mirrored, the mirror can also answer read requests. For example a mirror stream in a different cluster or on a leaf node.      | Higher availability with potential of fast local read responses but with lowest consistency. Mirrors can be in any relative state to the source.                                                                                             |

Additionally, if a stream is replicated and a consumer is created, there is no guarantee that the consumer can
immediately observe all the written messages at that time. For example, if a R1 consumer is created on a follower not up
to date on all writes yet. The consumer will eventually observe all the writes as it keeps on fetching new messages as
they come in.

## Proposal to add linearizability

Newer server versions, like for 2.14+, should support more configurability or in general higher levels of consistency as
opt-in. For example, higher read consistency for consumers can be achieved by having consumer CRUD operations go through
the stream's Raft log instead of the Meta Raft log, which ensures that a consumer created at time X in the stream log
can observe all the stream writes up to time X.

Specifically, higher level consistency for message read requests would roughly require:

- `AllowDirect` should not need to be disabled. The `$JS.API.STREAM.MSG.GET.<stream>` API, when `AllowDirect` is
  disabled, has significant overhead since these requests go to ALL servers not just the servers hosting the stream.
- Direct Get allows using batch requests, this should also be supported. (Which is not the case with the Msg Get API
  above)

Linearizable reads would be desirable, but a minimum would be to opt in to at least session-level guarantees such as
reading your own writes and monotonic reads.

### Discussion

#### What would be expected given different topologies?

For a replicated stream, the minimum to expect would be that within the cluster the read response will always be aware
of the writes performed up to that point. Either only writes performed by solely this process doing the read request, or
all writes performed by all processes on the same stream. (To be discussed later)

But what about when the stream is mirrored on a leaf node and a client is connected to the leaf node? Or similarly if
the client is connected to another cluster in a super cluster, and the stream is mirrored there?

Writes still only go to the source, which will be aware of all writes to the stream up to that point. So a new write may
be immediately reflected in a read request when the client is connected directly to the cluster, but perhaps not when
connected to the leaf node. Is that okay given the topology, or would the expectation be that the client can always get
the most consistent view of a stream without being "location-specific"?

#### Should all read requests get higher consistency, or only a few?

For example, if this would be a setting on the stream like `ReadConsistency: weak/high` that would mean that ALL read
requests would be served by the stream leader only if set to `high`. This has the side effect of lower availability when
there's no leader available at a given time.

But does this actually need to be for ALL read requests, or only a select few?

If high availability is valued, then the current Direct Get API could still be used while high consistency read requests
could be served by the stream leader only. Would such a hybrid approach even be desirable, given that now the app
developer will need to decide per process or app which consistency level to use? Is this additional complexity worth the
flexibility?

#### What should the performance considerations be?

Tradeoffs can be made regarding performance versus consistency.

Over-simplifying, there are two options:

- Reads go through Raft. Simplest way to implement and ensure no stale reads happen, but requires an additional network
  hop for consensus.
- Reads do not go through Raft. Requires a mechanism like a "leader lease", can immediately answer read requests like
  before, but requires timeout tuning and a new leader election to take way longer to happen.

Having reads go through Raft is essentially what etcd also did:
> When we evaluated etcd 0.4.1 in 2014, we found that it exhibited stale reads by default due to an optimization. While
> the Raft paper discusses the need to thread reads through the consensus system to ensure liveness, etcd performed reads
> on any leader, locally, without checking to see whether a newer leader could have more recent state. The etcd team
> implemented an optional quorum flag, and in version 3.0 of the etcd API, made linearizability the default for all
> operations except for watches.
> - https://jepsen.io/analyses/etcd-3.4.3 (2020-01-30)

Having leader leases is essentially what YugabyteDB did:
> Within a shard, Raft ensures linearizability for all operations which go through Raft’s log. However, for performance
> reasons, YugaByte DB does not use Raft’s consensus for reads. Instead, it cheats: reads return the local state from any
> Raft leader immediately, using leader leases to ensure safety. Using `CLOCK_MONOTONIC` for leases (instead of
`CLOCK_REALTIME`) insulates YugaByte DB from some classes of clock error, such as leap seconds.
> - https://jepsen.io/analyses/yugabyte-db-1.1.9 (2019-03-26)

Generally we hear users are willing to pay a performance "penalty" for higher consistency. But there are two things to
consider:

- The previous point of "Should all read requests get higher consistency, or only a few?", are all reads considered
  equal or should there be a hybrid approach? If hybrid, then going through Raft for some reads probably makes most sense?
- Leader leases are tricky to implement and can (under niche conditions) still result in stale reads. Do we prefer being
  able to strictly guarantee no stale reads?
- In some ways NATS' KV can be considered similar to etcd's KV, should we make similar choices?

> etcd ensures linearizability for all other operations by default. Linearizability comes with a cost, however, because linearized requests must go through the Raft consensus process. To obtain lower latencies and higher throughput for read requests, clients can configure a request’s consistency mode to serializable, which may access stale data with respect to quorum, but removes the performance penalty of linearized accesses’ reliance on live consensus.
> - https://etcd.io/docs/v3.5/learning/api_guarantees/
