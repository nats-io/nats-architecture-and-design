# Client File Locations

|Metadata|Value|
|--------|-----|
|Date    |2022-01-24|
|Author  |@philpennock|
|Status  |Approved|
|Tags    |client, config|


## Context and Problem Statement

The configuration files for NATS clients should be in a coherent predictable location.
Clients in various languages should be able to know where to read and write data,
and administrators should know what file-system access is needed and where
sensitive key data might be stored.

This document both unifies the location and acts as a living registry of the
namespace within the NATS configuration directory.


## Version History

|Date      |Revision|
|----------|--------|
|2022-01-24|Initial proposal|


## [Context | References | Prior Work]

* [ADR-21][] on Configuration Contexts moves us closer to the model
  described herein, but ADR-21 will need updating for the non-XDG platforms.
* The `nsc` and `ngs` commands use other locations, scattering files around.
* A Unix-centric model is the [XDG Base Directory Specification][xdgbase].


## Design

We mandate a standard schema for where all NATS clients should lay out their
configuration files, and guidance for migration and handling.

Our goals are:

 1. Keep files of like type together, while adhering to OS norms.
 2. Never break existing user configuration.
 3. Provide a predictable working path across Unix-like systems, both macOS
    and XDG-compliant Unix system.
 4. Register which files and sub-directories exist, to act as a central
    repository of such information, and record who can safely rely upon such
    details.

Rather than reinvent the wheel, we propose to use the layout schema of
[Adrian-George Boston's Go implementation of XDG][adrg/xdg], which uses
specific other locations for other operating systems, all documented in
tables in the front README.

The documentation is specific enough to allow implementations in languages
other than Go to use the same layouts.  For Go, the `adrg/xdg` implementation
has two dependencies, both of which we already depend upon in multiple
`nats-io` repositories: `github.com/stretchr/testify` and `golang.org/x/sys`.
The license is MIT and the code is stable but still seeing maintenance
updates.  It supports, “most flavors of Unix, Windows, macOS, and Plan 9”.

All configuration should live inside a directory called `nats`.  Users should
not need to learn which other tools might drop files elsewhere and should be
able to, minimally, discover all the configuration by files or symbolic links
within such directories.  For configuration files, on Unix, this means that
all per-tool configuration will be within the `~/.config/nats/` directory,
while managed data might be within the `~/.local/share/nats/` directory.
Environment variables and choice of OS will change that.

Our one deviation from the [adrg/xdg][] pattern is that on macOS, we
additionally require that `~/.config/nats` work, but in new installs it should
be a symbolic link to the `~/Library/Application Support/nats` directory.
This is a little extra work, but warranted by both complying with platform
expectations and being consistent since `.config/` does often exist on macOS
platforms where users have installed additional Unix heritage tools.


### Migration

Much of the existing client configuration involves authentication data such as
NKeys or combined `.creds` files.  These are more likely than most to have
been moved by the users to secure storage and replaced with symbolic links or
be on encrypted mounts.  As such, automatically migrating such data is fraught
with peril.

For the time being, we instead simply mandate that files be accessible by the
new paths; it is acceptable for client libraries to detect that the new
location does not exist on disk but the old location does exist, and put in a
symbolic link at the new location pointing to the old location.

Ideally, the users will migrate their data to clean up the symbolic links.

At a later date, we _might_ choose to automatically migrate files, as long as
a sweep of the old location confirms that all entries are files or
directories, free of symlinks, and all on the same file-system.

In the below descriptions of old locations, a phrase "unless overridden by an
environment variable" would be needed in many places to strictly cover all
contingencies; for clarity in the prose, we omit that to just describe the
defaults.
Similarly, unless specifically addressing an operating system, we'll use the
XDG locations for examples.

### The `nats` CLI

The `nats` CLI uses the XDG location logic, in a simplified form, so the data
has until now been written to `~/.config/nats/` always.  On Unix systems this
remains valid; on other systems supporting symbolic links, we should point to
that location.

The `nats` CLI has proven the utility of the configuration model we're moving
to.

The configuration used by the `nats` CLI are the configuration contexts, which
are generic for client tooling and not specific to that tool, so we consider
the `context/` directory and `context.txt` file to be top-level configuration
items.

### The `nsc` CLI

The default locations of files for `nsc` can be overridden with the
environment variables: `$NSC_HOME`, `$NKEYS_PATH`, and `$NSC_CWD_ONLY`.

In addition, `nsc` has traditionally detected an account system Operator in
the current directory and then ignored all environment variables to use the
current directory as a root.
This behavior is considered an authentication administrator mode and should
not be used by most tools.

Existing client tools should continue to honor `$NSC_HOME` and `$NKEYS_PATH`
to avoid breaking scripts, but new client tools should not use those
variables.

These files all now are under the ownership of the `nsc` concept, so live in
the `nsc/` sub-directory of the NATS locations.

The old file `~/.nsc/nsc.json` is configuration and so would live under
`$XDG_CONFIG_HOME` or equivalent.  Thus on Unix, `~/.config/nats/nsc/nsc.json`
should be used.

The credentials and nkeys files are considered data, and so will live under
`~/.local/share/nats/nsc/stores/` and `~/.local/share/nats/nsc/nkeys/`.


### Other environment variables not yet pulled in

Some client tools support `$NATS_KEY` and `$NATS_CERT` to refer to client
X.509 keys and certificates.

## Consequences

At some point in the future, we should attempt to migrate existing files in
their old locations to the new standard locations.  We should leave behind
symbolic links in "the other direction" to prevent breakage.

We should never remove the symbolic links from the old locations, it's up to
the users/administrators to do so when they're happy to do so.


## Registry

* Configuration
  + The `context/` directory and `context.txt` file are used as per [ADR-21][]
    for configuration contexts.
    - Library implementations of contexts should be aware of this.
    - Most applications should avoid assumptions.
    - Contexts can contain sensitive data, where a server uses password-based
      authentication.
    - compatibility location: strict XDG adherence on all platforms.
  + The `nsc/` and `nkeys/` directories are reserved for client credentials
    and the account system, as described above.
    - Most tools should not need to be specifically aware in code of the
      location of these; where credentials are passed in, they are typically
      part of the site configuration.
    - New tools should use [ADR-21][] configuration contexts to locate
      relevant data.
    - The `nkeys/` directory and `nsc/creds/` directory both contain sensitive
      client secrets and should be protected.  When creating these
      directories, or files within, permissions should be set to be tighter
      than the "umask", permitting access only for the user.
  + The `ngs/` directory is reserved for use by one of the corporate
    maintainers of NATS, Synadia Communications.
  + The `install-channel.txt` file is used by some installers for tracking
    which channel of available releases the installers/updaters should use.
    It should contain a single line of text containing a single word, the
    channel name.


<!-- Markdown References: -->

[ADR-21]: ADR-21.md
[adrg/xdg]: https://github.com/adrg/xdg
            "Adrian-George Bostan's implementation of XDG in Go"
[xdgbase]: https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html
           "XDG Base Directory Specification"
