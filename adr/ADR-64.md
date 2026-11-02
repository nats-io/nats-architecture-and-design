# JetStream Max Ack Pending Per Subject

| Metadata | Value                           |
|----------|---------------------------------|
| Date     | 2026-10-23                      |
| Author   | @jnmoyne, @MauriceVanVeen       |
| Status   | Proposed                        |
| Tags     | jetstream, server, client, 2.16 |

| Revision | Date       | Author                    | Info           | Server Version | API Level |
|----------|------------|---------------------------|----------------|----------------|-----------|
| 1        | 2026-10-23 | @jnmoyne, @MauriceVanVeen | Initial design | 2.16.0         | 6         |

## Context and Problem Statement

Users want to process related messages strictly in order, while processing unrelated
messages in parallel.

When several clients share one consumer messages are handed out on demand. Given a deposit
on `operations.X.deposit` followed by a withdrawal on `operations.X.withdrawal`, the
deposit may go to `client1` and the withdrawal to `client2`. Both were delivered in order,
but `client2` may finish first. Setting `max_ack_pending` to `1` fixes this but serializes
the whole consumer, even though messages for customers `X` and `Y` are unrelated.

This ADR bounds the number of unacknowledged messages per *key*, where the key is the
subject or selected wildcards of it.

## Design

### Configuration

```go
type SubjectAckPendingLimit struct {
    FilterSubject string `json:"filter_subject,omitempty"` // Empty means ">".
    Wildcards     []int  `json:"wildcards,omitempty"`      // 1-based indexes of the * tokens forming the key. Empty means the whole subject.
    MaxAckPending int    `json:"max_ack_pending"`          // Limit per key, >= 1.
}

// ConsumerConfig
MaxAckPendingPerSubject []SubjectAckPendingLimit `json:"max_ack_pending_per_subject,omitempty"`
```

```json
{
  "ack_policy": "explicit",
  "max_ack_pending": 1000,
  "max_ack_pending_per_subject": [
    { "filter_subject": "operations.*.>", "wildcards": [1], "max_ack_pending": 1 }
  ]
}
```

### Validation

Validation happens at create and update time, in normal and pedantic mode alike. Any
violation is an error.

When `max_ack_pending_per_subject` is set, the consumer must have:

* `ack_policy` set to `explicit`. `none` has no pending set to count, and `all` would
  acknowledge deferred messages that were never delivered.
* `max_ack_pending` set to `> 0`, since it bounds the deferred set (see Delivery).

Each entry must have:

* `filter_subject` either empty, meaning `>`, or a valid subject filter. Like the
  consumer's own filter subjects, it is not required to be a subset of the consumer's
  filter subjects or the stream's subjects. An entry that matches no message has no
  effect.
* `wildcards` that are unique and between `1` and the number of `*` tokens in
  `filter_subject`. As in ADR-30, only `*` tokens are indexed. A `>` is never indexed. To
  key on a token a `>` would cover, write that token as `*`.
* `max_ack_pending` set to `>= 1`. It may exceed the consumer level `max_ack_pending`,
  which still applies across all keys, but then never binds.

Across entries:

* Entries must not overlap: two filters that can match the same subject are an error, so a
  subject matches at most one entry.
* As a consequence, an entry with an empty `filter_subject` (or `>`) must be the only
  entry. It has no `*` tokens, so its `wildcards` must be empty and its key is always the
  whole subject.

Examples, with each entry written as (`filter_subject`, `wildcards`, `max_ack_pending`):

| Entries                                           | Result                                            |
|---------------------------------------------------|---------------------------------------------------|
| `("", [], 1)`                                     | valid, limit per subject                          |
| `("", [], 1)`, `("orders.*", [], 1)`              | error, an empty filter overlaps every other entry |
| `("", [1], 1)`                                    | error, `>` has no `*` tokens                      |
| `("orders.*.>", [], 1)`, `("orders.eu.>", [], 2)` | error, `orders.eu.>` overlaps `orders.*.>`        |
| `("orders.*.>", [2], 1)`                          | error, `orders.*.>` has one `*` token             |
| `("orders.*", [], 0)`                             | error, limit must be `>= 1`                       |
| `("orders.*", [1], 1)`, `("billing.*", [1], 5)`   | valid                                             |

### Error Codes

The server returns the following `err_code` values for rejection paths defined in this
ADR:

| Rejection path                                                            | `err_code` | Description                                                         |
|---------------------------------------------------------------------------|-----------:|---------------------------------------------------------------------|
| `max_ack_pending_per_subject` set with `ack_policy` other than `explicit` |    `10229` | `consumer max ack pending per subject requires explicit ack policy` |
| Any other violation of the rules in Validation                            |    `10230` | `consumer max ack pending per subject is invalid: {err}`            |

