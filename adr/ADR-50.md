# JetStream Batch Publishing

| Metadata | Value                                 |
|----------|---------------------------------------|
| Date     | 2025-06-10                            |
| Author   | @ripienaar                            |
| Status   | Approved                              |
| Tags     | jetstream, server, client, 2.12, 2.14 |

| Revision | Date       | Author          | Info                                                    | Server Version | API Level |
|----------|------------|-----------------|---------------------------------------------------------|----------------|-----------|
| 1        | 2025-06-10 | @ripienaar      | Initial design                                          | 2.12.0         | 2         |
| 2        | 2025-09-08 | @MauriceVanVeen | Initial release                                         | 2.12.0         | 2         |
| 3        | 2025-09-11 | @piotrpio       | Add server codes                                        | 2.12.0         | 2         |
| 4        | 2025-09-11 | @ripienaar      | Restore optional ack behavior                           | 2.12.0         | 2         |
| 5        | 2025-09-25 | @ripienaar      | Support batch commit without storing the commit message | 2.14.0         | 3         |

## Context

There exists a need to treat groups of related messages in a batched manner, there are a few goals with this work:

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

 * A batch will be started by adding the `Nats-Batch-Id:uuid` and `Nats-Batch-Sequence:1` headers using a *request*, the server will reply with error or zero byte message. Maximum length of the ID is 64 characters.
 * Following messages in the same batch will include the `Nats-Batch-Id:uuid` header and increment `Nats-Batch-Sequence:n` by one, and might optionally include a reply subject that will receive a zero byte reply
 * The final message will have the headers `Nats-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:1` the server will store the message, commit the batch and reply with a pub ack. 
 * Alternatively the final message will have headers `Nats-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:eob` the server will not store the message, commit the batch and reply with a pub ack  

The server will acknowledge in the following manner:

 * The initial message will get an error - for example, feature not supported - or a zero byte ack
 * Following messages, that have a reply set, will get a zero byte ack
 * The final message will get a pub ack as described later

The control headers are sent with payload, there are no additional messages to start and stop a batch we piggyback on the usual payload-bearing messages.

When `Nats-Batch-Commit:eob` is used to commit the final message if the `Nats-Required-Api-Level` is set it should be evaluated. Using the `eob` option should require level `3`.

#### Server Errors

The server will respond with the following errors if committing a batch fails:

| ErrCode | Code | Description                                                                          |
|---------|------|--------------------------------------------------------------------------------------|
| 10174   | 400  | Batch publish not enabled on stream                                                  |
| 10176   | 400  | Batch publish is incomplete and was abandoned                                        |
| 10179   | 400  | Batch publish ID is invalid (exceeds 64 characters)                                  |
| 10175   | 400  | Batch publish sequence is missing                                                    |
| 10199   | 400  | Batch publish sequence exceeds server limit (default 1000)                           |
| 10177   | 400  | Batch publish unsupported header used (`Nats-Expected-Last-Msg-Id` or `Nats-Msg-Id`) |

### Server Behavior Design

 * The server will limit the `Nats-Batch-ID` to 64 characters and response with a error Pub Ack if its too long
 * Server will reject messages for which the batch is unknown with a error Pub Ack
 * If messages in a batch is received and any gap is detected the batch will be rejected with a error Pub Ack
 * Check properties like `ExpectedLastSeq` using the sequences found in the stream prior to the batch, at the time when the batch is committed under lock for consistency. Rejects the batch with an error Pub Ack if any message fails these checks. Only the first message of the batch may contain `Nats-Expected-Last-Sequence`. Checks using `Nats-Expected-Last-Subject-Sequence` can only be performed if prior entries in the batch do not also write to that same subject.
 * Abandon without error reply anywhere a batch that has not had messages for 10 seconds, an advisory will be raised on abandonment in this case
 * Send a pub ack on the final message that includes a new property `Batch:ID` and `Count:10`. The sequence in the ack would be the final message sequence, previous messages in the batch would be the preceding sequences
 * If a stream is operating on the `PersistMode: async` mode, any batch published to it must fail

The server will operate under limits to safeguard itself:

 * Each stream can only have 50 batches in flight at any time
 * Each server can only have 1000 batches in flight at any time
 * A batch that has not had traffic for 10 seconds will be abandoned
 * Each batch can have maximum 1000 messages

### Stream State Constraints

Headers like `MsgId` and `LastMsgId` are currently not supported, there are ongoing discussions about how de-duplication is expected to work when using atomic batch publishing.

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
	BatchTimeout    BatchAbandonReason = "timeout"
	BatchIncomplete BatchAbandonReason = "incomplete"
)

type Advisory struct {
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

Mirrors can't enable this setting, and will ignore the various headers like `ExpectedLastSeq` and the batching headers.

Streams with Sources can enable the `AllowAtomicPublish`, but sources will ignore the batching headers when sourced into the stream similar to how mirrors work.
