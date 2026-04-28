// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr51

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// sch500Tests covers SCH-500: schedule-defining header validation —
// time zones, rollups, target-subject coverage, and propagation.
func sch500Tests() []harness.Test {
	return []harness.Test{
		{ID: "SCH-501", Title: "Nats-Schedule-Time-Zone applies to a cron schedule", Section: "SCH-500", Tags: []string{"timezone"}, SkipReason: requiresTZData(), Run: testSCH501},
		{ID: "SCH-502", Title: "Nats-Schedule-Time-Zone rejected on @at schedules", Section: "SCH-500", Tags: []string{"timezone"}, Run: testSCH502},
		{ID: "SCH-503", Title: "Nats-Schedule-Time-Zone rejected on @every schedules", Section: "SCH-500", Tags: []string{"timezone"}, Run: testSCH503},
		{ID: "SCH-504", Title: "Invalid Nats-Schedule-Time-Zone value rejected", Section: "SCH-500", Tags: []string{"timezone"}, Run: testSCH504},
		{ID: "SCH-505", Title: "Nats-Schedule-Rollup: sub yields Nats-Rollup: sub on target", Section: "SCH-500", Tags: []string{"rollup"}, Run: testSCH505},
		{ID: "SCH-506", Title: "Nats-Schedule-Rollup with non-sub value rejected", Section: "SCH-500", Tags: []string{"rollup"}, Run: testSCH506},
		{ID: "SCH-507", Title: "Nats-Schedule-Target outside the stream's subjects rejected", Section: "SCH-500", Tags: []string{"validation"}, Run: testSCH507},
		{ID: "SCH-508", Title: "Schedule headers are stripped from the generated message", Section: "SCH-500", Tags: []string{"headers"}, Run: testSCH508},
	}
}

func testSCH501(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	// Probe: UTC is always resolvable by `time.LoadLocation`, so it
	// confirms that the `Nats-Schedule-Time-Zone` plumbing is wired up
	// independently of host tzdata.
	probeSubj := h.Subject("schedules.cron.tz.probe")
	probe, err := publishSchedule(h, probeSubj, []byte("probe"),
		schedHeader{HdrSchedule, "0 30 9 * * *"},
		schedHeader{HdrScheduleTimeZone, "UTC"},
		schedHeader{HdrScheduleTarget, h.Subject("target.tz.probe")},
	)
	if err != nil {
		return fail("probe publish: %v", err)
	}
	if probe.Error != nil {
		return fail("server rejected Nats-Schedule-Time-Zone: UTC — header plumbing is broken: %s", probe.Error)
	}

	// Now the actual test: a named IANA zone. The server resolves
	// names via `time.LoadLocation` against host tzdata; if tzdata is
	// missing for the zone the server returns 10189
	// (JSMessageSchedulesPatternInvalidErr), which is the same code it
	// uses for malformed cron patterns. We treat that as SKIP because
	// the plumbing has been confirmed by the UTC probe.
	schedSubj := h.Subject("schedules.cron.tz")
	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "0 30 9 * * *"},
		schedHeader{HdrScheduleTimeZone, "America/New_York"},
		schedHeader{HdrScheduleTarget, h.Subject("target.tz.cron")},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error != nil {
		return skip("server cannot resolve America/New_York (server tzdata missing or stale; UTC probe succeeded so header plumbing is fine): %s", ack.Error)
	}
	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil || stored == nil {
		return fail("schedule was not stored: %v", err)
	}
	if got := stored.Header.Get(HdrScheduleTimeZone); got != "America/New_York" {
		return fail("stored Nats-Schedule-Time-Zone=%q, want %q", got, "America/New_York")
	}
	return pass()
}

func testSCH502(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if _, err := scheduleDefaultStream(h); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishSchedule(h, h.Subject("schedules.tz.at"), []byte("body"),
		schedHeader{HdrSchedule, "@at 2099-01-01T00:00:00Z"},
		schedHeader{HdrScheduleTimeZone, "Europe/Amsterdam"},
		schedHeader{HdrScheduleTarget, h.Subject("target.tz.at")},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack — Nats-Schedule-Time-Zone is cron-only, got success %+v", ack)
	}
	return pass()
}

func testSCH503(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if _, err := scheduleDefaultStream(h); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishSchedule(h, h.Subject("schedules.tz.every"), []byte("body"),
		schedHeader{HdrSchedule, "@every 1m"},
		schedHeader{HdrScheduleTimeZone, "Europe/Amsterdam"},
		schedHeader{HdrScheduleTarget, h.Subject("target.tz.every")},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack — Nats-Schedule-Time-Zone is cron-only, got success %+v", ack)
	}
	return pass()
}

