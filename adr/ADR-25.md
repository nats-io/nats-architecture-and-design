# Connection Re-Authentication 

|Metadata| Value                                                         |
|--------|---------------------------------------------------------------|
|Date    | 2022-06-29                                                    |
|Author  | @tbeets, @derekcollison>                                      |
|Status  | `Proposed` |
|Tags    | client, server                                     |

## Context and Problem Statement

There is latency and resource overhead for a NATS client and a NATS server to negotiate and establish a TCP connection, 
especially when the connection is TLS enabled.

In some cases it would be useful for a client (with an established connection) to initiate a re-authentication challenge
with the server and forego overhead (on both ends) of connection teardown and re-establishment.

Use cases include (but not limited to):

* Refresh soon-to-expire credential (e.g. User JWT expiry) with a fresh credential
* Switch client credential to affect different account identity and user entitlement

## Design

TODO