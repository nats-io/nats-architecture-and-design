# ADR-43 Conformance Tests — JetStream Per-Message TTL

This document describes the conformance tests that validate a server implementation of the **JetStream Per-Message TTL** feature defined in [ADR-43](../adr/ADR-43.md).

A conformance harness implementing these tests should be able to run them against any NATS server build claiming support for per-message TTL (introduced at server 2.11 / API Level 1).

## How to read this document

Each test has the following shape:

- **ID** — stable identifier, used by the harness for reporting (`TTL-NNN`).
- **Title** — one-line summary.
- **References** — the section of ADR-43 the test derives from.
- **Preconditions** — required server features, stream configuration, and any prior state.
- **Steps** — the actions the harness takes, expressed at the protocol level (headers, subject patterns, message values).
- **Expected** — the observable behavior the harness asserts on. Includes pub ack structure, error codes, headers placed by the server, and stored stream state.

A test passes only if every assertion in **Expected** holds. Where a test depends on another test's setup, that is called out in **Preconditions**.

## Common harness primitives

The harness needs the following building blocks. Implementations should provide them once and reuse them across tests.

- `new_stream(cfg)` — create a stream with the provided `StreamConfig`. Returns the stream name. Default config: `Subjects: ["TEST.>"]`, `Storage: file`, `Replicas: 1`, unless the test overrides these.
- `update_stream(name, cfg)` — apply a configuration update to an existing stream. Supports a `pedantic` flag so tests can exercise both modes.
- `delete_stream(name)` — clean up.
- `publish(stream, subject, headers, payload)` — performs a NATS *request* (not a fire-and-forget publish) so the harness receives the server's pub ack reply. Returns the parsed pub ack or error response.
- `delete_message(stream, seq, no_erase=true)` — calls the per-message delete API on a stream.
- `purge_subject(stream, subject, keep=0)` — purges the given subject (subject-scoped purge, not full-stream purge).
- `get_msg(stream, seq)` — direct-get a single message at `seq`. Used for asserting marker headers without scanning the whole stream.
- `get_last_for(stream, subject)` — fetches the last message stored on `subject` in `stream`, returning headers and payload.
- `stream_msgs(stream)` — returns the messages currently stored in the stream, in order, with their headers — used to assert on stored stream state and any server-placed markers.
- `stream_state(stream)` — returns last sequence, message count, first/last seq, and number of subjects.
- `wait_until_gone(stream, seq, timeout)` — polls the stream until the message at `seq` is no longer retrievable, or the timeout fires.
- `wait_for_marker(stream, subject, reason, timeout)` — polls `get_last_for(stream, subject)` until a server marker with `Nats-Marker-Reason: <reason>` appears, or the timeout fires.

The harness must use unique stream names per test so that prior state cannot leak.

## Wire-level reference

The harness asserts directly on these wire-level identifiers — they must match exactly.

Headers (request side):

- `Nats-TTL` — per-message TTL. Accepts:
  - An integer expressing **seconds** (e.g. `60`).
  - A Go duration string (e.g. `1h`, `90s`, `500ms`).
  - The literal string `never` — the message must never be expired.
  - Any other value — pub ack carries an `error`; the message is discarded.
- Minimum effective TTL value is `1s`. Sub-second durations (e.g. `500ms`) and a literal `0` are rejected.

Server-placed headers on marker messages:

- `Nats-Marker-Reason` — one of:
  - `MaxAge`  — placed when stream `MaxAge` removed the last message on a subject.
  - `Remove`  — placed when the per-message delete API removed the last message on a subject. *(Future; see TTL-700.)*
  - `Purge`   — placed when the subject-scoped purge API removed messages on a subject. *(Future; see TTL-800.)*
- `Nats-TTL` — TTL of the marker message itself. ADR-43 specifies that this value equals the stream's configured `SubjectDeleteMarkerTTL`, formatted as a Go duration string (e.g. `1m0s` for `60s`). The conformance harness also accepts the bare integer-seconds form (e.g. `60`).

