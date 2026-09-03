# ADR-42 Conformance Tests — Pull Consumer Priority Groups

This document describes the conformance tests that validate a server implementation of the **Pull Consumer Priority Groups** feature defined in [ADR-42](../adr/ADR-42.md).

A conformance harness implementing these tests should be able to run them against any NATS server build claiming support for Priority Groups (introduced at server 2.11, with `pinned_client` 423 status refinements at 2.12).

## How to read this document

Each test has the following shape:

- **ID** — stable identifier, used by the harness for reporting (`PG-NNN`).
- **Title** — one-line summary.
- **References** — the section of ADR-42 the test derives from.
- **Preconditions** — required server features, stream/consumer configuration, and any prior state.
- **Steps** — the actions the harness takes, expressed at the protocol level (request payloads, subjects, headers).
- **Expected** — the observable behavior the harness asserts on.

A test passes only if every assertion in **Expected** holds. Where a test depends on another test's setup, that is called out in **Preconditions**.

## Common harness primitives

The harness needs the following building blocks. Implementations should provide them once and reuse them across tests.

- `new_stream(cfg)` — create a stream with the provided `StreamConfig`. Default config: `Subjects: ["TEST.>"]`, `Storage: file`, `Replicas: 1`, unless the test overrides.
- `delete_stream(name)` — clean up.
- `new_consumer(stream, cfg)` — create a consumer using `$JS.API.CONSUMER.CREATE.<stream>.<consumer>`. Returns the assigned consumer name.
- `update_consumer(stream, cfg)` — invoke `$JS.API.CONSUMER.CREATE.<stream>.<consumer>` (server treats matching name as update); the harness records whether the server accepts or rejects.
- `delete_consumer(stream, consumer)` — clean up.
- `consumer_info(stream, consumer)` — returns the consumer's reported configuration (including `priority_groups`, `priority_policy`, `priority_timeout`) and dynamic state (including `priority_groups[].name`, `pinned_id`, `pinned_ts`).
- `publish(subject, payload, headers={})` — publish a regular JetStream message and capture the assigned sequence.
- `pull(stream, consumer, payload, inbox, timeout)` — sends a pull request to `$JS.API.CONSUMER.MSG.NEXT.<stream>.<consumer>` with the JSON payload; subscribes on `inbox`; returns every reply received within `timeout` (messages and any 4xx status replies).
- `unpin(stream, consumer, group)` — publishes JSON `{"group": "<group>"}` to `$JS.API.CONSUMER.UNPIN.<STREAM>.<CONSUMER>`; returns the API reply.
- `read_status(msg)` — extracts the NATS status code (e.g. `423`) and description from a status reply, or `nil` if the reply is `NATS/1.0` (success, message body present).
- `read_pin_id(msg)` — returns the `Nats-Pin-Id` header on a delivered message, or `""` if absent.
- `subscribe(subject)` — subscribes to an arbitrary subject (used for capturing advisories).
- `expect_no_message(inbox, timeout)` — drains the inbox; fails the test if any non-heartbeat message arrives within `timeout`.

The harness must distinguish three reply categories on a pull-request inbox:

- **Message delivery** — a JetStream message with subject + headers + payload; the consumer is delivering data.
- **Status reply** — `NATS/1.0 <code> <description>` with no payload (e.g. `408 Request Timeout`, `423 Nats-Pin-Id mismatch`, `409` for various pull errors).
- **Heartbeat** — `NATS/1.0 100 Idle Heartbeat`, ignored by these tests unless explicitly checked.

## Wire-level reference

### Pull-request payload (additional fields introduced by ADR-42)

```text
Group         string `json:"group,omitempty"`
MinPending    int    `json:"min_pending,omitempty"`     // overflow only
MinAckPending int    `json:"min_ack_pending,omitempty"` // overflow only
Failover      int    `json:"failover,omitempty"`        // overflow only (seconds)
Id            string `json:"id,omitempty"`              // pinned_client only
Priority      int    `json:"priority,omitempty"`        // prioritized only (0..9)
```

### Consumer-config fields introduced by ADR-42

```text
PriorityGroups  []string      `json:"priority_groups,omitempty"`
PriorityPolicy  string        `json:"priority_policy,omitempty"` // "overflow" | "pinned_client" | "prioritized"
PriorityTimeout time.Duration `json:"priority_timeout,omitempty"` // pinned_client only, nanoseconds
```

### Consumer-state field introduced by ADR-42

```text
PriorityGroups []PriorityGroupState `json:"priority_groups,omitempty"`

type PriorityGroupState struct {
    Group          string    `json:"group"`
    PinnedClientID string    `json:"pinned_client_id,omitempty"`
    PinnedTS       time.Time `json:"pinned_ts,omitempty"`
}
```

### Headers and status codes added by ADR-42

