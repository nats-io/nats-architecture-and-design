# Ordered Consumer

| Metadata | Value             |
|----------|-------------------|
| Date     | 2021-09-29        |
| Author   | @scottf           |
| Status   | Implemented       |
| Tags     | jetstream, client |

| Revision | Date       | Author          | Info                                                                                         |
|----------|------------|-----------------|----------------------------------------------------------------------------------------------|
| 1        | 2021-09-29 | @scottf         | Initial design                                                                               |
| 2        | 2026-11-13 | @MauriceVanVeen | Rewrite for conformance testing: add pull, specify gap detection, recovery and configuration |

## Context and Problem Statement

An application often needs to read a stream, or the part of it matching a filter, in order and without missing a
message, without acknowledging each message and without keeping durable state on the server. An ordered consumer gives
it that: the client reads through an ephemeral consumer, checks every message, and when it sees a gap or loses the
consumer, replaces it with one that starts right after the last message it accepted. The behavior is the same in push
mode, where the server sends messages to a deliver subject, and in pull mode, where the client asks for them with pull
requests.

## Design

### Configuration

The user can provide:

- `filter_subjects`. A single filter is sent as `filter_subject`. An empty entry in the list is an error, even as its
  only entry; where the client takes a single filter instead of a list, an empty one counts as not provided.
- `deliver_policy`. `by_start_sequence` requires `opt_start_seq` and `by_start_time` requires `opt_start_time`;
  either start option with another policy, or without one, is an error. With `last_per_subject` and no filter, the
  client sends the filter `>`.
- `replay_policy`, `headers_only` and `metadata`.
- `inactive_threshold`, default 5 minutes.
- `idle_heartbeat`, default 5 seconds, or for a continuous read the default of ADR-37's consume; set on the consumer
  in push mode and on each pull request in pull mode.
- A name prefix, default a random unique value such as a NUID. The client names the consumers it creates
  `<prefix>_<n>`, where `n` starts at 1 and grows by 1 for each recreation. A prefix that is not valid in a consumer
  name as ADR-6 defines it, or that is longer than 234 bytes so that `<prefix>_<n>` could exceed 255 bytes, is an
  error.
- The maximum recreation attempts, default no limit.

The client sets `ack_policy: none`, `max_deliver: 1`, `mem_storage: true` and `num_replicas: 1`, and in push mode
`flow_control: true` and the deliver subject. The user may provide one of these settings only with the value the
client sets; another value, a deliver subject, or `flow_control` in pull mode is an error. Any other setting, a
negative value, binding to an existing consumer and joining a queue group are an error. A zero, empty or `false` value
of any setting or read option counts as not provided, and so does an enumerated setting equal to its default
(`deliver_policy: all`, `replay_policy: instant`, `ack_policy: none`). The client finds every error above itself,
before it sends any consumer create request.

The client creates the first consumer when the ordered consumer is created, and creates every consumer with a
create-or-update request. If creating the first one fails, creating the ordered consumer is an error.

### Pull reads

A client offers some or all of ADR-37's read methods on a pull ordered consumer: fetch, next and consume. Consume is
the continuous read below, and next is a fetch with a batch of 1, so every rule about fetches holds for it.

A pull ordered consumer is read either continuously or by fetches, and the first read decides which, even when that
first read is an error. After a continuous read, another continuous read or a fetch is an error, even once the first
read stopped. After a fetch, a continuous read is an error. Starting a fetch while another runs is an error.

A fetch takes a batch, a byte limit, an `expires` and no-wait as read options; a continuous read takes a batch, a byte
limit and an `expires`, and sends its pull requests as ADR-37's consume does. `idle_heartbeat` is set on the ordered
consumer; a client may also take it per read, and then that is the provided value. Every pull request carries an
`expires`, by default 30 seconds, and the `idle_heartbeat`, except a no-wait fetch's, which carries `no_wait: true`
instead of either. When no `idle_heartbeat` is provided and a fetch's `expires` is under 10 seconds, the fetch sends
no heartbeat. A read whose pull request would carry a provided `idle_heartbeat` over half its `expires`, a read option
with a negative value, a no-wait fetch with an `expires`, and a fetch with neither a batch nor a byte limit are an
error before the read sends anything.

