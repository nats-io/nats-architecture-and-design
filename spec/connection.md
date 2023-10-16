# NATS Connection

|Metadata|Value|
|--------|-----|
|Date    |2023-10-12 |
|Author  |@Jarema |
|Status  |`Implemented`|
|Tags    |client, server|

|Revision|Date|Author|Info|
 |--------|----|------|----|
 |1       |2023-10-12|@author|Initial draft|

## Summary

This document describes how clients connect to the NATS server or NATS cluster.
That includes topics like:
- connection process
- reconnect
- tls
- discoverability of other nodes in a cluster

## Motivation

Ensuring a consistent way how Clients establish and maintain connection with the NATS server and provide consistent and predictable behaviour across the ecosystem.

## Guide-level Explanation

### Establishing the connection

#### Minimal example

1. Clients initiate a TCP connection to the Server.
2. Server responds with [INFO][INFO] json.
3. Client sends [CONNECT][CONNECT] json.
4. Clients and Server start to exchange PING/PONG messages to detect if the connection is alive.

#### Auth flow
TODO

#### TLS
There are two flows available in the Server that enable TLS.

##### Standard TLS

This method is available in all NATS Server versions.

1. Clients initiate a TCP connection to the Server.
2. Server responds with [INFO][INFO] json.
3. If Server [INFO][INFO] contains `tls_required` set to `true`, or the client has a tls requirement set to `true`, the client performs a TLS upgrade.
4. Client sends [CONNECT][CONNECT] json.
5. Clients and Server start to exchange PING/PONG messages to detect if the connection is alive.

##### TLS First

This method has been available since NATS Server 2.11.

There are two prerequisites to use this method:
1. Server config has enabled `handshake_first` field in the `tls` block.
2. The client has set the `tls_first` option set to true.

**handshake_first**
has those possible values:
- **`false`**: handshake first is disabled. Default value
- `true`: handshake first is enabled and enforced. Clients that do not use this flow will fail to connect.
- `duration` (i.e. 2s): a hybrid mode that will wait a given time, allowing the client to follow the `tls_first` flow. After the duration has expired, `INFO` is sent, enabling standard client TLS flow.
- `auto`: same as above, with some default value. By default it waits 50ms for TLS upgrade before sending the [INFO][INFO].

The flow itself is flipped. TLS is established before the Server sends INFO:

1. Clients initiate a TCP connection to the Server.
2. Client upgrades the connection to TLS.
2. Server [INFO][INFO] json.
4. Client sends [CONNECT][CONNECT] json.
5. Clients and Server start to exchange PING/PONG messages to detect if the connection is alive.


### Servers discovery

When Server sends back [INFO][INFO]. It may contain additional URLs to which the client can make connection attempts.
The client should store those URLs and use them in the Reconnection Strategy.

A client should have an option to turn off using advertised URLs.
By default, those URLs are used.

### Reconnection Strategies (In progress)

#### Detecting disconnection

There are two methods that clients should use to detect disconnections:
1. Missing two consecutive PONGs from the Server.
2. Handling errors from TCP connection.

#### Reconnect process

When the client detects disconnection, it starts to reconnect attempts with the following rules:
1. Immediate reconnect attempt
    The client attempts to reconnect immediately after finding out it has been disconnected.
2. Exponential BackOff with Jitter
   - When the first reconnect fails, the backoff process should kick in. Default Jitter should also be included to avoid thundering herd problems.
3. If the Server returned additional URLs, the client should try reconnecting in random order to each Server on the list.
4. Successful reconnect resets the timers
5. Upon reconnection, clients should resubscribe to all created subscriptions.

If there is any change in the connection state - connected/disconnected, the client should have some way of notifying the user about it.
This can be a callback function or any other idiomatic mechanism in a given language for reporting asynchronous events.

## Reference-level Explanation
### Client options

Although clients should provide sensible defaults for handling the connection,
in many cases, it requires some tweaking.
The below list defines what can be changed, what it means, and what the defaults are.

#### Retry on failed initial connect

**default: false**

By default, if a client makes a connection attempt, if it fails, `connect` returns an error.
In many scenarios, users might want to allow the first attempt to fail as long as clients continue the efforts
and notify the progress.

When this option is enabled, the client should start the initial connection process and return the standard NATS connection/client handle while in background connection attempts are continued.

The client should not wait for the first connection to succeed or fail, as in some network scenarios, this can
take much time.
If the first attempt fails, a standard [Reconnect process] should be performed.

#### Max reconnects

**default: 3 / none

Specifies the number of consecutive reconnect attempts the client will make before giving up.
This is useful for preventing `zombie services` from endlessly reaching the servers, but it can also
be a footgun and surprise for users who do not expect that the client can give up entirely.

#### Connection timeout

**default 5s**

Specifies how long the client will wait for the TCP connection to be established.
In some languages, this can hang eternally, and timeout mechanics might be necessary.
In others, the TCP Connection method might have a way to configure its timeout.

#### Custom reconnect

**Default: none**

If fine-grained control over reconnect attempts intervals is needed, this option allows users to specify one.
Implementation should make sense in a given language. For example, it can be a callback `fn reconnect(attempt: int) -> Duration`.

#### Tls required

**default: false**
If set, the client enforces the TLS, whether the Server also requires it or not.

If `tls://` scheme is used in the connection string, this also enforces tls.

#### Ingore advertised servers

**default: false**
When connecting to the Server, it may send back a list of other servers in the cluster of which it is aware.
This can be very helpful for discoverability and removes the need for the client to pass all servers in `connect`,
but it also may be unwanted if, for example, some servers URLs are unreachable for a given client.

#### Retain servers order

**default: false**
By default, if many server addresses are passed in the connect string or array, the client will try to connect to them in random order.
This helps healthy connection distribution, but if in a specific case list should be treated as a preference list,
randomization may be turned off.

### Protocol Commands and Grammar

#### INFO
[LINK][LINK]

Send by the Server before or after establishing TLS, depending of flow used.
It contains information about the Server, the nonce, and other server URLs to which the client can connect.

#### CONNECT
[CONNECT][CONNECT]

Send by the client in response to INFO.
Contains information about client, including optional signature, client version and connection options.


#### Ping Pong
This is a mechanism to detect broken connections that may not be reported by a TCP connection in a given language.

If the Server sends `PING`, the client should answer with `PONG`.
If the Client sends `PING`, the Server should answer with `PONG`.

If two consecutive `PONGs are missed, the client should treat the connection as broken, and it should start reconnect attempts.

The default interval for PING is 2 minutes.

#### Client connection related options
These options should be available to end users, allowing control over connection handling.

#####

### Error Handling (TODO)

Server can respond with `Authorization Error`.

### Security Considerations

Discuss any additional security considerations pertaining to the TLS implementation and connection handling.

## Future Possibilities

Smart Reconnection could be a potential big improvement.

## Design

NATS is a plaintext protocol based on TCP.

## Decision

[Maybe this was just an architectural decision...]

## Consequences

[Any consequences of this design, such as breaking change or Vorpal Bunnies]

[INFO]: https://beta-docs.nats.io/ref/protocols/client#info
[CONNECT]: https://beta-docs.nats.io/ref/protocols/client#connect