| Code / Header                | Meaning                                                                     |
|------------------------------|-----------------------------------------------------------------------------|
| `Nats-Pin-Id: <id>` (header) | Set on every message delivered to the pinned client                         |
| `423 Nats-Wrong-Pin-Id`      | A waiting pull's stored `id` no longer matches the pin (e.g. after a switch) |
| `423 Nats-Pin-Id mismatch`   | An inbound pull supplied an `id` that does not match the current pin        |

The exact textual variants of the 423 description (`Nats-Wrong-Pin-Id` vs `Nats-Pin-Id mismatch`) come from ADR-42 revision 5; the harness asserts only that **status is `423`** and records the description.

### API subjects added by ADR-42

```text
$JS.API.CONSUMER.UNPIN.<STREAM>.<CONSUMER>   # payload: {"group":"<name>"}
```

### Advisories added by ADR-42

```text
$JS.EVENT.ADVISORY.CONSUMER.PINNED.<STREAM>.<CONSUMER>     # type: io.nats.jetstream.advisory.v1.consumer_group_pinned
$JS.EVENT.ADVISORY.CONSUMER.UNPINNED.<STREAM>.<CONSUMER>   # type: io.nats.jetstream.advisory.v1.consumer_group_unpinned
```

---

## PG-100 — Configuration

### PG-101 — `priority_policy: overflow` is accepted with a single group

- **References** — General Overview; `overflow` policy.
- **Preconditions** — Stream with `Subjects: ["TEST.>"]`.
- **Steps**
  1. Create a pull consumer with `PriorityGroups: ["jobs"]`, `PriorityPolicy: "overflow"`, `AckPolicy: "explicit"`.
  2. Read back the consumer configuration.
- **Expected**
  - Consumer creation succeeds.
  - `consumer_info` reports `priority_groups: ["jobs"]` and `priority_policy: "overflow"`.

### PG-102 — `priority_policy: pinned_client` is accepted with a single group and timeout

- **References** — `pinned_client` policy.
- **Preconditions** — Stream as PG-101.
- **Steps**
  1. Create a pull consumer with `PriorityGroups: ["jobs"]`, `PriorityPolicy: "pinned_client"`, `PriorityTimeout: 2*time.Minute`, `AckPolicy: "explicit"`.
  2. Read back the consumer configuration.
- **Expected**
  - Consumer creation succeeds.
  - `consumer_info` reports `priority_groups: ["jobs"]`, `priority_policy: "pinned_client"`, `priority_timeout` equal to 120000000000 nanoseconds.

### PG-103 — `priority_policy: prioritized` is accepted with a single group

- **References** — `prioritized` policy.
- **Preconditions** — Stream as PG-101.
- **Steps**
  1. Create a pull consumer with `PriorityGroups: ["jobs"]`, `PriorityPolicy: "prioritized"`.
  2. Read back the consumer configuration.
- **Expected**
  - Consumer creation succeeds; `priority_policy` is `"prioritized"`.

### PG-104 — Unknown `priority_policy` is rejected

- **References** — General Overview ("The presence of the `PriorityPolicy` set to a known policy activates the set of features").
- **Preconditions** — Stream as PG-101.
- **Steps**
  1. Attempt to create a pull consumer with `PriorityGroups: ["jobs"]`, `PriorityPolicy: "bogus"`.
- **Expected**
  - Consumer creation fails with an error from the server.

### PG-105 — `priority_groups` set without `priority_policy` is rejected

- **References** — General Overview ("The presence of the `PriorityPolicy` set to a known policy activates the set of features").
- **Preconditions** — Stream as PG-101.
- **Steps**
  1. Attempt to create a pull consumer with `PriorityGroups: ["jobs"]` and no `PriorityPolicy`.
- **Expected**
  - Consumer creation fails. (Per ADR: groups are only meaningful when a policy is set.)

### PG-106 — `priority_policy` set without `priority_groups` is rejected

- **References** — General Overview ("`PriorityGroups` require at least one entry").
- **Preconditions** — Stream as PG-101.
- **Steps**
  1. Attempt to create a pull consumer with `PriorityPolicy: "overflow"`, `PriorityGroups: []`, `AckPolicy: "explicit"`.
- **Expected**
  - Consumer creation fails.

### PG-107 — Multiple groups in `priority_groups` is rejected (initial-impl limit)

- **References** — General Overview ("In the initial implementation we should limit `PriorityGroups` to one per consumer only and error should one be made with multiple groups").
- **Preconditions** — Stream as PG-101.
- **Steps**
  1. Attempt to create a pull consumer with `PriorityGroups: ["a", "b"]`, `PriorityPolicy: "overflow"`, `AckPolicy: "explicit"`.
- **Expected**
  - Consumer creation fails.

### PG-108 — Group name length and charset validation

