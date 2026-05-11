# ADR-50 Conformance Tests — Atomic Batch Publishing

This document describes the conformance tests that validate a server implementation of the **Atomic Batch Publishing** feature defined in [ADR-50](../adr/ADR-50.md). It does **not** cover the Fast-ingest Batch Publishing portion of that ADR.

A conformance harness implementing these tests should be able to run them against any NATS server build claiming support for atomic batch publishing (introduced at server 2.12.0 / API Level 2; revised at 2.12.1 for deduplication and 2.14.0 / API Level 4 for committing without storing the commit message).

## How to read this document

Each test has the following shape:

- **ID** — stable identifier, used by the harness for reporting (`AB-NNN`).
- **Title** — one-line summary.
- **References** — the section of ADR-50 the test derives from.
- **Preconditions** — required server features, stream configuration, and any prior state.
- **Steps** — the actions the harness takes, expressed at the protocol level (headers, subject patterns, message values).
- **Expected** — the observable behavior the harness asserts on. Includes pub ack structure, error codes, stored stream state, and advisories.

A test passes only if every assertion in **Expected** holds. Where a test depends on another test's setup, that is called out in **Preconditions**.

## Common harness primitives

The harness needs the following building blocks. Implementations should provide them once and reuse them across tests.

- `new_stream(cfg)` — create a stream with the provided `StreamConfig`. Returns the stream name. Default config: `Subjects: ["TEST.>"]`, `Storage: file`, `Replicas: 1`, unless the test overrides these.
- `delete_stream(name)` — clean up.
- `publish_initial(stream, batch_id, seq=1, headers={}, subject="TEST.a", payload=b"")` — performs a NATS *request* (not a fire-and-forget publish) so the harness receives the server's reply. Sets `Nats-Batch-Id` and `Nats-Batch-Sequence` headers.
- `publish_member(stream, batch_id, seq, headers={}, subject="TEST.a", payload=b"", with_reply=False)` — publishes a non-initial, non-final batch member. With `with_reply=True`, the publish carries an inbox reply so the harness can observe a zero-byte ack.
- `publish_commit(stream, batch_id, seq, mode="store", headers={}, subject="TEST.a", payload=b"")` — final message. `mode="store"` sets `Nats-Batch-Commit:1`; `mode="eob"` sets `Nats-Batch-Commit:eob`. Always uses request semantics so the pub ack can be captured.
- `subscribe_advisories()` — subscribes to `$JS.EVENT.ADVISORY.>` and yields decoded advisories so tests can assert on `io.nats.jetstream.advisory.v1.stream_batch_abandoned`.
- `stream_msgs(stream)` — returns the messages currently stored in the stream, in order, with their headers — used to assert on the *committed* state of a batch.
- `stream_state(stream)` — returns last sequence, message count, and similar state.

The harness must use a fresh, unique `batch_id` per test (UUID v4) unless a test specifically exercises ID reuse or invalid IDs.

## Wire-level reference

The harness asserts directly on these wire-level identifiers — they must match exactly.

Headers (request side):

- `Nats-Batch-Id` — UUID, max 64 characters.
- `Nats-Batch-Sequence` — integer, starts at `1`, monotonically increases by one per batch member.
- `Nats-Batch-Commit` — `1` (store the final message and commit) or `eob` (commit without storing the final message).
- `Nats-Required-Api-Level` — checked on every batch message.
- `Nats-Expected-Last-Sequence` — accepted only on the first message of a batch.
- `Nats-Expected-Last-Subject-Sequence` — accepted, with caveats (see AB-410).
- `Nats-Expected-Last-Msg-Id` — currently rejected.
- `Nats-Msg-Id` — accepted from 2.12.1, used for deduplication.

Pub ack fields (added by this ADR):

- `batch` (`BatchId`) — string.
- `count` (`BatchSize`) — integer.

Advisory:

- Subject prefix: `$JS.EVENT.ADVISORY.STREAM.BATCH_ABANDONED.<stream>` (or however the implementation routes JetStream advisories — the harness should subscribe to `$JS.EVENT.ADVISORY.>` and filter by event type).
- Type: `io.nats.jetstream.advisory.v1.stream_batch_abandoned`.
- Fields: `batch` (the `BatchId`), `reason` ∈ `{timeout, large, incomplete, unsupported}`. `large` is emitted when the batch is abandoned because a member's `Nats-Batch-Sequence` exceeds the per-batch limit (10199); it pairs with the same error pub ack returned to the offending publish.

Server error codes (atomic batch only):

| ErrCode | Code | Meaning                                                     |
|---------|------|-------------------------------------------------------------|
| 10174   | 400  | Batch publish not enabled on stream                         |
| 10175   | 400  | Batch publish sequence is missing                           |
| 10176   | 400  | Batch publish is incomplete and was abandoned               |
| 10177   | 400  | Batch publish unsupported header used                       |
| 10179   | 400  | Batch publish ID is invalid (exceeds 64 characters)         |
| 10199   | 400  | Batch publish sequence exceeds server limit (default 1000)  |
| 10201   | 400  | Batch publish contains duplicate message id (`Nats-Msg-Id`) |

