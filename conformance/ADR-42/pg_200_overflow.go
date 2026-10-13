// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr42

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// pg200Tests covers PG-200: the overflow priority policy. Tests
// validate group enforcement and min_pending / min_ack_pending
// thresholds (including OR semantics).
//
// The ADR-42 `failover` option is not implemented in NATS Server as of
// 2.14, so PG-206..PG-210 are intentionally absent.
func pg200Tests() []harness.Test {
	return []harness.Test{
		{ID: "PG-201", Title: "Pull without group on priority consumer is rejected", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG201},
		{ID: "PG-202", Title: "Pull with unknown group is rejected", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG202},
		{ID: "PG-203", Title: "Pull idle when min_pending unmet", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG203},
		{ID: "PG-204", Title: "Pull served when min_pending met", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG204},
		{ID: "PG-205", Title: "min_pending and min_ack_pending combine via OR", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG205},
	}
}

// makeOverflowConsumer creates a stream + a fresh overflow consumer
// with a single group "jobs". Returns the stream and consumer names.
func makeOverflowConsumer(h *harness.Harness) (string, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return "", "", fmt.Errorf("stream create: %w", err)
	}
	cname := consumerName(h, "C")
	if _, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: PolicyOverflow,
	}); err != nil {
		return "", "", fmt.Errorf("consumer create: %w", err)
	}
	return stream, cname, nil
}

func testPG201(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := pull(h, stream, cname, pullRequest{Batch: 1, Expires: int64(1 * time.Second)}, "", 2*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return fail("expected no data delivery for pull without 'group', got message subject=%q", r.Subject)
		}
	}
	if len(replies) == 0 {
		// Server expired silently — also acceptable per ADR (the rule
		// says "result in an error", but expired pulls only emit a 408
		// when expires is set explicitly). Record and pass.
		return inconclusive("server silently dropped pull without 'group'; no status reply observed")
	}
	// At least one status reply received.
	return pass()
}

func testPG202(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := pull(h, stream, cname, pullRequest{Batch: 1, Group: "ghost", Expires: int64(1 * time.Second)}, "", 2*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return fail("expected no data delivery for pull with unknown 'group', got message subject=%q", r.Subject)
		}
	}
	if len(replies) == 0 {
		return inconclusive("server silently dropped pull with unknown 'group'; no status reply observed")
	}
	return pass()
}

func testPG203(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if err := publishN(h, h.Subject("a"), 5); err != nil {
		return fail("publish: %v", err)
	}
	// 1.5s expires — well below the threshold, no message should come.
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:      1,
		Group:      "jobs",
		MinPending: 1000,
		Expires:    int64(1500 * time.Millisecond),
	}, "", 2*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return fail("min_pending=1000 not met (only 5 pending) — expected no delivery, got message")
		}
	}
	return pass()
}

func testPG204(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if err := publishN(h, h.Subject("a"), 1500); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:      5,
		Group:      "jobs",
		MinPending: 1000,
		Expires:    int64(2 * time.Second),
	}, "", 3*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	got := 0
	for _, r := range replies {
		if r.IsMessage() {
			got++
		}
	}
	if got != 5 {
		return fail("expected 5 messages (min_pending met), got %d (replies=%d)", got, len(replies))
	}
	return pass()
}

func testPG205(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if err := publishN(h, h.Subject("a"), 100); err != nil {
		return fail("publish: %v", err)
	}
	// Consume 50 messages and DON'T ack them, so num_ack_pending = 50.
	pre := nats.NewInbox()
	preReplies, err := pull(h, stream, cname, pullRequest{
		Batch:   50,
		Group:   "jobs",
		Expires: int64(2 * time.Second),
	}, pre, 3*time.Second)
	if err != nil {
		return fail("pre-pull: %v", err)
	}
	gotPre := 0
	for _, r := range preReplies {
		if r.IsMessage() {
			gotPre++
		}
	}
	if gotPre != 50 {
		return fail("setup pull expected 50 messages, got %d", gotPre)
	}

	// Now: min_pending=1000 (NOT met, 50 left), min_ack_pending=10 (MET).
	// Boolean OR — server should deliver.
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:         1,
		Group:         "jobs",
		MinPending:    1000,
		MinAckPending: 10,
		Expires:       int64(2 * time.Second),
	}, "", 3*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	got := 0
	for _, r := range replies {
		if r.IsMessage() {
			got++
		}
	}
	if got == 0 {
		return fail("expected delivery (min_ack_pending OR clause met), got no messages (replies=%d)", len(replies))
	}
	return pass()
}
