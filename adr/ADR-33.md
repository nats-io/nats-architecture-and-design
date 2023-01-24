# Metadata for Stream and Consumer

|Metadata|Value|
|--------|-----|
|Date    |2023-01-23|
|Author  |@Jarema|
|Status  |Approved|
|Tags    |jetstream, client, server|

## Context and Problem Statement

Until now, there was no way to easily add additional information about Stream or Consumer.
The only solution was using `Description` field, which is a not ergonomic workaround.

## Server PR
https://github.com/nats-io/nats-server/pull/3797

## Design

The solution is to add new `metadata` field to both `Consumer` and `Stream` config.
The `metadata` field would by a map of `string` keys and `string` values.

### JSON representation
The map would be represented in json as object with nested key/value pairs, which is a default
way to marshal maps/hashmaps in most languages.

### Size limit
To avoid abuse of the metadata, the size of it is limited to 128KB.
Size is equal to len of all keys and values summed.

### Reserved prefix
`_nats` is a reserved prefix.
Will be used for any potential internals of server or clients.
Server can lock its metadata to be immutable and deny any changes.


### Example
```json
{
  "durable_name": "consumer",
  ... // other consumer/stream fields
  "metadata": {
    "owner": "nack",
    "domain": "product",
    "_nats_created_version": "1.10.0"
  }
}

```


