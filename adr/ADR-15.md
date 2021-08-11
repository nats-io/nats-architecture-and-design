# JetStream Subscribe Workflow

|Metadata|Value|
|--------|-----|
|Date    |2021-08-11|
|Author  |@kozlovic|
|Status  |Partially Implemented|
|Tags    |jetstream, client|

## Context

This document attempts to describe the workflow of a JetStream subscription in the library, from the creation, the behavior at runtime, to the deletion of a subscription.

## Design

### Creation

The library should have API(s) that allow(s) creation of a JetStream subscription. Depending on the language, there may be various type of subscriptions (like Channel based in Go), or with various options such as Queue subscriptions, etc... There can be different APIs or one with a set of options.

#### Configuration

When creating a subscription, the user can provide several things:

- A mandatory subject (see meaning of this below)
- An optional stream name
- An optional consumer name, really a durable name for when the JS consumer already exists
- An optional JS consumer configuration (see below), if the library is expected to create the JS consumer
- A Queue name if this subscription is meant to be a queue subscription
- Indication if this is a "Pull" subscription or not

At the time of this writing, the consumer configuration object looks like this:

```go
type ConsumerConfig struct {
	Description     string        `json:"description,omitempty"`
	Durable         string        `json:"durable_name,omitempty"`
	DeliverSubject  string        `json:"deliver_subject,omitempty"`
	DeliverPolicy   DeliverPolicy `json:"deliver_policy"`
	OptStartSeq     uint64        `json:"opt_start_seq,omitempty"`
	OptStartTime    *time.Time    `json:"opt_start_time,omitempty"`
	AckPolicy       AckPolicy     `json:"ack_policy"`
	AckWait         time.Duration `json:"ack_wait,omitempty"`
	MaxDeliver      int           `json:"max_deliver,omitempty"`
	FilterSubject   string        `json:"filter_subject,omitempty"`
	ReplayPolicy    ReplayPolicy  `json:"replay_policy"`
	RateLimit       uint64        `json:"rate_limit_bps,omitempty"` // Bits per sec
	SampleFrequency string        `json:"sample_freq,omitempty"`
	MaxWaiting      int           `json:"max_waiting,omitempty"`
	MaxAckPending   int           `json:"max_ack_pending,omitempty"`
	FlowControl     bool          `json:"flow_control,omitempty"`
	Heartbeat       time.Duration `json:"idle_heartbeat,omitempty"`
}
```

#### Stream name

When no stream name is specified, the library will use the subject provided as a way to find out which stream this subscription is for. A request is sent to the server on the `<JS prefix>.STREAM.NAMES` subject with a JSON content `{"subject":"<subject here>"}`. If the response (a list of stream names) is positive and contains a single entry, then the library will use this stream name, otherwise an error indicating that there is no matching stream name should be returned.

#### Consumer name

A consumer name can be specified directly or as the `Durable` field of the `ConsumerConfig`. A value supplied directly takes precedence over the value supplied in the configuration if both are supplied.

There is a separate API call `Bind` which requires a stream name and a durable consumer name. When binding, the subscription will be created with the assumption that both the stream and the consumer already exist.

So if the consumer is not provided but set to the `ConsumerConfig.Durable` field, this means that if the durable exists, it will be attached to it, but if it doesn't, then the library will try to create this durable.

After this step, the library has a stream name and a consumer name, the latter possibly empty. If a consumer name is provided, the library will lookup the consumer info based on the pair (stream, consumer). If a consumer info is returned, some checks should be made so that the user provided `ConsumerConfig` matches what is returned by the server. Namely:

- The `FilterSubject` matches the subject passed to the "Subscribe" API, if not return an error indicating the subject mismatch
- If the user was trying to create a Pull subscription, but `DeliverSubject` is not empty, then return an error indicating that user can't create a pull subscription for a push based JS consumer
- The opposite: if the user was not creating a Pull subscription but the `DeliverSubject` is empty, then return an error indicating that a pull susbcription is required

If no error is found, then the internal NATS subscription should be created on the returned `DeliverSubject`.

If there was a consumer lookup but an error occurs, the behavior depends on few things:

