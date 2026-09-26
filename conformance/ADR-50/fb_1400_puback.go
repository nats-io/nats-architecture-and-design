// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb1400Tests covers FB-1400: PubAck shape (batch / count fields).
func fb1400Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-1401", Title: "PubAck.batch and PubAck.count populated correctly", Section: "FB-1400", Tags: []string{"puback"}, Run: testFB1401},
		{ID: "FB-1402", Title: "PubAck is the only message without a type field", Section: "FB-1400", Tags: []string{"puback"}, Run: testFB1402},
	}
}

func testFB1401(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("1")); err != nil {
		return fail("initial: %v", err)
	}
	for i := 2; i <= 6; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}
	ack, err := handle.awaitPubAck(10 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil {
		return fail("pub ack error: %s", ack.Error)
	}
	if ack.BatchID != handle.batchID {
		return fail("PubAck.batch=%q, want %q", ack.BatchID, handle.batchID)
	}
	if ack.BatchSize != 7 {
		return fail("PubAck.count=%d, want 7", ack.BatchSize)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if ack.Sequence != last {
		return fail("PubAck.seq=%d != stream last seq %d", ack.Sequence, last)
	}
	return pass()
}

func testFB1402(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 5, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("1")); err != nil {
		return fail("initial: %v", err)
	}
	if _, err := handle.awaitFlowAck(5 * time.Second); err != nil {
		return fail("first flow ack: %v", err)
	}
	for i := 2; i <= 5; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	// Skip seq 6 to elicit a gap
	handle.seq++
	for i := 7; i <= 49; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}

	gapSeen := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		m, err := handle.readNext(time.Until(deadline))
		if err != nil {
			return fail("read inbox: %v", err)
		}
		switch m.classify() {
		case "ack", "err":
			if m.Type == "" {
				return fail("intermediate inbox msg has empty type field: %+v", m)
			}
		case "gap":
			gapSeen = true
			if m.Type == "" {
				return fail("BatchFlowGap has empty type field: %+v", m)
			}
		case "pubAck":
			if m.Type != "" {
				return fail("PubAck unexpectedly carries a type field %q: %+v", m.Type, m)
			}
			if m.BatchSize == 0 {
				return fail("PubAck missing count: %+v", m)
			}
			if !gapSeen {
				return inconclusive("expected at least one BatchFlowGap before PubAck (none observed)")
			}
			return pass()
		}
	}
	return fail("timed out waiting for final PubAck (gapSeen=%v)", gapSeen)
}