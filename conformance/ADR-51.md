# ADR-51 Conformance Tests — JetStream Message Scheduler

This document describes the conformance tests that validate a server implementation of the **JetStream Message Scheduler** feature defined in [ADR-51](../adr/ADR-51.md).

A conformance harness implementing these tests should be able to run them against any NATS server build claiming support for message schedules (introduced at server 2.12.0 / API Level 2; revised at 2.14.0 for time zones, `Nats-Schedule-Rollup`, atomic stop semantics, schedule-source fallback, and stream retention interaction).

## How to read this document

Each test has the following shape:

- **ID** — stable identifier, used by the harness for reporting (`SCH-NNN`).
- **Title** — one-line summary.
- **References** — the section of ADR-51 the test derives from.
- **Preconditions** — required server features, stream configuration, and any prior state.
- **Steps** — the actions the harness takes, expressed at the protocol level (headers, subject patterns, message values).
- **Expected** — the observable behavior the harness asserts on. Includes pub ack structure, generated-message headers, stored stream state, and error responses.

A test passes only if every assertion in **Expected** holds. Where a test depends on another test's setup, that is called out in **Preconditions**.

## Common harness primitives

The harness needs the following building blocks. Implementations should provide them once and reuse them across tests.

- `new_stream(cfg)` — create a stream with the provided `StreamConfig`. Returns the stream name. Default config: `Subjects: ["schedules.>", "target.>"]`, `Storage: file`, `Replicas: 1`, `AllowMsgSchedules: true`, `AllowMsgTTL: true`, unless the test overrides these.
- `update_stream(name, cfg)` — apply a configuration update to an existing stream.
- `delete_stream(name)` — clean up.
- `publish_schedule(stream, subject, headers, payload=b"")` — performs a NATS *request* (not fire-and-forget) so the harness receives the server's reply. Sets the configured schedule headers verbatim. Returns the parsed pub ack or error response.
- `publish_raw(stream, subject, headers, payload)` — publishes an arbitrary message without injecting any scheduler headers. Used to exercise rejection paths and prepare source-subject state for sampling tests.
- `delete_msg(stream, seq)` — deletes a stream message by sequence (used for stopping schedules).
- `purge_subject(stream, subject)` — purges all messages on `subject`.
- `purge_subject_wildcard(stream, pattern)` — purges by wildcard subject pattern (used for SCH-704).
- `get_last_for(stream, subject)` — fetches the last message stored on `subject` in `stream`, returning headers and payload — used to assert on schedule replacement and on generated target messages.
- `wait_for_message_on(stream, subject, timeout)` — blocks until a message lands on `subject` in `stream` or the timeout fires. Returns the message (headers + payload).
- `wait_for_n_messages_on(stream, subject, n, timeout)` — blocks until `n` messages have landed on `subject` or the timeout fires. Returns the messages in order.
- `stream_msgs(stream)` — returns the messages currently stored in the stream, in order, with their headers — used to assert on the committed state of the stream.
- `stream_state(stream)` — returns last sequence, message count, and similar state.
- `now_utc()` — returns the harness's current UTC time as RFC3339, used to compose `@at` timestamps relative to test start.

The harness must use unique stream names per test so misconfigured streams from earlier tests do not leak. Schedule subjects within a test must also be unique unless a test specifically exercises schedule replacement.

## Wire-level reference

The harness asserts directly on these wire-level identifiers — they must match exactly.

Schedule-defining headers (set by the publisher of the schedule message):

- `Nats-Schedule` — schedule expression: `@at <RFC3339>`, `@every <duration>`, `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`, `@midnight`, `@hourly`, or a 6-field cron spec.
- `Nats-Schedule-Target` — subject the generated message will be delivered to. Must be a subject covered by the same stream.
- `Nats-Schedule-Source` — subject whose last message is read and republished to the target. Wildcards are not allowed.
- `Nats-Schedule-TTL` — TTL applied to the generated message via `Nats-TTL`.
- `Nats-Schedule-Time-Zone` — time zone applied to a cron schedule. Accepted values are exactly those Go's `time.LoadLocation` understands: an IANA Time Zone database name (e.g. `America/New_York`, `Europe/Amsterdam`, `Asia/Tokyo`), the literal `UTC`, the literal `Local`, or a time-zone abbreviation such as `EST` / `CET` when the server's host tzdata provides it (use of abbreviations is discouraged because they are ambiguous and DST-naive). Fixed UTC offsets (`+02:00`) are NOT accepted. Supplying the header with an empty value is also rejected — to default to UTC, omit the header. Not allowed on `@at` or `@every` schedules. Resolution happens against the server host's tzdata at runtime — if tzdata is missing or stale for the requested zone the schedule is rejected as an invalid pattern. Invalid time-zone values fail with the dedicated error `JSMessageSchedulesTimeZoneInvalidErr` (`10223`).
- `Nats-Schedule-Rollup` — only `sub` is accepted. Applies a `Nats-Rollup: sub` header to the generated message.
- `Nats-TTL` — bounds the lifetime of the schedule message itself (standard JetStream per-message TTL).

Headers the server adds to messages produced from a schedule (in addition to verbatim copies of any other headers on the schedule message):

- `Nats-Scheduler` — the subject of the schedule that produced the message.
- `Nats-Schedule-Next` — `purge` for single delayed messages, or an RFC3339 timestamp for the next firing of a cron / interval schedule.
- `Nats-TTL` — set when `Nats-Schedule-TTL` was set on the schedule.
- `Nats-Rollup` — set to `sub` when `Nats-Schedule-Rollup: sub` was set on the schedule.

Headers used to stop a schedule atomically with another publish:

- `Nats-Schedule-Next: purge` — together with `Nats-Scheduler: <schedule-subject>`, on a message published to a subject that is **not** the schedule subject itself, ends the named schedule and stores the published message in one operation.
- `Nats-Expected-Last-Subject-Sequence` and `Nats-Expected-Last-Subject-Sequence-Subject` — optional, used to make the atomic stop conditional on the schedule still existing.

