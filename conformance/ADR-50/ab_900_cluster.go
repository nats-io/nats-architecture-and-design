// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab900Tests covers AB-900: leader-change behaviour. R3 stream needed,
// gated by --cluster (predicate is shared with ADR-50-FB).
func ab900Tests() []harness.Test {
	skip := requiresCluster()
	return []harness.Test{
		{
			ID: "AB-901", Title: "Atomic batch survives leader change before commit",
			Section: "AB-900", Tags: []string{"cluster"},
			SkipReason: skip, Run: testAB901,
		},
		{
			ID: "AB-902", Title: "Atomic batch atomicity under leader change after commit",
			Section: "AB-900", Tags: []string{"cluster"},
			SkipReason: skip, Run: testAB902,
		},
	}
}

func testAB901(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	skipReason, err := requireR3Stream(h, streamConfig{Name: name, AllowAtomicPublish: true})
	if err != nil {
		return fail("stream create (R=3): %v", err)
	}
	if skipReason != "" {
		return skip("%s", skipReason)
	}
	get, cancel := captureAdvisories(h)
	defer cancel()
	batch := newUUID()
	for i := 1; i <= 5; i++ {
		ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, i, "", nil, []byte{byte('a' + i - 1)}), 5*time.Second)
		if err != nil {
			return fail("seq %d: %v", i, err)
		}
		if ack.Error != nil {
			return fail("seq %d unexpectedly errored: %s", i, ack.Error)
		}
	}
	oldLeader, _ := streamLeader(h, name)
	if err := stepDownLeader(h, name); err != nil {
		return fail("step down leader: %v", err)
	}
	if newLeader := awaitLeaderChange(h, name, oldLeader, 10*time.Second); newLeader == "" {
		return fail("no new leader elected within 10s")
	}

	commitAck, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 6, "1", nil, []byte("z")), 10*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if commitAck.Error != nil {
		// Branch A: batch was abandoned across the leader change. Stream
		// must contain zero batch members; advisory may have fired.
		if last != 0 {
			return fail("commit failed but stream has %d messages — atomicity violated", last)
		}
		_ = get // advisory presence is acceptable but not required
		return inconclusive("batch abandoned across leader change: %s", commitAck.Error)
	}
	// Branch B: new leader inherited state, commit succeeded with count=6.
	if commitAck.BatchSize != 6 {
		return fail("commit succeeded but count=%d (want 6) — partial batch is forbidden", commitAck.BatchSize)
	}
	if last < 6 {
		return fail("commit succeeded with count=6 but stream has only %d messages", last)
	}
	return pass()
}

func testAB902(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	skipReason, err := requireR3Stream(h, streamConfig{Name: name, AllowAtomicPublish: true})
	if err != nil {
		return fail("stream create (R=3): %v", err)
	}
	if skipReason != "" {
		return skip("%s", skipReason)
	}
	batch := newUUID()
	for i := 1; i <= 4; i++ {
		ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, i, "", nil, []byte{byte('a' + i - 1)}), 5*time.Second)
		if err != nil || ack.Error != nil {
			return fail("seq %d err=%v ack=%+v", i, err, ack)
		}
	}
	commitAck, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 5, "1", nil, []byte("e")), 5*time.Second)
	if err != nil || commitAck.Error != nil {
		return fail("commit err=%v ack=%+v", err, commitAck)
	}
	oldLeader, _ := streamLeader(h, name)
	if err := stepDownLeader(h, name); err != nil {
		return fail("step down leader: %v", err)
	}
	if newLeader := awaitLeaderChange(h, name, oldLeader, 10*time.Second); newLeader == "" {
		return fail("no new leader elected within 10s")
	}

	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("post-leader-change last seq: %v", err)
	}
	if last != commitAck.Sequence {
		return fail("post-leader-change last seq=%d != committed seq %d — durable batch lost messages", last, commitAck.Sequence)
	}
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	if len(msgs) != 5 {
		return fail("expected 5 stored messages after leader change, got %d", len(msgs))
	}
	return pass()
}
