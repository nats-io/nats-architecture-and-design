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

// ab1200Tests covers AB-1200: payload and header preservation edge cases.
func ab1200Tests() []harness.Test {
	return []harness.Test{
		{
			ID: "AB-1201", Title: "Empty payload allowed",
			Section: "AB-1200", Tags: []string{"payload"}, Run: testAB1201,
		},
		{
			ID: "AB-1202", Title: "Non-batch headers preserved across the batch",
			Section: "AB-1200", Tags: []string{"payload"}, Run: testAB1202,
		},
		{
			ID: "AB-1203", Title: "Nats-Batch-Commit:1 only honored with a Nats-Batch-Id",
			Section: "AB-1200", Tags: []string{"payload"}, Run: testAB1203,
		},
	}
}

func testAB1201(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, nil), 5*time.Second); err != nil || ack.Error != nil {
		return fail("seq 1 err=%v ack=%+v", err, ack)
	}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "1", nil, nil), 5*time.Second); err != nil || ack.Error != nil || ack.BatchSize != 2 {
		return fail("commit err=%v ack=%+v", err, ack)
	}
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	if len(msgs) != 2 {
		return fail("expected 2 stored, got %d", len(msgs))
	}
	for i, m := range msgs {
		if len(m.Data) != 0 {
			return fail("msg %d non-empty payload: %q", i, m.Data)
		}
	}
	return pass()
}

func testAB1202(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	for i := 1; i <= 2; i++ {
		commit := ""
		if i == 2 {
			commit = "1"
		}
		hdrs := nats.Header{"X-Test-Tag": []string{fmt.Sprintf("value-%d", i)}}
		if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, i, commit, hdrs, []byte{byte('a' + i - 1)}), 5*time.Second); err != nil || ack.Error != nil {
			return fail("seq %d err=%v ack=%+v", i, err, ack)
		}
	}
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	for i, m := range msgs {
		want := fmt.Sprintf("value-%d", i+1)
		if got := m.Header.Get("X-Test-Tag"); got != want {
			return fail("msg %d tag=%q want %q", i, got, want)
		}
	}
	return pass()
}

func testAB1203(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	m := nats.NewMsg(h.Subject("a"))
	m.Header.Set(HdrBatchCommit, "1")
	m.Data = []byte("rogue")
	ack, err := publishRequest(h, m, 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil && (ack.BatchID != "" || ack.BatchSize != 0) {
		return fail("server treated rogue Nats-Batch-Commit as batch commit: %+v", ack)
	}
	return pass()
}