# ADR-49 Conformance Tests — JetStream Distributed Counter CRDT

This document describes the conformance tests that validate a server implementation of the **JetStream Distributed Counter CRDT** feature defined in [ADR-49](../adr/ADR-49.md).

A conformance harness implementing these tests should be able to run them against any NATS server build claiming support for distributed counters (introduced at server 2.12.0 / API Level 2).

## How to read this document

Each test has the following shape:

- **ID** — stable identifier, used by the harness for reporting (`CTR-NNN`).
- **Title** — one-line summary.
- **References** — the section of ADR-49 the test derives from.
- **Preconditions** — required server features, stream configuration, and any prior state.
- **Steps** — the actions the harness takes, expressed at the protocol level (headers, subject patterns, message values).
- **Expected** — the observable behavior the harness asserts on. Includes pub ack structure, error codes, stored stream state, and headers.

A test passes only if every assertion in **Expected** holds. Where a test depends on another test's setup, that is called out in **Preconditions**.

## Common harness primitives

The harness needs the following building blocks. Implementations should provide them once and reuse them across tests.

- `new_stream(cfg)` — create a stream with the provided `StreamConfig`. Returns the stream name. Default config: `Subjects: ["counter.>"]`, `Storage: file`, `Replicas: 1`, unless the test overrides these.
- `update_stream(name, cfg)` — apply a configuration update to an existing stream.
- `delete_stream(name)` — clean up.
- `publish_increment(stream, subject, incr, headers={})` — performs a NATS *request* (not a fire-and-forget publish) so the harness receives the server's reply. Sets the `Nats-Incr` header to the supplied value (e.g. `+1`, `-10`, `0`). Any extra headers are merged in. Returns the parsed pub ack or error response.
- `publish_raw(stream, subject, headers, payload)` — publishes an arbitrary message (no `Nats-Incr` injection) so tests can exercise rejection paths and out-of-band counter messages.
- `get_last_for(stream, subject)` — fetches the last message stored on `subject` in `stream`, returning headers and payload — used to assert the post-increment counter state.
- `purge_subject(stream, subject, keep=0)` — purges the given subject, optionally keeping the most recent `keep` messages. Used for reset tests.
- `stream_msgs(stream)` — returns the messages currently stored in the stream, in order, with their headers — used to assert on the committed counter history and source tracking metadata.
- `stream_state(stream)` — returns last sequence, message count, and similar state.
- `wait_for_source(dst_stream, expected_subject, timeout)` — blocks until a sourced/mirrored message for `expected_subject` lands in `dst_stream` or the timeout fires.

The harness must use unique stream names per test so that misconfigured streams from earlier tests do not leak.

## Wire-level reference

The harness asserts directly on these wire-level identifiers — they must match exactly.

Headers (request side):

- `Nats-Incr` — counter delta. Must match `^[+-]\d+$`. May be any valid `BigInt` value, including `0`.

Headers the server adds / preserves:

- `Nats-Incr` — preserved from the incoming message for audit and recount.
- `Nats-Counter-Sources` — JSON object mapping `<source_stream>` → `<source_subject>` → most-recently-seen `"val"`. Maintained by the server when sourcing counter messages from other streams.
- `Nats-Stream-Source` — present on messages received via stream sourcing; the server uses its presence to trigger the source-aware delta calculation.

Headers that the server MUST reject when accompanied by `Nats-Incr` on a counter stream:

- `Nats-Rollup`
- `Nats-Expected-Last-Sequence`
- `Nats-Expected-Subject-Last-Sequence`
- `Nats-Expected-Stream`
- `Nats-Expected-Last-Msg-Id`

Stored payload format:

```json
{"val":"<decimal string, arbitrary precision>"}
```

Pub ack fields (added by this ADR):

- `val` — string. The counter total **after** the increment was applied. Omitted when the response is for a non-counter publish.

Stream configuration field (added by this ADR):

