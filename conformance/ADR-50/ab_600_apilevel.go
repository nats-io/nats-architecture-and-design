// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ab600Tests covers AB-600: Nats-Required-Api-Level enforcement.
func ab600Tests() []harness.Test {
	return []harness.Test{
		{
			ID: "AB-601", Title: "Nats-Required-Api-Level satisfied",
			Section: "AB-600", Tags: []string{"api-level"}, Run: testAB601,
		},
		{
			ID: "AB-602", Title: "Nats-Required-Api-Level unsatisfied on initial message",
			Section: "AB-600", Tags: []string{"api-level"}, Run: testAB602,
		},
		{
			ID: "AB-603", Title: "Nats-Required-Api-Level unsatisfied on a member with reply",
			Section: "AB-600", Tags: []string{"api-level"}, Run: testAB603,
		},
		{
			ID: "AB-604", Title: "Nats-Required-Api-Level unsatisfied on a member without reply",
			Section: "AB-600", Tags: []string{"api-level"}, Run: testAB604,
		},
	}
}

func testAB601(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	lvl := h.APILevel
	if lvl == 0 {
		lvl = 2
	}
	hdrs := nats.Header{HdrRequiredAPILvl: []string{fmt.Sprintf("%d", lvl)}}
	batch := newUUID()
	for i := 1; i <= 3; i++ {
		commit := ""
		if i == 3 {
			commit = "1"
		}
		ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, i, commit, hdrs, []byte{byte('a' + i - 1)}), 5*time.Second)
		if err != nil {
			return fail("seq %d: %v", i, err)
		}
		if ack.Error != nil {
			return fail("seq %d ack error: %s", i, ack.Error)
		}
	}
	return pass()
}

func testAB602(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	get, cancel := captureAdvisories(h)
	defer cancel()
	hdrs := nats.Header{HdrRequiredAPILvl: []string{"99"}}
	batch := newUUID()
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", hdrs, []byte("x")), 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack for unsatisfied required api level, got %+v", ack)
	}
	got := waitFor(3*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch && a.Reason == "unsupported" {
				return true
			}
		}
		return false
	})
	if !got {
		return inconclusive("error returned but no batch_abandoned advisory with reason=unsupported observed")
	}
	return pass()
}

func testAB603(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	hdrs := nats.Header{HdrRequiredAPILvl: []string{"99"}}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "", hdrs, []byte("b")), 5*time.Second)
	if err != nil {
		return fail("member 2: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack on member with high required api level, got %+v", ack)
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
		return inconclusive("member rejected but no batch_abandoned advisory with reason=unsupported observed")
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("expected empty stream after rejection, got last=%d", last)
	}
	return pass()
}

func testAB604(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	// Fire-and-forget: no reply set, so server cannot return an error.
	hdrs := nats.Header{HdrRequiredAPILvl: []string{"99"}}
	if err := publishFireAndForget(h, newBatchMsg(h.Subject("a"), batch, 2, "", hdrs, []byte("b"))); err != nil {
		return fail("fire-and-forget member: %v", err)
	}
	if err := h.NC.FlushTimeout(5 * time.Second); err != nil {
		return fail("flush: %v", err)
	}

	// Advisory must arrive within a few seconds.
	got := waitFor(15*time.Second, func() bool {
		for _, a := range get() {
			if a.BatchID == batch && a.Reason == "unsupported" {
				return true
			}
		}
		return false
	})
	if !got {
		return fail("did not observe batch_abandoned advisory with reason=unsupported")
	}

	// Subsequent commit must fail (batch unknown).
	commitAck, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "1", nil, []byte("c")), 5*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	if commitAck.Error == nil {
		return fail("expected commit error after batch was abandoned, got %+v", commitAck)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("expected empty stream, got last=%d", last)
	}
	return pass()
}