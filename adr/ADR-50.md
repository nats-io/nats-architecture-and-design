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
| 8        | 2025-10-09 | @MauriceVanVeen                                 | Update fast ingest details                              | 2.14.0         | 3         |

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

 * A batch will be started by adding the `Nats-Batch-Id:uuid` and `Nats-Batch-Sequence:1` headers using a *request*, the server will reply with an error or zero byte message. Maximum length of the ID is 64 characters.
 * Following messages in the same batch will include the `Nats-Batch-Id:uuid` header and increment `Nats-Batch-Sequence:n` by one, and might optionally include a reply subject that will receive a zero byte reply
 * If the final message has the headers `Nats-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:1`, the server will store the message, commit the batch and reply with a pub ack. 
 * Otherwise, the final message will have headers `Nats-Batch-Id:uuid`, `Nats-Batch-Sequence:n` and `Nats-Batch-Commit:eob` and the server will commit the batch without storing the message and reply with a pub ack. The last message will be updated to have the `Nats-Batch-Commit:1` header set by the server before the batch is saved.

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

 * The server will limit the `Nats-Batch-ID` to 64 characters and respond with an error Pub Ack if it's too long
 * Server will reject messages for which the batch is unknown with an error Pub Ack
 * If messages in a batch is received and any gap is detected the batch will be rejected with a error Pub Ack
 * Check properties like `ExpectedLastSeq` using the sequences found in the stream prior to the batch, at the time when the batch is committed under lock for consistency. Rejects the batch with an error Pub Ack if any message fails these checks. Only the first message of the batch may contain `Nats-Expected-Last-Sequence`. Checks using `Nats-Expected-Last-Subject-Sequence` can only be performed if prior entries in the batch do not also write to that same subject.
 * Abandon without error reply anywhere a batch that has not had messages for 10 seconds, an advisory will be raised on abandonment in this case
 * Send a pub ack on the final message that includes a new property `Batch:ID` and `Count:10`. The sequence in the ack would be the final message sequence, previous messages in the batch would be the preceding sequences
 * If a stream is operating on the `PersistMode: async` mode, any batch published to it must fail

The server will operate under limits to safeguard itself:

 * Each stream can only have 50 batches in flight at any time
 * Each server can only have 1000 batches in flight at any time
 * A batch that has not had traffic for 10 seconds since the last message will be abandoned
 * Each batch can have maximum 1000 messages

### Stream State Constraints

The `LastMsgId` header is currently not supported. A batch will be rejected if this header is used, but we might support this header in the future.

Initial release of this feature rejects the use of `MsgId`. Starting from 2.12.1 de-duplication is supported and a batch will be rejected with an error if it contains a duplicate message.

## Fast-ingest Batch Publishing

### Context

Today we have Async publishing in the clients that aims to move data into a stream at high speed, it works well but pays a very high cost for managing acknowledgements both in client and server.

We would like to build on the batching behaviours introduced here but deliver high performance ingest without atomicity.

These batches are not reliable, meaning messages can be lost and the batch will not be abandoned like atomic ones. For this reason we will also use a lighter method than headers for communicating the control state, since users are unlikely to carefully audit these like they would atomic batch publishes.

Clients will have the flexibility to choose how missing messages are handled - gaps are allowed or any gap terminates the batch. The latter also guarantees messages are persisted in-order without gaps (although they can be interleaved with messages from other publishers).

Crucially these batches are not pre-staged and so will not be limited to 1000 messages like Atomic ones, they will be unlimited and, if gaps are allowed, can survive leader changes.

While we wish to go fast when possible, we also want to not fail when many concurrent producers are doing fast-ingest as we see today with ObjectStore writes - a few concurrently will fail due to pending API counts etc. We will therefor create a flow-control mechanism where the server can tell the clients to go slower. This way the focus is not just fast but being able to actively manage the concurrent pressure ensuring everyone goes as fast as possible while remaining reliable.

The goal is to replace Async publish with one built on these behaviors.

### Control Channel