- `allow_msg_counter` (`AllowMsgCounter`) — boolean. When `true`, the stream becomes a counter stream and may only contain counter messages. The setting can only be configured at creation time and is read-only thereafter (a stream-config update that changes the value MUST be rejected).

Counter-stream configuration constraints (assert via stream-create / stream-update error):

- `mirror` set with `allow_msg_counter: true` MUST be rejected.
- Any retention other than `limits` combined with `allow_msg_counter: true` MUST be rejected.
- `discard: new` combined with `allow_msg_counter: true` MUST be rejected.
- Per-message TTL (`allow_msg_ttl: true` or any TTL configuration) combined with `allow_msg_counter: true` MUST be rejected.
- A counter stream's reported API level MUST be at least `2`.

Server error codes (counter-specific):

The ADR does not enumerate dedicated error codes for counter rejections. Implementations MAY reuse existing JetStream pub ack error codes (e.g. the generic header / configuration / API-level rejection codes). The harness asserts on:

- The presence of an `error` object in the pub ack (pub fails).
- The presence of a `description` field that meaningfully references the violated constraint.
- The absence of a stored message in the stream for any rejected publish.

Where a server reports a specific `err_code`, the harness records it so the suite can be tightened in a follow-up revision of this document.

---

## CTR-100 — Stream configuration

### CTR-101 — Enabling `AllowMsgCounter` works

- **References** — Stream Configuration.
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `AllowMsgCounter: true`, `Retention: limits`, `Subjects: ["counter.>"]`.
  2. Read back the stream configuration.
- **Expected**
  - Stream creation succeeds.
  - `AllowMsgCounter` is reported as `true`.
  - The stream's reported API level is at least `2`.

### CTR-102 — `AllowMsgCounter` defaults off

- **References** — Stream Configuration; Design and Behavior ("When a message with the header is published to a Stream without the option set the message is rejected with an error").
- **Preconditions** — None.
- **Steps**
  1. Create a stream without specifying `AllowMsgCounter`.
  2. Publish a message to a subject in the stream with header `Nats-Incr: +1`.
- **Expected**
  - Stream creation succeeds; `AllowMsgCounter` is reported as `false`.
  - The publish receives an error pub ack (counter not enabled).
  - The stream contains zero messages.

### CTR-103 — `AllowMsgCounter` cannot be enabled on an existing stream

- **References** — Stream Configuration ("This feature can only be enabled during creation, it is read only once the stream exist").
- **Preconditions** — Stream `S` created with `AllowMsgCounter: false`.
- **Steps**
  1. Update `S` to set `AllowMsgCounter: true`.
  2. Read back the stream configuration.
  3. Publish `Nats-Incr: +5` to `counter.hits`.
- **Expected**
  - Update fails with a stream-config error referencing the read-only counter setting.
  - `AllowMsgCounter` remains `false` after the rejected update.
  - The increment publish receives an error pub ack (the stream is still a non-counter stream — see CTR-207).

### CTR-104 — `AllowMsgCounter` cannot be disabled once enabled

- **References** — Stream Configuration ("This feature can only be enabled during creation, it is read only once the stream exist").
- **Preconditions** — Stream created with `AllowMsgCounter: true`.
- **Steps**
  1. Update the stream to set `AllowMsgCounter: false`.
- **Expected**
  - Update fails with an error referencing the read-only counter setting.
  - The stream's `AllowMsgCounter` remains `true`.

### CTR-105 — `AllowMsgCounter` rejected on Mirror

- **References** — Stream Configuration ("Setting this on a Mirror should cause an error").
- **Preconditions** — A source stream `SRC` exists.
- **Steps**
  1. Attempt to create a stream with `Mirror: {Name: SRC}` and `AllowMsgCounter: true`.
- **Expected**
  - Stream creation fails.

### CTR-106 — `AllowMsgCounter` rejected with non-Limits retention