- **References** — General Overview ("Valid `PriorityGroups` values must match `limited-term` … and may not exceed 16 characters").
- **Preconditions** — Stream as PG-101.
- **Steps** — for each of the following group names, attempt to create a consumer with `PriorityPolicy: "overflow"`, `AckPolicy: "explicit"`:
  1. `"jobs"` — 4 chars, valid charset → expected to succeed.
  2. `"abcdefghij012345"` — 16 chars exactly → expected to succeed.
  3. `"abcdefghij0123456"` — 17 chars → expected to fail.
  4. `"bad name"` — contains space → expected to fail.
  5. `"bad.name"` — contains `.` (not in limited-term) → expected to fail.
  6. `""` — empty → expected to fail.
- **Expected**
  - Cases 1–2 succeed; cases 3–6 fail with a server error.

### PG-109 — Push consumer with `priority_policy` is rejected

- **References** — General Overview ("This is only supported on Pull Consumers, configuring this on a Push consumer must raise an error").
- **Preconditions** — Stream as PG-101.
- **Steps**
  1. Attempt to create a *push* consumer (set `DeliverSubject`) with `PriorityGroups: ["jobs"]`, `PriorityPolicy: "overflow"`, `AckPolicy: "explicit"`.
- **Expected**
  - Consumer creation fails.

### PG-110 — `overflow` requires `AckPolicy: explicit`

- **References** — `overflow` policy ("AckPolicy has to be `explicit`. … in Pedantic mode").
- **Preconditions** — Stream as PG-101.
- **Steps**
  1. Attempt to create a pull consumer with `PriorityPolicy: "overflow"`, `PriorityGroups: ["jobs"]`, `AckPolicy: "none"`.
  2. Repeat with `AckPolicy: "all"`.
  3. Repeat with the same parameters but pass `pedantic: true` in the `$JS.API.CONSUMER.CREATE` request payload.
- **Expected**
  - Steps 1 and 2: server either rejects outright OR forces the policy to `explicit` (the harness records the branch — both are acceptable per ADR).
  - Step 3 (pedantic): server rejects with an error.

### PG-111 — `pinned_client` requires `AckPolicy: explicit`

- **References** — `pinned_client` policy ("AckPolicy has to be `explicit`").
- **Preconditions** — Stream as PG-101.
- **Steps**
  1. Attempt to create a pull consumer with `PriorityPolicy: "pinned_client"`, `PriorityGroups: ["jobs"]`, `AckPolicy: "none"`, `PriorityTimeout: 30s`.
  2. Repeat with `pedantic: true`.
- **Expected**
  - Step 1: server rejects, OR forces `AckPolicy: explicit` (harness records the branch).
  - Step 2 (pedantic): server rejects.

### PG-112 — Cannot add priority groups via update

- **References** — General Overview NOTE ("we cannot support updating a consumer from one with groups to one without and vice versa").
- **Preconditions** — Pull consumer created with no `PriorityGroups` / no `PriorityPolicy`.
- **Steps**
  1. Update the consumer to add `PriorityGroups: ["jobs"]`, `PriorityPolicy: "overflow"`, `AckPolicy: "explicit"`.
- **Expected**
  - Update fails with a server error.

### PG-113 — Cannot remove priority groups via update

- **References** — General Overview NOTE.
- **Preconditions** — Pull consumer created with `PriorityGroups: ["jobs"]`, `PriorityPolicy: "overflow"`, `AckPolicy: "explicit"`.
- **Steps**
  1. Update the consumer to clear `PriorityGroups` and `PriorityPolicy`.
- **Expected**
  - Update fails with a server error.

### PG-114 — Cannot switch between policies via update

- **References** — General Overview NOTE ("We also cannot switch between different policies").
- **Preconditions** — Pull consumer created with `PriorityPolicy: "overflow"`, `PriorityGroups: ["jobs"]`, `AckPolicy: "explicit"`.
- **Steps**
  1. Update the consumer to `PriorityPolicy: "pinned_client"` (keeping the same group).
- **Expected**
  - Update fails.

### PG-115 — `priority_timeout` is updatable on `pinned_client`

- **References** — `pinned_client` policy ("Today only the `PriorityTimeout` setting supports being updated").
- **Preconditions** — Pull consumer created with `PriorityPolicy: "pinned_client"`, `PriorityGroups: ["jobs"]`, `PriorityTimeout: 1m`, `AckPolicy: "explicit"`.
- **Steps**
  1. Update the consumer with `PriorityTimeout: 5m` (all other fields unchanged).
  2. Read back the configuration.
- **Expected**
  - Update succeeds.
  - `priority_timeout` reports `5*time.Minute` in nanoseconds.

---

## PG-200 — `overflow` policy

### PG-201 — Pull without `group` on a priority consumer is rejected

- **References** — `overflow` policy ("pulls not part of a valid group will result in an error").
- **Preconditions** — Pull consumer with `PriorityPolicy: "overflow"`, `PriorityGroups: ["jobs"]`, `AckPolicy: "explicit"`. Stream contains 1 message.
- **Steps**
  1. Send a pull request with payload `{"batch": 1, "expires": 1000000000}` (no `group` field) and a 2 s read window.
