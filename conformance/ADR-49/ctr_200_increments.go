// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"
	"math/big"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ctr200Tests covers CTR-200: validation of Nats-Incr — required,
// signed integers only, BigInt acceptance, non-counter stream rejection.
func ctr200Tests() []harness.Test {
	return []harness.Test{
		{ID: "CTR-201", Title: "Nats-Incr is required on a counter stream", Section: "CTR-200", Tags: []string{"increment"}, Run: testCTR201},
		{ID: "CTR-202", Title: "Valid positive increments accumulate", Section: "CTR-200", Tags: []string{"increment"}, Run: testCTR202},
		{ID: "CTR-203", Title: "Valid negative increments subtract and may go negative", Section: "CTR-200", Tags: []string{"increment"}, Run: testCTR203},
		{ID: "CTR-204", Title: "Signed zero increment is accepted and leaves value unchanged", Section: "CTR-200", Tags: []string{"increment"}, Run: testCTR204},
		{ID: "CTR-205", Title: "Malformed Nats-Incr values are rejected", Section: "CTR-200", Tags: []string{"increment"}, Run: testCTR205},
		{ID: "CTR-206", Title: "BigInt-sized increments are accepted", Section: "CTR-200", Tags: []string{"increment", "bigint"}, Run: testCTR206},
	}
}

func testCTR201(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}

	// No headers at all.
	bare := nats.NewMsg(h.Subject("hits"))
	bare.Data = []byte("ignored")
	if ack, err := publishMsg(h, bare); err != nil {
		return fail("bare publish: %v", err)
	} else if ack.Error == nil {
		return fail("expected error pub ack publishing without Nats-Incr, got %+v", ack)
	}

	// Other headers but no Nats-Incr.
	foo := nats.NewMsg(h.Subject("hits"))
	foo.Header.Set("X-Other", "yes")
	foo.Data = []byte("ignored")
	if ack, err := publishMsg(h, foo); err != nil {
		return fail("X-Other publish: %v", err)
	} else if ack.Error == nil {
		return fail("expected error pub ack publishing X-Other without Nats-Incr, got %+v", ack)
	}

	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("stream last seq advanced (now %d) — neither bare nor X-Other publish should have been stored", last)
	}
	return pass()
}

func testCTR202(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")

	ack, err := publishIncr(h, subj, "+1", nil)
	if err != nil || ack.Error != nil {
		return fail("first publish err=%v ack=%+v", err, ack)
	}
	if !bigEq(ack.Value, "1") {
		return fail("expected pub ack val=1, got %q", ack.Value)
	}

	ack, err = publishIncr(h, subj, "+99", nil)
	if err != nil || ack.Error != nil {
		return fail("second publish err=%v ack=%+v", err, ack)
	}
	if !bigEq(ack.Value, "100") {
		return fail("expected pub ack val=100, got %q", ack.Value)
	}

	last, err := lastMsgFor(h, name, subj)
	if err != nil || last == nil {
		return fail("last for %s: msg=%v err=%v", subj, last, err)
	}
	val, err := decodeVal(last.Data)
	if err != nil {
		return fail("decode body: %v", err)
	}
	if !bigEq(val, "100") {
		return fail("stored body val=%q, want 100", val)
	}
	if got := last.Header.Get(HdrIncr); got != "+99" {
		return fail("stored Nats-Incr=%q, want +99", got)
	}
	return pass()
}

func testCTR203(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")

	// Seed to value 100.
	if ack, err := publishIncr(h, subj, "+50", nil); err != nil || ack.Error != nil {
		return fail("seed1 err=%v ack=%+v", err, ack)
	}
	if ack, err := publishIncr(h, subj, "+50", nil); err != nil || ack.Error != nil {
		return fail("seed2 err=%v ack=%+v", err, ack)
	}

	if ack, err := publishIncr(h, subj, "-10", nil); err != nil || ack.Error != nil {
		return fail("dec1 err=%v ack=%+v", err, ack)
	} else if !bigEq(ack.Value, "90") {
		return fail("expected val=90, got %q", ack.Value)
	}

	ack, err := publishIncr(h, subj, "-100", nil)
	if err != nil || ack.Error != nil {
		return fail("dec2 err=%v ack=%+v", err, ack)
	}
	if !bigEq(ack.Value, "-10") {
		return fail("expected val=-10, got %q", ack.Value)
	}

	last, err := lastMsgFor(h, name, subj)
	if err != nil || last == nil {
		return fail("last for %s: msg=%v err=%v", subj, last, err)
	}
	val, err := decodeVal(last.Data)
	if err != nil {
		return fail("decode body: %v", err)
	}
	if !bigEq(val, "-10") {
		return fail("stored body val=%q, want -10", val)
	}
	return pass()
}

