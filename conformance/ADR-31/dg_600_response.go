// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr31

import (
	"context"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// dg600Tests covers DG-600: response format and headers.
func dg600Tests() []harness.Test {
	return []harness.Test{
		{ID: "DG-601", Title: "Success reply carries required headers", Section: "DG-600", Tags: []string{"format"}, Run: testDG601},
		{ID: "DG-602", Title: "EOB sentinel carries required headers", Section: "DG-600", Tags: []string{"format", "api-level-1"}, Run: testDG602},
		{ID: "DG-603", Title: "Multi-mode EOB carries Nats-UpTo-Sequence", Section: "DG-600", Tags: []string{"format", "api-level-1"}, Run: testDG603},
		{ID: "DG-604", Title: "Reply body is the raw stored payload (no JSON envelope)", Section: "DG-600", Tags: []string{"format"}, Run: testDG604},
		{ID: "DG-605", Title: "Original message headers are preserved on reply", Section: "DG-600", Tags: []string{"format"}, Run: testDG605},
	}
}

func testDG601(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	if _, err := publishMsg(h, subj, []byte("v"), nil); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{LastFor: subj}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	r := replies[0]
	if r.Status != "" {
		return fail("success reply has Status %q (expected empty)", r.Status)
	}
	for _, hdr := range []string{HdrStream, HdrSubject, HdrSequence, HdrTimeStamp} {
		if r.Headers.Get(hdr) == "" {
			return fail("missing header %s in success reply", hdr)
		}
	}
	return pass()
}

func testDG602(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("EOB requires batched Direct Get (server >= 2.11 / API level >= 1); got %d", h.APILevel)
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
	if len(replies) < 1 || !replies[len(replies)-1].IsEOB() {
		return fail("expected EOB, got %s", summary(replies))
	}
	eob := replies[len(replies)-1]
	if eob.Description != DescriptionEOB {
		return fail("EOB description got %q want %q", eob.Description, DescriptionEOB)
	}
	if len(eob.Data) != 0 {
		return fail("EOB has %d byte payload, want 0", len(eob.Data))
	}
	if _, ok := eob.HeaderUint(HdrNumPending); !ok {
		return fail("EOB missing Nats-Num-Pending")
	}
	if _, ok := eob.HeaderUint(HdrLastSeq); !ok {
		return fail("EOB missing Nats-Last-Sequence")
	}
	return pass()
}

func testDG603(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("multi_last requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("va"), nil); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject(">")},
	}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) < 1 || !replies[len(replies)-1].IsEOB() {
		return fail("expected EOB, got %s", summary(replies))
	}
	eob := replies[len(replies)-1]
	if _, ok := eob.HeaderUint(HdrUpToSeq); !ok {
		return fail("EOB missing Nats-UpTo-Sequence")
	}
	if _, ok := eob.HeaderUint(HdrNumPending); !ok {
		return fail("EOB missing Nats-Num-Pending")
	}
	if _, ok := eob.HeaderUint(HdrLastSeq); !ok {
		return fail("EOB missing Nats-Last-Sequence")
	}
	return pass()
}

func testDG604(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	body := `{"foo":"bar"}`
	if _, err := publishMsg(h, subj, []byte(body), nil); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{LastFor: subj}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	if string(replies[0].Data) != body {
		return fail("payload not byte-for-byte equal: got %q want %q", string(replies[0].Data), body)
	}
	return pass()
}

func testDG605(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	hdrs := nats.Header{}
	hdrs.Set("X-Custom", "hello")
	if _, err := publishMsg(h, subj, []byte("v"), hdrs); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{LastFor: subj}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	if got := replies[0].Headers.Get("X-Custom"); got != "hello" {
		return fail("expected X-Custom=hello, got %q (headers=%v)", got, replies[0].Headers)
	}
	if replies[0].Headers.Get(HdrStream) != name {
		return fail("Nats-Stream got %q want %q", replies[0].Headers.Get(HdrStream), name)
	}
	return pass()
}
