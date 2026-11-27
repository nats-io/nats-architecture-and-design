// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr31

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// dg400Tests covers DG-400: batched Direct Get requests — sequence
// chains via Nats-Last-Sequence, EOB sentinels, max_bytes bounds, old
// server detection.
func dg400Tests() []harness.Test {
	return []harness.Test{
		{ID: "DG-401", Title: "Basic batch returns up to N messages followed by EOB", Section: "DG-400", Tags: []string{"batch", "api-level-1"}, Run: testDG401},
		{ID: "DG-402", Title: "Batch with start_time filters by timestamp", Section: "DG-400", Tags: []string{"batch", "api-level-1"}, Run: testDG402},
		{ID: "DG-403", Title: "Batch respects max_bytes", Section: "DG-400", Tags: []string{"batch", "api-level-1"}, Run: testDG403},
		{ID: "DG-404", Title: "Batch is exhausted when fewer messages match than requested", Section: "DG-400", Tags: []string{"batch", "api-level-1"}, Run: testDG404},
		{ID: "DG-405", Title: "Batch sequence chain via Nats-Last-Sequence", Section: "DG-400", Tags: []string{"batch", "api-level-1"}, Run: testDG405},
		{ID: "DG-406", Title: "Old server detection via missing Nats-Num-Pending", Section: "DG-400", Tags: []string{"batch", "api-level-1"}, Run: testDG406},
		{ID: "DG-407", Title: "batch:0 is treated as a non-batch Get", Section: "DG-400", Tags: []string{"batch", "api-level-1"}, Run: testDG407},
	}
}

// publishN inserts n messages on subj with payloads "p1".."pn", returns
// each assigned sequence in order.
func publishN(h *harness.Harness, subj string, n int) ([]uint64, error) {
	out := make([]uint64, 0, n)
	for i := 1; i <= n; i++ {
		seq, err := publishMsg(h, subj, []byte{byte('a' + i - 1)}, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, seq)
	}
	return out, nil
}

func testDG401(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("batched Direct Get requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("a")
	seqs, err := publishN(h, subj, 5)
	if err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{Batch: 3, Seq: 1, NextFor: subj}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) != 4 {
		return fail("expected 4 replies (3 msgs + EOB), got %d (%s)", len(replies), summary(replies))
	}
	for i := 0; i < 3; i++ {
		if !replies[i].IsSuccess() {
			return fail("reply %d not success: %s", i, summary(replies))
		}
		gotSeq, _ := replies[i].HeaderSeq()
		if gotSeq != seqs[i] {
			return fail("reply %d seq got %d want %d", i, gotSeq, seqs[i])
		}
	}
	for i := 1; i < 3; i++ {
		last, _ := replies[i].HeaderUint(HdrLastSeq)
		prev, _ := replies[i-1].HeaderSeq()
		if last != prev {
			return fail("reply[%d].Nats-Last-Sequence=%d, want %d", i, last, prev)
		}
	}
	eob := replies[3]
	if !eob.IsEOB() {
		return fail("expected EOB sentinel, got status=%q", eob.Status)
	}
	if eob.Description != DescriptionEOB {
		return fail("EOB description got %q want %q", eob.Description, DescriptionEOB)
	}
	if len(eob.Data) != 0 {
		return fail("EOB has %d bytes payload, want 0", len(eob.Data))
	}
	pending, ok := eob.HeaderUint(HdrNumPending)
	if !ok || pending != 2 {
		return fail("EOB Nats-Num-Pending got %d ok=%v want 2", pending, ok)
	}
	lastSeq, _ := eob.HeaderUint(HdrLastSeq)
	expectedLast, _ := replies[2].HeaderSeq()
	if lastSeq != expectedLast {
		return fail("EOB Nats-Last-Sequence got %d want %d", lastSeq, expectedLast)
	}
	return pass()
}

