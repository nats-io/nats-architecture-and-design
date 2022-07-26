# Exclusive Consumer 

| Metadata | Value             |
|----------|-------------------|
| Date     | 2022-7-15         |
| Author   | @tbeets           |
| Status   | `Proposed`        |
| Tags     | jetstream, client |

## Context and Problem Statement

There are use cases where Clients of a Consumer represent a _worker pool_ to process Stream messages (filtered and
delivered via a Consumer's view of Stream). The semantics of a worker pool -- the number of active Clients, the 
alottment of work between Clients, the lifecycle of Clients, and the worker-identity of each Client -- is application 
defined.  The relationship of an individual worker (and its processing state and duties) to one (or more) business 
payloads delivered as messages by a Consumer is also application defined.

There is a class of worker pool semantics, where the processing desire is to have at most a single worker Client, 
referred to as "exclusive consumer" consuming the work stream. The duration of exclusivity is application defined and
generally relates to both the technical lifecycle of the application Client (e.g. health and viability of a Client to
continue processing a work stream) and the application-specific consequences of a new/different application Client
processing the same work stream.

This ADR defines requirements and general mechanism for a Pull JS Consumer to be configured for Exclusive Consumer
mode and the mechanism for Clients to convey their worker identity and duration of exclusivity to the Pull JS Consumer
in each Fetch Request.

This ADR assumes a minimum NATS Server release of v2.9.0 (round-robin fetch request delivery).

## [Context | References | Prior Work]

[What does the reader need to know before the design. These sections and optional, can be separate or combined.]

## Design

[If this is a specification or actual design, write something here.]

## Decision

[Maybe this was just an architectural decision...]

## Consequences

[Any consequences of this design, such as breaking change or Vorpal Bunnies]
