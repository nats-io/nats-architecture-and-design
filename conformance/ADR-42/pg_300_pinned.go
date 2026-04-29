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

// pg300Tests covers PG-300: the pinned_client priority policy. Tests
// validate the pin lifecycle (Nats-Pin-Id), 423 status replies on
// mismatched IDs, persistence within PriorityTimeout, idle switches,
// and Fetch interaction.
func pg300Tests() []harness.Test {
	return []harness.Test{
		{ID: "PG-301", Title: "First pull becomes the pinned client", Section: "PG-300", Tags: []string{"pinned"}, Run: testPG301},
		{ID: "PG-302", Title: "Subsequent pulls without an id are rejected with 423", Section: "PG-300", Tags: []string{"pinned"}, Run: testPG302},
		{ID: "PG-303", Title: "Pull with the wrong id is rejected with 423", Section: "PG-300", Tags: []string{"pinned"}, Run: testPG303},
		{ID: "PG-304", Title: "Pinned client receives messages with same Nats-Pin-Id", Section: "PG-300", Tags: []string{"pinned"}, Run: testPG304},
		{ID: "PG-305", Title: "Pinned client times out and the pin switches", Section: "PG-300", Tags: []string{"pinned", "slow"}, SkipReason: requiresSlow(), Run: testPG305},
		{ID: "PG-306", Title: "Pin survives across pulls within PriorityTimeout", Section: "PG-300", Tags: []string{"pinned", "slow"}, SkipReason: requiresSlow(), Run: testPG306},
		{ID: "PG-307", Title: "Fetch on pinned_client consumer (server enforcement)", Section: "PG-300", Tags: []string{"pinned"}, Run: testPG307},
	}
}

// makePinnedConsumer creates a stream + a fresh pinned_client consumer
// with a single group "jobs" and the supplied PriorityTimeout.
func makePinnedConsumer(h *harness.Harness, timeout time.Duration) (string, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return "", "", fmt.Errorf("stream create: %w", err)
	}
	cname := consumerName(h, "C")
	if _, err := createConsumer(h, stream, consumerConfig{
		Name:            cname,
		AckPolicy:       "explicit",
		PriorityGroups:  []string{"jobs"},
		PriorityPolicy:  PolicyPinnedClient,
		PriorityTimeout: int64(timeout),
	}); err != nil {
		return "", "", fmt.Errorf("consumer create: %w", err)
	}
	return stream, cname, nil
}

// pinClientFirst sends one initial pull and waits for the first
// delivered message. Returns the captured pin id and the inbox used so
// the caller can re-use it for subsequent pulls as the "same client".
func pinClientFirst(h *harness.Harness, stream, cname string) (string, string, error) {
	inbox := nats.NewInbox()
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:   1,
		Group:   "jobs",
		Expires: int64(2 * time.Second),
	}, inbox, 3*time.Second)
	if err != nil {
		return "", "", fmt.Errorf("initial pull: %w", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			pin := r.PinID()
			if pin == "" {
				return "", "", fmt.Errorf("delivered message lacked Nats-Pin-Id header (headers=%v)", r.Headers)
			}
			return pin, inbox, nil
		}
	}
	return "", "", fmt.Errorf("no message delivered to pin candidate (got %d replies)", len(replies))
}

func testPG301(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	pin, _, err := pinClientFirst(h, stream, cname)
	if err != nil {
		return fail("%v", err)
	}
	if pin == "" {
		return fail("captured pin id is empty")
	}
	info, err := consumerInfo(h, stream, cname)
	if err != nil {
		return fail("consumer info: %v", err)
	}
	if len(info.PriorityGroups) == 0 {
		return fail("priority_groups state missing in consumer info")
	}
	g := info.PriorityGroups[0]
	if g.Group != "jobs" {
		return fail("priority_groups[0].name=%q want %q", g.Group, "jobs")
	}
	if g.PinnedClientId != pin {
		return fail("priority_groups[0].pinned_id=%q want %q", g.PinnedClientId, pin)
	}
	if g.PinnedTs == nil || g.PinnedTs.IsZero() {
		return fail("priority_groups[0].pinned_ts is empty after pin")
	}
	return pass()
}