The heart of this design is a control channel that is open and kept open for the duration of the batch. In practice the reply subject uses a wildcard inbox, with all messages in the batch containing a unique reply subject for that message, but all under that wildcard inbox subject hierarchy.

The server will send acks over the channel on a frequency like once every 10. Crucially if at any stage an error is encountered errors can be sent back immediately and received by the client as the channel is always open.

Clients will not wait for each ack like they would in a standard JS Publish instead they will maintain a count of maximum outstanding acknowledgements from the server, this is part flow-control and part outage detection.

The server on the other hand will be able to adjust the frequency of acks based on internal metrics of the stream such as the amount of other fast publishers, the average message size they use, the size of the in-flight RAFT proposals, etc. In this way the server and clients will balance speed of fast ingest vs responsiveness of interactive commands requiring RAFT proposals.

At all times the server will maintain its self-protection mechanisms like dropping messages when buffers are full etc.

Clients should use old style inboxes not the mux inbox so that as soon as the server sends an error back the clients would drop interest. The reason is that with many in-flight messages once an error occurs the server might find it has to send thousands of Acks back to the client, if the client unsubscribes from the control channel completely the server will short circuit some of those acks.

### Client Design

The client will communicate key information about the batch using a reply subject, `<prefix>.<uuid>.<initial flow>.<gap 'ok' or 'fail'>.<batch seq>.<operation>.$FI`

| Operation | Description                                                             |
|-----------|-------------------------------------------------------------------------|
| 0         | Starts a batch                                                          |
| 1         | Append to a batch                                                       |
| 2         | Commit and store the final message                                      |
| 3         | Commit without storing the final message (EOB mode)                     |
| 4         | Ping a batch to keep it alive and receive the last flow control message |

The server MUST reject any operation that it does not know about

 * The client will set up an Inbox subscription that will be used for the duration of the batch, this must be an old style inbox. The inbox must subscribe to `<prefix>.uuid.>`
 * A batch will be started by setting a reply subject of `<prefix>.uuid.10.ok.1.0.$FI` (initial flow of `10`, gap `ok`, sequence `1`), the server will reply with an error or `BatchFlowAck` message. Maximum length of the ID is 64 characters. The client MUST wait for the first reply to detect if the feature is available on the server or Stream, and what flow control settings the server allows the client to start at. It is very important for a client to not immediately blast messages out at a speed that the server couldn't support if it already had many other fast publishers, since this needs to be coordinated by the server.
 * Following messages in the same batch must use a reply subject `<prefix>.uuid.10.ok.n.1.$FI` where `n` is the sequence incremented by one. We add the initial flow and gap information for replica followers who might have missed the first message due to limits
 * If the final message has the reply subject `<prefix>.uuid.10.ok.n.2.$FI`, the server will store the message, end the batch and reply with a pub ack. This last message will not get the `Nats-Batch-Commit` header like with atomic batch publishing.
 * Otherwise, the final message will have reply subject `<prefix>.uuid.10.ok.n.3.$FI` the server will end the batch without storing the message and reply with a pub ack.
 * Clients will monitor the `BatchFlowAck` acks and should an ack have different flow settings different from the active one they will adjust accordingly.
 * To deal with lost acks clients will manage outstanding `BatchFlowAck` acks in a way that ensures if an ack for message 30 comes in that it implies all earlier acks were received.
 * The client may send a ping message to keep the batch alive and receive (missed) flow control messages. A ping reports about gaps, if any, and resends the latest flow control message. The client can use this to deal with lost acks. The sequence in the ping message must not itself increment the batch sequence; instead, it should be the highest batch sequence the client has sent. This ensures missed ping messages don't show up as gaps which could otherwise fail the batch.

The server will acknowledge in the following manner:

 * The initial message will get an error - for example, feature not supported - or `BatchFlowAck` ack with the initial allowed flow rate in `AckMessages`.
 * The server will then send `BatchFlowAck` back based on the flow rate - which might adjust the flow rate.
 * The final message will get a standard pub ack as described later.
 * The server will reject with an error any unsupported operation value.

