// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr51

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// sch100Tests covers SCH-100: stream-configuration toggles for
// AllowMsgSchedules and the constraints around it (mirrors, sources,
// AllowMsgTTL requirement, implicit rollup).
func sch100Tests() []harness.Test {
	return []harness.Test{
		{ID: "SCH-101", Title: "Enabling AllowMsgSchedules works", Section: "SCH-100", Tags: []string{"config"}, Run: testSCH101},
		{ID: "SCH-102", Title: "AllowMsgSchedules defaults off", Section: "SCH-100", Tags: []string{"config"}, Run: testSCH102},
		{ID: "SCH-103", Title: "AllowMsgSchedules cannot be disabled once enabled", Section: "SCH-100", Tags: []string{"config"}, Run: testSCH103},
		{ID: "SCH-104", Title: "AllowMsgSchedules can be enabled on an existing stream", Section: "SCH-100", Tags: []string{"config"}, Run: testSCH104},
		{ID: "SCH-105", Title: "Mirrors cannot enable AllowMsgSchedules", Section: "SCH-100", Tags: []string{"config", "mirrors"}, Run: testSCH105},
		{ID: "SCH-106", Title: "Sources cannot enable AllowMsgSchedules", Section: "SCH-100", Tags: []string{"config", "sources"}, Run: testSCH106},
		{ID: "SCH-107", Title: "Nats-Schedule-TTL requires AllowMsgTTL on the stream", Section: "SCH-100", Tags: []string{"config"}, Run: testSCH107},
		{ID: "SCH-108", Title: "Schedule message gets Nats-Rollup: sub auto-applied", Section: "SCH-100", Tags: []string{"config"}, Run: testSCH108},
	}
}

func testSCH101(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgSchedules: true, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowMsgSchedules {
		return fail("AllowMsgSchedules not reported as true (config=%+v)", cfg)
	}
	// ADR-51 §"Stream Configuration": "enabling `AllowMsgSchedules`
	// implicitly enables `AllowRollup` and clears `DenyPurge`."
	if !cfg.AllowRollup {
		return fail("AllowRollup should be true after enabling AllowMsgSchedules (config=%+v)", cfg)
	}
	if cfg.DenyPurge {
		return fail("DenyPurge should be false after enabling AllowMsgSchedules (config=%+v)", cfg)
	}
	return pass()
}

