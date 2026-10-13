# ADR-50 Conformance Tests — Fast-ingest Batch Publishing

This document describes the conformance tests that validate a server implementation of the **Fast-ingest Batch Publishing** feature defined in [ADR-50](../adr/ADR-50.md). It does **not** cover the Atomic Batch Publishing portion of that ADR — that is in [ADR-50-atomic-batch.md](ADR-50-atomic-batch.md).

A conformance harness implementing these tests should be able to run them against any NATS server build claiming support for fast-ingest batch publishing (introduced at server 2.14.0 / API Level 4).

## How to read this document

Each test has the following shape:

- **ID** — stable identifier, used by the harness for reporting (`FB-NNN`).
- **Title** — one-line summary.
- **References** — the section of ADR-50 the test derives from.
- **Preconditions** — required server features, stream configuration, and any prior state.
- **Steps** — the actions the harness takes, expressed at the protocol level (reply subject layout, operation values, message values).
- **Expected** — the observable behavior the harness asserts on. Includes flow-ack shape, gap shape, error shape, pub-ack shape, and stored stream state.

A test passes only if every assertion in **Expected** holds. Where a test depends on another test's setup, that is called out in **Preconditions**.

## Common harness primitives

The harness needs the following building blocks. Implementations should provide them once and reuse them across tests.

- `new_stream(cfg)` — create a stream with the provided `StreamConfig`. Returns the stream name. Default config: `Subjects: ["TEST.>"]`, `Storage: file`, `Replicas: 1`, `AllowBatchPublish: true`, unless the test overrides these.
- `delete_stream(name)` — clean up.
- `open_batch(stream, flow=10, gap="ok", batch_id=<uuid>)` — allocates an old-style inbox subscription on `<inbox>.<batch_id>.>` and remembers the parameters needed to compose subsequent reply subjects. Returns a handle the other primitives below take. Implementations MUST use an old-style inbox (not the mux inbox).
- `publish_initial(handle, headers={}, subject="TEST.a", payload=b"")` — performs operation `0`: publishes the message with reply subject `<inbox>.<batch_id>.<flow>.<gap>.1.0.$FI`. Returns the first reply received on the inbox.
- `publish_append(handle, headers={}, subject="TEST.a", payload=b"")` — increments the local batch sequence, performs operation `1`: reply subject `<inbox>.<batch_id>.<flow>.<gap>.<seq>.1.$FI`. Does not block on a reply.
- `publish_commit_store(handle, headers={}, subject="TEST.a", payload=b"")` — operation `2`: stores the final message and commits.
- `publish_commit_eob(handle, headers={}, subject="TEST.a", payload=b"")` — operation `3`: commits without storing the final message.
- `publish_ping(handle)` — operation `4`. The published reply subject MUST encode the highest sequence already sent (not an incremented sequence) per ADR-50.
- `read_flow(handle, timeout)` — drains the inbox until the next `BatchFlowAck`, `BatchFlowGap`, `BatchFlowErr`, or `PubAck` is received and returns it typed.
- `await_pub_ack(handle, timeout)` — keeps reading from the inbox, ignoring `BatchFlowAck`/`BatchFlowGap`/`BatchFlowErr`, until the final `PubAck` arrives or the timeout fires.
- `stream_msgs(stream)` — returns the messages currently stored in the stream, in order, with their headers — used to assert on the *committed* state of a batch.
- `stream_state(stream)` — returns last sequence, message count, and similar state.

The harness must use a fresh, unique `batch_id` per test (UUID v4) unless a test specifically exercises ID reuse or invalid IDs.

The harness must distinguish the four message types received on the inbox:

- `BatchFlowAck`  — JSON object with `"type":"ack"`. Carries `seq` and `msgs`.
- `BatchFlowGap`  — JSON object with `"type":"gap"`. Carries `last_seq` and `seq`.
- `BatchFlowErr`  — JSON object with `"type":"err"`. Carries `seq` and an `error` object.
- `PubAck`        — standard JetStream pub ack JSON; tests detect it by the presence of `seq` plus the absence of `"type"` field.

## Wire-level reference

The harness asserts directly on these wire-level identifiers — they must match exactly.

Reply subject (sent by the client):

```
<inbox>.<batch_id>.<flow>.<gap>.<batch_seq>.<operation>.$FI
```

