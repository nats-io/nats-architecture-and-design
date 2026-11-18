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

// pg600Tests covers PG-600: the prioritized priority policy.
func pg600Tests() []harness.Test {
	return []harness.Test{
		{ID: "PG-601", Title: "Pull without group on prioritized consumer is rejected", Section: "PG-600", Tags: []string{"prioritized"}, Run: testPG601},
		{ID: "PG-602", Title: "Pull with priority out of range is rejected", Section: "PG-600", Tags: []string{"prioritized"}, Run: testPG602},
		{ID: "PG-603", Title: "Pull with no priority defaults to priority 0", Section: "PG-600", Tags: []string{"prioritized"}, Run: testPG603},
		{ID: "PG-604", Title: "Lower priority is served first when both are pending", Section: "PG-600", Tags: []string{"prioritized"}, Run: testPG604},
		{ID: "PG-605", Title: "Higher priorities receive when no lower exists", Section: "PG-600", Tags: []string{"prioritized"}, Run: testPG605},
		{ID: "PG-606", Title: "Within a single priority delivery is round-robin", Section: "PG-600", Tags: []string{"prioritized"}, Run: testPG606},
	}
}

// makePrioritizedConsumer creates a stream + a fresh prioritized
// consumer with a single group "jobs".
func makePrioritizedConsumer(h *harness.Harness) (string, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return "", "", fmt.Errorf("stream create: %w", err)
	}
	cname := consumerName(h, "C")
	if _, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: PolicyPrioritized,
	}); err != nil {
		return "", "", fmt.Errorf("consumer create: %w", err)
	}
	return stream, cname, nil
}

func testPG601(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePrioritizedConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:   1,
		Expires: int64(1500 * time.Millisecond),
	}, "", 2*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return fail("expected no delivery for pull without group on prioritized consumer")
		}
	}
	if len(replies) == 0 {
		return inconclusive("server silently dropped pull without group on prioritized consumer")
	}
	return pass()
}

func testPG602(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePrioritizedConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	for _, p := range []int{-1, 10} {
		replies, err := pull(h, stream, cname, pullRequest{
			Batch:    1,
			Group:    "jobs",
			Priority: p,
			Expires:  int64(1 * time.Second),
		}, "", 2*time.Second)
		if err != nil {
			return fail("pull (priority=%d): %v", p, err)
		}
		for _, r := range replies {
			if r.IsMessage() {
				return fail("expected no delivery for priority=%d (out of range), got message", p)
			}
		}
		if len(replies) == 0 {
			return inconclusive("server silently dropped pull with priority=%d", p)
		}
	}
	return pass()
}

func testPG603(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePrioritizedConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:   1,
		Group:   "jobs",
		Expires: int64(1 * time.Second),
	}, "", 2*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return pass()
		}
	}
	return fail("expected delivery for default-priority pull, got %v", summary(replies))
}

func testPG604(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePrioritizedConsumer(h)
	if err != nil {
		return fail("%v", err)
	}

	highInbox := nats.NewInbox()
	chHigh, stopHigh, err := pullStreaming(h, stream, cname, pullRequest{
		Batch:    5,
		Group:    "jobs",
		Priority: 5,
		Expires:  int64(20 * time.Second),
	}, highInbox)
	if err != nil {
		return fail("pull HIGH: %v", err)
	}
	defer stopHigh()

	lowInbox := nats.NewInbox()
	chLow, stopLow, err := pullStreaming(h, stream, cname, pullRequest{
		Batch:    5,
		Group:    "jobs",
		Priority: 0,
		Expires:  int64(20 * time.Second),
	}, lowInbox)
	if err != nil {
		return fail("pull LOW: %v", err)
	}
	defer stopLow()

	// Give the server a beat to register both pulls.
	time.Sleep(200 * time.Millisecond)

	if err := publishN(h, h.Subject("a"), 3); err != nil {
		return fail("publish: %v", err)
	}

	highMsgs := drainAll(chHigh, 2*time.Second)
	lowMsgs := drainAll(chLow, 500*time.Millisecond) // already drained, top up

	highCount := countMessages(highMsgs)
	lowCount := countMessages(lowMsgs)
	if highCount != 0 {
		return fail("HIGH (priority=5) received %d messages while LOW (priority=0) was waiting; expected 0", highCount)
	}
	if lowCount != 3 {
		return fail("LOW (priority=0) expected 3 messages, got %d", lowCount)
	}
	return pass()
}

