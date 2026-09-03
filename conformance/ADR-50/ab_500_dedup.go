// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab500Tests covers AB-500: within-batch deduplication via
// Nats-Msg-Id. Server >= 2.12.1.
func ab500Tests() []harness.Test {
	skip := requiresFlag("dedup", "dedup tests disabled (--dedup=false)")
	return []harness.Test{
		{
			ID: "AB-501", Title: "Unique Nats-Msg-Id across a batch is accepted",
			Section: "AB-500", Tags: []string{"dedup"},
			SkipReason: skip, Run: testAB501,
		},
		{
			ID: "AB-502", Title: "Duplicate Nats-Msg-Id within a batch is rejected",
			Section: "AB-500", Tags: []string{"dedup"},
			SkipReason: skip, Run: testAB502,
		},
		{
			ID: "AB-503", Title: "Nats-Msg-Id colliding with an existing stream message",
			Section: "AB-500", Tags: []string{"dedup"},
			SkipReason: skip, Run: testAB503,
		},
	}
}

func testAB501(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	for i := 1; i <= 3; i++ {
		hdrs := nats.Header{HdrMsgID: []string{fmt.Sprintf("m%d", i)}}
		commit := ""
		if i == 3 {
			commit = "1"
		}
		ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, i, commit, hdrs, []byte{byte('a' + i - 1)}), 5*time.Second)
		if err != nil {
			return fail("seq %d: %v", i, err)
		}
		if ack.Error != nil {
			return fail("seq %d ack error: %s", i, ack.Error)
		}
	}
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	if len(msgs) != 3 {
		return fail("expected 3 stored, got %d", len(msgs))
	}
	for i, m := range msgs {
		if got := m.Header.Get(HdrMsgID); got != fmt.Sprintf("m%d", i+1) {
			return fail("msg %d Nats-Msg-Id mismatch: %q", i, got)
		}
	}
	return pass()
}

func testAB502(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	hdrs := nats.Header{HdrMsgID: []string{"dup"}}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", hdrs, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial: err=%v ack=%+v", err, ack)
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "", hdrs, []byte("b")), 5*time.Second)
	if err != nil {
		return fail("dup publish: %v", err)
	}
	if ack.Error == nil {
		// Server may surface dup at commit time; try committing and check.
		commitAck, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "1", nil, []byte("c")), 5*time.Second)
		if err != nil {
			return fail("commit: %v", err)
		}
		if commitAck.Error == nil || commitAck.Error.ErrCode != ErrCodeDuplicate {
			return fail("expected ErrCode %d (within-batch dup), neither member nor commit reported it: %+v / %+v", ErrCodeDuplicate, ack, commitAck)
		}
	} else if ack.Error.ErrCode != ErrCodeDuplicate {
		return fail("expected ErrCode %d, got %+v", ErrCodeDuplicate, ack)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("batch should be abandoned, but stream last seq is %d", last)
	}
	return pass()
}

func testAB503(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	// Pre-publish a non-batch message with Nats-Msg-Id:keep.
	pre := nats.NewMsg(h.Subject("a"))
	pre.Header.Set(HdrMsgID, "keep")
	pre.Data = []byte("seed")
	if _, err := h.NC.RequestMsg(pre, 5*time.Second); err != nil {
		return fail("seed publish: %v", err)
	}
	preLast, err := streamLastSeq(h, name)
	if err != nil {
		return fail("seed last seq: %v", err)
	}

	batch := newUUID()
	hdrs := nats.Header{HdrMsgID: []string{"keep"}}
	if _, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", hdrs, []byte("a")), 5*time.Second); err != nil {
		return fail("initial publish: %v", err)
	}
	if _, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "", nil, []byte("b")), 5*time.Second); err != nil {
		return fail("seq 2 publish: %v", err)
	}
	commitAck, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "1", nil, []byte("c")), 5*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	// Server may flag this as a duplicate (commit error or commit ack
	// with duplicate flag set) — either way the batch must not persist
	// new members.
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("post last seq: %v", err)
	}
	if last != preLast {
		return fail("stream advanced past seed (was %d, now %d) — batch should have been rejected as duplicate", preLast, last)
	}
	if commitAck.Error == nil && !commitAck.Duplicate {
		return inconclusive("commit succeeded without error or duplicate flag (server may not surface dedup against pre-batch state at commit time)")
	}
	return pass()
}