| Field       | Format                                                  |
|-------------|---------------------------------------------------------|
| `<inbox>`   | Old-style inbox prefix the client subscribed to         |
| `<batch_id>`| UUID, max 64 characters                                 |
| `<flow>`    | Initial flow upper bound (positive integer)             |
| `<gap>`     | Either `ok` or `fail`                                   |
| `<batch_seq>` | Integer; starts at `1`, monotonically increases       |
| `<operation>` | `0` start, `1` append, `2` commit-store, `3` commit-eob, `4` ping |
| `$FI`       | Literal sentinel terminating the reply subject          |

Flow / gap / error message types:

- `BatchFlowAck` — `{"type":"ack","seq":N,"msgs":M}`
- `BatchFlowGap` — `{"type":"gap","last_seq":N,"seq":M}`
- `BatchFlowErr` — `{"type":"err","seq":N,"error":{"code":...,"err_code":...,"description":"..."}}`

Final pub ack (added fields, shared with atomic batch):

- `batch` (`BatchId`) — string.
- `count` (`BatchSize`) — integer.

Fast batch does not raise abandonment advisories. Per ADR-50, the
`stream_batch_abandoned` advisory is emitted only by atomic batch
publishing (covered in ADR-50-atomic-batch.md).

Server error codes (fast batch only):

| ErrCode | Code | Meaning                                             |
|---------|------|-----------------------------------------------------|
| 10205   | 400  | Batch publish not enabled on stream                 |
| 10206   | 400  | Batch publish invalid pattern used                  |
| 10207   | 400  | Batch publish ID is invalid (exceeds 64 characters) |
| 10208   | 400  | Batch publish ID is unknown                         |

---

## FB-100 — Stream configuration

### FB-101 — Enabling `AllowBatchPublish` works

- **References** — Stream Configuration.
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `AllowBatchPublish: true`.
  2. Read back the stream configuration.
- **Expected**
  - Stream creation succeeds.
  - `AllowBatchPublish` is reported as `true`.
  - The stream's reported API level is at least `3`.

### FB-102 — `AllowBatchPublish` defaults off

- **References** — Stream Configuration; FB-201 (depends on this default).
- **Preconditions** — None.
- **Steps**
  1. Create a stream without specifying `AllowBatchPublish`.
  2. Open a fast batch and send the initial message.
- **Expected**
  - Stream creation succeeds.
  - The initial message receives an error reply with `ErrCode 10205`.

### FB-103 — `AllowBatchPublish` toggles via update

- **References** — Stream Configuration ("These settings can be disabled and enabled using configuration updates").
- **Preconditions** — Stream created with `AllowBatchPublish: false`.
- **Steps**
  1. Update the stream to set `AllowBatchPublish: true`.
  2. Run a minimal commit batch (single message + commit-store) and assert it succeeds.
  3. Update the stream back to `AllowBatchPublish: false`.
  4. Attempt another initial message.
- **Expected**
  - Step 2 produces a successful pub ack with `batch` and `count`.
  - Step 4 produces an error reply with `ErrCode 10205`.

### FB-104 — `AllowBatchPublish` compatible with `PersistMode: async`

- **References** — Stream Configuration ("Setting `AllowAtomicPublish` and `PersistMode: async` must error, but this is allowed for `AllowBatchPublish`"), Server Behavior Design ("Streams with `PersistMode: async` set are compatible with fast ingest").
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `AllowBatchPublish: true` and `PersistMode: async`.
  2. Run a 5-message store-commit batch.
- **Expected**
  - Stream creation succeeds.
  - Batch commits successfully with `count=5` (or `count` matching the highest batch sequence reached, see FB-1401).

### FB-105 — `AllowBatchPublish` and `AllowAtomicPublish` may coexist

- **References** — Stream Configuration ("Both can be active at the same time, or just one at a time").
- **Preconditions** — None.
- **Steps**
  1. Create a stream with both `AllowBatchPublish: true` and `AllowAtomicPublish: true`.
  2. Run a 3-message atomic batch (per ADR-50-atomic-batch.md AB-203 layout).
  3. Run a 3-message fast batch on the same stream.
- **Expected**
  - Stream creation succeeds.
  - Both batches commit cleanly. Stream contains 6 messages, three from each batch.

### FB-106 — Mirrors cannot enable `AllowBatchPublish`

