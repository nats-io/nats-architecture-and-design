# Versioning for JetStream Assets

| Metadata | Value                                 |
|----------|---------------------------------------|
| Date     | 2024-07-22                            |
| Author   | @ripienaar                            |
| Status   | Implemented                           |
| Tags     | jetstream, server, client, 2.11, 2.12 |

| Revision | Date       | Author          | Info                                    | Server Requirement |
|----------|------------|-----------------|-----------------------------------------|--------------------|
| 1        | 2024-07-22 | @ripienaar      | Initial design                          |                    |
| 2        | 2025-08-05 | @MauriceVanVeen | Add required feature level in API calls | 2.12.0             |
| 3        | 2025-08-11 | @ripienaar      | Mark as fully implemented               | 2.12.0             |
| 4        | 2026-11-24 | @Jarema         | Add client rule for the server version  |                    |

# Context and Problem Statement

As development of the JetStream feature progress there is a complex relationship between connected-server, JetStream 
meta-leader and JetStream asset host versions that requires careful consideration to maintain compatibility.

 * The server a client connects to might be a Leafnode on a significantly older release and might not even have 
   JetStream enabled
 * The meta-leader could be on a version that does not support a feature and so would not forward the API fields it 
   is unaware of to the assigned servers
 * The server hosting an asset might not match the meta-leader and so can't honor the request for a certain 
   configuration and would silently drop fields

In general our stance is to insist on homogenous cluster versions in the general case but it's not reasonable to expect this
for leafnode servers nor is it possible during upgrading and other maintenance windows.

Our current approach is to check the connected-server version and project that it's representitive of the cluster as 
a whole but this is a known incorrect approach especially for Leafnodes as mentioned above.

We have considered approaches like fully versioning the API but this is unlikely to work for our case or be accepted 
by the team. Versioning the API would anyway not alleviate many of the problems encountered when upgrading and 
downgrading servers hosting long running assets. As a result we are evaluating some more unconventional approaches 
that should still improve our overall stance.

This ADR specifies a way for the servers to expose some versioning information to help clients improve the 
compatability story.

# Solution Overview

In lieu of enabling fine grained API versioning we want to start thinking about asset versioning instead, the server
should report it's properties when creating an asset and it should report similar properties when hosting an asset.

Fine grained API versioning would not really fix the entire problem as our long-lived assets would have to be upgraded 
over time between API versions as they get updated with new features. 

So we attempt to address both classes of problem here by utilizing Metadata and a few new server features and concepts.

# API Support Level

The first concept we wish to introduce is the concept of a number that indicates the API level of the servers
JetStream support.

| Level | Versions |
|-------|----------|
| 0     | < 2.11.0 |
| 1     | 2.11.x   |
| 2     | 2.12.x   |

While here it's shown incrementing at the major boundaries it's not strictly required, if we were to introduce a 
critical new feature mid 2.11 that could cause a bump in support level mid release without it being an issue - we 
do not require strict SemVer adherence.

The server will calculate this for a Stream and Consumer configuration. Here is example for the 2.11.x. It's not 
anticipated we would have to keep supporting versions for every asset for ever, it should be sufficient to support 
the most recent ones corresponding to the actively supported server versions.

```golang
func (s *Server) setConsumerAssetVersionMetadata(cfg *ConsumerConfig, create bool) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]string)
	}

	if create {
		cfg.Metadata[JSCreatedVersionMetadataKey] = VERSION
		cfg.Metadata[JSCreatedLevelMetadataKey] = JSFeatureLevel
	}

	featureLevel := "0"

	// Added in 2.11, absent | zero is the feature is not used.
	// one could be stricter and say even if its set but the time
	// has already passed it is also not needed to restore the consumer
	if cfg.PauseUntil != nil && !cfg.PauseUntil.IsZero() {
		featureLevel = "1"
	}

	cfg.Metadata[JSRequiredFeatureMetadataKey] = featureLevel
}
```

In this way we know per asset what server feature set it requires and only the server need this logic vs all the 
clients if the client had to assert `needs a server of at least level 1`.

We have to handle updates to asset configuration since an update might use a feature only found in newer servers.

Servers would advertise their supported API level in `jsz`, `varz` and `$JS.API.INFO`. It should also be logged
at JetStream startup.

## When to increment API Level

