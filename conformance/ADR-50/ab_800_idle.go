// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab800Tests covers AB-800: idle abandonment — batches without traffic
// for 10s are abandoned with an advisory; traffic resets the timer.
func ab800Tests() []harness.Test {
	return []harness.Test{
		{
			ID: "AB-801", Title: "Idle batch is abandoned after 10s with an advisory",
			Section: "AB-800", Tags: []string{"idle", "slow"}, Run: testAB801,
		},
		{
			ID: "AB-805", Title: "Idle timeout resets on traffic",
			Section: "AB-800", Tags: []string{"idle", "slow"}, Run: testAB805,
		},
	}
}

func testAB801(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	got := waitFor(15*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch && a.Reason == "timeout" {
				return true
			}
		}
		return false
	})
	if !got {
		return fail("did not observe batch_abandoned reason=timeout within 15s")
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "", nil, []byte("b")), 5*time.Second)
	if err != nil {
		return fail("post-timeout publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error after batch was abandoned, got %+v", ack)
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

func testAB805(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	for i := 2; i <= 4; i++ {
		time.Sleep(5 * time.Second)
		if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, i, "", nil, []byte{byte('a' + i - 1)}), 5*time.Second); err != nil || ack.Error != nil {
			return fail("seq %d err=%v ack=%+v", i, err, ack)
		}
	}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 5, "1", nil, []byte("e")), 5*time.Second); err != nil || ack.Error != nil || ack.BatchSize != 5 {
		return fail("commit err=%v ack=%+v", err, ack)
	}
	for _, a := range get() {
		if a.BatchID == batch && a.Reason == "timeout" {
			return fail("unexpected timeout abandonment for an active batch")
		}
	}
	return pass()
}