func testCTR204(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")

	if ack, err := publishIncr(h, subj, "+5", nil); err != nil || ack.Error != nil {
		return fail("seed err=%v ack=%+v", err, ack)
	}

	// ADR-49 specifies ^[+-]\d+$, so unsigned "0" MAY be rejected;
	// "+0" and "-0" MUST be accepted.
	mandatoryAccepted := []string{"+0", "-0"}
	for _, v := range mandatoryAccepted {
		ack, err := publishIncr(h, subj, v, nil)
		if err != nil {
			return fail("publish %q: %v", v, err)
		}
		if ack.Error != nil {
			return fail("publish %q rejected with %s; ADR-49 requires acceptance of signed zero", v, ack.Error)
		}
		if !bigEq(ack.Value, "5") {
			return fail("publish %q changed value to %q, want unchanged 5", v, ack.Value)
		}
	}

	// Unsigned "0" is allowed by either branch.
	if ack, err := publishIncr(h, subj, "0", nil); err == nil && ack.Error == nil {
		if !bigEq(ack.Value, "5") {
			return fail("publish 0 (unsigned) changed value to %q, want unchanged 5", ack.Value)
		}
	}

	return pass()
}

func testCTR205(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("hits")

	// Each value here violates ^[+-]\d+$ (or its BigInt-validity
	// extension). The unsigned "0" / "1" cases are kept out — CTR-204
	// covers the unsigned-zero ambiguity, and unsigned non-zero
	// numerics MAY also be accepted by some servers; they are not
	// asserted here to keep CTR-205 unambiguous.
	bad := []string{"+", "-", "++1", "+1.5", "+1e3", "abc", "+ 1", ""}
	for _, v := range bad {
		ack, err := publishIncr(h, subj, v, nil)
		if err != nil {
			return fail("publish %q: %v", v, err)
		}
		if ack.Error == nil {
			return fail("publish %q expected to fail (does not match ^[+-]\\d+$), got success ack=%+v", v, ack)
		}
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("expected empty stream after rejected publishes, got last seq %d", last)
	}
	return pass()
}

func testCTR206(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("big")

	pos := new(big.Int).Lsh(big.NewInt(1), 128) // 2^128
	if ack, err := publishIncr(h, subj, "+"+pos.String(), nil); err != nil || ack.Error != nil {
		return fail("+2^128 err=%v ack=%+v", err, ack)
	} else if !bigEq(ack.Value, pos.String()) {
		return fail("expected val=%s, got %q", pos.String(), ack.Value)
	}

	neg := new(big.Int).Lsh(big.NewInt(1), 64) // 2^64
	expected := new(big.Int).Sub(pos, neg)
	ack, err := publishIncr(h, subj, "-"+neg.String(), nil)
	if err != nil || ack.Error != nil {
		return fail("-2^64 err=%v ack=%+v", err, ack)
	}
	if !bigEq(ack.Value, expected.String()) {
		return fail("expected val=%s, got %q", expected.String(), ack.Value)
	}

	last, err := lastMsgFor(h, name, subj)
	if err != nil || last == nil {
		return fail("last for %s: msg=%v err=%v", subj, last, err)
	}
	val, err := decodeVal(last.Data)
	if err != nil {
		return fail("decode body: %v", err)
	}
	if !bigEq(val, expected.String()) {
		return fail("stored val=%s, want %s", val, expected.String())
	}
	return pass()
}
