// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

// Package adr42 contains conformance tests for ADR-42 Pull Consumer
// Priority Groups. It registers a single Group ("ADR-42-PG") with the
// harness via init(); the runner imports this package for that side
// effect.
//
// Server / API level matrix this group asserts against:
//
//   - 2.11.0 — initial Priority Groups support: overflow, pinned_client,
//     prioritized policies; UNPIN API; advisories.
//   - 2.12.0 — refined 423 status descriptions (Nats-Wrong-Pin-Id /
//     Nats-Pin-Id mismatch).
//
// Each test publishes under nats.adr.conformance.<test-id>.* and creates
// its own stream named CONF_<test-id>_<rand>. Streams (and their
// consumers) are torn down per test, plus a startup sweep removes
// leftovers from prior aborted runs.
package adr42

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

const groupName = "ADR-42-PG"

func init() {
	harness.Register(&harness.Group{
		Name:  groupName,
		Title: "ADR-42: Pull Consumer Priority Groups",
		Description: `Conformance tests for ADR-42 Pull Consumer Priority Groups.

Baseline: server >= 2.11.0. The 423 status description variants
(Nats-Wrong-Pin-Id / Nats-Pin-Id mismatch) are 2.12+ — PG-302 / PG-303
assert only on the 423 code; the description is recorded.

Tests publish under nats.adr.conformance.<test-id>.* and create their own
streams named CONF_<test-id>_<rand>. Streams are torn down per test, plus
a startup sweep removes leftovers from prior aborted runs.

The 'slow' flag gates tests that block on real-time waits: PG-305 (pin
idle switch), PG-503 (timeout-reason advisory), PG-306 (long pin
retention).

The ADR-42 'failover' overflow option is not implemented in NATS Server
as of 2.14, so PG-206..PG-210 are intentionally absent.`,
		References: []string{
			"../adr/ADR-42.md",
			"ADR-42.md",
		},
		Flags: []harness.FlagSpec{
			{Name: "slow", Help: "Run real-time-wait tests (PG-305, PG-306, PG-503)", Type: harness.FlagBool, Default: "true"},
		},
		Tests:    allTests(),
		Setup:    groupSetup,
		Teardown: groupTeardown,
	})
}

func allTests() []harness.Test {
	var out []harness.Test
	out = append(out, pg100Tests()...)
	out = append(out, pg200Tests()...)
	out = append(out, pg300Tests()...)
	out = append(out, pg400Tests()...)
	out = append(out, pg500Tests()...)
	out = append(out, pg600Tests()...)
	out = append(out, pg700Tests()...)
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

// requiresFlag returns a SkipReason that fires when the named bool flag
// is false. Used for opt-in / opt-out gates such as --slow.
func requiresFlag(flag, message string) func(*harness.Options) string {
	return func(opts *harness.Options) string {
		if !opts.Bool(flag, false) {
			return message
		}
		return ""
	}
}

func requiresSlow() func(*harness.Options) string {
	return requiresFlag("slow", "real-time-wait tests disabled (--slow=false)")
}