Fetches do not create their own consumer; each fetch uses the current consumer. A fetch sends its batch and byte limit
as the pull request's `batch` and `max_bytes`, and with only a byte limit it sends a batch of 1,000,000. It ends when
its batch or byte limit is met, it expires, a gap ends it, the recreation attempts are used up, or the server ends its
pull request with a status that is not a reason to recreate, such as a `409` status with description
`Message Size Exceeds MaxBytes`; any other reason to recreate continues it with the new consumer. A fetch expires when
the server ends its pull request with a `408` status or, if none arrives, on the client's own timer shortly after its
`expires`. Every stream message received for a pull request counts toward its limits, also one the client dropped or
reported as an error, and a limit is met once the count or the bytes reach it. A message counts toward a byte limit
with its subject, reply subject, headers and payload, as ADR-37 calculates message size.

### Sequence tracking

The last stream sequence and last consumer sequence are those of the last message the client accepted for delivery to
the application, and 0 before any message. The next expected consumer sequence is the last consumer sequence plus 1.
The client drops any message from a consumer other than the current one, which it tells by the consumer name in the
message's reply subject, read in the JetStream ack subject format ADR-37 defines; such a message is never a gap. It
accepts a message from the current consumer only when its consumer sequence is the next expected one. The stream
sequence is not checked: it can jump ahead, for example with a filter, after messages were deleted, or at a new
consumer's first message, and that is not a gap.

A message without a status header whose reply subject is not in that format is reported to the application as an
error instead of handed to it, and the read continues; it still starts the heartbeat interval again, changes no
sequence and causes no recreation.

### Heartbeats and flow control

The client handles heartbeats and flow control messages itself and never hands them to the application. Each consumer
gets its own deliver subject, or in pull mode its own inbox, and every message belongs to the consumer whose subject it
arrives on; one that arrives on an old consumer's subject is dropped, whatever its reply subject.

Heartbeats are missed when neither a message nor a heartbeat arrives within twice the `idle_heartbeat` interval. The
interval starts when a consumer is created, when a pull request is sent and when the client reconnects, and starts
again whenever any message for the current consumer arrives: a stream message, a heartbeat, a flow control message or
a status message. A message that arrives on an old consumer's subject does not count. The client does not check for
missed heartbeats while it is not connected, while it recreates the consumer, for a pull request that carries no
`idle_heartbeat`, or in pull mode while no pull request is outstanding. A pull request is outstanding from when it is
sent until its batch or byte limit is met, or the server ends it with any status other than a `100` idle heartbeat,
such as the `408` status that marks its expiry.

In push mode and for a continuous read, the client hands messages to the application in one or more of three ways,
and every rule holds for each way it offers: a callback called for each message, an iterator whose call returns the
next message, or a channel, a queue the application holds and takes messages from itself. A message counts as handed
to the application once the callback for it returns, once an iterator call returns it, or once the client has put it
in the channel, and a message the client dropped or reported as an error counts as handed too.

In push mode the client replies with an empty message to the reply subject of a flow control message once every
message before it has been handed to the application, and to the subject in a heartbeat's `Nats-Consumer-Stalled`
header as soon as the heartbeat arrives. It sends neither reply for an old consumer.

### Recovery

The client recreates the consumer when:

- a message arrives whose consumer sequence is not the next expected one
- a heartbeat arrives whose `Nats-Last-Consumer` header is equal to or greater than the next expected consumer
  sequence
- heartbeats are missed
- the server reports that the consumer was deleted, with a `409` status and description `Consumer Deleted`
- a pull request gets no responders