- **References** — Stream Configuration ("Setting this on a stream with anything but Limits retention should cause an error").
- **Preconditions** — None.
- **Steps**
  1. Attempt to create a stream with `AllowMsgCounter: true` and `Retention: workqueue`.
  2. Attempt to create a stream with `AllowMsgCounter: true` and `Retention: interest`.
- **Expected**
  - Both creations fail.

### CTR-107 — `AllowMsgCounter` rejected with `Discard: new`

- **References** — Stream Configuration ("Stream should not support Discard New with this setting set").
- **Preconditions** — None.
- **Steps**
  1. Attempt to create a stream with `AllowMsgCounter: true` and `Discard: new`.
- **Expected**
  - Stream creation fails.

### CTR-108 — `AllowMsgCounter` rejected with per-message TTLs

- **References** — Stream Configuration ("This setting may not be enabled along with Per Message TTLs").
- **Preconditions** — None.
- **Steps**
  1. Attempt to create a stream with `AllowMsgCounter: true` and `AllowMsgTTL: true`.
- **Expected**
  - Stream creation fails.

### CTR-109 — Counter stream may have Sources

- **References** — Stream Configuration; Source-based Replicated Counters.
- **Preconditions** — A counter stream `SRC` exists with `AllowMsgCounter: true`.
- **Steps**
  1. Create a stream `DST` with `AllowMsgCounter: true` and `Sources: [{Name: SRC}]`.
- **Expected**
  - Stream creation succeeds.

---

## CTR-200 — Increment validation

### CTR-201 — `Nats-Incr` is required on a counter stream

- **References** — Design and Behavior ("A Stream with the option set will reject all messages without `Nats-Incr`").
- **Preconditions** — Counter stream.
- **Steps**
  1. Publish a message to `counter.hits` with no headers.
  2. Publish a message to `counter.hits` with arbitrary headers but no `Nats-Incr`.
- **Expected**
  - Both publishes receive an error pub ack.
  - The stream contains zero messages.

### CTR-202 — Valid positive increment

- **References** — Design and Behavior; Solution Overview.
- **Preconditions** — Counter stream; subject `counter.hits` empty.
- **Steps**
  1. Publish `Nats-Incr: +1` to `counter.hits`.
  2. Publish `Nats-Incr: +99` to `counter.hits`.
- **Expected**
  - First publish: pub ack `val == "1"`. Stored payload `{"val":"1"}`. Stored `Nats-Incr` header preserved as `+1`.
  - Second publish: pub ack `val == "100"`. Stored payload `{"val":"100"}`. Stored `Nats-Incr` header preserved as `+99`.

### CTR-203 — Valid negative increment

- **References** — Design and Behavior.
- **Preconditions** — Counter stream; subject `counter.hits` already at value `100` (run CTR-202 setup first or seed via two publishes).
- **Steps**
  1. Publish `Nats-Incr: -10` to `counter.hits`.
  2. Publish `Nats-Incr: -100` to `counter.hits`.
- **Expected**
  - First publish: pub ack `val == "90"`. Stored payload `{"val":"90"}`.
  - Second publish: pub ack `val == "-10"`. Stored payload `{"val":"-10"}` (counter goes negative cleanly).

### CTR-204 — `Nats-Incr: 0` is valid

- **References** — Design and Behavior ("A value of `0` is valid").
- **Preconditions** — Counter stream; subject `counter.hits` already at value `5`.
- **Steps**
  1. Publish `Nats-Incr: 0` (no sign) to `counter.hits`.
  2. Publish `Nats-Incr: +0` to `counter.hits`.
  3. Publish `Nats-Incr: -0` to `counter.hits`.
- **Expected**
  - The harness records which signed-zero forms are accepted. ADR specifies `^[+-]\d+$`, so unsigned `0` MAY be rejected; `+0` and `-0` MUST be accepted.
  - For every accepted publish, pub ack `val == "5"` (no change). Subject value remains `5`.

### CTR-205 — `Nats-Incr` malformed values are rejected

