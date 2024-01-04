# NATS Connection

|Metadata|Value|
|--------|-----|
| Date    | 2023-10-12                    |
| Author  | @Jarema                       |
| Status  | Implemented                   |
| Tags    | client, server, spec          |

|Revision|Date|Author|Info|
 |--------|------------|---------|---------------|
 |1       | 2023-10-12 | @Jarema | Initial draft |

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

**TODO** Add WebSocket flow.

#### Minimal example

1. Clients initiate a network connection to the Server.
2. Server responds with [INFO][INFO] json.
3. Client sends [CONNECT][CONNECT] json.
4. Clients and Server start to exchange PING/PONG messages to detect if the connection is alive.

**Note** If clients sets `protocol` field in [Connect][Connect] to equal or greater than 1, Server can send subsequent [INFO][INFO] on a ongoing connection.
Client needs to handle them appropriately and update server lists and server info.

#### Auth flow
TODO

#### TLS
There are two flows available in the Server that enable TLS.

##### Standard NATS TLS (Explicit TLS)

This method is available in all NATS Server versions.

1. Clients initiate a network connection to the Server.
2. Server responds with [INFO][INFO] json.
3. If Server [INFO][INFO] contains `tls_required` set to `true`, or the client has a tls requirement set to `true`, the client performs a TLS upgrade.
4. Client sends [CONNECT][CONNECT] json.
5. Clients and Server start to exchange PING/PONG messages to detect if the connection is alive.

##### TLS First (Implicit TLS)

This method has been available since NATS Server 2.11.

There are two prerequisites to use this method:
1. Server config has enabled `handshake_first` field in the `tls` block.
2. The client has set the `tls_first` option set to true.

**handshake_first** has those possible values:
- **`false`**: handshake first is disabled. Default value
- `true`: handshake first is enabled and enforced. Clients that do not use this flow will fail to connect.
- `duration` (i.e. 2s): a hybrid mode that will wait a given time, allowing the client to follow the `tls_first` flow. After the duration has expired, `INFO` is sent, enabling standard client TLS flow.
- `auto`: same as above, with some default value. By default it waits 50ms for TLS upgrade before sending the [INFO][INFO].

The flow itself is flipped. TLS is established before the Server sends INFO:

1. Client initiate a network connection to the Server.
2. Client upgrades the connection to TLS.
2. Server sends [INFO][INFO] json.
4. Client sends [CONNECT][CONNECT] json.
5. Client and Server start to exchange PING/PONG messages to detect if the connection is alive.


### Servers discovery
**Note**: Server will send back the info only

When Server sends back [INFO][INFO]. It may contain additional URLs to which the client can make connection attempts.
The client should store those URLs and use them in the Reconnection Strategy.

A client should have an option to turn off using advertised URLs.
By default, those URLs are used.

**TODO**: Add more in-depth explanation how topology discovery works.

### Reconnection Strategies (In progress)

#### On-Demand reconnect

Client should have a way that allows users to force reconnection process.
This can be useful for refreshing auth or rebalancing clients.

When triggered, client will drop connection to the current server and perform standard reconnection process.
That means that all subscriptions and consumers should be resubscribed and their work resumed after successful reconnect where all reconnect options are respected.

For most clients, that means having a `reconnect` method on the Client/Connection handle.

#### Detecting disconnection

There are two methods that clients should use to detect disconnections:
1. Missing two consecutive PONGs from the Server (number of missing PONGs can be configured).
2. Handling errors from network connection.

#### Reconnect process

When the client detects disconnection, it starts to reconnect attempts with the following rules:
1. Immediate reconnect attempt
   - The client attempts to reconnect immediately after finding out it has been disconnected.
2. Exponential backoff with jitter
   - When the first reconnect fails, the backoff process should kick in. Default Jitter should also be included to avoid thundering herd problems.
3. If the Server returned additional URLs, the client should try reconnecting in random order to each Server on the list, unless randomization option is disabled in the client [options](#Retain-servers-order).
4. Successful reconnect resets the timers
5. Upon reconnection, clients should resubscribe to all created subscriptions.

If there is any change in the connection state - connected/disconnected, the client should have some way of notifying the user about it.
This can be a callback function or any other idiomatic mechanism in a given language for reporting asynchronous events.

**Disconnect buffer**
Most clients have a buffer that will aggregate messages on the client side in case of disconnection.
It will fill up the buffer and send pending messages as soon as connection is restored.
If buffer will be filled before the connection is restored - publish attempts should return error noting that fact.

## Reference-level Explanation
### Client options

Although clients should provide sensible defaults for handling the connection,
in many cases, it requires some tweaking.
The below list defines what can be changed, what it means, and what the defaults are.

#### Ping interval

**default**: 2 minutes

As the client or server might not know that the connection is severed, NATS has Ping/Pong protocol.
Client can set at what intervals it will send a PING to the server, expecting PONG.
If two consecutive PONGs are missed, connection is marked as lost triggering reconnect attempt.

It's worth noting that shorter PING intervals can improve responsiveness of the client to network issues,
but it also increases the load on the whole NATS system and the network itself with each added client.

#### Max Pings Out

**default**: 2

Sets number of allowed outstanding PONG responses for the client PINGs before marking client as disconnected and triggering reconnect.

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

Specifies how long the client will wait for the network connection to be established.
In some languages, this can hang eternally, and timeout mechanics might be necessary.
In others, the network connection method might have a way to configure its timeout.

#### Custom reconnect delay

**Default: none**

If fine-grained control over reconnect attempts intervals is needed, this option allows users to specify one.
Implementation should make sense in a given language. For example, it can be a callback `fn reconnect(attempt: int) -> Duration`.

#### Disconnect buffer

If given client supports storing messages during disconnect periods, this option allows to tweak the number of stored messages.
It should also allow disable buffering entirely.

#### Tls required

**default: false**
If set, the client enforces the TLS, whether the Server also requires it or not.

If `tls://` scheme is used in the connection string, this also enforces tls.

#### Ignore advertised servers

**default: false**
When connecting to the Server, it may send back a list of other servers in the cluster of which it is aware.
This can be very helpful for discoverability and removes the need for the client to pass all servers in `connect`,
but it also may be unwanted if, for example, some servers URLs are unreachable for a given client.

#### Retain servers order

**default: false**
By default, if many server addresses are passed in the connect string or array, the client will try to connect to them in random order.
This helps healthy connection distribution, but if in a specific case list should be treated as a preference list,
randomization may be turned off.

This function can be expressed "enable retaining order" or "disable randomization" depending on what is more idiomatic in given language.

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
This is a mechanism to detect broken connections that may not be reported by the network connection in a given language.

If the Server sends `PING`, the client should answer with `PONG`.
If the Client sends `PING`, the Server should answer with `PONG`.

If two (configurable) consecutive `PONGs are missed, the client should treat the connection as broken, and it should start reconnect attempts.

The default interval for PING is 2 minutes.


### Error Handling (TODO)

Server can respond with `Authorization Error`.

### Security Considerations

Discuss any additional security considerations pertaining to the TLS implementation and connection handling.

## Future Possibilities

Smart Reconnection could be a potential big improvement.


[INFO]: https://beta-docs.nats.io/ref/protocols/client#info
[CONNECT]: https://beta-docs.nats.io/ref/protocols/client#connect