---

## AB-100 — Stream configuration

### AB-101 — Enabling `AllowAtomicPublish` works

- **References** — Stream Configuration.
- **Preconditions** — None.
- **Steps**
  1. Create a stream with `AllowAtomicPublish: true`.
  2. Read back the stream configuration.
- **Expected**
  - Stream creation succeeds.
  - `AllowAtomicPublish` is reported as `true`.
  - The stream's reported API level is at least `2`.

### AB-102 — `AllowAtomicPublish` defaults off

- **References** — Stream Configuration; AB-201 (depends on this default).
- **Preconditions** — None.
- **Steps**
  1. Create a stream without specifying `AllowAtomicPublish`.
  2. Attempt to start a batch (`AB-201`-style minimal flow).
- **Expected**
  - Stream creation succeeds.
  - The initial batch publish receives an error pub ack with `ErrCode 10174`.

### AB-103 — `AllowAtomicPublish` toggles via update

- **References** — Stream Configuration ("These settings can be disabled and enabled using configuration updates").
- **Preconditions** — Stream created with `AllowAtomicPublish: false`.
- **Steps**
  1. Update the stream to set `AllowAtomicPublish: true`.
  2. Run a minimal commit batch (single message + commit) and assert it succeeds.
  3. Update the stream back to `AllowAtomicPublish: false`.
  4. Attempt another initial batch publish.
- **Expected**
  - Step 2 produces a successful pub ack with `batch` and `count`.
  - Step 4 produces an error pub ack with `ErrCode 10174`.

### AB-104 — `AllowAtomicPublish` rejected with `PersistMode: async`

- **References** — Stream Configuration ("Setting `AllowAtomicPublish` and `PersistMode: async` must error").
- **Preconditions** — None.
- **Steps**
  1. Attempt to create a stream with `AllowAtomicPublish: true` and `PersistMode: async`.
  2. Attempt to update an existing async-mode stream to set `AllowAtomicPublish: true`.
- **Expected**
  - Both attempts return a stream-config error from the server. The exact API error wording is server-defined, but the operation MUST fail.

### AB-105 — Mirrors cannot enable `AllowAtomicPublish`

- **References** — Mirrors and Sources.
- **Preconditions** — A source stream `SRC` exists.
- **Steps**
  1. Attempt to create a mirror stream with `Mirror: {Name: SRC}` and `AllowAtomicPublish: true`.
- **Expected**
  - Stream creation fails. (Per ADR: "Mirrors can't enable these settings".)

### AB-106 — Sources may enable `AllowAtomicPublish`, but ignore batching headers when sourcing

- **References** — Mirrors and Sources.
- **Preconditions** — A stream `SRC` exists with `AllowAtomicPublish: true`.
- **Steps**
  1. Create a stream `DST` with `Sources: [{Name: SRC}]` and `AllowAtomicPublish: true`. Assert this succeeds.
  2. Run a complete commit batch (`Nats-Batch-Commit:1`) of three messages on `SRC`.
  3. Wait for the messages to be sourced into `DST`.
  4. Inspect the messages on `DST`.
- **Expected**
  - All three messages appear on `DST`.
  - On `DST`, the `Nats-Batch-Id`, `Nats-Batch-Sequence`, and `Nats-Batch-Commit` headers are absent (sources ignore batching headers).

---

## AB-200 — Single-message batches and the happy path

### AB-201 — Minimal store-commit batch (one message)

- **References** — Client Design; Publish Acknowledgements.
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Send a single message as a request with headers `Nats-Batch-Id:<uuid>`, `Nats-Batch-Sequence:1`, `Nats-Batch-Commit:1`.
- **Expected**
  - The reply is a successful pub ack (no `error` field).
  - Pub ack `batch` equals the `<uuid>`.
  - Pub ack `count` equals `1`.
  - Pub ack `seq` equals the stream's last sequence.
  - The stream contains exactly one message; its stored headers include `Nats-Batch-Id` and `Nats-Batch-Sequence:1`. The stored message MUST carry `Nats-Batch-Commit:1` (the server marks the last stored message of a batch with this header).

### AB-202 — Minimal eob-commit batch (one stored message + EOB sentinel)

- **References** — Client Design ("commit without storing the commit message").
- **Preconditions** — Stream with `AllowAtomicPublish: true` on a server at API Level ≥ 4.
- **Steps**
  1. Initial publish (request): `Nats-Batch-Id:<uuid>`, `Nats-Batch-Sequence:1`. Subject `TEST.a`, payload `data`.
  2. Final publish (request): `Nats-Batch-Id:<uuid>`, `Nats-Batch-Sequence:2`, `Nats-Batch-Commit:eob`. Subject `TEST.eob`, payload `ignored`.
