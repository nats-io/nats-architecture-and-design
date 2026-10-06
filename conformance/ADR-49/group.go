// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

// Package adr49 contains conformance tests for ADR-49 JetStream
// Distributed Counter CRDT. It registers a single Group ("ADR-49-CTR")
// with the harness via init(); the runner imports this package for that
// side effect.
//
// Server / API level matrix this group asserts against:
//
//   - 2.12.0 / API Level 2 — initial counter support (baseline).
//
// Each test carries a Tag (e.g. "config", "increment", "sources",
// "mirrors", "reset") and may carry a SkipReason so resource-intensive
// or version-gated assertions stay visible in reports without forcing
// the whole suite to run.
package adr49

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

const groupName = "ADR-49-CTR"

func init() {
	harness.Register(&harness.Group{
		Name:  groupName,
		Title: "ADR-49: JetStream Distributed Counter CRDT",
		Description: `Conformance tests for ADR-49 JetStream Distributed Counter CRDT.

Baseline: server >= 2.12.0 (API level 2). Tests publish under
nats.adr.conformance.<test-id>.* and create their own streams named
CONF_<test-id>_<rand>. Streams are torn down per test, and a startup
sweep removes leftovers from prior aborted runs.`,
		References: []string{
			"../adr/ADR-49.md",
			"ADR-49.md",
		},
		Flags: []harness.FlagSpec{
			{Name: "api-level", Help: "Override server API level detection (0 = auto)", Type: harness.FlagInt, Default: "0"},
			{Name: "sources", Help: "Run sourcing-dependent tests (require eventually-consistent source delivery)", Type: harness.FlagBool, Default: "true"},
		},
		Tests:    allTests(),
		Setup:    groupSetup,
		Teardown: groupTeardown,
	})
}

func allTests() []harness.Test {
	var out []harness.Test
	out = append(out, ctr100Tests()...)
	out = append(out, ctr200Tests()...)
	out = append(out, ctr300Tests()...)
	out = append(out, ctr400Tests()...)
	out = append(out, ctr500Tests()...)
	out = append(out, ctr600Tests()...)
	out = append(out, ctr700Tests()...)
	out = append(out, ctr800Tests()...)
	out = append(out, ctr900Tests()...)
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

// requiresSources gates tests that depend on stream sourcing converging
// in bounded time.
func requiresSources() func(*harness.Options) string {
	return requiresFlag("sources", "sources tests disabled (--sources=false)")
}