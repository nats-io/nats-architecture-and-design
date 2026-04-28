// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab200Tests covers AB-200: the happy path for single- and
// multi-message batches plus pub-ack shape assertions.
func ab200Tests() []harness.Test {
	return []harness.Test{
		{
			ID:      "AB-201",
			Title:   "Minimal store-commit batch (one message)",
			Section: "AB-200",
			Tags:    []string{"happy-path"},
			Run:     testAB201,
		},
		{
			ID:         "AB-202",
			Title:      "Minimal eob-commit batch",
			Section:    "AB-200",
			Tags:       []string{"happy-path", "api-level-4"},
			SkipReason: requiresFlag("eob", "EOB tests disabled (--eob=false)"),
			Run:        testAB202,
		},
		{
			ID:      "AB-203",
			Title:   "Multi-message store-commit batch",
			Section: "AB-200",
			Tags:    []string{"happy-path"},
			Run:     testAB203,
		},
		{
			ID:      "AB-204",
			Title:   "Pub ack seq references the final stored message",
			Section: "AB-200",
			Tags:    []string{"happy-path"},
			Run:     testAB204,
		},
	}
}

func testAB201(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "1", nil, []byte("payload")), 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error != nil {
		return fail("unexpected error: %s", ack.Error)
	}
	if ack.BatchID != batch || ack.BatchSize != 1 {
		return fail("ack batch fields wrong: %+v", ack)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if ack.Sequence != last {
		return fail("ack.seq=%d != last seq=%d", ack.Sequence, last)
	}
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list msgs: %v", err)
	}
	if len(msgs) != 1 {
		return fail("expected 1 stored msg, got %d", len(msgs))
	}
	hdrs := msgs[0].Header
	if hdrs.Get(HdrBatchID) != batch {
		return fail("stored Nats-Batch-Id mismatch: %q", hdrs.Get(HdrBatchID))
	}
	if hdrs.Get(HdrBatchSequence) != "1" {
		return fail("stored batch sequence mismatch: %q", hdrs.Get(HdrBatchSequence))
	}
	if hdrs.Get(HdrBatchCommit) != "1" {
		return fail("stored Nats-Batch-Commit not set to 1: %q", hdrs.Get(HdrBatchCommit))
	}
	return pass()
}

func testAB202(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("data")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial publish err=%v ack=%+v", err, ack)
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("eob"), batch, 2, "eob", nil, []byte("ignored")), 5*time.Second)
	if err != nil {
		return fail("eob publish: %v", err)
	}
	// Per the ADR-50 clarification, the EOB sentinel does NOT count
	// toward BatchSize: 1 stored message + 1 EOB → count == 1.
	if ack.Error != nil || ack.BatchID != batch || ack.BatchSize != 1 {
		return fail("eob ack mismatch: %+v (want count=1, EOB excluded)", ack)
	}
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list msgs: %v", err)
	}
	if len(msgs) != 1 {
		return fail("expected 1 stored msg (EOB sentinel not stored), got %d", len(msgs))
	}
	stored := msgs[0]
	if stored.Header.Get(HdrBatchSequence) != "1" {
		return fail("stored batch seq mismatch: %q", stored.Header.Get(HdrBatchSequence))
	}
	if stored.Header.Get(HdrBatchCommit) != "1" {
		return fail("stored Nats-Batch-Commit not rewritten to 1: %q", stored.Header.Get(HdrBatchCommit))
	}
	return pass()
}

func testAB203(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("seq 1: err=%v ack=%+v", err, ack)
	}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "", nil, []byte("b")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("seq 2: err=%v ack=%+v", err, ack)
	}
	if err := publishFireAndForget(h, newBatchMsg(h.Subject("a"), batch, 3, "", nil, []byte("c"))); err != nil {
		return fail("seq 3 publish: %v", err)
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 4, "1", nil, []byte("d")), 5*time.Second)
	if err != nil {
		return fail("commit publish: %v", err)
	}
	if ack.Error != nil || ack.BatchID != batch || ack.BatchSize != 4 {
		return fail("commit ack mismatch: %+v", ack)
	}
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	if len(msgs) != 4 {
		return fail("expected 4 stored msgs, got %d", len(msgs))
	}
	want := []string{"a", "b", "c", "d"}
	for i, m := range msgs {
		if string(m.Data) != want[i] {
			return fail("msg %d payload=%q want %q", i, m.Data, want[i])
		}
		if m.Header.Get(HdrBatchID) != batch {
			return fail("msg %d batch id mismatch", i)
		}
		commit := m.Header.Get(HdrBatchCommit)
		if i == len(msgs)-1 {
			if commit != "1" {
				return fail("last msg should have Nats-Batch-Commit:1, got %q", commit)
			}
		} else if commit != "" {
			return fail("msg %d should not carry Nats-Batch-Commit, got %q", i, commit)
		}
	}
	return pass()
}

func testAB204(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := h.NC.Request(h.Subject("warm"), []byte("warm"), 5*time.Second); err != nil {
		return fail("warmup publish: %v", err)
	}
	s0, err := streamLastSeq(h, name)
	if err != nil {
		return fail("read s0: %v", err)
	}
	batch := newUUID()
	for i := 1; i <= 4; i++ {
		if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, i, "", nil, []byte{byte('A' + i - 1)}), 5*time.Second); err != nil || ack.Error != nil {
			return fail("seq %d err=%v ack=%+v", i, err, ack)
		}
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 5, "1", nil, []byte("Z")), 5*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	if ack.Sequence != s0+5 {
		return fail("expected ack.seq=%d (s0=%d + 5), got %d", s0+5, s0, ack.Sequence)
	}
	return pass()
}