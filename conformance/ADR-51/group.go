// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

// Package adr51 contains conformance tests for ADR-51 JetStream Message
// Scheduler. It registers a single Group ("ADR-51-SCH") with the harness
// via init(); the runner imports this package for that side effect.
//
// Server / API level matrix this group asserts against:
//
//   - 2.12.0 / API Level 2 — initial scheduler support (baseline).
//   - 2.14.0 — time zones for cron, Nats-Schedule-Rollup, atomic stop
//     semantics, schedule-source fallback, retention-interaction
//     clarifications.
//
// Each test carries a Tag (e.g. "config", "at", "cron", "sampling",
// "headers", "stop", "retention", "timezone") and may carry a
// SkipReason so tests dependent on optional features (cluster,
// time zones, slow firings) stay visible in reports without forcing the
// whole suite to run.
package adr51

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

const groupName = "ADR-51-SCH"

func init() {
	harness.Register(&harness.Group{
		Name:  groupName,
		Title: "ADR-51: JetStream Message Scheduler",
		Description: `Conformance tests for ADR-51 JetStream Message Scheduler.

Baseline: server >= 2.12.0 (API level 2). Tests publish under
nats.adr.conformance.<test-id>.* and create their own streams named
CONF_<test-id>_<rand>. Streams are torn down per test, and a startup
sweep removes leftovers from prior aborted runs.

Schedule subjects use the form nats.adr.conformance.<test-id>.schedules.*
and target subjects use nats.adr.conformance.<test-id>.target.* — both
are covered by the default per-test stream subject filter.

Some tests fire on a once-per-second cron and rely on a small wall-clock
budget (see --slow). Tests that depend on tzdata or on a clustered
deployment are gated by --tzdata and --cluster respectively.`,
		References: []string{
			"../adr/ADR-51.md",
			"ADR-51.md",
		},
		Flags: []harness.FlagSpec{
			{Name: "slow", Help: "Run tests that wait several seconds for cron firings", Type: harness.FlagBool, Default: "true"},
			{Name: "tzdata", Help: "Run tests that require server tzdata to be installed", Type: harness.FlagBool, Default: "true"},
			{Name: "cluster", Help: "Run cluster R3 tests (currently none — reserved)", Type: harness.FlagBool, Default: "false"},
		},
		Tests:    allTests(),
		Setup:    groupSetup,
		Teardown: groupTeardown,
	})
}

func allTests() []harness.Test {
	var out []harness.Test
	out = append(out, sch100Tests()...)
	out = append(out, sch200Tests()...)
	out = append(out, sch300Tests()...)
	out = append(out, sch400Tests()...)
	out = append(out, sch500Tests()...)
	out = append(out, sch600Tests()...)
	out = append(out, sch700Tests()...)
	out = append(out, sch800Tests()...)
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

// requiresSlow gates tests that wait multiple seconds for schedule
// firings to occur.
func requiresSlow() func(*harness.Options) string {
	return requiresFlag("slow", "slow tests disabled (--slow=false)")
}

// requiresTZData gates tests that need the server to have current
// tzdata installed (so `Nats-Schedule-Time-Zone: America/New_York` and
// similar resolve correctly).
func requiresTZData() func(*harness.Options) string {
	return requiresFlag("tzdata", "tzdata-dependent tests disabled (--tzdata=false)")
}