func testPG302(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	if _, _, err := pinClientFirst(h, stream, cname); err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("y")); err != nil {
		return fail("publish 2: %v", err)
	}
	// Different client (fresh inbox) WITHOUT id — must get 423.
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:   1,
		Group:   "jobs",
		Expires: int64(1500 * time.Millisecond),
	}, "", 2*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return fail("non-pinned client got a delivery; expected 423 status")
		}
		if r.Status == StatusPinMismatch {
			return pass()
		}
	}
	return fail("expected status 423 from non-pinned client; got replies=%v", summary(replies))
}

func testPG303(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	if _, _, err := pinClientFirst(h, stream, cname); err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("y")); err != nil {
		return fail("publish 2: %v", err)
	}
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:   1,
		Group:   "jobs",
		ID:      "definitely-not-the-pin",
		Expires: int64(1500 * time.Millisecond),
	}, "", 2*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return fail("wrong-id pull got a delivery; expected 423 status")
		}
		if r.Status == StatusPinMismatch {
			return pass()
		}
	}
	return fail("expected status 423 for wrong id; got replies=%v", summary(replies))
}

func testPG304(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if err := publishN(h, h.Subject("a"), 5); err != nil {
		return fail("publish: %v", err)
	}
	pin, inbox, err := pinClientFirst(h, stream, cname)
	if err != nil {
		return fail("%v", err)
	}
	// Pull 4 more from the same pinned client (same inbox + id).
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:   4,
		Group:   "jobs",
		ID:      pin,
		Expires: int64(2 * time.Second),
	}, inbox, 3*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	got := 0
	for _, r := range replies {
		if !r.IsMessage() {
			continue
		}
		got++
		if r.PinID() != pin {
			return fail("message %d carries Nats-Pin-Id=%q want %q", got, r.PinID(), pin)
		}
	}
	if got != 4 {
		return fail("expected 4 messages, got %d", got)
	}
	return pass()
}

func testPG305(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 5*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	pinX, _, err := pinClientFirst(h, stream, cname)
	if err != nil {
		return fail("%v", err)
	}
	// Open a long-lived standby pull from client B (no id).
	bInbox := nats.NewInbox()
	chB, stopB, err := pullStreaming(h, stream, cname, pullRequest{
		Batch:   1,
		Group:   "jobs",
		Expires: int64(30 * time.Second),
	}, bInbox)
	if err != nil {
		return fail("standby pull: %v", err)
	}
	defer stopB()

	// Publish a second message AFTER B is waiting; A is silent so it
	// will be unpinned at ~5s, then B will be selected.
	if _, err := publishMsg(h, h.Subject("a"), []byte("y")); err != nil {
		return fail("publish 2: %v", err)
	}

	// Wait up to 10s for B to receive a message.
	var bRep *pullReply
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rep, ok := drainNext(chB, time.Until(deadline))
		if !ok {
			break
		}
		if rep.IsMessage() {
			bRep = rep
			break
		}
	}
	if bRep == nil {
		return fail("client B did not receive a delivery after pin timeout")
	}
	pinY := bRep.PinID()
	if pinY == "" {
		return fail("client B's message lacked Nats-Pin-Id header")
	}
	if pinY == pinX {
		return fail("pin did not switch: B's id %q equals A's id", pinY)
	}
	info, err := consumerInfo(h, stream, cname)
	if err != nil {
		return fail("consumer info: %v", err)
	}
	if len(info.PriorityGroups) == 0 || info.PriorityGroups[0].PinnedClientId != pinY {
		return fail("priority_groups state did not move to %q (got %+v)", pinY, info.PriorityGroups)
	}

	// A pull from A with old id must now be 423.
	if _, err := publishMsg(h, h.Subject("a"), []byte("z")); err != nil {
		return fail("publish 3: %v", err)
	}
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:   1,
		Group:   "jobs",
		ID:      pinX,
		Expires: int64(1 * time.Second),
	}, "", 2*time.Second)
	if err != nil {
		return fail("post-switch A pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return fail("client A with stale id %q still receiving messages", pinX)
		}
		if r.Status == StatusPinMismatch {
			return pass()
		}
	}
	return fail("expected 423 from A's stale id pull; got replies=%v", summary(replies))
}