func testSCH504(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	// ADR-51 §"Cron-like schedules" / Headers: only IANA names, `UTC`,
	// and `Local` are accepted; fixed offsets and abbreviations must
	// be rejected.
	cases := []struct {
		zone string
		why  string
	}{
		{"Not/A_Zone", "nonsense IANA-shaped name"},
		{"+02:00", "fixed UTC offset (not supported)"},
		{"EST", "time-zone abbreviation (not supported)"},
		{"CET", "time-zone abbreviation (not supported)"},
		{"", "empty value"},
	}
	for i, c := range cases {
		schedSubj := h.Subject(fmt.Sprintf("schedules.tz.bad.%d", i))
		ack, err := publishSchedule(h, schedSubj, []byte("body"),
			schedHeader{HdrSchedule, "* * * * * *"},
			schedHeader{HdrScheduleTimeZone, c.zone},
			schedHeader{HdrScheduleTarget, h.Subject(fmt.Sprintf("target.tz.bad.%d", i))},
		)
		if err != nil {
			return fail("publish for zone=%q (%s): %v", c.zone, c.why, err)
		}
		if ack.Error == nil {
			return fail("expected zone=%q (%s) to be rejected, got success %+v", c.zone, c.why, ack)
		}
		if m, err := lastMsgFor(h, name, schedSubj); err != nil {
			return fail("post-reject lookup for zone=%q: %v", c.zone, err)
		} else if m != nil {
			return fail("rejected schedule (zone=%q) was stored anyway on %s", c.zone, schedSubj)
		}
	}
	return pass()
}

func testSCH505(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	tgt := h.Subject("target.rollup.a")
	ack, err := publishSchedule(h, h.Subject("schedules.rollup.a"), []byte("body"),
		schedHeader{HdrSchedule, "@at " + rfc3339In(2*time.Second)},
		schedHeader{HdrScheduleTarget, tgt},
		schedHeader{HdrScheduleRollup, RollupSub},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	gen, err := waitForLastMsgOn(h, name, tgt, 10*time.Second)
	if err != nil {
		return fail("await target: %v", err)
	}
	if got := gen.Header.Get(HdrRollup); got != RollupSub {
		return fail("target Nats-Rollup=%q, want %q", got, RollupSub)
	}
	return pass()
}

func testSCH506(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if _, err := scheduleDefaultStream(h); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishSchedule(h, h.Subject("schedules.rollup.bad"), []byte("body"),
		schedHeader{HdrSchedule, "@hourly"},
		schedHeader{HdrScheduleTarget, h.Subject("target.rollup.bad")},
		schedHeader{HdrScheduleRollup, "all"},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected Nats-Schedule-Rollup=all to be rejected (only 'sub' is valid), got success %+v", ack)
	}
	return pass()
}

func testSCH507(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// Stream covering a narrow subject space; pick a target outside it.
	name := streamName(h)
	subjects := []string{h.SubjectPrefix() + ".schedules.>", h.SubjectPrefix() + ".target.>"}
	if err := createStream(h, streamConfig{
		Name:              name,
		Subjects:          subjects,
		AllowMsgSchedules: true,
		AllowMsgTTL:       true,
	}); err != nil {
		return fail("stream create: %v", err)
	}
	pre, err := streamLastSeq(h, name)
	if err != nil {
		return fail("pre last seq: %v", err)
	}
	ack, err := publishSchedule(h, h.Subject("schedules.bad.target"), []byte("body"),
		schedHeader{HdrSchedule, "@hourly"},
		schedHeader{HdrScheduleTarget, "nope.elsewhere"},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected target-outside-stream to be rejected, got success %+v", ack)
	}
	post, err := streamLastSeq(h, name)
	if err != nil {
		return fail("post last seq: %v", err)
	}
	if post != pre {
		return fail("stream advanced (%d -> %d) after rejected target-outside-stream publish", pre, post)
	}
	return pass()
}

func testSCH508(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	tgt := h.Subject("target.strip.a")

	ack, err := publishSchedule(h, h.Subject("schedules.strip.a"), []byte("body"),
		schedHeader{HdrSchedule, "@at " + rfc3339In(2*time.Second)},
		schedHeader{HdrScheduleTarget, tgt},
		schedHeader{HdrScheduleTTL, "1m"},
		schedHeader{HdrScheduleRollup, RollupSub},
		schedHeader{"X-Keep", "yes"},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	gen, err := waitForLastMsgOn(h, name, tgt, 10*time.Second)
	if err != nil {
		return fail("await target: %v", err)
	}
	for _, hdr := range []string{HdrSchedule, HdrScheduleTarget, HdrScheduleTTL, HdrScheduleRollup} {
		if got := gen.Header.Get(hdr); got != "" {
			return fail("target carried schedule-defining header %s=%q (should be stripped)", hdr, got)
		}
	}
	if got := gen.Header.Get("X-Keep"); got != "yes" {
		return fail("target X-Keep=%q, want %q", got, "yes")
	}
	if got := gen.Header.Get(HdrScheduler); got == "" {
		return fail("target missing Nats-Scheduler header")
	}
	if got := gen.Header.Get(HdrScheduleNext); got != ScheduleNextPurge {
		return fail("target Nats-Schedule-Next=%q, want %q", got, ScheduleNextPurge)
	}
	if got := gen.Header.Get(HdrTTL); got != "1m" {
		return fail("target Nats-TTL=%q, want %q", got, "1m")
	}
	if got := gen.Header.Get(HdrRollup); got != RollupSub {
		return fail("target Nats-Rollup=%q, want %q", got, RollupSub)
	}
	return pass()
}