Generally when adding any new feature/field to the `StreamConfig` or `ConsumerConfig`. Especially when the field was set
by a user and the asset should be loaded in offline mode when the feature is not supported by the server.

If a new feature is added, the API level only needs to be incremented if another feature planned for the same release
didn't already increment the API level. Meaning if multiple new features are added within the same release cycle, the
API level only needs to be incremented once and not for every new feature.


# Server-set metadata

We'll store current server and asset related information in the existing `metadata` field allowing us to expand this in 
time, today we propose the following:

| Name              | Description                                   |
|-------------------|-----------------------------------------------|
| `_nats.ver`       | The current server version hosting an asset   |
| `_nats.level`     | The current server API level hosting an asset |
| `_nats.req.level` | The required API level to start an asset      |

We intend to store some client hints in here to help us track what client language and version created assets.

As some of these these are dynamic fields tools like Terraform and NACK will need to understand to ignore these fields 
when doing their remediation loops.

# Offline Assets

Today when an asset cannot be loaded it's simply not loaded. But to improve compatibility, user reporting and 
discovery we want to support a mode where a stream is visible in Stream reports but marked as offline with a reason.

To support this we add a field to the `io.nats.jetstream.api.v1.stream_list_response` and `io.nats.jetstream.api.v1.consumer_list_response`
that holds a map or offline assets and reasons `map[string]string`.

All offline streams will be added to the existing `missing` list in responses but offline ones will have the reasons in
the additional key. This ensure older tools will still understand these streams as inaccessible.

The `io.nats.jetstream.api.v1.stream_names_response` and `io.nats.jetstream.api.v1.consumer_names_response` results
should include the offline assets.

For starting incompatible streams in offline mode we would need to load the config in the current manner to figure out
which subjects Streams would listen on since even while Streams are offline we do need the protections of
overlapping subjects to be active to avoid issues later when the Stream can come online again.

# Safe unmarshalling of JSON data

The JetStream API and Meta-layer should start using the `DisallowUnknownFields` feature in the go json package and 
detect when asked to load incompatible assets or serve incompatible API calls and should error in the case of the 
API and start assets in Offline mode in the case of assets.

This will prevent assets inadvertently reverting some settings and changing behaviour during downgrades.

A POC branch against 2.11 main identified only 1 test failure after changing all JSON Unmarshall calls and this was a 
legit bug in a test.

One possible approach that can be introduced in 2.11 is to already perform strict unmarshalling in all cases but when a
issue is detected we would log the error and then do a normal unmarshal to remain compatible.  A configuration option
should exist to turn this into a fatal error rather than a logged warning only.

It could also be desirable to allow a header in the API requests to signal strict unmarshalling should be fatal for a 
specific API call in order to facilitate testing.

# Minimal supported API level for assets

When the server loads assets it should detect incompatible features using a combination of `DisallowUnknownFields` 
and comparing the server API levels to those required by the asset.

Incompatible assets should be loaded in Offline mode and an advisory should be published.

# Required API level in API Calls

Clients can assert that a certain API call requires a minimum API Level by including a `Nats-Required-Api-Level` header,
containing the requested minimum API level as a string. All endpoints under the JetStream API (`$JS.API.>`) should be
supported.

This asserts whether the server that's responding supports this API level. The request will error with
`api level not supported` if the server has a lower API level than required.

A single server does not represent a cluster-wide supported API level. In the future we should keep track of a
cluster-agreed API level, and have the servers enforce an agreed-upon API level.

# Client use of the server version

The `version` field in `INFO` is the release of the server a client is connected to. A JetStream request is answered
by the meta-leader or by the server hosting the asset, and the connected server may be neither: it may be a leafnode
on another release, or a server without JetStream, as the Context and Problem Statement describes. The release version
therefore does not describe the server that handles the request.

