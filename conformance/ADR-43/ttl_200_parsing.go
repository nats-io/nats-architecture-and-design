// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr43

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ttl200Tests covers TTL-200: header parsing rules for Nats-TTL.
func ttl200Tests() []harness.Test {
	return []harness.Test{
		{ID: "TTL-201", Title: "Integer seconds value is accepted", Section: "TTL-200", Tags: []string{"parsing"}, Run: testTTL201},
		{ID: "TTL-202", Title: "Go duration string is accepted", Section: "TTL-200", Tags: []string{"parsing"}, Run: testTTL202},
		{ID: "TTL-203", Title: "0 is rejected", Section: "TTL-200", Tags: []string{"parsing"}, Run: testTTL203},
		{ID: "TTL-204", Title: "Unparsable Nats-TTL is rejected", Section: "TTL-200", Tags: []string{"parsing"}, Run: testTTL204},
		{ID: "TTL-205", Title: "Sub-second Nats-TTL is rejected", Section: "TTL-200", Tags: []string{"parsing"}, Run: testTTL205},
		{ID: "TTL-206", Title: "Nats-TTL rejected when feature disabled", Section: "TTL-200", Tags: []string{"parsing"}, Run: testTTL206},
	}
}

func testTTL201(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishWithTTL(h, h.Subject("a"), "60", []byte("x"))
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error != nil {
		return fail("publish errored: %s", ack.Error)
	}
	if ack.Sequence != 1 {
		return fail("expected seq=1, got %d", ack.Sequence)
	}
	m, err := getMsg(h, name, 1)
	if err != nil || m == nil {
		return fail("get msg: %v / nil=%v", err, m == nil)
	}
	if got := m.Header.Get(HdrTTL); got != "60" {
		return fail("stored Nats-TTL=%q, want %q (header should be preserved verbatim)", got, "60")
	}
	return pass()
}

func testTTL202(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	cases := []string{"1h", "90s", "2m30s"}
	for i, ttl := range cases {
		ack, err := publishWithTTL(h, h.Subject("a"), ttl, []byte{byte('a' + i)})
		if err != nil {
			return fail("publish %s: %v", ttl, err)
		}
		if ack.Error != nil {
			return fail("publish %s errored: %s", ttl, ack.Error)
		}
		m, err := getMsg(h, name, ack.Sequence)
		if err != nil || m == nil {
			return fail("get msg seq %d: %v / nil=%v", ack.Sequence, err, m == nil)
		}
		if got := m.Header.Get(HdrTTL); got != ttl {
			return fail("seq %d Nats-TTL=%q, want %q", ack.Sequence, got, ttl)
		}
	}
	return pass()
}

func testTTL203(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishWithTTL(h, h.Subject("a"), "0", []byte("x"))
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("publish with TTL 0: expected error pub ack, got %+v (ADR-43: 0 is below the 1s minimum)", ack)
	}
	if ack.Error.ErrCode != ErrCodeMessageTTLInvalid {
		return fail("err_code=%d, want %d (invalid per-message TTL)", ack.Error.ErrCode, ErrCodeMessageTTLInvalid)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("stream last seq advanced to %d — rejected TTL 0 message should not be stored", last)
	}
	return pass()
}

func testTTL204(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	cases := []string{"not-a-duration", "5x", "-10s"}
	for _, ttl := range cases {
		ack, err := publishWithTTL(h, h.Subject("a"), ttl, []byte("x"))
		if err != nil {
			return fail("publish %q: %v", ttl, err)
		}
		if ack.Error == nil {
			return fail("publish %q: expected error pub ack, got %+v", ttl, ack)
		}
		if ack.Error.ErrCode != ErrCodeMessageTTLInvalid {
			return fail("publish %q: err_code=%d, want %d (invalid per-message TTL)", ttl, ack.Error.ErrCode, ErrCodeMessageTTLInvalid)
		}
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("stream last seq advanced to %d — rejected messages should not be stored", last)
	}
	return pass()
}

func testTTL205(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	cases := []string{"500ms", "999ms"}
	for _, ttl := range cases {
		ack, err := publishWithTTL(h, h.Subject("a"), ttl, []byte("x"))
		if err != nil {
			return fail("publish %s: %v", ttl, err)
		}
		if ack.Error == nil {
			return fail("publish %s: expected error pub ack, got %+v", ttl, ack)
		}
		if ack.Error.ErrCode != ErrCodeMessageTTLInvalid {
			return fail("publish %s: err_code=%d, want %d (invalid per-message TTL)", ttl, ack.Error.ErrCode, ErrCodeMessageTTLInvalid)
		}
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("stream last seq advanced to %d — sub-second TTL messages should not be stored", last)
	}
	return pass()
}

func testTTL206(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishWithTTL(h, h.Subject("a"), "60s", []byte("x"))
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack publishing Nats-TTL to non-TTL stream, got %+v", ack)
	}
	if ack.Error.ErrCode != ErrCodeMessageTTLDisabled {
		return fail("err_code=%d, want %d (per-message TTL is disabled)", ack.Error.ErrCode, ErrCodeMessageTTLDisabled)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("stream last seq advanced to %d — message should not have been stored", last)
	}
	return pass()
}