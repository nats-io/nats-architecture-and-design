# JetStream Filestore Compression

| Metadata | Value                     |
|----------|---------------------------|
| Date     | 2023-05-01                |
| Author   | @neilalexander            |
| Status   | Implemented               |
| Tags     | jetstream, client, server |

## Context and Problem Statement

Use of filestore encryption can almost completely prevent host filesystem compression or deduplication from working effectively. This may present a particular problem in environments where encryption is mandated for compliance reasons but local storage is either limited or expensive. Having the ability for the NATS Server to compress the message block content before encryption takes place can help in this area.

## References

Compression and decompression of messages is performed transparently by the NATS Server if configured to do so, therefore clients do not need to be modified in order to publish to or consume messages from a stream. However, clients will need to be modified in order to be able to configure or inspect the compression on a stream.

- Server PRs:
  - <https://github.com/nats-io/nats-server/pull/4004>
  - <https://github.com/nats-io/nats-server/pull/4072>
- JetStream schema:
  - <https://github.com/nats-io/jsm.go/pull/445>
- NATS CLI:
  - <https://github.com/nats-io/natscli/pull/762>

## Design

The stream configuration will gain a new optional `"compression"` field. If supplied, the following values are valid:

- `"none"` — No compression is enabled on the stream
- `"s2"` — S2 compression is enabled on the stream

This field can be provided when creating a stream with `$JS.API.STREAM.CREATE`, updating a stream with `$JS.API.STREAM.UPDATE` and it will be returned when requesting the stream info with `$JS.API.STREAM.INFO`.

When enabled, message blocks will be compressed asynchronously when they cease to be the tail block — that is, at the point that the message block reaches the maximum configured block size and a new block is created. This is to prevent unnecessary decompression and recompression of the tail block while it is still being written to, which would reduce publish throughput.

Compaction and truncation operations will also compress/decompress any relevant blocks synchronously as required.

Compressed blocks gain a new prepended header describing not only the compression algorithm in use but also the original block content size. This header is encrypted along with the rest of the block when filestore encryption is enabled. Absence of this header implies that the block is not compressed and the NATS Server will not ordinarily prepend a header to an uncompressed block. The presence of the original block content size within the header makes it possible to determine the effective compression ratio later without having to decompress the block, although the NATS Server does not currently do this.

The checksum at the end of the block is specifically excluded from compression and remains on disk as-is, so that checking the block integrity does not require decompressing the entire block.

## Decision

The design is such that different compression algorithms can easily be implemented within the NATS Server if necessary. Initially, only S2 compression is in scope.

Both block and individual message compression were initially explored. In order to benefit from repetition across individual messages (particularly where the data is structured, i.e. in JSON format), compression at the block level provides significantly better compression ratios over compressing individual messages separately.

The compression algorithm can be updated after the stream has been created. Newly minted blocks will use the newly selected compression algorithm, but this will not result in existing blocks being proactively compressed or decompressed. An existing block will only be compressed or decompressed according to the newly configured algorithm when it is modified for another reason, i.e. during truncation or compaction.

## Consequences

Compression requires extra system resources, therefore it is anticipated that a compressed stream may suffer some performance penalties compared to an uncompressed stream.