The requests a client sends, and how it handles their responses, must not depend on the release version a server
reports, except as described in [Connection decisions](#connection-decisions). A client must not refuse a feature,
choose an API subject, change or drop a configuration field, or fall back to an older API because of that version.
For example, when `INFO` reports 2.9.0, a client still sends `filter_subjects` in a consumer configuration, and the
server decides what to do with it.

The rule applies to the release version wherever a client reads it: the `version` field in `INFO`, the `_nats.ver`
metadata described in [Server-set metadata](#server-set-metadata), and the version that monitoring endpoints such as
`varz` report.

A client may read the version and expose it to users. It may check that the version in `INFO` is a valid semantic
version, using the regular expression suggested at
[semver.org](https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string),
and may fail the attempt to connect to that server when it is not. A valid version must not fail the attempt, however
old the release. See [Consequences](#consequences) for what this rule changes on older servers.

## Checking the API level

The rule does not cover the API level. The meta-leader answers `$JS.API.INFO`, and as JetStream clusters are
expected to run a single release, the level it reports is taken to represent the cluster, which the release version of
the connected server does not.

A client can use the API level that `$JS.API.INFO` reports to decide whether a feature is available, and does so
where an ADR asks for it, in the way that ADR describes, as [ADR-8](ADR-8.md) and [ADR-48](ADR-48.md) do for limit
markers. ADR-8 states the same rule for that check: "clients should assert this using the `$JS.API.INFO` call or
similar means (not connected server version)". When the reported level is lower than the feature needs, the client
may refuse the feature. Sending the `Nats-Required-Api-Level` header stays optional on any request.

## Connection decisions

One exception applies. A decision about how to establish the connection to a server may read the release version in
that server's own `INFO`. That version belongs to the server whose behavior is in question, which is not the case for
a JetStream request. Whether to connect is not such a decision: only a version that is not a valid semantic version
may fail the attempt to connect.

## Version comparison helpers

A client must deprecate its public functions that compare a server's release version with another version, or that
report a feature as supported because of that version, no later than the release in which it stops making decisions
from that version. It must remove them in its next release that may break its API. Accessors that return the version
string stay.

## Consequences

Under this rule, an older server receives requests it may not support, where a client could otherwise have refused
them, sent them to an older subject or changed them. The table lists the effects by server release, and the list
below it gives the detail.

| Server         | Effect                                                |
|----------------|-------------------------------------------------------|
| before 2.6.2   | Key-value buckets lack rollup and deny delete         |
| before 2.7.2   | Updating an existing key fails                        |
| before 2.9     | Creating a named consumer times out                   |
| up to 2.10     | Unknown fields are dropped without an error           |
| 2.11           | Unknown fields are dropped unless strict mode is on   |
| before 2.11    | A batch direct get returns a single message           |
| 2.12 and later | Unknown fields are rejected unless strict mode is off |
| any release    | A request to an API subject it lacks times out        |

 * Before 2.6.2, a key-value bucket is created without the rollup and deny delete settings that ADR-8 requires. These
   servers do not know the fields and drop them, and the create succeeds.
 * Before 2.7.2, a key-value bucket is created with discard policy `new`, as ADR-8 requires. These servers apply
   `new` to the per-subject limit, so with the default history of 1 every update to an existing key fails with a 503
   error, code 10077.
 * Before 2.9, a client that creates a named consumer on `$JS.API.CONSUMER.CREATE.<stream>.<consumer>`, with the
   stream name and the consumer name in place of the placeholders, gets no answer, and its request times out. The
   timeout does not tell the user that the server is too old. A consumer created without a name on
   `$JS.API.CONSUMER.CREATE.<stream>` is not affected.
 * Up to 2.10, the server drops a field it does not know without an error. 2.11 does the same unless strict mode is
   on, through the `strict` option in the `jetstream` configuration block, which is off by default in 2.11. For
   example, before 2.10 a consumer's `filter_subjects` is dropped and the consumer receives every message in the
   stream. On a work-queue stream the server rejects such a consumer when another consumer exists (code 10099), and
   when it is the first, every later consumer is rejected (codes 10099 and 10100).
 * Before 2.11, a batch direct get returns a single message without the `Nats-Num-Pending` header, and a batch
   request that sets none of `seq`, `last_by_subj` and `next_by_subj` gets a 408 status instead. A client that
   detects old servers by that header, as [ADR-31](ADR-31.md) describes, is not affected; a client that waits for the
   end-of-batch marker waits until its timeout.
 * From 2.12, strict mode is on by default, and the server rejects a field it does not know in a JetStream API
   request, such as a stream or consumer configuration, with a 400 error, code 10025, so the request fails instead of
   losing the field. A server with strict mode turned off drops the field as older servers do. Strict mode does not
   apply to direct get and pull requests, which drop such a field.
 * On any release, a request to an API subject the server does not have, such as the consumer reset API of
   [ADR-60](ADR-60.md) on a server before 2.14, gets no answer and times out.
