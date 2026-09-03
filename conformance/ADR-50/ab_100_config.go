// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab100Tests covers AB-100: stream configuration toggles for
// AllowAtomicPublish. Tests in this section verify that the feature
// can be enabled, defaults to disabled, and rejects invalid pairings.
func ab100Tests() []harness.Test {
	return []harness.Test{
		{
			ID:      "AB-101",
			Title:   "Enabling AllowAtomicPublish works",
			Section: "AB-100",
			Tags:    []string{"config"},
			Run:     testAB101,
		},
		{
			ID:      "AB-102",
			Title:   "AllowAtomicPublish defaults off",
			Section: "AB-100",
			Tags:    []string{"config"},
			Run:     testAB102,
		},
		{
			ID:      "AB-103",
			Title:   "AllowAtomicPublish toggles via update",
			Section: "AB-100",
			Tags:    []string{"config"},
			Run:     testAB103,
		},
		{
			ID:      "AB-104",
			Title:   "AllowAtomicPublish rejected with PersistMode async",
			Section: "AB-100",
			Tags:    []string{"config", "api-level-4"},
			Run:     testAB104,
		},
		{
			ID:      "AB-105",
			Title:   "Mirrors cannot enable AllowAtomicPublish",
			Section: "AB-100",
			Tags:    []string{"config", "mirrors"},
			Run:     testAB105,
		},
		{
			ID:      "AB-106",
			Title:   "Sources may enable AllowAtomicPublish",
			Section: "AB-100",
			Tags:    []string{"config", "sources"},
			Run:     testAB106,
		},
	}
}

func testAB101(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
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
	if info.Config == nil || !info.Config.AllowAtomicPublish {
		return fail("AllowAtomicPublish not reported as true (config=%+v)", info.Config)
	}
	return pass()
}

func testAB102(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	m := newBatchMsg(h.Subject("a"), newUUID(), 1, "1", nil, []byte("x"))
	ack, err := publishRequest(h, m, 5*time.Second)
	if err != nil {
		return fail("initial publish: %v", err)
	}
	if ack.Error == nil || ack.Error.ErrCode != ErrCodeNotEnabled {
		return fail("expected ErrCode %d, got ack=%+v", ErrCodeNotEnabled, ack)
	}
	return pass()
}

func testAB103(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	subjects := []string{h.SubjectPrefix() + ".>"}
	if err := updateStream(h, streamConfig{Name: name, Subjects: subjects, AllowAtomicPublish: true}); err != nil {
		return fail("enable update: %v", err)
	}
	batch := newUUID()
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "1", nil, []byte("x")), 5*time.Second)
	if err != nil {
		return fail("commit publish: %v", err)
	}
	if ack.Error != nil || ack.BatchID != batch || ack.BatchSize != 1 {
		return fail("commit ack mismatch: %+v", ack)
	}
	if err := updateStream(h, streamConfig{Name: name, Subjects: subjects, AllowAtomicPublish: false}); err != nil {
		return fail("disable update: %v", err)
	}
	batch2 := newUUID()
	ack2, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch2, 1, "1", nil, []byte("x")), 5*time.Second)
	if err != nil {
		return fail("post-disable publish: %v", err)
	}
	if ack2.Error == nil || ack2.Error.ErrCode != ErrCodeNotEnabled {
		return fail("expected ErrCode %d after disable, got %+v", ErrCodeNotEnabled, ack2)
	}
	return pass()
}

func testAB104(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	err := createStream(h, streamConfig{
		Name:               name,
		AllowAtomicPublish: true,
		PersistMode:        "async",
	})
	if err == nil {
		return fail("expected error combining AllowAtomicPublish + PersistMode:async, got success")
	}
	d := strings.ToLower(err.Error())
	if strings.Contains(d, "persist") || strings.Contains(d, "async") || strings.Contains(d, "atomic") {
		return pass()
	}
	return inconclusive("server rejected create but error didn't mention persist/async/atomic: %v", err)
}

func testAB105(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("AB_105_SRC")
	if err := createStream(h, streamConfig{Name: src}); err != nil {
		return fail("create source: %v", err)
	}
	mir := h.MintStreamName("AB_105_MIR")
	err := createStream(h, streamConfig{
		Name:               mir,
		Mirror:             &source{Name: src},
		AllowAtomicPublish: true,
	})
	if err == nil {
		return fail("expected error creating mirror with AllowAtomicPublish, got success")
	}
	return pass()
}

func testAB106(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("AB_106_SRC")
	srcSubj := h.Subject("src") + ".>"
	if err := createStream(h, streamConfig{
		Name:               src,
		Subjects:           []string{srcSubj},
		AllowAtomicPublish: true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	dst := h.MintStreamName("AB_106_DST")
	dstSubj := h.Subject("dst") + ".>"
	if err := createStream(h, streamConfig{
		Name:               dst,
		Subjects:           []string{dstSubj},
		AllowAtomicPublish: true,
		Sources:            []source{{Name: src}},
	}); err != nil {
		return fail("create dst with sources + AllowAtomicPublish: %v", err)
	}

	pubSubj := h.Subject("src") + ".a"
	batch := newUUID()
	for i := 1; i <= 2; i++ {
		if ack, err := publishRequest(h, newBatchMsg(pubSubj, batch, i, "", nil, []byte{byte('a' + i - 1)}), 5*time.Second); err != nil || ack.Error != nil {
			return fail("seq %d err=%v ack=%+v", i, err, ack)
		}
	}
	if ack, err := publishRequest(h, newBatchMsg(pubSubj, batch, 3, "1", nil, []byte("c")), 5*time.Second); err != nil || ack.Error != nil || ack.BatchSize != 3 {
		return fail("commit err=%v ack=%+v", err, ack)
	}

	caught := waitFor(10*time.Second, func() bool {
		last, err := streamLastSeq(h, dst)
		return err == nil && last == 3
	})
	if !caught {
		last, _ := streamLastSeq(h, dst)
		return fail("DST did not converge to 3 sourced messages (last=%d)", last)
	}
	// Source may strip batch headers when sourcing — assert per ADR §AB-106.
	msgs, err := listMsgs(h, dst)
	if err != nil {
		return fail("list dst: %v", err)
	}
	for i, m := range msgs {
		if got := m.Header.Get(HdrBatchID); got != "" {
			return fail("dst msg %d retains Nats-Batch-Id=%q; sources should strip batch headers", i, got)
		}
		if got := m.Header.Get(HdrBatchSequence); got != "" {
			return fail("dst msg %d retains Nats-Batch-Sequence=%q", i, got)
		}
		if got := m.Header.Get(HdrBatchCommit); got != "" {
			return fail("dst msg %d retains Nats-Batch-Commit=%q", i, got)
		}
	}
	return pass()
}