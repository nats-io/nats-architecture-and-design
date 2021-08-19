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

- A subject that is in some cases required (see "Stream name" section below)
- An optional stream name
- An optional consumer name
- An optional JS consumer configuration (see below)
- A Queue name if this subscription is meant to be a queue subscription
- Indication if this is a "Push" or "Pull" subscription
- Handlers for receiving messages asynchronously, whatever that means in the library language

Some misconfiguration should be checked by the subscribe API and return an error outright.

For instance:
- If a queue name is provided, configuring heartbeat or flow control is a mistake, since the server would send those message to random members.
- If no subject is provided, along with no stream name, the library won't be able to locate/create the consumer.
- For pull subscriptions ack policy of "none" or "all" is an error.

At the time of this writing, the consumer configuration object looks like this (ordred alphabetically, not as seen in js.go):

```go
type ConsumerConfig struct {
	AckPolicy       AckPolicy     `json:"ack_policy"`
	AckWait         time.Duration `json:"ack_wait,omitempty"`
	DeliverGroup    string        `json:"deliver_group,omitempty"`
	DeliverPolicy   DeliverPolicy `json:"deliver_policy"`
	DeliverSubject  string        `json:"deliver_subject,omitempty"`
	Description     string        `json:"description,omitempty"`
	Durable         string        `json:"durable_name,omitempty"`
	FilterSubject   string        `json:"filter_subject,omitempty"`
	FlowControl     bool          `json:"flow_control,omitempty"`
	Heartbeat       time.Duration `json:"idle_heartbeat,omitempty"`
	MaxAckPending   int           `json:"max_ack_pending,omitempty"`
	MaxDeliver      int           `json:"max_deliver,omitempty"`
	MaxWaiting      int           `json:"max_waiting,omitempty"`	// server returns error if provided if a DeliverSubject is provided
	OptStartSeq     uint64        `json:"opt_start_seq,omitempty"`	// server returns error if provided when Deliver Policy is not by_start_sequence
	OptStartTime    *time.Time    `json:"opt_start_time,omitempty"`	// server returns error if provided when Deliver Policy is not by_start_time
	RateLimit       uint64        `json:"rate_limit_bps,omitempty"` // Bits per sec
	ReplayPolicy    ReplayPolicy  `json:"replay_policy"`
	SampleFrequency string        `json:"sample_freq,omitempty"`
}
```

#### Stream name

When no stream is specified, the `subject` to the subscribe API calls is required. An error will be returned if it is not the case.
The library will use the subject provided as a way to find out which stream this subscription is for. A request is sent to the server on the `<JS prefix>.STREAM.NAMES` subject with a JSON content `{"subject":"<subject here>"}`. If the response (a list of stream names) is positive and contains a single entry, then the library will use this stream name, otherwise an error indicating that there is no matching stream name should be returned.

However, even with a provided stream name, the `subject` may be needed, for instance when the JS consumer will be created by the library, the `FilterSubject` is set to that, and when a consumer lookup is performed, if the incoming `FilterSubject` is not empty, we ensure that it matches the `subject`.

#### Consumer name

When a consumer name is specified, it indicates the the library that the intent is to use an existing JS Consumer. The library will lookup the consumer from the server and get a `ConsumerInfo`. If the lookup fails, it is considered an error, unless the subscription is for a "pull subscriber", in which case the library still proceeds with the JS subscription.

#### Queue name

If the user attempts to create a queue subscription, a consumer name or durable needs to be specified. The common pattern error that we noticed user doing was this:

```go
member1, _ := js.QueueSubscribe("foo", "bar")
member2, _ := js.QueueSubscribe("foo", "bar")
```
And then report that both members were receiving the same message.

This is because each subscription call would create their own ephemeral JetStream consumer. We could break the API and force the user to specify a consumer name. Instead, for the Go client we have taken the approach to describe that if no consumer name is provided (in Go with `nats.Bind(stream, consumer)`) nor a durable (in Go with `nats.Durable(durableName)`) then the library would use the queue name as the durable name.

#### Push Consumer active information and queue group binding

If the lookup succeeds, with newer server (v2.3.5+), the `ConsumerInfo` has now a field called `PushBound` which is a boolean:
```go
	PushBound	bool	`json:"push_bound,omitempty"`
```
This boolean indicates that the server has already registered an interest on the push consumer's deliver subject. The `ConsumerInfo.Config` which is a `ConsumerConfig` object, can be inspected to detect if a `DeliverGroup` is set. With both in hand, the library can now return a proper error to the user if they attempt to create an invalid subscription.