- **Expected**
  - The pull receives a status reply (4xx) indicating an invalid pull, OR an error advisory. No JetStream message is delivered. The harness records the exact status code.

### PG-202 — Pull with unknown `group` is rejected

- **References** — `overflow` policy ("pulls not part of a valid group will result in an error").
- **Preconditions** — As PG-201.
- **Steps**
  1. Send a pull with `{"batch": 1, "group": "ghost", "expires": 1000000000}`.
- **Expected**
  - Status reply (4xx). No message delivered.

### PG-203 — Pull idle when neither `min_pending` nor `min_ack_pending` is met

- **References** — `overflow` policy ("only deliver messages when `num_pending` for the consumer is >= 1000").
- **Preconditions** — Pull consumer with `PriorityPolicy: "overflow"`, `PriorityGroups: ["jobs"]`, `AckPolicy: "explicit"`. Stream contains 5 messages, none acked, none in flight.
- **Steps**
  1. Send a pull `{"batch": 1, "group": "jobs", "min_pending": 1000, "expires": 1500000000}`.
  2. Drain the inbox for 2 s.
- **Expected**
  - No message is delivered. The pull either expires silently after 1.5 s OR returns a `408 Request Timeout` per the standard pull-expires behaviour. No data message arrives.

### PG-204 — Pull served when `min_pending` is met

- **References** — `overflow` policy.
- **Preconditions** — Pull consumer as PG-203. Stream contains 1500 messages.
- **Steps**
  1. Send `{"batch": 5, "group": "jobs", "min_pending": 1000, "expires": 2000000000}`.
- **Expected**
  - 5 messages delivered.

### PG-205 — `min_pending` and `min_ack_pending` combine via boolean OR

- **References** — `overflow` policy ("If `min_pending` and `min_ack_pending` are both given either being satisfied will result in delivery").
- **Preconditions** — Pull consumer as PG-203. Stream contains 100 messages. Pre-fetch 50 messages with a different pull and DO NOT ack them, so that `num_ack_pending = 50` and `num_pending = 50`.
- **Steps**
  1. From a fresh pull, send `{"batch": 1, "group": "jobs", "min_pending": 1000, "min_ack_pending": 10, "expires": 2000000000}`.
- **Expected**
  - 1 message is delivered (the `min_ack_pending` clause is satisfied even though `min_pending` is not).

### PG-206 — `failover` value below 5 is rejected

- **References** — `overflow` policy ("The minimum value for `failover` is `5` … any out of bounds … value will result in a pull error").
- **Preconditions** — Pull consumer as PG-203. Stream contains 1 message.
- **Steps**
  1. Send `{"batch": 1, "group": "jobs", "failover": 4}`.
- **Expected**
  - Status reply (4xx); no data delivered.

### PG-207 — `failover` value above 3600 is rejected

- **References** — `overflow` policy ("maximum is `3600`").
- **Preconditions** — As PG-206.
- **Steps**
  1. Send `{"batch": 1, "group": "jobs", "failover": 3601}`.
- **Expected**
  - Status reply (4xx); no data delivered.

### PG-208 — `failover` accepts boundary values 5 and 3600

- **References** — `overflow` policy.
- **Preconditions** — As PG-206.
- **Steps**
  1. Send `{"batch": 1, "group": "jobs", "failover": 5, "expires": 1000000000}`.
  2. Send `{"batch": 1, "group": "jobs", "failover": 3600, "expires": 1000000000}`.
- **Expected**
  - Both pulls are accepted (no error status reply); each receives the message.

### PG-209 — Failover takes over when no non-failover pull is present

- **References** — `overflow` policy ("should there be no pull requests at all for 5 seconds this pull request will be serviced, overriding other limits").
- **Preconditions** — Pull consumer as PG-203. Stream contains 1 message; no other pulls active.
- **Steps**
  1. Send a pull `{"batch": 1, "group": "jobs", "failover": 5, "expires": 10000000000}`.
  2. Wait at least 6 s.
- **Expected**
  - The message is delivered after roughly 5 s of idle time (no near-by pulls). The harness asserts the message did NOT arrive within the first 4 s.

### PG-210 — Nearer pulls suppress further failover

- **References** — `overflow` policy ("Pulls from any nearer client will reset the timers").
- **Preconditions** — Pull consumer as PG-203. Stream contains messages but `min_pending` is set such that no pulls qualify.
- **Steps**
  1. Open pull A: `{"batch": 1, "group": "jobs", "min_pending": 999999, "expires": 30000000000}` (high-priority, will not be served because criterion not met).
  2. Open pull B: `{"batch": 1, "group": "jobs", "failover": 5, "expires": 30000000000}`.
  3. Every 2 s, send a fresh pull A (resetting its activity).