- **Expected**
  - Step 1 reply is a zero-byte ack (no error).
  - Step 2 reply is a successful pub ack with `batch=<uuid>`, `count=1`, `seq=<final stream seq>` — the EOB sentinel does NOT count toward `BatchSize`.
  - The stream contains exactly **one** message (the EOB sentinel is not stored).
  - The single stored message has `Nats-Batch-Sequence:1` and is updated by the server to carry `Nats-Batch-Commit:1` (the marker for "last stored message of the batch").

### AB-203 — Multi-message store-commit batch

- **References** — Client Design.
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Empty stream.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`, payload `a`. Use request semantics.
  2. Member: `Nats-Batch-Sequence:2`, payload `b`. With reply.
  3. Member: `Nats-Batch-Sequence:3`, payload `c`. Without reply.
  4. Commit: `Nats-Batch-Sequence:4`, `Nats-Batch-Commit:1`, payload `d`. With reply.
- **Expected**
  - Step 1 reply: zero-byte ack.
  - Step 2 reply: zero-byte ack.
  - Step 3 has no reply, so nothing is asserted.
  - Step 4 reply: successful pub ack with `batch=<uuid>`, `count=4`.
  - Stream contains exactly four messages, in order, with payloads `a`, `b`, `c`, `d`.
  - Each stored message has `Nats-Batch-Id=<uuid>` and `Nats-Batch-Sequence` matching its position. Only the *last* stored message carries `Nats-Batch-Commit:1`.

### AB-204 — Pub ack `seq` references the final stored message

- **References** — Server Behavior Design ("The sequence in the ack would be the final message sequence, previous messages in the batch would be the preceding sequences").
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Stream's last sequence is `S0` before the test.
- **Steps**
  1. Run a 5-message store-commit batch.
- **Expected**
  - Pub ack `seq` equals `S0 + 5`.
  - Stream sequences `S0+1 .. S0+5` correspond to batch sequences 1..5 in order.

### AB-205 — Member ack omission is permitted

- **References** — Client Design ("might optionally include a reply subject").
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Three-message store-commit batch where members at sequences 2 and 3 are published *without* a reply subject (fire-and-forget).
- **Expected**
  - The commit produces a successful pub ack with `count=3`.
  - The harness observed no acks for the no-reply members (correctly silent — not asserted positively, but the absence of ack on those publishes must not cause the batch to fail).

---

## AB-300 — Batch identifiers, sequence numbers, and operations

### AB-301 — Batch ID accepted at boundary length 64

- **References** — Server Behavior Design ("limit the `Nats-Batch-ID` to 64 characters").
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Use a `Nats-Batch-Id` exactly 64 characters long. Run a single-message store-commit batch.
- **Expected**
  - Successful pub ack. Stored message carries the full 64-character `Nats-Batch-Id`.

### AB-302 — Batch ID rejected at length 65

- **References** — Server Errors (10179).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial publish with a 65-character `Nats-Batch-Id`.
- **Expected**
  - Error pub ack with `ErrCode 10179`, code `400`.

### AB-303 — Sequence missing on initial message

- **References** — Server Errors (10175).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Send an initial message with `Nats-Batch-Id` set but **no** `Nats-Batch-Sequence` header.
- **Expected**
  - Error pub ack with `ErrCode 10175`.

### AB-304 — Sequence missing on a member message

- **References** — Server Errors (10175).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Send a valid initial message at sequence `1`.
  2. Send a member message with `Nats-Batch-Id` set but no `Nats-Batch-Sequence`.
- **Expected**
  - Step 2 produces an error pub ack with `ErrCode 10175`.
  - The batch is abandoned; a `stream_batch_abandoned` advisory is raised.
  - Final stream state contains no messages from this batch.

### AB-305 — Initial message must be sequence 1

- **References** — Server Behavior Design ("any gap is detected the batch will be rejected"); Client Design (sequence starts at 1).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Send a request with `Nats-Batch-Id:<uuid>`, `Nats-Batch-Sequence:2` (no prior batch state on the server).
- **Expected**
  - Error pub ack. The server treats this as an unknown/incomplete batch. A subsequent `Nats-Batch-Sequence:1` for the same `<uuid>` MAY be required to start a new batch; the harness asserts only that the sequence-2-first message does not silently start a batch and is rejected.

### AB-306 — Sequence gap mid-batch is rejected

- **References** — Server Behavior Design ("if any gap is detected the batch will be rejected with an error Pub Ack").
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`.
  2. Member with reply: `Nats-Batch-Sequence:3`.
- **Expected**
  - Step 2 produces an error pub ack with `ErrCode 10176`.
  - Stream contains no messages from this batch.
  - A `stream_batch_abandoned` advisory may be raised with `reason: incomplete` (assert presence).

### AB-307 — Repeated sequence is rejected

- **References** — Server Behavior Design (gap / sequence integrity).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`.
  2. Member with reply: `Nats-Batch-Sequence:1` (duplicate).
- **Expected**
  - Step 2 produces an error pub ack (the harness asserts an error reply, not a specific code beyond a 400). Stream contains no messages from this batch.

### AB-308 — Decreasing sequence is rejected

- **References** — Server Behavior Design.
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`.
  2. Member with reply: `Nats-Batch-Sequence:3`.
  3. Member with reply: `Nats-Batch-Sequence:2`.
