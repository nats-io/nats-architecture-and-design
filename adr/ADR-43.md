# JetStream Per-Message TTL

| Metadata | Value                           |
|----------|---------------------------------|
| Date     | 2024-07-11                      |
| Author   | @ripienaar                      |
| Status   | Approved                        |
| Tags     | jetstream, client, server, 2.11 |

## Context and motivation

Streams support a one-size-fits-all approach to message TTL based on the MaxAge setting. This causes any message in the 
Stream to expire at that age.

There are numerous uses for a per-message version of this limit, some listed below:

 * KV tombstones are a problem in that they forever clog up the buckets with noise, these could have a TTL to make them expire once not useful anymore
 * Server-applied limits can result in tombstones with a short per message TTL so that consumers can be notified of limits being processed. Useful in KV watch scenarios being notified about TTL removals
 * A stream may have a general MaxAge but some messages may have infinite retention, think a schema or type hints in a KV bucket that is forever while general keys have TTLs

Related issues [#3268](https://github.com/nats-io/nats-server/issues/3268)

## Per-Message TTL

We will allow a message to supply a TTL using a header called `Nats-TTL` followed by the duration as seconds or as a Go duration string like `1h`.

The duration will be used by the server to calculate the deadline for removing the message based on its Stream 
timestamp and the stated duration.

Setting the header `Nats-TTL` to `never` will result in a message that will never be expired.

A TTL of zero will be ignored, any other unparsable value will result in a error reported in the Pub Ack and the message
being discarded.

When a message with the `Nats-TTL` header is published to a stream with the feature disabled the message will be rejected with an error.

## Limit Tombstones

Several scenarios for server-created tombstones can be imagined, the most often requested one though is when MaxAge
removes last value (ie. the current value) for a Key.

In this case when the server removes a message and the message is the last in the subject it would place a message 
with a TTL matching the Stream configuration value.  The following headers would be placed:

```
Nats-Applied-Limit: MaxAge
Nats-TTL: 1
```

The `Nats-Limit-Applied` field is there to support future expansion of this feature.

This behaviour is off by default unless opted in on the Stream Configuration.

## Stream Configuration

Weather or not a stream support this behavior should be a configuration opt-in. We want clients to definitely know 
when this is supported which the opt-in approach with a boolean on the configuration would make clear.

We have to assume someone will want to create a replication topology where at some point in the topology these tombstone
type messages are retained for an audit trail. So a Stream with this feature enabled can replicate to one with it 
disabled and all the messages that would have been TTLed will be retained.

```golang
type StreamConfig struct {
	// AllowMsgTTL allows header initiated per-message TTLs
	AllowMsgTTL bool          `json:"allow_msg_ttl"`

	// LimitsTTL activates writing of messages when limits are applied with a specific TTL
	LimitsTTL   time.Duration `json:"limits_ttl"`
}
```

The `AllowMsgTTL` field must not be updatable, `LimitsTTL` may be updated and must have a minimum value of 1 second.

When either these settings are set the Stream should require API level `1`.