The first two are a gap. A heartbeat without a `Nats-Last-Consumer` header, or whose value is not a number made of
ASCII digits only that fits in an unsigned 64-bit integer, is not a gap. While the client recreates the consumer, it
ignores any further reason to recreate until the new consumer is created.

To recreate the consumer, the client:

1. Stops reading from the current consumer: in push mode it unsubscribes the old deliver subject, and in pull mode it
   sends no more pull requests to it. The application's subscription or read continues.
2. Sends a request to delete the current consumer, and does not wait for the reply. The delete does not have to be sent
   before the create. It is sent once per recreation, not again for a retry, a resume after a reconnect or a restart
   by the next fetch.
3. Sets the last consumer sequence back to 0 and keeps the last stream sequence.
4. Creates the next `<prefix>_<n>` consumer with the same settings, starting:
   - after a message was accepted, with `deliver_policy: by_start_sequence`, `opt_start_seq` the last stream sequence
     plus 1, and no `opt_start_time`;
   - before any message was accepted, where the user asked the first consumer to start.

When creating a new consumer during recovery fails for any reason, no responders included, the client tries again
with the same name. Each attempt times out after 10 seconds without a reply, and a later reply is ignored. The client
waits 1 second before the first retry, doubling the wait up to 10 seconds; the wait starts again at 1 second for each
recreation. The maximum recreation attempts counts every attempt of one recreation, the first included. When they are
used up, the client reports the error to the application and stops the read; a fetch first returns the messages
accepted so far, then the error, and only that fetch ends: the next fetch starts the recreation again, with its
attempts and waits counted afresh. Besides that, the read reports nothing about recovery. Whether the client also
tells the application about recovery by other means, such as an error callback, is up to the client.

While the client is not connected, it neither starts nor continues a recreation; once it reconnects, it acts on a
reason to recreate that arrived before the disconnect and resumes a recreation under way. An attempt still waiting for
its reply when the connection drops does not count; once the client reconnects it sends the next attempt right away,
in place of the wait it was in or of the attempt it lost, and the waits after it go on with the next one in the
sequence.

A gap during a fetch ends that fetch with the messages accepted before the gap; the old consumer stays the current one
until the next fetch starts and recreates the consumer before its pull request. For any other reason to recreate, the
fetch continues with the new consumer, with what is left of its batch, byte limit and expiry. A fetch's expiry runs
from its start and keeps running while it waits for a new consumer, also while the client is not connected. A fetch
that expires before the new consumer is created, or has less than 1 second left once it is, ends with the messages
accepted so far. A new consumer created by then is the current one, and the next fetch uses it; otherwise the
recreation stops with the fetch, a create reply that arrives after that is ignored, and the next fetch starts the
recreation again with the same name before its pull request, with its attempts and waits counted afresh. A next fetch
that creates the consumer before its pull request, after a gap or to restart a stopped recreation, sends its whole
batch and byte limit and what is left of its expiry; a restarted recreation keeps its name.

The client may let the application read the current consumer's info, which it asks the server for. Reading it is an
error from when the client decides to recreate the consumer until the new one is created, and once the read has
stopped, whether because the recreation attempts were used up or because the application stopped it. After a gap
during a fetch, the client decides to recreate the consumer only when the next fetch starts, so until then reading
it returns the old consumer's info.

## Consequences

- Messages are not acknowledged and not redelivered. The client recovers a lost message by reading it again from the
  stream. When the stream no longer holds it, the application is not told that it was skipped.
- The consumer is a single replica held in memory, so it costs the server little, and it is lost when its server
  restarts. The client then recreates it.
- A consumer recreated before any message was accepted starts where the first one was asked to start. With a
  `deliver_policy` of `new` or `last`, messages published in between can be skipped.
- After a recreation, a `deliver_policy` of `last_per_subject` delivers every later message, not only the last one per
  subject.
- The consumer's name changes with every recreation.
