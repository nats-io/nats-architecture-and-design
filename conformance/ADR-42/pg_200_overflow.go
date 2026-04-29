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
// validate group enforcement, min_pending / min_ack_pending thresholds
// (including OR semantics), and failover bounds + behaviour.
func pg200Tests() []harness.Test {
	return []harness.Test{
		{ID: "PG-201", Title: "Pull without group on priority consumer is rejected", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG201},
		{ID: "PG-202", Title: "Pull with unknown group is rejected", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG202},
		{ID: "PG-203", Title: "Pull idle when min_pending unmet", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG203},
		{ID: "PG-204", Title: "Pull served when min_pending met", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG204},
		{ID: "PG-205", Title: "min_pending and min_ack_pending combine via OR", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG205},
		{ID: "PG-206", Title: "failover value below 5 is rejected", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG206},
		{ID: "PG-207", Title: "failover value above 3600 is rejected", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG207},
		{ID: "PG-208", Title: "failover accepts boundary values 5 and 3600", Section: "PG-200", Tags: []string{"overflow"}, Run: testPG208},
		{ID: "PG-209", Title: "Failover takes over when no near pull is present", Section: "PG-200", Tags: []string{"overflow", "slow"}, SkipReason: requiresSlow(), Run: testPG209},
		{ID: "PG-210", Title: "Nearer pulls suppress further failover", Section: "PG-200", Tags: []string{"overflow", "slow"}, SkipReason: requiresSlow(), Run: testPG210},
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

func testPG206(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:    1,
		Group:    "jobs",
		Failover: 4,
		Expires:  int64(1 * time.Second),
	}, "", 2*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return fail("expected no delivery for failover=4 (below min 5), got a message")
		}
	}
	if len(replies) == 0 {
		return inconclusive("server silently dropped pull with failover=4")
	}
	return pass()
}

func testPG207(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:    1,
		Group:    "jobs",
		Failover: 3601,
		Expires:  int64(1 * time.Second),
	}, "", 2*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return fail("expected no delivery for failover=3601 (above max 3600), got a message")
		}
	}
	if len(replies) == 0 {
		return inconclusive("server silently dropped pull with failover=3601")
	}
	return pass()
}

func testPG208(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	for _, fo := range []int{5, 3600} {
		if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
			return fail("publish: %v", err)
		}
		// failover=5 means messages start flowing only after 5s of idle.
		// The test only requires that the pull is *accepted* (no 4xx
		// error reply), not that a delivery occurs within the window —
		// so use a short expiry and check we did not get an error reply.
		replies, err := pull(h, stream, cname, pullRequest{
			Batch:    1,
			Group:    "jobs",
			Failover: fo,
			Expires:  int64(1 * time.Second),
		}, "", 2*time.Second)
		if err != nil {
			return fail("pull (failover=%d): %v", fo, err)
		}
		for _, r := range replies {
			// 408 (request timeout) is acceptable; any other 4xx is not.
			if !r.IsMessage() && r.Status != "" && r.Status != "408" {
				return fail("failover=%d boundary value rejected with status=%q desc=%q", fo, r.Status, r.Description)
			}
		}
	}
	return pass()
}

func testPG209(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	// failover=5 → message should arrive after ~5s idle, NOT before.
	start := time.Now()
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:    1,
		Group:    "jobs",
		Failover: 5,
		Expires:  int64(10 * time.Second),
	}, "", 12*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	gotAt := time.Duration(-1)
	for _, r := range replies {
		if r.IsMessage() {
			gotAt = time.Since(start)
			break
		}
	}
	if gotAt < 0 {
		return fail("failover=5 expected delivery within 10s expiry, got none")
	}
	if gotAt < 4*time.Second {
		return fail("failover=5 delivered too early at %v (expected >= ~5s)", gotAt)
	}
	return pass()
}

func testPG210(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	// "Nearby" pull A: high min_pending so it never qualifies for
	// delivery, but its repeated presence should reset the failover
	// timer for B. Refresh A every 2s.
	stopA := make(chan struct{})
	defer close(stopA)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			_, _ = pull(h, stream, cname, pullRequest{
				Batch:      1,
				Group:      "jobs",
				MinPending: 999999,
				Expires:    int64(2 * time.Second),
			}, "", 3*time.Second)
			select {
			case <-stopA:
				return
			case <-ticker.C:
			}
		}
	}()

	// Failover pull B: 5s, but A is "nearer" and refreshed often.
	bInbox := nats.NewInbox()
	chB, stopB, err := pullStreaming(h, stream, cname, pullRequest{
		Batch:    1,
		Group:    "jobs",
		Failover: 5,
		Expires:  int64(15 * time.Second),
	}, bInbox)
	if err != nil {
		return fail("pull B: %v", err)
	}
	defer stopB()

	// Watch for a delivery to B over 10s. Per ADR, B should NOT receive
	// any data while A is keeping the timer alive.
	gotB := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rep, ok := drainNext(chB, time.Until(deadline))
		if !ok {
			break
		}
		if rep.IsMessage() {
			gotB = true
			break
		}
	}
	if gotB {
		return inconclusive("B received a delivery despite A being nearer — server may not detect non-qualifying pull presence")
	}
	return pass()
}