- **References** — Mirrors and Sources.
- **Preconditions** — A source stream `SRC` exists.
- **Steps**
  1. Attempt to create a mirror stream with `Mirror: {Name: SRC}` and `AllowBatchPublish: true`.
- **Expected**
  - Stream creation fails. (Per ADR: "Mirrors can't enable these settings".)

### FB-107 — Sources may enable `AllowBatchPublish`, but ignore batching reply subjects when sourcing

- **References** — Mirrors and Sources.
- **Preconditions** — A stream `SRC` exists with `AllowBatchPublish: true`.
- **Steps**
  1. Create a stream `DST` with `Sources: [{Name: SRC}]` and `AllowBatchPublish: true`. Assert this succeeds.
  2. Run a 3-message fast batch on `SRC`.
  3. Wait for the messages to be sourced into `DST`.
- **Expected**
  - All three messages appear on `DST`.
  - On `DST`, the messages do not retain any fast-batch reply-subject metadata; they appear as ordinary stream messages (the sourcing layer is unaware of fast-batch semantics).

---

## FB-200 — Single-message immediate commit

### FB-201 — Operation 2 with `<batch_seq>:1` returns a normal PubAck

- **References** — Server Errors ("a batch with only one message that immediately commits. That will return a `PubAck` like you would receive if you had used `js.Publish` or `js.PublishAsync` instead").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch with `flow=10`, `gap=ok`.
  2. Publish a single message with reply subject `<inbox>.<uuid>.10.ok.1.2.$FI` and payload `data`.
- **Expected**
  - First (and only) message received on the inbox is a `PubAck` (no preceding `BatchFlowAck`).
  - Pub ack `batch` equals the batch ID; `count` equals `1`; `seq` equals the stream's last sequence.
  - The stream contains exactly one message with payload `data`.

### FB-202 — Operation 3 with `<batch_seq>:1` (single EOB)

- **References** — Server Errors (single-message commit shortcut).
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch with `flow=10`, `gap=ok`.
  2. Publish a single message with reply subject `<inbox>.<uuid>.10.ok.1.3.$FI` (commit-eob).
- **Expected**
  - The harness asserts an unambiguous outcome:
    - Either: a single PubAck with `count=0` and **zero** stored messages (preferred — EOB applied; the EOB sentinel does NOT count toward `BatchSize`).
    - Or: an error reply (the operation is treated as an invalid initial-EOB).
  - The conformance suite records which branch the server chose; both are acceptable.
  - Inconsistent behavior across runs is a failure.

---

## FB-300 — Multi-message happy path

### FB-301 — Establishes batch, ramps flow, commits with PubAck

- **References** — Client Design; Flow Control ("the server may ramp up the flow"); Server Behavior Design (`count`).
- **Preconditions** — Stream with `AllowBatchPublish: true`. No other fast publishers.
- **Steps**
  1. Open a batch with `flow=100`, `gap=ok`.
  2. Send the initial message (operation `0`, batch seq `1`).
  3. Wait for the first `BatchFlowAck` and capture its `msgs`.
  4. Send 49 append messages (operations `1`, batch seq `2..50`).
  5. Send commit-store (operation `2`, batch seq `51`).
- **Expected**
  - The first reply on the inbox is a `BatchFlowAck`. Its `seq` is `0` or `1` — the establishment ack's job is to convey the allowed flow rate, and per the BatchFlowAck definition (`Sequence` reports "messages up to and including Sequence were persisted") the server may compose the ack before the initial publish has been persisted (seq=0) or after (seq=1). Its `msgs` is `≥ 1` and MUST NOT exceed the requested `flow` (`100`).
  - Subsequent `BatchFlowAck` messages, if any, also have `msgs ≤ 100`. Once the active `msgs` settles, ack frequency MUST match: between two consecutive non-decreasing `seq` values, there are exactly `msgs` published messages.
  - The final inbox message is a `PubAck` with `batch=<uuid>`, `count=51`, and `seq` equal to the stream's last sequence.
  - Stream contains exactly 51 messages.

### FB-302 — Multi-message ending in EOB

- **References** — Client Design (commit-eob).
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch (`flow=10`, `gap=ok`).
  2. Send 9 messages (operations `0` then 8 × `1`).
  3. Send commit-eob (operation `3`, batch seq `10`).
