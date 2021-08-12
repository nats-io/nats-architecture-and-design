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
- An optional durable consumer name.
- An optional `ConsumerConfig` (see below)
- Indication if this is a "Push" or "Pull" subscription
- Indication if this is a "Bind"
- A Queue name if this subscription is meant to be a queue subscription
- Handlers for receiving messages asynchronously, whatever that means in the library language. 

At the time of this writing, the consumer configuration (`ConsumerConfig`) object looks like this:

```go
type ConsumerConfig struct {
	Description     string        `json:"description,omitempty"`      
	Durable         string        `json:"durable_name,omitempty"`     
	DeliverSubject  string        `json:"deliver_subject,omitempty"`  
	DeliverPolicy   DeliverPolicy `json:"deliver_policy"`              
	OptStartSeq     uint64        `json:"opt_start_seq,omitempty"`    // server returns error if provided when Deliver Policy is not by_start_sequence
	OptStartTime    *time.Time    `json:"opt_start_time,omitempty"`   // server returns error if provided when Deliver Policy is not by_start_time 
	AckPolicy       AckPolicy     `json:"ack_policy"`                 
	AckWait         time.Duration `json:"ack_wait,omitempty"`         
	MaxDeliver      int           `json:"max_deliver,omitempty"`      
	FilterSubject   string        `json:"filter_subject,omitempty"`    
	ReplayPolicy    ReplayPolicy  `json:"replay_policy"`
	RateLimit       uint64        `json:"rate_limit_bps,omitempty"`   // Bits per sec
	SampleFrequency string        `json:"sample_freq,omitempty"`
	MaxWaiting      int           `json:"max_waiting,omitempty"`      // server returns error if provided if a DeliverSubject is provided
	MaxAckPending   int           `json:"max_ack_pending,omitempty"`
	FlowControl     bool          `json:"flow_control,omitempty"`
	Heartbeat       time.Duration `json:"idle_heartbeat,omitempty"`
}
```

If the user does not provide a `ConsumerConfig`, all defaults are assumed if a consumer is created for them.

#### Stream name

A stream name is required for the `CONSUMER.CREATE.<stream name>` and `CONSUMER.DURABLE.CREATE.<stream name>.<durable name>` requests.

Client must have a way for the user to provide a stream name.
When no stream name is specified, the library will use the subject provided as a way to find out which stream this subscription is for. 
A request is sent to the server on the `<JS prefix>.STREAM.NAMES` subject with a JSON content `{"subject":"<subject here>"}`. 
If the response (a list of stream names) is positive and contains a single entry, then the library will use this stream name, otherwise an error indicating that there is no matching stream name should be returned.

In documentation for the user, suggest that they should supply the stream name if it is known, as this saves this request to the server.

#### Consumer name

Consumers can be _durable_ or _ephemeral_. This is determined by whether a durable name is provided. If the consumer durable name is _not_ provided, this signals that the consumer is ephemeral which further indicates that it should be created for the user.

Clients should provide some helper mechanism for the user to provide a durable name in addition to accepting a `ConsumerConfig`. 
The rationale behind this is that it will be common to make a durable consumer with all the `ConsumerConfig` defaults, so this allows the user to not have to build their own `ConsumerConfig`

If a durable value is provided with both the helper and in a `ConsumerConfig`, the helper value takes precedence.

#### Consumer Lookup

When a durable value is provided and the user has not requested a "Bind" (See below), the client must determine whether this durable exists or not. 
This is done via the `CONSUMER.INFO.<stream name>.<consumer name>` request. The request returns a valid `consumer_info_response` or a `JSConsumerNotFoundErr: {Code: 404, ErrCode: 10014, Description: "consumer not found"}`

Important: On server versions before v2.3.2 `ErrCode` is not returned, so consider that when processing the response. The Java client basically does this:
```java
if (ErrCode == 10014 || (Code == 404 && Description.contains("consumer"))) {
    // this signals that the consumer does not exist
}
```

#### FilterSubject

When a durable consumer is found during lookup, a check must be performed with regard to the `FilterSubject`. If the user had provided value in the `ConsumerConfig`, 
that value must be an exact match to the value returned by the consumer lookup, otherwise the client should throw/return an error indicating the subject mismatch

#### Pull and Deliver Subject

A `DeliverSubject` is not valid for a "Pull" subscription. If the `ConsumerConfig` provides a `DeliverSubject`, and logic requires the client to create a consumer, just handle this silently and do not send it with a consumer create request.

#### Deliver Subject and Max Waiting

`MaxWaiting` does not apply to "Push" request. If `MaxWaiting` is supplied when a `DeliverSubject` is supplied the server will return an error. Handle this silently by not sending it with a consumer create request.   

#### Bind

The client must provide a way to "Bind" which requires a stream name and a durable consumer name. If the user does not provide both, this is an error.
The client will lookup the consumer and throw/return an error if the consumer is not found / does not already exist. 

#### Subscription / Inbox

If there was an existing consumer found, the inbox is the `DeliverSubject` found in the lookup.
If a consumer is being created and for "Push" subscription, use the user provided `DeliverSubject` if provided, otherwise create an inbox.  
If a consumer is being created and for a "Pull" subscription, always create an inbox. 

The subscription should be created prior to attempt to create the JS consumer (when applicable) because for durable subscriptions, this is the only way for the server to detect if the durable is already in use or not. 
If there is no subscription (no interest), then the server will simply update the delivery subject of the JS durable consumer.

The subscription can include a queue name.

#### Creating the JS Consumer

If the library determined that it should attempt to create a JS consumer (not bind, no consumer name provided, or durable consumer that does not exist), the library will fill a `ConsumerConfig` with what was provided by the user and what was calculated by the above steps. 

- Add the `DeliverSubject` to the inbox for "Push" subscription or leave it blank for "Pull" subscriptions.
- Set the `FilterSubject` to the user provided one if they provided it, otherwise use the provided stream subject.  
- optionally ensure that `AckPolicy` is set and possibly some other fields such as `MaxAckPending`.

The request is then sent to `<JS prefix>.CONSUMER.DURABLE.CREATE.<stream name>.<durable name>` for a durable subscription or `<JS prefix>.CONSUMER.CREATE.<stream name>` for an ephemeral.

The server returns a `consumer_info_response` if the consumer was created or an error if it was not. If there is an error while createing the consumer, the client must unsubscribe the subscription it just made.

For ephemeral, track the response, so that the ephemeral JS consumer can be deleted on Drain and Unsubscribe. (See runtime behavior)

The client should never attempt to create a durable consumer that it has determined already exists.

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
