# Auth Callout

|Metadata| Value                     |
|--------|---------------------------|
|Date    | 2022-06-29                |
|Author  | @tbeets, @derekcollison   |
|Status  | `Proposed`                |
|Tags    | client, server |

## Context and Problem Statement

For certain use cases, an organization may wish to implement and provide an identity provider service (IdP) 
made available as a NATS service. Such an IdP would take service requests over NATS, determine the validity and merits of passed
payload materials (e.g. relevant 1st-party credentials recognized by the IdP), and accordingly return a reply either with an
error or a new (issued) NATS credential for the client.

Use cases may include:

* Client initiates a "refresh" of a NATS credential that it knows is getting close to expiry
* Client is bootstrapping from initial/low-value credentials it has at time of provisioning (e.g. edge device scenario) to full credentials

## Design

TODO