- **Expected**
  - Pull B does NOT receive the message even after 10 s of total elapsed time, because pull A keeps the failover timer reset.
  - This is **inconclusive** if the server has no pull-A presence detection; the harness records the observed delivery time.

---

## PG-300 — `pinned_client` policy

### PG-301 — First pull becomes the pinned client

- **References** — `pinned_client` policy ("After selecting a new pinned client, the first message that will be delivered … will include a Nats-Pin-Id: xyz header").
- **Preconditions** — Pull consumer with `PriorityPolicy: "pinned_client"`, `PriorityGroups: ["jobs"]`, `PriorityTimeout: 30s`, `AckPolicy: "explicit"`. Stream contains 1 message.
- **Steps**
  1. Send a pull `{"batch": 1, "group": "jobs", "expires": 2000000000}`.
- **Expected**
  - The message is delivered.
  - The delivered message carries a `Nats-Pin-Id: <id>` header with a non-empty `<id>` value.
  - `consumer_info().priority_groups[0]` reports `name: "jobs"`, `pinned_id: "<id>"`, and `pinned_ts` set.

### PG-302 — Pulls without an `id` are kept as standby while another client is pinned

- **References** — `pinned_client` policy, revision 8 ("A pull request that omits the `id` field while another client is pinned is **not** rejected. The server keeps it on the waiting queue as a standby candidate").
- **Preconditions** — As PG-301; one client is pinned with `id = X`. Stream contains a second message.
- **Steps**
  1. From a *different* client (fresh inbox), send a pull `{"batch": 1, "group": "jobs", "expires": 1500000000}` with **no `id` field**.
  2. Drain the inbox until the pull's `expires` window elapses.
- **Expected**
  - No JetStream message is delivered (the pinned client holds the pin).
  - No `423` status reply is received — a no-`id` pull is a standby candidate, not a wrong-id pull.
  - The pull terminates with either a `408 Request Timeout` after `expires`, or no reply at all (server-implementation choice). The harness records which form occurred.

### PG-303 — Pull with the wrong `id` is rejected with 423

- **References** — `pinned_client` policy; revision 5 (`423 Nats-Wrong-Pin-Id`).
- **Preconditions** — As PG-302.
- **Steps**
  1. From a different client, send `{"batch": 1, "group": "jobs", "id": "definitely-not-the-pin", "expires": 1500000000}`.
- **Expected**
  - Status reply `423`. The harness records the description (expected `Nats-Wrong-Pin-Id`).

### PG-304 — Pinned client continues to receive messages with the same `Nats-Pin-Id`

- **References** — `pinned_client` policy ("the first message that will be delivered to this client, and all future ones, will include a Nats-Pin-Id: xyz header").
- **Preconditions** — As PG-301; pinned client holds `id = X`. Publish 4 more messages.
- **Steps**
  1. Pinned client sends `{"batch": 4, "group": "jobs", "id": "<X>", "expires": 2000000000}`.
- **Expected**
  - 4 messages are delivered.
  - Every delivered message carries `Nats-Pin-Id: <X>`.

### PG-305 — Pinned client times out and the pin switches

- **References** — `pinned_client` policy ("If no pulls from the pinned client are received within `PriorityTimeout` the server will switch again").
- **Preconditions** — Pull consumer with `PriorityPolicy: "pinned_client"`, `PriorityGroups: ["jobs"]`, `PriorityTimeout: 5s`, `AckPolicy: "explicit"`. Stream contains 2 messages. Establish pin with client A (`id = X`); receive 1 message.
- **Steps**
  1. Open a *standby* pull from client B with `{"batch": 1, "group": "jobs", "expires": 30000000000}` (no `id`).
  2. Wait 8 s without any further pulls from A.
  3. (Server should fire `PriorityTimeout` at 5 s, unpin A, then deliver to B.)
- **Expected**
  - Client B receives the second message with `Nats-Pin-Id: <Y>` where `<Y> != <X>`.
  - `consumer_info().priority_groups[0].pinned_id == <Y>`.
  - A subsequent pull from A using its old `id = <X>` is rejected with status `423`.

### PG-306 — Pin survives across pulls within `PriorityTimeout`

- **References** — `pinned_client` policy ("The pinned timeout only resets back to `PriorityTimeout` if the pinned client starts a new pull request within the timeout").
- **Preconditions** — As PG-305 but `PriorityTimeout: 10s`. Stream produces 1 new message every 2 s.
- **Steps**
  1. Client A pulls every 2 s for 20 s using its established `id = X`.
- **Expected**
  - Every message is delivered to A with `Nats-Pin-Id: <X>` and the same `<X>` throughout.
  - `consumer_info().priority_groups[0].pinned_id` remains `<X>` for the full window.

### PG-307 — `no_wait` pull on `pinned_client` consumer is delivered and pins the caller