func testPG605(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePrioritizedConsumer(h)
	if err != nil {
		return fail("%v", err)
	}

	highInbox := nats.NewInbox()
	chHigh, stopHigh, err := pullStreaming(h, stream, cname, pullRequest{
		Batch:    5,
		Group:    "jobs",
		Priority: 5,
		Expires:  int64(30 * time.Second),
	}, highInbox)
	if err != nil {
		return fail("pull HIGH: %v", err)
	}
	defer stopHigh()
	time.Sleep(200 * time.Millisecond)

	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish 1: %v", err)
	}
	highMsgs := drainAll(chHigh, 2*time.Second)
	if c := countMessages(highMsgs); c != 1 {
		return fail("HIGH expected 1 message (no LOW competing), got %d", c)
	}

	lowInbox := nats.NewInbox()
	chLow, stopLow, err := pullStreaming(h, stream, cname, pullRequest{
		Batch:    5,
		Group:    "jobs",
		Priority: 0,
		Expires:  int64(30 * time.Second),
	}, lowInbox)
	if err != nil {
		return fail("pull LOW: %v", err)
	}
	defer stopLow()
	time.Sleep(200 * time.Millisecond)

	if _, err := publishMsg(h, h.Subject("a"), []byte("y")); err != nil {
		return fail("publish 2: %v", err)
	}
	lowMsgs := drainAll(chLow, 2*time.Second)
	if c := countMessages(lowMsgs); c != 1 {
		return fail("LOW expected 1 message (preempts HIGH), got %d", c)
	}
	// HIGH must NOT have received this second message.
	more := drainAll(chHigh, 200*time.Millisecond)
	if c := countMessages(more); c > 0 {
		return fail("HIGH received message while LOW was waiting; expected 0 additional, got %d", c)
	}
	return pass()
}

func testPG606(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePrioritizedConsumer(h)
	if err != nil {
		return fail("%v", err)
	}

	c1Inbox := nats.NewInbox()
	ch1, stop1, err := pullStreaming(h, stream, cname, pullRequest{
		Batch:    10,
		Group:    "jobs",
		Priority: 0,
		Expires:  int64(30 * time.Second),
	}, c1Inbox)
	if err != nil {
		return fail("pull C1: %v", err)
	}
	defer stop1()

	c2Inbox := nats.NewInbox()
	ch2, stop2, err := pullStreaming(h, stream, cname, pullRequest{
		Batch:    10,
		Group:    "jobs",
		Priority: 0,
		Expires:  int64(30 * time.Second),
	}, c2Inbox)
	if err != nil {
		return fail("pull C2: %v", err)
	}
	defer stop2()
	time.Sleep(200 * time.Millisecond)

	if err := publishN(h, h.Subject("a"), 4); err != nil {
		return fail("publish: %v", err)
	}

	c1Msgs := drainAll(ch1, 2*time.Second)
	c2Msgs := drainAll(ch2, 500*time.Millisecond)
	c1Count := countMessages(c1Msgs)
	c2Count := countMessages(c2Msgs)
	if c1Count+c2Count != 4 {
		return fail("expected 4 messages distributed across C1+C2, got C1=%d C2=%d", c1Count, c2Count)
	}
	if c1Count == 0 || c2Count == 0 {
		return fail("expected fair distribution (round-robin), got C1=%d C2=%d (one client got everything)", c1Count, c2Count)
	}
	return pass()
}

func countMessages(replies []*pullReply) int {
	n := 0
	for _, r := range replies {
		if r.IsMessage() {
			n++
		}
	}
	return n
}
