![NATS](large-logo.png)

# NATS Architecture And Design

This repo is used to capture architectural and design decisions as a reference of the server implementation and expected client behavior.

# Architecture Decision Records
## Client

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-1](adr/ADR-1.md)|jetstream, client, server|JetStream JSON API Design|
|[ADR-2](adr/ADR-2.md)|jetstream, server, client|NATS Typed Messages|
|[ADR-4](adr/ADR-4.md)|server, client|NATS Message Headers|
|[ADR-5](adr/ADR-5.md)|server, client|Lame Duck Notification|
|[ADR-6](adr/ADR-6.md)|server, client|Protocol Naming Conventions|
|[ADR-7](adr/ADR-7.md)|server, client, jetstream|NATS Server Error Codes|
|[ADR-8](adr/ADR-8.md)|jetstream, client, kv|JetStream based Key-Value Stores|
|[ADR-9](adr/ADR-9.md)|server, client, jetstream|JetStream Consumer Idle Heartbeats|
|[ADR-10](adr/ADR-10.md)|server, client, jetstream|JetStream Extended Purge|
|[ADR-11](adr/ADR-11.md)|client|Hostname resolution|
|[ADR-13](adr/ADR-13.md)|jetstream, client|Pull Subscribe internals|
|[ADR-14](adr/ADR-14.md)|client, security|JWT library free jwt user generation|
|[ADR-15](adr/ADR-15.md)|jetstream, client|JetStream Subscribe Workflow|
|[ADR-18](adr/ADR-18.md)|client|URL support for all client options|

## Jetstream

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-1](adr/ADR-1.md)|jetstream, client, server|JetStream JSON API Design|
|[ADR-2](adr/ADR-2.md)|jetstream, server, client|NATS Typed Messages|
|[ADR-7](adr/ADR-7.md)|server, client, jetstream|NATS Server Error Codes|
|[ADR-8](adr/ADR-8.md)|jetstream, client, kv|JetStream based Key-Value Stores|
|[ADR-9](adr/ADR-9.md)|server, client, jetstream|JetStream Consumer Idle Heartbeats|
|[ADR-10](adr/ADR-10.md)|server, client, jetstream|JetStream Extended Purge|
|[ADR-12](adr/ADR-12.md)|jetstream|JetStream Encryption At Rest|
|[ADR-13](adr/ADR-13.md)|jetstream, client|Pull Subscribe internals|
|[ADR-15](adr/ADR-15.md)|jetstream, client|JetStream Subscribe Workflow|

## Kv

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-8](adr/ADR-8.md)|jetstream, client, kv|JetStream based Key-Value Stores|

## Observability

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-3](adr/ADR-3.md)|observability, server|NATS Service Latency Distributed Tracing Interoperability|

## Security

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-14](adr/ADR-14.md)|client, security|JWT library free jwt user generation|

## Server

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-1](adr/ADR-1.md)|jetstream, client, server|JetStream JSON API Design|
|[ADR-2](adr/ADR-2.md)|jetstream, server, client|NATS Typed Messages|
|[ADR-3](adr/ADR-3.md)|observability, server|NATS Service Latency Distributed Tracing Interoperability|
|[ADR-4](adr/ADR-4.md)|server, client|NATS Message Headers|
|[ADR-5](adr/ADR-5.md)|server, client|Lame Duck Notification|
|[ADR-6](adr/ADR-6.md)|server, client|Protocol Naming Conventions|
|[ADR-7](adr/ADR-7.md)|server, client, jetstream|NATS Server Error Codes|
|[ADR-9](adr/ADR-9.md)|server, client, jetstream|JetStream Consumer Idle Heartbeats|
|[ADR-10](adr/ADR-10.md)|server, client, jetstream|JetStream Extended Purge|

## When to write an ADR

Not every little decision needs an ADR, and we are not overly prescriptive about the format apart from the initial header format.
The kind of change that should have an ADR are ones likely to impact many client libraries, server configuration, security, deployment
and those where we specifically wish to solicit wider community input.

For a background of the rationale driving ADRs see [Documenting Architecture Decisions](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions) by
Michael Nygard

## Template

Please see the [template](adr-template.md). The template body is a guideline. Feel free to add sections as you feel appropriate. Look at the other ADRs for examples. However the initial Table of metadata and header format is required to match.

After editing / adding a ADR please run `go run main.go > README.md` to update the embedded index. This will also validate the header part of your ADR.

## Related Repositories

 * Server [nats-server](https://github.com/nats-io/nats-server)
 * Reference implementation [nats.go](https://github.com/nats-io/nats.go)
 * Java Client [nats.java](https://github.com/nats-io/nats..java)
 * .NET / C# client [nats.net](https://github.com/nats-io/nats.net)
 * JavaScript [nats.ws](https://github.com/nats-io/nats.ws) [nats.deno](https://github.com/nats-io/nats.deno)
 * C Client [nats.c](https://github.com/nats-io/nats.c)
 * Python3 Client for Asyncio [nats.py](https://github.com/nats-io/nats.py)

### Client Tracking

There is a [Client Feature Parity](https://docs.google.com/spreadsheets/d/1VcYcKqwOp8h8zZwNSRXMS5wrdA1jZz6AumMTHZbXrmY/edit#gid=1032495336) spreadsheet that tracks the clients somewhat, but it is not guaranteed to be complete or up to date.
