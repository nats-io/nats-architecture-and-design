// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr42

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// pg400Tests covers PG-400: the UNPIN administrative API.
func pg400Tests() []harness.Test {
	return []harness.Test{
		{ID: "PG-401", Title: "UNPIN clears the pin and forces a switch", Section: "PG-400", Tags: []string{"pinned", "unpin"}, Run: testPG401},
		{ID: "PG-402", Title: "UNPIN with unknown group returns an error", Section: "PG-400", Tags: []string{"pinned", "unpin"}, Run: testPG402},
		{ID: "PG-403", Title: "UNPIN on a non-pinned_client consumer returns an error", Section: "PG-400", Tags: []string{"pinned", "unpin"}, Run: testPG403},
		{ID: "PG-404", Title: "UNPIN with malformed payload returns an error", Section: "PG-400", Tags: []string{"pinned", "unpin"}, Run: testPG404},
	}
}

// hasError returns true when the JSON body decodes to {"error":{...}}.
func hasError(body []byte) bool {
	var resp struct {
		Error *apiError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false
	}
	return resp.Error != nil
}

func testPG401(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makePinnedConsumer(h, 60*time.Second)
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

	// Publish a message destined for the new pinned client.
	if _, err := publishMsg(h, h.Subject("a"), []byte("y")); err != nil {
		return fail("publish 2: %v", err)
	}
	body, err := unpinGroup(h, stream, cname, "jobs")
	if err != nil {
		return fail("unpin: %v", err)
	}
	if hasError(body) {
		return fail("UNPIN returned error: %s", string(body))
	}

	// B should now receive the message with a different pin id.
	var bRep *pullReply
	deadline := time.Now().Add(3 * time.Second)
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
		return fail("client B did not receive a delivery within 3s of UNPIN")
	}
	pinY := bRep.PinID()
	if pinY == "" || pinY == pinX {
		return fail("expected new pin id != %q, got %q", pinX, pinY)
	}

	// A pull from A with the old id must now be 423.
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
		return fail("post-unpin A pull: %v", err)
	}
	for _, r := range replies {
		if r.IsMessage() {
			return fail("client A with stale id still receiving messages after UNPIN")
		}
		if r.Status == StatusPinMismatch {
			return pass()
		}
	}
	return fail("expected 423 from A's stale-id pull after UNPIN; got %v", summary(replies))
}

func testPG402(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	body, err := unpinGroup(h, stream, cname, "ghost")
	if err != nil {
		return fail("unpin: %v", err)
	}
	if !hasError(body) {
		return fail("UNPIN with unknown group expected error reply, got %s", string(body))
	}
	info, err := consumerInfo(h, stream, cname)
	if err != nil {
		return fail("consumer info: %v", err)
	}
	if len(info.PriorityGroups) == 0 || info.PriorityGroups[0].PinnedClientId != pin {
		return fail("pin on 'jobs' was unexpectedly cleared (state=%+v)", info.PriorityGroups)
	}
	return pass()
}

func testPG403(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream, cname, err := makeOverflowConsumer(h)
	if err != nil {
		return fail("%v", err)
	}
	body, err := unpinGroup(h, stream, cname, "jobs")
	if err != nil {
		return fail("unpin: %v", err)
	}
	if !hasError(body) {
		return fail("UNPIN on overflow consumer expected error reply, got %s", string(body))
	}
	return pass()
}

func testPG404(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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

	for _, payload := range [][]byte{nil, []byte("{not json")} {
		body, err := unpin(h, stream, cname, payload)
		if err != nil {
			return fail("unpin (payload %q): %v", string(payload), err)
		}
		if !hasError(body) {
			return fail("UNPIN with malformed payload %q expected error reply, got %s", string(payload), string(body))
		}
	}
	return pass()
}
