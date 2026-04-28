// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb500Tests covers FB-500: gap detection in gap=ok mode. Gaps are
// reported via BatchFlowGap and the batch continues.
func fb500Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-501", Title: "Gap reported via BatchFlowGap; batch continues", Section: "FB-500", Tags: []string{"gap", "ok"}, Run: testFB501},
		{ID: "FB-503", Title: "BatchFlowGap carries no flow update (msgs absent)", Section: "FB-500", Tags: []string{"gap", "ok"}, Run: testFB503},
	}
}

func testFB501(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("2")); err != nil {
		return fail("seq 2: %v", err)
	}
	// Skip seq 3 — bump local counter and append at seq 4.
	handle.seq++
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("4")); err != nil {
		return fail("seq 4 (gap-trigger): %v", err)
	}
	for i := 5; i <= 9; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}

	gap, _, err := handle.drainUntilTypes(10*time.Second, "gap")
	if err != nil {
		return fail("await gap: %v", err)
	}
	if gap.Sequence < 4 {
		return fail("gap.seq=%d, want >=4", gap.Sequence)
	}
	if gap.LastSeq >= gap.Sequence {
		return fail("gap.last_seq=%d should be < gap.seq=%d", gap.LastSeq, gap.Sequence)
	}

	ack, err := handle.awaitPubAck(10 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil {
		return fail("pub ack error: %s", ack.Error)
	}
	if ack.BatchSize != 10 {
		return fail("pub ack count=%d, want 10", ack.BatchSize)
	}
	return pass()
}

func testFB503(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	handle.seq++ // skip seq 2
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("3")); err != nil {
		return fail("seq 3 (gap-trigger): %v", err)
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}
	gap, _, err := handle.drainUntilTypes(10*time.Second, "gap")
	if err != nil {
		return fail("await gap: %v", err)
	}
	// ADR §"Message Gaps": "these messages don't contain any flow
	// updates or information." So Messages MUST be the JSON zero value
	// (i.e. the field was absent or 0).
	if gap.Messages != 0 {
		return fail("BatchFlowGap carried msgs=%d; ADR forbids flow updates in gap messages", gap.Messages)
	}
	if _, err := handle.awaitPubAck(10 * time.Second); err != nil {
		return fail("await pubAck: %v", err)
	}
	return pass()
}