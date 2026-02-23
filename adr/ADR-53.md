# Feature flags

| Metadata | Value           |
|----------|-----------------|
| Date     | 2026-02-20      |
| Author   | @MauriceVanVeen |
| Status   | Proposed        |
| Tags     | server          |

| Revision | Date       | Author          | Info           |
|----------|------------|-----------------|----------------|
| 1        | 2026-02-20 | @MauriceVanVeen | Initial design |

## Context and Problem Statement

The server occasionally needs to evolve behaviors in ways that would be breaking changes if introduced in non-major
versions.

When a behavior change is desirable but breaking, there is no supported way for operators to opt in early, nor a way to
roll it out gradually across a cluster/system. For example, ADR-15 defines two JetStream ACK subject formats: the
original `$JS.ACK.<stream>.<consumer>.<...>` format and a newer format including the domain and account hash.
Immediately switching to the newer format in a minor or patch release would be problematic for the following reasons:

- The older server version will not know how to parse the new format if not backported to it. (The user would also need
  to be informed about only upgrading between certain versions).
- Some clients or tooling might not yet support the new format. (We don't want to block or postpone a server release
  because of this)
- The whole system will not be upgraded to the newer server version immediately. This requires us to support both
  formats at the same time anyway.
- Subject permissions could be using the old format, which would otherwise need to be updated and coordinated to align
  with the upgrade.

The addition of 'feature flags' allows the addition of new features or behavior changes without them immediately
impacting all users performing an upgrade. This allows for smooth migrations between server versions, while allowing
operators to opt in to new behaviors at their own pace and allowing the server to gradually deprecate old behaviors.

## Design

A new `feature_flags` configuration block is added to the server configuration file.

```
feature_flags {
  js_ack_fc_v2: true
  ...
}
```

The `feature_flags` field is of type `map[string]bool`. Allowing flexible addition and removal of flags.

On server startup, the server will log which feature flags are used and whether they are enabled or disabled. The server
should make clear whether the flag is set and is equal to the `default` value in use by that server version, or whether
it's acting as an `opt-in` or `opt-out`.

The server will not support reloading these flags at runtime at this time. Technically based on the current envisioned
`js_ack_fc_v2` flag we could allow reloading. However, future behavior changes could require certain migration steps to
be performed during recovery, or it might be expensive to allow reloading such flags if the flag influences behavior in
the hot-path (for example, with `js_ack_fc_v2`). For now, we'll only support server restarts to change flags.

Unknown flags are ignored by the server, allowing configuration files to be shared across server versions without error.
However, unknown flags should be logged to inform the user that they can be removed as they've become ineffective.

| Flag           | Description                                                               | Type   | Introduced | Default Flipped | Removed |
|:---------------|:--------------------------------------------------------------------------|:-------|:-----------|:----------------|:--------|
| `js_ack_fc_v2` | Use domain/account-aware JetStream ACK and FC subjects (ADR-15 v2 format) | Opt-in | 2.14       | TBD             | TBD     |

Example:

- `js_ack_fc_v2` is of type "Opt-in" which means server version 2.14 uses the V1 ack/fc format by default, but supports
  parsing the V2 format.
- The default can be flipped in a future version, which means that that server version will then use the V2 format by
  default. The user can still opt out using `js_ack_fc_v2: false` if needed.
- The flag can be removed in a future version after the new behavior has become the default. At this point the server
  might choose to keep supporting the old format anyway or remove it entirely. This will depend on the behavior and
  impact of the removal and will be decided in the future.

Flags should ideally be meant as opt-in behavior changes which become the default in a future release. Flags could also
be used as opt-out behavior changes; however, the flag should be clearly named to signal that it's an opt-out behavior
change as opposed to an opt-in that will become the default in a future release. Opt-out flags are recommended to be
prefixed, with `revert_` for example, clearly signaling that the server reverts to old/non-default behavior.

- Opt-in flags are to be used for gradual rollout and deprecation of behavior spanning multiple server versions.
    - These are meant for behavior changes that are either experimental and might end up not becoming the new default,
      or behavior changes that require coordination with other components or teams where we don't want to tie this
      coordination directly to the time of server upgrade.
- Opt-out flags are to be used for immediate behavior changes that can be opted out ahead of (or during) deploying a new
  version.
    - These are generally meant to not pollute the configuration file with options if the expectation is that most users
      will benefit from the new behavior. Not requiring them to change their configuration, while still
      allowing them to opt out if needed and having them remain on the upgraded version.