Stream configuration field (added by this ADR):

- `allow_msg_schedules` (`AllowMsgSchedules`) — boolean. When `true`, the stream supports schedules. May be enabled on an existing stream but **not** disabled once enabled. Cannot be set on a stream with `Mirror` or `Sources` configured. Implicitly enables `AllowRollup` and clears `DenyPurge`. Requires API Level 2.

Server error codes:

The ADR enumerates one scheduler-specific error:

| ErrCode | Code | Meaning                                                                                                                                                                                                       |
|---------|------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 10212   | 400  | `Nats-Scheduler` is invalid in an atomic-stop publish — equals the publish subject itself, is empty, or is not a valid publish subject (`JSMessageSchedulesSchedulerInvalidErr`). The publish is not stored. |

Other rejections (invalid schedule pattern, target outside stream, header used on the wrong schedule type, …) reuse existing JetStream pub-ack and stream-config error codes. The harness asserts on:

- The presence of an `error` object in the pub ack (or stream API response) for rejection tests.
- The presence of a `description` field that meaningfully references the violated constraint.
- The presence of `err_code: 10212` specifically on `Nats-Scheduler` validation rejections.
- The absence of any side effects: no schedule stored, no generated target message produced.

Where a server reports a different specific `err_code` for a rejection, the harness records it so the suite can be tightened in a follow-up revision.

---

## SCH-100 — Stream configuration

### SCH-101 — Enabling `AllowMsgSchedules` works

- **References** — Stream Configuration.
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `AllowMsgSchedules: true`, `Subjects: ["schedules.>", "target.>"]`.
  2. Read back the stream configuration.
- **Expected**
  - Stream creation succeeds.
  - `AllowMsgSchedules` is reported as `true`.
  - `AllowRollup` is reported as `true` (implicitly enabled by `AllowMsgSchedules`).
  - `DenyPurge` is reported as `false` (cleared by `AllowMsgSchedules`).
  - The stream's reported API level is at least `2`.

### SCH-102 — `AllowMsgSchedules` defaults off

- **References** — Stream Configuration.
- **Preconditions** — None.
- **Steps**
  1. Create a stream without specifying `AllowMsgSchedules`.
  2. Publish a schedule message with `Nats-Schedule: @hourly`, `Nats-Schedule-Target: target.a`.
- **Expected**
  - Stream creation succeeds. `AllowMsgSchedules` is reported as `false` (or absent).
  - The schedule publish either fails with an error pub ack, or stores the message as ordinary content with **no** schedule semantics (no generated target message ever appears). The harness records which branch occurred.

### SCH-103 — Cannot disable `AllowMsgSchedules` once enabled

- **References** — Stream Configuration ("This feature can be enabled on existing streams but not disabled").
- **Preconditions** — Stream with `AllowMsgSchedules: true`.
- **Steps**
  1. Update the stream to set `AllowMsgSchedules: false`.
- **Expected**
  - The update is rejected with a stream-config error.
  - The stream still reports `AllowMsgSchedules: true`.

### SCH-104 — `AllowMsgSchedules` can be enabled on an existing stream

- **References** — Stream Configuration ("This feature can be enabled on existing streams").
- **Preconditions** — Stream created with `AllowMsgSchedules: false`, `Subjects: ["schedules.>", "target.>"]`, `AllowMsgTTL: true`.
- **Steps**
  1. Update the stream to set `AllowMsgSchedules: true`.
  2. Publish an `@at` schedule that fires within ~2 seconds, with `Nats-Schedule-Target: target.a`.
- **Expected**
  - Update succeeds. Stream reports `AllowMsgSchedules: true` and `AllowRollup: true`.
  - The generated target message is observed within a generous timeout (≤ 5s).

### SCH-105 — Mirrors cannot enable `AllowMsgSchedules`

- **References** — Stream Configuration ("Setting this on a Source or Mirror should be denied").
- **Preconditions** — A source stream `SRC` exists.
- **Steps**
  1. Attempt to create a mirror stream with `Mirror: {Name: SRC}` and `AllowMsgSchedules: true`.
- **Expected**
  - Stream creation fails with a stream-config error.

### SCH-106 — Sources cannot enable `AllowMsgSchedules`

- **References** — Stream Configuration ("Setting this on a Source or Mirror should be denied"); Stream Retention Interaction ("the `Interest` stream cannot set it because it has sources configured").
- **Preconditions** — A stream `SRC` exists.
- **Steps**
  1. Attempt to create a stream with `Sources: [{Name: SRC}]` and `AllowMsgSchedules: true`.
- **Expected**
  - Stream creation fails with a stream-config error.

### SCH-107 — `Nats-Schedule-TTL` requires `AllowMsgTTL` on the stream

- **References** — Stream Configuration ("If the user intends to use the `Nats-Schedule-TTL` feature, the `AllowMsgTTL` must be true for the stream").
- **Preconditions** — Stream with `AllowMsgSchedules: true` and `AllowMsgTTL: false`.
- **Steps**
  1. Publish a schedule message with `Nats-Schedule: @hourly`, `Nats-Schedule-Target: target.a`, `Nats-Schedule-TTL: 5m`.
- **Expected**
  - The publish is rejected with an error pub ack.
  - No schedule message is stored.

### SCH-108 — `AllowMsgSchedules` auto-applies `Nats-Rollup: sub` to schedule messages

- **References** — Stream Configuration ("Schedules are stored as rollup-subject messages: the server auto-applies `Nats-Rollup: sub` if the publisher did not set it").
- **Preconditions** — Stream with `AllowMsgSchedules: true`.
- **Steps**
  1. Publish a schedule (`Nats-Schedule: @hourly`, `Nats-Schedule-Target: target.a`) **without** setting `Nats-Rollup` on the schedule message.
  2. Read back the stored schedule message via `get_last_for`.
- **Expected**
  - The stored schedule message carries `Nats-Rollup: sub`.

---

## SCH-200 — Single delayed publish (`@at`)

