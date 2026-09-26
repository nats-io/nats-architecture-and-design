# ADR-31 Conformance Tests — JetStream Direct Get

This document describes the conformance tests that validate a server implementation of the **JetStream Direct Get** feature defined in [ADR-31](../adr/ADR-31.md).

A conformance harness implementing these tests should be able to run them against any NATS server build claiming support for Direct Get (introduced pre-2.11; multi-subject and batch behaviors require server 2.11 / API Level 1+).

## How to read this document

Each test has the following shape:

- **ID** — stable identifier, used by the harness for reporting (`DG-NNN`).
- **Title** — one-line summary.
- **References** — the section of ADR-31 the test derives from.
- **Preconditions** — required server features, stream configuration, and any prior state.
- **Steps** — the actions the harness takes, expressed at the protocol level (request payload, subject, expected reply behaviors).
- **Expected** — the observable behavior the harness asserts on. Includes status codes, response headers, payload presence, and message ordering.

A test passes only if every assertion in **Expected** holds. Where a test depends on another test's setup, that is called out in **Preconditions**.

## Common harness primitives

The harness needs the following building blocks. Implementations should provide them once and reuse them across tests.

- `new_stream(cfg)` — create a stream with the provided `StreamConfig`. Returns the stream name. Default config: `Subjects: ["KV.>"]`, `Storage: file`, `Replicas: 1`, unless the test overrides these.
- `delete_stream(name)` — clean up.
- `publish(subject, payload, headers={})` — publish a regular JetStream message and capture the assigned sequence.
- `kv_put(stream, key, value)` — convenience wrapper for KV-style writes (`stream` like `KV_X`, subjects like `$KV.X.<key>`).
- `direct_get(stream, payload, timeout)` — publishes a request to `$JS.API.DIRECT.GET.<stream>` with a fresh inbox, returns one or more replies (a list — Batch and Multi modes return many).
- `direct_get_subject(stream, subject_tokens, timeout)` — publishes to `$JS.API.DIRECT.GET.<stream>.<subject_tokens>` and returns the reply.
- `read_replies(inbox, timeout)` — drains an inbox, returning every reply received within the timeout window in order.
- `parse_status(msg)` — extracts the NATS status code (e.g. `204`, `404`, `408`, `413`) and description from a reply's status line, or `nil` if the reply is `NATS/1.0` (success).
- `parse_headers(msg)` — returns a map of `Nats-*` headers on the reply.
- `stream_info(name)` — returns the stream's reported configuration, including `allow_direct` and `max_msgs_per_subject`.

The harness must distinguish reply categories by status:

- **Success** — status line is `NATS/1.0` with no code; payload is the stored message body; `Nats-Stream`, `Nats-Subject`, `Nats-Sequence`, `Nats-Time-Stamp` headers are present.
- **EOB sentinel** — status `204` with description `EOB`; zero-length payload; carries `Nats-Num-Pending`, `Nats-Last-Sequence`, optionally `Nats-UpTo-Sequence`.
- **Not found** — status `404`.
- **Bad request** — status `408`.
- **Too many subjects** — status `413`.

## Wire-level reference

### Request subjects

```
$JS.API.DIRECT.GET.<stream>          # request payload carries the query
$JS.API.DIRECT.GET.<stream>.<tokens> # subject-appended (no payload)
```

### Request payload

```text
Seq          uint64     `json:"seq,omitempty"`
LastFor      string     `json:"last_by_subj,omitempty"`
NextFor      string     `json:"next_by_subj,omitempty"`
Batch        int        `json:"batch,omitempty"`
MaxBytes     int        `json:"max_bytes,omitempty"`
StartTime    *time.Time `json:"start_time,omitempty"`
MultiLastFor []string   `json:"multi_last,omitempty"`
UpToSeq      uint64     `json:"up_to_seq,omitempty"`
UpToTime     *time.Time `json:"up_to_time,omitempty"`
```

### Reply headers

| Header                | Meaning                                                                |
|-----------------------|------------------------------------------------------------------------|
| `Nats-Stream`         | Stream name                                                            |
| `Nats-Subject`        | Subject of the returned message                                        |
| `Nats-Sequence`       | Stream sequence of the returned message                                |
| `Nats-Time-Stamp`     | Message timestamp                                                      |
| `Nats-Num-Pending`    | (batch/multi EOB) Remaining messages matching the request              |
| `Nats-Last-Sequence`  | (batch/multi EOB) Sequence of the previous message in the batch        |
| `Nats-UpTo-Sequence`  | (multi EOB) Stream sequence used as `up_to_seq` for follow-up requests |

