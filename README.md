![NATS](large-logo.png)

# NATS Architecture And Design

This repository captures Architecture, Design Specifications and Feature Guidance for the NATS ecosystem.

# Architecture Decision Records
## Client

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-1](adr/ADR-1.md)|jetstream, client, server|JetStream JSON API Design|
|[ADR-2](adr/ADR-2.md)|jetstream, server, client|NATS Typed Messages|
|[ADR-4](adr/ADR-4.md)|server, client|NATS Message Headers|
|[ADR-5](adr/ADR-5.md)|server, client|Lame Duck Notification|
|[ADR-6](adr/ADR-6.md)|server, client|Naming Rules|
|[ADR-7](adr/ADR-7.md)|server, client, jetstream|NATS Server Error Codes|
|[ADR-8](adr/ADR-8.md)|jetstream, client, kv, spec|JetStream based Key-Value Stores|
|[ADR-9](adr/ADR-9.md)|server, client, jetstream|JetStream Consumer Idle Heartbeats|
|[ADR-10](adr/ADR-10.md)|server, client, jetstream|JetStream Extended Purge|
|[ADR-11](adr/ADR-11.md)|client|Hostname resolution|
|[ADR-13](adr/ADR-13.md)|jetstream, client|Pull Subscribe internals|
|[ADR-14](adr/ADR-14.md)|client, security|JWT library free jwt user generation|
|[ADR-17](adr/ADR-17.md)|jetstream, client|Ordered Consumer|
|[ADR-18](adr/ADR-18.md)|client|URL support for all client options|
|[ADR-19](adr/ADR-19.md)|jetstream, client, kv, objectstore|API prefixes for materialized JetStream views|
|[ADR-20](adr/ADR-20.md)|jetstream, client, objectstore, spec|JetStream based Object Stores|
|[ADR-21](adr/ADR-21.md)|client|NATS Configuration Contexts|
|[ADR-22](adr/ADR-22.md)|jetstream, client|JetStream Publish Retries on No Responders|
|[ADR-31](adr/ADR-31.md)|jetstream, client, server|JetStream Direct Get|
|[ADR-32](adr/ADR-32.md)|client, spec|Service API|
|[ADR-33](adr/ADR-33.md)|jetstream, client, server|Metadata for Stream and Consumer|
|[ADR-34](adr/ADR-34.md)|jetstream, client, server|JetStream Consumers Multiple Filters|
|[ADR-36](adr/ADR-36.md)|jetstream, client, server|Subject Mapping Transforms in Streams|
|[ADR-37](adr/ADR-37.md)|jetstream, client, spec|JetStream Simplification|
|[ADR-40](adr/ADR-40.md)|client, server, spec|NATS Connection|
|[ADR-43](adr/ADR-43.md)|jetstream, client, server|JetStream Per-Message TTL|

## Deprecated

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-15](adr/ADR-15.md)|deprecated|JetStream Subscribe Workflow|

## Jetstream

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-1](adr/ADR-1.md)|jetstream, client, server|JetStream JSON API Design|
|[ADR-2](adr/ADR-2.md)|jetstream, server, client|NATS Typed Messages|
|[ADR-7](adr/ADR-7.md)|server, client, jetstream|NATS Server Error Codes|
|[ADR-8](adr/ADR-8.md)|jetstream, client, kv, spec|JetStream based Key-Value Stores|
|[ADR-9](adr/ADR-9.md)|server, client, jetstream|JetStream Consumer Idle Heartbeats|
|[ADR-10](adr/ADR-10.md)|server, client, jetstream|JetStream Extended Purge|
|[ADR-12](adr/ADR-12.md)|jetstream|JetStream Encryption At Rest|
|[ADR-13](adr/ADR-13.md)|jetstream, client|Pull Subscribe internals|
|[ADR-17](adr/ADR-17.md)|jetstream, client|Ordered Consumer|
|[ADR-19](adr/ADR-19.md)|jetstream, client, kv, objectstore|API prefixes for materialized JetStream views|
|[ADR-20](adr/ADR-20.md)|jetstream, client, objectstore, spec|JetStream based Object Stores|
|[ADR-22](adr/ADR-22.md)|jetstream, client|JetStream Publish Retries on No Responders|
|[ADR-28](adr/ADR-28.md)|jetstream, server|JetStream RePublish|
|[ADR-31](adr/ADR-31.md)|jetstream, client, server|JetStream Direct Get|
|[ADR-33](adr/ADR-33.md)|jetstream, client, server|Metadata for Stream and Consumer|
|[ADR-34](adr/ADR-34.md)|jetstream, client, server|JetStream Consumers Multiple Filters|
|[ADR-36](adr/ADR-36.md)|jetstream, client, server|Subject Mapping Transforms in Streams|
|[ADR-37](adr/ADR-37.md)|jetstream, client, spec|JetStream Simplification|
|[ADR-43](adr/ADR-43.md)|jetstream, client, server|JetStream Per-Message TTL|