- **References** — Design and Behavior ("if the value fails to parse the message is rejected with an error", `^[+-]\d+$`).
- **Preconditions** — Counter stream; subject `counter.hits` empty.
- **Steps** — for each value below, publish to `counter.hits`:
  1. `1` (no sign — does not satisfy `^[+-]\d+$`).
  2. `+`
  3. `-`
  4. `++1`
  5. `+1.5`
  6. `+1e3`
  7. `abc`
  8. `+ 1` (space)
  9. `` (empty string)
- **Expected**
  - Every publish receives an error pub ack.
  - The stream contains zero messages and no value is materialised on `counter.hits`.

### CTR-206 — `BigInt` values are accepted

- **References** — Design and Behavior ("any valid `BigInt`"); Solution Overview ("the server will always use `big.Int`").
- **Preconditions** — Counter stream.
- **Steps**
  1. Publish `Nats-Incr: +<2^128>` (a 39-digit positive integer well beyond int64) to `counter.big`.
  2. Publish `Nats-Incr: -<2^64>` to the same subject.
- **Expected**
  - Both publishes succeed.
  - Pub ack `val` for the second publish equals the decimal string `(2^128) - (2^64)`.
  - Stored payload `val` is the same decimal string. The harness compares the strings exactly (no scientific notation, no thousands separators).

### CTR-207 — Non-counter streams reject `Nats-Incr`

- **References** — Design and Behavior ("When a message with the header is published to a Stream without the option set the message is rejected with an error").
- **Preconditions** — A non-counter stream with `AllowMsgCounter: false`, listening on `counter.>`.
- **Steps**
  1. Publish `Nats-Incr: +1` to `counter.hits`.
- **Expected**
  - Pub ack carries an error.
  - The stream contains zero messages.

---

## CTR-300 — Forbidden header combinations on counter streams

### CTR-301 — `Nats-Rollup` is rejected with `Nats-Incr`

- **References** — Design and Behavior ("When a message has a `Nats-Rollup`, [...] header must be rejected").
- **Preconditions** — Counter stream.
- **Steps**
  1. Publish `Nats-Incr: +1` plus `Nats-Rollup: sub` to `counter.hits`.
  2. Publish `Nats-Incr: +1` plus `Nats-Rollup: all` to `counter.hits`.
- **Expected**
  - Both publishes receive an error pub ack.
  - The stream contains zero messages.

### CTR-302 — `Nats-Expected-Last-Sequence` is rejected with `Nats-Incr`

- **References** — Design and Behavior.
- **Preconditions** — Counter stream with one prior accepted increment so a non-zero last sequence exists.
- **Steps**
  1. Publish `Nats-Incr: +1` plus `Nats-Expected-Last-Sequence: <correct value>`.
  2. Publish `Nats-Incr: +1` plus `Nats-Expected-Last-Sequence: 0`.
- **Expected**
  - Both publishes receive an error pub ack — the header is rejected even when its value is correct.
  - No additional messages are stored.

### CTR-303 — `Nats-Expected-Subject-Last-Sequence` is rejected with `Nats-Incr`

- **References** — Design and Behavior.
- **Preconditions** — Counter stream with one prior accepted increment on `counter.hits`.
- **Steps**
  1. Publish `Nats-Incr: +1` plus `Nats-Expected-Subject-Last-Sequence: <correct value>` to `counter.hits`.
- **Expected**
  - Pub ack carries an error.
  - The stream count does not advance.

### CTR-304 — `Nats-Expected-Stream` is rejected with `Nats-Incr`

- **References** — Design and Behavior.
- **Preconditions** — Counter stream named `COUNTER`.
- **Steps**
  1. Publish `Nats-Incr: +1` plus `Nats-Expected-Stream: COUNTER` to `counter.hits`.
- **Expected**
  - Pub ack carries an error.
  - The stream count does not advance.

### CTR-305 — `Nats-Expected-Last-Msg-Id` is rejected with `Nats-Incr`

