// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr51

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// sch300Tests covers SCH-300: cron-format and @every interval schedules.
// Tests that wait several seconds for cron firings are gated by --slow.
func sch300Tests() []harness.Test {
	slow := requiresSlow()
	return []harness.Test{
		{ID: "SCH-301", Title: "6-field crontab fires every second", Section: "SCH-300", Tags: []string{"cron", "slow"}, SkipReason: slow, Run: testSCH301},
		{ID: "SCH-302", Title: "@hourly is recognized and stored", Section: "SCH-300", Tags: []string{"cron"}, Run: testSCH302},
		{ID: "SCH-303", Title: "Other predefined schedules are recognized", Section: "SCH-300", Tags: []string{"cron"}, Run: testSCH303},
		{ID: "SCH-304", Title: "Cron schedule message persists across firings", Section: "SCH-300", Tags: []string{"cron", "slow"}, SkipReason: slow, Run: testSCH304},
		{ID: "SCH-305", Title: "@every interval schedule fires repeatedly", Section: "SCH-300", Tags: []string{"interval", "slow"}, SkipReason: slow, Run: testSCH305},
		{ID: "SCH-306", Title: "Invalid cron expression rejected", Section: "SCH-300", Tags: []string{"cron"}, Run: testSCH306},
		{ID: "SCH-307", Title: "Nats-Schedule-TTL on cron yields Nats-TTL each firing", Section: "SCH-300", Tags: []string{"cron", "ttl", "slow"}, SkipReason: slow, Run: testSCH307},
		{ID: "SCH-308", Title: "Nats-TTL on a cron schedule bounds total firings", Section: "SCH-300", Tags: []string{"cron", "ttl", "slow"}, SkipReason: slow, Run: testSCH308},
		{ID: "SCH-309", Title: "@every minimum interval is 1s", Section: "SCH-300", Tags: []string{"interval"}, Run: testSCH309},
	}
}

func testSCH301(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.cron.every_sec")
	tgt := h.Subject("target.cron.a")

	ack, err := publishSchedule(h, schedSubj, []byte("tick"),
		schedHeader{HdrSchedule, "* * * * * *"},
		schedHeader{HdrScheduleTarget, tgt},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	got, err := waitForCountOn(h, name, tgt, 3, 10*time.Second)
	if err != nil {
		return fail("waiting for 3 firings: %v (got %d)", err, got)
	}
	gen, err := lastMsgFor(h, name, tgt)
	if err != nil || gen == nil {
		return fail("read latest target: %v", err)
	}
	if got := gen.Header.Get(HdrScheduler); got != schedSubj {
		return fail("target Nats-Scheduler=%q, want %q", got, schedSubj)
	}
	next := gen.Header.Get(HdrScheduleNext)
	if next == "" || next == ScheduleNextPurge {
		return fail("cron firing should carry an RFC3339 Nats-Schedule-Next, got %q", next)
	}
	if _, err := time.Parse(time.RFC3339, next); err != nil {
		return fail("Nats-Schedule-Next=%q does not parse as RFC3339: %v", next, err)
	}
	return pass()
}

func testSCH302(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.hourly")
	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@hourly"},
		schedHeader{HdrScheduleTarget, h.Subject("target.hourly")},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil || stored == nil {
		return fail("schedule was not stored: %v", err)
	}
	if got := stored.Header.Get(HdrSchedule); got != "@hourly" {
		return fail("stored schedule expression=%q, want @hourly", got)
	}
	return pass()
}

func testSCH303(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	for _, expr := range []string{"@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight"} {
		schedSubj := h.Subject("schedules.predef." + expr[1:])
		ack, err := publishSchedule(h, schedSubj, []byte("body"),
			schedHeader{HdrSchedule, expr},
			schedHeader{HdrScheduleTarget, h.Subject("target.predef." + expr[1:])},
		)
		if err != nil {
			return fail("publish %s: %v", expr, err)
		}
		if ack.Error != nil {
			return fail("publish %s rejected: %s", expr, ack.Error)
		}
		stored, err := lastMsgFor(h, name, schedSubj)
		if err != nil || stored == nil {
			return fail("schedule for %s was not stored: %v", expr, err)
		}
	}
	return pass()
}

func testSCH304(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.cron.persist")
	tgt := h.Subject("target.persist")

	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "* * * * * *"},
		schedHeader{HdrScheduleTarget, tgt},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if _, err := waitForCountOn(h, name, tgt, 3, 10*time.Second); err != nil {
		return fail("waiting for 3 firings: %v", err)
	}
	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil || stored == nil {
		return fail("schedule was removed after firings — should persist (err=%v)", err)
	}
	if got := stored.Header.Get(HdrSchedule); got != "* * * * * *" {
		return fail("stored schedule expression=%q, want %q (cron should preserve original schedule)", got, "* * * * * *")
	}
	return pass()
}

