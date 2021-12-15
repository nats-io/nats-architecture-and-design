# JetStream based Key-Value Stores

|Metadata|Value|
|--------|-----|
|Date    |2021-06-30|
|Author  |@ripienaar|
|Status  |Implemented|
|Tags    |jetstream, client, kv|

## Context

This document describes a design and initial implementation of a JetStream backed key-value store. The initial implementation
is available in the CLI as `nats kv` with the reference client implementation being the `nats.go` repository.

This document aims to guide client developers in implementing this feature in the language clients we maintain.

## Status and Roadmap

The API is now stable and considered version 1.0, we have several NATS maintained client libraries with this feature supported
and a few community efforts are under way.

A roadmap is included below, but note this is subject to change. The API as is will not have breaking changes until 2.0, but
additional behaviors will come during the 1.x cycle.

### 1.0

 * Multiple named buckets hosting a hierarchy of keys with n historical values kept per key. History set per bucket and capped at 64.
 * Put and Get of `string(k)=[]byte(v)` values
 * Put only if the revision of the last value for a key matches an expected revision
 * Put only if the key does not currently exist, or if when the latest historical operation is a delete operation.
 * Key deletes preserves history
 * Keys can be expired from the bucket based on a TTL, TTL is set for the entire bucket
 * Watching a specific key, ranges based on NATS wildcards, or the entire bucket for live updates
 * Read-after-write safety
 * Valid keys are `\A[-/_=\.a-zA-Z0-9]+\z`, additionally they may not start or end in `.`
 * Valid buckets are `\A[a-zA-Z0-9_-]+\z`
 * Custom Stream Names and Stream ingest subjects to cater for different domains, mirrors and imports
 * Key starting with `_kv` is reserved for internal use
 * CLI tool to manage the system as part of `nats`, compatible with client implementations
 * Accept arbitrary application prefixes, as outlined in [ADR-19](https://github.com/nats-io/nats-architecture-and-design/blob/main/adr/ADR-19.md)

### 1.1

 * Encoders and Decoders for keys and values
 * Additional Operation that indicates server limits management deleted messages

### 1.2

 * Read replicas facilitated by Stream Mirrors
 * Read-only operation mode
 * Replica auto discovery
 * Read cache against with replica support
 * Ranged operations

### 1.3

 * Standard Codecs that support zero-trust data storage with language interop

### 2.0

 * Formalise leader election against keys
 * Set management against key ranges to enable service discovery and membership management
 * Per value TTLs
 * Distributed locks against a key
 * Pluggable storage backends

## Data Types

Here's rough guidance, for some clients in some places you might not want to use `[]string` but an iterator, that's
fine, the languages should make appropriate choices based on this rough outline.

### Entry

This is the value and associated metadata made available over watchers, `Get()` etc. All backends must return an implementation providing at least this information.

```go
type Entry interface {
	// Bucket is the bucket the data was loaded from
	Bucket() string
	// Key is the key that was retrieved
	Key() string
	// Value is the retrieved value
	Value() []byte
	// Created is the time the data was received in the bucket
	Created() time.Time
	// Revision is a unique sequence for this value
	Revision() uint64
	// Delta is distance from the latest value. If history is enabled this is effectively the index of the historical value, 0 for latest, 1 for most recent etc.
	Delta() uint64
	// Operation is the kind of operation this entry represents, enum of PUT, DEL or PURGE
	Operation() Operation
}
```

### Status

This is the status of the KV as a whole

```go
type Status interface {
	// Bucket the name of the bucket
	Bucket() string

	// Values is how many messages are in the bucket, including historical values
	Values() uint64

	// History returns the configured history kept per key
	History() uint64

	// TTL is how long the bucket keeps values for
	TTL() time.Duration

	// Keys return a list of all keys in the bucket - not possible now except in caches
	Keys() ([]string, error)

	// BackingStore is a name indicating the kind of backend
	BackingStore() string
}
```

The `BackingStore` describes the type of backend and for now returns `JetStream` for this implementation.

Languages can choose to expose additional information about the bucket along with this interface, in the Go implementation
the `Status` interface is above but the `JetStream` specific implementation can be cast to gain access to `StreamInfo()`
for full access to JetStream state.

Other languages do not have a clear 1:1 match of the above idea so maintainers are free to do something idiomatic.

## RoKV

**NOTE:** Out of scope for version 1.0

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

## KV

This is the read-write KV store handle, every backend should implement a language equivalent interface. But note the comments
by `RoKV` for why I call these out separately.

```go
// KV is a read-write interface to a single key-value store bucket
type KV interface {
	// Put saves a value into a key
	Put(key string, val []byte, opts ...PutOption) (revision uint64, err error)

	// Create is a variant of Put that only succeeds when the key does not exist if last historic event is a delete or purge operation
	Create(key string, val []byte) (revision uint64, err error)

	// Update is a variant of Put that only succeeds when the most recent operation on a key has the expected revision
	Update(key string, value []byte, last uint64) (revision uint64, err error)

	// Delete purges the key in a way that preserves history subject to the bucket history setting limits
	Delete(key string) error

	// Purge removes all data for a key including history, leaving 1 historical entry being the purge
	Purge(key string) error

	// Destroy removes the entire bucket and all data, KV cannot be used after
	Destroy() error

	RoKV
}
```

## Storage Backends

We do have plans to support, and provide, commercial KV as part of our NGS offering, however there will be value in an
open source KV implementation that can operate outside of NGS, especially one with an identical API.

Today we will support a JetStream backend as documented here, future backends will have to be able to provide these
features, that is, this is the minimal feature set we can expect from any KV backend.

Client developers should keep this in mind while developing the library to at least not make it impossible to support
later.

### JetStream interactions

The features to support KV is in NATS Server 2.6.0.

#### Buckets

A bucket is a Stream with these properties:

 * The main write bucket must be called `KV_<Bucket Name>`
 * The ingest subjects must be `$KV.<Bucket Name>.>`
 * The bucket history is achieved by setting `max_msgs_per_subject` to the desired history level. The maximum allowed size is 64.
 * Safe key purges that deletes history requires rollup to be enabled for the stream using `rollup_hdrs`
 * Write replicas are File backed and can have a varying R value
 * Key TTL is managed using the `max_age` key
 * Duplicate window must be same as `max_age` when `max_age` is less than 2 minutes
 * Maximum value sizes can be capped using `max_msg_size`
 * Maximum number of keys cannot currently be limited
 * Overall bucket size can be limited using `max_bytes`

Here is a full example of the `CONFIGURATION` bucket:

```json
{
  "name": "KV_CONFIGURATION",
  "subjects": [
    "$KV.CONFIGURATION.>"
  ],
  "retention": "limits",
  "max_consumers": -1,
  "max_msgs_per_subject": 5,
  "max_msgs": -1,
  "max_bytes": -1,
  "max_age": 0,
  "max_msg_size": -1,
  "storage": "file",
  "discard": "old",
  "num_replicas": 1,
  "duplicate_window": 120000000000,
  "rollup_hdrs": true,
  "deny_delete": true
}
```

#### Storing Values

Writing a key to the bucket is a basic JetStream request.

The KV key `auth.username` in the `CONFIGURATION` bucket is written sent, using a request, to `$KV.CONFIGURATION.auth.username`.

To implement the feature that would accept a write only if the revision of the current value of a key has a specific revision
we use the new `Nats-Expected-Last-Subject-Sequence` header. The special value `0` for this header would indicate that the message 
should only be accepted if it's the first message on a subject. This is purge aware, ie. if a value is in and the subject is purged 
again a `0` value will be accepted.

This can be implemented as a `PutOption` ie. `Put("x.y", val, UpdatesRevision(10))`, `Put("x.y", val, MustCreate())` or 
by adding the `Create()` and `Update()` helpers, or both. Other options might be `UpdatesEntry(e)`, language implementations
can add what makes sense in addition.

To use this header correctly with KV when a value of `0` is given, on failure that indicates it's not the first message we
should attempt to load the current value and if that's a delete do an update with `Nats-Expected-Last-Subject-Sequence` equalling
to the value of the deleted message that was retrieved.

#### Retrieving Values

There are different situations where messages will be retrieved using different APIs, below describes the different models.

In all cases we return a generic `Entry` type.

Deleted data - (see later section on deletes) - has the `KV-Operation` header set to `DEL` or `PURGE`, really any value other than unset
- a value received from either of these methods with this header set indicates the data has been deleted. A delete operation is turned 
into a `key not found` error in basic gets and into a `Entry` with the correct operation value set in watchers or history. 

##### Get Operation

We have extended the `io.nats.jetstream.api.v1.stream_msg_get_request` API to support loading the latest value for a specific
subject.  Thus a read for `CONFIGURATION.username` becomes a `io.nats.jetstream.api.v1.stream_msg_get_request` with the
`last_by_subj` set to `$KV.CONFIGURATION.auth.username`.

##### History

These operations require access to all values for a key, to achieve this we create an ephemeral consumer on filtered by the subject
and read the entire value list in using `deliver_all`. Use an Ordered Consumer to do this efficiently.

JetStream will report the Pending count for each message, the latest value from the available history would have a pending of `0`.
When constructing historic values, dumping all values etc we ensure to only return pending 0 messages as the final value

##### Watch 

A watch, like History, is based on ephemeral consumers reading values using Ordered Consumers, but now we start with the new 
`last_per_subject` initial start, this means we will get all matching latest values for all keys.

Watch can take options to allow including history, sending only new updates or sending headers only. Using a Watch end users
should be able to implement at minimum history retrieval, data dumping, key traversal or updates notification behaviors.

#### Deleting Values

Since the store support history - via the `max_age` for messages - we should preserve history when deleting keys. To do this we
place a new message in the subject for the key with a nil body and the header `KV-Operation: DEL`.

This preserves history and communicate to watchers, caches and gets that a delete operation should be handled - clear cache,
return no key error etc.

#### Purging a key

Purge is like delete but history is not preserved. This is achieved by publishing a message in the same manner as Delete using the
`KV-Operation: PURGE` header but adding the header `Nats-Rollup: sub` in addition.

This will instruct the server to place the purge operation message in the stream and then delete all messages for that key up to 
before the delete operation.

#### List of known keys

Keys return a list of all keys defined in the bucket, this is done using a headers-only Consumer set to deliver last per subject.

Any received messages that isn't a Delete/Purge operation gets added to the list based on parsing the subject.

#### Deleting a bucket

Remove the stream entirely.

#### Watchers Implementation Detail

Watchers support sending received `PUT`, `DEL` and `PURGE` operations across a channel or language specific equivalent.

Watchers support accepting simple keys or ranges, for example watching on `auth.username` will get just operations on that key,
but watching `auth.>` will get operations for everything below `auth.`, the entire bucket can be watched using an empty key or a 
key with wildcard `>`.

We need to signal when we reach the end of the initial data set to facilitate use cases such as dumping a bucket, iterating keys etc.
Languages can implement an End Of Initial Data signal in a language idiomatic manner. Internal to the watcher you reach this state the
first time any message has a `Pending==0`. This signal must also be sent if no data is present - either by checking for messages using 
`GetLastMsg()` on the watcher range or by inspecting the Pending+Delivered after creating the consumer. The signal must always be sent.

Whatchers should support at least the following options. Languages can choose to support more models if they wish, as long as that
is clearly indicated as a language specific extension. Names should be language idiomatic but close to these below.

|Name|Description|
|----|-----------|
|`IncludeHistory`|Send all available history rather than just the latest entries|
|`IgnoreDeletes`|Only sends `PUT` operation entries|
|`MetaOnly`|Does not send any values, only metadata about those values|
|`UpdatesOnly`|Only sends new updates made, no current or historical values are sent. The End Of Initial Data marker is sent as soon as the watch starts.|

The default behavior with no options set is to send all the `last_per_subject` values, including delete/purge operations.

#### API Design notes

The API here represents a minimum, languages can add local flavour to the API - for example one can add `PutUint64()` and `GetUint64()`
in addition to the `Get()` and `Put()` defined here, but it's important that as far as possible all implementations strive to match
the core API calls - `Get()`, `Put()` and to a lesser extent `Delete()` and `Purge()` and `Entry` - the rest, admin APIs, and supporting
calls can be adjusted to be a natural fit in the language and design.  This is in line with existing efforts to harmonize
`Subscribe()`, `Publish()` and more.

The design is based on extensive prior art in the industry. During development 20+ libraries in Go, Ruby, Python and Java were reviewed
for their design choices and this influenced things like `Get()` returning an `Entry`.

Consul Go:

```go
func (k *KV) Put(p *KVPair, q *WriteOptions) (*WriteMeta, error){}
func (k *KV) Get(key string, q *QueryOptions) (*KVPair, *QueryMeta, error)
```

Here KVPair has properties like `Key`, various revision, flags, timestamps etc and `Value []byte`.

Dynanic language libraries like Python for example return maps, but there too the return value of a default `get()` operation is a hash
with all metadata and value stored in `Value` or similar.

Various Java libraries from the Consul site were reviewed, there's a mixed bag but most seem to settle on `getXXX() with `getValue()` being
a common function name  and it returns a fat value object with lots of metadata. [Ecwid/consul-api](https://github.com/Ecwid/consul-api) is a good example.

etcd Go:

```go
func Put(ctx context.Context, key, val string, opts ...OpOption) (*PutResponse, error) {}
func Get(ctx context.Context, key string, opts ...OpOption) (*GetResponse, error) {}
```

Here `GetResponse` is much fatter than we propose as it also supports ranged queries and so multiple values, each being like our Entry.

The official `jetcd` library has a `get()` that returns a future and an equally fat ranged response.

Various Ruby etcd clients were reviewed, same design around `get` with a object returned.

So the overall consensus is that a `Entry` like entity should be returned, hard to say but indeed `Get(string) ([]byte,error)` would seem
like a good default design, but in the face of massive evidence that this is just not the chosen design I picked returning a `Entry` as
the default behavior.  But leaving it open to languages to add additional helpers.

Given that `get()` as a name isn't impossible in Java - see etcd for example - I think in the interest of harmonized client design we should
use `get()` wherever possible augmented by other get helpers.

Regarding `Put`, these other APIs do not tend to add other functions like `Create()` or `Update()`, they accept put options, like we do.
Cases where they want to build for example a CAS wrapper around their KV they will write a wrapper with CAS specific function names and more,
ditto for service registeries and so forth.

On the name `Entry` for the returned result. `Value` seemed a bit generic and I didn't want to confuse matters mainly in the go client
that has the unfortunate design of just shoving everything and the kitchen sink into a single package. `KVValue` is a stutter and so
settled on `Entry`.

