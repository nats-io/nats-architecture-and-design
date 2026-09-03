// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr31

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// dg500Tests covers DG-500: multi-subject Direct Get
// (`multi_last`, `up_to_seq`, `up_to_time`).
func dg500Tests() []harness.Test {
	return []harness.Test{
		{ID: "DG-501", Title: "multi_last returns last message for each listed subject", Section: "DG-500", Tags: []string{"multi", "api-level-1"}, Run: testDG501},
		{ID: "DG-502", Title: "multi_last with wildcard", Section: "DG-500", Tags: []string{"multi", "api-level-1"}, Run: testDG502},
		{ID: "DG-503", Title: "multi_last with up_to_seq returns historical state", Section: "DG-500", Tags: []string{"multi", "api-level-1"}, Run: testDG503},
		{ID: "DG-504", Title: "multi_last with up_to_time returns point-in-time state", Section: "DG-500", Tags: []string{"multi", "api-level-1"}, Run: testDG504},
		{ID: "DG-505", Title: "multi_last with batch size limit", Section: "DG-500", Tags: []string{"multi", "api-level-1"}, Run: testDG505},
		{ID: "DG-506", Title: "multi_last returns 413 when too many subjects match", Section: "DG-500", Tags: []string{"multi", "api-level-1", "resource-intensive"}, SkipReason: requiresResourceIntensive(), Run: testDG506},
		{ID: "DG-507", Title: "Exactly 1024 matched subjects is allowed", Section: "DG-500", Tags: []string{"multi", "api-level-1", "resource-intensive"}, SkipReason: requiresResourceIntensive(), Run: testDG507},
		{ID: "DG-508", Title: "multi_last chained reads via Nats-UpTo-Sequence are consistent", Section: "DG-500", Tags: []string{"multi", "api-level-1"}, Run: testDG508},
	}
}

// successPayloads collects the message-body payloads from a multi-mode
// reply slice, indexed by Nats-Subject so callers can assert per-key.
func successPayloads(replies []*dgReply) map[string]string {
	out := map[string]string{}
	for _, r := range replies {
		if !r.IsSuccess() {
			continue
		}
		out[r.Headers.Get(HdrSubject)] = string(r.Data)
	}
	return out
}

func testDG501(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("multi_last requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("users.1234.name"), []byte("Bob"), nil); err != nil {
		return fail("publish name: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("users.1234.surname"), []byte("Smith"), nil); err != nil {
		return fail("publish surname: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("users.1234.address"), []byte("1 Main Street"), nil); err != nil {
		return fail("publish address v1: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("users.1234.address"), []byte("10 Oak Lane"), nil); err != nil {
		return fail("publish address v2: %v", err)
	}

	replies, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject("users.1234.name"), h.Subject("users.1234.address")},
	}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) < 3 || !replies[len(replies)-1].IsEOB() {
		return fail("expected 2 msgs + EOB, got %s", summary(replies))
	}
	got := successPayloads(replies)
	if got[h.Subject("users.1234.name")] != "Bob" {
		return fail("name payload mismatch: %v", got)
	}
	if got[h.Subject("users.1234.address")] != "10 Oak Lane" {
		return fail("address payload mismatch: %v", got)
	}
	if _, ok := replies[len(replies)-1].HeaderUint(HdrUpToSeq); !ok {
		return fail("EOB missing Nats-UpTo-Sequence: %s", summary(replies))
	}
	return pass()
}

func testDG502(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("multi_last requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	for _, p := range []struct{ subj, val string }{
		{h.Subject("users.1234.name"), "Bob"},
		{h.Subject("users.1234.surname"), "Smith"},
		{h.Subject("users.1234.address"), "1 Main Street"},
		{h.Subject("users.1234.address"), "10 Oak Lane"},
	} {
		if _, err := publishMsg(h, p.subj, []byte(p.val), nil); err != nil {
			return fail("publish %s: %v", p.subj, err)
		}
	}
	replies, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject("users.1234.>")},
	}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) < 4 || !replies[len(replies)-1].IsEOB() {
		return fail("expected 3 msgs + EOB, got %s", summary(replies))
	}
	got := successPayloads(replies)
	if got[h.Subject("users.1234.address")] != "10 Oak Lane" {
		return fail("address mismatch (expected 10 Oak Lane): %v", got)
	}
	if got[h.Subject("users.1234.name")] != "Bob" {
		return fail("name mismatch: %v", got)
	}
	if got[h.Subject("users.1234.surname")] != "Smith" {
		return fail("surname mismatch: %v", got)
	}
	return pass()
}