- **Expected**
  - Either step 2 or step 3 produces an error pub ack (server rejects the gap or out-of-order). Final stream state has no messages from this batch.

### AB-309 — Unknown batch ID is rejected

- **References** — Server Behavior Design ("Server will reject messages for which the batch is unknown").
- **Preconditions** — Stream with `AllowAtomicPublish: true`. No prior batch with `<uuid-X>` exists.
- **Steps**
  1. Publish a member with reply at `Nats-Batch-Sequence:5` for `Nats-Batch-Id:<uuid-X>` (no prior initial).
- **Expected**
  - Error pub ack. Stream contains no messages.

### AB-310 — Sequence exceeds server limit (1000)

- **References** — Server Errors (10199); Server Behavior Design ("Each batch can have maximum 1000 messages"); Abandonment Advisories.
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Server limit is the default 1000.
- **Steps**
  1. Run a batch publishing sequences 1..1000 (members), then attempt `Nats-Batch-Sequence:1001` as a member with reply (or as the commit).
- **Expected**
  - The 1001st message produces an error pub ack with `ErrCode 10199`.
  - Stream contains none of the 1000 staged messages (batch abandoned).
  - A `stream_batch_abandoned` advisory is raised with `reason: "large"`.

### AB-311 — Sequence exactly at limit (1000)

- **References** — Server Behavior Design ("Each batch can have maximum 1000 messages").
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Default limit.
- **Steps**
  1. Run a batch where the commit is at `Nats-Batch-Sequence:1000` (`Nats-Batch-Commit:1`).
- **Expected**
  - Successful pub ack with `count=1000`. Stream contains 1000 messages from this batch.

---

## AB-400 — Stream-state checks at commit

### AB-401 — `Nats-Expected-Last-Sequence` matches at commit

- **References** — Server Behavior Design (expected-sequence check at commit, under lock).
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Pre-publish a single non-batch message and capture its sequence `S`.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`, header `Nats-Expected-Last-Sequence:S`.
  2. Member, member, commit (sequences 2..N).
- **Expected**
  - Successful pub ack. Stream messages following `S` correspond to the batch.

### AB-402 — `Nats-Expected-Last-Sequence` mismatch fails the batch

- **References** — Server Behavior Design ("Check properties like `ExpectedLastSeq` using the sequences found in the stream prior to the batch, at the time when the batch is committed under lock for consistency. Rejects the batch with an error Pub Ack if any message fails these checks, **when the batch tries to commit**").
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Stream last sequence is `S`.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`, `Nats-Expected-Last-Sequence:S+99` (intentionally wrong).
  2. Send a member: `Nats-Batch-Sequence:2` (with reply, so the harness can observe the response).
  3. Send the commit: `Nats-Batch-Sequence:3`, `Nats-Batch-Commit:1`.
- **Expected**
  - Step 1 reply is a zero-byte ack — the server MUST NOT surface the wrong-last-sequence condition before the batch tries to commit (the check is performed under lock at commit time, against pre-batch sequences).
  - Step 2 reply is a zero-byte ack.
  - Step 3 pub ack carries an `error` describing the wrong-last-sequence condition.
  - Stream contains no messages from this batch.

### AB-403 — `Nats-Expected-Last-Sequence` racing with concurrent publish

- **References** — Server Behavior Design ("Check properties like `ExpectedLastSeq` using the sequences found in the stream prior to the batch, at the time when the batch is committed under lock").
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Stream last sequence is `S`.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`, `Nats-Expected-Last-Sequence:S`.
  2. Send a few members (do not commit yet).
  3. From a parallel client, publish a non-batch message to the same stream so the last sequence becomes `S+1`.
  4. Send the commit message for the batch.
- **Expected**
  - The commit fails with a wrong-last-sequence error in the pub ack (the server compares against the *current* last non-batch sequence at commit time, under lock).
  - The stream contains the parallel non-batch message but **none** of the batch members.

### AB-404 — `Nats-Expected-Last-Sequence` only allowed on the first message

- **References** — Server Behavior Design ("Only the first message of the batch may contain `Nats-Expected-Last-Sequence`"; ExpectedLastSeq checks happen "when the batch tries to commit").
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`.
  2. Member with reply: `Nats-Batch-Sequence:2` carrying `Nats-Expected-Last-Sequence:N`.
  3. Commit: `Nats-Batch-Sequence:3`, `Nats-Batch-Commit:1`.
- **Expected**
  - One of three branches is acceptable; the harness records which branch the server chose:
    - **(a) Early rejection**: step 2 returns an error pub ack referencing the disallowed header. Batch abandoned, no messages stored.
    - **(b) Commit-time rejection**: step 2 returns a zero-byte ack; step 3 returns an error pub ack referencing the disallowed header or a wrong-last-sequence condition. Batch abandoned, no messages stored.
    - **(c) Silent ignore**: step 2 returns a zero-byte ack; step 3 commits cleanly with `count=3`. The header is ignored on the non-initial member — the rule "only the first message may contain it" is enforced by ignoring it elsewhere. The harness reports this branch as INCONCLUSIVE since the spec doesn't explicitly require silent acceptance, only that the header take effect only on the first message.
  - Whichever branch the server takes, it MUST NOT use the wrong-message header value to evaluate ExpectedLastSeq against the stream.

