# NATS Client LifeCycle (-ERR Protocol Error Handling)

| Metadata | Value          |
| -------- | -------------- |
| Date     | 2024-05-31     |
| Author   | @aricart       |
| Status   | Implemented    |
| Tags     | client, server |

## Context and Problem Statement

Client lifecycle such as connect/reconnect/liveliness (ping/pong)/LDM behaviours
are fairly complex in a NATS client. This ADR simply documents `-ERR` protocol
messages that are sent to a client.

The `-ERR` protocol message is an important signal for clients about things that
are incorrect from the perspective of Permissions or Authorization.

## Errors

### Permission Violation

`Permission Violation` means that the client tried to publish or subscribe on a
subject for which it has no permissions. This type of error can happen or
surface at any time, as changes to permissions intentionally or not can happen.

The message will include `/(Publish|Subscription) to (\S+)/` this will indicate
whether the error is related to a publish or subscription operation.

For publish permission errors, it's hard to notify the client at the point of
failure unless the client is synchronous. But the standard async error
notification should be sufficient. In the case of request reply, since there's a
subscription handling the response, this means that you can search subscriptions
related to request and reply subjects, and notify them via the response
mechanism for the request depending on the type of operation that was rejected.

For subscription errors, a second level parse for `/using queue "(\S+)"/` will
yield the queue if any that was used during the subscribe. This means that a
client may have permissions on a subscription, but not in a specific queue or
some other permutation.

The server unfortunately doesn't make it easy for the client to know the actual
subscription (SID) hosting the error but the logic for processing is simple:
notify the first subscription that matches the subject and queue name (this
assumes you track the subject and queue name in your internal subscription
representation) - the server will send multiple error protocol messages (one per
offense) so if multiple subscriptions, you will be able to notify all of them.

For subscriptions, errors are _terminal_, as the server cancels the clients
interest. It is very convenient for client user code to receive an error using
some mechanism associated with the subscription in question as this will
simplify the handling of the client code.

It is also useful to have some sort of Promise/Future/etc that will get resolved
when a subscription closes (will not yield any more messages) - The
Promise/Future can resolve to an error or void (not thrown) which the client can
inspect for the reason if any why the subscription closed. Throwing an error is
discouraged, as this would create a possibility of crashing the client. Clients
can then use this information to perform their own error handling which may
require taking the service offline if the subscription is vital for its
operation.

Note that regardless of a localized error handling mechanism, you should also
notify the async error handler as you don't know exactly where they are looking
for errors.

## Authorization Violation

`Authorization Violation` is sent whenever the credentials for a client are not
accepted. This is followed by a server initiated disconnect. Clients will
normally reconnect (depending on their connection options). If the client
closes, this should be reported as the last error.

## User Authentication Expired

`User Authentication Expired` protocol error happens whenever credentials for
the client expire while the client is connected to the server. It is followed by
a server disconnect. This error should be notified in the async handler. On
reconnect the client is going to be rejected with `Authorization Violation` and
follow its reconnect logic.

## Account Expiration

`Account Authentication Expired` is sent whenever the account JWT expires and a
client for the account is connected. This will result in a disconnect initiated
by the server. On reconnect the client will be rejected with
`Authorization Violation` until the account configuration is refreshed on the
server. The client will follow its reconnect logic.

## Secure Connection - TLS Required

`Secure Connection - TLS Required` is sent if the client is trying to connect on
a server that requires TLS. The client should have done extensive ServerInfo
investigation and determined that this would have been a failure?
