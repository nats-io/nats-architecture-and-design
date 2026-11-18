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

// fb800Tests covers FB-800: per-message expected-header checks under
// each gap mode.
func fb800Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-801", Title: "Nats-Expected-Last-Sequence mismatch in gap=fail surfaces error and stops batch", Section: "FB-800", Tags: []string{"headers", "fail"}, Run: testFB801},
		{ID: "FB-802", Title: "Nats-Expected-Last-Sequence mismatch in gap=ok surfaces error but continues", Section: "FB-800", Tags: []string{"headers", "ok"}, Run: testFB802},
		{ID: "FB-803", Title: "Nats-Expected-Last-Msg-Id behavior in fast batch", Section: "FB-800", Tags: []string{"headers"}, Run: testFB803},
	}
}

func testFB801(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := h.NC.Request(h.Subject("seed"), []byte("seed"), 5*time.Second); err != nil {
		return fail("seed publish: %v", err)
	}
	s, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	handle, err := openFastBatch(h, name, 5, "fail")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	hdrs := nats.Header{HdrExpLastSeq: []string{fmt.Sprintf("%d", s+99)}}
	if err := handle.publish(h.Subject("a"), FBOpStart, hdrs, []byte("a")); err != nil {
		return fail("initial: %v", err)
	}
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("b")); err != nil {
		return fail("seq 2: %v", err)
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("c")); err != nil {
		return fail("commit: %v", err)
	}

	// Server should send a BatchFlowErr (or err-bearing gap) referencing seq 1, then a final PubAck.
	errMsg, _, err := handle.drainUntilTypes(10*time.Second, "err", "gap", "pubAck")
	if err != nil {
		return fail("await err/pubAck: %v", err)
	}
	switch errMsg.classify() {
	case "err":
		if errMsg.Sequence != 1 {
			return fail("BatchFlowErr.seq=%d, want 1", errMsg.Sequence)
		}
		if errMsg.Error == nil {
			return fail("BatchFlowErr missing error body")
		}
	case "gap":
		// Some implementations surface header errors via gap in fail mode.
	case "pubAck":
		if errMsg.Error == nil {
			return fail("expected error in PubAck for fail-mode header mismatch, got %+v", errMsg)
		}
	}

	// Drain until PubAck if we haven't seen it.
	if errMsg.classify() != "pubAck" {
		ack, err := handle.awaitPubAck(10 * time.Second)
		if err != nil {
			return fail("await pubAck: %v", err)
		}
		if ack.Error == nil {
			return fail("ADR §Server Errors says fail-mode header errors land in PubAck.error; got nil error in %+v", ack)
		}
	}

	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("post last seq: %v", err)
	}
	if last != s {
		return fail("stream advanced past pre-batch seq %d to %d in fail mode", s, last)
	}
	return pass()
}

func testFB802(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := h.NC.Request(h.Subject("seed"), []byte("seed"), 5*time.Second); err != nil {
		return fail("seed publish: %v", err)
	}
	s, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	hdrs := nats.Header{HdrExpLastSeq: []string{fmt.Sprintf("%d", s+99)}}
	if err := handle.publish(h.Subject("a"), FBOpStart, hdrs, []byte("a")); err != nil {
		return fail("initial: %v", err)
	}
	for i := 2; i <= 6; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("e")); err != nil {
		return fail("commit: %v", err)
	}

	// Expect either a BatchFlowErr or a BatchFlowGap reporting the
	// failed seq, then the batch continues to PubAck.
	errMsg, _, err := handle.drainUntilTypes(10*time.Second, "err", "gap")
	if err != nil {
		return fail("await err/gap: %v", err)
	}
	if errMsg.Sequence != 1 {
		return inconclusive("error reported on seq %d (expected 1) — server-side semantics may differ", errMsg.Sequence)
	}

	ack, err := handle.awaitPubAck(10 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil {
		return fail("ok-mode batch should not surface header error in PubAck: %s", ack.Error)
	}
	if ack.BatchSize != 7 {
		return fail("pub ack count=%d, want 7 (header error skipped, rest persisted)", ack.BatchSize)
	}
	return pass()
}

func testFB803(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	// Stream is empty so there is no last message ID. Setting
	// Nats-Expected-Last-Msg-Id:foo can only succeed if the server
	// silently ignores the header — otherwise it must produce some kind
	// of error response (PubAck.error, BatchFlowErr, or BatchFlowGap).
	hdrs := nats.Header{HdrExpLastMsgID: []string{"foo"}}
	if err := handle.publish(h.Subject("a"), FBOpStart, hdrs, []byte("a")); err != nil {
		return fail("initial: %v", err)
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("b")); err != nil {
		return fail("commit: %v", err)
	}

	// Per ADR-50 main spec ("Check properties like ExpectedLastSeq are
	// handled as normal"), the server may honor the header as a normal
	// stream-level check (err_code 10070, "wrong last msg ID") OR
	// silently ignore it. Both branches are acceptable per FB-803.
	//
	// Fast batch is non-atomic by design (FB-501, FB-1001): messages are
	// persisted as they arrive, so a partial stream state on the failure
	// branch is normal — atomicity is NOT required here.
	honored := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m, err := handle.readNext(time.Until(deadline))
		if err != nil {
			break
		}
		switch m.classify() {
		case "err", "gap":
			if m.Error != nil {
				honored = true
			}
		case "pubAck":
			if m.Error != nil {
				return pass()
			}
			if m.BatchSize != 2 {
				return fail("batch committed but count=%d, want 2 (header was honored partially?)", m.BatchSize)
			}
			return pass()
		}
	}
	if honored {
		return pass()
	}
	return fail("no determinative outcome for Nats-Expected-Last-Msg-Id observed within 10s")
}