Stream configuration fields (added by this ADR):

- `allow_msg_ttl` (`AllowMsgTTL`) — boolean. When `true`, the stream accepts and acts on `Nats-TTL` headers.
- `subject_delete_marker_ttl` (`SubjectDeleteMarkerTTL`) — Go duration. When non-zero, the stream emits server markers when a removal empties a subject. Minimum value `1s`.

Stream-configuration constraints:

- `AllowMsgTTL` MAY be enabled on an existing stream; once `true` it MUST NOT be settable back to `false`.
- `SubjectDeleteMarkerTTL` MUST NOT be set on a Mirror stream.
- When `AllowMsgTTL` or `SubjectDeleteMarkerTTL` is set, the stream's reported API level MUST be at least `1`.
- When `SubjectDeleteMarkerTTL` is set:
  - `AllowRollup` MUST be `true`. In non-pedantic mode the server SHOULD set this automatically; in pedantic mode the server MUST reject the configuration if it is not explicitly `true`.
  - `DenyPurge` MUST be `false`. Same pedantic / non-pedantic split as `AllowRollup`.

Server error codes:

ADR-43 enumerates `err_code` values for each rejection path. The harness asserts on:

- The presence of an `error` object in the pub ack (or stream-config response).
- The exact `err_code` matching ADR-43's table (see ADR-43 §"Error Codes").
- The absence of a stored message in the stream for any rejected publish.

The codes used by this suite:

- `10166` — publish with `Nats-TTL` to a stream where `AllowMsgTTL: false`.
- `10165` — `Nats-TTL` value is unparsable, sub-second, or a literal `0`.
- `10052` — all stream-config rejections: `SubjectDeleteMarkerTTL` minimum, mirror with `SubjectDeleteMarkerTTL`, pedantic `AllowRollup`/`DenyPurge` rejections, and attempting to disable `AllowMsgTTL`.

---

## TTL-100 — Stream configuration

### TTL-101 — Enabling `AllowMsgTTL` works

- **References** — Stream Configuration.
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `AllowMsgTTL: true`.
  2. Read back the stream configuration.
- **Expected**
  - Stream creation succeeds.
  - `AllowMsgTTL` is reported as `true`.
  - The stream's reported API level is at least `1`.

### TTL-102 — `AllowMsgTTL` defaults off

- **References** — Stream Configuration ("When a message with the `Nats-TTL` header is published to a stream with the feature disabled the message will be rejected with an error").
- **Preconditions** — None.
- **Steps**
  1. Create a stream without specifying `AllowMsgTTL`.
  2. Publish a message with header `Nats-TTL: 60s`.
- **Expected**
  - Stream creation succeeds; `AllowMsgTTL` is reported as `false`.
  - The publish receives an error pub ack.
  - The stream contains zero messages.

### TTL-103 — `AllowMsgTTL` can be enabled on an existing stream

- **References** — Stream Configuration ("The `AllowMsgTTL` field can be enabled on existing streams but not disabled").
- **Preconditions** — Stream `S` created with `AllowMsgTTL: false`.
- **Steps**
  1. Update `S` to set `AllowMsgTTL: true`.
  2. Read back the stream configuration.
  3. Publish a message with `Nats-TTL: 60s`.
- **Expected**
  - Update succeeds.
  - `AllowMsgTTL` is reported as `true`.
  - The publish succeeds; the message is stored.

### TTL-104 — `AllowMsgTTL` cannot be disabled once enabled

- **References** — Stream Configuration ("The `AllowMsgTTL` field can be enabled on existing streams but not disabled").
- **Preconditions** — Stream created with `AllowMsgTTL: true`.
- **Steps**
  1. Update the stream to set `AllowMsgTTL: false`.
- **Expected**
  - Update fails with a stream-config error.
  - `AllowMsgTTL` remains `true` after the rejected update.