By always sending the current flow state back in the `BatchFlowAck` we guard against lost acks.

The client specifies its initial/maximum flow rate, but the server dictates the actual flow rate. The server determines the flow rate in the following way:

 * The flow rate will start with a low flow value. For example, the client requests a maximum flow of 100 messages, but the server starts at a flow of 1.
 * The server may use a higher initial flow value if this is the first fast publisher to the stream.
 * The server may adjust the flow value when it's meant to send its next flow control message according to the current active settings.
 * The server may ramp up the flow of a client by increasing the flow value until it reaches the maximum flow rate. The server usually doubles the flow value in that case.
 * The server may slow down the flow of a client by decreasing the flow value until it reaches 1 (ack every message). The server usually halves the flow value in that case.
 * The server uses internal metrics to determine the flow rate for all fast publishers. For example, the total number of fast publishers, their average message sizes, the inflight messages pending to be persisted, etc.

### Message Gaps

We want to cater for 2 kinds of use cases around gaps:

 1. Object store would not be ok with any gaps in the published messages because those would be gaps in files.
 2. Fast metric publishers would be ok with some gaps and would just want to continue publishing.

To support both we set the gap mode to `fail` or `ok` in the reply subject. Invalid values must result in a batch abandon error.

Upon detecting a gap, the server immediately sends a `BatchFlowAck` with the `LastSequence` and `CurrentSequence` values set allowing clients to detect the gaps.

The `LastSequence` was the last received sequence by the server before the gap, and the `CurrentSequence` is the sequence of the received batch message. The messages with sequences between these two values were lost. Importantly, this flow control message MUST NOT be used to know whether `LastSequence` or `CurrentSequence` was persisted, it's purely informational. Also, this message will be immediately sent upon detecting a gap. This means it can be received out-of-order with the usual flow control messages that signal up to a certain batch sequence was persisted. Crucially, since these gap messages can be sent out-of-order, these messages don't contain any flow updates or information.

When `fail` the server will abandon the batch and send the final ack back with `BatchSize` set to the last received sequence before the gap. The client will receive the gap message first, and should use this to stop sending messages before eventually receiving the final ack.

When `ok` the server will allow the gap, only send the gap message, and continue onward from the received sequence.

When the leader of the Stream changes:

* In `fail` gap mode the new leader will abandon the batch (if a gap resulted from the leader change) and send back a final pub ack with details up to the last received message for the batch.
* In `ok` gap mode the new leader will continue and send a `BatchFlowAck` out indicating the gap.

When using per-message expected header checks, the server will either stop or continue the batch depending on the mode:

* In `fail` gap mode the error will commit/stop the batch. The final pub ack will contain the error, and no more messages are accepted in the batch after the batch sequence that triggered the error.
* In `ok` gap mode the error will be sent to the client in the `BatchFlowAck` message with the `CurrentSequence` set to the sequence of the message that caused the error. The batch will continue to accept messages.

### Flow Control

The client and server will cooperate around flow control, there are a number of buffers of concern:

 1. Socket buffers over client, server, gateways, routes, same as always.
 2. Stream pending RAFT proposals size and other related JetStream internal buffers.

The outstanding ack behaviour will address the first buffers and lead to client settling on a sustainable publish rate.

The 2nd is a concern because when there are too many outstanding RAFT proposals API requests like Msg Delete, Purge, Config change etc can take a long time to be serviced leading to client errors. Realistically we can't have those take more than 2 seconds.

The server will thus have to monitor those internal Stream and Raft related buffers and communicate back to all fast-publishers that they need to adjust their flow rate.

The primary mechanism for this is the `flow` field in the initial reply subject, it can have a value like `10` meaning every 10th message gets an ack.

Clients will surface settings like how many outstanding acks there can be before the client stops publishing and waits for acks and how long the timeout is while waiting. The client though must take care to track not just the count of outstanding acks but also the sequence they are for. If acks for messages 10,20,30,40 and the one for 30 is lost - when the one for 40 comes the client must also treat the one for message 30 as seen, this is critical to avoid unrecoverable stalls.

