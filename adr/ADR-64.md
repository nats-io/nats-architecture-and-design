# NATS Client Connection Environment Variables

| Metadata | Value |
|----------|-------|
| Date     | 2026-11-14 |
| Author   | @yordisprieto |
| Status   | Proposed |
| Tags     | client, configuration, spec, interoperability |

| Revision | Date       | Author       | Info |
|----------|------------|--------------|------|
| 1        | 2026-11-14 | @yordisprieto | Initial design |

## Context and Problem Statement

Applications that use NATS repeatedly translate environment variables into client
options. Each application has consequently chosen a different set of names,
defaults, precedence rules, credential handling rules, and error behavior. The
same deployment can therefore need one set of variables for a Rust service,
another for a Go service, and another for a TypeScript or framework adapter even
when every process connects to the same NATS server.

NATS already has a stable client configuration model and the `nats` CLI has an
established vocabulary for connection variables. ADR-21 defines file-backed
configuration contexts, while ADR-40 defines the connection behavior itself. A
portable environment contract is still missing. The question is: how can a
client opt into one predictable environment mapping without making environment
configuration implicit or preventing application-specific options?

## References and Prior Work

- [ADR-21: NATS Configuration Contexts](ADR-21.md) defines the connection fields
  that clients may support and their relationship to explicit options.
- [ADR-40: NATS Connection](ADR-40.md) defines connection, TLS, discovery, and
  reconnect behavior. This ADR does not change that behavior.
- The NATS CLI environment vocabulary is the compatibility target for the
  connection variables in this document.

## Design

### Opt-in API

Every client or adapter claiming support for this convention **MUST** expose an
explicit, language-idiomatic operation equivalent to:

```text
configureNatsFromEnv(options) -> options | error
```

The operation reads the process environment at the time it is called and returns
client options. The application then passes those options to the normal client
connection function. The environment resolver and the connection function may be
combined in one language-idiomatic call only when the environment step remains
explicit at the call site.

The client **MUST NOT** read these variables automatically from its ordinary
connect function or constructor. The operation does not start a server, mutate
the process environment, read a context file, or connect as a side effect. An
application must call it deliberately. Explicit options supplied by the
application take precedence over values returned by the resolver.

The operation may be named differently in each language, such as
`configureNatsFromEnv`, `NatsOptions::from_env`, or `nats_options_from_env`. Its
observable input, opt-in requirement, precedence, validation, and security
behavior must remain equivalent. A client claiming support **MUST** document the
operation and its supported variables.

### Standard variables

Empty values are treated as unset. A variable whose value is a list of server
URLs is comma-separated; surrounding whitespace is ignored.

| Variable | Meaning |
|----------|---------|
| `NATS_URL` | Comma-separated server URLs. |
| `NATS_USER` | Username, or a token when `NATS_PASSWORD` is unset. |
| `NATS_PASSWORD` | Password paired with `NATS_USER`. |
| `NATS_TOKEN` | Token sent with another identity, or by itself. |
| `NATS_CREDS` | Credentials file location. |
| `NATS_NKEY` | NKey seed file location. The value is not an inline seed. |
| `NATS_JWT` | User JWT, inline or using the implementation's documented file form. |
| `NATS_SEED` | Seed paired with `NATS_JWT`. |
| `NATS_CERT` | Client certificate location. Must be paired with `NATS_KEY`. |
| `NATS_KEY` | Client private key location. Must be paired with `NATS_CERT`. |
| `NATS_CA` | CA certificate chain location. |

`NATS_CONTEXT` and `NATS_SOCKS_PROXY` are not part of this convention. A
resolver that cannot implement either one must return an explicit unsupported
configuration error when it is set rather than ignore it and connect somewhere
else. Implementations may add support in a later revision without changing the
meaning of the variables above.

JetStream domains and prefixes, stream names, consumer names, bucket names,
subject roots, service names, and application authentication or tenancy
variables remain application-owned. They must not be added to this connection
convention merely because an application uses NATS.

### Identity selection and validation

When more than one identity form is present, the resolver selects the first
present form in this order:

1. `NATS_USER` with `NATS_PASSWORD`, or `NATS_USER` alone as a token.
2. `NATS_CREDS`.
3. `NATS_NKEY`.
4. `NATS_JWT` with `NATS_SEED`.
5. `NATS_JWT` by itself as a bearer JWT.

`NATS_TOKEN` may accompany any selected identity. Malformed URLs, an incomplete
certificate pair, an incomplete JWT and seed pair, or an unreadable credential
file are errors. An implementation must not silently fall back to a different
identity after a selected identity is malformed.

### Precedence and defaults

The precedence order is:

1. Explicit application options.
2. Values resolved from the standard environment variables.
3. Client library defaults.

The convention supplies no implicit server URL. If no URL is provided, the
underlying client may use its documented default. A resolver must not invent
application defaults such as a stream, bucket, subject, tenant, or replica count.

### Secret handling

Resolvers must not log variable values, URLs containing user information,
passwords, tokens, JWTs, seeds, or credential file contents. Error messages may
name the variable and describe the validation failure, but must redact values.
Returned options must retain the client library's normal secret lifetime and
zeroization behavior where available.

### Compatibility and conformance

A client claiming support for this ADR documents the variables it implements and
passes the same conformance cases for:

- URL list parsing and empty values.
- Identity precedence and token combination.
- JWT and seed pairing.
- Certificate and key pairing.
- Explicit-option precedence.
- Unsupported-variable errors.
- Redaction of secrets in errors.

Partial support is allowed only when the unsupported variables are documented and
the resolver fails explicitly when they are set. A client must not claim full
conformance while silently ignoring a standard variable.

## Decision

NATS clients and framework integrations **MUST** provide an explicit environment
configuration resolver before claiming support for this connection contract.
Applications adopt it with a single opt-in call and then use the same environment
names across languages and frameworks. The resolver is a configuration adapter,
not a connection manager, server bootstrapper, or application topology registry.

## Consequences

Applications gain one portable deployment contract and can move between client
languages without rewriting connection configuration. Client libraries retain
their native APIs and connection behavior, and applications retain control over
whether environment configuration is used.

The resolver adds a small compatibility surface that must be kept aligned across
clients. Credential precedence and failure behavior become part of the public
contract, so implementations need shared conformance fixtures and careful secret
redaction. Existing application-specific variables remain valid, but they are
outside this standard and do not become portable automatically.
