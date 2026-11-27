// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ctr800Tests covers CTR-800: counter reset behaviors.
func ctr800Tests() []harness.Test {
	return []harness.Test{
		{ID: "CTR-801", Title: "Subject purge resets a standalone counter", Section: "CTR-800", Tags: []string{"reset"}, Run: testCTR801},
		{ID: "CTR-802", Title: "Negative publish + purge-with-keep resets while leaving zero in stream", Section: "CTR-800", Tags: []string{"reset"}, Run: testCTR802},
	}
}

func testCTR801(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")

	// Seed to 42.
	if ack, err := publishIncr(h, subj, "+42", nil); err != nil || ack.Error != nil {
		return fail("seed err=%v ack=%+v", err, ack)
	}

	if err := purgeSubject(h, name, subj, 0); err != nil {
		return fail("purge: %v", err)
	}
	if last, err := lastMsgFor(h, name, subj); err != nil || last != nil {
		return fail("expected no message after purge, got %v err=%v", last, err)
	}

	ack, err := publishIncr(h, subj, "+1", nil)
	if err != nil || ack.Error != nil {
		return fail("post-purge publish err=%v ack=%+v", err, ack)
	}
	if !bigEq(ack.Value, "1") {
		return fail("post-purge ack.val=%q, want 1 (counter must restart from zero)", ack.Value)
	}
	return pass()
}

func testCTR802(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")

	// Drive the counter to 100 via a few increments.
	for _, v := range []string{"+50", "+30", "+20"} {
		if ack, err := publishIncr(h, subj, v, nil); err != nil || ack.Error != nil {
			return fail("seed %s err=%v ack=%+v", v, err, ack)
		}
	}

	// Negative publish equal to current total — reset to zero.
	ack, err := publishIncr(h, subj, "-100", nil)
	if err != nil || ack.Error != nil {
		return fail("zero-publish err=%v ack=%+v", err, ack)
	}
	if !bigEq(ack.Value, "0") {
		return fail("zero-publish ack.val=%q, want 0", ack.Value)
	}

	// Purge but keep the most recent message — that's the zero entry.
	if err := purgeSubject(h, name, subj, 1); err != nil {
		return fail("purge keep=1: %v", err)
	}

	last, err := lastMsgFor(h, name, subj)
	if err != nil || last == nil {
		return fail("expected reset message after purge keep=1, got %v err=%v", last, err)
	}
	val, err := decodeVal(last.Data)
	if err != nil {
		return fail("decode body: %v", err)
	}
	if !bigEq(val, "0") {
		return fail("retained message val=%q, want 0", val)
	}

	// New increment continues from zero.
	ack, err = publishIncr(h, subj, "+5", nil)
	if err != nil || ack.Error != nil {
		return fail("post-reset publish err=%v ack=%+v", err, ack)
	}
	if !bigEq(ack.Value, "5") {
		return fail("post-reset ack.val=%q, want 5", ack.Value)
	}
	return pass()
}
