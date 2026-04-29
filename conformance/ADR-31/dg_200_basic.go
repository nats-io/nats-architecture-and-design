// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr31

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// dg200Tests covers DG-200: the basic Direct Get API request payloads
// (`seq`, `last_by_subj`, `next_by_subj`, `start_time`) and the 404/408
// error sentinels.
func dg200Tests() []harness.Test {
	return []harness.Test{
		{ID: "DG-201", Title: "seq returns the message at that sequence", Section: "DG-200", Tags: []string{"basic"}, Run: testDG201},
		{ID: "DG-202", Title: "last_by_subj returns the most recent message for a subject", Section: "DG-200", Tags: []string{"basic"}, Run: testDG202},
		{ID: "DG-203", Title: "next_by_subj returns the first matching message", Section: "DG-200", Tags: []string{"basic"}, Run: testDG203},
		{ID: "DG-204", Title: "seq + next_by_subj returns the first match at or after seq", Section: "DG-200", Tags: []string{"basic"}, Run: testDG204},
		{ID: "DG-205", Title: "start_time returns the first message at or after the time", Section: "DG-200", Tags: []string{"basic", "api-level-1"}, Run: testDG205},
		{ID: "DG-206", Title: "Empty stream returns 404", Section: "DG-200", Tags: []string{"basic", "errors"}, Run: testDG206},
		{ID: "DG-207", Title: "last_by_subj for unknown subject returns 404", Section: "DG-200", Tags: []string{"basic", "errors"}, Run: testDG207},
		{ID: "DG-208", Title: "Empty payload returns 408", Section: "DG-200", Tags: []string{"basic", "errors"}, Run: testDG208},
		{ID: "DG-209", Title: "Malformed JSON returns 408", Section: "DG-200", Tags: []string{"basic", "errors"}, Run: testDG209},
		{ID: "DG-210", Title: "Batch with neither seq nor start_time defaults to seq=1", Section: "DG-200", Tags: []string{"basic", "api-level-1"}, Run: testDG210},
	}
}

