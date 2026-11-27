// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

// Package adr43 contains conformance tests for ADR-43 JetStream
// Per-Message TTL. It registers a single Group ("ADR-43-TTL") with the
// harness via init(); the runner imports this package for that side
// effect.
//
// Server / API level matrix this group asserts against:
//
//   - 2.11 / API Level 1 — initial per-message TTL support (baseline).
//
// Several tests cover features ADR-43 explicitly flags as future
// (delete API marker, purge API marker) and report INCONCLUSIVE
// when the server does not yet implement them.
package adr43

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

const groupName = "ADR-43-TTL"

func init() {
	harness.Register(&harness.Group{
		Name:  groupName,
		Title: "ADR-43: JetStream Per-Message TTL",
		Description: `Conformance tests for ADR-43 JetStream Per-Message TTL.

Baseline: server >= 2.11 (API level 1). Some tests are slow because
they wait for real wall-clock TTL or MaxAge expiry — they are gated
by --slow=true (default true). Tests for the future Delete and Purge
API markers are reported as INCONCLUSIVE on servers that have not yet
shipped them.

Tests publish under nats.adr.conformance.<test-id>.* and create their
own streams named CONF_<test-id>_<rand>. Streams are torn down per
test, and a startup sweep removes leftovers from prior aborted runs.`,
		References: []string{
			"../adr/ADR-43.md",
			"ADR-43.md",
		},
		Flags: []harness.FlagSpec{
			{Name: "api-level", Help: "Override server API level detection (0 = auto)", Type: harness.FlagInt, Default: "0"},
			{Name: "slow", Help: "Run wall-clock TTL/MaxAge expiry tests", Type: harness.FlagBool, Default: "true"},
			{Name: "sources", Help: "Run sourcing/mirror tests (require eventually-consistent delivery)", Type: harness.FlagBool, Default: "true"},
		},
		Tests:    allTests(),
		Setup:    groupSetup,
		Teardown: groupTeardown,
	})
}

func allTests() []harness.Test {
	var out []harness.Test
	out = append(out, ttl100Tests()...)
	out = append(out, ttl200Tests()...)
	out = append(out, ttl300Tests()...)
	out = append(out, ttl400Tests()...)
	out = append(out, ttl500Tests()...)
	out = append(out, ttl700Tests()...)
	out = append(out, ttl800Tests()...)
	out = append(out, ttl900Tests()...)
	return out
}

// groupSetup sweeps any leftover streams from prior aborted runs so the
// first test does not collide with stale state.
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

func requiresFlag(flag, message string) func(*harness.Options) string {
	return func(opts *harness.Options) string {
		if !opts.Bool(flag, false) {
			return message
		}
		return ""
	}
}

func requiresSlow() func(*harness.Options) string {
	return requiresFlag("slow", "wall-clock expiry tests disabled (--slow=false)")
}

func requiresSources() func(*harness.Options) string {
	return requiresFlag("sources", "source/mirror tests disabled (--sources=false)")
}