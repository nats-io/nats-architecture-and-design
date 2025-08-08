# Trusted Protocol Aware Proxies

| Metadata | Value        |
|----------|--------------|
| Date     | 2025-08-04   |
| Author   | @ripienaar   |
| Status   | Approved     |
| Tags     | server, 2.12 |


## Context and Problem Statement

A NATS protocol aware Proxy makes connections to NATS Servers on behalf of Clients and Leafnodes. The proxy would be aware of information the NATS Server could not know in such an arrangement and must securely pass that information to the Server.

 * Information such as Source IP Address
 * Information related to TLS connection properties
 * Users might be required to connect via Proxies and not direct in their JWT and other user records

In all these cases it would be required to have a list of trusted Proxy servers to communicate these states.

## Server Configuration

We introduce the concept of trusted Proxies into the server which combine with passing of information or requiring Proxied network path to form a trust relationship.

A server will be configured as follows:

```
proxies {
  trusted = [
    {key: xxxxxx}
  ]
}
```

Here we list a number of Proxies we trust using their public nkey as the identifier.

We will support configuration reload of the `proxies` block, and, should a trusted proxy be removed we will disconnect all connections that came from that proxy.

When configured this information should be exposed in `VARZ` output.

## Proxies Required

We should be able to require that users must be connecting via proxy, to support this case a `proxies` block must be configured in addition to per-user properties, here are some examples:

```
authorization {
  users = [
    {user: deliveries, password: $PASS, proxy_required: true}
  ]
}
```

Leafnodes could require the same:

```
leafnodes {
  port: ...
  authorization {
    ...
    proxy_required: true
  }
}
```

Likewise JWTs will gain a boolean field `ProxyRequired` which will indicate the same requirement.

When constructing the `INFO` line the NATS Server will:

 * If any `proxies` are configured always include a `nonce` in the `INFO` line
 * Always report the server `JSApiLevel` Level `api_lvl` on the `INFO` line

The NATS Protocol Aware Proxy will then intercept the `INFO` and `CONNECT` protocol lines and inject a `proxy_sig` key into the `CONNECT` line that holds a signature of the same `nonce` in addition to all the client provided `CONNECT` fields.

The proxy can detect that the server it is connected to is proxy-aware by checking the `api_lvl` being at least `2`.

While authorizing the connection the server will:

 * If a `proxy_sig` is present verify it is from a known trusted proxy, reject the connection if it is present but invalid
 * If Auth Callout is configured, call the script to obtain the user JWT which might set the `ProxyRequired` field
 * Checks if the user requires a proxy and reject ones that does not have an associated trusted proxy, or no configured proxies
 * The proxy handling a connection is stored with the connection and reported in `CONNZ`, `LEAFZ` and related monitoring endpoints

The above arrangement means Auth Callout does not need to know which proxies are trusted etc, it simply has to set the boolean in the resulting JWT.

When rejecting a connection the server will log a message indicating the reason, the `CONNZ` disconnect reason will also reflect that it was due to not accessing via a proxy and the `io.nats.server.advisory.v1.client_disconnect` event will reflect the same reason. The connecting client will get the same nondescript error message as today.