func testDG402(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("start_time + batch requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("a")
	for i := 1; i <= 2; i++ {
		if _, err := publishMsg(h, subj, []byte{byte('a' + i - 1)}, nil); err != nil {
			return fail("publish %d: %v", i, err)
		}
		time.Sleep(150 * time.Millisecond)
	}
	tBefore3 := time.Now().UTC()
	time.Sleep(50 * time.Millisecond)
	s3, err := publishMsg(h, subj, []byte("c"), nil)
	if err != nil {
		return fail("publish 3: %v", err)
	}
	for i := 4; i <= 5; i++ {
		time.Sleep(150 * time.Millisecond)
		if _, err := publishMsg(h, subj, []byte{byte('a' + i - 1)}, nil); err != nil {
			return fail("publish %d: %v", i, err)
		}
	}

	replies, err := directGet(h, name, directGetReq{Batch: 5, StartTime: &tBefore3, NextFor: subj}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) < 4 || !replies[len(replies)-1].IsEOB() {
		return fail("expected at least 3 msgs + EOB, got %s", summary(replies))
	}
	first := replies[0]
	if !first.IsSuccess() {
		return fail("first reply not success: %s", summary(replies))
	}
	gotSeq, _ := first.HeaderSeq()
	if gotSeq < s3 {
		return fail("first matched seq %d below cutoff %d", gotSeq, s3)
	}
	return pass()
}

func testDG403(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("max_bytes + batch requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("a")
	payload := make([]byte, 100)
	for i := range payload {
		payload[i] = 'x'
	}
	for i := 0; i < 10; i++ {
		if _, err := publishMsg(h, subj, payload, nil); err != nil {
			return fail("publish %d: %v", i, err)
		}
	}
	replies, err := directGet(h, name, directGetReq{Batch: 10, MaxBytes: 250, Seq: 1, NextFor: subj}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) < 1 || !replies[len(replies)-1].IsEOB() {
		return fail("expected EOB sentinel, got %s", summary(replies))
	}
	msgCount := 0
	totalBytes := 0
	for _, r := range replies {
		if r.IsSuccess() {
			msgCount++
			totalBytes += len(r.Data)
		}
	}
	if msgCount > 2 {
		return fail("max_bytes=250 should bound to <=2 messages of 100 bytes each, got %d", msgCount)
	}
	if totalBytes > 250 {
		return fail("total payload bytes %d exceeds max_bytes 250", totalBytes)
	}
	pending, _ := replies[len(replies)-1].HeaderUint(HdrNumPending)
	if pending == 0 {
		return fail("expected Nats-Num-Pending > 0 after partial batch, got 0")
	}
	return pass()
}

func testDG404(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("batched Direct Get requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("a")
	if _, err := publishN(h, subj, 2); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{Batch: 10, Seq: 1, NextFor: subj}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) != 3 {
		return fail("expected 2 msgs + EOB, got %d (%s)", len(replies), summary(replies))
	}
	if !replies[2].IsEOB() {
		return fail("third reply not EOB: %s", summary(replies))
	}
	pending, ok := replies[2].HeaderUint(HdrNumPending)
	if !ok {
		return fail("EOB missing Nats-Num-Pending: %s", summary(replies))
	}
	if pending != 0 {
		return fail("expected Nats-Num-Pending=0 after exhausted batch, got %d", pending)
	}
	return pass()
}

func testDG405(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("batched Direct Get requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("a")
	if _, err := publishN(h, subj, 5); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{Batch: 5, Seq: 1, NextFor: subj}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) != 6 {
		return fail("expected 5 msgs + EOB, got %d (%s)", len(replies), summary(replies))
	}
	for i := 1; i < 5; i++ {
		last, _ := replies[i].HeaderUint(HdrLastSeq)
		prev, _ := replies[i-1].HeaderSeq()
		if last != prev {
			return fail("reply[%d].Nats-Last-Sequence=%d, want %d", i, last, prev)
		}
	}
	eob := replies[5]
	if !eob.IsEOB() {
		return fail("final reply not EOB: %s", summary(replies))
	}
	last, _ := eob.HeaderUint(HdrLastSeq)
	lastMsgSeq, _ := replies[4].HeaderSeq()
	if last != lastMsgSeq {
		return fail("EOB Nats-Last-Sequence=%d, want %d", last, lastMsgSeq)
	}
	return pass()
}

func testDG406(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("batched Direct Get requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("a")
	if _, err := publishN(h, subj, 3); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{Batch: 3, Seq: 1, NextFor: subj}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) < 1 || !replies[0].IsSuccess() {
		return fail("expected first reply success, got %s", summary(replies))
	}
	if _, ok := replies[0].HeaderUint(HdrNumPending); !ok {
		return fail("first reply missing Nats-Num-Pending — server appears not to support batched Direct Get")
	}
	return pass()
}

func testDG407(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("batched Direct Get requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	// Per ADR-31 rev 7: batch:0 is treated as a non-batch single-message
	// Get. Exactly one success reply, no EOB, no Nats-Num-Pending.
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("a")
	seqs, err := publishN(h, subj, 2)
	if err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{Batch: 0, Seq: 1, NextFor: subj}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) != 1 {
		return fail("expected exactly 1 reply for batch:0, got %d (%s)", len(replies), summary(replies))
	}
	r := replies[0]
	if !r.IsSuccess() {
		return fail("expected success reply, got status=%q (%s)", r.Status, summary(replies))
	}
	gotSeq, _ := r.HeaderSeq()
	if gotSeq != seqs[0] {
		return fail("non-batch Get should return first matching message (seq %d), got seq %d", seqs[0], gotSeq)
	}
	if _, ok := r.HeaderUint(HdrNumPending); ok {
		return fail("batch:0 reply must not carry Nats-Num-Pending (non-batch Get); got headers=%v", r.Headers)
	}
	return pass()
}
