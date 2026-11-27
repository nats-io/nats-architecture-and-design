// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab300Tests covers AB-300: validation of Nats-Batch-Id length,
// sequence presence/order, and unknown batch handling.
func ab300Tests() []harness.Test {
	return []harness.Test{
		{
			ID: "AB-301", Title: "Batch ID accepted at boundary length 64",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB301,
		},
		{
			ID: "AB-302", Title: "Batch ID rejected at length 65",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB302,
		},
		{
			ID: "AB-303", Title: "Sequence missing on initial message",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB303,
		},
		{
			ID: "AB-304", Title: "Sequence missing on a member message",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB304,
		},
		{
			ID: "AB-305", Title: "Initial message must be sequence 1",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB305,
		},
		{
			ID: "AB-306", Title: "Sequence gap mid-batch is rejected",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB306,
		},
		{
			ID: "AB-307", Title: "Repeated sequence is rejected",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB307,
		},
		{
			ID: "AB-308", Title: "Decreasing sequence is rejected",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB308,
		},
		{
			ID: "AB-309", Title: "Unknown batch ID is rejected",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB309,
		},
		{
			ID: "AB-310", Title: "Sequence exceeds server limit (1000)",
			Section: "AB-300", Tags: []string{"ids", "resource-intensive"},
			SkipReason: requiresResourceIntensive(), Run: testAB310,
		},
		{
			ID: "AB-311", Title: "Sequence exactly at limit (1000)",
			Section: "AB-300", Tags: []string{"ids", "resource-intensive"},
			SkipReason: requiresResourceIntensive(), Run: testAB311,
		},
	}
}

func testAB301(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	id64 := strings.Repeat("x", 64)
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), id64, 1, "1", nil, []byte("y")), 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error != nil {
		return fail("expected success at 64-char id, got %s", ack.Error)
	}
	if ack.BatchID != id64 {
		return fail("ack batch id mismatch")
	}
	return pass()
}

func testAB302(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	id65 := strings.Repeat("x", 65)
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), id65, 1, "1", nil, []byte("y")), 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil || ack.Error.ErrCode != ErrCodeBadID {
		return fail("expected ErrCode %d, got %+v", ErrCodeBadID, ack)
	}
	return pass()
}

func testAB303(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	m := nats.NewMsg(h.Subject("a"))
	m.Header.Set(HdrBatchID, newUUID())
	m.Data = []byte("x")
	ack, err := publishRequest(h, m, 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil || ack.Error.ErrCode != ErrCodeMissingSeq {
		return fail("expected ErrCode %d, got %+v", ErrCodeMissingSeq, ack)
	}
	return pass()
}

func testAB306(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	get, cancel := captureAdvisories(h)
	defer cancel()
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial: err=%v ack=%+v", err, ack)
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "", nil, []byte("c")), 5*time.Second)
	if err != nil {
		return fail("gap publish: %v", err)
	}
	if ack.Error == nil || ack.Error.ErrCode != ErrCodeIncomplete {
		return fail("expected ErrCode %d on gap, got %+v", ErrCodeIncomplete, ack)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("expected empty stream after rejection, got last seq %d", last)
	}
	waitFor(2*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch {
				return true
			}
		}
		return false
	})
	return pass()
}

func testAB304(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	get, cancel := captureAdvisories(h)
	defer cancel()
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	// Member with Nats-Batch-Id but no Nats-Batch-Sequence.
	m := nats.NewMsg(h.Subject("a"))
	m.Header.Set(HdrBatchID, batch)
	m.Data = []byte("b")
	ack, err := publishRequest(h, m, 5*time.Second)
	if err != nil {
		return fail("member publish: %v", err)
	}
	if ack.Error == nil || ack.Error.ErrCode != ErrCodeMissingSeq {
		return fail("expected ErrCode %d on missing sequence, got %+v", ErrCodeMissingSeq, ack)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("expected empty stream after rejection, got last seq %d", last)
	}
	waitFor(3*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch {
				return true
			}
		}
		return false
	})
	return pass()
}

func testAB305(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	// First message at sequence 2 (no prior batch state) — server must reject.
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), newUUID(), 2, "", nil, []byte("x")), 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack publishing seq=2 as the first message, got %+v", ack)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("seq=2 first-message attempt persisted (last=%d)", last)
	}
	return pass()
}

func testAB307(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("a-dup")), 5*time.Second)
	if err != nil {
		return fail("duplicate-seq publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error on repeated sequence, got %+v", ack)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("expected no stored messages after repeated-seq rejection, got last=%d", last)
	}
	return pass()
}

func testAB308(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	// Step 2: publish seq 3 (gap)
	ack3, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "", nil, []byte("c")), 5*time.Second)
	if err != nil {
		return fail("seq 3 publish: %v", err)
	}
	// Step 3: publish seq 2 (decreasing)
	ack2, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "", nil, []byte("b")), 5*time.Second)
	if err != nil {
		return fail("seq 2 publish: %v", err)
	}
	if ack3.Error == nil && ack2.Error == nil {
		return fail("neither out-of-order publish was rejected: ack3=%+v ack2=%+v", ack3, ack2)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("expected no stored messages after rejection, got last=%d", last)
	}
	return pass()
}

func testAB310(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	get, cancel := captureAdvisories(h)
	defer cancel()
	batch := newUUID()
	subject := h.Subject("a")
	if ack, err := publishRequest(h, newBatchMsg(subject, batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	for seq := 2; seq <= 1000; seq++ {
		if err := publishFireAndForget(h, newBatchMsg(subject, batch, seq, "", nil, []byte("x"))); err != nil {
			return fail("member seq=%d: %v", seq, err)
		}
	}
	if err := h.NC.FlushTimeout(5 * time.Second); err != nil {
		return fail("flush: %v", err)
	}
	ack, err := publishRequest(h, newBatchMsg(subject, batch, 1001, "", nil, []byte("over")), 10*time.Second)
	if err != nil {
		return fail("1001st publish: %v", err)
	}
	if ack.Error == nil || ack.Error.ErrCode != ErrCodeSeqLimit {
		return fail("expected ErrCode %d at seq=1001, got %+v", ErrCodeSeqLimit, ack)
	}
	got := waitFor(10*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch && a.Reason == "large" {
				return true
			}
		}
		return false
	})
	if !got {
		return fail("did not observe stream_batch_abandoned with reason=large")
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("expected empty stream, got last seq %d", last)
	}
	return pass()
}

func testAB311(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	subject := h.Subject("a")
	if ack, err := publishRequest(h, newBatchMsg(subject, batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	for seq := 2; seq <= 999; seq++ {
		if err := publishFireAndForget(h, newBatchMsg(subject, batch, seq, "", nil, []byte("x"))); err != nil {
			return fail("member seq=%d: %v", seq, err)
		}
	}
	if err := h.NC.FlushTimeout(5 * time.Second); err != nil {
		return fail("flush: %v", err)
	}
	ack, err := publishRequest(h, newBatchMsg(subject, batch, 1000, "1", nil, []byte("z")), 30*time.Second)
	if err != nil {
		return fail("commit at seq=1000: %v", err)
	}
	if ack.Error != nil || ack.BatchSize != 1000 {
		return fail("commit ack mismatch: %+v", ack)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 1000 {
		return fail("expected stream last seq=1000, got %d", last)
	}
	return pass()
}

func testAB309(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), newUUID(), 5, "", nil, []byte("z")), 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error for unknown batch id, got %+v", ack)
	}
	return pass()
}