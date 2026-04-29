// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr43

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ttl300Tests covers TTL-300: TTL-driven expiry. Each test waits for
// real wall-clock expiry and is gated by --slow.
func ttl300Tests() []harness.Test {
	slow := requiresSlow()
	return []harness.Test{
		{ID: "TTL-301", Title: "Message expires after the supplied TTL", Section: "TTL-300", Tags: []string{"expiry", "slow"}, SkipReason: slow, Run: testTTL301},
		{ID: "TTL-302", Title: "Mixed-TTL messages expire independently", Section: "TTL-300", Tags: []string{"expiry", "slow"}, SkipReason: slow, Run: testTTL302},
		{ID: "TTL-303", Title: "TTL is calculated from the stream timestamp", Section: "TTL-300", Tags: []string{"expiry", "slow"}, SkipReason: slow, Run: testTTL303},
	}
}

func testTTL301(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishWithTTL(h, h.Subject("a"), "2s", []byte("x"))
	if err != nil || ack.Error != nil {
		return fail("publish err=%v ack=%+v", err, ack)
	}
	m, err := getMsg(h, name, ack.Sequence)
	if err != nil || m == nil {
		return fail("message not present immediately after publish: err=%v nil=%v", err, m == nil)
	}
	if !waitUntilGone(h, name, ack.Sequence, 8*time.Second) {
		return fail("message at seq %d still present after 8s — TTL of 2s should have triggered removal", ack.Sequence)
	}
	return pass()
}

func testTTL302(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	short, err := publishWithTTL(h, h.Subject("a"), "2s", []byte("short"))
	if err != nil || short.Error != nil {
		return fail("short publish: err=%v ack=%+v", err, short)
	}
	long, err := publishWithTTL(h, h.Subject("a"), "30s", []byte("long"))
	if err != nil || long.Error != nil {
		return fail("long publish: err=%v ack=%+v", err, long)
	}
	none, err := publishWithTTL(h, h.Subject("a"), "", []byte("none"))
	if err != nil || none.Error != nil {
		return fail("untagged publish: err=%v ack=%+v", err, none)
	}
	time.Sleep(5 * time.Second)

	if m, err := getMsg(h, name, short.Sequence); err != nil {
		return fail("get short: %v", err)
	} else if m != nil {
		return fail("seq %d (TTL 2s) still present after 5s", short.Sequence)
	}
	if m, err := getMsg(h, name, long.Sequence); err != nil || m == nil {
		return fail("seq %d (TTL 30s) gone after 5s — should still be present", long.Sequence)
	}
	if m, err := getMsg(h, name, none.Sequence); err != nil || m == nil {
		return fail("seq %d (no TTL header) gone after 5s — should still be present", none.Sequence)
	}
	return pass()
}

func testTTL303(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishWithTTL(h, h.Subject("a"), "5s", []byte("x"))
	if err != nil || ack.Error != nil {
		return fail("publish err=%v ack=%+v", err, ack)
	}
	m, err := getMsg(h, name, ack.Sequence)
	if err != nil || m == nil {
		return fail("get msg: err=%v nil=%v", err, m == nil)
	}
	deadline := m.Time.Add(5 * time.Second)
	earlyBound := deadline.Add(-1 * time.Second) // tolerate small clock skew

	if !waitUntilGone(h, name, ack.Sequence, 10*time.Second) {
		return fail("message at seq %d still present after 10s — TTL of 5s should have triggered removal", ack.Sequence)
	}
	disappearedAt := time.Now()
	if disappearedAt.Before(earlyBound) {
		return fail("message disappeared at %s, earlier than expected stream-timestamp deadline %s",
			disappearedAt.Format(time.RFC3339Nano), deadline.Format(time.RFC3339Nano))
	}
	return pass()
}