### Status codes

| Code | Meaning                                                  |
|------|----------------------------------------------------------|
| 204  | End of batch (description `EOB`); zero-length payload    |
| 404  | Valid request, no matching message in stream             |
| 408  | Empty or invalid request                                 |
| 413  | Multi-subject request matched too many subjects (>1024)  |

### Queue group

All Direct Get responders MUST subscribe under fixed queue group `_sys_`.

---

## DG-100 — Stream configuration: `allow_direct`

### DG-101 — `allow_direct` enables the Direct Get API

- **References** — Stream property: Allow Direct; Direct Get API.
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `AllowDirect: true` and `Subjects: ["KV.X.>"]`.
  2. Publish a message on `KV.X.k1` with payload `v1`.
  3. Send a Direct Get request with payload `{"last_by_subj":"KV.X.k1"}`.
- **Expected**
  - Stream creation succeeds; `stream_info` reports `allow_direct: true`.
  - Direct Get reply has status `NATS/1.0` (success), payload `v1`, and headers `Nats-Stream`, `Nats-Subject`, `Nats-Sequence`, `Nats-Time-Stamp` populated correctly.

### DG-102 — Direct Get is unavailable when `allow_direct` is false

- **References** — Direct Get API ("If Allow Direct is false, there will be no responder…").
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `AllowDirect: false` and `MaxMsgsPerSubject: -1` (unset).
  2. Publish a message on a subject in the stream.
  3. Send a Direct Get request with a 1-second timeout.
- **Expected**
  - The request times out (no reply received). The harness asserts no responder is registered.

### DG-103 — `MaxMsgsPerSubject > 0` does not auto-enable `allow_direct`

- **References** — Stream property: Allow Direct (rev 4: "`allow_direct` is opt-in. The server does not enable it implicitly based on other stream settings"). The legacy auto-promote behaviour was removed in server v2.9.0.
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `MaxMsgsPerSubject: 5` and **without** specifying `AllowDirect` (or with `AllowDirect: false`).
  2. Read back the stream configuration.
  3. Send a Direct Get request with a 1-second timeout.