- **Expected**
  - PubAck `count` equals `9` — the EOB sentinel does NOT count toward `BatchSize` (per ADR-50: "the pub ack's `BatchSize` will reflect the messages in the batch, without counting the EOB message").
  - Stream contains exactly **9** messages (the EOB sentinel is not stored).
  - Pub ack `seq` equals the sequence of the last stored message.

### FB-303 — `BatchFlowAck.seq` and `msgs` invariants

- **References** — Flow Control (`BatchFlowAck`).
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Run a 100-message store-commit batch with `flow=20`.
- **Expected**
  - Every `BatchFlowAck` has a strictly non-decreasing `seq`.
  - For each `BatchFlowAck` after the first, `seq <= currently-sent batch sequence`.
  - The PubAck `count` matches the last batch sequence sent.

---

## FB-400 — Reply subject and operation validation

### FB-401 — Unknown operation value

- **References** — Client Design ("The server MUST reject any operation that it does not know about").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Send an initial message with reply subject `<inbox>.<uuid>.10.ok.1.9.$FI` (operation `9`).
- **Expected**
  - Error reply with `ErrCode 10206` (invalid pattern).
  - No batch is started; a subsequent valid initial for the same `<uuid>` MAY succeed (server treats it as a new batch).

### FB-402 — Invalid `<gap>` value

- **References** — Server Behavior Design ("Server will reject values for `gap` that is not `ok` or `fail`").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Send an initial message with reply subject `<inbox>.<uuid>.10.maybe.1.0.$FI`.
- **Expected**
  - Error reply with `ErrCode 10206`.

### FB-403 — Batch ID accepted at boundary length 64

- **References** — Server Behavior Design ("limit the `uuid` to 64 characters").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch with a `<batch_id>` exactly 64 characters long. Send a single commit-store message.
- **Expected**
  - Successful pub ack. Stream contains the message.

### FB-404 — Batch ID rejected at length 65

- **References** — Server Errors (10207).
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch with a 65-character `<batch_id>` and send the initial message.
- **Expected**
  - Error reply with `ErrCode 10207`.

### FB-405 — Append for unknown batch ID

- **References** — Server Errors (10208).
- **Preconditions** — Stream with `AllowBatchPublish: true`. No prior batch for `<uuid-X>`.
- **Steps**
  1. Send a single message with reply subject `<inbox>.<uuid-X>.10.ok.5.1.$FI` (append at sequence 5 with no prior open).
- **Expected**
  - Error reply with `ErrCode 10208`.

### FB-406 — Malformed reply subject (missing `$FI`)

- **References** — Server Errors (10206).
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Send a message with reply subject `<inbox>.<uuid>.10.ok.1.0` (no trailing `$FI`).
- **Expected**
  - Error reply with `ErrCode 10206`, OR the message is silently treated as a non-batch publish. The harness records which branch occurred. The server MUST NOT start a fast batch from a malformed reply subject.

---

## FB-500 — Gap detection in `gap=ok` mode

### FB-501 — Gap is reported via `BatchFlowGap` and the batch continues

- **References** — Message Gaps ("When `ok` the server will allow the gap, only send the gap message, and continue onward from the received sequence").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch (`flow=5`, `gap=ok`).
  2. Send the initial message (seq 1) and wait for the first `BatchFlowAck`.
  3. Send append seq 2.
  4. Skip seq 3; send append seq 4 directly.
  5. Continue to seq 10 with commit-store.
- **Expected**
  - A `BatchFlowGap` message is received with `last_seq=3` and `seq=4` (or, equivalently, the highest pre-gap server-side expected seq paired with the seq of the message that detected the gap).
  - The batch continues. Final PubAck `count` equals `10`.
  - Stream contains 9 messages (or fewer; the missing seq-3 message is permanently lost — the harness must not assume any specific count beyond `count = highest seq sent`).

### FB-502 — Multiple gaps in `ok` mode

- **References** — Message Gaps.
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Send seq 1, 2, 5, 6, 9, then commit-store at seq 10.
- **Expected**
  - At least two `BatchFlowGap` messages observed (gap at seq 3-4 and gap at seq 7-8).
  - Final PubAck succeeds; `count=10`.

### FB-503 — `BatchFlowGap` carries no flow update

