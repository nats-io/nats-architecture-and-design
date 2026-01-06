# Key-Value Store Roadmap and future considerations

| Metadata | Value                 |
|----------|-----------------------|
| Date     | 2026-01-06            |
| Author   | @ripienaar, @piotrpio |
| Status   | Proposed              |
| Tags     | jetstream, client, kv |
| Updates  | ADR-8                 |

## Release History

| Revision | Date       | Author    | Info                           |
|----------|------------|-----------|--------------------------------|
| 1        | 2026-01-06 | @piotrpio | Moved roadmap items from ADR-8 |

## Context

This document contains the roadmap and future considerations for the JetStream backed key-value store. The contents of this document were moved from ADR-8 and are intended to provide a path forward for the development and enhancement of the key-value store functionality.

## Roadmap Items

### Read-Only Operation Mode

This is a read-only KV store handle, I call this out here to demonstrate that we need to be sure to support a read-only
variant of the client. One that will only function against a read replica and cannot support `Put()` etc.

That capability is important, how you implement this in your language is your choice. You can throw exceptions on `Put()`
when read-only or whatever you like.

The interface here is a guide of what should function in read-only mode.

```go
// RoKV is a read-only interface to a single key-value store bucket
type RoKV interface {
    // Get gets a key from the store
    Get(key string) (Entry, error)

    // History retrieves historic values for a key
    History(ctx context.Context, key string) ([]Entry, error)

    // Watch a key(s) for updates, the same Entry might be delivered more than once. Key can be a specific key, a NATS wildcard
    // or an empty string to watch the entire bucket
    Watch(ctx context.Context, keySpec string) (Watch, error)

    // Keys retrieves a list of all known keys in the bucket
    Keys(ctx context.Context) ([]string, error)

    // Close releases in-memory resources held by the KV, called automatically if the context used to create it is canceled
    Close() error

    // Status retrieves the status of the bucket
    Status() (Status, error)
}
```

### Multi-Cluster and Leafnode topologies

A bucket, being backed by a Stream, lives in one Cluster only. To make buckets available elsewhere we have to use
JetStream Sources and Mirrors.

In KV we call these `Toplogies` and adding *Topology Buckets* require using different APIs than the main Bucket API
allowing us to codify patterns and options that we support at a higher level than the underlying Stream options.

For example, we want to be able to expose a single boolean that says an Aggregate is read-only which would potentially
influence numerous options in the Stream Configuration.

![KV Topologies](images/0008-topologies.png)

To better communicate the intent than the word Source we will use `Aggregate` in KV terms:

 **Mirror**: Copy of exactly 1 other bucket. Used primarily for scaling out the `Get()` operations.

 * It is always Read-Only
 * It can hold a filtered subset of keys
 * Replicas are automatically picked using a RTT-nearest algorithm without any configuration
 * Additional replicas can be added and removed at run-time without any re-configuration of already running KV clients
 * Writes and Watchers are transparently sent to the origin bucket
 * Can replicate buckets from other accounts and domains

**Aggregate**: A `Source` that combines one or many buckets into 1 new bucket. Used to provide a full local copy of
other buckets that support watchers and gets locally in edge scenarios.

 * Requires being accessed specifically by its name used in a `KeyValue()` call
 * Can be read-only or read-write
 * It can hold a subset of keys from the origin buckets to limit data exposure or size
 * Can host watchers
 * Writes are not transparently sent to the origin Bucket as with Replicas, they either fail (default) or succeed and
   modify the Aggregate (opt-in)
 * Can combine buckets from multiple other accounts and domains into a single Aggregate
 * Additional Sources can be added after initially creating the Aggregate

Experiments:

These items we will add in future iterations of the Topology concept:

 * Existing Sources can be removed from an Aggregate. Optionally, but by default, purge the data out of the bucket
   for the Source being removed
 * Watchers could be supported against a Replica and would support auto-discovery of nearest replica but would
  minimise the ability to add and remove Replicas at runtime

*Implementation Note*: While this says Domains are supported, we might decide not to implement support for them at
this point as we know we will revisit the concept of a domain. The existing domain based mirrors that are supported
in KeyValueConfig will be deprecated but supported for the foreseeable future for those requiring domain support.

#### Creation of Aggregates

Since NATS Server 2.10 we support transforming messages as a stream configuration item. This allows us to source one
bucket from another and rewrite the keys in the new bucket to have the correct name.

We will model this using a few API functions and specific structures:

```go
// KVAggregateConfig configures an aggregate
//
// This one is quite complex because are buckets in their own right and so inevitably need
// to have all the options that are in buckets today (minus the deprecated ones).
type KVAggregateConfig struct {
    Bucket       string
    Writable     bool
    Description  string
    Replicas     int
    MaxValueSize int32
    History      uint8
    TTL          time.Duration
    MaxBytes     int64
    Storage      KVStorageType // a new kv specific storage struct, for now identical to normal one
    Placement    *KVPlacement // a new kv specific placement struct, for now identical to normal one
    RePublish    *KVRePublish // a new kv specific replacement struct, for now identical to normal one
    Origins      []*KVAggregateOrigin
}

type KVAggregateOrigin struct {
    Stream   string   // note this is Stream and not Bucket since the origin may be a mirror which may not be a bucket
    Bucket   string   // in the case where we are aggregating from a mirror, we need to know the bucket name to construct mappings
    Keys     []string // optional filter defaults to >
    External *ExternalStream
}

// CreateAggregate creates a new read-only Aggregate bucket with one or more sources
CreateAggregate(ctx context.Context, cfg KVAggregateOrigin) (KeyValue, error) {}

// AddAggregateOrigin updates bucket by adding new origin cfg, errors if bucket is not an Aggregate
AddAggregateOrigin(ctx context.Context, bucket KeyValue, cfg KVAggregateOrigin) error {}
```

To copy the keys `NEW.>` from bucket `ORDERS` into `NEW_ORDERS`:

```go
bucket, _ := CreateAggregate(ctx, KVAggregateConfig{
    Name: "NEW_ORDERS",
    Writable: false,
    Origins: []KVAggregateOrigin{
        {
            Stream: "KV_ORDERS",
            Keys: []string{"NEW.>"}
        }
    }
})
```

We create the new stream with the following partial config, rest as per any other KV, if the `orders` handle :

```json
    "subjects": []string{},
    "deny_delete": true,
    "deny_purge": true,
    "sources": [
      {
        "name": "KV_ORDERS",
        "subject_transforms": [
          {
            "src": "$KV.ORDERS.NEW.>",
            "dest": "$KV.NEW_ORDERS.>"
          }
        ]
      }
    ],
```

When writable, configure as normal just add the sources.

This results in all messages from `ORDERS` keys `NEW.>` to be copied into `NEW_ORDERS` and the subjects rewritten on
write to the new bucket so that a unmodified KV client on `NEW_ORDERS` would just work.

#### Creation of Mirrors

Replicas can be built using the standard mirror feature by setting `mirror_direct` to true as long as the origin bucket
also has `allow_direct`. When adding a mirror it should be confirmed that the origin bucket has `allow_direct` set.

We will model this using a few API functions and specific structures:

```go
type KVMirrorConfig struct {
    Name         string // name, not bucket, as this may not be accessed as a bucket
    Description  string
    Replicas     int
    History      uint8
    TTL          time.Duration
    MaxBytes     int64
    Storage      StorageType
    Placement    *Placement

    Keys         []string // mirrors only subsets of keys
    OriginBucket string
    External     *External
}

// CreateMirror creates a new read-only Mirror bucket from an origin bucket
CreateMirror(ctx context.Context, cfg KVMirrorConfig)  error {}
```

These mirrors are not called `Bucket` and may not have the `KV_` string name prefix as they are not buckets and cannot
be used as buckets without significant changes in how a KV client constructs its key names etc, we have done this in
the leafnode mode and decided it's not a good pattern.

When creating a replica of `ORDERS` to `MIRROR_ORDERS_NYC` we do:

```go
err := CreateMirror(ctx, origin, KVMirrorConfig{
    Name: "MIRROR_ORDERS_NYC",
    // ...
    OriginStream: "ORDERS"
})
```

When a direct read is done the response will be from the rtt-nearest mirror.  With a mirror added the `nats` command
can be used to verify that a alternative location is set:

```
$ nats s info KV_ORDERS
...
State:

            Alternates: MIRROR_ORDERS_NYC: Cluster: nyc Domain: hub
                                KV_ORDERS: Cluster: lon Domain: hub

```

Here we see a RTT-sorted list of alternatives, the `MIRROR_ORDERS_NYC` is nearest to me in the RTT sorted list.

When doing a direct get the headers will confirm the mirror served the request:

```
$ nats req '$JS.API.DIRECT.GET.KV_ORDERS.$KV.ORDERS.NEW.123' ''
13:26:06 Sending request on "JS.API.DIRECT.GET.KV_ORDERS.$KV.ORDERS.NEW.123"
13:26:06 Received with rtt 1.319085ms
13:26:06 Nats-Stream: MIRROR_ORDERS_NYC
13:26:06 Nats-Subject: $KV.ORDERS.NEW.123
13:26:06 Nats-Sequence: 12
13:26:06 Nats-Time-Stamp: 2023-10-16T12:54:19.409051084Z

{......}
```

As mirrors support subject filters these replicas can hold region specific keys.

As this is a `Mirror` this stream does not listen on a subject and so the only way to get data into it is via the origin
bucket.  We should also set the options to deny deletes and purges.

### Additional Considerations

In addition to the above items, some other considerations include:

* Merged buckets using NATS Server 2.10 subject transforms
* Replica auto discovery for mirror based replicas
* Read-only operation mode
* Read cache against with replica support
* Ranged operations
* Additional Operation that indicates server limits management deleted messages
* Standard Codecs that support zero-trust data storage with language interop
* Formalise leader election against keys
* Set management against key ranges to enable service discovery and membership management
* Distributed locks against a key
* Pluggable storage backends
