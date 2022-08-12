# Logical User Permissions

|Metadata|Value|
|--------|-----|
|Date    |2022-08-11|
|Author  |@bruth|
|Status  |`Proposed`|
|Tags    |server|

## Context and Problem Statement

The crux of a permission is whether a user can publish or subscribe to a subject. This is an appropriate basis for user permissions since every allow/deny can be expressed using a subject. Prior to JetStream, this model was sufficient since there were no abstractions of subjects other than `_INBOX.>` for the request-reply implementation. The `allow_responses` permission was introduced to address the generalization of a _reply_ with the introduction of custom inbox prefixes. It would impractible to know and list the inboxes ahead of time.

The introduction of JetStream brought an API which provides the ability to manage and interact with streams and consumers. This API was intended to be _internal_ given the design of the subjects. Client libraries have implemented constructs over top the raw API calls. However, the these higher level constructs never manifested as user permissions.

The concern is that developers need to model permissions based on the internal JS subjects which is both difficult, but also introduces coupling between the developer's application-level need and the API design.

## [Context | References | Prior Work]

[Permissions][1] are defined for publish or subscribe and list a set of subjects (including wildcards). As noted above, `allow_responses` is an abstraction to handle the "reply to any inbox" situation.

With the introduction of JetStream, APIs such as `add-stream`, `delete-stream`, etc. were introduced. These reduce down to (mostly) a set of request-reply interactions. For example, to create a stream, a request must be made to `$JS.API.STREAM.CREATE.<name>` where `<name>` is the named of the stream. To get consumer info a request to `$JS.API.CONSUMER.INFO.<stream>.<name>` must be allowed.

The question is, why does a developer need to know about these verbose subjects when defining permissions?

[1]: https://docs.nats.io/running-a-nats-service/configuration/securing_nats/authorization#permissions-configuration-map)

## Design

Fundamentally, this design is a light abstraction on top of the current way permissions are defined in configuration or JWTs. These new constructs can be converted to a standard permissions map internally.

There are two potential layers to defining user permissions. First, a logical name must be defined for the underlying API subject. This mapping is shown in the table below. In addition to the name, the _context_ (publish or subscribe) can also be inferred. For example, all of the JS APIs use request-reply initiated by the client. So `js-create-stream` is implicitly a publish-based permission. For subjects that are *parameterized* based on stream names, consumer names, or subjects, the `(...)` syntax is used to support specifying these parameters.

Category | Name    | Subject | Pub/Sub | Notes
---------|---------| ------- | ------- | -----
General  | `pub(subject)` | `{subject}` | pub | Potential alternative to a bare subject string.
         | `sub(subject)` | `{subject}` | sub | Same as above.
         | `inbox` | `_INBOX.>` | sub
         | `inbox(id)` | `_INBOX_{id}.>` | sub | *Convention for custom inboxes, so be used with `--inbox-prefix` for private access.*
