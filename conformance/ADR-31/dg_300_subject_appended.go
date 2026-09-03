// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr31

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// dg300Tests covers DG-300: the Subject-Appended Direct Get API
// ($JS.API.DIRECT.GET.<stream>.<tokens>) — empty payload returns the
// last message for the appended subject; a non-empty payload is an
// error.
func dg300Tests() []harness.Test {
	return []harness.Test{
		{ID: "DG-301", Title: "Subject-appended request returns last message for subject", Section: "DG-300", Tags: []string{"subject-appended"}, Run: testDG301},
		{ID: "DG-302", Title: "Subject-appended with payload returns 408", Section: "DG-300", Tags: []string{"subject-appended", "errors"}, Run: testDG302},
		{ID: "DG-303", Title: "Subject-appended request preserves multi-token subjects", Section: "DG-300", Tags: []string{"subject-appended"}, Run: testDG303},
		{ID: "DG-304", Title: "Subject-appended for unknown subject returns 404", Section: "DG-300", Tags: []string{"subject-appended", "errors"}, Run: testDG304},
	}
}

func testDG301(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("Subject-Appended Direct Get requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("key1")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish v1: %v", err)
	}
	if _, err := publishMsg(h, subj, []byte("v2"), nil); err != nil {
		return fail("publish v2: %v", err)
	}
	replies, err := directGetSubjectAppended(h, name, subj, nil, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("subject-appended direct get: err=%v replies=%s", err, summary(replies))
	}
	if string(replies[0].Data) != "v2" {
		return fail("payload mismatch: got %q want %q", string(replies[0].Data), "v2")
	}
	if replies[0].Headers.Get(HdrSubject) != subj {
		return fail("Nats-Subject got %q want %q", replies[0].Headers.Get(HdrSubject), subj)
	}
	return pass()
}

func testDG302(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("Subject-Appended Direct Get requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("key1")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGetSubjectAppended(h, name, subj, []byte(`{"seq":1}`), defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	if replies[0].Status != StatusBadRequest {
		return fail("expected status %s, got %q (%s)", StatusBadRequest, replies[0].Status, summary(replies))
	}
	return pass()
}

func testDG303(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("Subject-Appended Direct Get requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("users.1234.name")
	if _, err := publishMsg(h, subj, []byte("Bob"), nil); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGetSubjectAppended(h, name, subj, nil, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	if string(replies[0].Data) != "Bob" {
		return fail("payload mismatch: got %q want %q", string(replies[0].Data), "Bob")
	}
	if replies[0].Headers.Get(HdrSubject) != subj {
		return fail("Nats-Subject got %q want %q", replies[0].Headers.Get(HdrSubject), subj)
	}
	return pass()
}

func testDG304(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("Subject-Appended Direct Get requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	replies, err := directGetSubjectAppended(h, name, h.Subject("missing"), nil, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 {
		return fail("direct get: err=%v replies=%s", err, summary(replies))
	}
	if replies[0].Status != StatusNotFound {
		return fail("expected status %s, got %q (%s)", StatusNotFound, replies[0].Status, summary(replies))
	}
	return pass()
}
