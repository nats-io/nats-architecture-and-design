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