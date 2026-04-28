// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb1100Tests covers FB-1100: per-stream / per-server / per-batch
// limits. All are opt-in via --resource-intensive because they hold
// many batches open or publish thousands of messages.
func fb1100Tests() []harness.Test {
	skip := requiresResourceIntensive()
	return []harness.Test{
		{ID: "FB-1103", Title: "No upper bound on per-batch message count (>1000)", Section: "FB-1100", Tags: []string{"limits", "resource-intensive"}, SkipReason: skip, Run: testFB1103},
	}
}

func testFB1103(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 64, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("1")); err != nil {
		return fail("initial: %v", err)
	}
	for i := 2; i <= 1001; i++ {
		if err := handle.publish(h.Subject("a"), FBOpAppend, nil, []byte("x")); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("end")); err != nil {
		return fail("commit: %v", err)
	}
	ack, err := handle.awaitPubAck(60 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil {
		return fail("pub ack error: %s", ack.Error)
	}
	if ack.BatchSize != 1002 {
		return fail("pub ack count=%d, want 1002", ack.BatchSize)
	}
	return pass()
}