### TTL-105 — `SubjectDeleteMarkerTTL` minimum value is 1s

- **References** — Stream Configuration ("The `Nats-TTL` header value and `SubjectDeleteMarkerTTL` setting have a minimum value of 1 second").
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `SubjectDeleteMarkerTTL: 500ms`, `AllowMsgTTL: true`.
- **Expected**
  - Stream creation fails with `err_code: 10052` and a description referencing the marker minimum.

### TTL-106 — `SubjectDeleteMarkerTTL` rejected on a Mirror

- **References** — Stream Configuration ("The `SubjectDeleteMarkerTTL` setting may not be set on a Mirror Stream").
- **Preconditions** — A source stream `SRC` exists.
- **Steps**
  1. Attempt to create a stream with `Mirror: {Name: SRC}` and `SubjectDeleteMarkerTTL: 60s`.
- **Expected**
  - Stream creation fails with a stream-config error.

### TTL-107 — `SubjectDeleteMarkerTTL` requires `AllowRollup` (non-pedantic auto-set)

- **References** — Stream Configuration ("`AllowRollup` must be `true`, stream update and create should set this unless pedantic mode is enabled").
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `SubjectDeleteMarkerTTL: 60s`, `AllowMsgTTL: true`, *without* setting `AllowRollup`. Use non-pedantic mode.
  2. Read back the stream configuration.
- **Expected**
  - Stream creation succeeds.
  - `AllowRollup` is reported as `true` (server auto-set).

### TTL-108 — `SubjectDeleteMarkerTTL` requires `AllowRollup` (pedantic rejects)

- **References** — Stream Configuration ("stream update and create should set this unless pedantic mode is enabled").
- **Preconditions** — None.
- **Steps**
  1. In pedantic mode, attempt to create a stream with `SubjectDeleteMarkerTTL: 60s`, `AllowMsgTTL: true`, `AllowRollup: false`.
- **Expected**
  - Stream creation fails with a stream-config error referencing `AllowRollup`.

### TTL-109 — `SubjectDeleteMarkerTTL` requires `DenyPurge: false` (non-pedantic auto-clear)

- **References** — Stream Configuration ("`DenyPurge` must be `false`, stream update and create should set this unless pedantic mode is enabled").
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `SubjectDeleteMarkerTTL: 60s`, `AllowMsgTTL: true`, `DenyPurge: true`. Use non-pedantic mode.
  2. Read back the stream configuration.
- **Expected**
  - Stream creation succeeds.
  - `DenyPurge` is reported as `false` (server auto-cleared).

### TTL-110 — `SubjectDeleteMarkerTTL` requires `DenyPurge: false` (pedantic rejects)

- **References** — Stream Configuration ("stream update and create should set this unless pedantic mode is enabled").
- **Preconditions** — None.
- **Steps**
  1. In pedantic mode, attempt to create a stream with `SubjectDeleteMarkerTTL: 60s`, `AllowMsgTTL: true`, `DenyPurge: true`.
- **Expected**
  - Stream creation fails with a stream-config error referencing `DenyPurge`.

### TTL-111 — `SubjectDeleteMarkerTTL` set raises API level to ≥ 1

- **References** — Stream Configuration ("When `AllowMsgTTL` or `SubjectDeleteMarkerTTL` are set the Stream should require API level `1`").
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `SubjectDeleteMarkerTTL: 60s` (and `AllowMsgTTL: true`, `AllowRollup: true`, `DenyPurge: false` set explicitly so we are testing the effect of `SubjectDeleteMarkerTTL` alone).
  2. Read back the stream configuration.
- **Expected**
  - Stream creation succeeds.
  - The stream's reported API level is at least `1`.

---

## TTL-200 — `Nats-TTL` header parsing

These tests run against a stream with `AllowMsgTTL: true` and `SubjectDeleteMarkerTTL` unset, so we observe header handling without marker behavior interacting.