The ack policy gets its own code, like `10082` (`consumer requires ack policy for max ack
pending`), since it is a conflict with another consumer field rather than with the
entries. All other violations share one code, like the subject transform errors (`10145`,
`10155`), and `{err}` names the violating rule, for example
`filter subject "orders.*.>" has 1 wildcard, index 2 is out of range`.

### Keys

A message is in scope of an entry when its subject matches `filter_subject`. Its key is
`filter_subject` with each `*` selected by `wildcards` replaced by the subject's token at
that position, or the subject itself if `wildcards` is empty. Unselected `*` and `>`
tokens are kept, so a key is a subject filter that matches every subject sharing that
key. Messages matching no entry are only subject to the consumer level `max_ack_pending`.

Because entries don't overlap, keys of different entries never collide. With entries
`orders.*` and `billing.*`, both keyed on their `*`, the subjects `orders.X` and
`billing.X` have the distinct keys `orders.X` and `billing.X`.

A message counts towards the key its subject matches. With `filter_subject` set to
`orders.*.*.>`, `wildcards` set to `[2]` and `max_ack_pending` set to `1`, the subject
`orders.eu.X.created` has the key `orders.*.X.>`. While it is pending, a later
`orders.us.X.updated` has the same key and is deferred, whereas `orders.eu.Y.created` has
the key `orders.*.Y.>` and is delivered.

Examples with `max_ack_pending` set to `1`, from basic to advanced:

| Ordered per                                | `filter_subject` | `wildcards` | Example subject          | Key                    |
|--------------------------------------------|------------------|-------------|--------------------------|------------------------|
| subject                                    | empty            | `[]`        | `operations.X.deposit`   | `operations.X.deposit` |
| customer                                   | `operations.*.>` | `[1]`       | `operations.X.deposit`   | `operations.X.>`       |
| operation kind, regardless of customer     | `operations.*.*` | `[2]`       | `operations.X.deposit`   | `operations.*.deposit` |
| customer, regardless of region             | `orders.*.*.>`   | `[2]`       | `orders.eu.X.created`    | `orders.*.X.>`         |
| customer and region                        | `orders.*.*.>`   | `[1, 2]`    | `orders.eu.X.created`    | `orders.eu.X.>`        |
| top-level KV key, sub-keys grouped with it | `$KV.bucket.*.>` | `[1]`       | `$KV.bucket.user.1.name` | `$KV.bucket.user.>`    |

An entry with empty `wildcards` orders per subject within its filter, for example
`$KV.bucket.>` orders per KV key.

### Delivery

* Within one key, messages are delivered in stream sequence order.
* When the next message belongs to a key at its limit, the consumer *defers* it and
  continues with later messages.
* A deferred message is delivered, ahead of anything newer for its key, once its key has
  room.
* A key regains a slot when a message leaves the pending set: ack, term or `max_deliver`
  exhausted. NAK, in-progress and ack wait expiry keep the message pending.
* The deferred set is capped at the consumer level `max_ack_pending`, counted separately
  from pending. When both are full, the consumer stalls as it does at `max_ack_pending`
  today. Every message the consumer scans ends up pending or deferred, so the cap bounds
  both the stored state and the scan work.
* Deferred messages are still counted in `num_pending`.

With a `max_ack_pending` limit of `1`, processing follows stream order per key. With a
limit above `1`, the limit bounds parallelism per key but not processing order.

### Interaction with priority policies

The limits select which *messages* are eligible for delivery. The priority policies
of [ADR-42](ADR-42.md) select which *pull request* is served. The two compose without
extra configuration. Priority policies only exist on pull consumers, so this section does
not apply to push consumers.

* `pinned_client`: the limits still apply to the pinned client. A single client fetching
  batches and processing them concurrently can reorder messages within a key just as two
  clients would. On a pin switch the server waits for in-flight messages. Deferred
  messages are not in flight and are delivered to the new pinned client in sequence order.
* `overflow`: deferred messages were not delivered yet and are not counted towards
  `min_pending`, because overflow clients cannot be served a message whose key is blocked.
  While ADR-42 compares `min_pending` against `num_pending`, with this feature it is
  compared against `num_pending - num_deferred`. `min_ack_pending` is unaffected.
* `prioritized`: no specific interaction.

### State and replication

* Deferred sequences are consumer state, persisted and replicated alongside the pending
  set.
* The ack floor stream sequence is `min(pending ∪ deferred) - 1`, so it never passes a
  message that wasn't delivered yet. A deferred message is assigned its consumer sequence
  when delivered, so the consumer sequence order no longer matches the stream sequence
  order. The ack floor consumer sequence is unchanged: deferred messages have no consumer
  sequence, so it is derived from delivered messages only.
