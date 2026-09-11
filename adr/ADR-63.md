# JetStream Stream Backup and Restore V2

| Metadata | Value                          |
|----------|--------------------------------|
| Date     | 2026-09-08                     |
| Author   | @neilalexander                 |
| Status   | Implemented                   |
| Tags     | jetstream, server, client, 2.15 |

| Revision | Date       | Author         | Info           | Server Version | API Level |
|----------|------------|----------------|----------------|----------------|-----------|
| 1        | 2026-09-08 | @neilalexander | Initial design | 2.15.0         | 5         |

## Context

The original stream backup format is an S2-compressed tar archive of the file store's internal layout. It contains stream metadata, message blocks and the state of consumers with stores on the node serving the backup. Restore extracts the archive into a temporary directory under the account's store directory, moves the files into place and creates the stream from them.

This design has several limitations:

- Memory streams cannot be backed up.
- Backups depend on the file store's layout. Restoring those files into a memory stream does not load their messages, even if the restore reports success.
- Restore requires a temporary copy of the extracted files on disk.
- The legacy restore path checks resource limits using cumulative uncompressed tar entry sizes. It does not validate each message's size and sequence individually.
- A clustered backup includes only consumer stores on the stream leader's node. It can omit consumers placed elsewhere, such as an R1 consumer on an R3 stream, and include a local replica's state rather than the consumer leader's state.
- Stream and consumer activity can start before restore has finished.
- Snapshot completion advisories have no field for reporting transfer failures.

The replacement format supports both storage types, separates the archive from the storage engine and restores messages directly into the destination store. It also includes consumers placed elsewhere in the cluster, defers stream and consumer activation, and reports snapshot failures to clients and operators.

## Archive format

The backup is a sequence of entries compressed as a single S2 stream. It can be written and read in one pass without seeking.

The decompressed archive starts with the eight-byte magic `NATSARC1`. Each entry then contains:

| Field         | Encoding | Description                                      |
|---------------|----------|--------------------------------------------------|
| `NameLen`     | uvarint  | Entry name length in bytes                       |
| `Timestamp`   | varint   | Unix timestamp in nanoseconds; `0` when unset     |
| `Sequence`    | uvarint  | Stream sequence; `0` for metadata or the sentinel |
| `HeaderSize`  | uvarint  | Message header length in bytes                   |
| `PayloadSize` | uvarint  | Message body or metadata length in bytes         |
| `Name`        | bytes    | Entry name, `NameLen` bytes                       |
| `Payload`     | bytes    | Headers followed by body, `HeaderSize + PayloadSize` bytes |

Entries strictly appear in this order:

1. `state.json`: the initial stream state, encoded as JSON.
2. `consumers/<name>`: one JSON entry per included consumer.
3. Messages in ascending sequence order, with each subject as the entry name and its sequence and timestamp in the entry header.
4. An end-of-backup sentinel with an empty name and all numeric fields set to zero.

Restore requires the sentinel to report success. Reaching the end of the archive before it is an error, including when the archive ends cleanly between entries. The error can report unexpected end of input or an invalid archive; clients should not depend on a specific truncation error message.

### Stream state

The first entry describes the stream state, with the consumer count adjusted to match the entries that follow. For example:

```json
{
  "messages": 100,
  "bytes": 12800,
  "first_seq": 1,
  "first_ts": "2026-09-08T10:00:00Z",
  "last_seq": 100,
  "last_ts": "2026-09-08T10:01:00Z",
  "num_subjects": 1,
  "consumer_count": 1
}
```

Strictly speaking, `messages` and `bytes` are not guaranteed to be correct. They are a snapshot of the state of the stream at the beginning of the restore process, but message deletes could be processed as the backup is being generated. Only the range `first_seq` to `last_seq` strictly matters, but `bytes` is used to estimate the reservation needed for the restore to complete.

`consumer_count` is zero when `no_consumers` is set. Otherwise, it counts the stream's consumers included in the backup, including those placed elsewhere in a cluster. `first_seq` sets the store's initial sequence and `last_seq` preserves trailing sequence gaps.

The archive contains neither the stream configuration nor its creation time. The restore request supplies the configuration, including the destination storage type. Backup tooling must retain the configuration returned separately in the snapshot API response for a later restore, as is done with the existing stream backup today.

### Consumer state

Each consumer entry combines its configuration and state. This example abbreviates the configuration and shows a consumer with no outstanding acknowledgements:

```json
{
  "config": {
    "name": "worker",
    "durable_name": "worker",
    "deliver_policy": "all",
    "ack_policy": "explicit",
    "replay_policy": "instant"
  },
  "state": {
    "delivered": {
      "consumer_seq": 100,
      "stream_seq": 100
    },
    "ack_floor": {
      "consumer_seq": 100,
      "stream_seq": 100
    }
  }
}
```

The optional `pending` and `redelivered` objects are omitted when empty.

Before writing the entry, the server limits `delivered.stream_seq` and `ack_floor.stream_seq` to the initial stream state's `last_seq`, and removes pending and redelivered entries above that sequence. This clamping leaves consumer sequence numbers unchanged because filtered consumers do not have a one-to-one mapping between consumer and stream sequences.

Unless `no_consumers` is specified in the stream snapshot request, the backup includes each consumer's configuration and stored state, including pending and redelivery information. In a cluster, consumer configuration and requests state is fetched serially from each consumer's leader. These requests run sequentially with a two-second timeout each. Failure to obtain any consumer's state or the cluster metadata needed to identify the consumers fails the backup.

