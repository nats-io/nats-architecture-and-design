// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"
	"strings"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ctr700Tests covers CTR-700: mirrors and counter-disabled sources
// store counter messages verbatim (no re-evaluation).
func ctr700Tests() []harness.Test {
	skip := requiresSources()
	return []harness.Test{
		{
			ID: "CTR-701", Title: "Mirrors store counter messages verbatim",
			Section: "CTR-700", Tags: []string{"mirrors"}, SkipReason: skip, Run: testCTR701,
		},
		{
			ID: "CTR-702", Title: "Sourced counter messages into a non-counter stream are stored verbatim",
			Section: "CTR-700", Tags: []string{"sources"}, SkipReason: skip, Run: testCTR702,
		},
	}
}

func testCTR701(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	mir := h.MintStreamName(tag + "_MIR")

	srcSubj := h.Subject("src") + ".>"
	if err := createStream(h, streamConfig{
		Name:            src,
		Subjects:        []string{srcSubj},
		AllowMsgCounter: true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	// Mirrors cannot enable AllowMsgCounter (CTR-105) — verify that
	// they still receive counter messages, just verbatim.
	if err := createStream(h, streamConfig{
		Name:   mir,
		Mirror: &mirror{Name: src},
	}); err != nil {
		return fail("create mirror: %v", err)
	}

	hitsSubj := h.Subject("src") + ".hits"
	want := []struct {
		incr string
		val  string
	}{
		{"+1", "1"},
		{"+2", "3"},
		{"+3", "6"},
	}
	for i, w := range want {
		if ack, err := publishIncr(h, hitsSubj, w.incr, nil); err != nil || ack.Error != nil {
			return fail("src publish %d err=%v ack=%+v", i, err, ack)
		}
	}

	caught := waitFor(10*time.Second, func() bool {
		last, err := streamLastSeq(h, mir)
		return err == nil && last == 3
	})
	if !caught {
		return fail("mirror did not catch up to 3 messages")
	}

	msgs, err := listMsgs(h, mir)
	if err != nil {
		return fail("list mirror msgs: %v", err)
	}
	if len(msgs) != 3 {
		return fail("expected 3 mirrored msgs, got %d", len(msgs))
	}
	for i, m := range msgs {
		val, err := decodeVal(m.Data)
		if err != nil {
			return fail("mirror msg %d decode: %v", i, err)
		}
		if !bigEq(val, want[i].val) {
			return fail("mirror msg %d val=%q, want %s (verbatim copy of source body)", i, val, want[i].val)
		}
		if got := m.Header.Get(HdrIncr); got != want[i].incr {
			return fail("mirror msg %d Nats-Incr=%q, want %s (verbatim copy of source header)", i, got, want[i].incr)
		}
	}
	return pass()
}

func testCTR702(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	obs := h.MintStreamName(tag + "_OBS")

	srcSubj := h.Subject("src") + ".>"
	if err := createStream(h, streamConfig{
		Name:            src,
		Subjects:        []string{srcSubj},
		AllowMsgCounter: true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	// Observer is a normal (non-counter) stream sourcing from the
	// counter source. ADR-49 requires verbatim storage.
	if err := createStream(h, streamConfig{
		Name:    obs,
		Sources: []source{{Name: src}},
	}); err != nil {
		return fail("create observer: %v", err)
	}

	hitsSubj := h.Subject("src") + ".hits"
	if ack, err := publishIncr(h, hitsSubj, "+5", nil); err != nil || ack.Error != nil {
		return fail("src publish err=%v ack=%+v", err, ack)
	}

	caught := waitFor(10*time.Second, func() bool {
		last, err := lastMsgFor(h, obs, hitsSubj)
		if err != nil || last == nil {
			return false
		}
		val, err := decodeVal(last.Data)
		return err == nil && bigEq(val, "5")
	})
	if !caught {
		return fail("observer did not converge to val=5")
	}

	last, err := lastMsgFor(h, obs, hitsSubj)
	if err != nil || last == nil {
		return fail("observer last for %s: %v", hitsSubj, err)
	}
	if got := last.Header.Get(HdrIncr); got != "+5" {
		return fail("observer Nats-Incr=%q, want +5 (verbatim — non-counter destination must NOT recompute deltas)", got)
	}
	if got := last.Header.Get(HdrCounterSources); got != "" {
		return fail("observer Nats-Counter-Sources=%q, want empty (non-counter stream must not synthesize this header)", got)
	}
	return pass()
}
