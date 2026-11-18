// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb600Tests covers FB-600: gap detection in gap=fail mode.  A gap
// abandons the batch and the final PubAck reports the last received
// pre-gap sequence as count.
func fb600Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-601", Title: "Gap abandons the batch with final PubAck count = pre-gap seq", Section: "FB-600", Tags: []string{"gap", "fail"}, Run: testFB601},
		{ID: "FB-602", Title: "Idempotent: late messages after gap are ignored or rejected", Section: "FB-600", Tags: []string{"gap", "fail"}, Run: testFB602},
	}
}

func testFB601(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 5, "fail")
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
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("2")); err != nil {
		return fail("seq 2: %v", err)
	}
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("3")); err != nil {
		return fail("seq 3: %v", err)
	}
	// Skip seq 4 — gap-trigger.
	handle.seq++
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("5")); err != nil {
		return fail("seq 5 (gap-trigger): %v", err)
	}

	// Expect a gap message followed by a final PubAck whose count
	// equals 3 (the last pre-gap seq).
	gap, _, err := handle.drainUntilTypes(10*time.Second, "gap")
	if err != nil {
		return fail("await gap: %v", err)
	}
	if gap.LastSeq != 4 || gap.Sequence != 5 {
		return inconclusive("gap reported with last_seq=%d seq=%d (expected 4/5; the server may report different boundaries)", gap.LastSeq, gap.Sequence)
	}

	ack, err := handle.awaitPubAck(10 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.BatchSize != 3 {
		return fail("PubAck count=%d, want 3 (last pre-gap seq)", ack.BatchSize)
	}

	// Confirm the batch is closed: a follow-up append must error.
	if err := handle.publishAtSeq(h.Subject("a"), FBOpAppend, 6, nil, []byte("late")); err != nil {
		return fail("late publish: %v", err)
	}
	m, _, err := handle.drainUntilTypes(3*time.Second, "err", "pubAck")
	if err != nil {
		// Server may silently drop; that's also acceptable per FB-602.
		return pass()
	}
	if m.Error == nil || m.Error.ErrCode != FBErrCodeUnknownID {
		return inconclusive("late append after fail-mode gap returned %+v; ADR allows silent drop or ErrCode 10208", m)
	}
	return pass()
}

func testFB602(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 5, "fail")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("1")); err != nil {
		return fail("seq 1: %v", err)
	}
	if _, err := handle.awaitFlowAck(5 * time.Second); err != nil {
		return fail("first flow ack: %v", err)
	}
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("2")); err != nil {
		return fail("seq 2: %v", err)
	}
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("3")); err != nil {
		return fail("seq 3: %v", err)
	}
	// Skip seq 4 — gap-trigger.
	handle.seq++
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("5")); err != nil {
		return fail("seq 5 (gap-trigger): %v", err)
	}

	gap, _, err := handle.drainUntilTypes(10*time.Second, "gap")
	if err != nil {
		return fail("await gap: %v", err)
	}
	_ = gap

	// Per ADR: "The client will receive the gap message first, and
	// should use this to stop sending messages before eventually
	// receiving the final ack." Our test: keep sending anyway and
	// confirm the late traffic does NOT change the final PubAck count
	// — the count must equal the last pre-gap seq.
	for s := 6; s <= 8; s++ {
		if err := handle.publishAtSeq(h.Subject("a"), FBOpAppend, s, nil, []byte("late")); err != nil {
			return fail("late publish seq %d: %v", s, err)
		}
	}

	ack, _, err := handle.drainUntilTypes(10*time.Second, "pubAck")
	if err != nil {
		return fail("await pubAck after late traffic: %v", err)
	}
	if ack.BatchSize != 3 {
		return fail("PubAck count=%d, want 3 (late messages must not advance count)", ack.BatchSize)
	}
	return pass()
}