- **Expected**
  - `stream_info` reports `allow_direct: false` (the user's value is preserved; `MaxMsgsPerSubject` does not promote it).
  - The Direct Get probe times out — no responder is registered.

### DG-104 — Explicit `allow_direct: true` is honored when `MaxMsgsPerSubject` is unset

- **References** — Stream property: Allow Direct.
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `AllowDirect: true` and `MaxMsgsPerSubject: -1` (unset / 0).
  2. Read back the stream configuration.
  3. Send a Direct Get request.
- **Expected**
  - `stream_info` reports `allow_direct: true`.
  - The Direct Get request is serviced (responder is registered).

### DG-105 — `allow_direct` toggles via stream update

- **References** — Stream property: Allow Direct.
- **Preconditions** — Stream created with `AllowDirect: false` and `MaxMsgsPerSubject: -1`.
- **Steps**
  1. Update the stream to `AllowDirect: true`.
  2. Send a Direct Get request and assert it is serviced.
  3. Update back to `AllowDirect: false` (with `MaxMsgsPerSubject: -1`).
  4. Send a Direct Get request with a short timeout.
- **Expected**
  - Step 2's request returns a reply.
  - Step 4's request times out (responder removed).

### DG-106 — Direct Get is serviced on a multi-replica stream

- **References** — Direct Get API ("subscribes to `$JS.API.DIRECT.GET.<stream>` with fixed queue group `_sys_`").
- **Preconditions** — A 3-replica stream with `AllowDirect: true`. Single-server target → skip with reason.
- **Steps**
  1. Publish a message on a subject in the stream.
  2. Send a Direct Get request with `last_by_subj`.
- **Expected**
  - Reply is success with payload matching the published message.
  - Queue-group load spread across the stream's peers is standard NATS routing and is not asserted here — the conformance scope is "the responder is registered and the request is serviced".

---

## DG-200 — Basic Direct Get API

### DG-201 — `seq` returns the message at that sequence

- **References** — Request payloads (`{seq: number}`).
- **Preconditions** — Stream with `AllowDirect: true`. Pre-publish three messages on `KV.X.a`, `KV.X.b`, `KV.X.c`; capture sequences `s1`, `s2`, `s3`.
- **Steps**
  1. Send `{"seq": s2}`.
- **Expected**
  - Reply is success; payload matches the second message; `Nats-Sequence` equals `s2`; `Nats-Subject` is `KV.X.b`.

### DG-202 — `last_by_subj` returns the most recent message for a subject

- **References** — Request payloads (`{last_by_subj: string}`).
- **Preconditions** — Stream with `AllowDirect: true`. Publish two messages on `KV.X.k1` (values `v1` then `v2`).
- **Steps**
  1. Send `{"last_by_subj":"KV.X.k1"}`.
- **Expected**
  - Reply payload is `v2` (the latest).
  - `Nats-Subject` is `KV.X.k1`; `Nats-Sequence` matches the second publish.

### DG-203 — `next_by_subj` returns the first matching message

- **References** — Request payloads (`{next_by_subj: string}`).
- **Preconditions** — Stream with `AllowDirect: true`. Publish messages on `KV.X.a` (seq 1), `KV.X.b` (seq 2), `KV.X.a` (seq 3).
- **Steps**
  1. Send `{"next_by_subj":"KV.X.a"}`.
- **Expected**
  - Reply has `Nats-Sequence` equal to seq 1 (the lowest sequence matching the subject), payload of the first publish.

### DG-204 — `seq` + `next_by_subj` returns the first match at or after seq

- **References** — Request payloads (`{seq: number, next_by_subj: string}`).
- **Preconditions** — Stream with `AllowDirect: true`. Publish on `KV.X.a` (seq 1), `KV.X.b` (seq 2), `KV.X.a` (seq 3).
- **Steps**
  1. Send `{"seq": 2, "next_by_subj":"KV.X.a"}`.
- **Expected**
  - Reply has `Nats-Sequence` equal to seq 3 (the lowest seq ≥ 2 matching `KV.X.a`).

### DG-205 — `start_time` returns the first message at or after the time

- **References** — Request payloads (`{start_time: string}`); server 2.11+.
- **Preconditions** — Stream with `AllowDirect: true`. Publish three messages spaced apart, capturing each `Nats-Time-Stamp`.
- **Steps**
  1. Send `{"start_time": "<timestamp at or just before second message>"}`.
- **Expected**
  - Reply is the second message (smallest sequence whose timestamp ≥ the requested time).

### DG-206 — Empty stream returns 404

- **References** — Response Format (`404 if the request is valid but no matching message found in stream`).
- **Preconditions** — Stream with `AllowDirect: true`, no messages.
- **Steps**
  1. Send `{"seq": 1}`.
- **Expected**
  - Status `404`, no payload.

### DG-207 — `last_by_subj` for unknown subject returns 404

- **References** — Response Format (`404`).
- **Preconditions** — Stream with `AllowDirect: true`. Publish on `KV.X.a` only.
- **Steps**
  1. Send `{"last_by_subj":"KV.X.does.not.exist"}`.
- **Expected**
  - Status `404`.

### DG-208 — Empty payload returns 408

- **References** — Response Format (`408 if the request is empty or invalid`).
- **Preconditions** — Stream with `AllowDirect: true`.
- **Steps**
  1. Send a Direct Get request with a zero-length payload to `$JS.API.DIRECT.GET.<stream>` (note: Subject-Appended Direct Get with empty payload is *valid* — see DG-301; this test specifically targets the *non*-subject-appended endpoint).
- **Expected**
  - Status `408`. The harness records the description string for documentation.

### DG-209 — Malformed JSON returns 408

- **References** — Response Format (`408`).
- **Preconditions** — Stream with `AllowDirect: true`.
- **Steps**
  1. Send a Direct Get request with payload `{this is not json`.
- **Expected**
  - Status `408`.

### DG-210 — Batch with neither `seq` nor `start_time` defaults to `seq: 1`

- **References** — Batched Requests (rev 5: "If neither is supplied the server defaults to `seq: 1`").
- **Preconditions** — Stream with `AllowDirect: true`. Pre-publish three messages on `KV.X.a`; capture their sequences `s1`, `s2`, `s3`.
- **Steps**
  1. Send a batch request without `seq` and without `start_time`: `{"batch": 3, "next_by_subj":"KV.X.a"}`.
- **Expected**
  - 3 message replies followed by 1 EOB sentinel.
  - The first message reply has `Nats-Sequence == s1` (the lowest matching sequence in the stream — equivalent to having sent `seq: 1`).
  - Subsequent message replies have monotonically increasing `Nats-Sequence` matching `s2`, `s3`.

---

## DG-300 — Subject-Appended Direct Get API

### DG-301 — Subject-appended request returns last message for subject

- **References** — Subject-Appended Direct Get API.
- **Preconditions** — Stream with `AllowDirect: true`, `Subjects: ["$KV.X.>"]`. Publish two values on `$KV.X.key1` (`v1`, `v2`).
- **Steps**
  1. Publish to `$JS.API.DIRECT.GET.<stream>.$KV.X.key1` with **empty payload**.
- **Expected**
  - Reply payload is `v2` (semantics equivalent to `{"last_by_subj":"$KV.X.key1"}`).
  - `Nats-Subject` is `$KV.X.key1`.

### DG-302 — Subject-appended with payload returns 408

- **References** — Subject-Appended Direct Get API ("It is an error (408) if a client calls Subject-Appended Direct Get and includes a request payload").
- **Preconditions** — As DG-301.
- **Steps**
  1. Publish to `$JS.API.DIRECT.GET.<stream>.$KV.X.key1` with payload `{"seq":1}`.
- **Expected**
  - Status `408`.

### DG-303 — Subject-appended request preserves multi-token subjects

- **References** — Subject-Appended Direct Get API ("derived by the token (or series of tokens) following the stream name").
- **Preconditions** — Stream with `AllowDirect: true`, `Subjects: ["$KV.X.>"]`. Publish on `$KV.X.users.1234.name` value `Bob`.
- **Steps**
  1. Publish to `$JS.API.DIRECT.GET.<stream>.$KV.X.users.1234.name` with empty payload.
- **Expected**
  - Reply payload is `Bob`; `Nats-Subject` is `$KV.X.users.1234.name`.

### DG-304 — Subject-appended for unknown subject returns 404

- **References** — Response Format.
- **Preconditions** — Stream with `AllowDirect: true`. No messages on `$KV.X.missing`.
- **Steps**
  1. Publish to `$JS.API.DIRECT.GET.<stream>.$KV.X.missing` with empty payload.
- **Expected**
  - Status `404`.

---

## DG-400 — Batched requests

### DG-401 — Basic batch returns up to N messages followed by EOB

- **References** — Batched Requests.
- **Preconditions** — Stream with `AllowDirect: true`. Publish 5 messages on `KV.X.a`.
- **Steps**
  1. Send `{"batch": 3, "seq": 1, "next_by_subj":"KV.X.a"}`.
  2. Drain replies for 2 seconds.
- **Expected**
  - Exactly 4 replies received: 3 messages followed by 1 EOB sentinel.
  - The 3 message replies have monotonically increasing `Nats-Sequence`.
  - Each subsequent message reply's `Nats-Last-Sequence` equals the prior reply's `Nats-Sequence`.
  - The EOB reply has status `204` description `EOB`, zero-length payload, `Nats-Num-Pending: 2` (5 total minus 3 returned), and `Nats-Last-Sequence` equal to the last delivered message's sequence.

### DG-402 — Batch with `start_time` filters by timestamp

- **References** — Batched Requests (start time variant).
- **Preconditions** — Stream with `AllowDirect: true`. Publish 5 messages on `KV.X.a` with measurable time gaps; capture the timestamp of message 3.
- **Steps**
  1. Send `{"batch": 5, "start_time":"<ts of message 3>", "next_by_subj":"KV.X.a"}`.
- **Expected**
  - 3 message replies + 1 EOB.
  - First message has `Nats-Sequence` ≥ that of message 3.

### DG-403 — Batch respects `max_bytes`

- **References** — Batched Requests (max_bytes variant); Response Format.
- **Preconditions** — Stream with `AllowDirect: true`. Publish 10 messages on `KV.X.a`, each payload exactly 100 bytes.
- **Steps**
  1. Send `{"batch": 10, "max_bytes": 250, "seq": 1, "next_by_subj":"KV.X.a"}`.
- **Expected**
  - The number of message replies before EOB is bounded such that total message bytes do not exceed `max_bytes` (`max_bytes` is the **upper bound**; per ADR "it will send up to `max_bytes` messages").
  - At most 2 messages (since 3 × 100 = 300 > 250).
  - EOB sentinel follows; `Nats-Num-Pending` is non-zero (more messages remain).

### DG-404 — Batch is exhausted when fewer messages match than requested

- **References** — Batched Requests; Response Format.
- **Preconditions** — Stream with `AllowDirect: true`. Publish 2 messages on `KV.X.a`.
- **Steps**
  1. Send `{"batch": 10, "seq": 1, "next_by_subj":"KV.X.a"}`.
- **Expected**
  - 2 message replies followed by EOB.
  - EOB reply has `Nats-Num-Pending: 0`.

### DG-405 — Batch sequence chain via `Nats-Last-Sequence`

- **References** — Batched Requests; Response Format (`Nats-Last-Sequence`).
- **Preconditions** — As DG-401.
- **Steps**
  1. Send `{"batch": 5, "seq": 1, "next_by_subj":"KV.X.a"}`.
- **Expected**
  - For replies indexed 1..N (after the first), `reply[i].Nats-Last-Sequence == reply[i-1].Nats-Sequence`.
  - The EOB's `Nats-Last-Sequence` matches the last message reply's `Nats-Sequence`.

### DG-406 — Old server detection via missing `Nats-Num-Pending`

- **References** — Batched Requests ("Old servers can be detected by the absence of the `Nats-Num-Pending` header in the first reply").
- **Preconditions** — Stream with `AllowDirect: true` against a server that supports batches.
- **Steps**
  1. Send `{"batch": 3, "seq": 1, "next_by_subj":"KV.X.a"}`.
- **Expected**
  - The first message reply contains the `Nats-Num-Pending` header (proves a non-old server).
  - This test is **skipped with reason** when run against pre-2.11 servers.

### DG-407 — `batch:0` is treated as a non-batch Get

- **References** — Batched Requests (rev 7: "If `batch` is omitted or set to `0`, the request is treated as a non-batch single-message Get").
- **Preconditions** — Stream with `AllowDirect: true`. Pre-publish 2 messages on `KV.X.a`; capture sequences.
- **Steps**
  1. Send `{"batch": 0, "seq": 1, "next_by_subj":"KV.X.a"}`.
- **Expected**
  - Exactly 1 reply received.
  - Status is success (no status code); `Nats-Sequence` matches the first matching message.
  - The reply does **not** carry `Nats-Num-Pending` and is **not** followed by an EOB sentinel.

---

## DG-500 — Multi-subject requests

### DG-501 — `multi_last` returns last message for each listed subject

- **References** — Multi-subject requests.
- **Preconditions** — Stream with `AllowDirect: true`, `Subjects: ["$KV.USERS.>"]`, `MaxMsgsPerSubject: -1`. Publish:
  - `$KV.USERS.1234.name` = `Bob`
  - `$KV.USERS.1234.surname` = `Smith`
  - `$KV.USERS.1234.address` = `1 Main Street`
  - `$KV.USERS.1234.address` = `10 Oak Lane` (overwrite)
- **Steps**
  1. Send `{"multi_last":["$KV.USERS.1234.name", "$KV.USERS.1234.address"]}`.
  2. Drain replies for 2 seconds.
- **Expected**
  - Two message replies (one per subject) followed by an EOB.
  - The reply for `$KV.USERS.1234.address` has payload `10 Oak Lane`.
  - EOB reply has `Nats-UpTo-Sequence` set to the stream's last applicable sequence at the time of the request.

### DG-502 — `multi_last` with wildcard

- **References** — Multi-subject requests.
- **Preconditions** — As DG-501.
- **Steps**
  1. Send `{"multi_last":["$KV.USERS.1234.>"]}`.
- **Expected**
  - Three message replies (`name`, `surname`, `address`) and an EOB.
  - `address` reply payload is `10 Oak Lane` (latest, not overwritten).

### DG-503 — `multi_last` with `up_to_seq` returns historical state

- **References** — Multi-subject requests (`up_to_seq`).
- **Preconditions** — As DG-501. Capture sequence of `$KV.USERS.1234.address = "1 Main Street"` as `s_addr_v1`.
- **Steps**
  1. Send `{"multi_last":["$KV.USERS.1234.>"],"up_to_seq": s_addr_v1}`.
- **Expected**
  - Three message replies; `address` reply payload is `1 Main Street` (the value at sequence `s_addr_v1`).
  - EOB reply has `Nats-UpTo-Sequence` equal to `s_addr_v1`.

### DG-504 — `multi_last` with `up_to_time` returns point-in-time state

- **References** — Multi-subject requests (`up_to_time`).
- **Preconditions** — As DG-501; insert a time gap of ≥ 100ms between the two address writes; capture the timestamp `t_v1` of the first address write.
- **Steps**
  1. Send `{"multi_last":["$KV.USERS.1234.>"],"up_to_time":"<t_v1 + small offset before second write>"}`.
- **Expected**
  - `address` reply payload is `1 Main Street`.

### DG-505 — `multi_last` with `batch` size limit and follow-up pagination

- **References** — Multi-subject requests (`batch` size, paging cursors `seq` + `up_to_seq`).
- **Preconditions** — Stream with `AllowDirect: true`. Pre-publish 5 distinct subjects under `$KV.USERS.1234.>`.
- **Steps**
  1. Send `{"multi_last":["$KV.USERS.1234.>"],"batch":2}`.
  2. Capture the EOB's `Nats-UpTo-Sequence` (call it `U`) and `Nats-Last-Sequence` (call it `L`).
  3. Send `{"multi_last":["$KV.USERS.1234.>"],"batch":5,"seq":L+1,"up_to_seq":U}`.
- **Expected**
  - Step 1: 2 message replies + EOB. EOB has `Nats-Num-Pending > 0`, plus `Nats-UpTo-Sequence` and `Nats-Last-Sequence`.
  - Step 3: 3 message replies + EOB. The 3 subjects MUST be disjoint from the 2 returned in step 1 (the `seq` cursor advances past the prior page; `up_to_seq` keeps the snapshot stable).
  - Step 3 EOB has `Nats-Num-Pending == 0` (snapshot exhausted).

### DG-506 — `multi_last` returns 413 when too many subjects match

- **References** — Multi-subject requests ("any multi-subject request may only allow matching up to 1024 subjects. Any more will result in a `413` status reply").
- **Preconditions** — Stream with `AllowDirect: true`. Pre-publish 1025 distinct subjects under `KV.X.>`.
- **Steps**
  1. Send `{"multi_last":["KV.X.>"]}`.
- **Expected**
  - Status `413`. No message replies emitted.

### DG-507 — Exactly 1024 matched subjects is allowed

- **References** — Multi-subject requests (boundary).
- **Preconditions** — Stream with `AllowDirect: true`. Pre-publish exactly 1024 distinct subjects under `KV.X.>`.
- **Steps**
  1. Send `{"multi_last":["KV.X.>"]}`.
- **Expected**
  - 1024 message replies followed by EOB. No `413` raised.

### DG-508 — `multi_last` chained reads via `Nats-UpTo-Sequence` are consistent

- **References** — Multi-subject requests ("`up_to_seq` keeps the snapshot stable… `seq` advances the lower bound").
- **Preconditions** — Stream with `AllowDirect: true`. Pre-publish 4 distinct subjects.
- **Steps**
  1. Send `{"multi_last":["KV.X.>"],"batch":2}`. Capture `Nats-UpTo-Sequence` (`U`) and `Nats-Last-Sequence` (`L`) from the EOB.
  2. After step 1, publish a *new* update to one of the already-returned subjects (mutate ground truth at a sequence `> U`).
  3. Send `{"multi_last":["KV.X.>"],"batch":2,"seq":L+1,"up_to_seq":U}`.
- **Expected**
  - Step 3 returns the remaining 2 subjects as they were *at* the captured `up_to_seq` point.
  - The mutated subject from step 2 is **not** present in step 3 (the `seq` cursor skipped it; `up_to_seq` would have excluded its post-snapshot write anyway).
  - The 2 subjects returned by step 3 are disjoint from the 2 returned by step 1.

---

## DG-600 — Response format and headers

### DG-601 — Success reply carries required headers

- **References** — Response Format.
- **Preconditions** — Stream with `AllowDirect: true`. Publish one message on `KV.X.k`.
- **Steps**
  1. Send `{"last_by_subj":"KV.X.k"}`.
- **Expected**
  - Reply contains all of: `Nats-Stream`, `Nats-Subject`, `Nats-Sequence`, `Nats-Time-Stamp`.
  - No status code is present (status line is `NATS/1.0`).

### DG-602 — EOB sentinel carries required headers

- **References** — Response Format.
- **Preconditions** — As DG-401.
- **Steps**
  1. Run a 3-message batch and capture the EOB reply.
- **Expected**
  - EOB reply has status `204`, description `EOB`, zero-length payload.
  - Headers present: `Nats-Num-Pending`, `Nats-Last-Sequence`.

### DG-603 — Multi-mode EOB carries `Nats-UpTo-Sequence`

- **References** — Multi-subject requests; Response Format.
- **Preconditions** — As DG-501.
- **Steps**
  1. Run a `multi_last` request and capture the EOB.
- **Expected**
  - EOB reply has `Nats-UpTo-Sequence` populated; `Nats-Num-Pending` and `Nats-Last-Sequence` also present.

### DG-604 — Reply body is the raw stored payload (no JSON envelope)

- **References** — Response Format ("A *regular* (not JSON-encoded) NATS message is returned").
- **Preconditions** — Stream with `AllowDirect: true`. Publish a message with payload `{"foo":"bar"}` on `KV.X.k`.
- **Steps**
  1. Send `{"last_by_subj":"KV.X.k"}`.
- **Expected**
  - Reply payload is exactly `{"foo":"bar"}` — byte-for-byte. The harness must NOT decode any envelope.

### DG-605 — Original message headers are preserved on reply

- **References** — Response Format.
- **Preconditions** — Stream with `AllowDirect: true`. Publish on `KV.X.k` with header `X-Custom: hello`.
- **Steps**
  1. Send `{"last_by_subj":"KV.X.k"}`.
- **Expected**
  - Reply contains `X-Custom: hello` alongside the `Nats-*` direct-get-specific headers.

---

## DG-700 — Mirror Direct Get responders

These tests require either a 2-stream setup (source + mirror) on a single server *or* a clustered server. A multi-cluster cross-region setup is **out of scope**.

### DG-701 — Mirror of a Direct-Get-enabled stream serves Direct Get to the upstream

- **References** — Extended feature: MIRROR Direct Get responders.
- **Preconditions** — Stream `SRC` with `AllowDirect: true`. A mirror stream `MIR` of `SRC` (same account). Publish 5 messages on `SRC`.
- **Steps**
  1. Wait for `MIR` to catch up.
  2. Send a Direct Get request to `$JS.API.DIRECT.GET.SRC` (the upstream subject).
- **Expected**
  - The request is serviced — one of `SRC`'s peers or a mirror peer responds via the `_sys_` queue group.
  - The reply payload matches the most recent published value.
  - Which peer wins the queue group is governed by standard NATS routing and is not asserted here.

### DG-702 — Mirror still serves Direct Get when upstream is offline

- **References** — Extended feature ("read availability can be enhanced as mirrors may be available to clients when the upstream is offline").
- **Preconditions** — As DG-701, but stop or partition the `SRC` peer(s) before the request.
- **Steps**
  1. Confirm `MIR` is fully caught up.
  2. Take `SRC` offline (remove the stream OR pause the leader's peers — the harness chooses the least invasive method).
  3. Send Direct Get to `$JS.API.DIRECT.GET.SRC`.
- **Expected**
  - The request succeeds when at least one mirror peer remains. Reply payload matches the stored message.
  - **Skipped with reason** on harness configurations that cannot induce upstream offline state.

### DG-703 — Mirror Direct Get respects `allow_direct` on the source stream

- **References** — Extended feature.
- **Preconditions** — Stream `SRC` with `AllowDirect: false`; a mirror `MIR` of `SRC`.
- **Steps**
  1. Send a Direct Get request to `$JS.API.DIRECT.GET.SRC` with a 1s timeout.
- **Expected**
  - Times out — neither `SRC` nor `MIR` has registered a responder for `SRC`'s upstream subject when `SRC` does not have `allow_direct`.

### DG-704 — `mirror_direct` defaults from the upstream's `allow_direct`

- **References** — Extended feature ("the new mirror's `mirror_direct` is set to match the upstream's `allow_direct`").
- **Preconditions** — None.
- **Steps**
  1. Create stream `SRC_ON` with `AllowDirect: true`.
  2. Create mirror `MIR_ON` of `SRC_ON` **without** specifying `MirrorDirect`.
  3. Read back `MIR_ON`'s configuration via `$JS.API.STREAM.INFO`.
  4. Create stream `SRC_OFF` with `AllowDirect: false` (or omitted).
  5. Create mirror `MIR_OFF` of `SRC_OFF` without specifying `MirrorDirect`.
  6. Read back `MIR_OFF`'s configuration.
- **Expected**
  - `MIR_ON.config.mirror_direct == true` (defaulted from upstream).
  - `MIR_OFF.config.mirror_direct == false` (defaulted from upstream).

### DG-705 — `mirror_direct` can be specified explicitly

- **References** — Extended feature ("a user-supplied `mirror_direct` … is silently aligned with the upstream in non-pedantic mode").
- **Preconditions** — None.
- **Steps**
  1. Create stream `SRC` with `AllowDirect: true`.
  2. Create mirror `MIR` of `SRC` with explicit `MirrorDirect: true`.
  3. Read back `MIR`'s configuration via `$JS.API.STREAM.INFO`.
- **Expected**
  - Mirror creation succeeds.
  - `MIR.config.mirror_direct == true`.
  - The harness records that the explicit value was preserved (the round-trip case where user-supplied agrees with upstream). Conformance does **not** assert behavior of an explicit value that *disagrees* with the upstream — that path is observable only with `Pedantic` mode flagged in the request, which is out of scope.

### DG-706 — Upstream `allow_direct` change does not auto-propagate; mirror update re-aligns

- **References** — Extended feature ("`mirror_direct` is captured on the mirror at create time and is not automatically refreshed when the upstream's `allow_direct` changes later").
- **Preconditions** — None.
- **Steps**
  1. Create stream `SRC` with `AllowDirect: true`.
  2. Create mirror `MIR` of `SRC` (no explicit `MirrorDirect` — defaults to `true`).
  3. Verify `MIR.config.mirror_direct == true`.
  4. Update `SRC` to `AllowDirect: false`.
  5. Verify `SRC.config.allow_direct == false`. Wait briefly for propagation.
  6. Read back `MIR`'s configuration without updating it.
  7. Issue a no-op `STREAM.UPDATE` against `MIR` (re-submitting the same mirror config).
  8. Read back `MIR`'s configuration again.
- **Expected**
  - Step 6: `MIR.config.mirror_direct == true` — the value is **stale**; the upstream change does not auto-propagate.
  - Step 8: `MIR.config.mirror_direct == false` — the mirror update re-runs the alignment rule and pulls in the upstream's current `allow_direct`.

---

## Out of scope

The following ADR-31 areas are intentionally **not** covered by this conformance document:

- **Read-after-write coherency claim.** The ADR explicitly states Direct Get does NOT guarantee read-after-write coherency. Asserting that property would require fault-injection beyond conformance scope; conformance only checks the *protocol* shape.
- **Cross-cluster MIRROR Direct Get geo-routing.** Requires multi-cluster infrastructure; the latency-reduction goal is a deployment property, not a server protocol guarantee.
- **Permission-based subject restrictions.** The Subject-Appended API exists *to enable* user-permission and import/export grants, but conformance does not assert specific authorization outcomes — those are tested by auth-specific suites.
- **Performance, throughput, latency.** The harness validates correctness only.
- **Server resource limits beyond what the ADR specifies.** Only the documented `1024` multi-subject cap is exercised.
- **Old-server compatibility regressions.** DG-406 only verifies the documented detection mechanism; running against pre-2.11 servers skips with reason.

## Implementation notes for the harness

- **Server version gating** — Multi-subject (`multi_last`, `up_to_seq`, `up_to_time`) and `start_time` are 2.11+. The harness MUST skip DG-205, DG-210, DG-402, DG-500-series with a clear reason on older servers.
- **Cluster gating** — DG-106 (queue-group load spread) and DG-700-series mirror tests need either replication or a mirror configuration; skip with reason otherwise.
- **Inbox subscriptions** — each test opens a fresh inbox per request. For batch and multi-mode tests, the harness must drain the inbox until either EOB OR a generous timeout (3s) elapses, then assert.
- **Time-based tests** — DG-205 / DG-504 / DG-508 must insert measurable real-time gaps (≥ 100ms) so that timestamps are unambiguously distinguishable. Tests must read each published message's `Nats-Time-Stamp` rather than relying on client-side clocks.
- **Cleanup** — every test deletes its stream(s) (and any mirror) on completion.
- **Reporting** — per-test result is `pass`, `fail`, `skip` (e.g. server below required version, or no cluster available) or `inconclusive` (used when a test exercises a path the ADR explicitly leaves ambiguous and records observed behavior).
- **Subject naming** — examples in this document use `KV.X.>` and `$KV.USERS.>` for clarity. The harness may use any subjects consistent with the stream's `Subjects` configuration.
- **Resource-intensive tests** — DG-506 / DG-507 (1025 / 1024 subjects) and DG-505 chained reads can require several thousand publishes; mark them opt-in via a flag if the target environment is resource-constrained.

## Ambiguities flagged in this document

These items in ADR-31 are unclear and the conformance suite currently records observed behavior rather than asserting a single answer. Resolving them in a future ADR revision would let these tests become strict pass/fail:

- **DG-705** — Behavior of an explicit `mirror_direct` that *disagrees* with the upstream's `allow_direct` is conditional on `Pedantic` request mode; conformance only asserts the agreeing-value round-trip.