- **References** — `pinned_client` policy. ADR-42 places no client-side or server-side restriction on `no_wait` pulls in pinning mode; `no_wait` is a wire-level flag that the server treats identically regardless of `PriorityPolicy`.
- **Preconditions** — Pull consumer with `PriorityPolicy: "pinned_client"`, `PriorityGroups: ["jobs"]`, `PriorityTimeout: 30s`, `AckPolicy: "explicit"`. Stream contains 1 message.
- **Steps**
  1. Send a single pull `{"batch": 1, "group": "jobs", "no_wait": true}`.
- **Expected**
  - The pull is delivered with `Nats-Pin-Id: <X>`.
  - `consumer_info().priority_groups[0].pinned_client_id` is `<X>`.

---

## PG-400 — `pinned_client` policy: UNPIN API

### PG-401 — UNPIN clears the pin and forces a switch

- **References** — `pinned_client` policy ("A new API, `$JS.API.CONSUMER.UNPIN.<STREAM>.<CONSUMER>`, can be called which will clear the ID and trigger a client switch").
- **Preconditions** — Pull consumer with `PriorityPolicy: "pinned_client"`, `PriorityGroups: ["jobs"]`, `PriorityTimeout: 60s`, `AckPolicy: "explicit"`. Pin client A (`id = X`).
- **Steps**
  1. Open a standby pull from client B (no `id`, long expiry).
  2. Publish a new message.
  3. Send a request to `$JS.API.CONSUMER.UNPIN.<STREAM>.<CONSUMER>` with payload `{"group":"jobs"}`.
  4. Drain B's inbox for 3 s.
- **Expected**
  - The UNPIN API responds with a non-error JSON reply.
  - Client B receives the message with `Nats-Pin-Id: <Y>` where `<Y> != <X>`.
  - A pull from A with `id = <X>` is now rejected with status `423`.

### PG-402 — UNPIN with unknown group returns an error

- **References** — `pinned_client` policy.
- **Preconditions** — As PG-401.
- **Steps**
  1. Send a request to `$JS.API.CONSUMER.UNPIN.<STREAM>.<CONSUMER>` with payload `{"group":"ghost"}`.
- **Expected**
  - Reply is a JSON `{"error": ...}` from the JetStream API.
  - The pin on `jobs` is unchanged.

### PG-403 — UNPIN on a non-`pinned_client` consumer returns an error

- **References** — `pinned_client` policy.
- **Preconditions** — Pull consumer with `PriorityPolicy: "overflow"`, `PriorityGroups: ["jobs"]`, `AckPolicy: "explicit"`.
- **Steps**
  1. Send `$JS.API.CONSUMER.UNPIN.<STREAM>.<CONSUMER>` with payload `{"group":"jobs"}`.
- **Expected**
  - Reply is a JSON error.

### PG-404 — UNPIN with malformed payload returns an error

- **References** — `pinned_client` policy.
- **Preconditions** — As PG-401.
- **Steps**
  1. Send `$JS.API.CONSUMER.UNPIN.<STREAM>.<CONSUMER>` with empty payload.
  2. Send the same subject with payload `{not json`.
- **Expected**
  - Both return a JSON error reply. The pin is unchanged.

---

## PG-500 — `pinned_client` policy: Advisories

### PG-501 — `consumer_group_pinned` advisory is published when a pin is established

- **References** — `pinned_client` policy → Advisories.
- **Preconditions** — Subscribe to `$JS.EVENT.ADVISORY.CONSUMER.PINNED.<STREAM>.<CONSUMER>` BEFORE creating any pull. Pull consumer with `PriorityPolicy: "pinned_client"`, `PriorityGroups: ["jobs"]`, `PriorityTimeout: 30s`, `AckPolicy: "explicit"`. Stream contains 1 message.
- **Steps**
  1. Send a pull from client A (no `id`); receive the message.
  2. Drain the advisory subscription for 2 s.
- **Expected**
  - At least one advisory message arrives whose payload is JSON with `type: "io.nats.jetstream.advisory.v1.consumer_group_pinned"`.
  - The advisory body contains `stream`, `consumer`, `group: "jobs"`, `pinned_id` matching the `Nats-Pin-Id` header on the delivered message.

### PG-502 — `consumer_group_unpinned` advisory is published on UNPIN

- **References** — `pinned_client` policy → Advisories.
- **Preconditions** — As PG-501; pin established. Subscribe to `$JS.EVENT.ADVISORY.CONSUMER.UNPINNED.<STREAM>.<CONSUMER>`.
- **Steps**
  1. Send `$JS.API.CONSUMER.UNPIN.<STREAM>.<CONSUMER>` with `{"group":"jobs"}`.
  2. Drain the advisory subscription for 2 s.
- **Expected**
  - At least one advisory arrives with `type: "io.nats.jetstream.advisory.v1.consumer_group_unpinned"`.
  - Body contains `stream`, `consumer`, `group: "jobs"`, `reason: "admin"`.

### PG-503 — `consumer_group_unpinned` advisory has reason `timeout` on idle switch