func testDG503(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("multi_last + up_to_seq requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("users.1234.name"), []byte("Bob"), nil); err != nil {
		return fail("publish name: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("users.1234.surname"), []byte("Smith"), nil); err != nil {
		return fail("publish surname: %v", err)
	}
	sAddrV1, err := publishMsg(h, h.Subject("users.1234.address"), []byte("1 Main Street"), nil)
	if err != nil {
		return fail("publish address v1: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("users.1234.address"), []byte("10 Oak Lane"), nil); err != nil {
		return fail("publish address v2: %v", err)
	}

	replies, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject("users.1234.>")},
		UpToSeq:      sAddrV1,
	}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) < 1 || !replies[len(replies)-1].IsEOB() {
		return fail("expected EOB, got %s", summary(replies))
	}
	got := successPayloads(replies)
	if got[h.Subject("users.1234.address")] != "1 Main Street" {
		return fail("expected historical address %q, got %q", "1 Main Street", got[h.Subject("users.1234.address")])
	}
	upTo, _ := replies[len(replies)-1].HeaderUint(HdrUpToSeq)
	if upTo != sAddrV1 {
		return fail("EOB Nats-UpTo-Sequence=%d, want %d", upTo, sAddrV1)
	}
	return pass()
}

func testDG504(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("multi_last + up_to_time requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("users.1234.name"), []byte("Bob"), nil); err != nil {
		return fail("publish name: %v", err)
	}
	if _, err := publishMsg(h, h.Subject("users.1234.address"), []byte("1 Main Street"), nil); err != nil {
		return fail("publish address v1: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	tBetween := time.Now().UTC()
	time.Sleep(50 * time.Millisecond)
	if _, err := publishMsg(h, h.Subject("users.1234.address"), []byte("10 Oak Lane"), nil); err != nil {
		return fail("publish address v2: %v", err)
	}

	replies, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject("users.1234.>")},
		UpToTime:     &tBetween,
	}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) < 1 || !replies[len(replies)-1].IsEOB() {
		return fail("expected EOB, got %s", summary(replies))
	}
	got := successPayloads(replies)
	if got[h.Subject("users.1234.address")] != "1 Main Street" {
		return fail("expected historical address %q at up_to_time, got %q", "1 Main Street", got[h.Subject("users.1234.address")])
	}
	return pass()
}

func testDG505(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("multi_last + batch requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	for i := 1; i <= 5; i++ {
		if _, err := publishMsg(h, h.Subject(fmt.Sprintf("users.1234.k%d", i)), []byte{byte('a' + i - 1)}, nil); err != nil {
			return fail("publish %d: %v", i, err)
		}
	}
	replies, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject("users.1234.>")},
		Batch:        2,
	}, defaultDirectGetTimeout)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) != 3 {
		return fail("expected 2 msgs + EOB, got %d (%s)", len(replies), summary(replies))
	}
	if !replies[2].IsEOB() {
		return fail("third reply not EOB: %s", summary(replies))
	}
	pending, _ := replies[2].HeaderUint(HdrNumPending)
	if pending == 0 {
		return fail("expected Nats-Num-Pending > 0, got 0")
	}
	upTo, ok := replies[2].HeaderUint(HdrUpToSeq)
	if !ok {
		return fail("EOB missing Nats-UpTo-Sequence: %s", summary(replies))
	}
	lastSeq, ok := replies[2].HeaderUint(HdrLastSeq)
	if !ok {
		return fail("EOB missing Nats-Last-Sequence: %s", summary(replies))
	}

	// Page 2: keep the same point-in-time snapshot via up_to_seq, and
	// advance the cursor with seq = lastSeq + 1 so the server skips
	// past messages already delivered.
	replies2, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject("users.1234.>")},
		Batch:        5,
		Seq:          lastSeq + 1,
		UpToSeq:      upTo,
	}, defaultDirectGetTimeout)
	if err != nil {
		return fail("follow-up direct get: %v", err)
	}
	if len(replies2) < 1 || !replies2[len(replies2)-1].IsEOB() {
		return fail("follow-up expected EOB, got %s", summary(replies2))
	}
	got1 := successPayloads(replies)
	got2 := successPayloads(replies2)
	for k := range got1 {
		if _, dup := got2[k]; dup {
			return fail("subject %q appeared in both batches: %v / %v", k, got1, got2)
		}
	}
	if len(got1)+len(got2) != 5 {
		return fail("expected 5 distinct subjects across 2 batches, got %d (%v / %v)", len(got1)+len(got2), got1, got2)
	}
	finalPending, _ := replies2[len(replies2)-1].HeaderUint(HdrNumPending)
	if finalPending != 0 {
		return fail("expected page 2 EOB Nats-Num-Pending=0, got %d", finalPending)
	}
	return pass()
}

