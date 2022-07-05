# Restrict Client Type and Version 

|Metadata| Value                                                         |
|--------|---------------------------------------------------------------|
|Date    | 2022-07-04                                                    |
|Author  | @tbeets, @codegangsta                                         |
|Status  | `Proposed` |
|Tags    | server                                     |

## Context and Problem Statement

As proposed in server [issue #3215](https://github.com/nats-io/nats-server/issues/3215) from NATS community discuss,
there are use cases in which an operator needs to control the set of clients (by type and version of NATS client library) 
that may connect.

Scenarios could include (but are not limited to):

Client library:
* [does, does not] implement a performance optimization required by the server environment
* [does, does not] implement a functional feature (may be security-related) required by the server environment
* is known to have a bug that may cause undesirable behaviors in the server environment
* has been verified for compatibility with server environment by the operator

Depending on the environment's overall policy requirements, an operator might desire either a 
client _allow list_ (implicitly deny all others) -OR- a client _deny list_ (implicitly allow
all others). 

An operator may want to allow a range of clients but with specific deny exceptions within a range.

Sometimes an operator may need to specify only mininum version (low-bar) and sometimes an operator may want to place
a max version cap (high-bar) as well.

## References

Clients transmits `lang` and `version` in the [`CONNECT` message](https://docs.nats.io/reference/reference-protocols/nats-protocol#connect).

> Note: These field values cannot be verified as accurate by the server, and thus are not used in auth challenge. 

[Subject permissions in NATS](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/authorization#allow-deny-specified) can 
be defined with _allow_ and _deny_ semantics.

## Design

A new `clientpolicy` stanza would be added to server configuration (which could be extensible to various policies) with
a policy type of `version`.

Similar to the current semantics with allow/deny subject permissions, an operator could define allowed clients, denied clients,
or a combination of allowed clients with deny exceptions.

If:
* Allowed clients are specified, then all other clients are denied
* Denied clients are specified, then all other clients are allowed
* Allow and deny are both specified, denial always takes precedence over allowed

Minimum version (min) is an _optional_ field that sets a lower-bound (inclusive).

Maximum version (max) is an _optional_ field that sets an upper-bound (non-inclusive).

Lang is a _required_ field. If only lang is specified (neither min or max version stated) then the policy is simply an
allow or deny of a client library of given language regardless of version.

```text
clientpolicy: {
  version: {
    allow: [
      {lang: "java", min: "2.15.0", max: "3.0.0"}
      {lang: "go", min: "1.15.0"}
      {lang: "python"}
      {lang: "c", max: "4.0.0"}
    ]
    deny: [
      {lang: "java", min: "2.15.4", max: "2.15.18"}
    ]
  }
}
```

## Consequences

Certain clients may be prevented from establishing a connection with a NATS Server that has opted-in to client type/version 
restriction.