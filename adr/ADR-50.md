# JetStream Batch Publishing

| Metadata | Value                                 |
|----------|---------------------------------------|
| Date     | 2025-06-10                            |
| Author   | @ripienaar                            |
| Status   | Approved                              |
| Tags     | jetstream, server, client, 2.12, 2.14 |

| Revision | Date       | Author                                          | Info                                                    | Server Version | API Level |
|----------|------------|-------------------------------------------------|---------------------------------------------------------|----------------|-----------|
| 1        | 2025-06-10 | @ripienaar                                      | Initial design                                          | 2.12.0         | 2         |
| 2        | 2025-09-08 | @MauriceVanVeen                                 | Initial release                                         | 2.12.0         | 2         |
| 3        | 2025-09-11 | @piotrpio                                       | Add server codes                                        | 2.12.0         | 2         |
| 4        | 2025-09-11 | @ripienaar                                      | Restore optional ack behavior                           | 2.12.0         | 2         |
| 5        | 2025-09-25 | @ripienaar                                      | Support batch commit without storing the commit message | 2.14.0         | 3         |
| 6        | 2025-10-02 | @MauriceVanVeen                                 | Support deduplication                                   | 2.12.1         | 2         |
| 7        | 2025-10-08 | @ripienaar, @MauriceVanVeen, @piotrpio, @Jarema | Support fast ingest                                     | 2.14.0         | 3         |

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
 * Otherwise, the final message will have headers `Nats-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:eob` and the server will commit the batch without storing the message and reply with a pub ack. The last message will get the be updated to have the `Nats-Batch-Commit:1` header set by the server before the batch is saved.

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

TODO/Questions:

* What should clients limit max outstanding acks to, we want to avoid big bytes or many acks
* Should we only support `eob` style commits?
* Clients might stall if they lost all the acks involved in their max pending, we might then have to just timeout or perhaps add a way to probe the server to send a `BatchFlowAck` as a liveness check. We will though experiment first before doing this.

### Context

Today we have Async publishing in the clients that aims to move data into a stream at high speed, it works well but pays a very high cost for managing acknowledgements both in client and server.

We would like to build on the batching behaviours introduced here but deliver high performance ingest without atomicity.

These batches are not reliable, meaning messages can be lost and the batch will not be abandoned like atomic ones.

Clients will have the flexibility to choose how missing messages are handled - gaps are allowed or any gap terminates the batch.

Crucially these batches are not pre-staged and so will not be limited to 1000 messages like Atomic ones, they will be unlimited and, if gaps are allowed, can survive leader changes.

The goal is to replace Async publish with one built on these behaviors.

### Control Channel

The heart of this design is control channel that is open and kept open for the duration of the batch. In practise the reply subject of the first message will be used for all signaling during the batch.

The server will send acks over the channel on a frequency like once every 10 messages or once every 5MB. Crucially if at any stage an error is encountered errors can be sent back immediately and received by the client as the channel is always open.

Clients will not wait for each ack like they would in a standard JS Publish instead they will maintain a count of maximum outstanding acknowledgements from the server, this is part flow-control and part outage detection.

The server on the other hand will be able to adjust the frequency of acks based on internal metrics of the stream such as the size of the in-flight RAFT proposals. In this way the server and clients will balance speed of fast ingest vs responsiveness of interactive commands requiring RAFT proposals.

At all times the server will maintain its self-protection mechanisms like dropping messages when buffers are full etc.

### Client Design

The client will signal batch start, membership and flow control characteristics using headers on published messages.

 * The client will set up a Inbox subscription that will be used for the duration of the batch
 * A batch will be started by adding the `Nats-Fast-Batch-Id:uuid`, `Nats-Flow:10`, `Nats-Batch-Gap:ok` (optional) and `Nats-Batch-Sequence:1` headers using a message with reply being the above Inbox, the server will reply with error or `BatchFlowAck` message. Maximum length of the ID is 64 characters. Client should ideally wait for the first reply to detect if the feature is available on the server or Stream.
 * Following messages in the same batch will include the `Nats-Fast-Batch-Id:uuid` header and increment `Nats-Batch-Sequence:n` by one, and must set the same reply-to as the command channel
 * If the final message has the headers `Nats-Fast-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:1`, the server will store the message, commit the batch and reply with a pub ack
 * Otherwise, the final message will have headers `Nats-Fast-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:eob` and the server will commit the batch without storing the message and reply with a pub ack. In fast mode the previous message will not get the `Nats-Batch-Commit` header added after a `eob`
 * Clients will monitor the `BatchFlowAck` acks and should an ack have different flow settings different from the active one they will adjust accordingly
 * To deal with lost acks clients will manage outstanding `BatchFlowAck` acks in a way that ensure if a ack for message 30 comes in that it implies all earlier acks were received

The `Nats-Flow` header has a few formats see the later section.
The `Nats-Batch-Gap` header has a few formats see later section.

The server will acknowledge in the following manner:

 * The initial message will get an error - for example, feature not supported - or `BatchFlowAck` ack
 * The server will then send `BatchFlowAck` back based on the flow rate - which might adjust the flow rate
 * The final message will get a standard pub ack as described later

By always sending the current flow state back in the `BatchFlowAck` we guard against lost acks.

When the leader of the Stream changes:

* In `ok` gap mode the new leader will continue and send a `BatchFlowAck` out indicating the gap
* In `fail` gap mode the new leader will abandon the batch and send back a final pub ack with details up to the last received message for the batch

### Message Gaps

We want to cater for 2 kinds of use cases around gaps:

 1. Object store would not be ok with any gaps in the published messages because those would be gaps in files
 2. Fast metric publishers would be ok with some gaps and would just want to continue publishing

