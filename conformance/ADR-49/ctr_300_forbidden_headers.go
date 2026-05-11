// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ctr300Tests covers CTR-300: header combinations a counter stream must
// reject (Nats-Rollup, Nats-Expected-* family).
func ctr300Tests() []harness.Test {
	return []harness.Test{
		{ID: "CTR-301", Title: "Nats-Rollup is rejected with Nats-Incr", Section: "CTR-300", Tags: []string{"headers"}, Run: testCTR301},
		{ID: "CTR-302", Title: "Nats-Expected-Last-Sequence is rejected with Nats-Incr", Section: "CTR-300", Tags: []string{"headers"}, Run: testCTR302},
		{ID: "CTR-303", Title: "Nats-Expected-Subject-Last-Sequence is rejected with Nats-Incr", Section: "CTR-300", Tags: []string{"headers"}, Run: testCTR303},
		{ID: "CTR-304", Title: "Nats-Expected-Stream is rejected with Nats-Incr", Section: "CTR-300", Tags: []string{"headers"}, Run: testCTR304},
		{ID: "CTR-305", Title: "Nats-Expected-Last-Msg-Id is rejected with Nats-Incr", Section: "CTR-300", Tags: []string{"headers"}, Run: testCTR305},
	}
}

// seedCounter pushes one accepted increment so subsequent tests have a
// valid last-sequence to point at.
func seedCounter(h *harness.Harness, subj string) error {
	ack, err := publishIncr(h, subj, "+1", nil)
	if err != nil {
		return err
	}
	if ack.Error != nil {
		return fmt.Errorf("seed publish: %s", ack.Error)
	}
	return nil
}

// rejectIncrWith publishes an increment plus the supplied extra header
// and asserts the publish is rejected and the stream did not advance.
func rejectIncrWith(h *harness.Harness, name, subj string, extra nats.Header, label string) (harness.Status, string, error) {
	pre, err := streamLastSeq(h, name)
	if err != nil {
		return fail("pre last seq: %v", err)
	}
	ack, err := publishIncr(h, subj, "+1", extra)
	if err != nil {
		return fail("publish %s: %v", label, err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack with %s, got success %+v", label, ack)
	}
	post, err := streamLastSeq(h, name)
	if err != nil {
		return fail("post last seq: %v", err)
	}
	if post != pre {
		return fail("stream advanced (%d -> %d) after rejected %s publish", pre, post, label)
	}
	return pass()
}

func testCTR301(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")

	for _, val := range []string{"sub", "all"} {
		extra := nats.Header{HdrRollup: []string{val}}
		if status, detail, err := rejectIncrWith(h, name, subj, extra, "Nats-Rollup="+val); status != harness.StatusPass {
			return status, detail, err
		}
	}
	return pass()
}

func testCTR302(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")
	if err := seedCounter(h, subj); err != nil {
		return fail("seed: %v", err)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("read seq: %v", err)
	}

	// Even with a CORRECT value the header itself must be rejected.
	extra := nats.Header{HdrExpLastSeq: []string{itoa(last)}}
	if status, detail, err := rejectIncrWith(h, name, subj, extra, "Nats-Expected-Last-Sequence (correct)"); status != harness.StatusPass {
		return status, detail, err
	}
	extra = nats.Header{HdrExpLastSeq: []string{"0"}}
	if status, detail, err := rejectIncrWith(h, name, subj, extra, "Nats-Expected-Last-Sequence=0"); status != harness.StatusPass {
		return status, detail, err
	}
	return pass()
}

func testCTR303(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")
	if err := seedCounter(h, subj); err != nil {
		return fail("seed: %v", err)
	}
	extra := nats.Header{HdrExpLastSubjSeq: []string{itoa(1)}}
	return rejectIncrWith(h, name, subj, extra, "Nats-Expected-Last-Subject-Sequence")
}

func testCTR304(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	extra := nats.Header{HdrExpStream: []string{name}}
	return rejectIncrWith(h, name, h.Subject("hits"), extra, "Nats-Expected-Stream")
}

func testCTR305(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	extra := nats.Header{HdrExpLastMsgID: []string{"anything"}}
	return rejectIncrWith(h, name, h.Subject("hits"), extra, "Nats-Expected-Last-Msg-Id")
}

// itoa formats a uint64 without pulling in fmt.
func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
