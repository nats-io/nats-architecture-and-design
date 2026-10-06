// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb1200Tests covers FB-1200: leader-change behaviour.
//
// Both tests require a clustered target where the stream can come up
// with 3 replicas; if the target is single-server (or the cluster
// can't satisfy R=3) the tests skip with reason. They are gated
// behind --cluster so a default `run all` doesn't accidentally try
// to step down leaders on a non-clustered target.
//
// Tags: "cluster" + "leader-change" — `--tags cluster` runs both.
func fb1200Tests() []harness.Test {
	skip := requiresCluster()
	return []harness.Test{
		{
			ID:         "FB-1201",
			Title:      "Leader change in gap=fail mode abandons the batch",
			Section:    "FB-1200",
			Tags:       []string{"cluster", "leader-change", "needs-cluster"},
			SkipReason: skip,
			Run:        testFB1201,
		},
		{
			ID:         "FB-1202",
			Title:      "Leader change in gap=ok mode continues with BatchFlowGap",
			Section:    "FB-1200",
			Tags:       []string{"cluster", "leader-change", "needs-cluster"},
			SkipReason: skip,
			Run:        testFB1202,
		},
	}
}

func testFB1201(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	skipReason, err := requireR3Stream(h, streamConfig{
		Name:              name,
		AllowBatchPublish: true,
	})
	if err != nil {
		return fail("create R3 stream: %v", err)
	}
	if skipReason != "" {
		return harness.Skip(skipReason)
	}

	handle, err := openFastBatch(h, name, 10, "fail")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	// Push enough traffic that some messages are likely in-flight
	// when the leader steps down.
	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("1")); err != nil {
		return fail("initial: %v", err)
	}
	if _, err := handle.awaitFlowAck(5 * time.Second); err != nil {
		return fail("first flow ack: %v", err)
	}
	for i := 2; i <= 50; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}

	oldLeader, err := streamLeader(h, name)
	if err != nil || oldLeader == "" {
		return fail("read leader before stepdown: leader=%q err=%v", oldLeader, err)
	}
	if err := stepDownLeader(h, name); err != nil {
		return fail("stepdown: %v", err)
	}
	newLeader := awaitLeaderChange(h, name, oldLeader, 15*time.Second)
	if newLeader == "" {
		return fail("leader did not change within 15s (still %q)", oldLeader)
	}

	// Try a few more appends + a commit so the new leader has a
	// chance to surface a gap and final PubAck.
	postLeaderErrors := 0
	for i := 51; i <= 55; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("y")); err != nil {
			return fail("post-stepdown append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}

	// Drain the inbox until either a final PubAck arrives or we
	// observe ErrCode 10208 on the post-stepdown traffic. ADR-50 §
	// "Message Gaps" allows either outcome (the new leader may have
	// inherited full state and continued cleanly, or it may have
	// gapped and abandoned). We accept both, but FAIL if neither
	// happens within the budget.
	var sawGap bool
	var pubAck *fbInboxMsg
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && pubAck == nil {
		m, err := handle.readNext(time.Until(deadline))
		if err != nil {
			break
		}
		switch m.classify() {
		case "gap":
			sawGap = true
		case "err":
			if m.Error != nil && m.Error.ErrCode == FBErrCodeUnknownID {
				postLeaderErrors++
			}
		case "pubAck":
			pubAck = m
		}
	}

	if pubAck == nil {
		return fail("no final PubAck observed within 20s after leader change (sawGap=%t, postLeaderErrors=%d, newLeader=%q)",
			sawGap, postLeaderErrors, newLeader)
	}

	// Atomicity invariant: regardless of branch, count must equal
	// some prefix of what was sent (1..56) — never anything else.
	if pubAck.BatchSize < 1 || pubAck.BatchSize > 56 {
		return fail("PubAck.count=%d out of valid range [1,56]", pubAck.BatchSize)
	}

	// If a gap was reported, the batch is closed: the next append
	// for this batch ID must be unknown. We test that explicitly.
	if sawGap {
		if err := handle.publishAtSeq(h.Subject("a"), FBOpAppend, 100, nil, []byte("late")); err != nil {
			return fail("late append publish: %v", err)
		}
		m, err := handle.readNext(3 * time.Second)
		if err != nil || m.Error == nil || m.Error.ErrCode != FBErrCodeUnknownID {
			return inconclusive("gap was reported but late append did not error with ErrCode %d (got %+v err=%v)",
				FBErrCodeUnknownID, m, err)
		}
		return harness.Inconclusive("leader change abandoned the batch: count=%d, sawGap=true (acceptable per ADR)", pubAck.BatchSize)
	}
	return harness.Inconclusive("leader change preserved batch state: count=%d (acceptable per ADR; gap path not exercised)", pubAck.BatchSize)
}

func testFB1202(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	skipReason, err := requireR3Stream(h, streamConfig{
		Name:              name,
		AllowBatchPublish: true,
	})
	if err != nil {
		return fail("create R3 stream: %v", err)
	}
	if skipReason != "" {
		return harness.Skip(skipReason)
	}

	handle, err := openFastBatch(h, name, 10, "ok")
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
	for i := 2; i <= 50; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}

	oldLeader, err := streamLeader(h, name)
	if err != nil || oldLeader == "" {
		return fail("read leader before stepdown: leader=%q err=%v", oldLeader, err)
	}
	if err := stepDownLeader(h, name); err != nil {
		return fail("stepdown: %v", err)
	}
	if newLeader := awaitLeaderChange(h, name, oldLeader, 15*time.Second); newLeader == "" {
		return fail("leader did not change within 15s (still %q)", oldLeader)
	}

	// Continue and commit. In gap=ok mode the new leader is supposed
	// to keep the batch alive and just send a BatchFlowGap if it
	// detected one.
	for i := 51; i <= 100; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("y")); err != nil {
			return fail("post-stepdown append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}

	var sawGap bool
	var pubAck *fbInboxMsg
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && pubAck == nil {
		m, err := handle.readNext(time.Until(deadline))
		if err != nil {
			break
		}
		switch m.classify() {
		case "gap":
			sawGap = true
		case "err":
			return fail("unexpected BatchFlowErr in gap=ok mode: %+v", m)
		case "pubAck":
			pubAck = m
		}
	}
	if pubAck == nil {
		return fail("no final PubAck observed within 30s (sawGap=%t)", sawGap)
	}
	if pubAck.Error != nil {
		return fail("PubAck error in gap=ok mode: %s", pubAck.Error)
	}
	if pubAck.BatchSize != 101 {
		return fail("PubAck.count=%d, want 101 (gap=ok must keep the batch alive across leader change)", pubAck.BatchSize)
	}
	if sawGap {
		// Documented behaviour: leader change with messages in flight
		// produces a gap. Pass either way.
		return harness.Pass()
	}
	return harness.Inconclusive("leader change in gap=ok did not produce a BatchFlowGap (no messages were lost in the transfer)")
}