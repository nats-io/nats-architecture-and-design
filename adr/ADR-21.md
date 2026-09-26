# NATS Configuration Contexts

| Metadata | Value                 |
|----------|-----------------------|
| Date     | 2021-12-14            |
| Author   | @ripienaar            |
| Status   | Partially Implemented |
| Tags     | client                |

## Background

A `nats context` is a named configuration stored in a configuration file allowing a set of related configuration items to be stored and accessed later.

In the `nats` CLI this is used extensively, for example `nats stream ls --context orders` would load the `orders` context and configure items such as login credentials, servers, domains, API prefixes and more.

The intention of the ADR is to document the storage of these contexts so that clients can, optionally, support using them.

## Revision history

| Revision | Date       | Author     | Info                                       |
|----------|------------|------------|--------------------------------------------|
| 1        | 2020-08-12 | @ripienaar | Initial basic design                       |
| 2        | 2020-05-07 | @ripienaar | JetStream Domains                          |
| 3        | 2021-12-13 | @ripienaar | Custom Inbox Prefix                        |
| 4        | 2024-12-03 | @ripienaar | Windows Cert Store, User JWT and TLS First |
| 5        | 2026-04-27 | @ripienaar | Credential URI schemes, deprecate `nsc`    |

This reflects a current implementation in use widely via the CLI; as such it's a stable release. Only non-breaking additions will be considered.

## Design

The design is file based for portability.

### Configuration paths

There is generally no standard for what goes on in a users home directory on a Unix system. Recently the Free Desktop team have been working on [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html) that specifies in detail where configuration, data, binaries and more are to be stored in a way that's compatible with Linux desktops like KDE and Gnome but also with systems such as systemd.

We therefore based the design on this specification as a widely supported standard.

|File|Description|
|----|-----------|
|`~/.config`|The default location for user configuration, configurable using `XDG_CONFIG_HOME`|
|`~/.config/nats`|Where all NATS related user configuration should go|
|`~/.config/nats/context.txt`|The current selected (default) context, would contain just `ngs`|
|`~/.config/nats/context/ngs.json`|The configuration for the `ngs` context|

While this is Linux centered it does work on Windows.

### Context content

The `~/.config/nats/context/ngs.json` file has the following JSON fields:

