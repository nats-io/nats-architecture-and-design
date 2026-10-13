// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"strings"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb400Tests covers FB-400: reply-subject and operation validation.
func fb400Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-401", Title: "Unknown operation value", Section: "FB-400", Tags: []string{"validation"}, Run: testFB401},
		{ID: "FB-402", Title: "Invalid gap value", Section: "FB-400", Tags: []string{"validation"}, Run: testFB402},
		{ID: "FB-403", Title: "Batch ID accepted at boundary length 64", Section: "FB-400", Tags: []string{"validation"}, Run: testFB403},
		{ID: "FB-404", Title: "Batch ID rejected at length 65", Section: "FB-400", Tags: []string{"validation"}, Run: testFB404},
		{ID: "FB-405", Title: "Append for unknown batch ID", Section: "FB-400", Tags: []string{"validation"}, Run: testFB405},
		{ID: "FB-406", Title: "Malformed reply subject (missing $FI)", Section: "FB-400", Tags: []string{"validation"}, Run: testFB406},
	}
}

func testFB401(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	// op=9 is undefined.
	if err := handle.publishAtSeq(h.Subject("a"), 9, 1, nil, []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	m, err := handle.readNext(5 * time.Second)
	if err != nil {
		return fail("read inbox: %v", err)
	}
	if m.Error == nil || m.Error.ErrCode != FBErrCodeBadPattern {
		return fail("expected ErrCode %d, got %+v", FBErrCodeBadPattern, m)
	}
	return pass()
}

func testFB402(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	// Bypass the handle's normal reply-subject construction so we can
	// inject "maybe" into the gap field.
	reply := handle.inboxPrefix + "." + handle.batchID + ".10.maybe.1.0." + FBSentinel
	if err := handle.publishWithRawReply(h.Subject("a"), reply, nil, []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	m, err := handle.readNext(5 * time.Second)
	if err != nil {
		return fail("read inbox: %v", err)
	}
	if m.Error == nil || m.Error.ErrCode != FBErrCodeBadPattern {
		return fail("expected ErrCode %d, got %+v", FBErrCodeBadPattern, m)
	}
	return pass()
}

func testFB403(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	id64 := strings.Repeat("x", 64)
	handle, err := openFastBatchWithID(h, name, id64, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("y")); err != nil {
		return fail("publish: %v", err)
	}
	ack, err := handle.awaitPubAck(5 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil {
		return fail("expected success at 64-char id, got %s", ack.Error)
	}
	if ack.BatchID != id64 {
		return fail("ack batch id mismatch")
	}
	return pass()
}

func testFB404(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	id65 := strings.Repeat("x", 65)
	handle, err := openFastBatchWithID(h, name, id65, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("y")); err != nil {
		return fail("publish: %v", err)
	}
	m, err := handle.readNext(5 * time.Second)
	if err != nil {
		return fail("read inbox: %v", err)
	}
	if m.Error == nil || m.Error.ErrCode != FBErrCodeBadID {
		return fail("expected ErrCode %d, got %+v", FBErrCodeBadID, m)
	}
	return pass()
}

func testFB405(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	// Send an append at seq 5 without a prior op-0 / start.
	if err := handle.publishAtSeq(h.Subject("a"), FBOpAppend, 5, nil, []byte("z")); err != nil {
		return fail("publish: %v", err)
	}
	m, err := handle.readNext(5 * time.Second)
	if err != nil {
		return fail("read inbox: %v", err)
	}
	if m.Error == nil || m.Error.ErrCode != FBErrCodeUnknownID {
		return fail("expected ErrCode %d, got %+v", FBErrCodeUnknownID, m)
	}
	return pass()
}

func testFB406(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	// Reply subject without the $FI sentinel.
	reply := handle.inboxPrefix + "." + handle.batchID + ".10.ok.1.0"
	if err := handle.publishWithRawReply(h.Subject("a"), reply, nil, []byte("x")); err != nil {
		return fail("publish: %v", err)
	}

	// Acceptable outcomes per ADR-50 FB-406:
	//   * BatchFlowErr with ErrCode 10206
	//   * Treated as a non-batch publish (ordinary PubAck)
	//   * Silent drop (no reply)
	// MUST NOT: a fast batch is started.
	m, mErr := handle.readNext(5 * time.Second)
	if mErr == nil {
		switch {
		case m.Error != nil && m.Error.ErrCode == FBErrCodeBadPattern:
			// Branch (a): explicit error reply.
		case m.classify() == "pubAck" && m.Error == nil && m.BatchID == "" && m.BatchSize == 0:
			// Branch (b): treated as a normal non-batch publish.
		default:
			return fail("unexpected response to malformed reply subject: %+v", m)
		}
	}

	// MUST: no fast batch was started. Probe with an append at seq=2 on
	// the same batch_id; server must report unknown-batch.
	if err := handle.publishAtSeq(h.Subject("a"), FBOpAppend, 2, nil, []byte("probe")); err != nil {
		return fail("probe publish: %v", err)
	}
	probe, err := handle.readNext(5 * time.Second)
	if err != nil {
		return fail("read probe reply: %v", err)
	}
	if probe.Error == nil || probe.Error.ErrCode != FBErrCodeUnknownID {
		return fail("malformed reply subject implicitly started a fast batch — append at seq=2 returned %+v, expected ErrCode %d", probe, FBErrCodeUnknownID)
	}
	return pass()
}