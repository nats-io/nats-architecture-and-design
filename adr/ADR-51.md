# JetStream Message Scheduler

| Metadata | Value      |
|----------|------------|
| Date     | 2025-03-21 |
| Author   | @ripienaar |
| Status   | Approved   |
| Tags     | jetstream  |


| Revision | Date       | Author     | Info                    |
|----------|------------|------------|-------------------------|
| 1        | 2025-03-21 | @ripienaar | Document Initial Design |

## Context and Motivation

It's a frequently requested feature to allow messages to be delivered on a schedule or to support delayed publishing.

We propose here a feature where 1 message contains a Cron-like schedule and new messages are produced, into the same stream, on the schedule.

We target a few use cases in the initial design:

 * Publish a message at a later time
 * Regularly publish a message on a schedule
 * Publish the latest message for a subject on a schedule, to be used for data sampling

## Single scheduled message

In this use case the Stream will essentially hold onto a message and publish it again at a later time. Once published the held message is removed.

```bash
$ nats pub -J '$SCHED.update_orders' \
  -H "Nats-Schedule: @at 2025-01-01 16:00:00" \
  -H "Nats-Schedule-TTL: 5m" \
  -H "Nats-Schedule-Target: $SCHED.trigger.update_orders"
  body
```

This message will be published near the supplied timestamp, the `Nats-Schedule-Target` must be a subject in the same stream and the published message may could be republished using Stream Republish configuration. 

The generated message has a Message TTL of `5m`.

## Cron-like schedules

In this use case the Stream holds a message with a Cron-like schedule attached to it and the Stream will produce messages on the given schedule.

```bash
$ nats pub -J '$SCHED.update_orders' \
  -H "Nats-Schedule: @hourly" \
  -H "Nats-Schedule-TTL: 5m" \
  -H "Nats-Schedule-Target: $SCHED.trigger.update_orders"
  body
```

In this case a new message will be placed in `$SCHED.trigger.update_orders` holding the supplied body unchanged.  The original schedule message will remain and again produce a message the next hour.

The generated message has a Message TTL of `5m`.

Valid schedule header can match normal cron behaviour, perhaps based on the specification from [github.com/robfig/cron](https://pkg.go.dev/github.com/robfig/cron).

## Subject Sampling

In this use case we could have a sensor that produce a high frequency of data into a Stream subject in a Leafnode. We might have realtime processing happening in the site where the data is produced but externally we only want to sample the data every 5 minutes.

Another use case is with ADR-49 Counters, when many regional counters are aggregated into a single global counter the volume of updates could be very high - too high to republish each update.  In that scenario one can use a Schedular to publish the current count once every minute.

```bash
$ nats pub -J '$SCHED.update_orders' \
  -H "Nats-Schedule: @every 1m" \
  -H "Nats-Schedule-Source: $SCHED.counter.orders
  -H "Nats-Schedule-Target: $SCHED.sample.orders"
  ""
```

## Headers

These headers can be set on message that define a schedule:

| Header                 | Description                                                                                                                        |
|------------------------|------------------------------------------------------------------------------------------------------------------------------------|
| `Nats-Schedule`        | The schedule the message will be published on                                                                                      |
| `Nats-Schedule-Target` | The subject the message will be delivered to, if combined with `Nats-Schedule-Source` can have wildcards for replacement           |
| `Nats-Schedule-Source` | Instructs the schedule to read the last message on the given subject and publish it. If the Subject is empty, nothing is published |
| `Nats-Schedule-Ttl`    | When publishing sets a TTL on the message if the stream supports per message TTLs                                                  |

Messages that the Schedules produce will have these headers set in addition to any other headers on that was found in the message.

| Header               | Description                   |
|----------------------|-------------------------------|
| `Nats-Schedular`     | `$SCHED.update_orders`        |
| `Nats-Schedule-Next` | Timestamp for next invocation |
| `Nats-TTL`           | `5m`                          |

The body of the message will simply be the provided body in the schedule.

Valid schedule header can match normal cron behaviour, perhaps based on the specification from [github.com/robfig/cron](https://pkg.go.dev/github.com/robfig/cron).

We would support one additional schedule kind `@at <timestamp>` which will act as a single use schedule, after 
triggering the schedule will be removed.

## Stream Configuration

```go
type StreamConfig struct {
	// AllowMsgSchedules enables the feature
	AllowMsgSchedules bool          `json:"allow_msg_schedules"`
}
```

 * Setting this on a Source or Mirror should be denied
 * This feature can be turned off and on using Stream edits, turning it on should only be allowed on an empty, or purged, Stream.
 * A Stream with this feature on should require API level 2
