// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab300Tests covers AB-300: validation of Nats-Batch-Id length,
// sequence presence/order, and unknown batch handling.
func ab300Tests() []harness.Test {
	return []harness.Test{
		{
			ID: "AB-301", Title: "Batch ID accepted at boundary length 64",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB301,
		},
		{
			ID: "AB-302", Title: "Batch ID rejected at length 65",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB302,
		},
		{
			ID: "AB-303", Title: "Sequence missing on initial message",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB303,
		},
		{
			ID: "AB-306", Title: "Sequence gap mid-batch is rejected",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB306,
		},
		{
			ID: "AB-309", Title: "Unknown batch ID is rejected",
			Section: "AB-300", Tags: []string{"ids"}, Run: testAB309,
		},
	}
}

func testAB301(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	id64 := strings.Repeat("x", 64)
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), id64, 1, "1", nil, []byte("y")), 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error != nil {
		return fail("expected success at 64-char id, got %s", ack.Error)
	}
	if ack.BatchID != id64 {
		return fail("ack batch id mismatch")
	}
	return pass()
}

func testAB302(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	id65 := strings.Repeat("x", 65)
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), id65, 1, "1", nil, []byte("y")), 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil || ack.Error.ErrCode != ErrCodeBadID {
		return fail("expected ErrCode %d, got %+v", ErrCodeBadID, ack)
	}
	return pass()
}

func testAB303(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	m := nats.NewMsg(h.Subject("a"))
	m.Header.Set(HdrBatchID, newUUID())
	m.Data = []byte("x")
	ack, err := publishRequest(h, m, 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil || ack.Error.ErrCode != ErrCodeMissingSeq {
		return fail("expected ErrCode %d, got %+v", ErrCodeMissingSeq, ack)
	}
	return pass()
}

func testAB306(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "", nil, []byte("c")), 5*time.Second)
	if err != nil {
		return fail("gap publish: %v", err)
	}
	if ack.Error == nil || ack.Error.ErrCode != ErrCodeIncomplete {
		return fail("expected ErrCode %d on gap, got %+v", ErrCodeIncomplete, ack)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("expected empty stream after rejection, got last seq %d", last)
	}
	waitFor(2*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch {
				return true
			}
		}
		return false
	})
	return pass()
}

func testAB309(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), newUUID(), 5, "", nil, []byte("z")), 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error for unknown batch id, got %+v", ack)
	}
	return pass()
}