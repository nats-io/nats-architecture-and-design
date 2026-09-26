// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

// Package adr31 contains conformance tests for ADR-31 JetStream Direct
// Get. It registers a single Group ("ADR-31-DG") with the harness via
// init(); the runner imports this package for that side effect.
//
// Server / API level matrix this group asserts against:
//
//   - pre-2.11 — basic Direct Get (last_by_subj, next_by_subj, seq).
//   - 2.11.0 / API Level 1 — start_time, batched requests, multi-subject
//     requests, Subject-Appended Direct Get.
//
// Each test publishes under nats.adr.conformance.<test-id>.* and creates
// its own stream named CONF_<test-id>_<rand>. Streams are torn down per
// test, plus a startup sweep removes leftovers from prior aborted runs.
package adr31

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

const groupName = "ADR-31-DG"

func init() {
	harness.Register(&harness.Group{
		Name:  groupName,
		Title: "ADR-31: JetStream Direct Get",
		Description: `Conformance tests for ADR-31 JetStream Direct Get.

Baseline: server with Direct Get support (any recent server). Multi-subject,
batched, start_time, and Subject-Appended endpoints require server >= 2.11
(API level >= 1) — those tests skip with a reason on older builds.

Tests publish under nats.adr.conformance.<test-id>.* and create their own
streams named CONF_<test-id>_<rand>. Streams are torn down per test, plus a
startup sweep removes leftovers from prior aborted runs.`,
		References: []string{
			"../adr/ADR-31.md",
			"ADR-31.md",
		},
		Flags: []harness.FlagSpec{
			{Name: "cluster", Help: "Run cluster R3 queue-group spread tests", Type: harness.FlagBool, Default: "false"},
			{Name: "resource-intensive", Help: "Run resource-intensive tests (1024+ subjects, large batches)", Type: harness.FlagBool, Default: "false"},
			{Name: "mirror", Help: "Run MIRROR Direct Get extension tests", Type: harness.FlagBool, Default: "true"},
		},
		Tests:    allTests(),
		Setup:    groupSetup,
		Teardown: groupTeardown,
	})
}

func allTests() []harness.Test {
	var out []harness.Test
	out = append(out, dg100Tests()...)
	out = append(out, dg200Tests()...)
	out = append(out, dg300Tests()...)
	out = append(out, dg400Tests()...)
	out = append(out, dg500Tests()...)
	out = append(out, dg600Tests()...)
	out = append(out, dg700Tests()...)
	return out
}

// groupSetup runs once before the group's tests — sweeps any leftover
// streams from prior aborted runs that share our subject namespace, so
// the first test does not collide with stale state.
func groupSetup(_ context.Context, h *harness.Harness) error {
	names, err := h.StreamsBySubject("nats.adr.conformance.>")
	if err != nil || len(names) == 0 {
		return nil
	}
	for _, n := range names {
		h.DeleteStream(n)
	}
	return nil
}

func groupTeardown(_ context.Context, _ *harness.Harness) error {
	return nil
}

// ---- per-test skip predicates ----

// requiresFlag returns a SkipReason that fires when the named bool flag
// is false.
func requiresFlag(flag, message string) func(*harness.Options) string {
	return func(opts *harness.Options) string {
		if !opts.Bool(flag, false) {
			return message
		}
		return ""
	}
}

func requiresCluster() func(*harness.Options) string {
	return requiresFlag("cluster", "cluster tests disabled (--cluster=false)")
}

func requiresResourceIntensive() func(*harness.Options) string {
	return requiresFlag("resource-intensive", "resource-intensive tests disabled (--resource-intensive=false)")
}

func requiresMirror() func(*harness.Options) string {
	return requiresFlag("mirror", "mirror tests disabled (--mirror=false)")
}
