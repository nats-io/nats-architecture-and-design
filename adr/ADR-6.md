# Naming Rules

|Metadata|Value|
|--------|-----|
|Date    |2021-08-17|
|Author  |@scottf|
|Status  |Approved|
|Tags    |server, client|

| Revision | Date       | Author    | Info                                           |
|----------|------------|-----------|------------------------------------------------|
| 1        | 2021-08-17 | @scottf   | Initial design                                 |
| 2        | 2026-10-22 | @piotrpio | Align rules with what the server accepts       |

## Context

This document describes naming conventions for these protocol components:

* Subjects (including Reply Subjects)
* Queue Names
* Stream Names
* Consumer Names
* Account Names
* JetStream API Prefixes and Domains
* Key-Value and Object Store Names

## Prior Work

Currently, the NATS Docs regarding [protocol convention](https://docs.nats.io/nats-protocol/nats-protocol#protocol-conventions) says this:

> Subject names, including reply subject (INBOX) names, are case-sensitive and must be non-empty alphanumeric strings with no embedded whitespace. All ascii alphanumeric characters except spaces/tabs and separators which are "." and ">" are allowed. Subject names can be optionally token-delimited using the dot character (.), e.g.:
A subject is comprised of 1 or more tokens. Tokens are separated by "." and can be any non space ascii alphanumeric character. The full wildcard token ">" is only valid as the last token and matches all tokens past that point. A token wildcard, "*" matches any token in the position it was listed. Wildcard tokens should only be used in a wildcard capacity and not part of a literal token.

> Character Encoding: Subject names should be ascii characters for maximum interoperability. Due to language constraints and performance, some clients may support UTF-8 subject names, as may the server. No guarantees of non-ASCII support are provided.

## Specification

The rules below describe what is valid. Where the server accepts more than is advisable, the Notes give a
recommended, stricter form.

`literal-token`, `term`, `limited-term` and `restricted-term` are listed from loosest to strictest: each accepts
only a subset of what the one before it accepts.

```
dot                = "."
asterisk           = "*"
gt                 = ">"
fwd-slash          = "/"
backslash          = "\"
dash               = "-"
underscore         = "_"
equals             = "="
whitespace         = space, tab, carriage return, line feed or form feed
printable          = all printable ascii (33 to 126 inclusive)
literal-token      = (any character except whitespace or dot)+, other than exactly asterisk or exactly gt
term               = (printable except dot, asterisk or gt)+
limited-term       = (A-Z, a-z, 0-9, dash, underscore, fwd-slash, equals)+
restricted-term    = (A-Z, a-z, 0-9, dash, underscore)+
name               = (any character except whitespace, dot, asterisk or gt)+
filename-safe      = (name except fwd-slash, backslash or NUL) maximum 255 bytes

message-subject    = literal-token (dot literal-token)*
subject-filter     = (literal-token | asterisk) (dot (literal-token | asterisk))* (dot gt)?
                   | gt
reply-to           = message-subject
queue-name         = (any character except whitespace)+
stream-name        = filename-safe
durable-name       = filename-safe
consumer-name      = filename-safe
account-name       = filename-safe
js-api-prefix      = message-subject
js-domain          = name
kv-key-name        = limited-term (dot limited-term)*
kv-bucket-name     = restricted-term maximum 252 characters
os-bucket-name     = restricted-term maximum 251 characters
os-object-name     = (any character)+
```

## Notes

### Subjects

A `message-subject` is the subject a message is published to and is always literal. A `subject-filter` is used
where wildcards are allowed: subscriptions, stream subjects, consumer filter subjects and permissions.

`*` and `>` are wildcards only when they make up a whole token. Inside a longer token, such as `foo*` or `a>b`,
they are ordinary characters: a subscription on `foo*` receives messages published to `foo*` and nothing else.

The server rejects a subscription with an empty token or with `>` anywhere but the last token. It checks the
subject of a published message only for connections in pedantic mode, and then only reports an error; the
message is still delivered.

Apart from whitespace and dot, the server does not restrict the characters in a token; UTF-8 and control
characters are accepted. For maximum interoperability, use `term` for every literal token: printable ASCII,
with no `*` or `>` inside a longer token. Some clients reject tokens that embed `*` or `>`.

### reply-to

A reply subject is where the responder publishes its response, so it is a `message-subject`. The server does
not check the syntax of a reply subject, but it rejects a message from a client whose reply subject starts with a
prefix it reserves for its own replies: `_R_.`, `_GR_.` or `$JS.ACK.`.

### queue-name

The server accepts any queue name without whitespace, except `_sys_`, which it reserves. It compares queue names
as plain strings, so `.`, `*` and `>` have no special meaning in a queue name. The exception is queue permissions
(`"<subject> <queue>"`): wildcards in the permission's queue are matched against the queue name token by token,
so the permission `orders workers.*` allows the queue `workers.east`. Queue names written as a `message-subject`
work with such permissions.

### filename-safe

The server rejects stream and consumer names that contain whitespace, `.`, `*`, `>`, `/` or `\`, and stream and
durable names longer than 255 bytes. The limit is in bytes, so names that use multi-byte UTF-8 characters hold
fewer than 255 characters. Any other character, including non-ASCII and control characters, is accepted.

The server has no operating-system-specific name checks. When the filesystem cannot create a directory for a
name (NUL on any platform, or characters that the platform forbids), creating a file-based stream or consumer
fails with a store error rather than a name error. For names that must work on every platform, also avoid
control characters, `<`, `:`, `"`, `|`, `?` and names that Windows reserves, such as `CON` or `NUL`.

On case-insensitive filesystems (the default on macOS and Windows), names that differ only in case share a
directory. The server does not detect this, so do not rely on case alone to tell stream or consumer names apart.

### account-name

In operator (JWT) mode, an account name is the account's public NKey. In configuration mode, the server does not
validate account names (other than reserving `$G`), but it uses the account name as the account's JetStream
storage directory and as a token in system subjects such as `$SYS.ACCOUNT.<account>.CONNECT`, so account names
must be `filename-safe`.

### js-api-prefix and js-domain

The default JetStream API prefix is `$JS.API`. A JetStream domain `<domain>` uses the prefix `$JS.<domain>.API`.
A custom prefix, for example `JS.acc1.API` for an account that imports another account's `$JS.API.>`, can be any
`message-subject` and may start with `$`. Prefixes are written without a trailing dot; the server rejects one in
a stream source's external API prefix.

The server rejects a JetStream `domain` in its configuration that contains whitespace, `.`, `*` or `>`.

### kv-key-name

Keys starting with `_kv` are limited to internal use. The server does not check keys; clients do.

### kv-bucket-name and os-bucket-name

A Key-Value bucket is stored in the stream `KV_<bucket>` and an Object Store bucket in the stream
`OBJ_<bucket>`. The 255-byte stream name limit therefore caps bucket names at 252 and 251 characters.

### Client Validation

The server is the authority on these rules, and clients validate to different degrees. Key-Value
([ADR-8](ADR-8.md)), Object Store ([ADR-20](ADR-20.md)) and Service API ([ADR-32](ADR-32.md)) are client
constructs, so for their names the clients are the authority on what is valid.

Clients should, at a minimum, reject a subject, reply subject or queue name that is empty or contains whitespace
before sending it in `PUB`, `HPUB` or `SUB`. Whitespace separates protocol arguments and ends the protocol line,
so the server would misread such a value or close the connection.
