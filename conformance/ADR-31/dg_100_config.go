// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr31

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// dg100Tests covers DG-100: stream-configuration toggles for AllowDirect
// and the interaction with MaxMsgsPerSubject.
func dg100Tests() []harness.Test {
	return []harness.Test{
		{ID: "DG-101", Title: "AllowDirect enables the Direct Get API", Section: "DG-100", Tags: []string{"config"}, Run: testDG101},
		{ID: "DG-102", Title: "Direct Get is unavailable when AllowDirect is false", Section: "DG-100", Tags: []string{"config"}, Run: testDG102},
		{ID: "DG-103", Title: "MaxMsgsPerSubject>0 does not auto-enable AllowDirect", Section: "DG-100", Tags: []string{"config"}, Run: testDG103},
		{ID: "DG-104", Title: "Explicit AllowDirect:true is honored when MaxMsgsPerSubject is unset", Section: "DG-100", Tags: []string{"config"}, Run: testDG104},
		{ID: "DG-105", Title: "AllowDirect toggles via stream update", Section: "DG-100", Tags: []string{"config"}, Run: testDG105},
		{ID: "DG-106", Title: "Direct Get is serviced on a multi-replica stream", Section: "DG-100", Tags: []string{"config", "cluster"}, SkipReason: requiresCluster(), Run: testDG106},
	}
}

func testDG101(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowDirect {
		return fail("AllowDirect not reported as true (config=%+v)", cfg)
	}
	subj := h.Subject("k1")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{LastFor: subj}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("expected 1 success reply, got %d (%+v)", len(replies), summary(replies))
	}
	r := replies[0]
	if string(r.Data) != "v1" {
		return fail("payload mismatch: got %q want %q", string(r.Data), "v1")
	}
	for _, hdr := range []string{HdrStream, HdrSubject, HdrSequence, HdrTimeStamp} {
		if r.Headers.Get(hdr) == "" {
			return fail("missing header %s in success reply (headers=%v)", hdr, r.Headers)
		}
	}
	if r.Headers.Get(HdrStream) != name {
		return fail("Nats-Stream mismatch: got %q want %q", r.Headers.Get(HdrStream), name)
	}
	if r.Headers.Get(HdrSubject) != subj {
		return fail("Nats-Subject mismatch: got %q want %q", r.Headers.Get(HdrSubject), subj)
	}
	return pass()
}

func testDG102(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("k1"), []byte("v1"), nil); err != nil {
		return fail("publish: %v", err)
	}
	timedOut, err := directGetExpectTimeout(h, name, directGetReq{LastFor: h.Subject("k1")}, 1*time.Second)
	if err != nil {
		return fail("direct get probe: %v", err)
	}
	if !timedOut {
		return fail("expected no responder, but a reply was received")
	}
	return pass()
}

func testDG103(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// Per ADR-31 rev 4 / server >= 2.9.0: MaxMsgsPerSubject does NOT
	// auto-enable AllowDirect. Setting MaxMsgsPerSubject alone must
	// leave AllowDirect at its user-supplied value (false here), and
	// no Direct Get responder should be registered.
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, MaxMsgsPerSubject: 5}); err != nil {
		return fail("stream create: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil {
		return fail("stream info returned no config")
	}
	if cfg.AllowDirect {
		return fail("MaxMsgsPerSubject>0 must not auto-enable AllowDirect, got config=%+v", cfg)
	}
	timedOut, err := directGetExpectTimeout(h, name, directGetReq{LastFor: h.Subject("k1")}, 1*time.Second)
	if err != nil {
		return fail("direct get probe: %v", err)
	}
	if !timedOut {
		return fail("expected no Direct Get responder when AllowDirect=false; reply was received")
	}
	return pass()
}

func testDG104(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowDirect {
		return fail("expected AllowDirect true (config=%+v)", cfg)
	}
	subj := h.Subject("k1")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{LastFor: subj}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get not serviced: err=%v replies=%+v", err, summary(replies))
	}
	return pass()
}

func testDG105(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	subjects := []string{h.SubjectPrefix() + ".>"}
	if err := createStream(h, streamConfig{Name: name, Subjects: subjects}); err != nil {
		return fail("stream create: %v", err)
	}
	if err := updateStream(h, streamConfig{Name: name, Subjects: subjects, AllowDirect: true}); err != nil {
		return fail("enable update: %v", err)
	}
	subj := h.Subject("k1")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{LastFor: subj}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("post-enable direct get: err=%v replies=%+v", err, summary(replies))
	}
	if err := updateStream(h, streamConfig{Name: name, Subjects: subjects}); err != nil {
		return fail("disable update: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("post-disable stream info: %v", err)
	}
	if cfg == nil || cfg.AllowDirect {
		return fail("expected AllowDirect false after disable update (config=%+v)", cfg)
	}
	timedOut, err := directGetExpectTimeout(h, name, directGetReq{LastFor: subj}, 1*time.Second)
	if err != nil {
		return fail("post-disable direct get probe: %v", err)
	}
	if !timedOut {
		return fail("post-disable: expected no responder, but a reply was received")
	}
	return pass()
}

func testDG106(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// Conformance scope is that a Direct Get against a multi-replica
	// stream is correctly serviced. Queue-group spread across the
	// stream's peers is standard NATS routing, not an ADR-31 concern.
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true, Replicas: 3}); err != nil {
		return skip("R3 stream create failed (cluster may be unavailable): %v", err)
	}
	subj := h.Subject("k1")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := directGet(h, name, directGetReq{LastFor: subj}, defaultDirectGetTimeout)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get on R3: err=%v replies=%s", err, summary(replies))
	}
	return pass()
}