* A message that is deferred still needs an ack, so Interest and WorkQueue streams retain
  it.
* A deferred message that is removed from the stream (limits, per-message TTL, purge,
  delete) is removed from the deferred set.
* Per-key counters are not replicated. A new leader rebuilds them from the subjects of
  pending and deferred messages.
* Stream backups ([ADR-63](ADR-63.md)) don't include the deferred set. When `num_deferred`
  is above `0`, the backup lowers the consumer's delivered stream sequence to its ack
  floor, which is below every deferred message. Deferred messages are therefore scanned
  again after a restore and the deferred set is regenerated.

### Updates

Entries can be added, removed or changed, and changes take effect immediately. After
lowering a limit, messages already pending stay pending, and no new messages are delivered
for a key until its pending count is below the lowered limit. Raising a limit makes
deferred messages eligible for keys that now have room. Changing a filter or wildcards
re-keys pending and deferred messages. Removing all entries makes all deferred messages
eligible, in sequence order.

### Consumer reset

A consumer reset ([ADR-60](ADR-60.md)) clears the deferred set and the per-key counters,
alongside the pending and redelivered messages. Delivery then starts from the new ack
floor, so previously deferred messages above it are scanned again and deferred anew if
their key is at its limit.

### Consumer Info

Consumer info includes `num_deferred`, the size of the deferred set:

```go
// ConsumerInfo
NumDeferred int `json:"num_deferred,omitempty"`
```

### Advisories

An advisory is published when the deferred set reaches its cap and the consumer stalls. It
is not published again until the deferred set has dropped below half of its cap, so a
consumer hovering at the cap doesn't flood advisories.

* `stream_seq` is the stream sequence of the message that could not be deferred because
  the deferred set is at its cap.
* `limit` is the effective cap that was reached, the consumer level `max_ack_pending`.

```go
const JSAdvisoryConsumerDeferredLimitReachedPre = "$JS.EVENT.ADVISORY.CONSUMER.DEFERRED_LIMIT_REACHED"
const JSConsumerDeferredLimitReachedAdvisoryType = "io.nats.jetstream.advisory.v1.consumer_deferred_limit_reached"

// JSConsumerDeferredLimitReachedAdvisory indicates that a consumer's deferred set is full and delivery stalled.
type JSConsumerDeferredLimitReachedAdvisory struct {
    TypedEvent
    Account   string `json:"account,omitempty"`
    Stream    string `json:"stream"`
    Consumer  string `json:"consumer"`
    Domain    string `json:"domain,omitempty"`
    StreamSeq uint64 `json:"stream_seq"`
    Limit     int    `json:"limit"`
}
```

Published on `$JS.EVENT.ADVISORY.CONSUMER.DEFERRED_LIMIT_REACHED.<STREAM>.<CONSUMER>`:

```json
{
  "type": "io.nats.jetstream.advisory.v1.consumer_deferred_limit_reached",
  "id": "zO3ZqvLaQCh7S9AaOVu4dC",
  "timestamp": "2026-10-22T09:15:42.181622Z",
  "account": "ACME",
  "stream": "OPERATIONS",
  "consumer": "processor",
  "stream_seq": 1024,
  "limit": 1000
}
```

## Decision

* Deferred messages are tracked as an explicit set of stream sequences, capped at
  `max_ack_pending`. The stream is scanned only once, and a deferred message is looked up
  by its sequence when its key has room or after a leader change. Tracking per key where
  to resume instead would require a scan of the stream for every key that has room, which
  is significantly more expensive.
* No separate `max_deferred` field. A deferred entry costs the same as a pending one, so
  the existing `max_ack_pending` sizing and its limits apply. The server
  (`jetstream.limits.max_ack_pending`), account (`max_ack_pending`, including JWT limits)
  and stream (`consumer_limits.max_ack_pending`) limits therefore bound the deferred set
  too, with no new limits. If added later, `max_deferred` defaults to `max_ack_pending` and
  is validated against those same limits.
* Not a priority policy. Priority policies are mutually exclusive, select pull requests
  and only exist on pull consumers. This feature limits pending messages per key, works with
  every priority policy and works for both push and pull consumers.

## Consequences

* A consumer using this feature requires API level `6` ([ADR-44](ADR-44.md)). Servers that
  don't support it load the consumer in Offline mode, since they would otherwise treat
  deferred messages as acknowledged.
* A stuck key that keeps receiving messages fills the deferred set until it reaches its
  cap, after which the whole consumer stalls.
* A message beyond the deferred cap waits until earlier keys drain, even if its own key
  has room.
* Clients only need to expose the new configuration. Pull requests are unchanged.