### AB-405 — `Nats-Expected-Last-Msg-Id` rejected

- **References** — Server Errors (10177); Stream State Constraints.
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`, `Nats-Expected-Last-Msg-Id:foo`.
- **Expected**
  - Error pub ack with `ErrCode 10177`.
  - Stream contains no messages from this batch.

### AB-410 — `Nats-Expected-Last-Subject-Sequence` happy path

- **References** — Server Behavior Design ("Checks using `Nats-Expected-Last-Subject-Sequence` can only be performed if prior entries in the batch do not also write to that same subject").
- **Preconditions** — Stream with `AllowAtomicPublish: true`, subjects `TEST.>`. Pre-publish a non-batch message on subject `TEST.x`; capture its stream sequence `Sx`.
- **Steps**
  1. Initial on subject `TEST.x`: `Nats-Batch-Sequence:1`, `Nats-Expected-Last-Subject-Sequence:Sx`.
  2. Commit on subject `TEST.x`: `Nats-Batch-Sequence:2`, `Nats-Batch-Commit:1`.
- **Expected**
  - Successful pub ack with `count=2`.

### AB-411 — `Nats-Expected-Last-Subject-Sequence` mismatch

- **References** — Server Behavior Design.
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Pre-publish a non-batch message on subject `TEST.x`; capture sequence `Sx`.
- **Steps**
  1. Initial on `TEST.x`: `Nats-Batch-Sequence:1`, `Nats-Expected-Last-Subject-Sequence:Sx+10` (wrong).
  2. Commit on `TEST.x`: `Nats-Batch-Sequence:2`, `Nats-Batch-Commit:1`.
- **Expected**
  - The commit pub ack carries an error indicating wrong-last-subject-sequence. Stream has no messages from this batch.

### AB-412 — `Nats-Expected-Last-Subject-Sequence` skipped when the batch wrote that subject earlier

- **References** — Server Behavior Design ("can only be performed if prior entries in the batch do not also write to that same subject").
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Pre-publish a non-batch message on `TEST.x`; capture `Sx`.
- **Steps**
  1. Initial on `TEST.x`: `Nats-Batch-Sequence:1` (no expected-subject header).
  2. Member with reply on `TEST.x`: `Nats-Batch-Sequence:2`, `Nats-Expected-Last-Subject-Sequence:Sx`.
  3. Commit on `TEST.y`: `Nats-Batch-Sequence:3`, `Nats-Batch-Commit:1`.
- **Expected**
  - The harness asserts that the server's behavior is one of:
    - It rejects step 2 with an error (because a prior batch entry already wrote `TEST.x`), abandoning the batch; **or**
    - It silently skips the per-subject check for step 2 and the commit succeeds.
  - The implementation MUST be self-consistent across runs. Whichever it does, the stream MUST NOT end up in a state where the per-subject expectation was honored against pre-batch data while the batch already shadowed it.
  - This test is informational with respect to the ADR — the conformance suite reports the observed branch and flags it; both branches are acceptable per the current spec text.

---

## AB-500 — Deduplication (`Nats-Msg-Id`)

These tests apply only on servers at 2.12.1 or later, where deduplication is supported within batches.

### AB-501 — Unique `Nats-Msg-Id` across a batch is accepted

- **References** — Stream State Constraints ("Starting from 2.12.1 de-duplication is supported").
- **Preconditions** — Stream with `AllowAtomicPublish: true`, default deduplication window.
- **Steps**
  1. Run a 3-message store-commit batch where each message has a distinct `Nats-Msg-Id` (`m1`, `m2`, `m3`).
- **Expected**
  - Successful pub ack with `count=3`. All three messages stored, each with its `Nats-Msg-Id` retained.

### AB-502 — Duplicate `Nats-Msg-Id` *within* a batch is rejected

- **References** — Server Errors (10201).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial: `Nats-Msg-Id:dup`.
  2. Member with reply: `Nats-Batch-Sequence:2`, `Nats-Msg-Id:dup`.
- **Expected**
  - The duplicate-detection error surfaces with `ErrCode 10201` (either on step 2 or at commit time, depending on the server). Whichever message produces the error, the batch is abandoned; stream contains no messages from this batch.

### AB-503 — `Nats-Msg-Id` colliding with an existing stream message

- **References** — Stream State Constraints (dedup interacts with normal stream-level dedupe).
- **Preconditions** — Stream with `AllowAtomicPublish: true`, default dedup window. Pre-publish a non-batch message with `Nats-Msg-Id:keep`.
- **Steps**
  1. Initial: `Nats-Msg-Id:keep`.
  2. Member, commit.
- **Expected**
  - The batch fails dedup against the existing message at commit time. Pub ack carries the standard duplicate-message indication. Stream contains only the original pre-batch message; none of the batch members are stored.

---

## AB-600 — Required API level

### AB-601 — `Nats-Required-Api-Level` satisfied

- **References** — Client Design ("The server will check `Nats-Required-Api-Level` for every batch related message").
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Server's API level is `L`.
- **Steps**
  1. Run a 3-message store-commit batch where every message carries `Nats-Required-Api-Level:L`.
- **Expected**
  - Successful pub ack with `count=3`. Stream contains all three messages.

### AB-602 — `Nats-Required-Api-Level` unsatisfied on initial message

- **References** — Client Design (required API level check abandons batch with advisory).
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Server's API level is `L`.
- **Steps**
  1. Initial (request): `Nats-Batch-Sequence:1`, `Nats-Required-Api-Level:L+99`.
- **Expected**
  - Error pub ack on the initial message.
  - A `stream_batch_abandoned` advisory is raised with `reason: unsupported`.
  - Stream contains no messages.

### AB-603 — `Nats-Required-Api-Level` unsatisfied on a later member with reply

- **References** — Client Design ("if a reply is set a full error ack is sent").
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial: ok.
  2. Member with reply: `Nats-Required-Api-Level:L+99`.
- **Expected**
  - Step 2 produces a full error pub ack.
  - Advisory `stream_batch_abandoned` with `reason: unsupported` raised.
  - Stream contains no messages from this batch.

### AB-604 — `Nats-Required-Api-Level` unsatisfied on a member without reply

- **References** — Client Design ("if a reply is set a full error ack is sent" — so without a reply, no error is sent, but the batch is still abandoned).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial: ok.
  2. Member without reply: `Nats-Required-Api-Level:L+99`.
  3. Wait for the abandonment advisory (or for the idle timeout, whichever is earlier).
  4. Attempt a commit at the next sequence.
- **Expected**
  - No error reply is observed for step 2 (no reply was set).
  - A `stream_batch_abandoned` advisory with `reason: unsupported` is raised within a few seconds.
  - The commit attempt in step 4 fails (batch is unknown). Stream contains no messages from this batch.

---

## AB-700 — Commit semantics and EOB

### AB-701 — `Nats-Batch-Commit:1` finalizes the batch with the final message stored

- **References** — Client Design.
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Run a 3-message batch where the third message has `Nats-Batch-Commit:1`.
- **Expected**
  - Stored stream contains exactly 3 messages. The 3rd stored message carries `Nats-Batch-Commit:1`.

### AB-702 — `Nats-Batch-Commit:eob` finalizes the batch without storing the final message

- **References** — Client Design (revision 5, API Level 4).
- **Preconditions** — Stream with `AllowAtomicPublish: true` on a server at API Level ≥ 4.
- **Steps**
  1. Initial (`Nats-Batch-Sequence:1`), member (`Nats-Batch-Sequence:2`), commit with `Nats-Batch-Commit:eob` (`Nats-Batch-Sequence:3`).
- **Expected**
  - Pub ack `count=2` — the EOB sentinel does NOT count toward `BatchSize` (per ADR-50 revision: "the pub ack's `BatchSize` will reflect the messages in the batch, without counting the EOB message").
  - Stream contains exactly 2 messages.
  - The last stored message (the one at batch sequence 2) was rewritten by the server to carry `Nats-Batch-Commit:1`.
  - The stream's last-sequence advances by 2, not 3. The pub ack `seq` is the stream sequence of that last stored message.

### AB-703 — `Nats-Batch-Commit:eob` on the very first message

- **References** — Client Design (single-message EOB edge case).
- **Preconditions** — Stream with `AllowAtomicPublish: true`, API Level ≥ 4.
- **Steps**
  1. Initial publish (request) with `Nats-Batch-Sequence:1` and `Nats-Batch-Commit:eob`.
- **Expected**
  - The harness asserts an unambiguous outcome: either a successful pub ack with `count=0` and **zero** messages stored (preferred — EOB applied; per the EOB-doesn't-count rule, `BatchSize` is `0` when only the EOB message was sent), or an error pub ack. The server MUST NOT silently store the message.
  - The conformance suite records which branch the server chose; both are acceptable until the ADR clarifies. Inconsistent behavior across runs is a failure.

### AB-704 — `Nats-Batch-Commit` with an unknown value

- **References** — Server Behavior Design ("Server will reject any operation that it does not know about" — applied here to commit values).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial.
  2. Final with `Nats-Batch-Commit:nope`.
- **Expected**
  - Error pub ack on the final message. Stream contains no messages from this batch.

---

## AB-800 — Idle abandonment and limits

### AB-801 — Idle batch is abandoned after 10s with an advisory

- **References** — Server Behavior Design ("Abandon without error reply anywhere a batch that has not had messages for 10 seconds, an advisory will be raised").
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`. Wait for the zero-byte ack.
  2. Sleep 12 seconds.
  3. Attempt a member with reply at `Nats-Batch-Sequence:2`.