- **References** — Design and Behavior.
- **Preconditions** — Counter stream.
- **Steps**
  1. Publish `Nats-Incr: +1` plus `Nats-Expected-Last-Msg-Id: anything` to `counter.hits`.
- **Expected**
  - Pub ack carries an error.
  - The stream count does not advance.

---

## CTR-400 — Stored representation and PubAck

### CTR-401 — Stored body is `{"val":"<decimal>"}`

- **References** — Design and Behavior; Solution Overview.
- **Preconditions** — Counter stream; subject `counter.hits` empty.
- **Steps**
  1. Publish `Nats-Incr: +42` to `counter.hits`.
  2. Fetch the last message for `counter.hits`.
- **Expected**
  - The body is parseable JSON equal to `{"val":"42"}`. Whitespace inside the JSON is permitted but the `val` field MUST be a JSON string, not a JSON number.
  - The body has no other fields (forwards-compatible additions are allowed by the ADR but not exercised here; the harness records any unexpected fields without failing).

### CTR-402 — `Nats-Incr` header is preserved on the stored message

- **References** — Design and Behavior ("the body is parsed, incremented and written into the new message body. The headers are all preserved"); Recounts and Audit.
- **Preconditions** — Counter stream; subject `counter.hits` empty.
- **Steps**
  1. Publish `Nats-Incr: +7` to `counter.hits` with one extra header `X-Trace: abc123`.
  2. Fetch the last message.
- **Expected**
  - Stored message has `Nats-Incr: +7`.
  - Stored message has `X-Trace: abc123`.

### CTR-403 — PubAck contains `val` equal to the post-increment total

- **References** — Solution Overview ("The `PubAck` will include the value post-increment for fast feedback").
- **Preconditions** — Counter stream; subject `counter.hits` empty.
- **Steps**
  1. Publish `Nats-Incr: +1`.
  2. Publish `Nats-Incr: +1`.
  3. Publish `Nats-Incr: -3`.
- **Expected**
  - Pub acks (in order) carry `val == "1"`, `"2"`, `"-1"`.
  - Pub acks include `seq`, `stream`, and `domain` (where applicable) — i.e. the standard pub ack fields are still present alongside `val`.

### CTR-404 — Stream that mixes counter subjects with non-counter publishes is impossible

- **References** — Design and Behavior ("All subjects in the stream must be counters").
- **Preconditions** — Counter stream listening on `counter.>`.
- **Steps**
  1. Publish a regular (no `Nats-Incr`) message to `counter.misc`.
- **Expected**
  - Pub ack carries an error (per CTR-201). The harness asserts the same outcome regardless of subject — the stream-wide constraint applies on every subject the stream covers.

---

## CTR-500 — Subject transforms

### CTR-501 — Subject transform on the stream — counter accounting uses the rewritten subject

- **References** — Design and Behavior ("When rewrites are in place on the Stream, the rewritten subject should be used to perform the calculation").
- **Preconditions** — Counter stream with subject transform `counter.es.> -> counter.>` and `Subjects: ["counter.es.>"]`.
- **Steps**
  1. Publish `Nats-Incr: +1` to `counter.es.hits` (rewritten to `counter.hits`).
  2. Publish `Nats-Incr: +1` to `counter.es.hits`.
  3. Fetch the last message for `counter.hits`.
- **Expected**
  - Both publishes succeed.
  - Pub acks carry `val == "1"` then `val == "2"` — the counter accumulates against the rewritten subject `counter.hits`, not against `counter.es.hits`.
  - The stored message is on subject `counter.hits` with body `{"val":"2"}`.

---

## CTR-600 — Sources

### CTR-601 — Sourced counter messages produce a `Nats-Counter-Sources` header

- **References** — Design and Behavior ("When the previous message in the stream has the `Nats-Counter-Sources` header, it must be copied into the new message"); Adding Sources.
- **Preconditions**
  - Source counter stream `SRC` with `AllowMsgCounter: true`, listening on `count.es.>`.
  - Aggregate counter stream `AGG` with `AllowMsgCounter: true`, `Sources: [{Name: SRC}]`. No subjects of its own.
