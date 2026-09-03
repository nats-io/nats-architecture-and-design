// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr51

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// sch200Tests covers SCH-200: single delayed publishes via @at.
func sch200Tests() []harness.Test {
	return []harness.Test{
		{ID: "SCH-201", Title: "@at in the near future fires once", Section: "SCH-200", Tags: []string{"at"}, Run: testSCH201},
		{ID: "SCH-202", Title: "@at in the past fires immediately", Section: "SCH-200", Tags: []string{"at"}, Run: testSCH202},
		{ID: "SCH-203", Title: "@at with non-UTC time zone is honored", Section: "SCH-200", Tags: []string{"at", "timezone"}, Run: testSCH203},
		{ID: "SCH-204", Title: "Nats-Schedule-TTL produces Nats-TTL on generated message", Section: "SCH-200", Tags: []string{"at", "ttl"}, Run: testSCH204},
		{ID: "SCH-205", Title: "Additional headers propagate verbatim to the target", Section: "SCH-200", Tags: []string{"at", "headers"}, Run: testSCH205},
		{ID: "SCH-206", Title: "Nats-TTL on schedule bounds schedule lifetime", Section: "SCH-200", Tags: []string{"at", "ttl", "slow"}, SkipReason: requiresSlow(), Run: testSCH206},
	}
}

// scheduleDefaultStream creates the standard test stream used across
// SCH-200..SCH-600 — schedules + targets covered, TTL allowed.
func scheduleDefaultStream(h *harness.Harness) (string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{
		Name:              name,
		AllowMsgSchedules: true,
		AllowMsgTTL:       true,
	}); err != nil {
		return "", err
	}
	return name, nil
}

func testSCH201(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.delayed.a")
	tgt := h.Subject("target.delayed.a")

	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@at " + rfc3339In(2*time.Second)},
		schedHeader{HdrScheduleTarget, tgt},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}

	gen, err := waitForLastMsgOn(h, name, tgt, 10*time.Second)
	if err != nil {
		return fail("await target: %v", err)
	}
	if string(gen.Data) != "body" {
		return fail("target payload=%q, want %q", string(gen.Data), "body")
	}
	if got := gen.Header.Get(HdrScheduleNext); got != ScheduleNextPurge {
		return fail("target Nats-Schedule-Next=%q, want %q", got, ScheduleNextPurge)
	}
	if got := gen.Header.Get(HdrScheduler); got != schedSubj {
		return fail("target Nats-Scheduler=%q, want %q", got, schedSubj)
	}
	// Single-delayed: schedule must self-purge after firing.
	gone := waitFor(5*time.Second, func() bool {
		m, err := lastMsgFor(h, name, schedSubj)
		return err == nil && m == nil
	})
	if !gone {
		return fail("single-delayed schedule was not removed after firing (still present on %s)", schedSubj)
	}
	return pass()
}

func testSCH202(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.past.a")
	tgt := h.Subject("target.past.a")

	ack, err := publishSchedule(h, schedSubj, []byte("past-body"),
		schedHeader{HdrSchedule, "@at 2009-11-10T23:00:00Z"},
		schedHeader{HdrScheduleTarget, tgt},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	gen, err := waitForLastMsgOn(h, name, tgt, 5*time.Second)
	if err != nil {
		return fail("await target: %v", err)
	}
	if got := gen.Header.Get(HdrScheduleNext); got != ScheduleNextPurge {
		return fail("target Nats-Schedule-Next=%q, want %q", got, ScheduleNextPurge)
	}
	return pass()
}

func testSCH203(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.tz.a")
	tgt := h.Subject("target.tz.a")

	// Express the target time in a non-UTC zone. We pick a fixed +02:00
	// offset rather than a named zone so this test does not depend on
	// server tzdata being installed — RFC3339 offsets are unambiguous.
	loc := time.FixedZone("plus2", 2*60*60)
	ts := rfc3339InZone(2*time.Second, loc)

	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@at " + ts},
		schedHeader{HdrScheduleTarget, tgt},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v (timestamp=%s)", err, ack, ts)
	}
	// If the server ignored the offset and read the wall-time as UTC,
	// the schedule would fire ~2 hours late. We bound the wait at 15s
	// so that path turns into a failure rather than a hang.
	if _, err := waitForLastMsgOn(h, name, tgt, 15*time.Second); err != nil {
		return fail("target did not fire — server may have ignored time-zone offset (%v)", err)
	}
	return pass()
}

func testSCH204(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.ttl.a")
	tgt := h.Subject("target.ttl.a")

	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@at " + rfc3339In(2*time.Second)},
		schedHeader{HdrScheduleTarget, tgt},
		schedHeader{HdrScheduleTTL, "5m"},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	gen, err := waitForLastMsgOn(h, name, tgt, 10*time.Second)
	if err != nil {
		return fail("await target: %v", err)
	}
	if got := gen.Header.Get(HdrTTL); got != "5m" {
		return fail("target Nats-TTL=%q, want %q", got, "5m")
	}
	if got := gen.Header.Get(HdrScheduleTTL); got != "" {
		return fail("target carried Nats-Schedule-TTL=%q — schedule-defining headers should be stripped from generated messages", got)
	}
	return pass()
}

func testSCH205(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.hdr.a")
	tgt := h.Subject("target.hdr.a")

	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@at " + rfc3339In(2*time.Second)},
		schedHeader{HdrScheduleTarget, tgt},
		schedHeader{"X-Custom", "test-42"},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	gen, err := waitForLastMsgOn(h, name, tgt, 10*time.Second)
	if err != nil {
		return fail("await target: %v", err)
	}
	if got := gen.Header.Get("X-Custom"); got != "test-42" {
		return fail("target X-Custom=%q, want %q (extra headers should propagate verbatim)", got, "test-42")
	}
	if got := gen.Header.Get(HdrScheduler); got != schedSubj {
		return fail("target Nats-Scheduler=%q, want %q", got, schedSubj)
	}
	if got := gen.Header.Get(HdrSchedule); got != "" {
		return fail("target carried Nats-Schedule=%q — schedule-defining headers should be stripped", got)
	}
	if got := gen.Header.Get(HdrScheduleTarget); got != "" {
		return fail("target carried Nats-Schedule-Target=%q — schedule-defining headers should be stripped", got)
	}
	return pass()
}

func testSCH206(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.expire.a")
	tgt := h.Subject("target.expire.a")

	// Schedule fires far enough out that the per-message TTL on the
	// schedule itself is what evicts it before it can fire.
	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@at " + rfc3339In(60*time.Second)},
		schedHeader{HdrScheduleTarget, tgt},
		schedHeader{HdrTTL, "2s"},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	// Wait long enough for the TTL to kick in.
	time.Sleep(5 * time.Second)
	if m, err := lastMsgFor(h, name, tgt); err != nil {
		return fail("target subject lookup: %v", err)
	} else if m != nil {
		return fail("target message appeared on %s — schedule should have been removed by TTL before firing", tgt)
	}
	if m, err := lastMsgFor(h, name, schedSubj); err != nil {
		return fail("schedule subject lookup: %v", err)
	} else if m != nil {
		return fail("schedule still present on %s after TTL — Nats-TTL should have removed it", schedSubj)
	}
	return pass()
}