- **Expected**
  - During the sleep, an advisory `io.nats.jetstream.advisory.v1.stream_batch_abandoned` is published with `batch=<uuid>` and `reason: timeout`.
  - Step 3 produces an error pub ack (batch unknown).
  - Stream contains no messages from this batch.

### AB-802 — Idle abandonment without a reply produces no error reply

- **References** — Server Behavior Design ("without error reply").
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial: `Nats-Batch-Sequence:1`.
  2. Member *without* reply at `Nats-Batch-Sequence:2`.
  3. Sleep 12 seconds.
- **Expected**
  - No error reply is delivered to the harness (no reply was set on either the abandonment-triggering message or any subsequent traffic).
  - Advisory `stream_batch_abandoned` with `reason: timeout` is raised.
  - Stream contains no messages from this batch.

### AB-803 — Per-stream concurrent batch limit (50)

- **References** — Server Behavior Design ("Each stream can only have 50 batches in flight at any time").
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Open 50 batches concurrently (each: send only the initial message, do not commit). Assert all 50 initial messages succeed.
  2. Send the initial message of a 51st batch.
- **Expected**
  - The 51st initial publish produces an error pub ack indicating the per-stream batch limit was reached. Existing batches are unaffected.
  - Cleanup: commit one of the 50 batches and then the 51st batch's initial publish should succeed.

