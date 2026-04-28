// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb1400Tests covers FB-1400: PubAck shape (batch / count fields).
func fb1400Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-1401", Title: "PubAck.batch and PubAck.count populated correctly", Section: "FB-1400", Tags: []string{"puback"}, Run: testFB1401},
	}
}

func testFB1401(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	for i := 2; i <= 6; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}
	ack, err := handle.awaitPubAck(10 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil {
		return fail("pub ack error: %s", ack.Error)
	}
	if ack.BatchID != handle.batchID {
		return fail("PubAck.batch=%q, want %q", ack.BatchID, handle.batchID)
	}
	if ack.BatchSize != 7 {
		return fail("PubAck.count=%d, want 7", ack.BatchSize)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if ack.Sequence != last {
		return fail("PubAck.seq=%d != stream last seq %d", ack.Sequence, last)
	}
	return pass()
}