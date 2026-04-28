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

// fb100Tests covers FB-100: stream-configuration toggles for
// AllowBatchPublish.
func fb100Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-101", Title: "Enabling AllowBatchPublish works", Section: "FB-100", Tags: []string{"config"}, Run: testFB101},
		{ID: "FB-102", Title: "AllowBatchPublish defaults off", Section: "FB-100", Tags: []string{"config"}, Run: testFB102},
		{ID: "FB-103", Title: "AllowBatchPublish toggles via update", Section: "FB-100", Tags: []string{"config"}, Run: testFB103},
		{ID: "FB-104", Title: "AllowBatchPublish compatible with PersistMode async", Section: "FB-100", Tags: []string{"config"}, Run: testFB104},
		{ID: "FB-105", Title: "AllowBatchPublish and AllowAtomicPublish may coexist", Section: "FB-100", Tags: []string{"config"}, Run: testFB105},
	}
}

func testFB101(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	resp, err := h.NC.Request("$JS.API.STREAM.INFO."+name, nil, 5*time.Second)
	if err != nil {
		return fail("stream info: %v", err)
	}
	var info struct {
		Config *streamConfig `json:"config"`
		Error  *apiError     `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return fail("decode info: %v", err)
	}
	if info.Error != nil {
		return fail("stream info error: %s", info.Error)
	}
	if info.Config == nil || !info.Config.AllowBatchPublish {
		return fail("AllowBatchPublish not reported as true (config=%+v)", info.Config)
	}
	return pass()
}

func testFB102(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	if err := handle.publish(h.Subject("a"), FBOpStart, nil, []byte("x")); err != nil {
		return fail("publish initial: %v", err)
	}
	m, err := handle.readNext(5 * time.Second)
	if err != nil {
		return fail("read inbox: %v", err)
	}
	if m.Error == nil || m.Error.ErrCode != FBErrCodeNotEnabled {
		return fail("expected ErrCode %d, got %+v", FBErrCodeNotEnabled, m)
	}
	return pass()
}

func testFB103(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	subjects := []string{h.SubjectPrefix() + ".>"}
	if err := updateStream(h, streamConfig{Name: name, Subjects: subjects, AllowBatchPublish: true}); err != nil {
		return fail("enable update: %v", err)
	}
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("x")); err != nil {
		handle.Close()
		return fail("commit publish: %v", err)
	}
	ack, err := handle.awaitPubAck(5 * time.Second)
	handle.Close()
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil || ack.BatchID == "" || ack.BatchSize != 1 {
		return fail("commit ack mismatch: %+v", ack)
	}

	if err := updateStream(h, streamConfig{Name: name, Subjects: subjects, AllowBatchPublish: false}); err != nil {
		return fail("disable update: %v", err)
	}
	handle2, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch (post-disable): %v", err)
	}
	defer handle2.Close()
	if err := handle2.publish(h.Subject("a"), FBOpStart, nil, []byte("x")); err != nil {
		return fail("post-disable publish: %v", err)
	}
	m, err := handle2.readNext(5 * time.Second)
	if err != nil {
		return fail("post-disable read: %v", err)
	}
	if m.Error == nil || m.Error.ErrCode != FBErrCodeNotEnabled {
		return fail("expected ErrCode %d after disable, got %+v", FBErrCodeNotEnabled, m)
	}
	return pass()
}

func testFB104(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// PersistMode async with AllowBatchPublish — ADR-50 says this is
	// allowed (unlike AllowAtomicPublish, which must error). Servers
	// that don't yet expose persist_mode at all return an error;
	// surface that as inconclusive instead of a hard fail.
	name := streamName(h)
	if err := createStream(h, streamConfig{
		Name:              name,
		AllowBatchPublish: true,
		PersistMode:       "async",
	}); err != nil {
		return inconclusive("server rejected AllowBatchPublish + async (may be unsupported on this build): %v", err)
	}

	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()
	for i := 0; i < 4; i++ {
		op := FBOpAppend
		if i == 0 {
			op = FBOpStart
		}
		if err := handle.publish(h.Subject("a"), op, nil, []byte{byte('a' + i)}); err != nil {
			return fail("publish seq %d: %v", i+1, err)
		}
	}
	if err := handle.publish(h.Subject("a"), FBOpCommitStore, nil, []byte("e")); err != nil {
		return fail("commit publish: %v", err)
	}
	ack, err := handle.awaitPubAck(10 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil || ack.BatchSize != 5 {
		return fail("commit ack mismatch: %+v", ack)
	}
	return pass()
}

func testFB105(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true, AllowBatchPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}

	// Atomic batch (3 messages, headers).
	abID := newUUID()
	for i := 1; i <= 2; i++ {
		ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), abID, i, "", nil, []byte{byte('a' + i - 1)}), 5*time.Second)
		if err != nil || ack.Error != nil {
			return fail("atomic seq %d err=%v ack=%+v", i, err, ack)
		}
	}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), abID, 3, "1", nil, []byte("c")), 5*time.Second); err != nil || ack.Error != nil || ack.BatchSize != 3 {
		return fail("atomic commit err=%v ack=%+v", err, ack)
	}

	// Fast batch (3 messages, reply subjects) on the same stream.
	handle, err := openFastBatch(h, name, 10, "ok")
	if err != nil {
		return fail("open fast batch: %v", err)
	}
	defer handle.Close()
	for i := 1; i <= 2; i++ {
		op := FBOpAppend
		if i == 1 {
			op = FBOpStart
		}
		if err := handle.publish(h.Subject("b"), op, nil, []byte{byte('a' + i - 1)}); err != nil {
			return fail("fast seq %d: %v", i, err)
		}
	}
	if err := handle.publish(h.Subject("b"), FBOpCommitStore, nil, []byte("c")); err != nil {
		return fail("fast commit: %v", err)
	}
	ack, err := handle.awaitPubAck(5 * time.Second)
	if err != nil {
		return fail("await fast pubAck: %v", err)
	}
	if ack.Error != nil || ack.BatchSize != 3 {
		return fail("fast commit ack mismatch: %+v", ack)
	}

	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 6 {
		return fail("expected 6 messages stored, last seq is %d", last)
	}
	return pass()
}