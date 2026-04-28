// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb1000Tests covers FB-1000: idle abandonment.
//
// Per ADR-50 Fast-ingest §"Server Behavior Design", an idle batch is
// abandoned silently after 10s; no advisory is raised (advisories are
// only emitted by atomic batch publishing).
func fb1000Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-1001", Title: "Idle batch is abandoned after 10s", Section: "FB-1000", Tags: []string{"idle", "slow"}, SkipReason: requiresSlow(), Run: testFB1001},
	}
}

func testFB1001(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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

	// Wait out the >10s idle window so the server abandons the batch
	// session. Fast batch raises no advisory on abandonment, so the
	// observable effect is that subsequent appends are rejected as
	// unknown-ID.
	time.Sleep(12 * time.Second)

	if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("late")); err != nil {
		return fail("post-timeout publish: %v", err)
	}
	m, err := handle.readNext(5 * time.Second)
	if err != nil {
		return fail("read inbox: %v", err)
	}
	if m.Error == nil || m.Error.ErrCode != FBErrCodeUnknownID {
		return fail("expected ErrCode %d after timeout, got %+v", FBErrCodeUnknownID, m)
	}
	// Fast batch is NOT staged: any message that received a
	// BatchFlowAck before the idle period is already in the stream.
	// "Abandonment" ends the batch session (so later appends fail with
	// unknown-ID), but does not roll back already-persisted messages.
	// The initial seq=1 message acked above must still be present.
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 1 {
		return fail("expected the pre-timeout initial message to remain (last seq=1), got last seq %d", last)
	}
	return pass()
}
