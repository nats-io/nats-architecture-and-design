# Server versioning semantics

|Metadata|Value|
|--------|-----|
|Date    |2022-12-07|
|Author  |@bruth, @wallyqs|
|Status  |`Proposed`|
|Tags    |server|

## Context and Problem Statement

Recent efforts for documenting the server's release process led to identifying two kinds of release types, currently referred to as *defect* and *feature* releases.

A defect release can include bug fixes, security patches, performance improvements, source documentation updates, etc. The requirement is that changes bundled in this release type do not change the API, and by default, is fully backwards compatible.

A feature release is reserved for additive changes in the form of new APIs, but still, by default, backwards compatible.

Following SemVer, these two types would result in patch increments for defect releases (2.9.8 &rarr; 2.9.9) and minor increments for feature releases (2.9.8 &rarr; 2.10.0).

What about *major version increments* which designate backwards incompatible changes? To date, this has only been applied once when changes to the NATS protocol itself were made. This was done in the [v2.0.0](https://github.com/nats-io/nats-server/releases/tag/v2.0.0) release, which was a major bump from [v1.4.1](https://github.com/nats-io/nats-server/releases/tag/v1.4.1).

With the introduction of JetStream, APIs in the form of reserved subjects with specific token hierarchy and payload schema have been introduced. This brings a new layer of compatibility to consider one step above the protocol, but below the client libraries that are able to abstract away most of these details.

The question is for backwards incompatible _API_ changes, how do you increment the version?

## Design

There are four possible options:

- Perform major version increments for any and all breaking changes introduced and stick with the standard `major.minor.patch` structure. Applying our semantics, it would looke like `breaking.feature.defect`
- Reserve the major version to be protocol changes, and rely on a *pre-release* token (supported by SemVer) for patch releases, e.g. `major.minor.patch-pre`. Applying our semantics, it would look like `protocol.breaking.feature-defect`.
- Use increments of 10 on the `patch` number to denote a *feature* release and single digits (1-9) as defect increments. For example, starting with `2.9.8`, if a feature release occurred, the version would be bumped to `2.9.10` (the first increment of 10). If two defect releases follow, `2.9.11` and `2.9.12` would occur. If a feature release follows that, the version would be bumped to `2.9.20`, etc.
- Don't use SemVer proper and come up with a superset using four numbers, `protocol.major.minor.patch`.

## Decision

TBD

## Consequences

Since the server is written in Go and it can be embedded, moving forward with major version increments, e.g. 2.9.8 &rarr; 3.0.0, has the unfortunate side effect with Go modules that a new branch would need to be created per major version. `main` currently represented [v2](https://github.com/nats-io/nats-server/blob/main/go.mod#L1) while a `v3` branch would need to be created to prevent breaking imports (it may be possible to rename `main` to `v2`, but I have not tested that.)

The second approach looks promising, however, a nunance is that a version of `2.9.8` has _greater_ precedence than `2.9.8-1`. The reason is because the `-1` is interpreted as a pre-release to `2.9.8`. If we wanted to use this approach and maintain correct precedence, then once `2.9.8` is release (which would be a feature release), the version would need to be bumped to `2.9.9-1` for the first defect release that follows. These could be framed as defect releases leading up to the final `2.9.9` *feature* release. However, again, this is still bastardizing the standard SemVer semantics, since a breaking change would only increment to `2.10.0` where most would expect the first number to increment.

The third approach is still a hack like option two, but does not bastardize the pre-release token.

The fourth option would allow us to define our own semantics, but any tooling or services that expect SemVer versions would likely fail to behave correctly, specifically with determining precedence correctly.
