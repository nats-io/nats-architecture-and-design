![NATS](large-logo.png)

# NATS Client Functionality Record

This repo is used as a reference of suggested client behavior based on available server functionality. 

## Architecture Decision Records

# Architecture Decision Records
## ADRs for **client**

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-1](adr/0001-jetstream-json-api-design.md)|jetstream, client, server|JetStream JSON API Design|
|[ADR-2](adr/0002-nats-typed-messages.md)|jetstream, server, client|NATS Typed Messages|
|[ADR-4](adr/0004-nats-headers.md)|server, client|NATS Message Headers|
|[ADR-5](adr/0005-lame-duck-notification.md)|server, client|Lame Duck Notification|
|[ADR-6](adr/0006-protocol-naming-conventions.md)|server, client|Protocol Naming Conventions|
|[ADR-7](adr/0007-error-codes.md)|server, client, jetstream|NATS Server Error Codes|
|[ADR-8](adr/0008-jetstream-kv.md)|jetstream, client, kv|JetStream based Key-Value Stores|
## ADRs for **jetstream**

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-1](adr/0001-jetstream-json-api-design.md)|jetstream, client, server|JetStream JSON API Design|
|[ADR-2](adr/0002-nats-typed-messages.md)|jetstream, server, client|NATS Typed Messages|
|[ADR-7](adr/0007-error-codes.md)|server, client, jetstream|NATS Server Error Codes|
|[ADR-8](adr/0008-jetstream-kv.md)|jetstream, client, kv|JetStream based Key-Value Stores|
## ADRs for **kv**

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-8](adr/0008-jetstream-kv.md)|jetstream, client, kv|JetStream based Key-Value Stores|
## ADRs for **observability**

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-3](adr/0003-distributed-tracing.md)|observability, server|NATS Service Latency Distributed Tracing Interoperability|
## ADRs for **server**

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-1](adr/0001-jetstream-json-api-design.md)|jetstream, client, server|JetStream JSON API Design|
|[ADR-2](adr/0002-nats-typed-messages.md)|jetstream, server, client|NATS Typed Messages|
|[ADR-3](adr/0003-distributed-tracing.md)|observability, server|NATS Service Latency Distributed Tracing Interoperability|
|[ADR-4](adr/0004-nats-headers.md)|server, client|NATS Message Headers|
|[ADR-5](adr/0005-lame-duck-notification.md)|server, client|Lame Duck Notification|
|[ADR-6](adr/0006-protocol-naming-conventions.md)|server, client|Protocol Naming Conventions|
|[ADR-7](adr/0007-error-codes.md)|server, client, jetstream|NATS Server Error Codes|

## Issues

Issues can be used to request design records or to propose or discuss client features. Eventually issues should become ADRs

## Template

The [template](adr-template.md) is a guideline, a suggestion for what to include in an ADR. Feel free to add or remove sections as you feel appropriate. Look at the other ADRs for examples.

## Repositories

Server [nats-server](https://github.com/nats-io/nats-server)

Reference implementation [nats.go](https://github.com/nats-io/nats.go)

Java Client [nats.java](https://github.com/nats-io/nats..java)

.NET / C# client [nats.net](https://github.com/nats-io/nats.net)

Java Script [nats.ws](https://github.com/nats-io/nats.ws) [nats.deno](https://github.com/nats-io/nats.deno)

C Client [nats.c](https://github.com/nats-io/nats.c)

Python3 Client for Asyncio [nats.py](https://github.com/nats-io/nats.py)