- If the error is a "not found" (that is, the library got a response indicating that the said consumer does not exists):
    - If the subscription is "bound" (stream and consumer names were provided), the error is returned.
    - If the subscription is not "bound", then it is ok and the library will attempt to create the JS consumer.
- For a "lookup error", for instance because the consumer info API is not allowed, or the consumer is not in the current account, etc.. then unless this is a Pull subscription, the library should return an error.

#### NATS subscription

At this point the NATS subscription will be created on the deliver subject found from the consumer info or on an inbox.

Note that the subscription should be created prior to attempt to create the JS consumer (when applicable) because for durable subscriptions, this is the only way for the server to detect if the durable is already in use or not. If there is no subscription (no interest), then the server will simply update the delivery subject of the JS durable consumer.

#### Creating the JS Consumer

If the library determined that it should attempt to create a JS consumer (no consumer name provided, or durable name that does not exists), the library will fill a consumer config with what was provided by the user, but add the `DeliverSubject` to the inbox (but leave it blank for pull subscription), set the `FilterSubject` to the user provided subject, ensure that `AckPolicy` is set and possibly some other fields such as `MaxAckPending`.

The request is then sent to `<JS prefix>.CONSUMER.DURABLE.CREATE.<stream name>.<durable name>` for a durable subscription or `<JS prefix>.CONSUMER.CREATE.<stream name>` for an ephemeral.

If the result of this request is "OK", it means that the server has created the JS consumer and uses the deliver subject the NATS subscription is created on. For ephemeral, the consumer name will be saved from the creation request's response, so that the ephemeral JS consumer can be deleted on Drain and Unsubscribe.

If the result indicates that the consumer already exists, then it means that there is an active durable consumer, with some NATS subscription attached and so we should fail this, *unless* a Queue name was provided. The client/server currently does not use the concept of a queue group in the JS consumer, but if the server is enhanced, as part of getting the consumer information, the library will be able to determine if the consumer that already exists is part of the same group, in which case it is fine, or not, in which case an error should be returned.

When the consumer already exists, the NATS queue subscription that was created prior to the "AddConsumer" call needs to be destroyed and replaced with the new NATS queue subscription on the consumer info's `DeliverSubject`.

### Runtime

#### Heartbeats

When heartbeats have been configured, the server will send heartbeats to the client. A timer should be set to report through the asynchronous error callback if heartbeats are detected as missed.

In hearbeats mode, the library should keep track of all messages which reply subject start with `$JS.ACK` since they contain information about the stream and consumer sequence, etc..

When the library receives a message that is found to be a control message (no payload and containst the "Status" header), if the header is "100 Idle Heartbeat", then the library should check if there is any gap detected between what was the last tracked message meta information and the content of the heartbeat. If a gap is detected, an asynchronous error should be reported indicating what the gap is.

Ordered consumers (should have its own ADR) will handle all that for the application, recreating the subscription as needed at the right stream sequence.

#### Flow control

When flow control is enabled, the library receives a control message that indicates that this is a flow control and should respond to the provided subject when the current pending message has been processed.

It is possible, however, that either the flow control or its response is missed and that the consumer is considered stalled from a server perspective.

The server includes in the heartbeats a header to indicate if the consumer is stalled and a subject to send an empty message to. When that happens, the library need to respond to this message, regardless of its internal state of flow control.

Without this, it can be that a consumer remains stalled, so having both heartbeats and flow control configured seem like necessary, so we may make that a misconfiguration if flow control is configured without heartbeat.

Again, Ordered consumers should be favored.

### Unsubscribe and Drain

When a subscription is unsubscribed or drained, if the library created the JS consumer, (or it already existed and this is a queue member added to the group), at the end of those calls the JS consumer should be deleted.

#### Unsubscribe

Once the normal unsubscribe processing is done, and for a JS subscription that has been marked as needing to delete the JS consumer, the library should send a "DeleteConsumer" request and return the error if any.

#### Drain

The deletion of the JS consumer need to be delayed passed the point when the subscription is fully drained and removed from the connection. Since this is an asynchronous process, if the deletion of the JS consumer fail, an error will be pushed to the asynchronous error callback.

## Consequences

It is possible that some existing libraries will need changes that would break SimVer. It will be left to the library maintainer to evaluate this.
