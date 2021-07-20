# JetStream API unsigned 64 bit numerical value fields

|Metadata|Value|
|--------|-----|
|Date    |2021-07-15|
|Author  |@scottf|
|Status  |Approved|
|Tags    |server, client, jetstream|

## Context

This document summarizes fields in the JetStream api schema that are unsigned 64 bit numerical values. 
The most up-to-date information can be found in the [schema](https://github.com/nats-io/jsm.go/tree/main/schemas/jetstream/api/v1)
Some fields are unsigned 64 bit values. These will be communicated as numbers in json. Clients must handle them, even if it means custom parsing.

## Example Schema Object Fields

* All message sequence or ids.
* duration fields in nanos

## Message Meta Data

The message meta data carried in the reply_to field of a JetStream message contains meta data. 
All numerical fields are unsigned 64 bit values.

Example:

```
$JS.ACK.test-stream.test-consumer.4.5.6.1605139610113260007
```

|Index|Description|
|---|---|
|4|number of delivered messages|
|5|stream sequence|
|6|consumer sequence|
|7|timestamp nanos|