- If `PushBound` is true and there is no `DeliverGroup` and the user tries to create a non queue subscription, this should return an error such as "duplicate subscription".
- Regardless of `PushBound` value:
	- If the user tries to create a non queue subscription and `DeliverGroup` is non empty, this should return an error such as "trying to create non queue subscription for consumer created for a queue group".
	- If the user tries to create a queue subscription and `DeliverGroup` is non empty but does not match the user's queue name, this should return an error such as "trying to create a queue subscription for a consumer created for a different queue group".
	- If the user tries to create a queue subscription and `DeliverGroup` is empty, this should return an error such as "trying to create a queue subscription for a consumer create without a deliver group".

Other checks:

- The `FilterSubject` (if not empty) must match the subject passed to the "Subscribe" API, if not return an error indicating the subject mismatch
- If the user was trying to create a Pull subscription, but `DeliverSubject` is not empty, then return an error indicating that user can't create a pull subscription for a push based JS consumer
- The opposite: if the user was not creating a Pull subscription but the `DeliverSubject` is empty, then return an error indicating that a pull susbcription is required
- More generally, if the user provided configuration does not match the configuration that we get from the `ConsumerInfo.Config`, and error should be returned to indicate that the changes are not applied (only a deliver subject can be changed for an existing JS consumer).

#### NATS subscription

At this point the NATS subscription will be created on the deliver subject found from the consumer info or on an inbox.

Note that the subscription should be created prior to attempt to create the JS consumer (when applicable) because for durable subscriptions, this is the only way for the server to detect if the durable is already in use or not. If there is no subscription (no interest), then the server will simply update the delivery subject of the JS durable consumer.

#### Creating the JS Consumer

If the library determined that it should attempt to create a JS consumer (no consumer name provided, or durable name that does not exists), the library will fill a consumer config with what was provided by the user.

For push consumers, if the `DeliverSubject` is not specified, the library will pick an inbox name. For pull consumers, `DeliverSubject` needs to be left blank.

The library will set the `FilterSubject` to the user provided subject, ensure that `AckPolicy` is set and possibly some other fields such as `MaxAckPending` and set the `DeliverGroup` to the queue name if the subscribe call is for a queue subscription.

The request is then sent to `<JS prefix>.CONSUMER.DURABLE.CREATE.<stream name>.<durable name>` for a durable subscription or `<JS prefix>.CONSUMER.CREATE.<stream name>` for an ephemeral.

If the operation is successful, the library will get as an API response with a `ConsumerInfo`. Since the library successfully created the JetStream consumer, it will keep track of this fact and will delete the consumer on Unsubscribe() or Drain(). For ephemerals, the consumer name will be saved from the `ConsumerInfo`'s response, since it was not available beforehand.

If the result indicates that the consumer already exists, then it means that there was a race where a process got a lookup "not found" error, but when attempting to create the JetStream consumer, got an "already exists" error. In this case, the library will perform a consumer lookup and will perform the same checks that are described in the "Push Consumer active information and queue group binding" section. Note that for pull subscriptions, basic checks of consumer type validity should be done, but not the checks specific to "push" consumers. For them, unless this is a queue subscription, the API call will return an error.

When the consumer already exists, for push consumers, the NATS queue subscription that was created prior to the "AddConsumer" call needs to be destroyed and replaced with the new NATS queue subscription on the consumer info's `DeliverSubject`.

### Runtime

#### Heartbeats

If the JetStream consumer is configured with heartbeats, the server will periodically (based on the specified heartbeat interval) send hearbeats containing meta information about the stream and consumer sequences.

The library should setup a timer to monitor that heartbeats from the server are properly received. No specific action is taken by the library other than notifying the user through the asynchronous error callback if heartbeats are detected missing.

Also, the library should always check for JetStream heartbeat (and flow control) status messages, regardless of how the subscription was created. That is, the checks should not be conditional to the `IdleHeartbeat` option passed to a subscribe API call.

The library should keep track of all messages which reply subject start with `$JS.ACK` since they contain information about the stream and consumer sequence, etc..

When the library receives a message that is found to be a control message (no payload and containst the "Status" header), if the header is "100 Idle Heartbeat", then the library should check if there is any gap detected between what was the last tracked message meta information and the content of the heartbeat. If a gap is detected, an asynchronous error should be reported indicating what the gap is.

Ordered consumers (should have its own ADR) will handle all that for the application, recreating the subscription as needed at the right stream sequence.

**Note**: In most libraries the hearbeats messages are never presented to the user. Those are handled internally.

#### JS Ack

When the server sends a message to a subscriber, and if there is an AckPolicy specified other than "AckNone", then the message will have a reply subject that the library is supposed to use in order to acknowledge the message. This subject also encodes some delivery information, such as stream and consumer name, stream sequence, etc..

Some information allowing proper routing in context of multiple accounts and preventing an ACK for an account to ACK a message for the same stream and consumer names in another, were missing.

