# Testing flow for long-running server tests

| Metadata | Value                                                           |
|----------|-----------------------------------------------------------------|
| Date     | 2022-09-16                                                      |
| Author   | @mprimi, @ReubenMathew                                          |
| Status   | Approved                                                        |
| Tags     | server, testing                                                 |

| Revision | Date       | Author  | Info           |
|----------|------------|---------|----------------|
| 1        | 2022-09-16 | @mprimi | Initial design |

## Context and Problem Statement

The server codebase contains dormant tests currently skipped because they take too long to execute during PR validation.

## [Context | References | Prior Work]

The list of currently skipped test is relatively short:

 * [TestStreamSourcingScalingSourcingManyBenchmark](https://github.com/nats-io/nats-server/blob/2faea26f63ce7a6e14b3fa577025c47009d87d11/server/jetstream_sourcing_scaling_test.go#L111)
 * [TestJetStreamConsumerFetchWithDrain](https://github.com/nats-io/nats-server/blob/2faea26f63ce7a6e14b3fa577025c47009d87d11/server/jetstream_consumer_test.go#L1081)
 * [TestJetStreamClusterBusyStreams](https://github.com/nats-io/nats-server/blob/2faea26f63ce7a6e14b3fa577025c47009d87d11/server/jetstream_cluster_4_test.go#L1684)
 * [TestJetStreamClusterKeyValueSync](https://github.com/nats-io/nats-server/blob/2faea26f63ce7a6e14b3fa577025c47009d87d11/server/jetstream_cluster_4_test.go#L2882)
 * [TestJetStreamClusterRestartThenScaleStreamReplicas](https://github.com/nats-io/nats-server/blob/2faea26f63ce7a6e14b3fa577025c47009d87d11/server/jetstream_cluster_3_test.go#L5787)
 * [TestJetStreamClusterRestartThenScaleStreamReplicas](https://github.com/nats-io/nats-server/blob/2faea26f63ce7a6e14b3fa577025c47009d87d11/server/filestore_test.go#L6549)

However, addressing these would have the additional benefit of creating a new path for more long-running tests to be created in the future.

Other tests that are currently slow but enabled could also be relocated, (e.g., `TestJetStreamClusterStreamOrphanMsgsAndReplicasDrifting` average 6+ minutes runtime).

## Design

Proposed change summary:

 - Create a new test for long files in the server package, e.g.: `server/long_running_tests.go`
 - Test in the new files are **disabled by default** to avoid unintentional execution
   - e.g. `//go:build long_running_tests`
 - Relocate existing slow tests in the new file
 - Rename tests with a prefix `TestLongRunning<...>`
 - Update PR build workflow to ensure the tests are **built** but not executed
   - i.e. the `compile` target should include the `long_running_tests` build tag
 - Set up a new pipeline that runs nightly (or N times a day) exclusively for the long running tests

## Consequences

 - More tests, more coverage
 - New paved path for writing more kinds of tests (chaos, recovery, large assets, ...)