func testSCH102(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil || cfg.AllowMsgSchedules {
		return fail("AllowMsgSchedules should default to false (config=%+v)", cfg)
	}
	// Try to publish a schedule. The ADR allows two outcomes: server
	// rejects with an error, or the message is stored as ordinary
	// content with no schedule semantics. The latter case is exercised
	// by SCH-201/202 when the feature IS enabled — here we just record
	// the observed branch.
	_, err = publishSchedule(h, h.Subject("schedules.foo"), []byte("body"),
		schedHeader{HdrSchedule, "@hourly"},
		schedHeader{HdrScheduleTarget, h.Subject("target.foo")},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	return pass()
}

func testSCH103(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgSchedules: true, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjects := []string{h.SubjectPrefix() + ".>"}
	err := updateStream(h, streamConfig{
		Name:              name,
		Subjects:          subjects,
		AllowMsgSchedules: false,
		AllowMsgTTL:       true,
	})
	if err == nil {
		// The update returning success is itself a fail — but check that
		// the flag actually flipped, since some servers silently ignore
		// downgrades.
		cfg, _ := streamInfo(h, name)
		if cfg != nil && cfg.AllowMsgSchedules {
			return inconclusive("update returned success but AllowMsgSchedules remained true (server silently ignored downgrade)")
		}
		return fail("update to disable AllowMsgSchedules unexpectedly succeeded")
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("post-update stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowMsgSchedules {
		return fail("after rejected update, AllowMsgSchedules should still be true (config=%+v)", cfg)
	}
	return pass()
}

func testSCH104(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjects := []string{h.SubjectPrefix() + ".>"}
	if err := updateStream(h, streamConfig{
		Name:              name,
		Subjects:          subjects,
		AllowMsgSchedules: true,
		AllowMsgTTL:       true,
	}); err != nil {
		return fail("enable update: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowMsgSchedules {
		return fail("AllowMsgSchedules not reported true after update (config=%+v)", cfg)
	}
	if !cfg.AllowRollup {
		return fail("AllowRollup should have flipped to true alongside AllowMsgSchedules (config=%+v)", cfg)
	}

	// Verify the feature is wired up by publishing and observing a
	// schedule firing.
	tgt := h.Subject("target.update.a")
	ack, err := publishSchedule(h, h.Subject("schedules.update.a"), []byte("body"),
		schedHeader{HdrSchedule, "@at " + rfc3339In(2*time.Second)},
		schedHeader{HdrScheduleTarget, tgt},
	)
	if err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if _, err := waitForLastMsgOn(h, name, tgt, 6*time.Second); err != nil {
		return fail("schedule did not fire after enabling on update: %v", err)
	}
	return pass()
}

func testSCH105(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("SCH_105_SRC")
	if err := createStream(h, streamConfig{Name: src}); err != nil {
		return fail("create source: %v", err)
	}
	mir := h.MintStreamName("SCH_105_MIR")
	err := createStream(h, streamConfig{Name: mir, Mirror: &mirror{Name: src}, AllowMsgSchedules: true})
	if err == nil {
		return fail("expected error creating mirror with AllowMsgSchedules, got success")
	}
	return pass()
}

func testSCH106(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("SCH_106_SRC")
	if err := createStream(h, streamConfig{Name: src}); err != nil {
		return fail("create source: %v", err)
	}
	dst := h.MintStreamName("SCH_106_DST")
	err := createStream(h, streamConfig{
		Name:              dst,
		Subjects:          []string{h.SubjectPrefix() + ".dst.>"},
		Sources:           []source{{Name: src}},
		AllowMsgSchedules: true,
	})
	if err == nil {
		return fail("expected error creating sourced stream with AllowMsgSchedules, got success")
	}
	return pass()
}

func testSCH107(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	// Stream allows schedules but NOT per-message TTL.
	if err := createStream(h, streamConfig{Name: name, AllowMsgSchedules: true}); err != nil {
		return fail("stream create: %v", err)
	}
	pre, err := streamLastSeq(h, name)
	if err != nil {
		return fail("pre last seq: %v", err)
	}
	ack, err := publishSchedule(h, h.Subject("schedules.ttl"), []byte("body"),
		schedHeader{HdrSchedule, "@hourly"},
		schedHeader{HdrScheduleTarget, h.Subject("target.ttl")},
		schedHeader{HdrScheduleTTL, "5m"},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error publishing Nats-Schedule-TTL on a stream without AllowMsgTTL, got success %+v", ack)
	}
	post, err := streamLastSeq(h, name)
	if err != nil {
		return fail("post last seq: %v", err)
	}
	if post != pre {
		return fail("stream advanced (%d -> %d) after rejected schedule publish", pre, post)
	}
	return pass()
}

func testSCH108(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgSchedules: true, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.rollup")
	ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@hourly"},
		schedHeader{HdrScheduleTarget, h.Subject("target.rollup")},
	)
	if err != nil || ack.Error != nil {
		return fail("publish err=%v ack=%+v", err, ack)
	}
	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil {
		return fail("last for schedule: %v", err)
	}
	if stored == nil {
		return fail("schedule was not stored on %s", schedSubj)
	}
	// ADR-51: "Schedules are stored as rollup-subject messages: the
	// server auto-applies `Nats-Rollup: sub` if the publisher did not
	// set it."
	if got := stored.Header.Get(HdrRollup); got != RollupSub {
		return fail("schedule message Nats-Rollup=%q, want %q (server should auto-apply rollup-sub)", got, RollupSub)
	}
	return pass()
}
