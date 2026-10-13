// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr43

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ttl400Tests covers TTL-400: the special "never" Nats-TTL value.
func ttl400Tests() []harness.Test {
	slow := requiresSlow()
	return []harness.Test{
		{ID: "TTL-401", Title: "Nats-TTL: never survives normal publish/read", Section: "TTL-400", Tags: []string{"never", "slow"}, SkipReason: slow, Run: testTTL401},
		{ID: "TTL-402", Title: "Nats-TTL: never survives MaxAge shorter than elapsed time", Section: "TTL-400", Tags: []string{"never", "slow"}, SkipReason: slow, Run: testTTL402},
	}
}

func testTTL401(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishWithTTL(h, h.Subject("a"), "never", []byte("x"))
	if err != nil || ack.Error != nil {
		return fail("publish err=%v ack=%+v", err, ack)
	}
	time.Sleep(5 * time.Second)
	m, err := getMsg(h, name, ack.Sequence)
	if err != nil {
		return fail("get msg: %v", err)
	}
	if m == nil {
		return fail("message expired after 5s despite Nats-TTL:never")
	}
	if got := m.Header.Get(HdrTTL); got != "never" {
		return fail("stored Nats-TTL=%q, want %q", got, "never")
	}
	return pass()
}

func testTTL402(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{
		Name:        name,
		AllowMsgTTL: true,
		MaxAge:      int64(3 * time.Second),
	}); err != nil {
		return fail("stream create: %v", err)
	}
	never, err := publishWithTTL(h, h.Subject("never"), "never", []byte("forever"))
	if err != nil || never.Error != nil {
		return fail("never publish: err=%v ack=%+v", err, never)
	}
	normal, err := publishWithTTL(h, h.Subject("normal"), "", []byte("transient"))
	if err != nil || normal.Error != nil {
		return fail("normal publish: err=%v ack=%+v", err, normal)
	}
	time.Sleep(6 * time.Second)

	if m, err := getMsg(h, name, normal.Sequence); err != nil {
		return fail("get normal: %v", err)
	} else if m != nil {
		return fail("seq %d (no TTL) still present after 6s — MaxAge of 3s should have removed it", normal.Sequence)
	}
	m, err := getMsg(h, name, never.Sequence)
	if err != nil {
		return fail("get never: %v", err)
	}
	if m == nil {
		return fail("seq %d (Nats-TTL:never) was expired by MaxAge — ADR-43 says never overrides MaxAge", never.Sequence)
	}
	return pass()
}