- **References** — Message Gaps ("these gap messages can be sent out-of-order, these messages don't contain any flow updates or information").
- **Preconditions** — As FB-501.
- **Expected**
  - The `BatchFlowGap` body has only `type`, `last_seq`, and `seq`. The harness asserts there is no `msgs` field in the gap body.

---

## FB-600 — Gap detection in `gap=fail` mode

### FB-601 — Gap abandons the batch with a final PubAck

- **References** — Message Gaps ("When `fail` the server will abandon the batch and send the final ack back with `BatchSize` set to the last received sequence before the gap").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch (`flow=5`, `gap=fail`).
  2. Send seq 1, 2, 3.
  3. Skip seq 4; send seq 5 (gap-triggering append).
- **Expected**
  - A `BatchFlowGap` is received first.
  - Then a final `PubAck` arrives. The harness asserts `count=3` (the last received sequence before the gap).
  - The batch is closed: a subsequent append at seq 6 for the same batch ID returns `ErrCode 10208`.

### FB-602 — Idempotent: stop sending after gap

- **References** — Message Gaps ("The client will receive the gap message first, and should use this to stop sending messages before eventually receiving the final ack").
- **Preconditions** — As FB-601.
- **Steps**
  1. After receiving `BatchFlowGap`, send seq 6, 7, 8 anyway.
- **Expected**
  - These late messages are either silently dropped by the server or yield `ErrCode 10208` errors. The eventual `PubAck` from FB-601 still has `count` equal to the last pre-gap seq, regardless of the late traffic.

---

## FB-700 — Flow control behavior

### FB-701 — Initial `msgs` may be lower than requested `flow`

- **References** — Flow Control ("The flow rate will start with a low flow value. For example, the client requests a maximum flow of 100 messages, but the server starts at a flow of 1").
- **Preconditions** — Stream with `AllowBatchPublish: true`. The server's "first publisher" optimisation MAY return the requested flow immediately; the harness encodes both branches.
- **Steps**
  1. Open a batch with `flow=100`. Send the initial message.
- **Expected**
  - The first `BatchFlowAck.msgs` is `1 ≤ msgs ≤ 100`.

### FB-702 — Server never exceeds the requested upper bound

- **References** — Flow Control ("The server must treat the initial flow parameters as the upper bound").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Run a 200-message batch with `flow=10`.
- **Expected**
  - Every `BatchFlowAck.msgs` value observed is `≤ 10`. (Server may reduce, but must not exceed.)

### FB-703 — Server may ramp `msgs` upward

- **References** — Flow Control ("The server may ramp up the flow of a client by increasing the flow value").
- **Preconditions** — Stream with `AllowBatchPublish: true`. No other fast publishers.
- **Steps**
  1. Run a 500-message batch with `flow=64`.
- **Expected**
  - At least one observed `BatchFlowAck.msgs > first_msgs` (the server ramped up). This test is **inconclusive** rather than failing if the server chooses not to ramp under the test load — record the observed series.

### FB-704 — Lost ack does not stall

- **References** — Flow Control ("if acks for messages 10,20,30,40 and the one for 30 is lost - when the one for 40 comes the client must also treat the one for message 30 as seen").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Run a 100-message batch with `flow=10`. Have the harness deliberately drop the third `BatchFlowAck` from its in-process tracker.
- **Expected**
  - The fourth `BatchFlowAck` (with a higher `seq`) is sufficient to release any outstanding-ack stall. The batch commits cleanly with `count=100`. This test asserts on the **client harness** behaviour, but is included so the conformance suite explicitly covers the documented semantics.

---

## FB-800 — Per-message expected-header checks

### FB-801 — `Nats-Expected-Last-Sequence` mismatch in `gap=fail`

- **References** — Message Gaps ("In `fail` gap mode the error will commit/stop the batch. The final pub ack will contain the error, and no more messages are accepted in the batch after the batch sequence that triggered the error").
- **Preconditions** — Stream with `AllowBatchPublish: true`. Pre-publish a non-batch message; capture last sequence `S`.
- **Steps**
  1. Open a batch (`gap=fail`).
  2. Send seq 1 with header `Nats-Expected-Last-Sequence: S+99` (intentionally wrong).
  3. Send a few more appends.
  4. Send commit-store.