| Key                      | Default                 | Description                                                                                              |
|--------------------------|-------------------------|----------------------------------------------------------------------------------------------------------|
| `description`            |                         | A human friendly description for the specific context                                                    |
| `url`                    | `nats://localhost:4222` | Comma separated list of server urls                                                                      |
| `token`                  |                         | Authentication token                                                                                     |
| `user`                   |                         | The username to connect with, requires a password                                                        |
| `password`               |                         | Password to connect with                                                                                 |
| `creds`                  |                         | Path to a NATS Credentials file, or a credential URI (see [Credential URI schemes](#credential-uri-schemes)) |
| `nkey`                   |                         | Path to a NATS Nkey file, or a credential URI (see [Credential URI schemes](#credential-uri-schemes))    |
| `cert`                   |                         | Path to the x509 public certificate                                                                      |
| `key`                    |                         | Path to the x509 private key                                                                             |
| `ca`                     |                         | Path to the x509 Certificate Authority                                                                   |
| `nsc`                    |                         | **Deprecated.** Use `creds` with an `nsc://` URI instead. See [Migration of the deprecated `nsc` field](#migration-of-the-deprecated-nsc-field) |
| `jetstream_domain`       |                         | The JetStream Domain to use                                                                              |
| `jetstream_api_prefix`   |                         | The JetStream API Prefix to use                                                                          |
| `jetstream_event_prefix` |                         | The JetStream Event Prefix                                                                               |
| `inbox_prefix`           |                         | A prefix to use when generating inboxes                                                                  |
| `user_jwt`               |                         | The user JWT token                                                                                       |
| `tls_first`              |                         | Enables the use of TLS on Connect rather than historical INFO first approach                             |
| `windows_cert_store`     |                         | The Windows cert store to use for access to the TLS files, `windowscurrentuser` or `windowslocalmachine` |
| `windows_cert_match_by`  |                         | Which certificate to use inside the store                                                                |
| `windows_cert_match`     |                         | How certificates are searched for in the store, `subject` or `issuer`                                    |
| `windows_ca_certs_match` |                         | Which Certificate Authority to use inside the store                                                      |

All fields are optional, none are marked as `omitempty`, users wishing to edit these with an editor should know all valid key names.

### Credential URI schemes

The `creds` and `nkey` fields accept either a bare filesystem path or a URI that names where the credential material is fetched from at connect time. A bare path is equivalent to a `file://` URI. The following schemes are supported:

| Scheme   | Form                                  | Description                                                                                              |
|----------|---------------------------------------|----------------------------------------------------------------------------------------------------------|
| `file://`| `file:///path/to/file`                | Reads bytes from a filesystem path. The default when no scheme is given.                                 |
| `nsc://` | `nsc://<operator>/<account>/<user>`   | Shells out to the `nsc` CLI to materialize a creds file (see below).                                     |
| `op://`  | `op://<vault>/<item>/<field>`         | Shells out to the 1Password `op` CLI to read a secret reference.                                         |
| `env://` | `env://NAME`                          | Reads bytes from the named environment variable. Intended for containers or CI.                          |
| `data:`  | `data:;base64,<payload>`              | RFC 2397 inline payload, base64-encoded. Intended for embedding short credential material in a context.  |

`cert`, `key`, and `ca` are path-only and do not accept URI schemes.

#### `nsc://` references

The `nsc://` scheme takes a URL-shaped value, where `<operator>`, `<account>`, and `<user>` are placeholders for the corresponding nsc names and the query-string flags are literal:

 * `nsc://<operator>`
 * `nsc://<operator>/<account>`
 * `nsc://<operator>/<account>/<user>`
 * `nsc://<operator>/<account>/<user>?operatorSeed&accountSeed&userSeed`
 * `nsc://<operator>/<account>/<user>?operatorKey&accountKey&userKey`
 * `nsc://<operator>?key&seed`
 * `nsc://<operator>/<account>?key&seed`
 * `nsc://<operator>/<account>/<user>?key&seed`
 * `nsc://<operator>/<account>/<user>?store=/a/.nsc/nats&keystore=/foo/.nkeys`

When the credential is resolved the implementation invokes `nsc generate profile <ref>`, where `<ref>` is the URI with any leading `nsc://` stripped. The response is a non-zero exit code for error else a structure like:

```json
{
  "user_creds": "<filepath>",
  "operator" : {
     "service": "hostport"
   }
}
```

The `user_creds` file is read and used as the credentials. The `service` value is used to populate `url` when the context did not set it explicitly. If `url` or `creds` are specifically configured in the context, those override the answer from `nsc`.

See `nsc generate profile --help` for details.

### Migration of the deprecated `nsc` field

The top-level `nsc` field is retained for backward compatibility but is deprecated. New contexts should set `creds` to an `nsc://...` URI instead.

Implementations are expected to migrate the legacy field on write so existing contexts upgrade in place.

On load, a context that still carries the legacy `nsc` field continues to work. The implementation invokes `nsc generate profile` to populate the effective `creds` and `url` for the connection. An explicitly-set `url` or `creds` in the context overrides the values discovered from `nsc`.

On save, the registry rewrites the legacy `nsc` field into `creds` as an `nsc://<ref>` URI and clears the legacy field. If `url` is empty and a service URL was discovered from `nsc` at load time, that URL is captured into `url`. If both `url` and `creds` are empty — a truly legacy context with no override — `nsc generate profile` is invoked at save time to populate `url` from the operator's service URL. When `creds` was overridden the user has opted out of `nsc`-driven resolution, so the migration does not invoke `nsc` and does not fail in environments where `nsc` is unavailable.

After migration the saved context contains `creds: "nsc://<operator>/<account>/<user>"` and a populated `url`, and the legacy `nsc` field is absent.

## Sample usage APIs

This ADR does not dictate a standard API surface. In Go the package exposes two common patterns; delegating context management to the `nats` CLI is also fine.

A basic way to connect to a specific context:

```go
nc, _ := Connect(os.Getenv("CONTEXT"), nats.MaxReconnects(-1))
```

Here `Connect` constructs `[]nats.Option` from the context settings and appends the user-supplied options to the list.

A way to load a context and optionally override some options:

```go
nctx, _ := Load(os.Getenv("CONTEXT"), WithServerURL("nats://other:4222"))
nc, _ := nctx.Connect(nats.MaxReconnects(-1))
```

When `name` is empty the currently selected context is used; if no context is selected and `name` is empty it is an error.
