# JetStream Batch Publishing

| Metadata | Value                           |
|----------|---------------------------------|
| Date     | 2025-06-10                      |
| Author   | @ripienaar                      |
| Status   | Approved                        |
| Tags     | jetstream, server, client, 2.12 |

## Context

There exist a need to treat groups of related messages in batched manner, there are a few goals with this work:

 * We need to be able to improve performance
 * We need to deliver Atomic Batch writes - where an entire batch is stored or abandoned
 * We need to perform replication layer work to batch RAFT append entries

This ADR focus mainly on the first 2 sections.

## Atomic Batched Publishes

### Context 

There are many use cases for this but one scenario would demonstrate the underlying need that is shared by these cases.

When a KV store is used to store a User record the it might span many keys, the address for example might be 5 keys. Performing updates on these related keys is not safe today as writes might fail after a first update resulting in an address line in one city but a post code in another.

To address this we want to be able to deliver the 5 writes as a batch and the entire batch either fails or succeeds.

### Client Design

The client will signal batch start and membership using headers on published messages.

 * A batch will be started by adding the `Nats-Batch-Id:uuid` and `Nats-Batch-Sequence:1` headers using a request. 
 * Following messages in the same batch will include the `Nats-Batch-Id:uuid` header and increment `Nats-Batch-Sequence:n` by one,
 * The final message will have the headers `Nats-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:1` the server will reply with a pub ack.

The server will not acknowledge any of the publishes except the one doing the Commit, clients must publish using Core NATS publish.

The control headers are sent with payload, there are no additional messages to start and stop a batch we piggyback on the usual payload-bearing messages.

Clients can decide to optimise the empty acks by only sending a request every N messages or N bytes, this facilitates some flow control and awareness if the batch is getting to a server or not.

### Server Behavior Design

 * The server will limit the `Nats-Batch-ID` to 64 characters and response with a error Pub Ack if its too long
 * Server will reject messages for which the batch is unknown with a error Pub Ack
 * If messages in a batch is received and any gap is detected the batch will be rejected with a error Pub Ack
 * Check properties like `ExpectedLastSeq` using the sequences found in the stream prior to the batch, at the time when the batch is committed under lock for consistency. Rejects the batch with an error Pub Ack if any message fails these checks. Only the first message of the batch may contain `Nats-Expected-Last-Sequence` or `Nats-Expected-Last-Msg-Id`. Checks using `Nats-Expected-Last-Subject-Sequence` can only be performed if prior entries in the batch not also write to that same subject.
 * Abandon without error reply anywhere a batch that has not had messages for 10 seconds, an advisory will be raised on abandonment in this case
 * Send a pub ack on the final message that includes a new property `Batch:ID` and `Count:10`. The sequence in the ack would be the final message sequence, previous messages in the batch would be the preceding sequences

The server will operate under limits to safeguard itself:

 * Each stream can only have 50 batches in flight at any time
 * Each server can only have 1000 batches in flight at any time
 * A batch that has not had traffic for 10 seconds will be abandoned
 * Each batch can have maximum 1000 messages

### Stream State Constraints

Headers like `ExpectedLastSeq` and `LastMsgId` makes sense if checked before committing the batch aginst the pre-commit state of the Stream.  Unfortunately our current implementation would not make this feasible. 

Initial release of this feature will reject messages published with those headers and we might support them in future.

### Publish Acknowledgements

When the server sends a Pub Ack at the end of a batch the `PubAck` will set these 2 new fields

```go
type PubAck struct {
	// ...
	BatchId   string `json:"batch,omitempty"`
	BatchSize int    `json:"count,omitempty"`
}
```

### Abandonment Advisories

When a batch is abandoned it might be for reasons that will never be communicated back to clients, so we raise advisories:

```go
type BatchAbandonReason string

var (
	BatchUnknown BatchAbandonReason = "unknown"
	BatchTimeout BatchAbandonReason = "timeout"
)

type PubAck struct {
	// ...
	BatchId      string             `json:"batch"`
	Reason       BatchAbandonReason `json:"reason"`
}
```

The event type is `io.nats.jetstream.advisory.v1.batch_abandoned`.

### Stream Configuration

Streams get a new property that enables this behavior

```go
type StreamConfig struct {
	// ...
	AllowAtomicPublish bool `json:"allow_atomic,omitempty"`
}
```

The setting can be disabled and enabled using configuration updates.

Setting this to true should set the API level to 2.

### Mirrors and Sources

Mirrors can enable this setting as long as the mirror is unfiltered. Batches will be processed as above except when a batch gets rejected the mirror would need to resume as the sequence before the batch started which will result in retrying the batch write. 

Mirrors will ignore the various headers like `ExpectedLastSeq` as normal.

Streams with Sources cannot enable the `AllowAtomicPublish` and Sources may not be added to streams with `AllowAtomicPublish` set. 