// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

const fbGroupName = "ADR-50-FB"

func init() {
	harness.Register(&harness.Group{
		Name:  fbGroupName,
		Title: "ADR-50: Fast-ingest Batch Publishing",
		Description: `Conformance tests for ADR-50 Fast-ingest Batch Publishing.

Baseline: server >= 2.14.0 / API level 4. Some tests gate on optional
behaviours:
  * cluster R3 leader-change tests (--cluster, default off)
  * resource-intensive limits / large-batch tests (--resource-intensive, off)
  * 10s+ idle abandonment test (--slow, default on)

Each test publishes under nats.adr.conformance.<test-id>.* and creates
its own stream named CONF_<test-id>_<rand>. Streams are torn down per
test, plus a startup sweep removes leftovers from prior aborted runs.

The control channel is an old-style NATS inbox per ADR-50; the harness
allocates a fresh inbox per test/batch and subscribes to
<inbox>.<batch-id>.> for the batch's lifetime.`,
		References: []string{
			"../adr/ADR-50.md",
			"ADR-50-fast-batch.md",
		},
		Flags: []harness.FlagSpec{
			// "cluster" and "resource-intensive" are shared with
			// ADR-50-AB — the runner accepts identical specs across
			// groups so a single --cluster controls both suites.
			{Name: "cluster", Help: "Run cluster R3 leader-change tests", Type: harness.FlagBool, Default: "false"},
			{Name: "resource-intensive", Help: "Run resource-intensive tests (large batches, limit probes)", Type: harness.FlagBool, Default: "false"},
			{Name: "slow", Help: "Run >10s idle abandonment test", Type: harness.FlagBool, Default: "true"},
		},
		Tests:    fbAllTests(),
		Setup:    groupSetup,    // shared with ADR-50-AB (sweeps subject namespace)
		Teardown: groupTeardown, // shared no-op
	})
}

func fbAllTests() []harness.Test {
	var out []harness.Test
	out = append(out, fb100Tests()...)
	out = append(out, fb200Tests()...)
	out = append(out, fb300Tests()...)
	out = append(out, fb400Tests()...)
	out = append(out, fb500Tests()...)
	out = append(out, fb600Tests()...)
	out = append(out, fb700Tests()...)
	out = append(out, fb800Tests()...)
	out = append(out, fb900Tests()...)
	out = append(out, fb1000Tests()...)
	out = append(out, fb1100Tests()...)
	out = append(out, fb1200Tests()...)
	out = append(out, fb1300Tests()...)
	out = append(out, fb1400Tests()...)
	return out
}

// requiresSlow gates the ≥10s idle-abandonment test.
func requiresSlow() func(*harness.Options) string {
	return func(opts *harness.Options) string {
		if !opts.Bool("slow", true) {
			return "slow tests disabled (--slow=false)"
		}
		return ""
	}
}

// requiresCluster gates cluster R3 tests.
func requiresCluster() func(*harness.Options) string {
	return func(opts *harness.Options) string {
		if !opts.Bool("cluster", false) {
			return "cluster tests disabled (--cluster=false)"
		}
		return ""
	}
}

// requiresResourceIntensive gates large-batch / limit-probe tests.
func requiresResourceIntensive() func(*harness.Options) string {
	return func(opts *harness.Options) string {
		if !opts.Bool("resource-intensive", false) {
			return "resource-intensive tests disabled (--resource-intensive=false)"
		}
		return ""
	}
}