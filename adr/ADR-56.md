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
| 3        | 2025-12-05 | @MauriceVanVeen             | Add design for linearizable reads                 |

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

| Stream configuration                     | JetStream API                     | Description                                                                                                                                     | Level of consistency                                                                                                                                                                                                                                                                                                                                                   |
|:-----------------------------------------|:----------------------------------|:------------------------------------------------------------------------------------------------------------------------------------------------|:-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `AllowDirect` disabled.                  | `$JS.API.STREAM.MSG.GET.<stream>` | An API meant only for management outside of the hot path. The request goes to every server, but is normally only answered by the stream leader. | Current highest level of read consistency. Only the stream leader answers, but stale reads are technically possible after leader changes or during network partitions since an old leader could still answer before the current leader does.                                                                                                                           |
| `AllowDirect` enabled.                   | `$JS.API.DIRECT.GET.<stream>`     | If the stream is replicated, the followers will also answer read requests.                                                                      | Higher availability read responses but with lower consistency. A read request will be randomly served by a server hosting the stream. Recently written data is not guaranteed to be returned on a subsequent read request.                                                                                                                                             |
| `MirrorDirect` enabled on mirror stream. | `$JS.API.DIRECT.GET.<stream>`     | If the stream is mirrored, the mirror can also answer read requests. For example a mirror stream in a different cluster or on a leaf node.      | Higher availability with potential of fast local read responses but with lowest consistency. Mirrors can be in any relative state to the source. Although mirrors will initially wait with responding to read requests until they're _largely_ up-to-date, they don't offer a way to stop responding to reads if contact with the upstream was lost for a long period. |

Additionally, if a stream is replicated and a consumer is created, there is no guarantee that the consumer can
immediately observe all the written messages at that time. For example, if a R1 consumer is created on a follower not up
to date on all writes yet. The consumer will eventually observe all the writes as it keeps on fetching new messages as
they come in.

## Proposal to add linearizability

Newer server versions should support more configurability or in general higher levels of consistency as opt-in. For
example, higher read consistency for consumers can be achieved by having consumer CRUD operations go through the
stream's Raft log instead of the Meta Raft log, which ensures that a consumer created at time X in the stream log can
observe all the stream writes up to time X.

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

### Design

The design introduces 'linearizable reads' to JetStream in two ways:

- Stream-level opt-in; an easy 'toggle' to get high-consistency reads, with some notes of caution for specific
  topologies.
- A new API that's specifically used for this purpose and guarantees high-consistency reads from anywhere a client is
  connected.

#### Stream-level opt-in

The stream configuration will be extended with a new setting: `ReadConsistency`. This setting will be a string that
can be set to different values depending on the desired consistency level. Specifically, when the `default` value is
used, the `AllowDirect` and `MirrorDirect` can be manually specified. If `ReadConsistency` is not set to `default`, the
read consistency level will take control over these fields, not allowing them to be manually set. The `ReadConsistency`
setting will require API Level 3.

| Value     | Description                                                                                                                                                                                                        |
|:----------|:-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `default` | Used when the consistency is not explicitly specified. Used for backward-compatibility, can be used to allow manually specifying `AllowDirect` and `MirrorDirect`.                                                 |
| `weak`    | Weak read consistency valuing availability and fast responses over consistency. If the stream is replicated, followers can answer read requests. If the stream is mirrored, mirrors can also answer read requests. |
| `strong`  | Strong read consistency valuing consistency over availability. The stream will guarantee linearizable consistency. If set on a stream that's acting as a mirror, it will guarantee sequential consistency.         |

The addition of `ReadConsistency` allows for various levels of read consistencies (from weakest to strongest):

- Leader/Follower/Mirror reads, high availability with potential of fast local read responses by a mirror with no
  cross-request/session consistency guarantees, weakest consistency: `ReadConsistency: weak` on both the stream and the
  mirror.
- Leader/Follower reads, high availability with no cross-request/session consistency guarantees: `ReadConsistency: weak`
  with no mirrors.
- Leader/Mirror reads, higher consistency with potential of fast local read responses by a replicated mirror with
  cross-request/session consistency guarantees (when using a connection to the cluster/server hosting either the stream
  or mirror, but not both). Both the stream and mirror are set to `ReadConsistency: strong`. The stream itself will
  guarantee linearizable consistency as specified below, the mirror will guarantee sequential consistency.
- Linearizable reads, highest level of consistency: `ReadConsistency: strong` with no mirrors. A read request which is
  only answered by the stream leader if it can guarantee linearizability.

