# NATS Client LifeCycle (-ERR Protocol Error Handling)

| Metadata | Value          |
| -------- | -------------- |
| Date     | 2024-05-31     |
| Author   | @aricart       |
| Status   | Implemented    |
| Tags     | client, server |

## Context and Problem Statement

Client lifecycle such as connect/reconnect/liveliness/LDM behaviours are fairly
complex in a NATS client. This ADR simply documents `-ERR` protocol messages
that are sent to a client.

The `-ERR` protocol message is an important signal for clients about things that
are incorrect from the perspective of Permissions or Authorization.

## Errors

### Permission Violation

`Permission Violation` means that the client tried to publish or subscribe on a
subject that it has no permissions. This type of error can happen or surface at
any time, as changes to permissions intentionally or not can happen.

The message will include `/(Publish|Subscription) to (\S+)/` this will indicate
whether the error is related to a publish or subscirption. A second level parse
for `/using queue "(\S+)"/` will yield the queue if any.

The server unfortunately doesn't make it easy for the client to know the actual
subscription (SID) hosting the error but the logic is simple: notify the first
one that matches the subject and queue (this assumes you track the subject and
queue name in your internal subscription representation) name - the server will
send multiple protocol errors (one per offense) so if multiple subscriptions,
you will be able to notify all of them.

For subscriptions, errors are _terminal_, as the server has cancelled the
subscription for the client. It is very convenient for client code to receive an
error using some mechanism associated with their subscription as this will
simplify the handling by not needing to hardcode subjects/etc in an async error
handler.

It is also useful to have some sort of Promise/Future etc that will get notified
when a subscription closes (will not yield any more messages) - The
Promise/Future can resolve to an error or void (not thrown) which the client can
inspect for the reason if any why the subscription closed. Client can then use
this information to perform their own error handling which may require taking
the service offline.

For publish permission errors, it's hard to notify the client at the point of
failure unless the client is synchronous. But the standard async error
notification should be sufficient. In the case of request reply, since there's a
subscription handling the response, this means that you can search subscriptions
related to request and reply subjects, and notify them via the response
mechanism for the request.

Note that regardless of a localized error handling, you should also notify the
async error handler (you don't know exactly how they are looking for errors).

## Authorization Violation

`Authorization Violation` is sent whenever the credentials for a client are not
accepted. This is followed by a server initiated disconnect.

## User Authentication Expired

`User Authentication Expired` protocol error happens whenever credentials for
the client expire while the client is connected to the server. It is followed by
a server disconnect. This error should be notified in the async handler. On
reconnect the client is going to be rejected with `Authorization Violation`.

## Account Expiration

`Account Authentication Expired` is sent whenever the account JWT expires and a
client for the account is connected. This will result in a disconnect initiated
by the server. On reconnect the client will be rejected with
`Authorization Violation` until the account configuration is refreshed on the
server.

## Secure Connection - TLS Required

`Secure Connection - TLS Required` is sent if the client is trying to connect on
a server that requires TLS. The client should have done extensive ServerInfo
investigation and determined that this would have been a failure?
