// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ctr400Tests covers CTR-400: stored representation and PubAck shape.
func ctr400Tests() []harness.Test {
	return []harness.Test{
		{ID: "CTR-401", Title: "Stored body is {\"val\":\"<decimal>\"}", Section: "CTR-400", Tags: []string{"storage"}, Run: testCTR401},
		{ID: "CTR-402", Title: "Nats-Incr and extra headers preserved on stored message", Section: "CTR-400", Tags: []string{"storage", "audit"}, Run: testCTR402},
		{ID: "CTR-403", Title: "PubAck.val equals post-increment total", Section: "CTR-400", Tags: []string{"storage", "puback"}, Run: testCTR403},
	}
}

func testCTR401(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")
	if ack, err := publishIncr(h, subj, "+42", nil); err != nil || ack.Error != nil {
		return fail("publish err=%v ack=%+v", err, ack)
	}
	last, err := lastMsgFor(h, name, subj)
	if err != nil || last == nil {
		return fail("last for %s: msg=%v err=%v", subj, last, err)
	}
	val, err := decodeVal(last.Data)
	if err != nil {
		return fail("decode body: %v", err)
	}
	if !bigEq(val, "42") {
		return fail("stored val=%q, want 42", val)
	}
	return pass()
}

func testCTR402(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")
	m := newIncrMsg(subj, "+7", nil)
	m.Header.Set("X-Trace", "abc123")
	if ack, err := publishMsg(h, m); err != nil || ack.Error != nil {
		return fail("publish err=%v ack=%+v", err, ack)
	}
	last, err := lastMsgFor(h, name, subj)
	if err != nil || last == nil {
		return fail("last for %s: msg=%v err=%v", subj, last, err)
	}
	if got := last.Header.Get(HdrIncr); got != "+7" {
		return fail("stored Nats-Incr=%q, want +7", got)
	}
	if got := last.Header.Get("X-Trace"); got != "abc123" {
		return fail("stored X-Trace=%q, want abc123", got)
	}
	return pass()
}

func testCTR403(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")

	steps := []struct {
		incr string
		want string
	}{
		{"+1", "1"},
		{"+1", "2"},
		{"-3", "-1"},
	}
	for i, s := range steps {
		ack, err := publishIncr(h, subj, s.incr, nil)
		if err != nil {
			return fail("step %d publish: %v", i, err)
		}
		if ack.Error != nil {
			return fail("step %d ack error: %s", i, ack.Error)
		}
		if !bigEq(ack.Value, s.want) {
			return fail("step %d ack.val=%q, want %s", i, ack.Value, s.want)
		}
		if ack.Stream != name {
			return fail("step %d ack.stream=%q, want %s", i, ack.Stream, name)
		}
		if ack.Sequence == 0 {
			return fail("step %d ack.seq=0, want non-zero", i)
		}
	}
	return pass()
}
