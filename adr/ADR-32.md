# Title

| Metadata | Value                                                          |
| -------- |----------------------------------------------------------------|
| Date     | 2022-11-23                                                     |
| Author   | @aricart, @derekcollison, @tbeets, @scottf, @Jarema, @piotrpio |
| Status   | Approved                                                    |
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

#### Consume

Retrieve messages from the server while maintaining a buffer that will refill at
some point during the message processing maintaining a buffer that will allow
the processing to go as fast as the client has selected by options on the read
call.

Client may want some way to drain the buffer or iterator without pulling
messages, so that the client can cleanly stop without leaving many messages
un-acked.


#### Info

An optional operation that returns the consumer info. Note that depending on
the context (a consumer that is exported across account) the JS API to retrieve
the info on the consumer may not be available.

#### Delete

An optional operation that allows deleting the consumer. Note that depending
on the context (a consumer that is exported across account) the JS API to
delete the consumer may not be available.

## Consequences

The new JetStream simplified consumer API is separate from the _legacy_
functionality. The legacy functionality will be deprecacted.
