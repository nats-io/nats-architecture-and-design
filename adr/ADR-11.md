# Hostname resolution

|Metadata|Value|
|--------|-----|
|Date    |2021-07-21|
|Author  |@kozlovic|
|Status  |Approved|
|Tags    |client|

## Context and Problem Statement

The client library should take a random IP address when performing a host name resolution prior to creating the TCP connection.

## Prior Work

The Go client is doing host name resolution as shown [here](https://github.com/nats-io/nats.go/blob/2b2bb8f326dfdd2814ba6d59c59b562354b1af30/nats.go#L1641)
and then shuffle this list (unless the `NoRandomize` option is enabled) as shown [here](https://github.com/nats-io/nats.go/blob/2b2bb8f326dfdd2814ba6d59c59b562354b1af30/nats.go#L1663)

## Design

When the library is about to create a TCP connection, if given a host name (and not an IP), a name resolution must be performed.

If the list has more than 1 IP returned, it should be randomized, unless the existing `NoRandomize` option is enabled.
We could introduce a new option specific to this IP list as opposed to the server URLs provided by the user.

Then the connection should happen in the order of the shuffled list and stop as soon as one is successful.

## Decision

This was driven by the fact that the Go client behaves as described above and some users have shown interest in all clients behaving this way.
Some users have DNS where the order almost never change, which with client libraries not performing randomization, would cause all clients
to connect to the same server.

## Consequences

This should be considered as a CHANGE for client libraries, since we are changing the default behavior.

If it is strongly felt that this new default behavior should have an opt-out, other than the use of the existing `NoRandomize` option, a new option can be introduced to disable this new default behavior.
