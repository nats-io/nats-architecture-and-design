# JetStream Message Scheduler

| Metadata | Value                 |
|----------|-----------------------|
| Date     | 2025-03-21            |
| Author   | @ripienaar            |
| Status   | Approved              |
| Tags     | jetstream, 2.12, 2.14 |

| Revision | Date       | Author                      | Info                                                       | Server Version |
|----------|------------|-----------------------------|------------------------------------------------------------|----------------|
| 1        | 2025-03-21 | @ripienaar                  | Document Initial Design                                    | 2.12.0         |
| 2        | 2025-09-30 | @ripienaar                  | Use `omitempty` on configuration fields                    | 2.12.0         |
| 3        | 2026-01-05 | @MauriceVanVeen             | Support time zones for cron                                | 2.14.0         |
| 4        | 2026-04-08 | @ripienaar, @MauriceVanVeen | Add `Nats-Schedule-Rollup` & document stopping schedules   | 2.14.0         |
| 5        | 2026-04-20 | @MauriceVanVeen             | Clarify `Nats-Schedule-Source` on no messages              | 2.14.0         |
| 6        | 2026-04-23 | @MauriceVanVeen             | Clarify stream retention interaction & auto-applied rollup | 2.14.0         |

## Context and Motivation

It's a frequently requested feature to allow messages to be delivered on a schedule or to support delayed publishing.

We propose here a feature where 1 message contains a Cron-like schedule and new messages are produced, into the same stream, on the schedule. In all cases the last message on a subject holds the current schedule. In other words every schedule must have its own unique subject.

We target a few use cases in the initial design:

 * Publish a message at a later time
 * Regularly publish a message on a schedule
 * Publish the latest message for a subject on a schedule, to be used for data sampling

## Single scheduled message

In this use case the Stream will essentially hold onto a message and publish it again at a later time. Once published the held message is removed.

```bash
$ nats pub -J 'schedules.orders.single' \
  -H "Nats-Schedule: @at 2009-11-10T23:00:00Z" \
  -H "Nats-Schedule-TTL: 5m" \
  -H "Nats-Schedule-Target: orders"
  body
```

This message will be published near the supplied timestamp, the `Nats-Schedule-Target` must be a subject in the same stream and the published message could be republished using Stream Republish configuration. Additional headers added to the message will be sent to the target subject verbatim.

If a message is made with a schedule in the past it is immediately sent. If a server was down for a month and a scheduled message is recovered, even if it was schedule  for a month ago, it will be sent immediately. To avoid this, add a `Nats-TTL` header to the message so it will be removed after the TTL.

Messages produced from this kind of schedule will have a `Nats-Schedule-Next` header set with the value `purge`

The generated message has a Message TTL of `5m`.

The time format is RFC3339 and may include a timezone which the server will convert to UTC when received and execute according to UTC time later.

There may only be one message per subject that holds a schedule, if a user wishes to have many delayed messages all publishing into the same subject the scheduled messages need to go into something like `orders.schedule.UUID` where UUID is a unique identifier, set the `Nats-Schedule-Target` to the desired target subject.

## Cron-like schedules

In this use case the Stream holds a message with a Cron-like schedule attached to it and the Stream will produce messages on the given schedule.

```bash
$ nats pub -J 'schedules.orders.hourly' \
  -H "Nats-Schedule: @hourly" \
  -H "Nats-Schedule-TTL: 5m" \
  -H "Nats-Schedule-Target: orders"
  body
```

In this case a new message will be placed in `orders` holding the supplied body unchanged.The original schedule message will remain and again produce a message the next hour. Additional headers added to the message will be sent to the target subject verbatim. If the original schedule message has a `Nats-TTL` header the schedule will be removed after that time.

The generated message has a Message TTL of `5m`.

Execution times will be in UTC regardless of server local time zone.

There may only be one message per subject that holds a schedule, if a user wishes to have many scheduled messages all publishing into the same subject the scheduled messages need to go into something like `orders.cron.UUID` where UUID is a unique identifier, set the `Nats-Schedule-Target` to the desired target subject.

### Schedule Format

#### 6 field crontab format

Valid schedule header can match normal cron behavior with a few additional conveniences.

| Field Name   | Allowed Values                |
|--------------|-------------------------------|
| Seconds      | 0-59                          |
| Minutes      | 0-59                          |
| Hours        | 0-23                          |
| Day of Month | 1-31                          |
| Month        | 1-12, or names                |
| Day of Week  | 0-6, or names, 0 means Sunday |

(Note this is largely copied from `crontab(5)` man page)

