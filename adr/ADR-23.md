# JetStream Simplified Client API 

|Metadata| Value             |
|--------|-------------------|
|Date    | 2022-06-27        |
|Author  | @aricart, @tbeets |
|Status  | Proposed          |
|Tags    | jetstream, client |

## Context and Problem Statement

JetStream implements signaling choreography between client and server to affect the lifecycle
of streams and stream consumers and implement At-Least-Once message publishing and delivery end-to-end. 

An app developer could implement and use JetStream leveraging only NATS pub-sub "core" primitives, but this would not be
productive for typical application development, thus NATS client libraries have been enhanced with JetStream
methods and abstractive helper features. 

With community uptake and learnings, it's clear that first-generation client library abstractions are of the "leaky" variety, requiring the app developer to
have both intimate knowledge of low-level JetStream architecture and choreography semantics on the one hand **and** obscuring some 
runtime actions in others, often causing developer confusion and misunderstanding.

## [Context | References | Prior Work]

[What does the reader need to know before the design. These sections and optional, can be separate or combined.]

## Design

[If this is a specification or actual design, write something here.]

## Decision

[Maybe this was just an architectural decision...]

## Consequences

[Any consequences of this design, such as breaking change or Vorpal Bunnies]