### TTL-201 — Integer seconds value is accepted

- **References** — General Behavior ("a duration as seconds or as a Go duration string").
- **Preconditions** — Stream with `AllowMsgTTL: true`.
- **Steps**
  1. Publish to `TEST.a` with header `Nats-TTL: 60`.
- **Expected**
  - Successful pub ack with `seq=1`.
  - The stored message at `seq=1` has `Nats-TTL: 60` preserved on its headers.

### TTL-202 — Go duration string is accepted

- **References** — General Behavior.
- **Preconditions** — Stream with `AllowMsgTTL: true`.
- **Steps**
  1. Publish three messages with `Nats-TTL` of `1h`, `90s`, and `2m30s` respectively.
- **Expected**
  - All three publishes succeed.
  - Each stored message has its original `Nats-TTL` preserved verbatim on its headers.

### TTL-203 — `0` is rejected

- **References** — General Behavior ("a value that parses to a duration below the 1 second minimum (including a literal `0`) will result in an error").
- **Preconditions** — Stream with `AllowMsgTTL: true`.
- **Steps**
  1. Publish to `TEST.a` with header `Nats-TTL: 0`.
- **Expected**
  - Publish receives an error pub ack with `err_code: 10165`.
  - The stream contains zero messages.

### TTL-204 — Unparsable value is rejected

- **References** — General Behavior ("any other unparsable value will result in a error reported in the Pub Ack and the message being discarded").
- **Preconditions** — Stream with `AllowMsgTTL: true`.
- **Steps**
  1. Publish a message with `Nats-TTL: not-a-duration`.
  2. Publish a message with `Nats-TTL: 5x`.
  3. Publish a message with `Nats-TTL: -10s`.
- **Expected**
  - Each publish receives an error pub ack with `err_code: 10165`.
  - The stream contains zero messages.

### TTL-205 — Sub-second value is rejected

- **References** — Stream Configuration ("The `Nats-TTL` header value and `SubjectDeleteMarkerTTL` setting have a minimum value of 1 second").
- **Preconditions** — Stream with `AllowMsgTTL: true`.
- **Steps**
  1. Publish a message with `Nats-TTL: 500ms`.
  2. Publish a message with `Nats-TTL: 999ms`.
- **Expected**
  - Each publish receives an error pub ack with `err_code: 10165`.
  - The stream contains zero messages.

### TTL-206 — `Nats-TTL` rejected when feature disabled

- **References** — General Behavior ("When a message with the `Nats-TTL` header is published to a stream with the feature disabled the message will be rejected with an error").
- **Preconditions** — Stream created with `AllowMsgTTL: false` (default).
- **Steps**
  1. Publish to a subject in the stream with header `Nats-TTL: 60s`.
- **Expected**
  - Publish receives an error pub ack with `err_code: 10166`.
  - The stream contains zero messages.

---

## TTL-300 — TTL expiry behavior

### TTL-301 — Message expires after the supplied TTL

- **References** — General Behavior ("The duration will be used by the server to calculate the deadline for removing the message based on its Stream timestamp and the stated duration").
- **Preconditions** — Stream with `AllowMsgTTL: true`.
- **Steps**
  1. Publish to `TEST.a` with `Nats-TTL: 2s`. Capture the assigned `seq`.
  2. Immediately read back the message and assert it is present.
  3. Use `wait_until_gone(stream, seq, 8s)`.
- **Expected**
  - Step 2: message is present.
  - Step 3 returns `true` within the timeout — the message is removed roughly at its TTL deadline.

### TTL-302 — Mixed-TTL messages expire independently

- **References** — General Behavior.
- **Preconditions** — Stream with `AllowMsgTTL: true`.
- **Steps**
  1. Publish three messages on `TEST.a` with `Nats-TTL` of `2s`, `30s`, and (no header) respectively. Capture all three seqs.
  2. Wait 5 seconds.