A field may contain an asterisk (*), which always stands for "first-last". See `Step Values` for interaction with the `/` special character.

Ranges  of numbers are allowed. For example, 8-11 for an 'hours' entry specifies execution at hours 8, 9, 10, and 11. The first number must be less than or equal to the second one.

Lists are allowed.  A list is a set of numbers (or ranges) separated by commas.  Examples: `1,2,5,9`, `0-4,8-12`.

Step values  can  be  used in conjunction with ranges.  Following a range with "/<number>" specifies skips of the number's value through the range.  For example, `0-23/2` can be used in the 'hours' field to specify command execution for every other hour. Step values are  also  per‐mitted after an asterisk, so if specifying a job to be run every two hours, you can use `*/2`.

Names  can  also  be used for the 'month' and 'day of week' fields.  Use the first three letters of the particular day or month (case does not matter).  Ranges and list of names are allowed. Examples: `mon,wed,fri`, `jan-mar`.

Note:  The day of a command's execution can be specified in the following two fields — 'day of month', and 'day of week'.  If both fields are restricted (i.e., do not contain the `*` character), the command will be run when either field matches the current time.  For example, `30 4 1,15 * 5` would cause a command to be run at 4:30 am on the 1st and 15th of each month, plus every Friday.

#### Predefined Schedules

A number of predefined schedules exist, they can be used them like `Nats-Schedule: @hourly`.

| Entry                      | Description                                | Cron Format   |
|----------------------------|--------------------------------------------|---------------|
| `@yearly` (or `@annually`) | Run once a year, midnight, Jan. 1st        | `0 0 0 1 1 *` |
| `@monthly`                 | Run once a month, midnight, first of month | `0 0 0 1 * *` |
| `@weekly`                  | Run once a week, midnight between Sat/Sun  | `0 0 0 * * 0` |
| `@daily` (or `@midnight`)  | Run once a day, midnight                   | `0 0 0 * * *` |
| `@hourly`                  | Run once an hour, beginning of hour        | `0 0 * * * *` |

#### Intervals

You may also schedule a job to execute at fixed intervals, starting at the time it's added or cron is run. This is supported by formatting the cron spec like this:

`@every 1m`

The time specification complies with go `time.ParseDuration()` format.

## Subject Sampling

In this use case we could have a sensor that produce a high frequency of data into a Stream subject in a Leafnode. We might have realtime processing happening in the site where the data is produced but externally we only want to sample the data every 5 minutes.

```bash
$ nats pub -J 'schedules.sensors.cnc_temperature_sampled' \
  -H "Nats-Schedule: @every 5m" \
  -H "Nats-Schedule-Source: sensors.cnc.temperature
  -H "Nats-Schedule-Target: sensors.sampled.cnc.temperature"
  ""
```

Here the local site would produce high frequency temperature readings into `sensors.cnc.temperature` but we publish the latest sensor value every 5 minutes into an aggregate subject. 

## Headers

These headers can be set on message that define a schedule:

| Header                    | Description                                                                                                                                                                                                                                 |
|---------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Nats-Schedule`           | The schedule the message will be published on                                                                                                                                                                                               |
| `Nats-Schedule-Target`    | The subject the message will be delivered to                                                                                                                                                                                                |
| `Nats-Schedule-Source`    | Instructs the schedule to read the last message on the given subject and publish it to the target. If no message exists on the source subject, the schedule's own body and headers is published as a fallback. Wildcards are not supported. |
| `Nats-Schedule-TTL`       | When publishing sets a TTL on the message if the stream supports per message TTLs                                                                                                                                                           |
| `Nats-Schedule-Time-Zone` | The time zone used for the Cron schedule. If not specified, the Cron schedule will be in UTC. Not allowed to be used if the schedule is not a Cron schedule.                                                                                |
| `Nats-Schedule-Rollup`    | When publishing sets a Rollup on the message, only `sub` is a valid value                                                                                                                                                                   |

Messages that the Schedules produce will have these headers set in addition to any other headers on that was found in the message.

| Header               | Description                                                                              |
|----------------------|------------------------------------------------------------------------------------------|
| `Nats-Scheduler`     | The subject holding the schedule                                                         |
| `Nats-Schedule-Next` | Timestamp for next invocation for cron schedule messages or `purge` for delayed messages |
| `Nats-TTL`           | `5m` when `Nats-Schedule-TTL` is given with value `5m`                                   |
| `Nats-Rollup`        | `sub` when `Nats-Schedule-Rollup` is set to `sub`                                        |

The body of the message will simply be the provided body in the schedule.

Valid schedule header can match normal cron behavior as defined earlier

All time calculations will be done in UTC, a Cron schedule like `* 0 5 * * *` means exactly 5AM UTC.

Cron schedules may use different time zones, if specified in the `Nats-Schedule-Time-Zone` header. Although time zones
are supported, it's not recommended to use Cron schedules that trigger during daylight saving time (DST) changes. If
time moves forward due to DST, a schedule could be skipped if its time was not reached. If time moves backward due to
DST, a schedule could be executed twice if its time was reached twice. Additionally, the server's time zones need to be
kept up to date; otherwise servers might not run the Cron schedule at the expected time.

### Ending/stopping schedules early

Schedules can be stopped early in two ways:

- Basic: stopping one or more schedules.
- Advanced: only stop a schedule if publishing a message to a different subject succeeds (atomic).

The most basic way to stop one or more schedules is by simply deleting the message for that particular schedule. This
can be performed by:

- Deleting the schedule message directly by its stream sequence.
- Purging one schedule by its schedule subject.
- Purging one or more schedules by using a purge subject with wildcards that can match multiple schedule subjects.

Alternatively, but for more advanced use cases, a schedule can be stopped only after a message on a different subject is
persisted. This guarantees a schedule can be stopped, and a new message published, as a single atomic operation. This
can be done by sending a message:

- `Nats-Schedule-Next` header set to `purge`.
- `Nats-Scheduler` header set to the subject of the schedule.
- The message's subject equals:
  - The target subject (`Nats-Schedule-Target`) of the original schedule to publish the delayed message earlier than the
    schedule would.
  - Or, any other subject to publish to (except for the schedule subject itself). For example, with a schedule subject
    of `schedules.orders.delayed` and a target subject of `orders`, which publishes a delayed message after 5 minutes.
    You could publish to `schedules.orders.canceled` with the aforementioned headers to cancel the schedule, ensure no
    message is published to the target subject, and signal the canceled schedule to a potentially different set of
    consumers.

This is also used by single delayed scheduled messages to automatically stop the schedule after the delayed message is
published.

Clients can also use this in combination with `Nats-Expected-Last-Subject-Sequence` and
`Nats-Expected-Last-Subject-Sequence-Subject` to only end a schedule if the schedule still exists. Useful in cases
where:

- You want to make sure the schedule still exists and didn't fire already.
- You want to stop the schedule AND send a message to a different subject in one atomic operation.
- Similarly, you want to publish to the selected `Nats-Schedule-Target` earlier than the schedule would, but ensure the
  schedule doesn't duplicate the message when the schedule would (eventually) trigger.

NOTE: The selected subject in `Nats-Scheduler` can NOT equal the publish subject itself, as this would mean this message
would be purged as well due to `Nats-Schedule-Next: purge`. If the intention was to update the schedule, replacing the
former, then these headers aren't required to be set. The server already does this by default when publishing an updated
schedule. These headers are only intended to be used when desiring to stop a schedule early and publishing a message to
a different subject in one atomic operation.

## Stream Configuration

#### Creating the stream.

The `AllowMsgSchedules` field is new, added specifically for this feature and must be set to true for the feature to be enabled.

```go
type StreamConfig struct {
	// AllowMsgSchedules enables the feature
	AllowMsgSchedules bool          `json:"allow_msg_schedules,omitempty"`
}
```
* If the user intends to use the `Nats-Schedule-TTL` feature, the `AllowMsgTTL` must be true for the stream.
* Setting this on a Source or Mirror should be denied
* This feature can be enabled on existing streams but not disabled
* A Stream with this feature on should require API level 2
* Schedules are stored as rollup-subject messages: the server auto-applies `Nats-Rollup: sub` if the publisher did not set it. This is why enabling `AllowMsgSchedules` implicitly enables `AllowRollup` and clears `DenyPurge`. Publishing a new schedule to an existing schedule subject replaces the prior one.
* All three stream retention policies are supported with schedules, each with different caveats, see [Stream Retention Interaction](#stream-retention-interaction).

#### Stream Subjects
As already noted, every schedule must have its own unique subject, so it is recommended that the stream subject contain wild cards to easily allow for many schedules. 
For instance adding `schedules.>` as a stream subject would allow for all the example subjects: `schedules.orders.single`, `schedules.orders.hourly` and `schedules.sensors.cnc_temperature_sampled`

The target subjects just normal subjects like `orders`, `sensors.cnc.temperature` or `sensors.sampled.cnc.temperature` and their pattern must also be added as a stream subject. 

#### Stream Retention Interaction

Schedules occupy a sequence on the schedule subject and must remain stored for as long as the schedule should keep producing messages. Once a schedule message is removed from storage, by any mechanism, its schedule stops firing. Generated target messages follow normal retention with no special treatment or warnings.

The table below summarizes how each scheduling use case behaves under the three retention policies.

| Use case                                      | `Limits` (default)                                                                                                                                                                                                                       | `WorkQueue`                                                                                                                          | `Interest`                                                                                                         |
|-----------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------|
| **Single delayed publish** (`@at`)            | Works as documented.                                                                                                                                                                                                                     | Works, provided no consumer acknowledges the message before the schedule fires (unless that was intentional to cancel the schedule). | Works, provided at least one consumer has interest in the schedule subject, plus the mentioned `WorkQueue` caveat. |
| **Cron schedule** (`@hourly`, `@every`, ...)  | Works as documented.                                                                                                                                                                                                                     | Works under the same condition above.                                                                                                | Works under the same condition above.                                                                              |
| **Subject sampling** (`Nats-Schedule-Source`) | Works as documented.                                                                                                                                                                                                                     | Works under the same condition above; source subject must also fit within `WorkQueue` semantics.                                     | Works under the same condition above; source subject must also fit within `Interest` semantics.                    |
| **Stream limits on schedule lifetime**        | `MaxAge` shorter than the firing interval deletes the schedule before it fires, prefer `Nats-TTL` on the schedule itself. Schedule removal due to `DiscardOld`, `MaxMsgs`, `MaxBytes`, etc. similarly removes and disables the schedule. | Same as `Limits`, plus the `WorkQueue` caveats above.                                                                                | Same as `Limits`, plus the `Interest` caveats above.                                                               |
| **Consumer acknowledges the schedule**        | Standard consumer behaviour. If the schedule is consumed and acknowledged, it is not removed.                                                                                                                                            | Consumer removes the schedule if acknowledged.                                                                                       | Consumer removes the schedule if (all) acknowledged, or the schedule is removed if no interest remains.            |

Operational notes:

- **Limits** is the simplest configuration for most scheduling use cases and is recommended unless there is a specific reason to use the other policies for the same stream.
- **WorkQueue**: a consumer filtered on a schedule subject will remove the schedule on ack, permanently stopping it. It is valid for no consumer to exist on the schedule subjects, such that the schedules fire independently and aren't removed via acks. If schedules are consumed and acknowledged, this disables the schedule but this can pose a race condition, since message deletion as a result of acknowledgement isn't immediate. If used in this way, be aware not to use a schedule interval that triggers very quickly if you want to optimistically acknowledge it to stop it.
- **Interest**: if no consumer has interest in the schedule subject, the schedule will not be stored, nor will it trigger scheduled messages.

Acknowledging a schedule on `WorkQueue` or `Interest` retention can be used to remove the schedule, but more reliable ways exist to disable a schedule in general, see [Ending/stopping schedules early](#endingstopping-schedules-early).

There are two ways to combine schedules with `Interest` retention:

1. **Pinning consumer on the Interest stream (single-stream workaround).** Configure at least one consumer with a `FilterSubject` covering the schedule subject pattern on the same `Interest` stream that holds the schedules. The consumer does not need to deliver anywhere; `AckPolicy=none` or an unconsumed durable is enough to pin interest and prevent schedules from being removed. Simple to set up, but the pinning consumer becomes load-bearing configuration: if it is deleted or its filter drifts, schedules silently stop. Although this is a "mostly" functional configuration, it's not recommended to be used since the pinning consumer adds overhead, especially when replicated.
2. **Separate WorkQueue source stream (two-stream composition).** Place the schedules in a dedicated `WorkQueue` stream (with `AllowMsgSchedules=true` and subjects covering both schedule and target patterns), and have the `Interest` stream source the target subjects from it. Schedules live entirely in the `WorkQueue` stream, where nothing removes them so long as no consumer acknowledges the message containing the schedule. The schedule fires into the target subject inside the `WorkQueue` stream; the source relationship drains the generated target messages into the `Interest` stream for application consumers. This isolates schedule state from interest-driven retention and does not require a pinning consumer, at the cost of an extra stream and sourcing configuration. Note that `AllowMsgSchedules` must be set on the `WorkQueue` source stream only, the `Interest` stream cannot set it because it has sources configured.

Option 2 is generally the more robust configuration because schedule persistence no longer depends on a consumer that exists solely to hold interest.