## Kv

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-8](adr/ADR-8.md)|jetstream, client, kv, spec|JetStream based Key-Value Stores|
|[ADR-19](adr/ADR-19.md)|jetstream, client, kv, objectstore|API prefixes for materialized JetStream views|

## Objectstore

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-19](adr/ADR-19.md)|jetstream, client, kv, objectstore|API prefixes for materialized JetStream views|
|[ADR-20](adr/ADR-20.md)|jetstream, client, objectstore, spec|JetStream based Object Stores|

## Observability

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-3](adr/ADR-3.md)|observability, server|NATS Service Latency Distributed Tracing Interoperability|
|[ADR-41](adr/ADR-41.md)|observability, server|NATS Message Path Tracing|

## Security

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-14](adr/ADR-14.md)|client, security|JWT library free jwt user generation|
|[ADR-38](adr/ADR-38.md)|server, security|OCSP Peer Verification|
|[ADR-39](adr/ADR-39.md)|server, security|Certificate Store|

## Server

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-1](adr/ADR-1.md)|jetstream, client, server|JetStream JSON API Design|
|[ADR-2](adr/ADR-2.md)|jetstream, server, client|NATS Typed Messages|
|[ADR-3](adr/ADR-3.md)|observability, server|NATS Service Latency Distributed Tracing Interoperability|
|[ADR-4](adr/ADR-4.md)|server, client|NATS Message Headers|
|[ADR-5](adr/ADR-5.md)|server, client|Lame Duck Notification|
|[ADR-6](adr/ADR-6.md)|server, client|Naming Rules|
|[ADR-7](adr/ADR-7.md)|server, client, jetstream|NATS Server Error Codes|
|[ADR-9](adr/ADR-9.md)|server, client, jetstream|JetStream Consumer Idle Heartbeats|
|[ADR-10](adr/ADR-10.md)|server, client, jetstream|JetStream Extended Purge|
|[ADR-26](adr/ADR-26.md)|server|NATS Authorization Callouts|
|[ADR-28](adr/ADR-28.md)|jetstream, server|JetStream RePublish|
|[ADR-30](adr/ADR-30.md)|server|Subject Transform|
|[ADR-31](adr/ADR-31.md)|jetstream, client, server|JetStream Direct Get|
|[ADR-33](adr/ADR-33.md)|jetstream, client, server|Metadata for Stream and Consumer|
|[ADR-34](adr/ADR-34.md)|jetstream, client, server|JetStream Consumers Multiple Filters|
|[ADR-36](adr/ADR-36.md)|jetstream, client, server|Subject Mapping Transforms in Streams|
|[ADR-38](adr/ADR-38.md)|server, security|OCSP Peer Verification|
|[ADR-39](adr/ADR-39.md)|server, security|Certificate Store|
|[ADR-40](adr/ADR-40.md)|client, server, spec|NATS Connection|
|[ADR-41](adr/ADR-41.md)|observability, server|NATS Message Path Tracing|
|[ADR-43](adr/ADR-43.md)|jetstream, client, server|JetStream Per-Message TTL|

## Spec

|Index|Tags|Description|
|-----|----|-----------|
|[ADR-8](adr/ADR-8.md)|jetstream, client, kv, spec|JetStream based Key-Value Stores|
|[ADR-20](adr/ADR-20.md)|jetstream, client, objectstore, spec|JetStream based Object Stores|
|[ADR-32](adr/ADR-32.md)|client, spec|Service API|
|[ADR-37](adr/ADR-37.md)|jetstream, client, spec|JetStream Simplification|
|[ADR-40](adr/ADR-40.md)|client, server, spec|NATS Connection|

## When to write an ADR

We use this repository in a few ways:

 1. Design specifications where a single document captures everything about a feature, examples are ADR-8, ADR-32, ADR-37 and ADR-40
 1. Guidance on conventions and design such as ADR-6 which documents all the valid naming rules
 1. Capturing design that might impact many areas of the system such as ADR-2

We want to move away from using these to document individual minor decisions, moving instead to spec like documents that are living documents and can change over time. Each capturing revisions and history.

## Template

Please see the [template](adr-template.md). The template body is a guideline. Feel free to add sections as you feel appropriate. Look at the other ADRs for examples. However the initial Table of metadata and header format is required to match.

After editing / adding a ADR please run `go run main.go > README.md` to update the embedded index. This will also validate the header part of your ADR.
