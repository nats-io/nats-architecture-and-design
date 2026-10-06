// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr42

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// pg500Tests covers PG-500: pinned/unpinned advisories. Each test
// subscribes to the per-consumer advisory subject before the trigger
// event so no event is missed.
func pg500Tests() []harness.Test {
	return []harness.Test{
		{ID: "PG-501", Title: "consumer_group_pinned advisory on pin establishment", Section: "PG-500", Tags: []string{"advisory"}, Run: testPG501},
		{ID: "PG-502", Title: "consumer_group_unpinned advisory on UNPIN (reason=admin)", Section: "PG-500", Tags: []string{"advisory"}, Run: testPG502},
		{ID: "PG-503", Title: "consumer_group_unpinned advisory on idle switch (reason=timeout)", Section: "PG-500", Tags: []string{"advisory", "slow"}, SkipReason: requiresSlow(), Run: testPG503},
	}
}

func testPG501(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	get, stop, err := captureAdvisories(h, AdvisoryPinnedPrefix+stream+"."+cname, AdvisoryPinnedType)
	if err != nil {
		return fail("subscribe pinned advisory: %v", err)
	}
	defer stop()

	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	pin, _, err := pinClientFirst(h, stream, cname)
	if err != nil {
		return fail("%v", err)
	}

	if !waitFor(2*time.Second, func() bool { return len(get()) > 0 }) {
		return fail("no consumer_group_pinned advisory observed within 2s")
	}
	got := get()
	for _, ev := range got {
		if ev.Type != AdvisoryPinnedType {
			continue
		}
		if ev.Stream != stream {
			continue
		}
		if ev.Consumer != cname {
			continue
		}
		if ev.Group != "jobs" {
			return fail("advisory group=%q want %q", ev.Group, "jobs")
		}
		if ev.PinnedID != pin {
			return fail("advisory pinned_id=%q want %q", ev.PinnedID, pin)
		}
		return pass()
	}
	return fail("no matching pinned advisory found in %d events", len(got))
}

func testPG502(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 60*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	if _, _, err := pinClientFirst(h, stream, cname); err != nil {
		return fail("%v", err)
	}

	get, stop, err := captureAdvisories(h, AdvisoryUnpinnedPrefix+stream+"."+cname, AdvisoryUnpinnedType)
	if err != nil {
		return fail("subscribe unpinned advisory: %v", err)
	}
	defer stop()

	body, err := unpinGroup(h, stream, cname, "jobs")
	if err != nil {
		return fail("unpin: %v", err)
	}
	if hasError(body) {
		return fail("UNPIN returned error: %s", string(body))
	}

	if !waitFor(2*time.Second, func() bool { return len(get()) > 0 }) {
		return fail("no consumer_group_unpinned advisory observed within 2s of UNPIN")
	}
	for _, ev := range get() {
		if ev.Type != AdvisoryUnpinnedType {
			continue
		}
		if ev.Group != "jobs" {
			return fail("advisory group=%q want %q", ev.Group, "jobs")
		}
		if ev.Reason != "admin" {
			return fail("advisory reason=%q want %q", ev.Reason, "admin")
		}
		return pass()
	}
	return fail("no matching unpinned advisory found in %d events", len(get()))
}

func testPG503(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 5*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	if _, _, err := pinClientFirst(h, stream, cname); err != nil {
		return fail("%v", err)
	}

	get, stop, err := captureAdvisories(h, AdvisoryUnpinnedPrefix+stream+"."+cname, AdvisoryUnpinnedType)
	if err != nil {
		return fail("subscribe unpinned advisory: %v", err)
	}
	defer stop()

	// Stop pulling — wait for idle timeout (~5s) and then for advisory.
	if !waitFor(10*time.Second, func() bool { return len(get()) > 0 }) {
		return fail("no consumer_group_unpinned advisory observed within 10s of pin idle")
	}
	for _, ev := range get() {
		if ev.Type != AdvisoryUnpinnedType {
			continue
		}
		if ev.Reason != "timeout" {
			return fail("advisory reason=%q want %q", ev.Reason, "timeout")
		}
		if ev.Group != "jobs" {
			return fail("advisory group=%q want %q", ev.Group, "jobs")
		}
		return pass()
	}
	return fail("no matching unpinned advisory with reason=timeout found")
}