### SCH-201 — `@at` in the near future fires once

- **References** — Single scheduled message.
- **Preconditions** — Default stream from harness primitives.
- **Steps**
  1. Compute `T = now_utc() + 2s`.
  2. Publish a schedule on `schedules.delayed.a` with `Nats-Schedule: @at <T>`, `Nats-Schedule-Target: target.delayed.a`, payload `body`.
  3. `wait_for_message_on(stream, "target.delayed.a", timeout=10s)`.
- **Expected**
  - A single message is observed on `target.delayed.a` close to `T` (within a generous server-driven slack — the harness asserts on presence, not exact firing time).
  - The generated message payload is `body`.
  - The generated message carries `Nats-Schedule-Next: purge` and `Nats-Scheduler: schedules.delayed.a`.
  - After firing, the schedule message on `schedules.delayed.a` has been removed (`get_last_for` returns nothing, or returns a message that is not the original schedule).

### SCH-202 — `@at` in the past fires immediately

- **References** — Single scheduled message ("If a message is made with a schedule in the past it is immediately sent").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule with `Nats-Schedule: @at 2009-11-10T23:00:00Z`, `Nats-Schedule-Target: target.past.a`.
  2. `wait_for_message_on(stream, "target.past.a", timeout=5s)`.
- **Expected**
  - A target message appears within a few seconds (server fires immediately for past schedules).
  - The generated message carries `Nats-Schedule-Next: purge`.

### SCH-203 — `@at` with non-UTC time zone is honored

- **References** — Single scheduled message ("The time format is RFC3339 and may include a timezone which the server will convert to UTC when received").
- **Preconditions** — Default stream.
- **Steps**
  1. Compute `T_local` = a timestamp ~2 seconds in the future expressed in a non-UTC offset (e.g. `+02:00`).
  2. Publish a schedule with `Nats-Schedule: @at <T_local>`, `Nats-Schedule-Target: target.tz.a`.
  3. `wait_for_message_on(stream, "target.tz.a", timeout=10s)`.
- **Expected**
  - The target message fires near `T_local` (the server converted the offset correctly). It MUST NOT fire 2+ hours late as it would if the offset were ignored.

### SCH-204 — `Nats-Schedule-TTL` on a single delayed message produces `Nats-TTL`

- **References** — Single scheduled message ("The generated message has a Message TTL of `5m`"); Headers.
- **Preconditions** — Default stream (with `AllowMsgTTL: true`).
- **Steps**
  1. Publish a schedule with `Nats-Schedule: @at <now+2s>`, `Nats-Schedule-Target: target.ttl.a`, `Nats-Schedule-TTL: 5m`.
  2. `wait_for_message_on(stream, "target.ttl.a", timeout=10s)`.
- **Expected**
  - The generated message carries `Nats-TTL: 5m`.
  - The schedule message itself does not carry the consumer-visible `Nats-Schedule-TTL` header propagated to the target (only `Nats-TTL` appears on the generated message).

### SCH-205 — Additional headers on the schedule are propagated verbatim

- **References** — Single scheduled message ("Additional headers added to the message will be sent to the target subject verbatim").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule on `schedules.hdr.a` with `Nats-Schedule: @at <now+2s>`, `Nats-Schedule-Target: target.hdr.a`, plus an arbitrary header `X-Custom: test-42`.
  2. `wait_for_message_on(stream, "target.hdr.a", timeout=10s)`.
- **Expected**
  - The generated message carries `X-Custom: test-42`.
  - It also carries `Nats-Scheduler: schedules.hdr.a` and `Nats-Schedule-Next: purge`.
  - It does **not** carry `Nats-Schedule` itself (schedule headers are stripped on the generated copy).

### SCH-206 — `Nats-TTL` on the schedule itself bounds the schedule lifetime

- **References** — Single scheduled message ("To avoid this, add a `Nats-TTL` header to the message so it will be removed after the TTL").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule with `Nats-Schedule: @at <T>` where `T = now + 60s`, `Nats-Schedule-Target: target.expire.a`, `Nats-TTL: 2s`.
  2. Wait 5 seconds.
- **Expected**
  - No message appears on `target.expire.a` (the schedule was removed by its own TTL before firing).
  - The schedule subject has been emptied.

---

## SCH-300 — Cron schedules

These tests use schedules that fire frequently so the harness can observe firings within a reasonable runtime. A 6-field cron of `* * * * * *` fires every second.

### SCH-301 — 6-field crontab fires every second

- **References** — Cron-like schedules; Schedule Format / 6 field crontab format.
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule on `schedules.cron.every_sec` with `Nats-Schedule: * * * * * *`, `Nats-Schedule-Target: target.cron.a`.
  2. `wait_for_n_messages_on(stream, "target.cron.a", n=3, timeout=10s)`.
- **Expected**
  - At least 3 messages observed on `target.cron.a` within the timeout.
  - Each generated message carries `Nats-Scheduler: schedules.cron.every_sec` and a `Nats-Schedule-Next` header whose value parses as an RFC3339 timestamp **after** the current message's delivery time.
  - The schedule message on `schedules.cron.every_sec` is still present after the firings (cron schedules do not self-delete).

### SCH-302 — `@hourly` predefined schedule is recognized

