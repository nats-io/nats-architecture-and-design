# Metadata for Stream and Consumer

|Metadata|Value|
|--------|-----|
|Date    |2023-01-23|
|Author  |@Jarema|
|Status  |Approved|
|Tags    |jetstream, client, server|

## Context and Problem Statement

Until now, there was no way to easily add additional information about Stream or Consumer.
The only solution was using `Description` field, which is a ugly workaround.

## Server PR
https://github.com/nats-io/nats-server/pull/3797

## Design

The solution is to add new `metadata` field to both `Consumer` and `Stream` config.
The `metadata` field would by a map of `string` keys and `string` values.

The map would be represented in json as object with nested key/value pairs, which is a default
way to marshal maps/hashmaps in most languages.

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

## Open questions

Do we want to limit the size of `metadata`?
