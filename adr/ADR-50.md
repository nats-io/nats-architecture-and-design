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
| 6        | 2025-10-02 | @MauriceVanVeen | Support deduplication                                   | 2.12.1         | 2         |
| 7        | 2025-10-08 | @ripienaar      | Support fast ingest                                     | 2.14.0         | 3         |

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
 * If the final message has the headers `Nats-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:1`, the server will store the message, commit the batch and reply with a pub ack. 
 * Otherwise, the final message will have headers `Nats-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:eob` and the server will commit the batch without storing the message and reply with a pub ack.

The server will acknowledge in the following manner:

 * The initial message will get an error - for example, feature not supported - or a zero byte ack
 * Following messages, that have a reply set, will get a zero byte ack
 * The final message will get a pub ack as described later
 * The server will check `Nats-Required-Api-Level` for every batch related message. If for any message the check fails the batch is abandoned, with advisory, and if a reply is set a full error ack is sent.

### Server Errors

The server will respond with the following errors if committing a batch fails:

| ErrCode | Code | Description                                                         |
|---------|------|---------------------------------------------------------------------|
| 10174   | 400  | Batch publish not enabled on stream                                 |
| 10176   | 400  | Batch publish is incomplete and was abandoned                       |
| 10179   | 400  | Batch publish ID is invalid (exceeds 64 characters)                 |
| 10175   | 400  | Batch publish sequence is missing                                   |
| 10199   | 400  | Batch publish sequence exceeds server limit (default 1000)          |
| 10177   | 400  | Batch publish unsupported header used (`Nats-Expected-Last-Msg-Id`) |
| 10201   | 400  | Batch publish contains duplicate message id (`Nats-Msg-Id`)         |

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

The `LastMsgId` header is currently not supported. A batch will be rejected if this header is used, but we might support this header in the future.

Initial release of this feature rejects the use of `MsgId`. Starting from 2.12.1 de-duplication is supported and a batch will be rejected with an error if it contains a duplicate message.

## Fast-ingest Batch Publishing

### Context

Today we have Async publishing in the clients that aims to move data into a stream at high speed, it works well but pays a very high cost for managing acknowledgements both in client and server.

We would like to build on the batching behaviours introduced here but deliver high performance ingest without atomicity.

Crucially these batches are not pre-staged and so will not be limited to 1000 messages like Atomic ones.

### Control Channel

The heart of this design is control channel that is open and kept open for the duration of the batch. In practise this the reply subject of the first message will be used for all signaling during the batch.

The server will send acks over the channel on a frequency like once every 10 messages or once every 5MB. Crucially if at any stage an error is encountered errors can be sent back immediately and received by the client.

Clients will not wait for each ack like they would in a standard JS Publish instead they will maintain a count of maximum outstanding acknowledgements from the server, this is part flow-control and part outage detection.

The server on the other hand will be able to adjust the frequency of acks based on internal metrics of the stream such as the size of the in-flight RAFT proposals. In this way the server and clients will balance speed of fast ingest vs responsiveness of interactive commands requiring RAFT proposals.

At all times the server will maintain its self-protection mechanisms like dropping messages when buffers are full etc.

### Client Design

The client will signal batch start, membership and flow control characteristics using headers on published messages.

 * The client will set up a Inbox subscription that will be used for the duration of the batch
 * A batch will be started by adding the `Nats-Fast-Batch-Id:uuid`, `Nats-Flow:10` and `Nats-Batch-Sequence:1` headers using a *request* with reply being the above Inbox, the server will reply with error or zero byte message. Maximum length of the ID is 64 characters.
 * Following messages in the same batch will include the `Nats-Fast-Batch-Id:uuid` header and increment `Nats-Batch-Sequence:n` by one, and might optionally include a reply set to the same Inbox to force a immediate zero byte reply
 * If the final message has the headers `Nats-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:1`, the server will store the message, commit the batch and reply with a pub ack.
 * Otherwise, the final message will have headers `Nats-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:eob` and the server will commit the batch without storing the message and reply with a pub ack.
 * Clients will monitor the acks and should an ack have a `Nats-Flow` header that differs from the active one they will adjust accordingly

