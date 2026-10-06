// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"
	"math/big"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ctr900Tests covers CTR-900: audit / recount via the preserved
// Nats-Incr header history.
func ctr900Tests() []harness.Test {
	return []harness.Test{
		{ID: "CTR-901", Title: "Replaying preserved Nats-Incr headers reproduces the total", Section: "CTR-900", Tags: []string{"audit"}, Run: testCTR901},
	}
}

func testCTR901(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")

	steps := []string{"+1", "+5", "-2", "+10", "-3"}
	for i, s := range steps {
		if ack, err := publishIncr(h, subj, s, nil); err != nil || ack.Error != nil {
			return fail("step %d (%s) err=%v ack=%+v", i, s, err, ack)
		}
	}

	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	if len(msgs) != len(steps) {
		return fail("expected %d stored msgs, got %d", len(steps), len(msgs))
	}
	sum := new(big.Int)
	for i, m := range msgs {
		hdr := m.Header.Get(HdrIncr)
		if hdr == "" {
			return fail("msg %d missing Nats-Incr header", i)
		}
		bi, ok := new(big.Int).SetString(hdr, 10)
		if !ok {
			return fail("msg %d Nats-Incr=%q not parseable as integer", i, hdr)
		}
		sum.Add(sum, bi)
	}
	if sum.String() != "11" {
		return fail("recounted sum=%s, want 11", sum.String())
	}
	last, err := lastMsgFor(h, name, subj)
	if err != nil || last == nil {
		return fail("last for %s: %v", subj, err)
	}
	val, err := decodeVal(last.Data)
	if err != nil {
		return fail("decode body: %v", err)
	}
	if !bigEq(val, "11") {
		return fail("stored val=%q, want 11", val)
	}
	if val != sum.String() {
		// Different lexical formatting is acceptable in principle but
		// surprising — record without failing.
		return inconclusive("recount sum=%s, stored val=%s — equivalent but lexically different", sum.String(), val)
	}
	return pass()
}