func testDG201(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjA := h.Subject("a")
	subjB := h.Subject("b")
	subjC := h.Subject("c")
	s1, err := publishMsg(h, subjA, []byte("va"), nil)
	if err != nil {
		return fail("publish a: %v", err)
	}
	s2, err := publishMsg(h, subjB, []byte("vb"), nil)
	if err != nil {
		return fail("publish b: %v", err)
	}
	if _, err := publishMsg(h, subjC, []byte("vc"), nil); err != nil {
		return fail("publish c: %v", err)
	}

	replies, err := directGet(h, name, directGetReq{Seq: s2}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	r := replies[0]
	if string(r.Data) != "vb" {
		return fail("payload mismatch: got %q want %q", string(r.Data), "vb")
	}
	gotSeq, _ := r.HeaderSeq()
	if gotSeq != s2 {
		return fail("Nats-Sequence got %d want %d", gotSeq, s2)
	}
	if r.Headers.Get(HdrSubject) != subjB {
		return fail("Nats-Subject got %q want %q", r.Headers.Get(HdrSubject), subjB)
	}
	_ = s1
	return pass()
}

func testDG202(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k1")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish v1: %v", err)
	}
	s2, err := publishMsg(h, subj, []byte("v2"), nil)
	if err != nil {
		return fail("publish v2: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{LastFor: subj}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	r := replies[0]
	if string(r.Data) != "v2" {
		return fail("payload mismatch: got %q want %q", string(r.Data), "v2")
	}
	gotSeq, _ := r.HeaderSeq()
	if gotSeq != s2 {
		return fail("Nats-Sequence got %d want %d", gotSeq, s2)
	}
	if r.Headers.Get(HdrSubject) != subj {
		return fail("Nats-Subject got %q want %q", r.Headers.Get(HdrSubject), subj)
	}
	return pass()
}

func testDG203(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjA := h.Subject("a")
	subjB := h.Subject("b")
	s1, err := publishMsg(h, subjA, []byte("a1"), nil)
	if err != nil {
		return fail("publish a1: %v", err)
	}
	if _, err := publishMsg(h, subjB, []byte("b1"), nil); err != nil {
		return fail("publish b1: %v", err)
	}
	if _, err := publishMsg(h, subjA, []byte("a2"), nil); err != nil {
		return fail("publish a2: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{NextFor: subjA}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	r := replies[0]
	gotSeq, _ := r.HeaderSeq()
	if gotSeq != s1 {
		return fail("expected lowest seq match (got %d, want %d)", gotSeq, s1)
	}
	if string(r.Data) != "a1" {
		return fail("payload mismatch: got %q want %q", string(r.Data), "a1")
	}
	return pass()
}

func testDG204(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjA := h.Subject("a")
	subjB := h.Subject("b")
	if _, err := publishMsg(h, subjA, []byte("a1"), nil); err != nil {
		return fail("publish a1: %v", err)
	}
	if _, err := publishMsg(h, subjB, []byte("b1"), nil); err != nil {
		return fail("publish b1: %v", err)
	}
	s3, err := publishMsg(h, subjA, []byte("a2"), nil)
	if err != nil {
		return fail("publish a2: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{Seq: 2, NextFor: subjA}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	gotSeq, _ := replies[0].HeaderSeq()
	if gotSeq != s3 {
		return fail("expected first match seq>=2 to be %d, got %d", s3, gotSeq)
	}
	return pass()
}

func testDG205(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("start_time requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("a")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish v1: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	tBeforeV2 := time.Now().UTC()
	time.Sleep(50 * time.Millisecond)
	s2, err := publishMsg(h, subj, []byte("v2"), nil)
	if err != nil {
		return fail("publish v2: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	if _, err := publishMsg(h, subj, []byte("v3"), nil); err != nil {
		return fail("publish v3: %v", err)
	}

	replies, err := directGet(h, name, directGetReq{StartTime: &tBeforeV2}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	gotSeq, _ := replies[0].HeaderSeq()
	if gotSeq != s2 {
		return fail("expected start_time to return seq %d (v2), got %d", s2, gotSeq)
	}
	return pass()
}

func testDG206(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{Seq: 1}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	if replies[0].Status != StatusNotFound {
		return fail("expected status %s, got %q (%s)", StatusNotFound, replies[0].Status, summary(replies))
	}
	if len(replies[0].Data) != 0 {
		return fail("expected zero-length payload, got %d bytes", len(replies[0].Data))
	}
	return pass()
}

func testDG207(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("va"), nil); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{LastFor: h.Subject("does.not.exist")}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	if replies[0].Status != StatusNotFound {
		return fail("expected status %s, got %q (%s)", StatusNotFound, replies[0].Status, summary(replies))
	}
	return pass()
}

func testDG208(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	replies, err := directGetRaw(h, name, nil, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	if replies[0].Status != StatusBadRequest {
		return fail("expected status %s, got %q (%s)", StatusBadRequest, replies[0].Status, summary(replies))
	}
	return pass()
}

func testDG209(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	replies, err := directGetRaw(h, name, []byte("{this is not json"), defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	if replies[0].Status != StatusBadRequest {
		return fail("expected status %s, got %q (%s)", StatusBadRequest, replies[0].Status, summary(replies))
	}
	return pass()
}

func testDG210(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("batched Direct Get requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	// Per ADR-31 rev 5: a batch request with neither seq nor start_time
	// defaults to seq=1, equivalent to "from the start of the stream".
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("a")
	seqs, err := publishN(h, subj, 3)
	if err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{Batch: 3, NextFor: subj}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) != 4 {
		return fail("expected 3 msgs + EOB, got %d (%s)", len(replies), summary(replies))
	}
	if !replies[3].IsEOB() {
		return fail("final reply not EOB: %s", summary(replies))
	}
	for i := 0; i < 3; i++ {
		if !replies[i].IsSuccess() {
			return fail("reply %d not success: %s", i, summary(replies))
		}
		gotSeq, _ := replies[i].HeaderSeq()
		if gotSeq != seqs[i] {
			return fail("reply %d seq got %d want %d (default seq=1 should yield messages from the start of the stream)", i, gotSeq, seqs[i])
		}
	}
	return pass()
}
