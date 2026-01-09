# JetStream durable stream sourcing/mirroring

| Metadata | Value                           |
|----------|---------------------------------|
| Date     | 2025-11-06                      |
| Author   | @MauriceVanVeen                 |
| Status   | Proposed                        |
| Tags     | jetstream, client, server, 2.14 |

| Revision | Date       | Author          | Info                                                                   |
|----------|------------|-----------------|------------------------------------------------------------------------|
| 1        | 2025-11-06 | @MauriceVanVeen | Initial design                                                         |
| 2        | 2025-12-05 | @MauriceVanVeen | Refinement after initial implementation                                |
| 3        | 2025-01-09 | @MauriceVanVeen | Server-managed durable sourcing & migrating from ephemeral to durable  |

## Context and Problem Statement

JetStream streams can be mirrored or sourced from another stream. Usually this is done on separate servers, for example,
loosely connected as a leaf node. This is achieved by the server creating an ephemeral ordered push consumer using
`AckNone`. This is really reliable if the stream that's being mirrored/sourced is a Limits stream. If the server detects
a gap, it recreates the consumer at the sequence it missed. And since the stream is a Limits stream, it will be able to
recover from the gap since the messages will still be in the stream.

However, if the stream is a WorkQueue or Interest stream, then the use of an ephemeral `AckNone` consumer is problematic
for two reasons:

- For both WorkQueue and Interest streams any messages that are sent are immediately acknowledged and removed. If this
  message is not received on the other end, this message will be lost.
- Additionally, for an Interest stream since the consumer is ephemeral, interest will be lost while there's no active
  connection between the two servers. This also results in messages being lost.

Reliable stream mirroring/sourcing is required for use cases where WorkQueue or Interest streams are used or desired.

## Design

### Pre-created durable consumer

Instead of the server creating and managing ephemeral consumers for stream sourcing, the user creates a durable consumer
that the server will use.

The benefits of using a durable consumer are that these will be visible to the user and can be monitored. This eases the
control (and security implications) of the consumer configuration as this consumer will be manually created on the
server containing the data that will be sourced. Additionally, the consumer can be paused and resumed, allowing the
sourcing to temporarily stop if desired.

WorkQueue streams don't allow having multiple consumers with overlapping filter subjects. This means that a durable
consumer used for mirroring/sourcing of a WorkQueue stream, would not allow another overlapping consumer to be created
used for a different purpose. In that case, an Interest or Limits stream should be used.

Some additional tooling will be required to create the durable consumer with the proper configuration. But through the
use of a new `AckPolicy=AckFlowControl` field, the server will be able to help enforce the correct configuration.

Additionally, when configuring the stream source, fields like `OptStartSeq`, `OptStartTime`, and `FilterSubject` are not
allowed when using durable sourcing. Instead, the durable consumer needs to be configured as such:

- Instead of configuring `OptStartSeq` on the source, configure it in the consumer config: `OptStartSeq`.
- Instead of configuring `OptStartTime` on the source, configure it in the consumer config: `OptStartTime`.
- Instead of configuring `FilterSubject` on the source, configure it in the consumer config: `FilterSubject`.

All other consumer configuration fields, like `DeliverPolicy` and `ReplayPolicy`, are allowed as long as the constraints
for durable sourcing are met. This also allows for new use cases that the ephemeral sourcing currently doesn't support:

- Using `DeliverPolicy=last_per_subject` for only sourcing the last message per subject and every update onward.
- Using `ReplayPolicy=original` to allow sourcing at the speed the messages were received in originally.

### Performance / Consumer configuration

The durable consumer used for stream sourcing/mirroring will need to be just as performant as the current ephemeral
variant. The current ephemeral consumer configuration uses `AckNone` which is problematic for WorkQueue and Interest
streams. A new `AckPolicy` (`AckFlowControl`) will need to be used to ensure that performance is on par with an
ordered consumer and the messages are not lost.

The consumer configuration will closely resemble the ephemeral push consumer variant:

- The consumer will still act as an "ordered push consumer" but it will be durable.
- Requires `FlowControl` and `Heartbeat` to be set.
- Uses `AckPolicy=AckFlowControl` instead of `AckNone`.
- `AckPolicy=AckFlowControl` will function like `AckAll`. The flow control messages (see below) will ack messages rather
  than the current ack reply format.
