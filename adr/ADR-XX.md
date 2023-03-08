# Subject Mapping Transforms in Streams

|Metadata| Value                     |
|--------|---------------------------|
|Date    | 2023-02-10                |
|Author  | @jnmoyne                  |
|Status  | `Partially Implemented`   |
|Tags    | jetstream, client, server |

## Context and Problem Statement

Subject mapping and transformation is only available at the Core NATS level, meaning that in order to define or modify mappings one has to either have access to the server config file, or have access to the account's key in operator security mode. While Core NATS subject mapping has its place and use (e.g. scaling a single stream for writes using partitioning), many (most) use cases for subject mapping happen in the context of streams, and having to go to the Core NATS server/account level to define subject mappings is quite limiting as it's not easy for an application programmer to be able to define the mappings he/she needs (even if they have access to the account's key).

On the other hand allowing the application of subject mapping transforms at the stream level makes it very easy for the application developers or the NATS administrators to define and manage those mappings. There is more than one place in a stream's message flow where subject mapping transforms can be applied which enables some very interesting new functionalities (e.g. KV bucket sourcing).

## Prior Work

See [ADR-30](ADR-30.md) for Core NATS subject mapping and a description of the available subject transform functions.

## Features introduced

The new features introduced by the [PR](https://github.com/nats-io/nats-server/pull/3814) allow the application of subject mapping transformations in two places in the space configuration:

- You can apply a subject mapping transformation as part of a Stream source.
  - This enables the ability to do sourcing between KV bucket  (as the name of the bucket is part of the subject name in the KV bucket streams, and therefore has to be transformed during the sourcing as the name of the sourcing bucket is different from the name(s) of the bucket(s) being sourced).
- You can apply a subject mapping transformation at the ingres (input) of the stream, meaning after it's been received on Core NATS, or mirrored or sourced from another stream, and before limits are applied (and it gets persisted). This subject mapping transformation is only that, it does not filter messages, it only transforms the subjects of the messages matching the subject mapping source.
  - This enables the ability to insert a partition number as a token in the message subjects.

![](images/stream-transform.png)

In addition, it is now possible to source from the same stream more than once.

From the user's perspective these features manifest themselves as new fields in the Stream Configuration request and Stream Info response messages.

- Additional `"subject_transform_dest"` field in objects in the `"sources"` array of the Stream Config
- Additional `"subject_transform"` field in Stream Config containing two strings: `"src"` and `"dest"`

E.g.
```
{
  "name": "foo",
  "retention": "limits",
  "max_consumers": -1,
  "max_msgs_per_subject": -1,
  "max_msgs": -1,
  "max_bytes": -1,
  "max_age": 0,
  "max_msg_size": -1,
  "storage": "file",
  "discard": "old",
  "num_replicas": 1,
  "duplicate_window": 120000000000,
  "sources": [
    {
      "name": "source1",
      "filter_subject": "stream1.>",
      "subject_transform_dest": "foo.>"
    }
  ],
  "subject_transform": {
    "src": "foo.>",
    "dest": "mapped.foo.>"
  },
  "sealed": false,
  "deny_delete": false,
  "deny_purge": false,
  "allow_rollup_hdrs": false,
  "allow_direct": false,
  "mirror_direct": false
}
```
## Client implementation PRs

- [jsm.go](https://github.com/nats-io/jsm.go/pull/436)
- [nats.go](https://github.com/nats-io/nats.go/pull/1200)
- [natscli](https://github.com/nats-io/natscli/pull/695)