### AB-804 — Per-server concurrent batch limit (1000)

- **References** — Server Behavior Design ("Each server can only have 1,000 batches in flight at any time").
- **Preconditions** — Many streams with `AllowAtomicPublish: true` so the per-stream limit (50) does not bind. (Need ≥ 21 streams to reach 1000.)
- **Steps**
  1. Distribute 1000 in-flight batches across enough streams to stay under the per-stream cap.
  2. Attempt to start a 1001st batch on any stream.
- **Expected**
  - The 1001st initial publish fails with an error pub ack indicating the server-wide batch limit. This test may be marked as resource-intensive; the harness may run it only when explicitly opted in.

### AB-805 — Idle timeout resets on traffic

- **References** — Server Behavior Design ("not had messages for 10 seconds").
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Initial.
  2. Every 5 seconds, publish a member with reply (sequences 2, 3, 4) — total elapsed > 10s, but no idle gap > 10s.
  3. Commit.
- **Expected**
  - All members receive zero-byte acks. The commit pub ack succeeds with `count=4`. Stream contains 4 messages. No `stream_batch_abandoned` advisory is observed for this batch.

---

## AB-900 — Replicated and clustered behavior

These tests require a clustered server (R3 stream).

### AB-901 — Atomic batch survives leader change before commit

- **References** — Atomic Batched Publishes (atomicity guarantee).
- **Preconditions** — A 3-replica stream with `AllowAtomicPublish: true`. Identify the current leader.
- **Steps**
  1. Start a 5-message batch (initial through sequence 5, no commit yet).
  2. Step down the leader (admin API).
  3. Wait for a new leader.
  4. Send the commit message.
- **Expected**
  - The harness asserts one of two well-defined outcomes:
    - The batch is abandoned during the leader change: an advisory is raised, the commit fails with an "unknown batch" error pub ack, and the stream has zero messages from this batch (atomicity preserved). **Or**
    - The new leader inherited the in-flight batch state and the commit succeeds normally with `count=6` and 6 stored messages.
  - The MUST condition is atomicity: the stream NEVER ends up with a partial batch (1 ≤ stored < expected). Either everything or nothing.
  - The conformance suite records which branch occurred and reports it; both are acceptable per the ADR's "stored or abandoned" guarantee.

### AB-902 — Atomic batch atomicity under leader change after commit

- **References** — Atomicity guarantee.
- **Preconditions** — A 3-replica stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Run a 5-message store-commit batch and capture the pub ack.
  2. Immediately step down the leader.
  3. From a follower-promoted-leader, read back the stream.
- **Expected**
  - Stream contains exactly 5 messages from this batch on the new leader (the commit was acknowledged, so it is durable).
  - Stream's last sequence matches the pub ack `seq`.

---

## AB-1000 — Advisories

### AB-1001 — `stream_batch_abandoned` event shape

- **References** — Abandonment Advisories.
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Subscription to advisories active.
- **Steps**
  1. Cause a `timeout` abandonment (AB-801 setup) and capture the advisory.
- **Expected**
  - Advisory subject contains `BATCH_ABANDONED` (case as published by the server).
  - Advisory `type` field is `io.nats.jetstream.advisory.v1.stream_batch_abandoned`.
  - Advisory body has `batch` equal to the `<uuid>` and `reason` equal to `"timeout"`.
  - Advisory carries the standard JetStream advisory fields (`id`, `timestamp`, `stream`).

### AB-1002 — Advisory `reason: incomplete`

- **References** — Abandonment Advisories.
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Trigger an incomplete abandonment by publishing a sequence-gap mid-batch (AB-306 setup).
- **Expected**
  - A `stream_batch_abandoned` advisory is raised with `reason: "incomplete"`.

