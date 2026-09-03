// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb900Tests covers FB-900: the ping operation (op 4).
func fb900Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-901", Title: "Ping resends the latest flow control state", Section: "FB-900", Tags: []string{"ping"}, Run: testFB901},
		{ID: "FB-902", Title: "Ping does NOT advance the batch sequence", Section: "FB-900", Tags: []string{"ping"}, Run: testFB902},
	}
}

func testFB901(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	for i := 2; i <= 15; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}

	// Drain at least one BatchFlowAck, then issue a ping at the
	// highest-sent seq. The server must respond with the latest flow
	// state (a fresh BatchFlowAck) so the client doesn't stall.
	if _, err := handle.awaitFlowAck(5 * time.Second); err != nil {
		return fail("first flow ack: %v", err)
	}

	if err := handle.publishAtSeq(h.Subject("a"), FBOpPing, handle.seq, nil, nil); err != nil {
		return fail("ping: %v", err)
	}

	// Within a generous window we should observe at least one further
	// BatchFlowAck before sending more data.
	deadline := time.Now().Add(5 * time.Second)
	postPingAck := false
	for time.Now().Before(deadline) {
		m, err := handle.readNext(time.Until(deadline))
		if err != nil {
			break
		}
		if m.classify() == "ack" {
			postPingAck = true
			break
		}
	}
	if !postPingAck {
		return inconclusive("server did not respond to ping with a fresh BatchFlowAck within 5s; flow state may already have been current")
	}

	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}
	ack, err := handle.awaitPubAck(10 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil || ack.BatchSize != 16 {
		return fail("commit ack mismatch: %+v", ack)
	}
	return pass()
}

func testFB902(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	for i := 2; i <= 3; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("seq %d: %v", i, err)
		}
	}
	// Three pings, all using the highest-sent seq (3) per ADR.
	for i := 0; i < 3; i++ {
		if err := handle.publishAtSeq(h.Subject("a"), FBOpPing, handle.seq, nil, nil); err != nil {
			return fail("ping %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("4")); err != nil {
		return fail("seq 4: %v", err)
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("5")); err != nil {
		return fail("commit: %v", err)
	}

	// Ensure no gap was emitted (the pings did not register as gaps).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		m, err := handle.readNext(time.Until(deadline))
		if err != nil {
			return fail("read inbox: %v", err)
		}
		switch m.classify() {
		case "gap":
			return fail("ping registered as a gap: %+v", m)
		case "err":
			return fail("unexpected BatchFlowErr: %+v", m)
		case "pubAck":
			if m.BatchSize != 5 {
				return fail("pub ack count=%d, want 5 (pings must not advance seq)", m.BatchSize)
			}
			return pass()
		}
	}
	return fail("timed out waiting for final PubAck")
}