A note of caution when using `ReadConsistency: strong` for a mirrored stream; the mirror's consistency level will not be
linearizable, it will be sequential. This can be a desirable guarantee when mirroring a stream on a local leaf node. The
difference of consistency will be clear since the leaf node is likely meant to be "loosely" connected to the cluster,
and the client will be guaranteed to always connect to the leaf node and not the cluster. However, if a mirror is
created in a cluster part of a super cluster setup, this could be problematic depending on the use case. If clients are
allowed to reconnect between clusters, this could result in the consistency levels changing between linearizable to
sequential. For example, if the client was first connected to the cluster containing the stream and then reconnected to
the cluster containing the mirror. This will depend on how the client is configured and what the desired use case is.
Please keep this in mind when designing your topology.

Additionally, if the `ReadConsistency` setting is set to anything other than `default`, the 'Linearizable reads API'
specified in the next section will be enabled. This can be used to guarantee linearizable reads even if the stream is
configured to `ReadConsistency: weak`, as well as guaranteeing linearizable reads if a read would otherwise be served by
a local mirror.

#### Linearizable reads API

This linearizable reads API allows location transparent access; it doesn't matter if a client is connected via a leaf
node and several hops to the stream leader, if it requires linearizable reads, it can use this new API to get this
guarantee. Additionally, this API is enabled through setting a non-default value for `ReadConsistency`. If enabled, the
leader will also respond to `$JS.API.DIRECT.GET.<stream>`, not requiring `AllowDirect`. This allows clients to migrate
away from using the `$JS.API.STREAM.MSG.GET.<stream>` API, since it's primarily meant for management purposes only.

- Introduce a new API for linearizable reads: `$JS.API.DIRECT_LEADER.GET.<stream>`.
- The new API will be similar to `$JS.API.DIRECT.GET.<stream>` but will go to the stream leader only. If it's a
  replicated stream, the read will need to ensure linearizability.
- The new API will be enabled by the `ReadConsistency` setting on the stream. Once enabled, the new API will be active,
  and the leader will also respond to `$JS.API.DIRECT.GET.<stream>`, not requiring `AllowDirect`. This allows clients to
  use the DirectGet API instead of the MsgGet API:
    - If the user specifies requiring linearizable reads:
        - If `ReadConsistency` is NOT set, then the client should return an error that 'linearizable reads are not
          enabled for this stream'.
        - If `ReadConsistency` is set, then the client should use the new `$JS.API.DIRECT_LEADER.GET.<stream>` API.
        - If the client does not know the current value of `ReadConsistency` (since it might not have access to the
          stream info), then the client should use the new `$JS.API.DIRECT_LEADER.GET.<stream>` API anyway. A '503 No
          Responders' error will be returned to the user, which will either mean there's temporarily no leader
          available, or the stream is not configured to allow linearizable reads, but the client can't differentiate
          between these two cases.
    - If `AllowDirect` is set, or if `ReadConsistency` is non-`default`, the client should use
      `$JS.API.DIRECT.GET.<stream>`.
    - If none are specified, the client falls back to the `$JS.API.STREAM.MSG.GET.<stream>` API.

This design makes linearizability an opt-in and a conscious choice by a user. The server provides all the tools required
for various consistency levels. Clients can ease the user experience by offering:

- Per-request linearizable read opt-in. For example: `js.GetMsg("my-stream", 1, nats.Linearizable()` and
  `js.GetLastMsg("stream", "subject", nats.Linearizable())`. This allows the user to value availability by default, but
  opt in to linearizable reads for the requests that need it.
- Per-object linearizable read opt-in. Opt-in to linearizable reads for a specific stream, KV or Object Store. All reads
  to that 'object' will use linearizable read requests by default, without needing the user to specify this on a
  per-request basis. For example:
  `js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "TEST", Replicas: 3, LinearizableReads: true})`.

The clients are free to implement this in a way that's best for the given language, but should generally provide both
the per-request and per-object options.

The server can implement linearizable reads by having them "go through Raft". However, this is expensive as it requires
an additional network hop for consensus. Instead, the server can implement linearizable reads by using leader leases.
This can be done like how it is described in "LeaseGuard: Raft Leases Done Right":

- The leader refreshes its leader lease by writing an entry into the log. This will be automatic during normal stream
  operations, but when idle will need to happen based on a noop-entry to prevent cold-starts or read timeouts.
- The leader can serve reads as long as its leader lease based on the last committed entry's age is still active. The
  age is based on when the entry was created, not when it was committed.
- The leader will stop serving reads after the lease expires.
- A new leader will wait for the lease to expire before answering new reads/writes. It must wait at least the lease
  duration after the last entry in its log was received.
- This can be implemented by using monotonic clock readings.
- The leader lease can immediately expire, allowing a new leader to immediately start serving reads/writes after
  becoming leader, as a result of manual/automatic step-down as part of a `LeaderTransfer`.