- **Steps**
  1. On `SRC`, publish `Nats-Incr: +3` to `count.es.hits`.
  2. Wait for the message to land in `AGG`.
  3. On `SRC`, publish `Nats-Incr: +4` to `count.es.hits`.
  4. Wait for the second message to land in `AGG`.
  5. Fetch the last message for `count.es.hits` from `AGG`.
- **Expected**
  - `AGG` last message body is `{"val":"7"}`.
  - `AGG` last message carries `Nats-Counter-Sources` containing an entry keyed by `SRC` → `count.es.hits` → `"7"` (the most recent sourced value).
  - `AGG` last message `Nats-Incr` is `+4` (the delta between the previously-seen sourced value `3` and the new sourced value `7`), NOT the verbatim `+4` from the source's increment header — it happens to be equal in this case but the harness asserts the value reflects the ADR-defined delta semantics.

### CTR-602 — Source delta calculation handles missed messages

- **References** — Design and Behavior ("`Nats-Incr` will be overwritten with the delta between the last seen `"val"` for that source [...] and the new one"); Adding Sources ("possible to replay `Nats-Incr` headers and recount on the aggregate stream even if some messages from the source were lost").
- **Preconditions**
  - Source counter stream `SRC` with a tight retention (e.g. `MaxMsgs: 1`) so older messages are evicted.
  - Aggregate counter stream `AGG` with `Sources: [{Name: SRC}]`.
- **Steps**
  1. On `SRC`, publish `Nats-Incr: +2` to `count.hits` (val=2). Wait for `AGG` to receive it.
  2. On `SRC`, publish `Nats-Incr: +5` to `count.hits` (val=7). Note that retention now drops the val=2 message from `SRC`. `AGG` may or may not have already received it; flush before continuing.
  3. On `SRC`, publish `Nats-Incr: +10` to `count.hits` (val=17). Wait for `AGG` to receive at least the first and last increments.
- **Expected**
  - `AGG` last message body is `{"val":"17"}`.
  - The cumulative `Nats-Incr` deltas stored in `AGG` for `count.hits` sum to `17` regardless of which intermediate messages were sourced — verifying the recount property.

### CTR-603 — Adding a source whose counter is already non-zero

- **References** — Adding Sources ("adding a source that has a non-zero counter is possible and the `Nats-Incr` header will include that initial count, i.e. adding source counter with an existing value of 10 will result in the first sourced count having a `Nats-Incr: 10`").
- **Preconditions**
  - Source counter stream `SRC` already at value `10` on `count.hits`.
  - Aggregate stream `AGG` with `AllowMsgCounter: true` and **no** sources yet.
- **Steps**
  1. Update `AGG` to add `Sources: [{Name: SRC}]`.
  2. Wait for the first sourced message to land in `AGG`.
- **Expected**
  - The first sourced message in `AGG` for `count.hits` has `Nats-Incr: +10` (or `10` — the harness records the exact sign-prefix form).
  - `AGG` body is `{"val":"10"}`.

### CTR-604 — `Nats-Counter-Sources` is preserved across local writes

- **References** — Design and Behavior ("When the previous message in the stream has the `Nats-Counter-Sources` header, it must be copied into the new message [...] This is necessary to ensure that we never lose sourcing state").
- **Preconditions**
  - Aggregate counter stream `AGG` with both `Sources: [{Name: SRC}]` and a local subject `count.local.hits`.
  - At least one prior sourced message has populated `Nats-Counter-Sources` on `AGG`.
- **Steps**
  1. Publish `Nats-Incr: +1` directly to `count.local.hits` (NOT via sourcing).
  2. Fetch the resulting stored message.
- **Expected**
  - The stored local message carries the `Nats-Counter-Sources` header inherited from the previous stream message — it is NOT dropped just because this publish was local.
  - The contents of `Nats-Counter-Sources` are unchanged by the local write.

