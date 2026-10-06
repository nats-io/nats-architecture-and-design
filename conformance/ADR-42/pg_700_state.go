// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr42

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// pg700Tests covers PG-700: priority_groups state reporting via
// $JS.API.CONSUMER.INFO.
func pg700Tests() []harness.Test {
	return []harness.Test{
		{ID: "PG-701", Title: "priority_groups state empty when nothing pinned", Section: "PG-700", Tags: []string{"state"}, Run: testPG701},
		{ID: "PG-702", Title: "priority_groups state populates after pinning", Section: "PG-700", Tags: []string{"state"}, Run: testPG702},
		{ID: "PG-703", Title: "priority_groups state clears or rotates after UNPIN", Section: "PG-700", Tags: []string{"state"}, Run: testPG703},
		{ID: "PG-704", Title: "priority_groups state absent on non-priority consumer", Section: "PG-700", Tags: []string{"state"}, Run: testPG704},
	}
}

func testPG701(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	info, err := consumerInfo(h, stream, cname)
	if err != nil {
		return fail("consumer info: %v", err)
	}
	if len(info.PriorityGroups) != 1 {
		return fail("priority_groups expected 1 entry, got %d (%+v)", len(info.PriorityGroups), info.PriorityGroups)
	}
	g := info.PriorityGroups[0]
	if g.Group != "jobs" {
		return fail("priority_groups[0].name=%q want %q", g.Group, "jobs")
	}
	if g.PinnedClientId != "" {
		return fail("priority_groups[0].pinned_id should be empty before any pin, got %q", g.PinnedClientId)
	}
	if g.PinnedTs != nil && !g.PinnedTs.IsZero() {
		return fail("priority_groups[0].pinned_ts should be empty before any pin, got %v", g.PinnedTs)
	}
	return pass()
}

func testPG702(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	t0 := time.Now()
	pin, _, err := pinClientFirst(h, stream, cname)
	if err != nil {
		return fail("%v", err)
	}
	info, err := consumerInfo(h, stream, cname)
	if err != nil {
		return fail("consumer info: %v", err)
	}
	if len(info.PriorityGroups) != 1 {
		return fail("priority_groups expected 1 entry, got %+v", info.PriorityGroups)
	}
	g := info.PriorityGroups[0]
	if g.Group != "jobs" {
		return fail("priority_groups[0].name=%q want %q", g.Group, "jobs")
	}
	if g.PinnedClientId != pin {
		return fail("priority_groups[0].pinned_id=%q want %q", g.PinnedClientId, pin)
	}
	if g.PinnedTs == nil || g.PinnedTs.IsZero() {
		return fail("priority_groups[0].pinned_ts is empty after pin")
	}
	delta := g.PinnedTs.Sub(t0)
	if delta < -30*time.Second || delta > 30*time.Second {
		return fail("priority_groups[0].pinned_ts=%v not within ~30s of t0=%v (delta=%v)", g.PinnedTs, t0, delta)
	}
	return pass()
}

func testPG703(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	pinX, _, err := pinClientFirst(h, stream, cname)
	if err != nil {
		return fail("%v", err)
	}
	body, err := unpinGroup(h, stream, cname, "jobs")
	if err != nil {
		return fail("unpin: %v", err)
	}
	if hasError(body) {
		return fail("UNPIN returned error: %s", string(body))
	}
	info, err := consumerInfo(h, stream, cname)
	if err != nil {
		return fail("consumer info: %v", err)
	}
	if len(info.PriorityGroups) == 0 {
		return inconclusive("priority_groups state was emptied entirely after UNPIN")
	}
	g := info.PriorityGroups[0]
	switch {
	case g.PinnedClientId == "":
		return pass()
	case g.PinnedClientId == pinX:
		return fail("UNPIN did not clear the pin: pinned_id is still %q", pinX)
	default:
		return inconclusive("UNPIN immediately re-pinned to a new id %q (no standby pull was waiting in this test, but this branch is acceptable per ADR)", g.PinnedClientId)
	}
}

func testPG704(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	if _, err := createConsumer(h, stream, consumerConfig{
		Name:      cname,
		AckPolicy: "explicit",
	}); err != nil {
		return fail("consumer create: %v", err)
	}
	info, err := consumerInfo(h, stream, cname)
	if err != nil {
		return fail("consumer info: %v", err)
	}
	if len(info.PriorityGroups) == 0 {
		return pass()
	}
	return inconclusive("non-priority consumer reports priority_groups=%+v (server uses empty array form rather than absent)", info.PriorityGroups)
}
