// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab1100Tests covers AB-1100: concurrent non-batch publishes
// interleaving with an open batch.
func ab1100Tests() []harness.Test {
	return []harness.Test{
		{
			ID: "AB-1101", Title: "Non-batch publish to the same subject during an open batch",
			Section: "AB-1100", Tags: []string{"concurrent"}, Run: testAB1101,
		},
		{
			ID: "AB-1102", Title: "Two concurrent batches on the same stream",
			Section: "AB-1100", Tags: []string{"concurrent"}, Run: testAB1102,
		},
	}
}

func testAB1101(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("x"), batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	parResp, err := h.NC.Request(h.Subject("x"), []byte("interleave"), 5*time.Second)
	if err != nil {
		return fail("parallel publish: %v", err)
	}
	var parAck pubAck
	if err := json.Unmarshal(parResp.Data, &parAck); err != nil || parAck.Error != nil {
		return fail("parallel publish ack: data=%q err=%v", string(parResp.Data), err)
	}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("x"), batch, 2, "", nil, []byte("b")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("seq 2 err=%v ack=%+v", err, ack)
	}
	commitAck, err := publishRequest(h, newBatchMsg(h.Subject("x"), batch, 3, "1", nil, []byte("c")), 5*time.Second)
	if err != nil || commitAck.Error != nil {
		return fail("commit err=%v ack=%+v", err, commitAck)
	}
	if commitAck.BatchSize != 3 {
		return fail("commit count=%d, want 3", commitAck.BatchSize)
	}
	if commitAck.Sequence != parAck.Sequence+3 {
		return fail("commit seq=%d, expected parallel seq %d + 3", commitAck.Sequence, parAck.Sequence)
	}
	return pass()
}

func testAB1102(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batchA := newUUID()
	batchB := newUUID()
	subj := h.Subject("a")

	// Open both batches with initials and members.
	for _, batch := range []string{batchA, batchB} {
		for i := 1; i <= 2; i++ {
			if ack, err := publishRequest(h, newBatchMsg(subj, batch, i, "", nil, []byte{byte('a' + i - 1)}), 5*time.Second); err != nil || ack.Error != nil {
				return fail("batch %s seq %d err=%v ack=%+v", batch, i, err, ack)
			}
		}
	}

	// Commit A first, then B.
	commitA, err := publishRequest(h, newBatchMsg(subj, batchA, 3, "1", nil, []byte("c")), 5*time.Second)
	if err != nil || commitA.Error != nil {
		return fail("commit A err=%v ack=%+v", err, commitA)
	}
	if commitA.BatchSize != 3 {
		return fail("commit A count=%d, want 3", commitA.BatchSize)
	}
	commitB, err := publishRequest(h, newBatchMsg(subj, batchB, 3, "1", nil, []byte("c")), 5*time.Second)
	if err != nil || commitB.Error != nil {
		return fail("commit B err=%v ack=%+v", err, commitB)
	}
	if commitB.BatchSize != 3 {
		return fail("commit B count=%d, want 3", commitB.BatchSize)
	}

	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	if len(msgs) != 6 {
		return fail("expected 6 stored, got %d", len(msgs))
	}
	// First three messages should be batch A (committed first), then batch B.
	for i, m := range msgs {
		want := batchA
		if i >= 3 {
			want = batchB
		}
		if got := m.Header.Get(HdrBatchID); got != want {
			return fail("msg %d batch id=%q, want %q", i, got, want)
		}
	}
	return pass()
}