- The receiving server responds with a flow control message, which includes the stream sequence (`Nats-Last-Stream`)
  and delivery sequence (`Nats-Last-Consumer`) as headers to signal which messages have been successfully stored.
- The server receiving the flow control response will ack messages based on these stream/delivery sequences. For
  WorkQueue and Interest streams this may result in messages deletion.
- Acknowledgements happen based on flow control limits, usually a data window size. But if the stream is idle the
  `Heartbeat` will also trigger a flow control message to move the acknowledgement floor up.
- Flow control messages will happen automatically after a certain data size is reached, but can be controlled using the
  `MaxAckPending` setting. `MaxAckPending` determines the maximum number of pending messages that can be sent before the
  sourcing pauses. A flow control message will be automatically sent (no need to wait for a `Heartbeat`) so these
  messages are acknowledged and new messages can be sent as soon as possible.
- Since acknowledgements happen based on dynamic flow control, it being determined either by data size, `MaxAckPending`
  or `Heartbeat`, the consumer cannot have an `AckWait` or `BackOff` setting. These fields need to be unset, or an error
  will be returned.
- Additionally, `MaxDeliver` must be set to `-1` (infinite) to ensure if some messages are lost in transit, they can
  still be reliably redelivered.

### Stream configuration

The stream configuration will be extended to include details about the consumer that should be used for the
sourcing/mirroring.

```go
type StreamSource struct {
    Name     string                   `json:"name"`
    Consumer *StreamConsumerSource    `json:"consumer,omitempty"`
}

type StreamConsumerSource struct {
    Name           string `json:"name,omitempty"`
    ServerManaged  bool   `json:"server_managed,omitempty"`
    DeliverSubject string `json:"deliver_subject,omitempty"`
}
```

Prior to durable sourcing, sourcing can only be configured to use an ephemeral consumer:

```json
{
  "stream": "aggregate",
  "sources": [
    {
      "name": "source",
      "opt_start_seq": 100,
      "filter_subject": "source.type.>"
    }
  ]
}
```

The ephemeral consumer is created and managed by the server doing the sourcing. The user has limited control to define
the consumer config, for example, by using an optional starting sequence and filter subject.

Durable sourcing/mirroring introduces two ways to use durable consumers:

- "Server-managed": the server creates the durable consumer with the proper configuration. Providing more control
  over the consumer lifecycle, but the same control over the consumer config as when using ephemeral sourcing. The
  starting sequence and filter subject can still be configured in the sourcing config.

```json
{
  "stream": "aggregate",
  "sources": [
    {
      "name": "source",
      "opt_start_seq": 100,
      "filter_subject": "source.type.>",
      "consumer": {
        "name": "source-consumer",
        "server_managed": true
      }
    }
  ]
}
```

- "User-managed": the user pre-creates the durable consumer and configures it to use the proper configuration. Providing
  the highest level of control over both the consumer config and its lifecycle. The starting sequence and filter subject
  need to be configured on the consumer that's used, instead of on the sourcing config. The sourcing config is only
  extended to contain the consumer name and deliver subject.

```json
{
  "stream": "aggregate",
  "sources": [
    {
      "name": "source",
      "consumer": {
        "name": "source-consumer",
        "deliver_subject": "mirror.consumer.deliver.subject"
      }
    }
  ]
}
```

These options allow users to choose the best fit for their use case, as well as migrate from ephemeral to durable
sourcing:

- Simplest config and lifecycle control: ephemeral. The user doesn't manage nor see the consumer when using the
  JetStream API. It's automatically created and recreated in the background. This consumer generally works well for
  sourcing from and to Limits-based streams, but has limitations and should not be used to source from a WorkQueue or
  Interest stream.
- "Server-managed" allows easy migration from ephemeral to durable sourcing. The user only needs to choose the desired
  consumer name, as well as specifying `server_managed: true`, but sourcing otherwise functions in the same way. Since a
  durable consumer is used, it will be visible to the user and can be paused or updated (although limited).
- "User-managed" allows full control over the consumer lifecycle and configuration. Intended to be used if the
  "server-managed" approach doesn't fit the use case, primarily for security reasons or to allow more advanced uses of
  the consumer configuration that's not supported in the sourcing config. Or, in some use cases where the stream that's
  sourced is bootstrapped along with the durable consumer on a leaf node that's not yet connected to the server or
  cluster doing the sourcing. Once this connection is established, all available messages will be mirrored/sourced.