func testSCH305(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.every.1s")
	tgt := h.Subject("target.every.a")

	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if _, err := waitForCountOn(h, name, tgt, 3, 10*time.Second); err != nil {
		return fail("waiting for 3 firings: %v", err)
	}
	gen, err := lastMsgFor(h, name, tgt)
	if err != nil || gen == nil {
		return fail("read latest target: %v", err)
	}
	next := gen.Header.Get(HdrScheduleNext)
	if next == "" || next == ScheduleNextPurge {
		return fail("interval firing should carry an RFC3339 Nats-Schedule-Next, got %q", next)
	}
	if _, err := time.Parse(time.RFC3339, next); err != nil {
		return fail("Nats-Schedule-Next=%q does not parse as RFC3339: %v", next, err)
	}
	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil || stored == nil {
		return fail("schedule was removed after firings — should persist (err=%v)", err)
	}
	return pass()
}

func testSCH306(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	pre, err := streamLastSeq(h, name)
	if err != nil {
		return fail("pre last seq: %v", err)
	}
	ack, err := publishSchedule(h, h.Subject("schedules.invalid"), []byte("body"),
		schedHeader{HdrSchedule, "not a valid cron"},
		schedHeader{HdrScheduleTarget, h.Subject("target.invalid")},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected invalid cron to be rejected, got success %+v", ack)
	}
	post, err := streamLastSeq(h, name)
	if err != nil {
		return fail("post last seq: %v", err)
	}
	if post != pre {
		return fail("stream advanced (%d -> %d) after rejected invalid-cron publish", pre, post)
	}
	return pass()
}

func testSCH307(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.cron.ttl")
	tgt := h.Subject("target.cron.ttl")

	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "* * * * * *"},
		schedHeader{HdrScheduleTarget, tgt},
		schedHeader{HdrScheduleTTL, "1m"},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if _, err := waitForCountOn(h, name, tgt, 2, 5*time.Second); err != nil {
		return fail("waiting for 2 firings: %v", err)
	}
	gen, err := lastMsgFor(h, name, tgt)
	if err != nil || gen == nil {
		return fail("read latest target: %v", err)
	}
	if got := gen.Header.Get(HdrTTL); got != "1m" {
		return fail("target Nats-TTL=%q, want %q", got, "1m")
	}
	return pass()
}

func testSCH308(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.cron.bounded")
	tgt := h.Subject("target.cron.bounded")

	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "* * * * * *"},
		schedHeader{HdrScheduleTarget, tgt},
		schedHeader{HdrTTL, "3s"},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	// Wait long enough for the schedule's own TTL to evict it.
	time.Sleep(8 * time.Second)
	stillPresent, err := lastMsgFor(h, name, schedSubj)
	if err != nil {
		return fail("schedule lookup: %v", err)
	}
	if stillPresent != nil {
		return fail("schedule still present on %s after TTL — Nats-TTL should have removed it", schedSubj)
	}
	// The number of generated messages is bounded by ~3 firings; we
	// don't assert on a precise count because the server's firing
	// scheduler granularity is implementation-defined.
	got, err := subjectCount(h, name, tgt)
	if err != nil {
		return fail("count on target: %v", err)
	}
	if got > 10 {
		return fail("target count=%d after 8s — TTL of 3s should bound firings to a small number", got)
	}
	return pass()
}

func testSCH309(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	tgt := h.Subject("target.every.tooShort")

	// Each value below is below the documented `1s` minimum and must be
	// rejected (`@every 0s` is also explicitly out — zero interval has
	// no meaning for a periodic schedule).
	tooShort := []string{"500ms", "100ms", "0s"}
	for _, v := range tooShort {
		schedSubj := h.Subject("schedules.every.short." + v)
		ack, err := publishSchedule(h, schedSubj, []byte("body"),
			schedHeader{HdrSchedule, "@every " + v},
			schedHeader{HdrScheduleTarget, tgt},
		)
		if err != nil {
			return fail("publish @every %s: %v", v, err)
		}
		if ack.Error == nil {
			return fail("expected @every %s to be rejected (below 1s minimum), got success %+v", v, ack)
		}
		if m, err := lastMsgFor(h, name, schedSubj); err != nil {
			return fail("post-reject lookup for @every %s: %v", v, err)
		} else if m != nil {
			return fail("rejected @every %s schedule was stored anyway on %s", v, schedSubj)
		}
	}

	// Positive control: 1s is exactly at the inclusive minimum and must
	// succeed. We don't wait on a firing — acceptance is sufficient.
	okSubj := h.Subject("schedules.every.short.ok")
	ack, err := publishSchedule(h, okSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	)
	if err != nil {
		return fail("positive-control publish: %v", err)
	}
	if ack.Error != nil {
		return fail("@every 1s unexpectedly rejected: %s", ack.Error)
	}
	return pass()
}