The `Nats-Flow` message has a few formats see the later section.

The server will acknowledge in the following manner:

 * The initial message will get an error - for example, feature not supported - or zero byte ack
 * The server will then send zero byte acks back based on the values of `Nats-Flow`, the `Nats-Flow` header will always be included, such acks can optionally include a header `Nats-Batch-Sequence:n` which indicates which message triggered the ack
 * The final message will get a pub ack as described later

By always sending the current flow header back we guard against lost acks.

### Server Errors

The server will respond with the following errors if committing a batch fails:

| ErrCode | Code | Description                                                         |
|---------|------|---------------------------------------------------------------------|
| 10174   | 400  | Batch publish not enabled on stream                                 |
| 10176   | 400  | Batch publish is incomplete and was abandoned                       |
| 10179   | 400  | Batch publish ID is invalid (exceeds 64 characters)                 |
| 10175   | 400  | Batch publish sequence is missing                                   |
| 10177   | 400  | Batch publish unsupported header used (`Nats-Expected-Last-Msg-Id`) |
| 10201   | 400  | Batch publish contains duplicate message id (`Nats-Msg-Id`)         |

### Server Behavior Design

* The server will limit the `Nats-Fast-Batch-Id` to 64 characters and response with a error Pub Ack if its too long
* Server will reject messages for which the batch is unknown with a error Pub Ack
* If messages in a batch is received and any gap is detected an ack will be sent back indicating the gap
* Check properties like `ExpectedLastSeq` using the sequences found in the stream prior to the batch, at the time when the batch is committed under lock for consistency. Rejects the batch with an error Pub Ack if any message fails these checks. Only the first message of the batch may contain `Nats-Expected-Last-Sequence`. Checks using `Nats-Expected-Last-Subject-Sequence` can only be performed if prior entries in the batch do not also write to that same subject.
* Abandon without error reply anywhere a batch that has not had messages for 10 seconds, an advisory will be raised on abandonment in this case
* Send a pub ack on the final message that includes a new property `Batch:ID` and `Count:10`. The sequence in the ack would be the final message sequence, previous messages in the batch would be the preceding sequences

The server will operate under limits to safeguard itself:

* Each stream can only have 50 batches in flight at any time
* Each server can only have 1000 batches in flight at any time
* A batch that has not had traffic for 10 seconds will be abandoned

### Stream State Constraints

The `LastMsgId` header is currently not supported. A batch will be rejected if this header is used, but we might support this header in the future.

## Publish Acknowledgements

When the server sends a Pub Ack at the end of a batch the `PubAck` will set these 2 new fields

```go
type PubAck struct {
	// ...
	BatchId   string `json:"batch,omitempty"`
	BatchSize int    `json:"count,omitempty"`
}
```

## Abandonment Advisories

When a batch is abandoned it might be for reasons that will never be communicated back to clients, so we raise advisories:

```go
type BatchAbandonReason string

var (
	BatchTimeout              BatchAbandonReason = "timeout"
	BatchIncomplete           BatchAbandonReason = "incomplete"
	BatchRequirementsNotMetqq   BatchAbandonReason = "unsupported"
)

type Advisory struct {
	// ...
	BatchId      string             `json:"batch"`
	Reason       BatchAbandonReason `json:"reason"`
}
```

The event type is `io.nats.jetstream.advisory.v1.batch_abandoned`.

## Stream Configuration

Streams get a new property that enables this behavior

```go
type StreamConfig struct {
	// ...
	AllowAtomicPublish bool `json:"allow_atomic,omitempty"`
	AllowBatchedPublish bool `json:"allow_batched,omitempty"`
}
```

The setting can be disabled and enabled using configuration updates.

Setting `AllowAtomicPublish` to true should set the API level to 2, setting `AllowBatchedPublish` to true should set the API level to 3.

## Mirrors and Sources

Mirrors can't enable these settings, and will ignore the various headers like `ExpectedLastSeq` and the batching headers.

Streams with Sources can enable these settings, but sources will ignore the batching headers when sourced into the stream similar to how mirrors work.