func testDG506(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("multi_last requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	for i := 0; i < 1025; i++ {
		if _, err := publishMsg(h, h.Subject(fmt.Sprintf("k.%d", i)), []byte("v"), nil); err != nil {
			return fail("publish %d: %v", i, err)
		}
	}
	replies, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject("k.>")},
	}, 5*time.Second)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) != 1 {
		return fail("expected single 413 reply, got %d (%s)", len(replies), summary(replies))
	}
	if replies[0].Status != StatusTooMany {
		return fail("expected status %s, got %q (%s)", StatusTooMany, replies[0].Status, summary(replies))
	}
	return pass()
}

func testDG507(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("multi_last requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	for i := 0; i < 1024; i++ {
		if _, err := publishMsg(h, h.Subject(fmt.Sprintf("k.%d", i)), []byte("v"), nil); err != nil {
			return fail("publish %d: %v", i, err)
		}
	}
	replies, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject("k.>")},
	}, 10*time.Second)
	if err != nil {
		return fail("direct get: %v", err)
	}
	if len(replies) < 1 || !replies[len(replies)-1].IsEOB() {
		return fail("expected EOB, got %d replies (last status %q)", len(replies), replies[len(replies)-1].Status)
	}
	msgCount := 0
	for _, r := range replies {
		if r.IsSuccess() {
			msgCount++
		}
	}
	if msgCount != 1024 {
		return fail("expected exactly 1024 success replies, got %d", msgCount)
	}
	return pass()
}

func testDG508(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	if h.APILevel < 1 {
		return skip("multi_last + up_to_seq chaining requires server >= 2.11 (API level >= 1); got %d", h.APILevel)
	}
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowDirect: true}); err != nil {
		return fail("stream create: %v", err)
	}
	for i := 1; i <= 4; i++ {
		if _, err := publishMsg(h, h.Subject(fmt.Sprintf("k%d", i)), []byte{byte('a' + i - 1)}, nil); err != nil {
			return fail("publish %d: %v", i, err)
		}
	}
	first, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject(">")},
		Batch:        2,
	}, defaultDirectGetTimeout)
	if err != nil {
		return fail("first direct get: %v", err)
	}
	if len(first) < 3 || !first[len(first)-1].IsEOB() {
		return fail("first request expected 2 msgs + EOB, got %s", summary(first))
	}
	upTo, ok := first[len(first)-1].HeaderUint(HdrUpToSeq)
	if !ok {
		return fail("first EOB missing Nats-UpTo-Sequence: %s", summary(first))
	}
	lastSeq, ok := first[len(first)-1].HeaderUint(HdrLastSeq)
	if !ok {
		return fail("first EOB missing Nats-Last-Sequence: %s", summary(first))
	}
	got1 := successPayloads(first)

	// Mutate ground truth: overwrite a key already returned by request 1.
	// The new write lands at a sequence > upTo, so a snapshot-anchored
	// page 2 must NOT see it. Without the snapshot anchor, the post-
	// mutation value would replace the original on the second read.
	for k := range got1 {
		if _, err := publishMsg(h, k, []byte("MUTATED"), nil); err != nil {
			return fail("mutate publish: %v", err)
		}
		break
	}

	// Page 2: keep the snapshot via up_to_seq, advance via seq.
	second, err := directGet(h, name, directGetReq{
		MultiLastFor: []string{h.Subject(">")},
		Batch:        2,
		Seq:          lastSeq + 1,
		UpToSeq:      upTo,
	}, defaultDirectGetTimeout)
	if err != nil {
		return fail("second direct get: %v", err)
	}
	if len(second) < 1 || !second[len(second)-1].IsEOB() {
		return fail("second request expected EOB, got %s", summary(second))
	}
	got2 := successPayloads(second)
	for k, v := range got2 {
		if v == "MUTATED" {
			return fail("subject %q reflects post-up_to_seq mutation; expected point-in-time read", k)
		}
	}
	for k := range got1 {
		if _, dup := got2[k]; dup {
			return fail("subject %q appeared in both pages of chained read", k)
		}
	}
	return pass()
}