- **Expected**
  - A `BatchFlowErr` with `seq=1` and an `error` describing the wrong-last-sequence condition is received as soon as the server detects it.
  - The final `PubAck` carries an `error` (NOT `BatchFlowErr` — see ADR §"Server Errors": the rationale is that PubAck contains *either* error *or* persisted info but not both, and the client gets both via the separate `BatchFlowErr` and `PubAck`).
  - No messages from the batch are stored.
  - Subsequent appends are rejected with `ErrCode 10208`.

### FB-802 — `Nats-Expected-Last-Sequence` mismatch in `gap=ok`

- **References** — Message Gaps ("In `ok` gap mode the error will be sent to the client in the `BatchFlowGap` message").

> Note: ADR-50 §"Message Gaps" states the error is sent in a `BatchFlowGap` for `ok` mode; the §"Server Errors" prose introduces a separate `BatchFlowErr` envelope. The harness checks both shapes — first for `BatchFlowErr`, then `BatchFlowGap` with embedded error — and reports the observed shape so the spec can be tightened.

- **Preconditions** — As FB-801, but `gap=ok`.
- **Steps**
  1. Open a batch (`gap=ok`).
  2. Send seq 1 with `Nats-Expected-Last-Sequence: S+99` (wrong).
  3. Send 5 more appends with valid headers.
  4. Send commit-store.
- **Expected**
  - The harness observes one of:
    - A `BatchFlowErr` with `seq=1` and an `error` referencing wrong-last-sequence; the batch continues; the final `PubAck` succeeds with `count=7`.
    - A `BatchFlowGap` with `seq=1` carrying the error; the batch continues; the final `PubAck` succeeds with `count=7`.
  - Stream gains messages 2..7 (the failing message is not stored, but later messages are).

### FB-803 — `Nats-Expected-Last-Msg-Id` is rejected in fast batch

- **References** — Server Behavior Design ("Check properties like `ExpectedLastSeq` are handled as normal to be fully compatible with `Publish` and `PublishAsync`").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch.
  2. Send seq 1 with `Nats-Expected-Last-Msg-Id: foo`.
- **Expected**
  - The harness records the observed result: error returned via `BatchFlowErr` / `BatchFlowGap`, header silently ignored, or batch abandoned. Conformance currently treats this as **inconclusive** — the ADR cross-references atomic batch's hard rejection (10177) but does not explicitly forbid the header for fast batch.

---

## FB-900 — Ping operation

### FB-901 — Ping resends the latest flow control state

- **References** — Client Design ("the client may send a ping message to keep the batch alive and receive (missed) flow control messages").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch (`flow=10`, `gap=ok`). Send seq 1..15 (wait for the second `BatchFlowAck`).
  2. Drop the second ack from the harness's tracker (simulating loss).
  3. Send a ping with reply subject `<inbox>.<uuid>.10.ok.15.4.$FI` (sequence == highest seen).
- **Expected**
  - Within 1s the inbox receives a `BatchFlowAck` with `seq ≥ 10` (resent / latest state).
  - A subsequent append at seq 16 commits without stalling.

### FB-902 — Ping does NOT advance the batch sequence

- **References** — Client Design ("The sequence in the ping message must not itself increment the batch sequence; instead, it should be the highest batch sequence the client has sent").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch. Send seq 1, 2, 3.
  2. Send three pings, each with `<batch_seq>=3`.
  3. Send seq 4, then commit-store at seq 5.
- **Expected**
  - Final PubAck `count=5`. Stream contains 5 messages.
  - No `BatchFlowGap` was emitted (the pings did not register as gaps).

---

## FB-1000 — Idle abandonment

Fast batch does not raise advisories on abandonment; the only
observable signal is that subsequent appends with the same
`<batch_id>` are rejected as unknown.

### FB-1001 — Idle batch is abandoned after 10s

- **References** — Server Behavior Design ("Abandon, without error reply, anywhere a batch that has not had messages for 10 seconds"); Fast-ingest semantics ("Crucially these batches are not pre-staged").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch and send seq 1. Wait for the first `BatchFlowAck`.
  2. Sleep 12 seconds.
  3. Attempt an append at seq 2.
- **Expected**
  - The append in step 3 receives `ErrCode 10208` (batch unknown) — abandonment ends the batch *session*, so later appends with the same `<batch_id>` are rejected.
  - The stream STILL contains the pre-timeout initial message (seq 1). Fast batch does not stage messages — anything that received a `BatchFlowAck` before the idle period is already persisted and stays. Idle abandonment does NOT roll back the stream.