It's generally recommended to use "server-managed" durable sourcing for most use cases, as it's simple to configure and
allows migration from ephemeral to durable sourcing. While also allowing the consumer that's used to be observable as
well as paused, and supporting reliable and guaranteed sourcing from any stream to any other stream regardless of stream
retention policies.

### Consumer delivery state reset API

The ordered consumer implementation relies on the consumer's delivery sequence to start at 1 and increment by 1 for
every delivered message. Gaps are detected by ensuring this delivery sequence increments monotonically. If a gap is
detected, the consumer delivery state will need to be reset such that the delivery sequence starts at 1 again.

This is a non-issue for the ephemeral ordered push consumer variant as it creates a new consumer starting at the
expected sequence if a gap is detected. However, the durable consumer must not be deleted and recreated, since that will
result in losing interest on an Interest stream and subsequently losing messages. Waiting for re-delivery is also not an
option as this will result in out of order delivery.

Therefore, the server will provide an API to reset the consumer delivery state. When the server detects a gap, it will
call this API to reset the consumer delivery state. The consumer delivery sequence restarts at 1 and re-delivers pending
messages. Optionally re-delivery will start from a specified stream sequence.

This reset API, `$JS.API.CONSUMER.RESET.<STREAM>.<CONSUMER>`, will have the following functionality:

- The consumer will be reset, resembling the delivery state of creating a new consumer with `opt_start_seq` set to the
  specified sequence.
- The pending and redelivered messages will always be reset.
- The delivered stream and consumer sequences will always be reset.
- The ack floor consumer sequence will always be reset.
- The ack floor stream sequence will be updated depending on the payload. The next message to be delivered is above this
  new ack floor.
- An empty payload will reset the consumer's state, but the ack floor stream sequence will remain the same. (This will
  be used for the durable sourcing consumer after detecting a gap.)
- A payload of `{"seq":<seq>}` (with `seq>0`) will update the ack floor stream sequence to be one below the provided
  sequence. The next message to be delivered has a sequence of `msg.seq >= reset.seq`. A zero-sequence is invalid.
- Resetting a consumer to a specific sequence will only be allowed on specific consumer configurations.
    - Only allowed on `DeliverPolicy=all,by_start_sequence,by_start_time`.
    - If `DeliverPolicy=all`, the reset will always be successful and allow to move forward or backward arbitrarily.
    - If `DeliverPolicy=by_start_sequence,by_start_time`, the reset will only be successful if
      `reset.seq >= opt_start_seq`, or if `loadNextMsg(reset.seq).start_time >= opt_start_time`. This is a safety
      measure to prevent the consumer from being reset to a sequence before what was allowed by the consumer
      configuration.
- The response to the reset API call will follow standard JS API conventions. Specifically, returning a "consumer
  reset" response to not only reset the consumer, but also expose the current configuration and updated delivery state
  like a "consumer create" response. This is useful for the durable sourcing consumer to confirm the proper
  configuration is used before allowing the sourcing to happen. As well as generally looking as if the consumer was
  recreated, this response can then also be kept by clients if they need to keep a cached consumer response.
- Additionally, the response will contain the `ResetSeq` that the consumer is reset to.

Importantly, the server should also handle the case where a user manually resets the consumer that's used for sourcing.
The server should handle this gracefully and ensure no messages are lost. However, the user could also reset the
consumer such that it moves ahead in the stream. The server should also handle this by properly skipping over those
messages. This is done by noticing the consumer delivery sequence goes back to 1, the server can then skip the messages
between the received stream sequence and the one it received previously. If instead the user manually resets the
consumer to go backward, the server should guarantee that mirrored messages are not duplicated.

## Consequences

Client should support the `$JS.API.CONSUMER.RESET.<STREAM>.<CONSUMER>` reset API. Clients should not rely on this call
to be initiated by the client process, but it potentially being called by another process, by the CLI for example.
Importantly, clients should not fail when the consumer delivery sequence is not monotonic, except when needed for the
"ordered consumer" implementations. If the reset API is called for an ordered consumer, the client should detect a gap
as it would normally and simply recreate the consumer.
