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

When a KV store is used to store a User record the it might span many keys, the address for example might be 5 keys. 
Performing updates on these related keys is not safe today as writes might fail after a first update resulting in an 
address line in one city but a post code in another.

To address this we want to be able to deliver the 5 writes as a batch and the entire batch either fails or succeeds.

These features will be cumulative with other features such as `ExpectedLastSeq` and `ExpectedSeq`.

### Client Design

The client will signal batch start and membership using headers on published messages.

 * A batch will be started by adding the `Nats-Batch-ID:uuid` and `Nats-Batch-Seq:1` headers using a request, the 
   server will acknowledge the batch was started using an empty reply
 * Following messages in the same batch will include the `Nats-Batch-ID:uuid` header and increment 
   `Nats-Batch-Seq:n` by one, the server will acknowledge receipt using an empty reply
 * The final message will have the headers `Nats-Batch-ID:uuid`, `Nats-Batch-Seq:n` and `Nats-Batch-Commit:1` the 
   server will reply with a pub ack

The control headers are sent with payload, there are no additional messages to start and stop a batch we piggyback 
on the usual payload-bearing messages.

### Server Behavior Design

 * Server will reject messages for which the batch is unknown with a error Pub Ack
 * If messages in a batch is received and any gap is detected the batch will be rejected with a error Pub Ack
 * Check properties like `ExpectedLastSeq` using the sequences found in the stream prior to the batch, at the time when 
   the batch is committed under lock for consistency. Rejects the batch with a error Pub Ack if any message fails 
   these checks
 * Abandon without error reply anywhere a batch that has not had messages for 10 seconds, an advisory will be raised on abandonment in this case
 * Send a pub ack on the final message that includes a new property `Batch:ID` and `Messages:10`. The sequence in the ack would be the final message sequence, previous messages in the batch would be the preceding sequences

The server will operate under limits to safeguard itself:

 * Each stream can only have 50 batches in flight at any time
 * Each server can only have 1000 batches in flight at any time
 * A batch that has not had traffic for 10 seconds will be abandoned
 * Each batch can have maximum 1000 messages

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

When a batch is abandoned it might be for reasons that will never be communicated back to clients, so we raise 
advisories:

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

### Stream Configuration

Streams get a new property that enables this behavior

```go
type StreamConfig struct {
	// ...
	AllowAtomicPublish bool `json:"allow_atomic,omitempty"`
}
```
### Mirrors and Sources

Mirrors will ignore these headers, Sources will process them as above.