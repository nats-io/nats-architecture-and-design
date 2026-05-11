// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"fmt"
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
			ID: "AB-802", Title: "Idle abandonment without a reply produces no error reply",
			Section: "AB-800", Tags: []string{"idle", "slow"}, Run: testAB802,
		},
		{
			ID: "AB-803", Title: "Per-stream concurrent batch limit (50)",
			Section: "AB-800", Tags: []string{"limits"}, Run: testAB803,
		},
		{
			ID: "AB-804", Title: "Per-server concurrent batch limit (1000)",
			Section: "AB-800", Tags: []string{"limits", "resource-intensive"},
			SkipReason: requiresResourceIntensive(), Run: testAB804,
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

func testAB802(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	// Member 2 fire-and-forget — no reply.
	if err := publishFireAndForget(h, newBatchMsg(h.Subject("a"), batch, 2, "", nil, []byte("b"))); err != nil {
		return fail("fire-and-forget: %v", err)
	}
	if err := h.NC.FlushTimeout(5 * time.Second); err != nil {
		return fail("flush: %v", err)
	}

	// Wait out the idle window. We must NOT receive an error reply for
	// either step (no reply was set on member 2).
	got := waitFor(15*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch && a.Reason == "timeout" {
				return true
			}
		}
		return false
	})
	if !got {
		return fail("did not observe batch_abandoned advisory with reason=timeout")
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("expected empty stream after timeout, got last=%d", last)
	}
	return pass()
}

func testAB803(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	const target = 50
	batches := make([]string, 0, target)
	for i := 0; i < target; i++ {
		batch := newUUID()
		ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("x")), 10*time.Second)
		if err != nil {
			return fail("open batch %d: %v", i, err)
		}
		if ack.Error != nil {
			return fail("open batch %d unexpectedly rejected: %s", i, ack.Error)
		}
		batches = append(batches, batch)
	}

	// 51st batch must be rejected.
	extraBatch := newUUID()
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), extraBatch, 1, "", nil, []byte("over")), 10*time.Second)
	if err != nil {
		return fail("51st publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack on 51st batch, got %+v", ack)
	}

	// Cleanup: commit one of the in-flight batches so the 51st can succeed.
	if commit, err := publishRequest(h, newBatchMsg(h.Subject("a"), batches[0], 2, "1", nil, []byte("y")), 5*time.Second); err != nil || commit.Error != nil {
		return fail("cleanup commit: err=%v ack=%+v", err, commit)
	}
	retryBatch := newUUID()
	retry, err := publishRequest(h, newBatchMsg(h.Subject("a"), retryBatch, 1, "1", nil, []byte("ok")), 5*time.Second)
	if err != nil {
		return fail("retry publish: %v", err)
	}
	if retry.Error != nil {
		return fail("retry should succeed after a batch was committed: %s", retry.Error)
	}
	return pass()
}

func testAB804(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// 21 streams × 50 batches = 1050 capacity. Each stream listens on a
	// disjoint subject so publishes route deterministically.
	const totalBatches = 1000
	const perStream = 50
	streamCount := (totalBatches + perStream - 1) / perStream

	type sx struct {
		name    string
		subject string
	}
	streams := make([]sx, 0, streamCount)
	for i := 0; i < streamCount; i++ {
		nm := h.MintStreamName("AB_804_" + itoa(i))
		subj := fmt.Sprintf("%s.s%d", h.SubjectPrefix(), i)
		if err := createStream(h, streamConfig{
			Name:               nm,
			Subjects:           []string{subj},
			AllowAtomicPublish: true,
		}); err != nil {
			return fail("stream create %d: %v", i, err)
		}
		streams = append(streams, sx{name: nm, subject: subj})
	}

	opened := 0
	for _, s := range streams {
		for i := 0; i < perStream && opened < totalBatches; i++ {
			batch := newUUID()
			ack, err := publishRequest(h, newBatchMsg(s.subject, batch, 1, "", nil, []byte("x")), 10*time.Second)
			if err != nil {
				return fail("open batch on %s: %v", s.name, err)
			}
			if ack.Error != nil {
				return fail("unexpected reject opening batch %d on %s: %s", opened, s.name, ack.Error)
			}
			opened++
		}
	}

	// 1001st batch on any stream must be rejected.
	ack, err := publishRequest(h, newBatchMsg(streams[0].subject, newUUID(), 1, "", nil, []byte("over")), 10*time.Second)
	if err != nil {
		return fail("1001st publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error on 1001st batch, got %+v", ack)
	}
	return pass()
}