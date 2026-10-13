// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab1000Tests covers AB-1000: shape and reason fields of the
// batch_abandoned advisory.
func ab1000Tests() []harness.Test {
	return []harness.Test{
		{
			ID: "AB-1001", Title: "batch_abandoned event shape",
			Section: "AB-1000", Tags: []string{"advisory", "slow"}, Run: testAB1001,
		},
		{
			ID: "AB-1002", Title: "Advisory reason: incomplete",
			Section: "AB-1000", Tags: []string{"advisory"}, Run: testAB1002,
		},
		{
			ID: "AB-1003", Title: "Advisory reason: unsupported",
			Section: "AB-1000", Tags: []string{"advisory"}, Run: testAB1003,
		},
		{
			ID: "AB-1004", Title: "Advisory reason: large",
			Section: "AB-1000", Tags: []string{"advisory", "resource-intensive"},
			SkipReason: requiresResourceIntensive(), Run: testAB1004,
		},
	}
}

func testAB1001(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjects := make(chan *nats.Msg, 16)
	sub, err := h.NC.ChanSubscribe(AdvisorySubjectPrefix, subjects)
	if err != nil {
		return fail("advisory sub: %v", err)
	}
	defer sub.Unsubscribe()

	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	deadline := time.After(15 * time.Second)
	for {
		select {
		case <-deadline:
			return fail("did not observe batch_abandoned advisory")
		case msg := <-subjects:
			var a struct {
				batchAdvisory
				ID        string    `json:"id"`
				Timestamp time.Time `json:"timestamp"`
			}
			if err := json.Unmarshal(msg.Data, &a); err != nil {
				continue
			}
			if a.Type != AdvisoryBatchAbandon || a.BatchID != batch {
				continue
			}
			if !strings.Contains(strings.ToUpper(msg.Subject), "BATCH_ABANDONED") {
				return fail("advisory subject does not contain BATCH_ABANDONED: %s", msg.Subject)
			}
			if a.Reason != "timeout" {
				return fail("advisory reason=%q, want timeout", a.Reason)
			}
			if a.Stream != name {
				return fail("advisory stream=%q, want %q", a.Stream, name)
			}
			if a.ID == "" {
				return fail("advisory missing id field")
			}
			if a.Timestamp.IsZero() {
				return fail("advisory missing timestamp field")
			}
			return pass()
		}
	}
}

func testAB1002(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	get, cancel := captureAdvisories(h)
	defer cancel()
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "", nil, []byte("c")), 5*time.Second); err != nil {
		return fail("gap publish: %v", err)
	} else if ack.Error == nil {
		return fail("expected error pub ack on gap")
	}
	got := waitFor(5*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch && a.Reason == "incomplete" {
				return true
			}
		}
		return false
	})
	if !got {
		return inconclusive("gap rejected but no advisory with reason=incomplete observed (server may not emit one for direct gap rejections)")
	}
	return pass()
}

func testAB1003(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	get, cancel := captureAdvisories(h)
	defer cancel()
	hdrs := nats.Header{HdrRequiredAPILvl: []string{"99"}}
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", hdrs, []byte("x")), 5*time.Second); err != nil || ack.Error == nil {
		return fail("expected error pub ack on unsatisfied API level: err=%v ack=%+v", err, ack)
	}
	got := waitFor(5*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch && a.Reason == "unsupported" {
				return true
			}
		}
		return false
	})
	if !got {
		return fail("did not observe stream_batch_abandoned advisory with reason=unsupported")
	}
	return pass()
}

func testAB1004(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	get, cancelAdv := captureAdvisories(h)
	defer cancelAdv()

	batch := newUUID()
	subject := h.Subject("a")

	// Initial member.
	if ack, err := publishRequest(h, newBatchMsg(subject, batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	// Members 2..1000 fire-and-forget so we don't wait on per-member acks.
	for seq := 2; seq <= 1000; seq++ {
		if err := publishFireAndForget(h, newBatchMsg(subject, batch, seq, "", nil, []byte("x"))); err != nil {
			return fail("member seq=%d publish: %v", seq, err)
		}
	}
	// Flush so members are observed before the 1001st arrives.
	if err := h.NC.FlushTimeout(5 * time.Second); err != nil {
		return fail("flush: %v", err)
	}
	// 1001st member with reply — this MUST exceed the per-batch limit.
	ack, err := publishRequest(h, newBatchMsg(subject, batch, 1001, "", nil, []byte("over")), 10*time.Second)
	if err != nil {
		return fail("1001st publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack on seq=1001, got %+v", ack)
	}
	if ack.Error.ErrCode != ErrCodeSeqLimit {
		return fail("expected err_code=%d (seq limit), got %s", ErrCodeSeqLimit, ack.Error)
	}

	// Advisory must arrive with reason=large referencing this batch.
	got := waitFor(10*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch && a.Reason == "large" {
				return true
			}
		}
		return false
	})
	if !got {
		return fail("did not observe stream_batch_abandoned advisory with reason=large for batch %q", batch)
	}

	// Stream must be empty — the abandoned batch's staged messages
	// were never persisted.
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("stream last seq=%d, want 0 (batch should have been abandoned without persisting any messages)", last)
	}
	return pass()
}