### CTR-605 — Removed source key persists in `Nats-Counter-Sources`

- **References** — Adding Sources ("once a source key is added to this map, it is not ever removed, so that if the stream source is removed and re-added later to the stream config, the server will be able to do the right thing").
- **Preconditions**
  - Aggregate stream `AGG` sourcing `SRC` (already populated `Nats-Counter-Sources` for `SRC` → `count.hits` at value `5`).
- **Steps**
  1. Update `AGG` to remove `SRC` from `Sources`.
  2. Publish a local increment on `AGG` that triggers writing a new message (e.g. `Nats-Incr: +1` to a local subject the stream covers).
  3. Fetch the resulting stored message.
- **Expected**
  - `Nats-Counter-Sources` still contains the prior `SRC` → `count.hits` → `"5"` entry.
  - Re-adding `SRC` to `Sources` later (out of scope for this assertion) would resume from value `5` — captured in CTR-606.

### CTR-606 — Re-adding a previously-removed source resumes from the recorded value

- **References** — Adding Sources ("re-added later to the stream config, the server will be able to do the right thing and determine the difference during that time, instead of re-adding the whole value").
- **Preconditions** — Continues from CTR-605: `AGG` retains `SRC` → `count.hits` → `"5"` in `Nats-Counter-Sources`. While `SRC` was unsourced, two more `+10` increments were applied at `SRC` (so `SRC` `count.hits` is now at `25`).
- **Steps**
  1. Update `AGG` to add `Sources: [{Name: SRC}]` again.
  2. Wait for sourcing to resume and the next sourced message to arrive in `AGG`.
- **Expected**
  - The first sourced message after re-adding has `Nats-Incr` equal to the delta `25 - 5 = +20` (or possibly two separate `+10` deltas, one per re-sourced message — whichever ordering reaches `AGG` first; the harness records the breakdown).
  - The cumulative `val` on `AGG` advances by exactly `20` across the resumption.

---

## CTR-700 — Mirrors

### CTR-701 — Mirrors store counter messages verbatim

- **References** — Design and Behavior ("When a message with the header is received over a Source (with the setting disabled) or Mirror the message is stored verbatim").
- **Preconditions**
  - Counter stream `SRC` with `AllowMsgCounter: true`.
  - Mirror stream `MIR` with `Mirror: {Name: SRC}` and `AllowMsgCounter: false` (per CTR-105 mirrors cannot enable counter mode).
- **Steps**
  1. On `SRC`, publish `Nats-Incr: +1`, `+2`, `+3` to `counter.hits`.
  2. Wait for `MIR` to catch up.
  3. Inspect each message on `MIR`.
- **Expected**
  - `MIR` contains 3 messages.
  - Each message body equals what `SRC` stored (`{"val":"1"}`, `{"val":"3"}`, `{"val":"6"}`).
  - Each message preserves the original `Nats-Incr` header verbatim.
  - The mirror does NOT re-evaluate counter logic on top — it stores what `SRC` stored.

### CTR-702 — Sourced counter messages into a non-counter stream are stored verbatim

- **References** — Design and Behavior ("received over a Source (with the setting disabled) [...] is stored verbatim").
- **Preconditions**
  - Counter stream `SRC` with `AllowMsgCounter: true`.
  - Stream `OBS` with `AllowMsgCounter: false` and `Sources: [{Name: SRC}]`.
- **Steps**
  1. On `SRC`, publish `Nats-Incr: +5` to `counter.hits`.
  2. Wait for `OBS` to receive it.
- **Expected**
  - `OBS` stores the message with the body `{"val":"5"}` and `Nats-Incr: +5` preserved.
  - `OBS` does NOT compute deltas (it is not a counter stream) — the `Nats-Incr` is the verbatim source increment, not a recomputed delta.
  - `OBS` does NOT add a `Nats-Counter-Sources` header.