JetStream | `js-all` | `$JS.API.>` | pub
         | `js-info` | `$JS.API.INFO` | pub
         | `js-create-stream` | `$JS.API.STREAM.CREATE.*` | pub
         | `js-create-stream(name)` | `$JS.API.STREAM.CREATE.{name}` | pub
         | `js-update-stream` | `$JS.API.STREAM.UPDATE.*` | pub
         | `js-update-stream(name)` | `$JS.API.STREAM.UPDATE.{name}` | pub
         | `js-delete-stream` | `$JS.API.STREAM.DELETE.*` | pub
         | `js-delete-stream(name)` | `$JS.API.STREAM.DELETE.{name}` | pub
         | `js-purge-stream` | `$JS.API.STREAM.PURGE.*` | pub
         | `js-purge-stream(name)` | `$JS.API.STREAM.PURGE.{name}` | pub
         | `js-snapshot-stream` | `$JS.API.STREAM.SNAPSHOT.*` | pub
         | `js-snapshot-stream(name)` | `$JS.API.STREAM.SNAPSHOT.{name}` | pub
         | `js-snapshot-stream-ack` | `$JS.API.SNAPSHOT.ACK.*.>` | pub
         | `js-snapshot-stream-ack(name)` | `$JS.API.SNAPSHOT.ACK.{name}.>` | pub
         | `js-restore-stream` | `$JS.API.STREAM.RESTORE.*` | pub
         | `js-restore-stream(name)` | `$JS.API.STREAM.RESTORE.{name}` | pub
         | `js-snapshot-restore` | `$JS.API.SNAPSHOT.RESTORE.*.>` | pub
         | `js-snapshot-restore(name)` | `$JS.API.SNAPSHOT.RESTORE.{name}.>` | pub
         | `js-stream-names` | `$JS.API.STREAM.NAMES` | pub
         | `js-stream-list` | `$JS.API.STREAM.LIST` | pub
         | `js-stream-info` | `$JS.API.STREAM.INFO.*` | pub
         | `js-stream-info(name)` | `$JS.API.STREAM.INFO.{name}` | pub
         | `js-stream-delete-msg` | `$JS.API.STREAM.MSG.DELETE.*` | pub
         | `js-stream-delete-msg(name)` | `$JS.API.STREAM.MSG.DELETE.{name}` | pub
         | `js-stream-get-msg` | `$JS.API.DIRECT.GET.*` | pub
         | `js-stream-get-msg(name)` | `$JS.API.DIRECT.GET.{name}` | pub
         | `js-stream-get-last-subject-msg` | `$JS.API.DIRECT.GET.*.>` | pub
         | `js-stream-get-last-subject-msg(stream)` | `$JS.API.DIRECT.GET.{stream}.>` | pub
         | `js-stream-get-last-subject-msg(stream, subject)` | `$JS.API.DIRECT.GET.{stream}.{subject}` | pub
         | `js-stream-get-last-subject-msg(*, subject)` | `$JS.API.DIRECT.GET.*.{subject}` | pub
         | `js-create-ephemeral-consumer` | `$JS.API.CONSUMER.CREATE.*` | pub
         | `js-create-ephemeral-consumer(stream)` | `$JS.API.CONSUMER.CREATE.{stream}` | pub
         | `js-create-durable-consumer` | `$JS.API.CONSUMER.DURABLE.CREATE.*.*` | pub
         | `js-create-durable-consumer(stream)` | `$JS.API.CONSUMER.DURABLE.CREATE.{stream}.*` | pub
         | `js-create-durable-consumer(stream, name)` | `$JS.API.CONSUMER.DURABLE.CREATE.{stream}.{name}` | pub |
         | `js-create-durable-consumer(*, name)` | `$JS.API.CONSUMER.DURABLE.CREATE.*.{name}` | pub |
         | `js-delete-consumer` | `$JS.API.CONSUMER.DELETE.*.*` | pub |
         | `js-delete-consumer(stream)` | `$JS.API.CONSUMER.DELETE.{stream}.*` | pub |
         | `js-delete-consumer(stream, name)` | `$JS.API.CONSUMER.DELETE.{stream}.{name}` | pub |
         | `js-delete-consumer(*, name)` | `$JS.API.CONSUMER.DELETE.*.{name}` | pub |
         | `js-consumer-names` | `$JS.API.CONSUMER.NAMES.*` | pub |
         | `js-consumer-names(stream)` | `$JS.API.CONSUMER.NAMES.{stream}` | pub |
         | `js-consumer-list` | `$JS.API.CONSUMER.LIST.*` | pub |
         | `js-consumer-list(stream)` | `$JS.API.CONSUMER.LIST.{stream}` | pub |
         | `js-consumer-info` | `$JS.API.CONSUMER.INFO.*.*` | pub |
         | `js-consumer-info(stream)` | `$JS.API.CONSUMER.INFO.{stream}.*` | pub |
         | `js-consumer-info(stream, name)` | `$JS.API.CONSUMER.INFO.{stream}.{name}` | pub |
         | `js-consumer-info(*, name)` | `$JS.API.CONSUMER.INFO.*.{name}` | pub |
         | `js-consumer-next-msg` | `$JS.API.CONSUMER.MSG.NEXT.*.*` | pub |
         | `js-consumer-next-msg(stream)` | `$JS.API.CONSUMER.MSG.NEXT.{stream}.*` | pub |
         | `js-consumer-next-msg(stream, name)` | `$JS.API.CONSUMER.MSG.NEXT.{stream}.{name}` | pub |
         | `js-consumer-next-msg(*, name)` | `$JS.API.CONSUMER.MSG.NEXT.*.{name}` | pub |
         | `js-consumer-ack-reply` | `$JS.ACK.*.*.>` | pub |
         | `js-consumer-ack-reply(stream)` | `$JS.ACK.{stream}.*.>` | pub |
         | `js-consumer-ack-reply(stream, name)` | `$JS.ACK.{stream}.{name}.>` | pub |
         | `js-consumer-ack-reply(*, name)` | `$JS.ACK.*.{name}.>` | pub |
KeyValue | `kv-put` | `$KV.*.>` | pub
         | `kv-put(bucket)` | `$KV.{bucket}.>` | pub
         | `kv-put(bucket, key)` | `$KV.{bucket}.{key}` | pub
         | etc..

*TODO: add remaining subjects, leader, peer, advisories, etc.*

The second logical tier can be permission groups, often designed as *roles*, such as `js-stream-operator`. Parameters, such as stream or consumer names, will be transitively applied to the underlying permissions. The goal of roles is to abstract away a set of permissions for common use cases.

This table shows a few examples.

Name | Permissions | Notes
---- | ----------- | -----
`js-stream-operator` | `js-create-stream`, `js-update-stream`, `js-delete-stream` `js-purge-stream`, `js-stream-info`, `js-snapshot-stream`, `js-restore-stream`
`js-stream-operator(name)` | `js-create-stream(name)`, `js-update-stream(name)`, etc...
`js-stream-user(stream, subject)` | `publish(subject)`, `js-stream-info(stream)`, `js-stream-get-last-subject-msg(stream, subject)` | The `publish(subject)` is alternative to independently setting a pub-allow.


The best way to model this in configuration is TBD, however, one approach could support `allow` and `deny` as top-level keys in the `permissions` map which would be used exclusively with these new logical permissions.

For example:

```
accounts: {
  APP: {
    users: [
      {
        user: operator,
        password: operator,
        permissions: {
          allow: [
            "inbox(operator)",
            "js-stream-operator(EVENTS)",
          ]
        }
      },
      {
        user: greeter,
        password: greeter,
        permissions: {
          allow: [
            "inbox(greeter)",
            "sub(services.greeter)",
            "pub(events.greeter.>)",
          ],
          allow_responses: true,
        }
      },
      {
        user: joe,
        password: joe,
        permissions: {
          allow: [
            "inbox(joe)",
            "pub(services.*)",
          ]
        }
      },
      {
        user: sue,
        password: sue,
        permissions: {
          allow: [
            "inbox(sue)",
            "pub(services.*)",
          ]
        }
      }
    ]
  }
}
```

## Decision

[Maybe this was just an architectural decision...]

TODO

## Consequences

[Any consequences of this design, such as breaking change or Vorpal Bunnies]

TODO
