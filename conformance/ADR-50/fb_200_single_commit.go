// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb200Tests covers FB-200: the special-case single-message commit
// shortcut where seq 1 + op 2/3 returns a normal PubAck.
func fb200Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-201", Title: "Op 2 with batch_seq=1 returns a normal PubAck", Section: "FB-200", Tags: []string{"single-commit"}, Run: testFB201},
		{ID: "FB-202", Title: "Op 3 with batch_seq=1 (single EOB)", Section: "FB-200", Tags: []string{"single-commit"}, Run: testFB202},
	}
}

func testFB201(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("data")); err != nil {
		return fail("publish: %v", err)
	}
	m, err := handle.readNext(5 * time.Second)
	if err != nil {
		return fail("read inbox: %v", err)
	}
	// ADR §"Server Errors": single-message immediate commit returns a
	// PubAck directly (no preceding BatchFlowAck).
	if m.classify() != "pubAck" {
		return fail("expected PubAck on single-message commit, got %s (%+v)", m.classify(), m)
	}
	if m.Error != nil {
		return fail("pub ack error: %s", m.Error)
	}
	if m.BatchID != handle.batchID {
		return fail("pub ack batch=%q, want %q", m.BatchID, handle.batchID)
	}
	if m.BatchSize != 1 {
		return fail("pub ack count=%d, want 1", m.BatchSize)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if m.Sequence != last {
		return fail("pub ack seq=%d != stream last seq %d", m.Sequence, last)
	}
	return pass()
}

func testFB202(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	if err := handle.publish(h.Subject("a"), FBOpCommitEOB, nil, []byte("never-stored")); err != nil {
		return fail("publish: %v", err)
	}
	m, err := handle.readNext(5 * time.Second)
	if err != nil {
		return fail("read inbox: %v", err)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}

	// Per ADR-50 FB-202 both branches are acceptable:
	//   - successful PubAck with count=0 (EOB doesn't count) and zero stored
	//   - error reply (initial-EOB treated as invalid)
	// MUST NOT: silently store the EOB sentinel.
	if last != 0 {
		return fail("server stored the single-EOB message (last seq=%d)", last)
	}
	if m.Error != nil {
		return pass()
	}
	if m.classify() != "pubAck" {
		return fail("unexpected response shape for single-message EOB: %+v", m)
	}
	if m.BatchSize != 0 {
		return fail("pub ack count=%d, want 0 (EOB sentinel does not count toward BatchSize)", m.BatchSize)
	}
	return pass()
}