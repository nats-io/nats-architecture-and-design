# JetStream Message Scheduler

| Metadata | Value           |
|----------|-----------------|
| Date     | 2025-03-21      |
| Author   | @ripienaar      |
| Status   | Approved        |
| Tags     | jetstream, 2.12 |


| Revision | Date       | Author     | Info                    |
|----------|------------|------------|-------------------------|
| 1        | 2025-03-21 | @ripienaar | Document Initial Design |

## Context and Motivation

It's a frequently requested feature to allow messages to be delivered on a schedule or to support delayed publishing.

We propose here a feature where 1 message contains a Cron-like schedule and new messages are produced, into the same stream, on the schedule. In all cases the last message on a subject holds the current schedule. In other words every schedule must have it's own subject.

We target a few use cases in the initial design:

 * Publish a message at a later time
 * Regularly publish a message on a schedule
 * Publish the latest message for a subject on a schedule, to be used for data sampling

## Single scheduled message

In this use case the Stream will essentially hold onto a message and publish it again at a later time. Once published the held message is removed.

```bash
$ nats pub -J 'orders_schedules' \
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
$ nats pub -J 'orders_schedules' \
  -H "Nats-Schedule: @hourly" \
  -H "Nats-Schedule-TTL: 5m" \
  -H "Nats-Schedule-Target: orders"
  body
```

In this case a new message will be placed in `orders` holding the supplied body unchanged.  The original schedule message will remain and again produce a message the next hour. Additional headers added to the message will be sent to the target subject verbatim. If the original schedule message has a `Nats-TTL` header the schedule will be removed after that time.

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
$ nats pub -J 'sensors.schedules' \
  -H "Nats-Schedule: @every 5m" \
  -H "Nats-Schedule-Source: sensors.cnc.temperature
  -H "Nats-Schedule-Target: sensors.sampled.cnc.temperature"
  ""
```

Here the local site would produce high frequency temperature readings into `sensors.cnc.temperature` but we publish the latest sensor value every 5 minutes into an aggregate subject. 

## Headers

These headers can be set on message that define a schedule:

| Header                 | Description                                                                                                                                                     |
|------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Nats-Schedule`        | The schedule the message will be published on                                                                                                                   |
| `Nats-Schedule-Target` | The subject the message will be delivered to                                                                                                                    |
| `Nats-Schedule-Source` | Instructs the schedule to read the last message on the given subject and publish it. If the Subject is empty, nothing is published, wildcards are not supported |
| `Nats-Schedule-TTL`    | When publishing sets a TTL on the message if the stream supports per message TTLs                                                                               | |

Messages that the Schedules produce will have these headers set in addition to any other headers on that was found in the message.

| Header               | Description                                                                              |
|----------------------|------------------------------------------------------------------------------------------|
| `Nats-Scheduler`     | The subject holding the schedule                                                         |
| `Nats-Schedule-Next` | Timestamp for next invocation for cron schedule messages or `purge` for delayed messages |
| `Nats-TTL`           | `5m` when `Nats-Schedule-TTL` is given                                                   |

The body of the message will simply be the provided body in the schedule.

Valid schedule header can match normal cron behavior as defined earlier

All time calculations will be done in UTC, a Cron schedule like `* 0 5 * * *` means exactly 5AM UTC.

## Stream Configuration

```go
type StreamConfig struct {
	// AllowMsgSchedules enables the feature
	AllowMsgSchedules bool          `json:"allow_msg_schedules"`
}
```

 * Setting this on a Source or Mirror should be denied
 * This feature can be enabled on existing streams but not disabled
 * A Stream with this feature on should require API level 2
 * `allow_msg_ttl` is needed if the user intends to use the `Nats-Schedule-TTL` feature
