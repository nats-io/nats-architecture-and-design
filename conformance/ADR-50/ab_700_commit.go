// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab700Tests covers AB-700: commit semantics — Nats-Batch-Commit:1
// stores the final message, :eob commits without storing, unknown
// values are rejected.
func ab700Tests() []harness.Test {
	eobSkip := requiresFlag("eob", "EOB tests disabled (--eob=false)")
	return []harness.Test{
		{
			ID: "AB-701", Title: "Nats-Batch-Commit:1 finalizes with the final message stored",
			Section: "AB-700", Tags: []string{"commit"}, Run: testAB701,
		},
		{
			ID: "AB-702", Title: "Nats-Batch-Commit:eob finalizes without storing the final message",
			Section: "AB-700", Tags: []string{"commit", "api-level-4"},
			SkipReason: eobSkip, Run: testAB702,
		},
		{
			ID: "AB-704", Title: "Nats-Batch-Commit with an unknown value",
			Section: "AB-700", Tags: []string{"commit"}, Run: testAB704,
		},
	}
}

func testAB701(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	for i := 1; i <= 2; i++ {
		if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, i, "", nil, []byte{byte('a' + i - 1)}), 5*time.Second); err != nil || ack.Error != nil {
			return fail("seq %d err=%v ack=%+v", i, err, ack)
		}
	}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "1", nil, []byte("c")), 5*time.Second); err != nil || ack.Error != nil || ack.BatchSize != 3 {
		return fail("commit err=%v ack=%+v", err, ack)
	}
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	if len(msgs) != 3 {
		return fail("expected 3 stored, got %d", len(msgs))
	}
	if msgs[2].Header.Get(HdrBatchCommit) != "1" {
		return fail("last stored msg missing Nats-Batch-Commit:1, got %q", msgs[2].Header.Get(HdrBatchCommit))
	}
	return pass()
}

func testAB702(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	for i := 1; i <= 2; i++ {
		if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, i, "", nil, []byte{byte('a' + i - 1)}), 5*time.Second); err != nil || ack.Error != nil {
			return fail("seq %d err=%v ack=%+v", i, err, ack)
		}
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "eob", nil, []byte("ignored")), 5*time.Second)
	if err != nil {
		return fail("eob commit: %v", err)
	}
	// Per the ADR-50 clarification, the EOB sentinel does NOT count
	// toward BatchSize: 2 stored + 1 EOB → count == 2.
	if ack.Error != nil || ack.BatchSize != 2 {
		return fail("eob commit ack mismatch: %+v (want count=2, EOB excluded)", ack)
	}
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	if len(msgs) != 2 {
		return fail("expected 2 stored (EOB sentinel not stored), got %d", len(msgs))
	}
	if msgs[1].Header.Get(HdrBatchCommit) != "1" {
		return fail("server should mark last stored msg with Nats-Batch-Commit:1, got %q", msgs[1].Header.Get(HdrBatchCommit))
	}
	if ack.Sequence != msgs[1].Sequence {
		return fail("ack.seq=%d != stored last seq %d", ack.Sequence, msgs[1].Sequence)
	}
	return pass()
}

func testAB704(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial: err=%v ack=%+v", err, ack)
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "nope", nil, []byte("b")), 5*time.Second)
	if err != nil {
		return fail("bad commit: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error for unknown commit value, got %+v", ack)
	}
	return pass()
}