- **References** — `pinned_client` policy → Advisories ("one of \"admin\" or \"timeout\"").
- **Preconditions** — Pull consumer with `PriorityPolicy: "pinned_client"`, `PriorityGroups: ["jobs"]`, `PriorityTimeout: 5s`, `AckPolicy: "explicit"`. Subscribe to the unpinned advisory subject. Pin a client.
- **Steps**
  1. Stop sending pulls from the pinned client.
  2. Wait 8 s.
  3. Drain the advisory subscription.
- **Expected**
  - An unpinned advisory is published with `reason: "timeout"`.

---

## PG-600 — `prioritized` policy

### PG-601 — Pull without `group` on a `prioritized` consumer is rejected

- **References** — `prioritized` policy ("pulls not part of a valid group will result in an error").
- **Preconditions** — Pull consumer with `PriorityPolicy: "prioritized"`, `PriorityGroups: ["jobs"]`. Stream contains 1 message.
- **Steps**
  1. Send `{"batch": 1, "expires": 1500000000}` (no `group`).
- **Expected**
  - 4xx status reply; no message delivered.

### PG-602 — Pull with `priority` out of range is rejected

- **References** — `prioritized` policy ("invalid or out of bounds priorities will result in an error").
- **Preconditions** — As PG-601.
- **Steps**
  1. Send `{"batch": 1, "group": "jobs", "priority": -1}`.
  2. Send `{"batch": 1, "group": "jobs", "priority": 10}`.
- **Expected**
  - Both receive a 4xx status reply. No message delivery.

### PG-603 — Pull with no `priority` defaults to priority 0

- **References** — `prioritized` policy ("pulls without a priority will be priority `0`").
- **Preconditions** — As PG-601 with 1 message in stream.
- **Steps**
  1. Send `{"batch": 1, "group": "jobs", "expires": 1000000000}` (no `priority`).
- **Expected**
  - The message is delivered.

### PG-604 — Lower priority is served first when both are pending

- **References** — `prioritized` policy ("Lower priorities will always be served first").
- **Preconditions** — Pull consumer with `PriorityPolicy: "prioritized"`, `PriorityGroups: ["jobs"]`. Stream initially empty.
- **Steps**
  1. Open a long-living pull from client HIGH (`priority: 5`, batch 5, expires 30 s).
  2. Open a long-living pull from client LOW (`priority: 0`, batch 5, expires 30 s).
  3. Publish 3 messages.
  4. Wait 2 s and capture which inbox each message landed on.
- **Expected**
  - All 3 messages are delivered to LOW. HIGH receives none.

### PG-605 — Higher priorities receive messages once lower priorities have no pull

- **References** — `prioritized` policy.
- **Preconditions** — As PG-604.
- **Steps**
  1. Open a pull from HIGH (`priority: 5`, batch 5, expires 30 s).
  2. Publish 1 message; assert HIGH receives it (no LOW pull active).
  3. Open a pull from LOW (`priority: 0`, batch 5, expires 30 s).
  4. Publish 1 more message.
- **Expected**
  - In step 2 the message goes to HIGH (no competition).
  - In step 4 the message goes to LOW (lower priority preempts).

### PG-606 — Within a single priority, delivery is round-robin

- **References** — `prioritized` policy ("within each priority they could be delivered on round-robin basis").
- **Preconditions** — Pull consumer as PG-604. Stream initially empty.
- **Steps**
  1. Open pulls from clients C1 and C2, both `priority: 0`, both `batch: 10`, both expires 30 s.
  2. Publish 4 messages.
- **Expected**
  - Each of C1 and C2 receives a non-zero share of the 4 messages (round-robin / fair distribution). Strict 2-2 split is not asserted — the harness only requires that neither received all 4.

---

## PG-700 — Consumer state reporting

### PG-701 — `priority_groups` state is empty when nothing is pinned

- **References** — `pinned_client` policy ("Consumer state to include a new field `PriorityGroups` of type `[]PriorityGroupState`").
- **Preconditions** — Pull consumer with `PriorityPolicy: "pinned_client"`, `PriorityGroups: ["jobs"]`, `PriorityTimeout: 30s`, `AckPolicy: "explicit"`. No pulls yet issued.
- **Steps**
  1. Read `consumer_info`.
- **Expected**
  - `priority_groups` is present and contains exactly one entry with `name: "jobs"`.
  - For that entry, `pinned_id` is empty (or absent) and `pinned_ts` is empty (or absent).

### PG-702 — `priority_groups` state populates after pinning

- **References** — `pinned_client` policy.
- **Preconditions** — As PG-701; one client just received its first pinned message with `Nats-Pin-Id: <X>` at wall time `t0`.
- **Steps**
  1. Read `consumer_info`.
- **Expected**
  - The single `priority_groups` entry has `name: "jobs"`, `pinned_id: <X>`, and `pinned_ts` non-empty and approximately equal to `t0` (within a 30 s window).

