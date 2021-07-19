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

## Schema Object Fields

Object and list of fields within the object that are unsigned 64 bit values.

#### `consumer_configuration`
 
* `opt_start_seq`

#### `consumer_create_request`

* `opt_start_seq`

#### `consumer_create_response`

* `consumer_seq`
* `stream_seq`

#### `consumer_info_response`

* `opt_start_seq`
* `consumer_seq`
* `stream_seq`

#### `consumer_list_response`

* `opt_start_seq`
* `consumer_seq`
* `stream_seq`

#### `stream_configuration`
 
* `opt_start_seq`

#### `stream_create_request`

* `opt_start_seq`

#### `stream_create_response`

* `opt_start_seq`

#### `stream_info_response`

* `opt_start_seq`
* `first_seq`
* `last_seq`

#### `stream_purge_response`

* `purged`


#### `pub_ack_response`

* `seq`

#### `stream_purge_request`

* `seq`
* `keep`

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
