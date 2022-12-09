# Title

| Metadata | Value                                                          |
| -------- | -------------------------------------------------------------- |
| Date     | 2022-11-23                                                     |
| Author   | @aricart, @derekcollison, @tbeets, @scottf, @Jarema, @piotrpio |
| Status   | Approved                                                       |
| Tags     | jetstream, client                                              |

## Context and Problem Statement

Consuming messages from a JetStream require a large number of options and design
decisions from client API users:

- Current JetStream clients create and update a consumer definition on the fly
  as `subscribe()` or some other functionality for consuming messages is
  invoked. This leads to some unexpected behaviors as different clients,
  possibly written using different versions using different options of the
  library attempt to consume from the same consumer.

- Clients implementing JetStream code are also confronted with a choice on
  whether they should be implementing a `Pull` or `Push` subscriber.

The goal of this ADR is to provide a simpler API to JetStream users that reduces
the number of options they are confronted with and provides the expected
performance.

## Design

JetStream simplification revolves around the concept of the `Consumer`. The
consumer definition managed by the server is created by tools (such as nats cli)
or code. A `Consumer` can be created or added as per the client language
implementation, typically by providing a stream name and some consumer options.

Clients wanting to consume messages from the consumer, simply locate the
`Consumer`, which then provides functionality for processing, or consuming
messages. The representation of the `Consumer` is retrieved, and provides
operations to `fetch` and `consume` messages.

### Consumers

Consumer are JetStream API entities from which messages can be read. Consumers
can be created using language specific verbs. In some cases some of the
libraries will hang them under a _consumer api_ to prevent a collision with
functionality that may already exist in the client for purposes of managing the
low level `ConsumerInfo`.

This means that in some languages `add` may not be available (unless major
version bump in the library). For that reason libraries can choose how to
instantiate a consumer:

- `new Consumer(consumerInfo)`
- `add(streamName, consumerOptions)`
- `create(streamName, consumerOptions)`
- `get(streamName, consumerName)`
- `delete(streamName, consumerName)`

Some of the libraries can provide this functionality by chaining if that is
appropriate, and makes sense given their current JetStreamManager implementation
semantics:

`getStream(name).getConsumer(name)`

### Ordered Consumer

By their very nature, OrderedConsumers don’t play well with the above API for
retrieving the consumer, because the consumer itself will be created and
possibly re-created by the client during it's lifecycle. Clients may wish to
provide a:

`getOrderedConsumer(streamName, startOptions, bufferingOptions)` which allows
consuming messages while conforming to the API operations on a consumer outlined
below.

### Operations

Consumers will have the following operations:

- `Fetch`
- `Consume`
- `Info` - An optional operation that returns the consumer info of the consumer
- `Delete` - An optional operation to delete the referenced consumer

Both the fetch/consume can provide hints on the batch of messages/data they want
to process, and perhaps how long to keep a request for messages open.

Lifecycle of the Read/Consume may need to be controlled - for example to stop
delivering messages to the callback or drain messages already accumulated before
stopping the consumer, these can be additional methods on the consumer
implementation if appropriate or an object that is the return value of callback
driven consumers.

#### Fetch

Get one or more messages. This operation will end once the RPC expires or the
number of messages/data batch requested is provided. The user is in control of
when they retrieve the messages from the server.

Depending on the language, the messages will be delivered via a callback with
some signal to indicate that the fetch has finished (could be message is null)
or via some iterator functionality where getting the next message will block
until a message is yielded or the operation or the operation finishes, which
terminates the iterator.

##### Options

- `max_messages?: number` - max number of messages to return
- `expires: number` - amount of time to wait for the request to expire
  (required)
- `max_bytes?: number` - max number of bytes to return
- `idle_heartbeat?: number` - amount idle time the server should wait before
  sending a heartbeat

Note that while `batch` and `max_bytes` are described as optional at least one
of them is required.

Note that when specifying both `batch` and `max_bytes`, `max_bytes` will take
precedence. This means that if all messages exceed the specified `max_bytes` no
message will be yielded by the server

#### Consume

Retrieve messages from the server while maintaining a buffer that will refill at
some point during the message processing maintaining a buffer that will allow
the processing to go as fast as the client has selected by options on the read
call.

Client may want some way to `drain()` the buffer or iterator without pulling
messages, so that the client can cleanly stop without leaving many messages
un-acked.

##### Options

- `max_messages?: number` - max number of messages to return
- `expires: number` - amount of time to wait for the request to expire.
- `max_bytes?: number` - max number of bytes to return
- `idle_heartbeat?: number` - amount idle time the server should wait before
  sending a heartbeat
- `threshold_messages?: number` - hint for the number of messages that should
  trigger a low watermark on the client, and influence it to request more
  messages.
- `threshold_bytes?: number` - hint for the number of messages that should
  trigger a low watermark on the client, and influence it to request more data.

Note that `max_messages` and `max_bytes` are exclusive. Clients should not allow depending on both constraints.
If no options is provided, clients should use a default value for `max_messages` and not set `max_bytes`.
For each constraint, a corresponding threshold can be set.

###### Defaults and constraints

> Values used as defaults if options are not provided are subject to further discussion.
> The values presented below are not definite.