Until now, ACK reply subject contained 9 tokens, with this layout:
```
$JS.ACK.<stream name>.<consumer name>.<num delivered>.<stream sequence>.<consumer sequence>.<timestamp>.<num pending>
```

This is going to change with newer servers where the number of tokens will be 12, with the tokens being:
```
$JS.ACK.<domain>.<account hash>.<stream name>.<consumer name>.<num delivered>.<stream sequence>.<consumer sequence>.<timestamp>.<num pending>.<a token with a random value>
```

When there is no domain, the server will still set the token to a special value of `_` (the server will make sure that users can't pick this as a domain name). Having the domain always present simplifies the library code which does not have to bother with a variable number of tokens somewhere close to the beginning of the subject, and possibly doing some shifting to find out the location of the fields it cares about.

Why not append those new tokens at the end? This is to simplify the export/import subject, ie `$JS.ACK.<domain>.<account>.>`. Otherwise, you would need to possibly have something like `$JS.ACK.*.*.*.*.*.*.*.<domain>.<account>.>`

When unpacking an ACK subject, the library should verify that it starts with `$JS.ACK.` but then check the overall number of tokens, and if 9 assume that it is the "old" ACK messages without domain nor account hash.

If it has at *least* 12 tokens, we know that 3rd token is domain, and is reported as a new field `Domain` in the `MsgMetadata` object. The library can replace `_` with empty string when returning a `MsgMetada` object, or document the meaning of the domain named `_`.

The account hash is not used by the client at this time, only used for routing, same for the last token.

It is recommended that library no longer report an error if there are more than 12 tokens, so that new tokens may be added, especially if their purpose is solely for routing and have no impact on the library itself.

#### Flow control

When a JetStream consumer has flow control enabled (not applicable to pull consumers), the library may receive a control message that indicates that this is a flow control and should respond to the provided subject when the current pending message has been processed.

That is, suppose messages are received on a connection and pushed to a JetStream subscription, and there are currently 1000 pending messages. When the connection received a JetStream flow control status message for a given subscription, it should mark that the subscription should send an empty message to the given Flow Control subject (which is the reply subject of the incoming control message) when the library has dispatched that 1000's message in the subscription's pending list.

**Note** The format or the number of tokens of the Flow Control subject should not to be inspected, since it may change at the server discretion. From the library perspective, this is a subject that the library needs to send an empty message to, that's it.

For synchronous subscription, the `NextMsg()` call need to perform the same check and if it realized that the current message is the one that was "marked" as being the one at which the flow control should be "responded" to, then send an empty message to the recorded Flow Control subject.

It is possible, that either the flow control or its response is missed and that the consumer is considered stalled from a server perspective.

The server may include, in heartbeats status messages, a header called `Nats-Consumer-Stalled`, with a value being a flow control subject. If this header is found in an hearbeat message, the library should send "right away" an empty message to the flow control subject, regardless of its current flow control state.

Without this, it can be that a consumer remains stalled, so having both heartbeats and flow control configured seem like necessary, so we may make that a misconfiguration if flow control is configured without heartbeat.

Again, Ordered consumers should be favored.

**Note**: In most libraries the flow control messages are never presented to the user. Those are handled internally.

### Unsubscribe and Drain

When a subscription is unsubscribed or drained, if the library created the JS consumer (when it called "AddConsumer" and got an OK), the JetStream consumer is deleted at the end of those calls.

**Note** I think it is problematic for queue subscriptions since the first member to start will create the JS consumer (in cases where the JS consumer does not already exists), but will be marked as needing to delete the JS consumer on Unsubscribe()/Drain(), while other members may have attached to the same JS consumer.

For instance:
```go

// No JS consumer exists.

// Create a queue group on a durable named "shared". Since the JS consumer
// does not exist, this call will create one.
member1, _ := js.QueueSubscribe("foo", "bar", nats.Durable("shared"))

// From another application (or not), we add a member:
// Since the JS consumer existed prior to this call, the following call
// will not create the consumer and the subscription will NOT be marked
// as having to delete the consumer.
member2, _ := js.QueueSubscribe("foo", "bar", nats.Durable("shared"))

// However, at this point, member1 goes away:
member1.Drain()

// When the above completes, the library will delete the JS consumer "shared"
// because the library created it. This will cause member2 to stop receiving
// messages.
```

#### Unsubscribe

Once the normal unsubscribe processing is done, and for a JS subscription that has been marked as needing to delete the JS consumer, the library should send a "DeleteConsumer" request and return the error if any.

#### Drain

The deletion of the JS consumer need to be delayed past the point when the subscription is fully drained and removed from the connection. Since this is an asynchronous process, if the deletion of the JS consumer fails, an error will be pushed to the asynchronous error callback.

## Consequences

It is possible that some existing libraries will need changes that would break SimVer. It will be left to the library maintainer to evaluate this.
