// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb1100Tests covers FB-1100: per-stream / per-server / per-batch
// limits. All are opt-in via --resource-intensive because they hold
// many batches open or publish thousands of messages.
func fb1100Tests() []harness.Test {
	skip := requiresResourceIntensive()
	return []harness.Test{
		{ID: "FB-1101", Title: "Per-stream concurrent batch limit (1000)", Section: "FB-1100", Tags: []string{"limits", "resource-intensive"}, SkipReason: skip, Run: testFB1101},
		{ID: "FB-1102", Title: "Per-server concurrent batch limit (50,000)", Section: "FB-1100", Tags: []string{"limits", "resource-intensive"}, SkipReason: skip, Run: testFB1102},
		{ID: "FB-1103", Title: "No upper bound on per-batch message count (>1000)", Section: "FB-1100", Tags: []string{"limits", "resource-intensive"}, SkipReason: skip, Run: testFB1103},
	}
}

func testFB1101(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}

	const target = 1000
	handles := make([]*fbHandle, 0, target)
	defer func() {
		for _, hh := range handles {
			hh.Close()
		}
	}()

	for i := 0; i < target; i++ {
		hh, err := openFastBatch(h, name, 10, "ok")
		if err != nil {
			return fail("open batch %d: %v", i, err)
		}
		if err := hh.publish(h.Subject("a"), FBOpStart, nil, []byte("x")); err != nil {
			return fail("publish initial %d: %v", i, err)
		}
		if _, err := hh.awaitFlowAck(15 * time.Second); err != nil {
			return fail("flow ack on batch %d: %v", i, err)
		}
		handles = append(handles, hh)
	}

	// 1001st batch must be rejected.
	extra, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open extra batch: %v", err)
	}
	defer extra.Close()
	if err := extra.publish(h.Subject("a"), FBOpStart, nil, []byte("x")); err != nil {
		return fail("publish extra initial: %v", err)
	}
	m, err := extra.readNext(15 * time.Second)
	if err != nil {
		return fail("read extra inbox: %v", err)
	}
	if m.Error == nil {
		return fail("expected error on 1001st batch, got %+v", m)
	}
	return pass()
}

func testFB1102(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// 50 streams × 1000 batches each = 50,000 in-flight batches.
	const streams = 50
	const perStream = 1000
	streamNames := make([]string, 0, streams)
	for i := 0; i < streams; i++ {
		nm := h.MintStreamName("FB_1102_" + itoa50(i))
		if err := createStream(h, streamConfig{Name: nm, AllowBatchPublish: true}); err != nil {
			return fail("stream create %d: %v", i, err)
		}
		streamNames = append(streamNames, nm)
	}

	handles := make([]*fbHandle, 0, streams*perStream)
	defer func() {
		for _, hh := range handles {
			hh.Close()
		}
	}()

	for _, nm := range streamNames {
		for i := 0; i < perStream; i++ {
			hh, err := openFastBatch(h, nm, 10, "ok")
			if err != nil {
				return fail("open batch on %s: %v", nm, err)
			}
			if err := hh.publish(h.Subject("a"), FBOpStart, nil, []byte("x")); err != nil {
				return fail("publish initial: %v", err)
			}
			if _, err := hh.awaitFlowAck(15 * time.Second); err != nil {
				return fail("flow ack on stream %s batch %d: %v", nm, i, err)
			}
			handles = append(handles, hh)
		}
	}

	// 50,001st batch on any stream must be rejected.
	extra, err := openFastBatch(h, streamNames[0], 10, "ok")
	if err != nil {
		return fail("open extra batch: %v", err)
	}
	defer extra.Close()
	if err := extra.publish(h.Subject("a"), FBOpStart, nil, []byte("x")); err != nil {
		return fail("publish extra: %v", err)
	}
	m, err := extra.readNext(15 * time.Second)
	if err != nil {
		return fail("read extra inbox: %v", err)
	}
	if m.Error == nil {
		return fail("expected error on 50,001st batch, got %+v", m)
	}
	return pass()
}

func itoa50(n int) string {
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

func testFB1103(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 64, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("1")); err != nil {
		return fail("initial: %v", err)
	}
	for i := 2; i <= 1001; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}
	ack, err := handle.awaitPubAck(60 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil {
		return fail("pub ack error: %s", ack.Error)
	}
	if ack.BatchSize != 1002 {
		return fail("pub ack count=%d, want 1002", ack.BatchSize)
	}
	return pass()
}