### PG-703 — `priority_groups` state clears (or rotates) after UNPIN

- **References** — `pinned_client` policy.
- **Preconditions** — As PG-702; pin established with `id = X`.
- **Steps**
  1. Send `$JS.API.CONSUMER.UNPIN.<STREAM>.<CONSUMER>` with `{"group":"jobs"}`.
  2. Read `consumer_info` immediately afterwards.
- **Expected**
  - Either `priority_groups[0].pinned_id` is empty (no pin) OR a new id `<Y> != <X>` if a standby pull was already waiting and the server has already delivered to it. The harness records which branch occurred.

### PG-704 — `priority_groups` state is absent on non-priority consumers

- **References** — Consumer state field introduced by ADR-42.
- **Preconditions** — Pull consumer with no `PriorityPolicy`.
- **Steps**
  1. Read `consumer_info`.
- **Expected**
  - `priority_groups` field is absent OR is an empty array — the harness records which form the server uses.

---

## Out of scope

The following ADR-42 areas are intentionally **not** covered by this conformance document:

- **Geographical / topology-aware failover semantics for `overflow.failover`.** The ADR describes failover in terms of "AZ" / "region", but the server has no built-in awareness of these — the only observable is the *time* before a failover pull takes over. Conformance only asserts time-based behaviour (PG-209, PG-210), not deployment topology.
- **Specific 4xx status codes for malformed pull-request fields** (other than `423` for pin mismatch). ADR-42 describes the conditions but does not assign exact status codes for "missing group", "unknown group", "invalid priority", or "out-of-range failover". The harness records the observed code.
- **Multi-group consumers.** ADR-42 explicitly forbids them in the initial implementation; tests only assert the rejection (PG-107). Future-iteration multi-group semantics are out of scope.
- **Future iteration features** explicitly deferred by the ADR: per-group delivery stats, dynamic add/remove of groups via update, a per-pull `priority` field on `pinned_client`, message-grouping / partition routing.
- **Client-library callback semantics** ("expose call-back notifications when they become pinned"). The conformance harness asserts at the wire level only — `Nats-Pin-Id` header, `423` status, advisory messages.
- **Performance, throughput, latency, fairness guarantees beyond round-robin presence.**
- **Pedantic-mode comprehensive matrix.** PG-110 / PG-111 spot-check pedantic-mode rejection of forced `AckPolicy`; the broader pedantic-mode behaviour for every option combination is not exhaustively tested.

## Implementation notes for the harness

- **Server version gating** — Priority Groups are introduced at server 2.11. The 423 description variants `Nats-Wrong-Pin-Id` / `Nats-Pin-Id mismatch` are introduced at 2.12 (revision 5). On older builds, PG-303 should be skipped with reason; the rest of PG-300 must still pass. PG-302 (no-id standby behaviour) is independent of the 423 description and applies to all versions.
- **Inbox subscriptions** — every test opens a fresh inbox per pull. Pins are tied to client identity, which the server derives from the pull's reply subject, so the harness MUST use distinct inbox subjects to model "client A" vs "client B".
- **Time-based tests** — PG-209, PG-305, PG-503 use real-time waits. The harness must allow a generous slack (≥ 1 s above the configured timeout) to absorb scheduling jitter.
- **Cleanup** — every test deletes its consumer and stream on completion. Advisory subscriptions from PG-500 must be drained before the next test runs.
- **Reporting** — per-test result is `pass`, `fail`, `skip` (server below required version), or `inconclusive` (used for tests where the ADR allows multiple acceptable behaviours — PG-110, PG-111, PG-210, PG-703, PG-704).
- **Standby clients** — for switch tests (PG-305, PG-401), at least one standby pull must be in flight *before* the switch event so the server has a candidate to pin to. Without a standby, the server will simply have no pinned client until the next pull arrives.

## Ambiguities flagged in this document

These items in ADR-42 are unclear and the conformance suite currently records observed behaviour rather than asserting a single answer. Resolving them in a future ADR revision would let these tests become strict pass/fail:

- **Exact 4xx status code** for a pull missing `group`, having an unknown `group`, an out-of-range `priority`, or an out-of-range `failover`. Currently asserted only as "some 4xx".
- **AckPolicy enforcement strength** (PG-110, PG-111). The ADR says the server MUST error in pedantic mode but is silent on non-pedantic mode — implementations may either reject or silently coerce to `explicit`.
- **`priority_groups` consumer-info shape** when no policy is configured (PG-704) — absent vs empty array is implementation-defined.
- **Failover under a competing nearer pull that has not yet been *served*** (PG-210). The "nearer client resets the timer" wording assumes the server can detect pull presence even when the pull's criteria are not satisfied; this is reasonable but not formally specified.
- **"Wait for in-flight messages to be completed"** (Step 1 of pinned-client switch flow). The ADR does not specify a maximum time, retry behaviour, or interaction with redelivery. Not directly tested.