### AB-1003 — Advisory `reason: unsupported`

- **References** — Abandonment Advisories; AB-602 / AB-603.
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Trigger an unsupported-API-level abandonment (AB-602 setup).
- **Expected**
  - A `stream_batch_abandoned` advisory is raised with `reason: "unsupported"`.

### AB-1004 — Advisory `reason: large`

- **References** — Abandonment Advisories; AB-310.
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Default per-batch limit (1000). Resource-intensive: marked `--resource-intensive` in the harness because it has to push 1001 messages to drive the batch over the limit.
- **Steps**
  1. Subscribe to advisories.
  2. Publish the initial batch member at `Nats-Batch-Sequence:1`, then sequences 2..1000 as members, then `Nats-Batch-Sequence:1001` with reply.
- **Expected**
  - The 1001st publish receives an error pub ack with `ErrCode 10199`.
  - A `stream_batch_abandoned` advisory is raised with `reason: "large"` and `batch` equal to the test's `<uuid>`.
  - Stream contains none of the staged batch messages.

---

## AB-1100 — Negative interaction with normal publishing

### AB-1101 — Non-batch publish to the same subject during an open batch

- **References** — Server Behavior Design (concurrency at commit time).
- **Preconditions** — Stream with `AllowAtomicPublish: true`. Empty stream.
- **Steps**
  1. Initial of batch on `TEST.x`.
  2. From a parallel client, publish a non-batch message to `TEST.x`. Capture its sequence.
  3. Send members and commit on `TEST.x`.
- **Expected**
  - The non-batch publish from step 2 succeeds independently and lives in the stream (it is interleaved between batch start and batch commit).
  - The batch commit succeeds; batch members appear in the stream after the non-batch publish.
  - The batch's final pub ack `seq` is `(non-batch seq) + (batch member count)`. Order: batch initial-time arrival is irrelevant; only the commit-time ordering matters.

### AB-1102 — Two concurrent batches on the same stream

- **References** — Atomic Batched Publishes (multi-batch concurrency).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Open batch A (initial + 2 members).
  2. Open batch B (initial + 2 members) — different `<uuid>`.
  3. Commit A.
  4. Commit B.
- **Expected**
  - Both commits succeed. Stream contains all 6 messages (3 from A, 3 from B).
  - Within each batch, the messages appear consecutively. The commit order in the stream matches the commit order over the wire (A before B).
  - Pub ack `count` is `3` for each batch.

---

## AB-1200 — Header / payload edge cases

### AB-1201 — Empty payload allowed

- **References** — Client Design (no restriction on payload).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Run a 2-message store-commit batch where both payloads are empty (`b""`).
- **Expected**
  - Both messages stored with empty payloads. Pub ack `count=2`.

### AB-1202 — Non-batch headers preserved across the batch

- **References** — General message handling (batching does not strip user headers).
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Run a 2-message store-commit batch where each message carries an unrelated header `X-Test-Tag: value-N`.
- **Expected**
  - Each stored message retains its `X-Test-Tag` header alongside the batch headers. Pub ack `count=2`.

### AB-1203 — `Nats-Batch-Commit:1` only honored on a message that carries `Nats-Batch-Id`

- **References** — Client Design.
- **Preconditions** — Stream with `AllowAtomicPublish: true`.
- **Steps**
  1. Publish a message with `Nats-Batch-Commit:1` but no `Nats-Batch-Id` and no `Nats-Batch-Sequence`. Use request semantics.
- **Expected**
  - The message is treated as a normal publish: it either succeeds as a non-batch message (no `Batch` / `Count` in the pub ack) or fails with a normal publish error. The server MUST NOT treat this as a batch commit.
  - Stream contains the message (or not, depending on stream subjects) but its stored headers do not include batch markers added by the server.

---

## Out of scope

The following ADR-50 areas are intentionally **not** covered by this conformance document:

- Async publish API ergonomics on the client side. Conformance is asserted at the protocol layer (request/reply with explicit headers), not via any specific client library.
- Performance and throughput characteristics. The harness validates correctness only.

## Implementation notes for the harness

- **Determinism over speed** — tests should wait for advisories with a generous bound (e.g. 15s for `AB-801` / 12s sleep + 3s slack) rather than racing on tight timeouts.
- **Cleanup** — every test deletes its stream(s) on completion. The harness must isolate state across tests so a crashed test cannot leak a `<uuid>` into a later one.
- **Reporting** — per-test result is `pass`, `fail`, `skip` (e.g. server below required version), or `inconclusive` (e.g. AB-412 / AB-703 / AB-901 where multiple acceptable behaviors are allowed — record the observed branch).
- **Server version gating** — tests AB-202, AB-501, AB-502, AB-503, AB-702, AB-703 require specific minimum server versions; the harness must skip with a clear reason on older builds.
- **Replicated tests** — AB-901 / AB-902 require a cluster. Skip with reason on a single-server target.
- **Resource-intensive tests** — AB-804 (per-server limit) should be opt-in via a flag, since it briefly holds 1000 batches open.