### FB-1002 — Idle timeout resets on traffic

- **References** — Server Behavior Design.
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch.
  2. Every 5 seconds for ≥ 15 seconds, append a message.
  3. Commit-store.
- **Expected**
  - All appends succeed; commit succeeds.

### FB-1003 — Ping resets the idle timer

- **References** — Client Design ("the client may send a ping message to keep the batch alive").
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch and send seq 1.
  2. Sleep 7 seconds. Send a ping at seq 1.
  3. Sleep 7 seconds. Append seq 2 and commit at seq 3.
- **Expected**
  - Total elapsed >10s but every quiescent gap is <10s. Commit succeeds with `count=3`.

---

## FB-1100 — Limits

### FB-1101 — Per-stream concurrent batch limit (1000)

- **References** — Server Behavior Design ("Each stream can only have 1,000 batches in flight at any time").
- **Preconditions** — Stream with `AllowBatchPublish: true`. This test is opt-in via the harness's resource-intensive flag (it briefly holds 1000 batches open).
- **Steps**
  1. Open 1000 batches concurrently (each: send only the initial message; wait for its first `BatchFlowAck`).
  2. Open a 1001st batch.
- **Expected**
  - The 1001st initial message receives an error indicating the per-stream batch limit was reached. Existing batches are unaffected.

### FB-1102 — Per-server concurrent batch limit (50,000)

- **References** — Server Behavior Design ("Each server can only have 50,000 batches in flight at any time").
- **Preconditions** — Many streams with `AllowBatchPublish: true` so the per-stream limit (1,000) does not bind. (Need ≥ 50 streams to reach 50,000.) Resource-intensive; opt-in.
- **Steps**
  1. Distribute 50,000 in-flight batches across enough streams to stay under the per-stream cap.
  2. Attempt to start a 50,001st batch on any stream.
- **Expected**
  - The 50,001st initial message receives an error indicating the server-wide batch limit.

### FB-1103 — No upper bound on per-batch message count

- **References** — Server Behavior Design ("There will be no maximum size for fast ingest batches").
- **Preconditions** — Stream with `AllowBatchPublish: true`. Opt-in resource-intensive test.
- **Steps**
  1. Run a single batch of 1,001 messages followed by commit-store.
- **Expected**
  - Commit succeeds with `count=1002`. (Notably, the atomic-batch limit of 1000 does not apply here.)

---

## FB-1200 — Leader change behavior

These tests require a clustered server (R3 stream).

### FB-1201 — Leader change in `gap=fail` mode abandons the batch

- **References** — Message Gaps ("In `fail` gap mode the new leader will abandon the batch (if a gap resulted from the leader change), send a `BatchFlowGap` out indicating the gap, and send back a final pub ack with details up to the last received message for the batch").
- **Preconditions** — A 3-replica stream with `AllowBatchPublish: true`. Identify the current leader.
- **Steps**
  1. Open a batch (`gap=fail`). Send seq 1..N (no commit yet).
  2. Step down the leader. Wait for a new leader.
  3. Continue sending seq N+1..N+5 against the new leader.
- **Expected**
  - Soon after the leader change, the inbox receives a `BatchFlowGap` (or no gap if no messages were lost in the transfer).
  - The inbox eventually receives a final `PubAck` with `count` equal to the last received pre-gap sequence on the new leader.
  - Subsequent appends from step 3 either receive `ErrCode 10208` (batch closed) OR are accepted as part of a NEW implicit batch — the harness asserts the *first* of those outcomes per ADR-50; the *second* would be a server bug.

### FB-1202 — Leader change in `gap=ok` mode continues

- **References** — Message Gaps ("In `ok` gap mode the new leader will continue and send a `BatchFlowGap` out indicating the gap").
- **Preconditions** — A 3-replica stream with `AllowBatchPublish: true`.
- **Steps**
  1. Open a batch (`gap=ok`). Send seq 1..50.
  2. Step down the leader. Wait for a new leader.
  3. Continue sending seq 51..100; commit-store at seq 101.
- **Expected**
  - A `BatchFlowGap` may be received around the leader-change boundary.
  - The batch continues. Final `PubAck` `count=101`.
  - Stream contains some subset of messages 1..100; the gap message (if any) reflects the lost range.