- **Expected**
  - The 2s message is gone.
  - The 30s message is still present.
  - The header-less message is still present (no per-message TTL applied).

### TTL-303 — TTL is calculated from the stream timestamp

- **References** — General Behavior ("based on its Stream timestamp and the stated duration").
- **Preconditions** — Stream with `AllowMsgTTL: true`.
- **Steps**
  1. Publish a message with `Nats-TTL: 5s`. Record the local clock at publish time.
  2. Use `get_msg` to fetch the stream timestamp (`time`) recorded for the message.
  3. Use `wait_until_gone` and record when the message disappears.
- **Expected**
  - The message disappears no earlier than `time + 5s` (modulo the server's timer granularity, which the harness allows to slip by up to 2s on the late side).
  - The harness logs the observed `time + 5s` vs `disappearance` delta for visibility but does not fail on small skew.

---

## TTL-400 — `never` value

### TTL-401 — `Nats-TTL: never` survives normal publish/read

- **References** — General Behavior ("Setting the header `Nats-TTL` to `never` will result in a message that will never be expired").
- **Preconditions** — Stream with `AllowMsgTTL: true`.
- **Steps**
  1. Publish to `TEST.a` with `Nats-TTL: never`.
  2. Wait 5 seconds.
  3. Read the message back.
- **Expected**
  - Publish succeeds.
  - Message is still present after 5 seconds.
  - The stored message has `Nats-TTL: never` on its headers.

### TTL-402 — `Nats-TTL: never` survives a `MaxAge` shorter than the elapsed time

- **References** — General Behavior ("a `never` message is not removed by the stream's `MaxAge` setting").
- **Preconditions** — Stream with `AllowMsgTTL: true`, `MaxAge: 3s`.
- **Steps**
  1. Publish a message on `TEST.never` with `Nats-TTL: never`. Capture `seq_never`.
  2. Publish a message on `TEST.normal` *without* a TTL header. Capture `seq_normal`.
  3. Wait 6 seconds.
- **Expected**
  - `seq_normal` is gone (removed by `MaxAge`).
  - `seq_never` is still present (the `never` value overrides `MaxAge`).

---

## TTL-500 — MaxAge limit markers

These tests are **opt-in / slow** — they wait for real wall-clock expiry. The harness should mark them resource-intensive and run them with generous timeouts.

### TTL-501 — `MaxAge` removal of last value places a marker

- **References** — Limit Markers ("when the server removes a message and the message is the last in the subject it would place a message with a TTL matching the Stream configuration value").
- **Preconditions** — Stream with `AllowMsgTTL: true`, `SubjectDeleteMarkerTTL: 60s`, `MaxAge: 3s`. (`AllowRollup`, `DenyPurge` allowed to be auto-set.)
- **Steps**
  1. Publish a single message to `TEST.k` (no TTL header). Capture `seq=1`.
  2. Wait 6 seconds for `MaxAge` to remove the message.
  3. `get_last_for(stream, "TEST.k")`.
- **Expected**
  - The last message on `TEST.k` is a server-placed marker.
  - The marker carries `Nats-Marker-Reason: MaxAge`.
  - The marker carries `Nats-TTL` set to the configured `SubjectDeleteMarkerTTL` value (`60s`).
  - The marker payload is empty.

### TTL-502 — `MaxAge` removal that does NOT empty the subject does not place a marker

- **References** — Limit Markers ("the message is the last in the subject").
- **Preconditions** — Stream with `AllowMsgTTL: true`, `SubjectDeleteMarkerTTL: 60s`, `MaxAge: 4s`.
- **Steps**
  1. Publish msg A on `TEST.k` at t=0.
  2. Publish msg B on `TEST.k` at t=2s.
  3. Wait until t=6s (msg A is past MaxAge but msg B is not).
  4. `get_last_for(stream, "TEST.k")`.
- **Expected**
  - Last message on `TEST.k` is msg B (no marker placed — the subject still has a live message).

### TTL-503 — Marker is itself subject to its own `Nats-TTL`

- **References** — Limit Markers (marker has `Nats-TTL` from `SubjectDeleteMarkerTTL`).
- **Preconditions** — Stream with `AllowMsgTTL: true`, `SubjectDeleteMarkerTTL: 2s`, `MaxAge: 3s`.
- **Steps**
  1. Publish a single message on `TEST.k`.
  2. Wait 6 seconds for `MaxAge` to remove the message and a marker to be placed.
  3. Capture the marker via `wait_for_marker`.
  4. Wait an additional 4 seconds.
  5. `get_last_for(stream, "TEST.k")`.
- **Expected**
  - Step 3: marker is observed with `Nats-Marker-Reason: MaxAge` and `Nats-TTL: 2s`.
  - Step 5: the marker has expired (no message on `TEST.k`, or the message returned is something else).

### TTL-504 — `Nats-TTL` removal of last value places a marker

- **References** — Limit Markers ("This marker will also be placed for a message removed by the `Nats-TTL` timer").
- **Preconditions** — Stream with `AllowMsgTTL: true`, `SubjectDeleteMarkerTTL: 60s`, no `MaxAge`.
- **Steps**
  1. Publish a single message on `TEST.k` with `Nats-TTL: 2s`.
  2. Wait 6 seconds.
  3. `wait_for_marker(stream, "TEST.k", "MaxAge", 4s)`.
- **Expected**
  - A marker is observed on `TEST.k` with `Nats-Marker-Reason: MaxAge`. *(Per ADR-43: the same `MaxAge` reason is used for both `MaxAge` and per-message TTL expiries — see "Spec gaps" for whether a distinct reason is desirable.)*
  - The marker carries `Nats-TTL` equal to `SubjectDeleteMarkerTTL`.

### TTL-505 — Markers off when `SubjectDeleteMarkerTTL` is unset

- **References** — Limit Markers ("This behaviour is off by default unless opted in on the `SubjectDeleteMarkerTTL` Stream Configuration").
- **Preconditions** — Stream with `AllowMsgTTL: true`, `MaxAge: 3s`, `SubjectDeleteMarkerTTL` unset (zero).
- **Steps**
  1. Publish a single message on `TEST.k`.
  2. Wait 6 seconds.
  3. `get_last_for(stream, "TEST.k")`.
- **Expected**
  - No marker is placed; the subject is empty after expiry.

### TTL-506 — `SubjectDeleteMarkerTTL` clamps sub-floor `Nats-TTL` upward

- **References** — Stream Configuration ("Unless `MaxMsgsPer` equals 1 the server treats `SubjectDeleteMarkerTTL` as the minimum effective `Nats-TTL` ... raises the effective TTL to the floor and rewrites the stored `Nats-TTL` header").
- **Preconditions** — Stream with `AllowMsgTTL: true`, `SubjectDeleteMarkerTTL: 60s`, `MaxMsgsPer` unset (i.e. not 1).
- **Steps**
  1. Publish a message with `Nats-TTL: 2s` (below the marker TTL).
  2. Read the stored message back.
- **Expected**
  - Publish succeeds (the publish is *not* rejected for being below the marker minimum).
  - The stored message's `Nats-TTL` header has been rewritten to a value matching `SubjectDeleteMarkerTTL` (the server clamped it upward).

---

## TTL-600 — Per-message TTL expiry markers

Covered by TTL-504 above. No additional cases here — the ADR does not define a distinct reason for per-message TTL expiry vs `MaxAge` (both use `Nats-Marker-Reason: MaxAge`).

---

## TTL-700 — Delete API marker (future feature)

ADR-43 marks this section with: *"This feature will come either later in 2.11.x series or in 2.12."* These tests are run as **inconclusive** unless a probe confirms the feature is implemented. The harness records the observed branch.

### TTL-701 — Delete API places a marker on the now-empty subject

- **References** — Delete API Call Marker.
- **Preconditions** — Stream with `AllowMsgTTL: true`, `SubjectDeleteMarkerTTL: 60s`.
- **Steps**
  1. Publish a single message on `TEST.k`. Capture `seq`.
  2. Call `delete_message(stream, seq)`.
  3. `get_last_for(stream, "TEST.k")`.
- **Expected**
  - Either:
    - A marker is placed with `Nats-Marker-Reason: Remove` and `Nats-TTL` equal to `SubjectDeleteMarkerTTL`. (Feature implemented.)
    - The subject is empty (no marker). (Feature not yet implemented.)
  - The harness records which branch occurred and reports the test result as **inconclusive** until a server is observed implementing the feature, at which point this test becomes a hard pass/fail.

### TTL-702 — Delete API: deleting non-last message does NOT place a marker

- **References** — Delete API Call Marker (marker is for emptying a subject).
- **Preconditions** — As TTL-701.
- **Steps**
  1. Publish msg A on `TEST.k` (capture `seqA`); publish msg B on `TEST.k` (capture `seqB`).
  2. Call `delete_message(stream, seqA)`.
  3. `get_last_for(stream, "TEST.k")`.
- **Expected**
  - Last message on `TEST.k` is msg B (no marker placed). This must hold regardless of whether the feature in TTL-701 is implemented.

---

## TTL-800 — Purge API marker (future feature)

ADR-43 marks this section with: *"This feature will come either later in 2.11.x series or in 2.12."* These tests are run as **inconclusive** unless a probe confirms the feature is implemented.

### TTL-801 — Purge subject places a marker

- **References** — Purge API Call Marker.
- **Preconditions** — Stream with `AllowMsgTTL: true`, `SubjectDeleteMarkerTTL: 60s`.
- **Steps**
  1. Publish three messages on `TEST.k`.
  2. `purge_subject(stream, "TEST.k")` (no `keep`).
  3. `get_last_for(stream, "TEST.k")`.
- **Expected**
  - Either:
    - A marker is placed with `Nats-Marker-Reason: Purge` and `Nats-TTL` equal to `SubjectDeleteMarkerTTL`. (Feature implemented.)
    - The subject is empty (no marker). (Feature not yet implemented.)
  - The harness records which branch occurred and reports inconclusive until implementation is observed.

### TTL-802 — Purge with `keep` does NOT place a marker

- **References** — Purge API Call Marker (marker is for emptying a subject).
- **Preconditions** — As TTL-801.
- **Steps**
  1. Publish three messages on `TEST.k`.
  2. `purge_subject(stream, "TEST.k", keep=1)`.
  3. `get_last_for(stream, "TEST.k")`.
- **Expected**
  - The last message on `TEST.k` is the most recent original publish (no marker placed).

---

## TTL-900 — Sources and Mirrors

### TTL-901 — Mirror always stores `Nats-TTL` messages even with `AllowMsgTTL` disabled

- **References** — Sources and Mirrors ("Sources and Mirrors will always accept and store messages with `Nats-TTL` header present, even if the `AllowMsgTTL` setting is disabled in the Stream settings").
- **Preconditions** — A `SRC` stream with `AllowMsgTTL: true`. A `MIR` mirror of `SRC` with `AllowMsgTTL: false` (default).
- **Steps**
  1. Publish msg A to `SRC` with `Nats-TTL: 60s`.
  2. Wait for `MIR` to catch up.
  3. Read the mirrored message from `MIR`.
- **Expected**
  - `MIR` contains the message.
  - The `Nats-TTL` header is preserved on the mirrored message.
  - The mirrored message is **not** subject to TTL expiry on `MIR` (because `AllowMsgTTL: false` on the mirror — the message is "just stored").

### TTL-902 — Mirror with `AllowMsgTTL: true` honours `Nats-TTL` on mirrored messages

- **References** — Sources and Mirrors ("If the `AllowMsgTTL` setting is enabled then processing continues as outlined in the General Behavior section with messages removed after the TTL").
- **Preconditions** — `SRC` with `AllowMsgTTL: true`. `MIR` mirror of `SRC` with `AllowMsgTTL: true`.
- **Steps**
  1. Publish msg A to `SRC` with `Nats-TTL: 3s`. Capture the mirrored seq.
  2. Wait for `MIR` to catch up.
  3. `wait_until_gone(MIR, mirrored_seq, 8s)`.
- **Expected**
  - The mirrored message expires on `MIR` after ~3s.

### TTL-903 — Mirror cannot enable `SubjectDeleteMarkerTTL`

- **References** — Sources and Mirrors ("Mirrors may not enable `SubjectDeleteMarkerTTL` since it would insert new messages into the Stream"). This duplicates TTL-106 from a "Mirrors and Sources" framing — included separately for cross-section discoverability.
- **Preconditions** — A source stream `SRC`.
- **Steps**
  1. Attempt to create / update a mirror of `SRC` with `SubjectDeleteMarkerTTL: 60s`.
- **Expected**
  - Operation fails with a stream-config error.

### TTL-904 — Source can enable `SubjectDeleteMarkerTTL`

- **References** — Sources and Mirrors ("Sources may set the `SubjectDeleteMarkerTTL` option").
- **Preconditions** — A source stream `SRC` with `AllowMsgTTL: true`.
- **Steps**
  1. Create a stream `DST` with `Sources: [{Name: SRC}]`, `AllowMsgTTL: true`, `SubjectDeleteMarkerTTL: 60s`, `MaxAge: 3s`. (Marker setup mirrors TTL-501.)
  2. Publish a single message on `SRC` so that it gets sourced into `DST`.
  3. Wait for `MaxAge` on `DST` to remove the sourced message (~6s) and a marker to land.
- **Expected**
  - `DST` is created successfully.
  - A marker with `Nats-Marker-Reason: MaxAge` is placed on `DST` for the now-empty subject.

---

## Out of scope

The following are intentionally **not** covered by this conformance document:

- KV / Object Store layered semantics built atop per-message TTL — covered by their own ADRs / conformance docs.
- Performance characteristics of TTL expiry (timer granularity beyond the small skew tolerance in TTL-303).
- Client-side header builders / `Nats-TTL` ergonomics. Conformance is asserted at the protocol layer.
- Replay / restart durability of TTL deadlines across server restart. (Could be added as a follow-up if the ADR clarifies expectations.)

## Implementation notes for the harness

- **Determinism over speed** — TTL tests should use TTLs of `2s` to `5s` and check expiry with `wait_until_gone` polling (250ms cadence, 8s upper bound) rather than tight sleeps.
- **Cleanup** — every test deletes its stream(s) on completion.
- **Reporting** — per-test result is `pass`, `fail`, `skip` (e.g. server below required version), or `inconclusive` (TTL-700 / TTL-800 series until a server implements them).
- **Server version gating** — per-message TTL is introduced at 2.11 / API Level 1; the harness skips every TTL-* test on older builds with a clear reason.
- **Resource-intensive tests** — TTL-500 series wait for real wall-clock `MaxAge` expiry and should be opt-in via a slow-test flag.
- **`err_code` assertions** — every rejection-path test asserts the exact `err_code` enumerated by ADR-43 (see the table at the top of this document).

---

## Spec gaps to feed back to ADR-43

Per the conformance review, these items are **flagged as failures / inconclusive results** so the ADR can be tightened. The harness should attach a "spec-gap" tag to the affected results.

1. **Delete / Purge API markers** (TTL-700 / TTL-800) — flagged as future in ADR-43 itself; tracked in server issue **SRV-499**. These tests will become hard pass/fail once a server claims support; the ADR should announce when that happens.