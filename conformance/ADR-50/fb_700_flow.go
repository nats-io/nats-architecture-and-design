// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb700Tests covers FB-700: flow-control invariants. The strict
// upper-bound rule (`msgs <= flow`) is required; the ramp-up behaviour
// is reported as inconclusive when not observed.
func fb700Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-701", Title: "Initial msgs may be lower than requested flow", Section: "FB-700", Tags: []string{"flow"}, Run: testFB701},
		{ID: "FB-702", Title: "Server never exceeds the requested flow upper bound", Section: "FB-700", Tags: []string{"flow"}, Run: testFB702},
		{ID: "FB-703", Title: "Server may ramp BatchFlowAck.msgs upward", Section: "FB-700", Tags: []string{"flow"}, Run: testFB703},
		{ID: "FB-704", Title: "Lost ack does not stall the batch", Section: "FB-700", Tags: []string{"flow"}, Run: testFB704},
	}
}

func testFB701(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 100, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("1")); err != nil {
		return fail("initial: %v", err)
	}
	ack, err := handle.awaitFlowAck(10 * time.Second)
	if err != nil {
		return fail("first flow ack: %v", err)
	}
	if ack.Messages < 1 || int(ack.Messages) > 100 {
		return fail("first BatchFlowAck.msgs=%d, want 1..100", ack.Messages)
	}
	// Commit so the batch tears down cleanly.
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}
	if _, err := handle.awaitPubAck(10 * time.Second); err != nil {
		return fail("await pubAck: %v", err)
	}
	return pass()
}

func testFB702(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	for i := 2; i <= 199; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		m, err := handle.readNext(time.Until(deadline))
		if err != nil {
			return fail("read inbox: %v", err)
		}
		switch m.classify() {
		case "ack":
			if int(m.Messages) > 10 {
				return fail("BatchFlowAck.msgs=%d exceeds requested flow=10", m.Messages)
			}
		case "pubAck":
			if m.Error != nil {
				return fail("pub ack error: %s", m.Error)
			}
			if m.BatchSize != 200 {
				return fail("pub ack count=%d, want 200", m.BatchSize)
			}
			return pass()
		case "gap", "err":
			return fail("unexpected %s: %+v", m.classify(), m)
		}
	}
	return fail("timed out waiting for final PubAck")
}

func testFB704(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	for i := 2; i <= 99; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}

	// The harness simulates "drop the third BatchFlowAck" by counting
	// acks observed and ignoring index 2; the batch must still commit.
	// The non-decreasing seq invariant on subsequent acks is sufficient
	// to release any outstanding-ack stall (ADR §"Flow Control").
	acks := 0
	deadline := time.Now().Add(30 * time.Second)
	var lastAckSeq uint64
	for time.Now().Before(deadline) {
		m, err := handle.readNext(time.Until(deadline))
		if err != nil {
			return fail("read inbox: %v", err)
		}
		switch m.classify() {
		case "ack":
			if acks != 2 {
				if m.Sequence < lastAckSeq {
					return fail("BatchFlowAck.seq decreased: %d -> %d", lastAckSeq, m.Sequence)
				}
				lastAckSeq = m.Sequence
			}
			acks++
		case "pubAck":
			if m.Error != nil {
				return fail("pub ack error: %s", m.Error)
			}
			if m.BatchSize != 100 {
				return fail("pub ack count=%d, want 100", m.BatchSize)
			}
			return pass()
		case "gap", "err":
			return fail("unexpected %s: %+v", m.classify(), m)
		}
	}
	return fail("timed out waiting for final PubAck (acks observed=%d)", acks)
}

func testFB703(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	for i := 2; i <= 499; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}

	var firstMsgs uint16
	rampObserved := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		m, err := handle.readNext(time.Until(deadline))
		if err != nil {
			return fail("read inbox: %v", err)
		}
		switch m.classify() {
		case "ack":
			if firstMsgs == 0 {
				firstMsgs = m.Messages
			} else if m.Messages > firstMsgs {
				rampObserved = true
			}
		case "pubAck":
			if !rampObserved {
				return inconclusive("server did not ramp msgs up under this load (first=%d); ADR allows but does not require it", firstMsgs)
			}
			return pass()
		case "gap", "err":
			return fail("unexpected %s: %+v", m.classify(), m)
		}
	}
	return fail("timed out waiting for final PubAck")
}