func testPG306(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 10*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("0")); err != nil {
		return fail("publish: %v", err)
	}
	pin, inbox, err := pinClientFirst(h, stream, cname)
	if err != nil {
		return fail("%v", err)
	}
	// Pull every 2s for 18s. Each pull publishes 1 fresh message first.
	for i := 1; i < 9; i++ {
		if _, err := publishMsg(h, h.Subject("a"), []byte(fmt.Sprintf("%d", i))); err != nil {
			return fail("publish loop %d: %v", i, err)
		}
		replies, err := pull(h, stream, cname, pullRequest{
			Batch:   1,
			Group:   "jobs",
			ID:      pin,
			Expires: int64(1500 * time.Millisecond),
		}, inbox, 2*time.Second)
		if err != nil {
			return fail("pull loop %d: %v", i, err)
		}
		got := false
		for _, r := range replies {
			if r.IsMessage() {
				if r.PinID() != pin {
					return fail("pin id changed mid-loop: got %q want %q (iter %d)", r.PinID(), pin, i)
				}
				got = true
			}
		}
		if !got {
			return fail("loop iter %d did not deliver a message (replies=%v)", i, summary(replies))
		}
		time.Sleep(2 * time.Second)
	}
	info, err := consumerInfo(h, stream, cname)
	if err != nil {
		return fail("consumer info: %v", err)
	}
	if len(info.PriorityGroups) == 0 || info.PriorityGroups[0].PinnedClientId != pin {
		return fail("pin id rotated unexpectedly: got %+v want %q", info.PriorityGroups, pin)
	}
	return pass()
}

func testPG307(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 30*time.Second)
	if err != nil {
		return fail("%v", err)
	}
	if _, err := publishMsg(h, h.Subject("a"), []byte("x")); err != nil {
		return fail("publish: %v", err)
	}
	// Single pull with NoWait: this is what a "Fetch" looks like. ADR
	// places the no-Fetch rule on clients, not on the server, so this
	// is recorded as inconclusive.
	replies, err := pull(h, stream, cname, pullRequest{
		Batch:  1,
		Group:  "jobs",
		NoWait: true,
	}, "", 2*time.Second)
	if err != nil {
		return fail("pull: %v", err)
	}
	gotMsg := false
	gotErr := false
	for _, r := range replies {
		if r.IsMessage() {
			gotMsg = true
		} else if r.Status != "" && r.Status != "404" {
			gotErr = true
		}
	}
	if gotMsg {
		return inconclusive("server delivered to a Fetch (no_wait) on pinned_client; ADR client guidance, not server-enforced")
	}
	if gotErr {
		return inconclusive("server rejected a Fetch (no_wait) on pinned_client; behaviour recorded")
	}
	return inconclusive("server did not deliver and did not error explicitly; behaviour recorded")
}

// summary renders a slice of pullReplies in a compact, log-friendly
// form for failure messages.
func summary(replies []*pullReply) string {
	if len(replies) == 0 {
		return "<no replies>"
	}
	parts := make([]string, 0, len(replies))
	for i, r := range replies {
		status := r.Status
		if status == "" {
			status = "OK"
		}
		parts = append(parts, fmt.Sprintf("[%d status=%s desc=%q pin=%q data=%dB]", i, status, r.Description, r.PinID(), len(r.Data)))
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += p
	}
	return out
}