---

## CTR-800 — Reset behavior

### CTR-801 — Subject purge resets a standalone counter

- **References** — Counter Resets ("subject purge being used only for entire counter deletes").
- **Preconditions** — Counter stream; `counter.hits` at value `42` (seeded by prior increments).
- **Steps**
  1. Purge `counter.hits` (no `keep`).
  2. Publish `Nats-Incr: +1` to `counter.hits`.
- **Expected**
  - After the purge, fetching the last message for `counter.hits` yields no message.
  - The new publish succeeds with `val == "1"` (counter started fresh).

### CTR-802 — Negative publish followed by purge-with-keep resets a sourced counter

- **References** — Counter Resets ("The preferred method for Reset should be a negative value being published followed by a purge up to the message holding the negative value").
- **Preconditions** — Counter stream; `counter.hits` at value `100`.
- **Steps**
  1. Publish `Nats-Incr: -100` to `counter.hits`. Capture the resulting `seq` (call it `R`).
  2. Purge `counter.hits` keeping the most recent message (or purge by sequence such that only the message at `seq=R` and later remain).
  3. Publish `Nats-Incr: +5` to `counter.hits`.
- **Expected**
  - After step 1, pub ack `val == "0"` and the stored message has body `{"val":"0"}`.
  - After step 2, only the reset message remains on `counter.hits`.
  - After step 3, pub ack `val == "5"`.

---

## CTR-900 — Audit and recount

### CTR-901 — Replaying preserved `Nats-Incr` headers reproduces the total

- **References** — Recounts and Audit ("given streams with no limits applied one can manually recount the entire stream").
- **Preconditions** — Counter stream with no message limits; subject `counter.hits` empty at start.
- **Steps**
  1. Publish increments `+1`, `+5`, `-2`, `+10`, `-3` in order to `counter.hits`.
  2. Iterate every message on the stream and sum the `Nats-Incr` header values, treating each as a `BigInt`.
  3. Read the last message's body `val`.
- **Expected**
  - The sum equals `11`.
  - The body `val` equals `"11"`.
  - The two values match — confirming the stored header history is sufficient to recount independently of the body.

---

## Out of scope

The following ADR-49 areas are intentionally **not** covered by this conformance document:

- The Orbit Counter client API (`Counter`, `Entry`, `Add`, `Get`, `GetMultiple`). Conformance is asserted at the protocol layer (raw headers and stored bodies) so any client is testable.
- Throughput and concurrency stress tests. The harness validates correctness only.
- Multi-tier global aggregation topologies beyond a single source/aggregate pair (CTR-600 covers the primitives).
- Cluster leader-change behavior. Counter increments are handled on the leader the same as ordinary publishes; ADR-49 does not introduce counter-specific leader-change semantics.
- The exact `err_code` values returned for counter-specific rejections — ADR-49 does not enumerate them and the harness asserts on error presence + descriptive message rather than specific codes.

## Implementation notes for the harness

- **Determinism over speed** — sourcing tests must wait for the destination stream to catch up rather than assuming immediate delivery. A 5–15s ceiling is reasonable; tests should fail with a clear timeout reason if sourcing does not converge.
- **Cleanup** — every test deletes its stream(s) on completion and uses unique stream names so a crashed test cannot leak state into a later one.
- **Reporting** — per-test result is `pass`, `fail`, `skip` (e.g. server below required version), or `inconclusive` (e.g. CTR-204 where multiple acceptable behaviors for unsigned `0` exist — record the observed branch).
- **Server version gating** — counters are introduced at 2.12.0 / API Level 2; the harness skips every CTR-* test on older builds with a clear reason.
- **BigInt comparisons** — counter values are decimal strings of arbitrary precision. The harness MUST compare them as strings (or via a `BigInt`/`big.Int` library), never via native floats or `int64`.
- **Header preservation assertions** — tests that read back stored headers should treat header ordering as unspecified and compare by header name + value set, not by raw header block bytes.