---

## FB-1300 — Mirrors and Sources behavior

### FB-1301 — Mirrors do not propagate fast-batch state

- **References** — Mirrors and Sources.
- **Preconditions** — A `SRC` stream with `AllowBatchPublish: true` and a mirror `MIR` of `SRC`.
- **Steps**
  1. Run a 5-message fast batch on `SRC`.
  2. Wait for the mirror to catch up.
- **Expected**
  - `MIR` contains all 5 messages.
  - The messages on `MIR` are ordinary (no fast-batch reply-subject reconstruction). The mirror does not attempt to re-batch.

### FB-1302 — Sources do not propagate fast-batch state

- **References** — Mirrors and Sources ("sources will ignore the batching headers when sourced into the stream").
- **Preconditions** — A `SRC` stream with `AllowBatchPublish: true` and a `DST` with `Sources: [{Name: SRC}]`.
- **Steps**
  1. Run a 5-message fast batch on `SRC`.
  2. Wait for `DST` to catch up.
- **Expected**
  - `DST` contains all 5 messages.
  - As with mirrors, the messages are ordinary on `DST`.

---

## FB-1400 — PubAck shape

### FB-1401 — `PubAck.batch` and `PubAck.count`

- **References** — Publish Acknowledgements; Server Behavior Design.
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Run a 7-message store-commit batch.
- **Expected**
  - PubAck `batch` equals the `<uuid>`.
  - PubAck `count` equals `7`.
  - PubAck `seq` equals the stream's last sequence.

### FB-1402 — `PubAck` is the only message *without* a `type` field

- **References** — Wire-level reference.
- **Preconditions** — Stream with `AllowBatchPublish: true`.
- **Steps**
  1. Run a 50-message batch with `flow=5`, `gap=ok`, deliberately introducing one gap to elicit a `BatchFlowGap`.
- **Expected**
  - Every message received on the inbox before the final commit has a `"type"` field of `"ack"`, `"gap"`, or `"err"`.
  - The final message has no `"type"` field but does have `seq`, `batch`, `count`. The harness uses the absence of `"type"` plus the presence of `count` to identify the PubAck.

---

## Out of scope

The following ADR-50 areas are intentionally **not** covered by this conformance document:

- Atomic Batch Publishing (`AllowAtomicPublish`, `Nats-Batch-*` headers, AB-* tests). Covered in [ADR-50-atomic-batch.md](ADR-50-atomic-batch.md).
- Specific client-library ergonomics for the `FastPublisher` / `AddMsg` / `CommitMsg` API. Conformance is asserted at the protocol layer (raw reply subjects and message types) so any client is testable.
- Throughput, latency, and resource-pressure characteristics. The harness validates correctness only.
- Mux-inbox subscription correctness. ADR-50 mandates old-style inboxes; the harness uses old-style inboxes throughout and does not exercise mux-inbox edge cases.

## Implementation notes for the harness

- **Determinism over speed** — tests should wait for flow-acks with a generous bound (e.g. 12s sleep + 3s slack for FB-1001) rather than racing on tight timeouts.
- **Inbox subscriptions** — each test opens a fresh old-style inbox and subscribes to `<inbox>.<batch_id>.>`. The harness must drain the subscription on test completion to release server-side interest promptly.
- **Cleanup** — every test deletes its stream(s) on completion. The harness must isolate state across tests so a crashed test cannot leak a `<uuid>` into a later one.
- **Reporting** — per-test result is `pass`, `fail`, `skip` (e.g. server below required version), or `inconclusive` (e.g. FB-202 / FB-703 / FB-803 / FB-1201 where multiple acceptable behaviors are allowed — record the observed branch).
- **Server version gating** — fast-ingest is introduced at 2.14.0 / API Level 4; the harness skips every FB-* test on older builds with a clear reason.
- **Replicated tests** — FB-1201 / FB-1202 require a cluster. Skip with reason on a single-server target.
- **Resource-intensive tests** — FB-1101 (per-stream limit, holds 1000 batches), FB-1102 (per-server limit, holds 50,000 batches), and FB-1103 (1001-message batch) should be opt-in via a flag.
- **Concurrent fast publishers** — many flow-control assertions only ramp / settle when no other fast publisher is competing. The harness should ensure no other fast-batch traffic is active during FB-700 tests.