The state reported by a consumer leader does not identify individual pending or redelivered messages. If any messages await acknowledgement, the backup rolls both delivered sequence numbers back to the acknowledgement floor before limiting the stream sequences. Restore can therefore redeliver already acknowledged messages above the ack floor as well as messages still awaiting acknowledgement, and does not preserve their previous delivery counts.

## Snapshot behavior

In a cluster, the stream leader serves the snapshot. The stream store provides the messages for both file and memory storage. Messages read from file storage undergo the store's normal integrity checks. A read error, including detected corruption, aborts the backup. The `jsck` request field has no meaning anymore, the usual store checks are performed regardless.

Archive entries are compressed and streamed to the client as they are produced. Flow control slows archive generation when the receiver cannot keep up. Consumers precede messages so their state is available before restoring interest or work queue streams.

The snapshot does not lock the stream and all its consumers at one instant. Stream state is sampled first, consumers are collected afterwards, and the stream store provides messages while the stream remains active. Only messages between the snapshotted `first_seq` and `last_seq` are included. Concurrent deletions or expiry can still change which messages are included, so the initial state is not an exact manifest of the message entries, but rather a starting and ending bound.

### Chunking and flow control

The snapshot uses the existing delivery subject and acknowledgement protocol:

- The default chunk size is 128 KiB, clamped to 1 KiB–1 MiB.
- The default window size is 8 MiB, clamped to 1 KiB–32 MiB and raised to at least one chunk.
- The maximum number of full chunks awaiting acknowledgement is the window size divided by the chunk size, rounded down: 1–32,768 chunks. Each acknowledgement allows another chunk to be sent.
- The sender allows about two seconds for initial delivery interest to appear.
- A five-second timeout detects stalled transfer progress, measured from the last full chunk sent. Time spent waiting for archive data can delay detection, so this is not a strict deadline measured from the last acknowledgement.

The sender ends the transfer with an empty message. Successful transfers that sent a full chunk use status `204`; a transfer smaller than one chunk can end without a status header. Errors use `408 No Interest`, `408 No Flow Response`, or `500 <error>` for a snapshot generation failure.

## Restore behavior

The restore API recognizes `NATSARC1` after S2 decompression and selects V2 restore. Other input is treated as a legacy tar backup, which remain supported, including their existing disk staging and storage restrictions.

The initial restore response contains the upload subject. Each chunk requires a reply subject. An empty chunk ends the upload; the completion response uses the stream-create response schema, containing either stream information or an error. A five-second inactivity timeout is refreshed as chunks arrive and are consumed.

### Validation and accounting

V2 restore must create a new destination stream. An error will be returned by the server if a restore is attempted on an existing stream.

V2 restore validates the stream configuration and checks that the stream name is available. It also rejects a negative consumer count and inconsistent initial sequence ranges or message counts. An empty stream with an advanced sequence must have `first_seq == last_seq + 1` whereas a never-used stream can have both sequences set to zero. Consumer entries must have the expected prefix and contain both configuration and state.

Every message must satisfy these checks before storage:

- Its sequence is non-zero and strictly greater than the previous processed sequence, starting from `first_seq - 1`.
- Header and body sizes are non-negative and their sum does not overflow.
- Headers plus body fit within the server's `max_payload` and the stream's configured maximum message size.
- The destination store's calculated record size is representable and within its format limits.
- The bytes read match the declared sizes, and the TTL header can be parsed.

Before creating the stream, restore adds `state.json`'s `bytes` to account usage and checks reservation, account/tier and server resource limits. As each message is stored or skipped for expiry, its destination-store size is released from the temporary reservation. Stored messages contribute actual usage through the store's normal accounting. On every exit, any unused reservation is released. Messages retained after a failed restore continue to count towards actual usage.

### Sequences, gaps and TTLs

Restore begins at `first_seq`, preserving sequence numbers even when no messages remain. Gaps between message entries are filled with skipped sequences. A message whose positive per-message TTL has elapsed is skipped while preserving its sequence. After the sentinel, restore skips any remaining sequences through `last_seq`, preserving trailing deletions.

These steps preserve sequence numbering for a consistent backup. They do not guarantee identical retained contents: expired messages are omitted, and the destination stream's limits and retention behavior still apply.

## Advisories

The snapshot completion advisory adds an optional `error` string containing the failure reason.

After a snapshot starts, the completion advisory reports the result of archive delivery. `error` is omitted on success and contains the failure reason otherwise, including `no interest` or `no flow response`. The server logs the result. Failures before a snapshot starts are returned in the API response.

The event type remains `io.nats.jetstream.advisory.v1.snapshot_complete`. Snapshot-create and restore advisory schemas are unchanged; restore completion advisories do not gain this error field.

## API level and compatibility

This work targets server 2.15.0. It adds no stream or consumer configuration fields and no format-selection field to the snapshot or restore requests. The snapshot API writes V2 by default; clients that store and replay opaque archive bytes need no archive parser.

The optional advisory `error` field is an additive schema change. The archive bytes also change on the wire, so this is not solely an advisory change. Older servers cannot restore V2 archives. The new server retains the legacy tar reader for older backups, but that path does not acquire V2's storage portability or restore behavior.

## Summary

- The V2 implementation supports file and memory streams and can restore into either storage type.
- V2 restore writes directly into the destination store without a second extracted copy on disk.
- Clustered backups include assigned consumers placed away from the stream leader. Their state can require broader redelivery and does not preserve sparse pending or redelivery information.
- Backup tooling must retain the stream configuration separately. V2 restore uses the request's configuration and a new creation time.
- Read errors, including detected file-store checksum failures, abort backup generation.
- Snapshot collection is not an atomic view of stream and consumer state, and failed restores are not transactional rollbacks.
- Legacy backups remain readable through the legacy restore path. V2 archives require a server that recognizes and restores the V2 format.