- **References** — Predefined Schedules.
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule with `Nats-Schedule: @hourly`, `Nats-Schedule-Target: target.hourly.a`. (No firing assertion — `@hourly` won't fire within a test.)
  2. Read back the schedule via `get_last_for`.
- **Expected**
  - The schedule is accepted and stored.
  - The harness can introspect server state (if exposed) or derive `Nats-Schedule-Next` from a subsequent server-side firing once an hour rolls over; for runtime purposes the test is satisfied by acceptance plus a parseable schedule.

### SCH-303 — Other predefined schedules are recognized

- **References** — Predefined Schedules.
- **Preconditions** — Default stream.
- **Steps**
  1. For each of `@yearly`, `@annually`, `@monthly`, `@weekly`, `@daily`, `@midnight`: publish a fresh schedule with that expression and a unique target.
- **Expected**
  - Each publish is accepted (no error pub ack).
  - Each schedule message is stored.

### SCH-304 — Cron schedule message persists across firings

- **References** — Cron-like schedules ("The original schedule message will remain and again produce a message the next hour").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a `* * * * * *` schedule on `schedules.cron.persist` targeting `target.persist.a`.
  2. After 3 firings (≥ 3s), read the schedule via `get_last_for(stream, "schedules.cron.persist")`.
- **Expected**
  - The schedule message is still present and unchanged (same payload, same `Nats-Schedule` header).

### SCH-305 — `@every` interval schedule fires repeatedly

- **References** — Intervals (`@every`).
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule on `schedules.every.1s` with `Nats-Schedule: @every 1s`, `Nats-Schedule-Target: target.every.a`.
  2. `wait_for_n_messages_on(stream, "target.every.a", n=3, timeout=10s)`.
- **Expected**
  - At least 3 generated messages observed.
  - Each message has `Nats-Schedule-Next` containing an RFC3339 timestamp.
  - The schedule message persists on `schedules.every.1s`.

### SCH-306 — Invalid cron expression rejected

- **References** — Schedule Format.
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule with `Nats-Schedule: not a valid cron`.
- **Expected**
  - Publish rejected with an error pub ack.
  - No schedule message is stored.

### SCH-307 — `Nats-Schedule-TTL` on a cron schedule produces `Nats-TTL` on each firing

- **References** — Cron-like schedules ("The generated message has a Message TTL of `5m`").
- **Preconditions** — Default stream (with `AllowMsgTTL: true`).
- **Steps**
  1. Publish a schedule on `schedules.cron.ttl` with `Nats-Schedule: * * * * * *`, `Nats-Schedule-Target: target.cron.ttl`, `Nats-Schedule-TTL: 1m`.
  2. `wait_for_n_messages_on(stream, "target.cron.ttl", n=2, timeout=5s)`.
- **Expected**
  - Each generated message carries `Nats-TTL: 1m`.

### SCH-308 — `Nats-TTL` on a cron schedule bounds total firings

- **References** — Cron-like schedules ("If the original schedule message has a `Nats-TTL` header the schedule will be removed after that time").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a `* * * * * *` schedule with `Nats-TTL: 3s` targeting `target.cron.bounded`.
  2. Wait 8 seconds.
- **Expected**
  - The number of generated messages is bounded by the TTL (the schedule stops producing after ~3s of life).
  - The schedule subject is empty after the TTL elapses.

### SCH-309 — `@every` minimum interval is `1s`

- **References** — Intervals (`@every`) ("The minimum supported interval is `1s`; shorter intervals are rejected").
- **Preconditions** — Default stream.
- **Steps** — for each value below, publish a schedule with `Nats-Schedule: @every <value>`, `Nats-Schedule-Target: target.every.tooShort`:
  1. `500ms`
  2. `100ms`
  3. `0s`
- **Expected**
  - Every publish receives an error pub ack referencing the minimum interval.
  - No schedule is stored on the schedule subject.
  - As a positive control, publishing `@every 1s` with the same target succeeds (the minimum is inclusive).

---

## SCH-400 — Subject sampling (`Nats-Schedule-Source`)

### SCH-401 — Source subject's last message is republished to the target

- **References** — Subject Sampling.
- **Preconditions** — Default stream covering `sensors.>`, `sampled.>`, `schedules.>`. `AllowMsgSchedules: true`.
- **Steps**
  1. `publish_raw` 5 messages to `sensors.cnc.temperature` with payloads `1`..`5`.
  2. Publish a schedule on `schedules.sample.cnc` with `Nats-Schedule: @every 1s`, `Nats-Schedule-Source: sensors.cnc.temperature`, `Nats-Schedule-Target: sampled.cnc.temperature`, payload `""` (empty).
  3. `wait_for_message_on(stream, "sampled.cnc.temperature", timeout=5s)`.
- **Expected**
  - The first generated message on `sampled.cnc.temperature` has payload `5` (the last message of the source subject at firing time).
  - It carries `Nats-Scheduler: schedules.sample.cnc` and a `Nats-Schedule-Next` timestamp.

### SCH-402 — Wildcards in `Nats-Schedule-Source` are rejected

- **References** — Subject Sampling header description ("Wildcards are not supported").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule with `Nats-Schedule: @every 1s`, `Nats-Schedule-Source: sensors.*`, `Nats-Schedule-Target: sampled.cnc.temperature`.
- **Expected**
  - Publish rejected with an error pub ack. No schedule stored.

### SCH-403 — Empty source falls back to the schedule's own body and headers

- **References** — Headers (`Nats-Schedule-Source`: "If no message exists on the source subject, the schedule's own body and headers is published as a fallback").
- **Preconditions** — Default stream. `sensors.empty.subject` has never received a message.
- **Steps**
  1. Publish a schedule on `schedules.sample.fallback` with `Nats-Schedule: @every 1s`, `Nats-Schedule-Source: sensors.empty.subject`, `Nats-Schedule-Target: sampled.fallback`, headers `X-Marker: schedule-body`, payload `fallback-body`.
  2. `wait_for_message_on(stream, "sampled.fallback", timeout=5s)`.
- **Expected**
  - The generated message on `sampled.fallback` has payload `fallback-body`.
  - It carries `X-Marker: schedule-body` (the schedule's own header), `Nats-Scheduler: schedules.sample.fallback`, and a `Nats-Schedule-Next` timestamp.

### SCH-404 — Source updates are reflected on subsequent firings

- **References** — Subject Sampling.
- **Preconditions** — As SCH-401, with the schedule already firing.
- **Steps**
  1. With a `@every 1s` source-sampling schedule active and previously sampling value `5`, publish (`publish_raw`) a new message `9` to the source subject.
  2. Wait for the next firing.
- **Expected**
  - The next generated target message has payload `9`.

---

## SCH-500 — Schedule-defining headers and validation

### SCH-501 — `Nats-Schedule-Time-Zone` applies to a cron schedule

- **References** — Cron-like schedules ("Cron schedules may use different time zones, if specified in the `Nats-Schedule-Time-Zone` header"). The accepted values are those Go's `time.LoadLocation` understands: IANA zone names (`America/New_York`, `Europe/Amsterdam`, …), `UTC`, `Local`, and abbreviations such as `EST` / `CET` when host tzdata provides them. Fixed offsets are not accepted.
- **Preconditions** — Default stream.
- **Steps**
  1. Probe: publish a schedule on `schedules.cron.tz.probe` with `Nats-Schedule-Time-Zone: UTC` and a valid 6-field cron. `UTC` always resolves regardless of host tzdata, so this confirms the header plumbing.
  2. Publish a schedule on `schedules.cron.tz` with `Nats-Schedule: 0 30 9 * * *`, `Nats-Schedule-Time-Zone: America/New_York`, `Nats-Schedule-Target: target.tz.cron`.
- **Expected**
  - The probe is accepted. If the probe fails, the test fails — header plumbing is broken.
  - The named-zone publish is accepted and stored with the supplied `Nats-Schedule-Time-Zone`. If it is rejected, the test **skips** (server tzdata is missing or stale for the requested zone — the server returns `10189` `JSMessageSchedulesPatternInvalidErr`, which it cannot distinguish from a malformed cron pattern). The harness does not assert on next-firing time within the run.

### SCH-502 — `Nats-Schedule-Time-Zone` is rejected on `@at` schedules

- **References** — Headers (`Nats-Schedule-Time-Zone`: "Not allowed to be used if the schedule is not a Cron schedule").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule with `Nats-Schedule: @at 2099-01-01T00:00:00Z`, `Nats-Schedule-Time-Zone: Europe/Amsterdam`, `Nats-Schedule-Target: target.tz.at`.
- **Expected**
  - Publish rejected with an error pub ack.
  - No schedule stored.

### SCH-503 — `Nats-Schedule-Time-Zone` is rejected on `@every` schedules

- **References** — Headers (`Nats-Schedule-Time-Zone`: "Not allowed to be used if the schedule is not a Cron schedule").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule with `Nats-Schedule: @every 1m`, `Nats-Schedule-Time-Zone: Europe/Amsterdam`, `Nats-Schedule-Target: target.tz.every`.
- **Expected**
  - Publish rejected with an error pub ack.

### SCH-504 — Invalid `Nats-Schedule-Time-Zone` value rejected

- **References** — Cron-like schedules; Headers (accepted forms are exactly those Go's `time.LoadLocation` understands; fixed offsets are not).
- **Preconditions** — Default stream.
- **Steps** — for each value below, publish a cron schedule (`Nats-Schedule: * * * * * *`) with `Nats-Schedule-Time-Zone` set to the value:
  1. `Not/A_Zone` (nonsense IANA-shaped name — `time.LoadLocation` returns an error)
  2. `+02:00` (fixed UTC offset — `time.LoadLocation` does not accept fixed offsets)
- **Expected**
  - Every publish is rejected with `JSMessageSchedulesTimeZoneInvalidErr` (`10223`).
  - No schedule is stored on the schedule subject for any of the rejected values.

### SCH-505 — `Nats-Schedule-Rollup: sub` produces `Nats-Rollup: sub` on the generated message

- **References** — Headers (`Nats-Schedule-Rollup`).
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule with `Nats-Schedule: @at <now+2s>`, `Nats-Schedule-Target: target.rollup.a`, `Nats-Schedule-Rollup: sub`.
  2. `wait_for_message_on(stream, "target.rollup.a", timeout=10s)`.
- **Expected**
  - The generated message carries `Nats-Rollup: sub`.

### SCH-506 — `Nats-Schedule-Rollup` with a value other than `sub` is rejected

- **References** — Headers (`Nats-Schedule-Rollup` description: "only `sub` is a valid value").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule with `Nats-Schedule-Rollup: all`, otherwise valid.
- **Expected**
  - Publish rejected with an error pub ack.

### SCH-507 — `Nats-Schedule-Target` outside the stream's subjects is rejected

- **References** — Single scheduled message ("the `Nats-Schedule-Target` must be a subject in the same stream").
- **Preconditions** — Stream covering only `schedules.>` and `target.>`.
- **Steps**
  1. Publish a schedule with `Nats-Schedule-Target: nope.elsewhere` (not covered by stream subjects).
- **Expected**
  - Publish rejected with an error pub ack. No schedule is stored.

### SCH-508 — Schedule headers are stripped from the generated message

- **References** — Single scheduled message ("Additional headers added to the message will be sent to the target subject verbatim"), implicit: schedule-control headers are not "additional".
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule with `Nats-Schedule: @at <now+2s>`, `Nats-Schedule-Target: target.strip.a`, `Nats-Schedule-TTL: 1m`, `Nats-Schedule-Rollup: sub`, plus a custom `X-Keep: yes`.
  2. `wait_for_message_on(stream, "target.strip.a", timeout=10s)`.
- **Expected**
  - The generated message does **not** carry `Nats-Schedule`, `Nats-Schedule-Target`, `Nats-Schedule-TTL`, or `Nats-Schedule-Rollup`.
  - It does carry `Nats-Scheduler`, `Nats-Schedule-Next: purge`, `Nats-TTL: 1m`, `Nats-Rollup: sub`, and `X-Keep: yes`.

### SCH-509 — Empty `Nats-Schedule-Time-Zone` is rejected (omit the header for UTC)

- **References** — Cron-like schedules ("If not specified, the Cron schedule will be in UTC"). The server distinguishes "header omitted" (treated as UTC) from "header present with an empty value" (rejected). This pins that distinction so a future server change cannot silently start accepting an empty value as UTC, which would mask client bugs that produce empty headers.
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a cron schedule on `schedules.tz.empty` with `Nats-Schedule: * * * * * *`, `Nats-Schedule-Time-Zone:` (empty value), and `Nats-Schedule-Target: target.tz.empty`.
- **Expected**
  - The publish is rejected with an error pub ack carrying `JSMessageSchedulesTimeZoneInvalidErr` (`10223`).
  - No schedule is stored on `schedules.tz.empty`.

---

## SCH-600 — Schedule replacement and stopping

### SCH-601 — Publishing a new schedule on an existing schedule subject replaces it

- **References** — Stream Configuration ("Publishing a new schedule to an existing schedule subject replaces the prior one"). Schedules are stored as rollup-subject messages.
- **Preconditions** — Default stream.
- **Steps**
  1. Publish schedule A on `schedules.replace.a` with `Nats-Schedule: @every 1s`, target `target.replace.a`, payload `A`.
  2. After observing one firing, publish schedule B on the same subject with `Nats-Schedule: @every 1s`, target `target.replace.a`, payload `B`.
  3. `wait_for_message_on(stream, "target.replace.a", timeout=5s)` for the next firing after step 2.
- **Expected**
  - After step 2, only the new schedule message exists on `schedules.replace.a` (rollup behaviour); reading via `get_last_for` returns payload `B`.
  - Subsequent firings deliver payload `B` on `target.replace.a`.

### SCH-602 — Deleting the schedule message by sequence stops firings

- **References** — Ending/stopping schedules early ("Deleting the schedule message directly by its stream sequence").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a `@every 1s` schedule on `schedules.stop.delete` and observe one firing; capture its stream sequence `S`.
  2. `delete_msg(stream, S)`.
  3. Wait 4 seconds; count messages on the target after step 2.
- **Expected**
  - Generated target messages stop appearing after the delete (allow at most one in-flight extra firing).

### SCH-603 — Purging the schedule subject stops firings

- **References** — Ending/stopping schedules early ("Purging one schedule by its schedule subject").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a `@every 1s` schedule on `schedules.stop.purge` targeting `target.stop.purge` and observe one firing.
  2. `purge_subject(stream, "schedules.stop.purge")`.
  3. Wait 4 seconds.
- **Expected**
  - No further firings on `target.stop.purge`.
  - `schedules.stop.purge` is empty.

### SCH-604 — Purging by wildcard stops multiple schedules

- **References** — Ending/stopping schedules early ("Purging one or more schedules by using a purge subject with wildcards that can match multiple schedule subjects").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish three `@every 1s` schedules on `schedules.stop.multi.a`, `.b`, `.c`.
  2. After at least one firing of each, `purge_subject_wildcard(stream, "schedules.stop.multi.>")`.
  3. Wait 4 seconds.
- **Expected**
  - All three schedules cease firing.
  - All matching schedule subjects are empty.

### SCH-605 — Atomic stop publishes to a different subject and removes the schedule

- **References** — Ending/stopping schedules early ("Alternatively, but for more advanced use cases, a schedule can be stopped only after a message on a different subject is persisted").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a single delayed schedule on `schedules.cancel.delayed` with `Nats-Schedule: @at <now+30s>`, `Nats-Schedule-Target: target.cancel.delayed`, payload `delayed-body`.
  2. Publish a message to `schedules.cancel.canceled` (a *different* subject covered by the stream) with headers `Nats-Schedule-Next: purge`, `Nats-Scheduler: schedules.cancel.delayed`, payload `cancel-body`.
  3. Wait 35 seconds.
- **Expected**
  - The cancel message is stored on `schedules.cancel.canceled` (count of stored messages on that subject increases by 1).
  - The schedule on `schedules.cancel.delayed` is gone (no message at that subject).
  - No firing ever occurs on `target.cancel.delayed`.

### SCH-606 — Atomic stop conditional on schedule still existing

- **References** — Ending/stopping schedules early (use of `Nats-Expected-Last-Subject-Sequence` and `Nats-Expected-Last-Subject-Sequence-Subject`).
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a `@every 1s` schedule on `schedules.cancel.cond` targeting `target.cancel.cond`. Capture its stream sequence `S` and the schedule subject.
  2. Publish a message to `schedules.cancel.cond_signal` with `Nats-Schedule-Next: purge`, `Nats-Scheduler: schedules.cancel.cond`, `Nats-Expected-Last-Subject-Sequence: S`, `Nats-Expected-Last-Subject-Sequence-Subject: schedules.cancel.cond`.
  3. Now repeat step 2 with the same headers (but the schedule has already been purged).
- **Expected**
  - Step 2 succeeds, the signal message is stored, and the schedule is removed; no further firings occur.
  - Step 3 fails (schedule no longer exists at the expected subject sequence) with an error pub ack and the signal message is **not** stored a second time.

### SCH-607 — Atomic stop publish subject MUST NOT be the schedule's own subject

- **References** — Ending/stopping schedules early ("The selected subject in `Nats-Scheduler` can NOT equal the publish subject itself, as this would mean this message would be purged as well due to `Nats-Schedule-Next: purge`"). Server enforces with `JSMessageSchedulesSchedulerInvalidErr` (10212).
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a `@every 1s` schedule on `schedules.self.cancel`.
  2. Publish a message to `schedules.self.cancel` (same subject) with `Nats-Schedule-Next: purge`, `Nats-Scheduler: schedules.self.cancel`.
- **Expected**
  - **Expected**: server rejects the publish with an error pub ack carrying `err_code: 10212`. The schedule was not canceled and **continues firing** — that continuation is the correct outcome, not a violation. The harness asserts on rejection plus the specific `err_code`, not on subsequent firing behavior.
  - **Inconclusive / fail**: server accepts the publish despite the ADR's explicit prohibition. The harness then verifies the schedule actually stopped firing (if not, that's a hard failure: the cancel was accepted with no effect). If the schedule did stop, the result is `inconclusive` and the spec is the source of truth.

### SCH-610 — Empty `Nats-Scheduler` is rejected with `10212`

- **References** — Ending/stopping schedules early ("The same error is returned when `Nats-Scheduler` is empty or is not a valid publish subject").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a message to a stream-covered subject (e.g. `schedules.cancel.empty`) with `Nats-Schedule-Next: purge` and `Nats-Scheduler:` (empty value).
- **Expected**
  - The publish is rejected with `err_code: 10212`.
  - The message is not stored.

### SCH-611 — Invalid `Nats-Scheduler` subject is rejected with `10212`

- **References** — Ending/stopping schedules early ("The same error is returned when `Nats-Scheduler` is empty or is not a valid publish subject").
- **Preconditions** — Default stream.
- **Steps** — for each value below, publish to a stream-covered subject (e.g. `schedules.cancel.bad`) with `Nats-Schedule-Next: purge` and `Nats-Scheduler` set to the value:
  1. ` ` (whitespace-only)
  2. `bad subject with spaces`
  3. `*.>` (wildcard — not a valid publish subject)
  4. `.leading.dot`
- **Expected**
  - Every publish is rejected with `err_code: 10212`.
  - No publish is stored.

### SCH-608 — Cancel-publish to target subject delivers and stops the schedule

- **References** — Ending/stopping schedules early (target-subject example: "to publish the delayed message earlier than the schedule would").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a schedule on `schedules.early.delayed` with `Nats-Schedule: @at <now+30s>`, `Nats-Schedule-Target: target.early.delayed`, payload `slow-body`.
  2. Publish a message directly to `target.early.delayed` with `Nats-Schedule-Next: purge`, `Nats-Scheduler: schedules.early.delayed`, payload `fast-body`.
  3. Wait 35 seconds.
- **Expected**
  - `target.early.delayed` contains exactly one message with payload `fast-body` (the schedule did not double-publish).
  - The schedule on `schedules.early.delayed` has been removed.

### SCH-609 — Single delayed message auto-stops its schedule after firing

- **References** — Ending/stopping schedules early ("This is also used by single delayed scheduled messages to automatically stop the schedule after the delayed message is published").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a single `@at` schedule and wait for the firing (as in SCH-201).
- **Expected**
  - After firing, the schedule message has been removed automatically (no manual cancel needed).

---

## SCH-700 — Stream retention interaction

These tests cover the table in [Stream Retention Interaction](../adr/ADR-51.md#stream-retention-interaction).

### SCH-701 — `Limits` retention works as documented

- **References** — Stream Retention Interaction (`Limits` row).
- **Preconditions** — Default stream with `Retention: limits`.
- **Steps**
  1. Run a 3-firing `@every 1s` schedule.
- **Expected**
  - 3 firings observed; schedule still present.

### SCH-702 — `WorkQueue` retention: schedule fires when no consumer is filtered on the schedule subject

- **References** — Stream Retention Interaction (`WorkQueue` row, "Works, provided no consumer acknowledges the message before the schedule fires").
- **Preconditions** — Stream with `AllowMsgSchedules: true`, `Retention: workqueue`. No consumers configured.
- **Steps**
  1. Publish a `@every 1s` schedule on `schedules.wq.simple` targeting `target.wq.simple`.
  2. Wait for 3 firings.
- **Expected**
  - At least 3 firings observed.
  - Schedule message still present on `schedules.wq.simple`.

### SCH-703 — `WorkQueue` retention: consumer acknowledging the schedule subject removes the schedule

- **References** — Stream Retention Interaction (`WorkQueue` row, "Consumer removes the schedule if acknowledged").
- **Preconditions** — Stream with `Retention: workqueue`, `AllowMsgSchedules: true`.
- **Steps**
  1. Publish a `@every 5s` schedule on `schedules.wq.acked` targeting `target.wq.acked`.
  2. Create a durable consumer with `FilterSubject: schedules.wq.acked`, `AckPolicy: explicit`.
  3. Receive the schedule message and ack it.
  4. Wait 8 seconds.
- **Expected**
  - The schedule no longer fires (or fires at most one already-in-flight time before stopping).
  - The schedule subject is empty after the consumer's ack is processed.

### SCH-704 — `Interest` retention: schedule held by a pinning consumer fires

- **References** — Stream Retention Interaction (`Interest` row); option 1 ("Pinning consumer on the Interest stream").
- **Preconditions** — Stream with `Retention: interest`, `AllowMsgSchedules: true`. **Two** pinning consumers, both with `AckPolicy: none`:
  - `FilterSubject: schedules.>` — keeps the schedule message itself, per option 1.
  - `FilterSubject: target.>` — keeps the generated firings long enough for the harness to observe them. On `Interest` retention every retained subject needs its own interest; the schedule subject's interest does not extend to the target subject.
- **Steps**
  1. Publish a `@every 1s` schedule on `schedules.interest.pinned` targeting `target.interest.pinned`.
  2. Wait for 3 firings.
- **Expected**
  - At least 3 firings observed on `target.interest.pinned`.
  - Schedule still present on `schedules.interest.pinned`.

### SCH-705 — `Interest` retention without a consumer does not store the schedule

- **References** — Stream Retention Interaction (`Interest` row, "if no consumer has interest in the schedule subject, the schedule will not be stored, nor will it trigger scheduled messages").
- **Preconditions** — Stream with `Retention: interest`, `AllowMsgSchedules: true`. **No** consumers configured.
- **Steps**
  1. Publish a `@every 1s` schedule on `schedules.interest.unheld`.
  2. Wait 3 seconds. Inspect the stream.
- **Expected**
  - `schedules.interest.unheld` has zero stored messages.
  - No firings occur on the target subject.

### SCH-706 — `MaxAge` shorter than the firing interval removes the schedule before it fires

- **References** — Stream Retention Interaction (`Stream limits on schedule lifetime` row, "`MaxAge` shorter than the firing interval deletes the schedule before it fires").
- **Preconditions** — Stream with `AllowMsgSchedules: true`, `MaxAge: 2s`.
- **Steps**
  1. Publish a `@every 10s` schedule.
  2. Wait 5 seconds.
- **Expected**
  - The schedule subject is empty (`MaxAge` removed it before firing).
  - No target message ever appeared.

### SCH-707 — Two-stream composition: WorkQueue source + Interest dest

- **References** — Stream Retention Interaction (option 2, "Separate WorkQueue source stream").
- **Preconditions** —
  - Stream `WQ` with `Retention: workqueue`, `AllowMsgSchedules: true`, `Subjects: ["schedules.>", "target.>"]`.
  - Stream `INT` with `Retention: interest`, `Sources: [{Name: WQ, FilterSubject: "target.>"}]`. `AllowMsgSchedules` MUST be unset (it cannot be set on a stream with sources).
  - A consumer on `INT` filtered on `target.>`.
- **Steps**
  1. Publish a `@every 1s` schedule on `schedules.composed` targeting `target.composed` (in `WQ`).
  2. Wait for 3 firings to flow through to `INT`.
- **Expected**
  - `WQ` retains the schedule and produces target messages.
  - `INT` receives the target messages via sourcing.
  - The consumer on `INT` can deliver them.
  - No fast-batch / no scheduler reconstruction occurs on `INT`.

---

## SCH-800 — Time zones and DST

### SCH-801 — Default cron timing is UTC

- **References** — Cron-like schedules ("Execution times will be in UTC regardless of server local time zone"); Headers ("All time calculations will be done in UTC").
- **Preconditions** — Default stream.
- **Steps**
  1. Publish a cron schedule whose 6-field expression specifies a particular UTC second within the next ~5 seconds, without `Nats-Schedule-Time-Zone`.
  2. Wait for the firing.
- **Expected**
  - The firing occurs near the specified UTC time, regardless of the server's local time zone.

### SCH-802 — Specifying a valid time zone shifts firing accordingly (informational)

- **References** — Cron-like schedules (time zone support).
- **Preconditions** — Default stream. Server tzdata up to date (skip otherwise).
- **Steps**
  1. Publish two equivalent cron schedules (e.g. `0 30 9 * * *`), one without a time zone (UTC), one with `Nats-Schedule-Time-Zone: America/New_York`.
  2. If the server exposes the next-fire time via API, compare the two.
- **Expected**
  - The two next-fire times differ by the New_York-vs-UTC offset for the relevant date (with DST taken into account).
  - This test is **inconclusive** if the server does not expose next-fire times to the harness; record the observed surface.

### SCH-803 — DST forward-skip / backward-repeat behaviour (informational)

- **References** — Cron-like schedules ("it's not recommended to use Cron schedules that trigger during daylight saving time (DST) changes").
- **Preconditions** — None.
- **Steps**
  - The harness does not actively wait through a DST transition. The conformance suite records this as **out of scope for runtime**; documentation conformance is asserted by ensuring the ADR's recommendation is reflected in client docs.
- **Expected**
  - No runtime assertion; this entry is a placeholder so server implementers and client authors can extend the test suite when a DST simulation harness exists.

---

## Out of scope

The following ADR-51 areas are intentionally **not** covered by this conformance document:

- DST transition correctness (skipped or repeated firings during forward/backward DST shifts) — see SCH-803.
- Long-horizon scheduling (`@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`) firing accuracy at the exact wall-clock instant. The harness asserts on schedule acceptance and storage; production-style timing accuracy is left to integration testing.
- Performance / scaling: the maximum number of schedules per stream, the cost of evaluating many schedules concurrently, and replicated-stream replication of schedule state under load.
- Cross-cluster topology / super-cluster propagation. Schedules execute within the cluster holding the stream; conformance does not exercise multi-cluster routing.
- Client-library ergonomics for setting up schedules. Conformance is asserted at the protocol layer (raw headers and pub-ack shape) so any client is testable.

## Implementation notes for the harness

- **Determinism over speed** — schedule firings are server-driven. Use generous timeouts (e.g. 5–10s when the schedule fires every second; 10–35s for `@at` firings ~30s out). Where a test waits for absence of a firing (e.g. SCH-602, SCH-705, SCH-706), wait for a clearly longer interval than the schedule period and tolerate at most one already-in-flight firing for purge-style stops.
- **Subject layout** — every test uses unique schedule subjects (`schedules.<test-id>.<key>`) and target subjects (`target.<test-id>.<key>`) so concurrent runs do not interfere. A single base stream covering `schedules.>` and `target.>` is enough for most tests; retention-specific tests use freshly-named streams.
- **Cleanup** — every test deletes its stream(s) on completion to release server-side state. Before deletion, the harness may want to purge schedule subjects so any in-flight firings do not produce surprising messages on a successor stream.
- **Reporting** — per-test result is `pass`, `fail`, `skip` (e.g. server below required version, missing tzdata), or `inconclusive` (e.g. SCH-102, SCH-607, SCH-802 where multiple acceptable behaviours are allowed — record the observed branch).
- **Server version gating** — the scheduler is introduced at 2.12.0 / API Level 2; revisions land at 2.14.0 (time-zone, `Nats-Schedule-Rollup`, atomic stop semantics, schedule-source fallback, retention interaction). Tests touching 2.14-only behaviour (SCH-403, SCH-501–504, SCH-505–506, SCH-605–608, SCH-701–707) skip with reason on older builds.
- **Time zones** — tests using `Nats-Schedule-Time-Zone` rely on the server having current tzdata. The harness should detect missing or stale tzdata and skip those tests with a clear reason.
- **Stream subject coverage** — schedules are stored on the stream as messages on the schedule subject and produce messages on the target subject; both subject patterns must be covered by the stream's `Subjects` configuration. The default harness stream covers both via `schedules.>` and `target.>`. Tests that intentionally violate this constraint (SCH-507) override the default.
- **Rollup interaction** — schedules are stored as rollup-subject messages. Tests asserting on schedule presence/absence read the schedule subject via `get_last_for`, which returns the most recent (and only) schedule on that subject, not the historical sequence.
