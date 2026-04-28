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