Clients should only allow limited configurability of outstanding acks, since each ack represents a batch of N messages:
- Outstanding acks = 1, functions like Async publishing up to N, but flow-controled.
- Outstanding acks = 2, while the server is working on the first batch, continue sending the next batch, and then wait for the first. This setting is generally optimal, as it allows the server to keep working on the next batch while we're waiting for the ack to come in.
- Outstanding acks = 3, similar to 2, but may work better on setups with larger RTTs to allow the server to have a bit more work to compensate for this higher RTT. This should be a conscious decision though, and not a default. Outstanding acks 1 or 2 will work best for most use cases, especially ones intending to support many concurrent fast publishers.

The server can adjust the active flow parameters once the batch is established by sending a new flow rate back to the client in `BatchFlowAck` messages. In effect this will mean that the frequency of acks will change, the client will then have to adjust its expectations accordingly to calculate the outstanding acks against the new expectation for new publishes.

The server must treat the initial flow parameters as the upper bound though, when a client says ack every 10 messages we cannot decide from the server side to change that to > 10. 

Aside from this, all current self-protection mechanisms in the server - dropping too fast messages etc - will remain in place.

```go
type BatchFlowAck struct {
    // LastSequence is the previously highest sequence seen, this is set when a gap is detected with "gap: ok".
    LastSequence uint64 `json:"last_seq,omitempty"`
    // CurrentSequence is the sequence of the message that triggered the ack.
    // If "gap: fail" this means the messages up to CurrentSequence were persisted.
    // If "gap: ok" and Error is set, this means this message was NOT persisted and had an error instead.
    CurrentSequence uint64 `json:"seq,omitempty"`
    // AckMessages indicates acknowledgements will be sent every N messages.
    AckMessages uint16 `json:"ack_msgs,omitempty"`
    // Error is used for "gap:ok" to return the error for the CurrentSequence.
    Error *ApiError `json:"error,omitempty"`
}
```

This kind of ack is differentiated from Pub Acks by the absence of the `batch` field that the standard publish acks are required to set.

The following sample Go code can be used when implementing support for this feature, importantly:
- Manage the proper reply subject (and inbox subscription) used.
- Publish the batch message.
- If the batch is new (sequence 1), wait for confirmation (and initial flow settings) from the server.
- The client waits for acknowledgements depending on the maximum outstanding acks and the coordinated ack per N messages value.
- The client must update the flow values if the server tells it to, and adjust accordingly when it has to wait for acks.
- The client needs to handle delivery of both flow and publish acknowledgements over the same inbox.
- The client needs to return the batch and ack sequence so the application can be made aware of which messages were acknowledged.