To support both we add the `Nats-Batch-Gap` header with values `ok` or `fail`. The default for this header is `fail` when it is not set for a batch. Invalid values must result in a batch abandon error.

When `ok` the server will, upon detecting a gap, immediately send a `BatchFlowAck` with the `LastSequence` and `CurrentSequence` values set allowing clients to detect the gaps.

When `fail` the server will abandon the batch and send the final ack back with `BatchSize` set to the last received sequence before the gap.

### Flow Control

The client and server will cooperate around flow control, there are a number of buffers of concern:

 1. Socket buffers over client, server, gateways, routes, same as always
 2. Stream pending RAFT proposals size and other related JetStream internal buffers

The outstanding ack behaviour will address the first buffers and lead to client settling on a sustainable publish rate.

The 2nd is a concern because when there are too many outstanding RAFT proposals API requests like Msg Delete, Purge, Config change etc can take a long time to be serviced leading to client errors. Realistically we can't have those take more than 2 seconds.

The server will thus have to monitor those internal Stream and Raft related buffers and communicate back to all fast-publishers that they need to adjust their flow rate.

The primary mechanism for this is the `Nats-Flow` header, it can have a value like `10` meaning every 10th message gets an ack or `1024B` meaning every 1024 bytes gets an ack.

Clients will surface settings like how many outstanding acks there can be before the client stops publishing and waits for acks and how long the timeout is while waiting. The client though must take care to track not just the count of outstanding acks but also the sequence they are for.  If acks for messages 10,20,30,40 and the one for 30 is lost - when the one for 40 comes the client must also treat the one for message 30 as seen, this is critical to avoid unrecoverable stalls.

The server can adjust the active flow parameters once the batch is established by sending a new flow rates back to the client in `BatchFlowAck` messages. In effect this will mean that the frequency of acks will change, the client will then have to adjust its expectations accordingly to calculate the outstanding acks against the new expectation for new publishes.

The server must treat the initial flow parameters as the upper bound though, when a client says ack every 10 messages we cannot decide from the server side to change that to > 10. 

Aside from this, all current self-protection mechanisms in the server - dropping too fast messages etc - will remain in place.

```go
type BatchFlowAck struct {
	// LastSequence is the previously highest sequence seen, this is set when a gap is detected
	LastSequence int `last_seq,omitempty`
	// CurrentSequence is the sequence of the message that triggered the ack
	CurrentSequence int `seq,omitempty`
	// AckMessages indicates the active per-message frequency of Flow Acks
	AckMessages int `messages,omitempty`
	// AckBytes indicates the active per-bytes frequency of Flow Acks in unit of bytes
	AckBytes int64 `bytes,omitempty`
}
```

This kind of ack is differentiated from Pub Acks by the absence of the `batch` field that the standard publish acks are required to set.

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
| 10202   | 400  | Invalid batch gap mode                                              |

### Server Behavior Design

* The server will limit the `Nats-Fast-Batch-Id` to 64 characters and response with a error Pub Ack if its too long
* Server will reject messages for which the batch is unknown with a error Pub Ack
* Server will reject values for `Nats-Batch-Gap` that is not `ok` or `fail`, absent means `fail`
* If messages in a batch is received and any gap is detected an ack will be sent back indicating the gap and optionally abandon the batch based on the gap configuration
* Check properties like `ExpectedLastSeq` using the sequences found in the stream prior to the batch, at the time when the batch is committed under lock for consistency. Rejects the batch with an error Pub Ack if any message fails these checks. Only the first message of the batch may contain `Nats-Expected-Last-Sequence`. Checks using `Nats-Expected-Last-Subject-Sequence` can only be performed if prior entries in the batch do not also write to that same subject.
* Abandon, without error reply, anywhere a batch that has not had messages for 10 seconds, an advisory will be raised on abandonment in this case
* Send a pub ack on the final message that includes a new property `Batch:ID` and `Count:10`. The sequence in the ack would be the final message sequence, previous messages in the batch would be the preceding sequences

The server will operate under limits to safeguard itself:

* Each stream can only have 50 batches in flight at any time
* Each server can only have 1000 batches in flight at any time
* A batch that has not had traffic for 10 seconds will be abandoned
* There will be no maximum size for fast ingest batches
* Streams with `PersistMode: async` set are compatible with fast ingest

### Stream State Constraints

The `LastMsgId` header is currently not supported. A batch will be rejected if this header is used, but we might support this header in the future.

## Publish Acknowledgements

When the server sends a Pub Ack at the end of a batch the `PubAck` will set these 2 new fields

```go
type PubAck struct {
	// ...
	BatchId    string `json:"batch,omitempty"`
	BatchSize  int    `json:"count,omitempty"`
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

Streams get a new properties that enables this behavior

```go
type StreamConfig struct {
	// ...
	AllowAtomicPublish bool `json:"allow_atomic,omitempty"`
	AllowBatchPublish bool `json:"allow_batched,omitempty"`
}
```

These settings can be disabled and enabled using configuration updates. Both can be active at the same time, or just one at a time.

Setting `AllowAtomicPublish` and `PersistMode: async` must error, but this is allowed for `AllowBatchPublish`

Setting `AllowAtomicPublish` to true should set the API level to 2, setting `AllowBatchPublish` to true should set the API level to 3.

## Mirrors and Sources

Mirrors can't enable these settings, and will ignore the various headers like `ExpectedLastSeq` and the batching headers.

Streams with Sources can enable these settings, but sources will ignore the batching headers when sourced into the stream similar to how mirrors work.
