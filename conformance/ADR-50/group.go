// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

// Package adr50 contains conformance tests for ADR-50 Atomic Batch
// Publishing. It registers a single Group ("ADR-50-AB") with the
// harness via init(); the runner imports this package for that side
// effect.
//
// Server / API level matrix this group asserts against:
//
//   - 2.12.0 / API Level 2 — initial atomic batch support (baseline).
//   - 2.12.1 — within-batch deduplication via Nats-Msg-Id.
//   - 2.14.0 / API Level 4 — Nats-Batch-Commit:eob (commit without
//     storing the final message).
//
// Each test carries a Tag (e.g. "dedup", "api-level-4", "cluster",
// "resource-intensive") and a SkipReason that consults the parsed
// run-time options. Skipped tests stay visible in the report so it is
// always clear which assertions did and did not run.
package adr50

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

const groupName = "ADR-50-AB"

func init() {
	harness.Register(&harness.Group{
		Name:  groupName,
		Title: "ADR-50: Atomic Batch Publishing",
		Description: `Conformance tests for ADR-50 Atomic Batch Publishing.

Baseline: server >= 2.12.0 (API level 2). Some tests gate on later
features:
  * 2.12.1 — within-batch dedup (--dedup, default on)
  * 2.14.0 / API level 4 — Nats-Batch-Commit:eob (--eob, default on)
  * cluster R3 leader-change tests (--cluster, default off)
  * AB-804 holds 1000 batches at once (--resource-intensive, off)

Each test publishes under nats.adr.conformance.<test-id>.* and creates
its own stream named CONF_<test-id>_<rand>. Streams are torn down per
test, plus a startup sweep removes leftovers from prior aborted runs.`,
		References: []string{
			"../adr/ADR-50-atomic-batch.md",
			"ADR-50-atomic-batch.md",
		},
		Flags: []harness.FlagSpec{
			{Name: "dedup", Help: "Run dedup tests (server >= 2.12.1)", Type: harness.FlagBool, Default: "true"},
			{Name: "eob", Help: "Run EOB / API-level-4 tests (server >= 2.14.0)", Type: harness.FlagBool, Default: "true"},
			{Name: "cluster", Help: "Run cluster R3 leader-change tests", Type: harness.FlagBool, Default: "false"},
			{Name: "resource-intensive", Help: "Run resource-intensive tests (e.g. AB-804: 1000 batches)", Type: harness.FlagBool, Default: "false"},
			{Name: "api-level", Help: "Override server API level detection (0 = auto)", Type: harness.FlagInt, Default: "0"},
		},
		Tests:    allTests(),
		Setup:    groupSetup,
		Teardown: groupTeardown,
	})
}

func allTests() []harness.Test {
	var out []harness.Test
	out = append(out, ab100Tests()...)
	out = append(out, ab200Tests()...)
	out = append(out, ab300Tests()...)
	out = append(out, ab400Tests()...)
	out = append(out, ab500Tests()...)
	out = append(out, ab600Tests()...)
	out = append(out, ab700Tests()...)
	out = append(out, ab800Tests()...)
	out = append(out, ab1000Tests()...)
	out = append(out, ab1100Tests()...)
	out = append(out, ab1200Tests()...)
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

// requiresAPILevel returns a SkipReason that fires when the server's
// detected API level (or override) is below required.
func requiresAPILevel(required int) func(*harness.Options) string {
	return func(opts *harness.Options) string {
		// We can't actually read the server's level from Options; the
		// Harness carries that. The runner's skip pass happens before
		// Run, but skipping is a per-test decision based on stable
		// configuration. We therefore rely on the override flag for
		// gating and document that auto-detection is for reporting.
		// If --api-level was explicitly set, honor it.
		got := opts.Int("api-level", 0)
		if got > 0 && got < required {
			return optionGateMessage(required, got)
		}
		return ""
	}
}

// requiresFlag returns a SkipReason that fires when the named bool flag
// is false. Used for opt-in / opt-out gates such as --dedup and --eob.
func requiresFlag(flag, message string) func(*harness.Options) string {
	return func(opts *harness.Options) string {
		if !opts.Bool(flag, false) {
			return message
		}
		return ""
	}
}

// requiresAny returns a SkipReason that fires when ANY supplied
// predicate produces a skip reason. The first non-empty reason wins.
func requiresAny(fns ...func(*harness.Options) string) func(*harness.Options) string {
	return func(opts *harness.Options) string {
		for _, fn := range fns {
			if r := fn(opts); r != "" {
				return r
			}
		}
		return ""
	}
}

func optionGateMessage(required, got int) string {
	return "requires server API level " + itoa(required) + ", got " + itoa(got)
}

func itoa(i int) string {
	// fmt.Sprint indirect: tiny helper to avoid pulling fmt into
	// every test's import set when only itoa is needed here.
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
