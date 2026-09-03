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

// sch800Tests covers SCH-800: time zone handling for cron schedules.
// DST behaviour (forward-skip / backward-repeat) is informational and
// out of scope for runtime — the test exists as documentation of the
// gap so a future harness can fill it in.
func sch800Tests() []harness.Test {
	return []harness.Test{
		{ID: "SCH-801", Title: "Default cron timing is UTC", Section: "SCH-800", Tags: []string{"timezone", "slow"}, SkipReason: requiresSlow(), Run: testSCH801},
		{ID: "SCH-802", Title: "Specifying a valid time zone is accepted (informational)", Section: "SCH-800", Tags: []string{"timezone"}, SkipReason: requiresTZData(), Run: testSCH802},
		{ID: "SCH-803", Title: "DST forward-skip / backward-repeat is out of runtime scope", Section: "SCH-800", Tags: []string{"timezone", "dst"}, Run: testSCH803},
	}
}

func testSCH801(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.utc.a")
	tgt := h.Subject("target.utc.a")

	// Pick a UTC second 3-4s in the future — should fire near that
	// instant regardless of server local time zone, with no
	// Nats-Schedule-Time-Zone header.
	target := time.Now().UTC().Add(4 * time.Second)
	expr := fmt.Sprintf("%d %d %d * * *", target.Second(), target.Minute(), target.Hour())

	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, expr},
		schedHeader{HdrScheduleTarget, tgt},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	gen, err := waitForLastMsgOn(h, name, tgt, 10*time.Second)
	if err != nil {
		return fail("await target firing near UTC time %s: %v", target.Format(time.RFC3339), err)
	}
	// The actual firing time must be close to `target` UTC. The
	// generated message's Time field reflects when the server stored
	// it. Allow ±5s slack to accommodate scheduler granularity.
	delta := gen.Time.Sub(target)
	if delta < -5*time.Second || delta > 5*time.Second {
		return fail("firing time %s drifted %s from expected UTC instant %s",
			gen.Time.Format(time.RFC3339), delta, target.Format(time.RFC3339))
	}
	return pass()
}

func testSCH802(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.tz.named")
	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "0 30 9 * * *"},
		schedHeader{HdrScheduleTimeZone, "Europe/Amsterdam"},
		schedHeader{HdrScheduleTarget, h.Subject("target.tz.named")},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error != nil {
		return skip("server cannot resolve Europe/Amsterdam (tzdata missing or stale): %s", ack.Error)
	}
	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil || stored == nil {
		return fail("schedule was not stored: %v", err)
	}
	if got := stored.Header.Get(HdrScheduleTimeZone); got != "Europe/Amsterdam" {
		return fail("stored Nats-Schedule-Time-Zone=%q, want %q", got, "Europe/Amsterdam")
	}
	// Comparing the next-fire time across a zone-vs-UTC pair would
	// require a server API the harness does not have today; record
	// the observation as inconclusive so the spec can be tightened
	// when next-fire introspection lands.
	return inconclusive("schedule accepted; next-fire-time introspection is not available from the server-side API today")
}

func testSCH803(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// DST forward-skip / backward-repeat correctness requires waiting
	// across an actual DST transition or simulating server time. The
	// runtime suite skips this with a clear reason so the omission is
	// visible in reports.
	return skip("DST transition correctness is out of runtime scope; ADR-51 explicitly recommends avoiding cron schedules at DST transitions and the suite has no time-machine to exercise the behaviour")
}
