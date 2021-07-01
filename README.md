![NATS](large-logo.png)

# NATS Client Functionality Record

This repo is used as a reference of suggested client behavior based on available server functionality. 

## Architecture Decision Records

The [adr](adr) directory will hold documents that suggest client API and behavior. Not all API and behavior is appropriate or implementable for all programming languages or paradigms, 
but it is a best effort to define behavior in order to provide consistency between clients.

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

## Tracking

There is a [Client Feature Parity](https://docs.google.com/spreadsheets/d/1VcYcKqwOp8h8zZwNSRXMS5wrdA1jZz6AumMTHZbXrmY/edit#gid=1032495336) spreadsheet that tracks the clients somewhat, but it is not guaranteed to be complete or up to date. 

## Index

| ID  | Name / Link | Comments |
| ---- | ---- | --- |
| 0001 | [Ephemeral Consumer Behavior](adr/0001-ephemeral-consumer-behavior.md) | |