- `max_messages` - 1000, [1-???]
- `expires` - 60s, [4s-10m]?
- `max_bytes` - not set, use `max_messages` if not provided
- `idle_heartbeat` - 30s, [2s-60s]?
- `threshold_messages` - 25% of `max_messages`, rounded up (to avoi getting stuck for low max_messages values)
- `threshold_bytes` - 25% of `max_bytes`

The default values and constraints used for `expiry` and `idle_heartbeat` need to be carefully selected,
as consumer stability has to be taken into account (when those values are very low, e.g. heartbeat == 1s).

##### Consume specification

An algorithm for continuously fetching messages should be implemented in clients,
taking into account language constructs.

###### NATS subscription

Consume should create a single subscription to handle responses for all pull requests.
The subject on which the subscription is created is used as reply for each `CONSUMER.MSG.NEXT` request.

###### Max messages and max bytes options

Users should be able to set either max_messages or max_bytes values, but not both:

- If no option is provided, the default value for `max_messages` should be used, and
`max_bytes` should not be set.
- If `max_messages` is set by the user, the value should be set for `max_messages` and `max_bytes`
should not be set.
- User cannot set both constraint for a single `Consume()` execution.
- For each constraint, a custom threshold can be set, containing the number of messages/bytes that should be received to
trigger the next pull request. The value of threshold cannot be higher than the corresponding constraint's value.
- For each pull request, `batch` or `max_bytes` value should be equal to the threshold value (to fill the buffer)

###### Buffering messages

`Consume()` should pre-buffer messages up to a limit set by `max_messages` or `max_bytes` options (whichever is provided).
Clients should track the total amount of messages pending in a buffer. Whenever a threshold is reached,
a new request to `CONSUMER.MSG.NEXT` should be published.

There is no need to track specific pull request's status - as long as the aggregate message and byte count is
maintained, `Consume()` should be able to fill the buffer appropriately.

Pending messages and bytes count should be updated when:

- A new pull request is published - add a value of `request.batch_size` to the pending messages count and
the value of `request.max_bytes` to the pending byte count.
- A new user message is processed - subtract 1 from pending messages count and subtract message size from penging byte count.
The message size (in bytes) is defined as: `len(msg.data) + len(msg.subject) + len(msg.reply)`
- A pull request termination error is received - subtract the value of `Nats-Pending-Messages` header
from pending messages count and subtract the value of `Nats-Pending-Bytes` from pending bytes count.

###### JetStream error handling

There are 2 kinds of errors which can be received as a response to a pull request:

- Pull request termination error is any error containing `Nats-Pending-Messages` and `Nats-Pending-Bytes` headers.
These should not be treated as errors by the application, but rather used to calculate the pending messages/bytes count.
- Every other pull request error should be treated as terminal error - it should be telegraphed to the user (in langie-specific way),
the `Consume()` execution should be stopped and subscription should be drained.

###### Idle heartbeats

`Consume()` should always utilize idle heartbeats. Heartbeat values are calculated as follows:

An error is triggered if the timer reaches 2 * request's idle_heartbeat value.
The timer is reset on each received message (this can be either user message, error message or heartbeat message).

Heartbeat timer should be reset and paused in the event of client disconnect and resumed on reconnect.

On heartbeat error, the consumer subscription should be drained and the message processing should be terminated.

###### Server reconnects

Clients should detect server disconnections and reconnections.

When a disconnect event is received, client should:

- Reset the heartbeat timer.
- Pause the heartbeat timer.

When a reconnect event is received, client should:

- Resume the heartbeat timer.
- Check if consumer exists (fetch consumer info). If consumer is not available, terminate `Consume()` execution.
- Publish a new pull request.

###### Message processing algorithm

Below is the algorithm for receiving and processing messages.
It does not take into account server reconnects and heartbeat checks - those should be
handled asynchronously in a separate thread / routine.

1. Verify whether a new pull request needs to be sent:
   - pending messages count reaches threshold
   - pending byte count reaches threshold
2. If yes, publish a new pull request and add request's `batch` and
`max_bytes` to pending messages and bytes counters.
3. Check if new message is availabe.
   - if yes, go to #4
   - if not, go to #1
4. Reset the heartbeat timer.
5. Verify the type of message:
   - if message is a hearbeat message, go to #1
   - if message is a user message, handle it and subtract 1 message from pending message count
    and message size from pending bytes count and go to #1
   - if message is an error, go to #6
6. Verify error type:
   - if message contains `Nats-Pending-Messages` and `Nats-Pending-Bytes` headers, go to #7
   - else terminate the subscription and exit
7. Read the values of `Nats-Pending-Messages` and `Nats-Pending-Bytes` headers.
8. Subtract the values from pending messages count and pending bytes count respectively.
9. Go to #1.

#### Info

An optional operation that returns the consumer info. Note that depending on the
context (a consumer that is exported across account) the JS API to retrieve the
info on the consumer may not be available.

#### Delete

An optional operation that allows deleting the consumer. Note that depending on
the context (a consumer that is exported across account) the JS API to delete
the consumer may not be available.

## Consequences

The new JetStream simplified consumer API is separate from the _legacy_
functionality. The legacy functionality will be deprecacted.