```go
type FastPubAck struct {
    // BatchSequence is the sequence of this message within the current batch.
    BatchSequence uint64
    // AckSequence is the highest sequence within the current batch that has been acknowledged.
    // This can be used by application code to release resources of the messages it might want to otherwise retry.
    // If "gap: fail" is used this means all messages below and including this sequence were persisted.
    // If "gap: ok" is used there's no guarantee that all messages were persisted.
    AckSequence uint64
}

func (f *FastPublisher) AddMsg(m *nats.Msg) (FastPubAck, error) {
    // Generate fast batch reply subject, and publish message.
    f.batchSeq++
    m.Reply = "<prefix>.<uuid>.<initial flow>.<gap mode>.<batch seq>.<operation>.$FI"
    if err := f.js.Conn().PublishMsg(m); err != nil {
        return FastPubAck{}, err
    }

    // If this batch is new, we immediately get an ack back potentially updating settings.
    if f.batchSeq == 1 {
        // TODO: wait for and process ack, and store latest flow.CurrentSequence and flow.AckMessages
        // TODO: differentiate between flow and publish acknowledgements, as we could receive a publish
        //  acknowledgement early if there was an error.
    }

    // Check if we need to wait for acknowledgements.
    // Repeat until we don't need to wait for acknowledgements anymore.
    for {
        // If there are any pending messages in our subscription, process them first.
        // TODO: process pending acks, and store latest flow.CurrentSequence and flow.AckMessages
        // TODO: if the server detected a gap, it will send a flow message with
        //  flow.LastSequence and flow.CurrentSequence. It should purely be treated as informational, and
        //  MUST NOT be used to update the flow.CurrentSequence and flow.AckMessages settings.
		// TODO: differentiate between flow and publish acknowledgements, as we could receive a publish
		//  acknowledgement early if there was an error.

        // Otherwise, calculate if we should wait for acknowledgments based on the up-to-date flow values.
        waitForAck := flow.CurrentSequence+flow.AckMessages*opt.maxOutstandingAcks <= f.batchSeq
        // TODO: wait for and process ack, and store latest flow.CurrentSequence and flow.AckMessages
        //  if waited for ack, repeat until we don't need to wait anymore.

        // Break from loop if there's no more acknowledgements to receive/process.
        return FastPubAck{BatchSequence: f.batchSeq, AckSequence: flow.CurrentSequence}, nil
    }
}

func (f *FastPublisher) CommitMsg(m *nats.Msg) (*PubAck, error) {
    // Generate fast batch reply subject, and publish message to commit (either through final message or EOB).
    f.batchSeq++
    m.Reply = "<prefix>.<uuid>.<initial flow>.<gap mode>.<batch seq>.<operation>.$FI"
    if err := f.js.Conn().PublishMsg(m); err != nil {
        return nil, err
    }

    // If this batch is new and immediately commits, we can expect a publish acknowledgement immediately.
    if f.batchSeq == 1 {
        // TODO: parse publish acknowledgement.
        return pubAck, nil
    }

    // Wait for all remaining acknowledgements until we receive the final publish acknowledgement.
    for {
        // If a flow acknowledgement is received, we can just skip over it, and continue waiting for the final pub ack.
        // If a publish acknowledgement is received, return it.
        // TODO: parse and differentiate between flow and publish acknowledgements.
        return pubAck, nil
    }
}
```

### Server Errors

The server will respond with the following errors if using fast batch fails:

| ErrCode | Code | Description                                         |
|---------|------|-----------------------------------------------------|
| 10203   | 400  | Batch publish not enabled on stream                 |
| 10204   | 400  | Batch publish invalid pattern used                  |
| 10205   | 400  | Batch publish ID is invalid (exceeds 64 characters) |
| 10206   | 400  | Batch publish ID is unknown                         |

### Server Behavior Design

* The server will limit the `uuid` to 64 characters and respond with an error Pub Ack if it's too long.
* Server will reject messages for which the batch is unknown with an error Pub Ack.
* Server will reject values for `gap` that is not `ok` or `fail`.
* If messages in a batch are received and any gap is detected an ack will be sent back indicating the gap and optionally abandon the batch based on the gap configuration.
* Check properties like `ExpectedLastSeq` are handled as normal to be fully compatible with `Publish` and `PublishAsync`. Fast batch publishing changes the API through flow control, but per-message content can remain the same. This allows to swap between publish implementations as needed.
* Abandon, without error reply, anywhere a batch that has not had messages for 10 seconds, an advisory will be raised on abandonment in this case.
* Send a pub ack on the final message that includes a new property `Batch:ID` and `Count:10`. The sequence in the ack would be the final message sequence, previous messages in the batch would be for earlier sequences.

The server will operate under limits to safeguard itself:

* A batch that has not had traffic for 10 seconds since the last message will be abandoned
* There will be no maximum size for fast ingest batches
* Streams with `PersistMode: async` set are compatible with fast ingest

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
	BatchRequirementsNotMet   BatchAbandonReason = "unsupported"
)

type Advisory struct {
	// ...
	BatchId      string             `json:"batch"`
	Reason       BatchAbandonReason `json:"reason"`
}
```

The event type is `io.nats.jetstream.advisory.v1.batch_abandoned`.

## Stream Configuration

Streams get new properties that enable this behavior

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
