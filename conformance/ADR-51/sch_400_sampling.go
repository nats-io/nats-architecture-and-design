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

// sch400Tests covers SCH-400: subject sampling via Nats-Schedule-Source.
func sch400Tests() []harness.Test {
	slow := requiresSlow()
	return []harness.Test{
		{ID: "SCH-401", Title: "Source subject's last message is republished to target", Section: "SCH-400", Tags: []string{"sampling", "slow"}, SkipReason: slow, Run: testSCH401},
		{ID: "SCH-402", Title: "Wildcards in Nats-Schedule-Source rejected", Section: "SCH-400", Tags: []string{"sampling"}, Run: testSCH402},
		{ID: "SCH-403", Title: "Empty source falls back to schedule's body and headers", Section: "SCH-400", Tags: []string{"sampling", "slow"}, SkipReason: slow, Run: testSCH403},
		{ID: "SCH-404", Title: "Source updates reflected on subsequent firings", Section: "SCH-400", Tags: []string{"sampling", "slow"}, SkipReason: slow, Run: testSCH404},
	}
}

func testSCH401(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	src := h.Subject("sensors.cnc.temperature")
	tgt := h.Subject("sampled.cnc.temperature")
	schedSubj := h.Subject("schedules.sample.cnc")

	for i := 1; i <= 5; i++ {
		ack, err := publishSchedule(h, src, []byte(fmt.Sprintf("%d", i)))
		if err != nil || ack.Error != nil {
			return fail("seed source %d err=%v ack=%+v", i, err, ack)
		}
	}

	ack, err := publishSchedule(h, schedSubj, []byte(""),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleSource, src},
		schedHeader{HdrScheduleTarget, tgt},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}

	gen, err := waitForLastMsgOn(h, name, tgt, 5*time.Second)
	if err != nil {
		return fail("await target: %v", err)
	}
	if string(gen.Data) != "5" {
		return fail("target payload=%q, want %q (last source value)", string(gen.Data), "5")
	}
	if got := gen.Header.Get(HdrScheduler); got != schedSubj {
		return fail("target Nats-Scheduler=%q, want %q", got, schedSubj)
	}
	return pass()
}

func testSCH402(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	pre, err := streamLastSeq(h, name)
	if err != nil {
		return fail("pre last seq: %v", err)
	}
	ack, err := publishSchedule(h, h.Subject("schedules.sample.bad"), []byte(""),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleSource, h.Subject("sensors.*")},
		schedHeader{HdrScheduleTarget, h.Subject("sampled.cnc.temperature")},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected wildcard Nats-Schedule-Source to be rejected, got success %+v", ack)
	}
	post, err := streamLastSeq(h, name)
	if err != nil {
		return fail("post last seq: %v", err)
	}
	if post != pre {
		return fail("stream advanced (%d -> %d) after rejected wildcard-source publish", pre, post)
	}
	return pass()
}

func testSCH403(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	src := h.Subject("sensors.empty.subject") // never written
	tgt := h.Subject("sampled.fallback")
	schedSubj := h.Subject("schedules.sample.fallback")

	ack, err := publishSchedule(h, schedSubj, []byte("fallback-body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleSource, src},
		schedHeader{HdrScheduleTarget, tgt},
		schedHeader{"X-Marker", "schedule-body"},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	gen, err := waitForLastMsgOn(h, name, tgt, 5*time.Second)
	if err != nil {
		return fail("await target: %v", err)
	}
	if string(gen.Data) != "fallback-body" {
		return fail("target payload=%q, want %q (fallback to schedule body)", string(gen.Data), "fallback-body")
	}
	if got := gen.Header.Get("X-Marker"); got != "schedule-body" {
		return fail("target X-Marker=%q, want %q (fallback should carry schedule headers)", got, "schedule-body")
	}
	if got := gen.Header.Get(HdrScheduler); got != schedSubj {
		return fail("target Nats-Scheduler=%q, want %q", got, schedSubj)
	}
	return pass()
}

func testSCH404(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	src := h.Subject("sensors.update.subject")
	tgt := h.Subject("sampled.update")
	schedSubj := h.Subject("schedules.sample.update")

	if ack, err := publishSchedule(h, src, []byte("first")); err != nil || ack.Error != nil {
		return fail("seed source err=%v ack=%+v", err, ack)
	}
	if ack, err := publishSchedule(h, schedSubj, []byte(""),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleSource, src},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}

	first := waitFor(5*time.Second, func() bool {
		m, err := lastMsgFor(h, name, tgt)
		return err == nil && m != nil && string(m.Data) == "first"
	})
	if !first {
		return fail("target did not converge to source value 'first'")
	}

	if ack, err := publishSchedule(h, src, []byte("second")); err != nil || ack.Error != nil {
		return fail("update source err=%v ack=%+v", err, ack)
	}
	updated := waitFor(5*time.Second, func() bool {
		m, err := lastMsgFor(h, name, tgt)
		return err == nil && m != nil && string(m.Data) == "second"
	})
	if !updated {
		return fail("target did not pick up updated source value 'second'")
	}
	return pass()
}
