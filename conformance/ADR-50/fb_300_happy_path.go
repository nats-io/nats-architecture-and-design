// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb300Tests covers FB-300: multi-message happy path — establishes the
// batch, observes BatchFlowAck shape, commits.
func fb300Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-301", Title: "Establishes batch, observes BatchFlowAck, commits with PubAck", Section: "FB-300", Tags: []string{"happy-path"}, Run: testFB301},
		{ID: "FB-302", Title: "Multi-message ending in EOB", Section: "FB-300", Tags: []string{"happy-path"}, Run: testFB302},
		{ID: "FB-303", Title: "BatchFlowAck.seq is non-decreasing across the batch", Section: "FB-300", Tags: []string{"happy-path"}, Run: testFB303},
	}
}

func testFB301(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 100, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("1")); err != nil {
		return fail("initial publish: %v", err)
	}
	first, err := handle.awaitFlowAck(5 * time.Second)
	if err != nil {
		return fail("first flow ack: %v", err)
	}
	// The establishment ack's primary job is to convey the allowed
	// flow rate (msgs). Per the BatchFlowAck spec ("Sequence is the
	// sequence of the message that triggered the ack ... messages up
	// to and including Sequence were persisted"), the initial ack may
	// legitimately report seq=0 if the server composes the ack before
	// the initial message has been persisted, or seq=1 if it has.
	if first.Sequence > 1 {
		return fail("first flow ack seq=%d, want 0 or 1", first.Sequence)
	}
	if first.Messages == 0 || int(first.Messages) > 100 {
		return fail("first flow ack msgs=%d, want 1..100", first.Messages)
	}

	for i := 2; i <= 50; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit publish: %v", err)
	}
	ack, err := handle.awaitPubAck(10 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil {
		return fail("pub ack error: %s", ack.Error)
	}
	if ack.BatchSize != 51 {
		return fail("pub ack count=%d, want 51", ack.BatchSize)
	}
	if ack.BatchID != handle.batchID {
		return fail("pub ack batch=%q, want %q", ack.BatchID, handle.batchID)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if ack.Sequence != last {
		return fail("pub ack seq=%d != stream last seq %d", ack.Sequence, last)
	}
	return pass()
}

func testFB302(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	if _, err := handle.awaitFlowAck(5 * time.Second); err != nil {
		return fail("first flow ack: %v", err)
	}
	for i := 2; i <= 9; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitEOB, nil, []byte("never-stored")); err != nil {
		return fail("commit eob: %v", err)
	}
	ack, err := handle.awaitPubAck(10 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil {
		return fail("pub ack error: %s", ack.Error)
	}
	// Per the ADR-50 clarification, the EOB sentinel does NOT count
	// toward BatchSize: 9 stored + 1 EOB → count == 9.
	if ack.BatchSize != 9 {
		return fail("pub ack count=%d, want 9 (EOB sentinel does not count toward BatchSize)", ack.BatchSize)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 9 {
		return fail("expected 9 stored (EOB sentinel not stored), last seq is %d", last)
	}
	return pass()
}

func testFB303(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 20, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("1")); err != nil {
		return fail("initial: %v", err)
	}
	for i := 2; i <= 99; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}

	var lastSeq uint64
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m, err := handle.readNext(time.Until(deadline))
		if err != nil {
			return fail("read inbox: %v", err)
		}
		switch m.classify() {
		case "ack":
			if m.Sequence < lastSeq {
				return fail("BatchFlowAck.seq decreased: %d after %d", m.Sequence, lastSeq)
			}
			lastSeq = m.Sequence
		case "pubAck":
			if m.Error != nil {
				return fail("pub ack error: %s", m.Error)
			}
			if m.BatchSize != 100 {
				return fail("pub ack count=%d, want 100", m.BatchSize)
			}
			return pass()
		case "gap", "err":
			return fail("unexpected %s message: %+v", m.classify(), m)
		}
	}
	return fail("timed out waiting for final PubAck")
}