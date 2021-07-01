![NATS](large-logo.png)

# NATS Architecture And Design

This repo is used to capture architectural and design decisions as a reference of the server implementation and expected client behavior.

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
## ADRs for **jetstream**

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-1](adr/0001-jetstream-json-api-design.md)|jetstream, client, server|JetStream JSON API Design|
|[ADR-2](adr/0002-nats-typed-messages.md)|jetstream, server, client|NATS Typed Messages|
|[ADR-7](adr/0007-error-codes.md)|server, client, jetstream|NATS Server Error Codes|
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

## When to write an ADR

Not every little decision needs an ADR, and we are not overly prescriptive about the format apart from the initial header format.
The kind of change that should have an ADR are ones likely to impact many client libraries, server configuration, security, deployment
and those where we specifically wish to solicit wider community input.

## Template

Please see the [template](adr-template.md). The template is a guideline, a suggestion. Feel free to add sections as you feel appropriate. Look at the other ADRs for examples.

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
