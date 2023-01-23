# Metadata for Stream and Consumer

|Metadata|Value|
|--------|-----|
|Date    |2023-01-23|
|Author  |@Jarema|
|Status  |Proposed|
|Tags    |jetstream, client, server|

## Context and Problem Statement

Until now, there was no way to easily add additional information about Stream or Consumer.
The only solution was using `Description` field, which is a not ergonomic workaround.

## Server PR
https://github.com/nats-io/nats-server/pull/3797

## Design

The solution is to add new `metadata` field to both `Consumer` and `Stream` config.
The `metadata` field would by a map of `string` keys and `string` values.

The map would be represented in json as object with nested key/value pairs, which is a default
way to marshal maps/hashmaps in most languages.

To avoid abuse of the metadata, the size of it will be limited to 1MB.
Size will be counter for lenght of keys + values.

### Example
```json
{
  "durable_name": "consumer",
  ... // other consumer/stream fields
  "metadata": {
    "owner": "nack",
    "domain": "product"
  }
}

```


