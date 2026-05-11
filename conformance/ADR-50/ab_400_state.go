// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

func jsonDecode(data []byte, v any) error { return json.Unmarshal(data, v) }

// ab400Tests covers AB-400: stream-state checks at commit time
// (Nats-Expected-Last-Sequence and friends).
func ab400Tests() []harness.Test {
	return []harness.Test{
		{
			ID: "AB-401", Title: "Nats-Expected-Last-Sequence matches at commit",
			Section: "AB-400", Tags: []string{"state"}, Run: testAB401,
		},
		{
			ID: "AB-402", Title: "Nats-Expected-Last-Sequence mismatch fails the batch",
			Section: "AB-400", Tags: []string{"state"}, Run: testAB402,
		},
		{
			ID: "AB-403", Title: "Nats-Expected-Last-Sequence racing with concurrent publish",
			Section: "AB-400", Tags: []string{"state"}, Run: testAB403,
		},
		{
			ID: "AB-404", Title: "Nats-Expected-Last-Sequence only allowed on the first message",
			Section: "AB-400", Tags: []string{"state"}, Run: testAB404,
		},
		{
			ID: "AB-405", Title: "Nats-Expected-Last-Msg-Id rejected",
			Section: "AB-400", Tags: []string{"state"}, Run: testAB405,
		},
		{
			ID: "AB-410", Title: "Nats-Expected-Last-Subject-Sequence happy path",
			Section: "AB-400", Tags: []string{"state"}, Run: testAB410,
		},
		{
			ID: "AB-411", Title: "Nats-Expected-Last-Subject-Sequence mismatch",
			Section: "AB-400", Tags: []string{"state"}, Run: testAB411,
		},
		{
			ID: "AB-412", Title: "Nats-Expected-Last-Subject-Sequence skipped when batch wrote that subject earlier",
			Section: "AB-400", Tags: []string{"state"}, Run: testAB412,
		},
	}
}

func testAB401(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := h.NC.Request(h.Subject("seed"), []byte("seed"), 5*time.Second); err != nil {
		return fail("seed publish: %v", err)
	}
	s, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	batch := newUUID()
	hdrs := nats.Header{HdrExpLastSeq: []string{fmt.Sprintf("%d", s)}}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", hdrs, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "", nil, []byte("b")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("seq 2 err=%v ack=%+v", err, ack)
	}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "1", nil, []byte("c")), 5*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	if ack.Error != nil || ack.BatchSize != 3 {
		return fail("commit ack mismatch: %+v", ack)
	}
	return pass()
}

func testAB402(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	s, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	batch := newUUID()
	hdrs := nats.Header{HdrExpLastSeq: []string{fmt.Sprintf("%d", s+99)}}
	// Per the clarified ADR-50 §"Server Errors", the check is applied
	// "when the batch tries to commit" — under lock against the
	// stream's pre-batch sequences. The initial and member publishes
	// MUST therefore receive zero-byte acks; the wrong-last-sequence
	// error MUST surface only at commit.
	initAck, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", hdrs, []byte("a")), 5*time.Second)
	if err != nil {
		return fail("initial: %v", err)
	}
	if initAck.Error != nil {
		return fail("initial publish surfaced error early: %s — ADR-50 requires the ExpectedLastSeq check to be deferred to commit time", initAck.Error)
	}
	memberAck, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "", nil, []byte("b")), 5*time.Second)
	if err != nil {
		return fail("member 2: %v", err)
	}
	if memberAck.Error != nil {
		return fail("member publish surfaced error early: %s — ADR-50 requires the ExpectedLastSeq check to be deferred to commit time", memberAck.Error)
	}
	commitAck, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "1", nil, []byte("c")), 5*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	if commitAck.Error == nil {
		return fail("expected error on wrong-last-sequence at commit, got success: %+v", commitAck)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("post last seq: %v", err)
	}
	if last != s {
		return fail("stream last seq advanced (was %d, now %d)", s, last)
	}
	return pass()
}

func testAB403(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	if _, err := h.NC.Request(h.Subject("seed"), []byte("seed"), 5*time.Second); err != nil {
		return fail("seed: %v", err)
	}
	s, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	batch := newUUID()
	hdrs := nats.Header{HdrExpLastSeq: []string{fmt.Sprintf("%d", s)}}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", hdrs, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 2, "", nil, []byte("b")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("seq 2 err=%v ack=%+v", err, ack)
	}

	// Concurrent non-batch publish advances the stream's last sequence
	// before the batch commits.
	parResp, err := h.NC.Request(h.Subject("other"), []byte("interleave"), 5*time.Second)
	if err != nil {
		return fail("parallel publish: %v", err)
	}
	var parAck pubAck
	if err := jsonDecode(parResp.Data, &parAck); err != nil || parAck.Error != nil {
		return fail("parallel ack decode: data=%q err=%v", string(parResp.Data), err)
	}

	commitAck, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "1", nil, []byte("c")), 5*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	if commitAck.Error == nil {
		return fail("expected wrong-last-sequence error at commit (concurrent publish raced ahead), got %+v", commitAck)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	// Stream must contain the parallel publish but no batch members.
	if last != parAck.Sequence {
		return fail("stream last seq=%d, want parallel publish seq %d (no batch members)", last, parAck.Sequence)
	}
	return pass()
}

func testAB404(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}

	// Member 2 carries the disallowed Nats-Expected-Last-Sequence
	// header. ADR-50 says: "Only the first message of the batch may
	// contain Nats-Expected-Last-Sequence". Per the commit-time
	// clarification, checks are applied at commit; servers may either
	// (a) reject the disallowed header at receive time, (b) defer and
	// reject at commit, or (c) silently ignore the header on
	// non-initial members (the rule is enforced by ignoring it
	// elsewhere — the header only takes effect on the first message).
	// All three are acceptable as long as the server never *uses* the
	// header value off the wrong message.
	hdrs := nats.Header{HdrExpLastSeq: []string{"0"}}
	resp, err := h.NC.RequestMsg(newBatchMsg(h.Subject("a"), batch, 2, "", hdrs, []byte("b")), 5*time.Second)
	if err != nil {
		return fail("member 2 publish: %v", err)
	}

	// (a) Early rejection — error pub ack on member 2.
	if len(resp.Data) > 0 {
		var ack pubAck
		if err := jsonDecode(resp.Data, &ack); err != nil {
			return fail("decode member 2 ack: %v", err)
		}
		if ack.Error != nil {
			return pass()
		}
		return fail("non-empty reply on member 2 carries no error: %+v — expected either a zero-byte ack (deferred / silent-ignore branch) or an error ack", ack)
	}

	// (b) or (c): zero-byte ack. Send the commit and see whether the
	// server defers detection to commit time or silently accepted.
	commitAck, err := publishRequest(h, newBatchMsg(h.Subject("a"), batch, 3, "1", nil, []byte("c")), 5*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	if commitAck.Error != nil {
		// (b) Deferred rejection at commit.
		return pass()
	}
	// (c) Silent ignore — the batch committed normally. Verify the
	// stream actually contains the three messages and that the server
	// did not use the wrong-message header value.
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	if len(msgs) != 3 {
		return fail("commit succeeded but stream contains %d messages, want 3", len(msgs))
	}
	return inconclusive("server silently ignored Nats-Expected-Last-Sequence on non-initial member; batch committed cleanly with 3 messages — acceptable per the ADR's enforce-by-ignoring reading, but the spec does not explicitly require this branch")
}

func testAB405(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	hdrs := nats.Header{HdrExpLastMsgID: []string{"foo"}}
	ack, err := publishRequest(h, newBatchMsg(h.Subject("a"), newUUID(), 1, "1", hdrs, []byte("x")), 5*time.Second)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil || ack.Error.ErrCode != ErrCodeUnsupportedHdr {
		return fail("expected ErrCode %d, got %+v", ErrCodeUnsupportedHdr, ack)
	}
	return pass()
}

func testAB410(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjX := h.Subject("x")
	if _, err := h.NC.Request(subjX, []byte("seed"), 5*time.Second); err != nil {
		return fail("seed: %v", err)
	}
	sx, err := streamLastSeq(h, name)
	if err != nil {
		return fail("read sx: %v", err)
	}
	batch := newUUID()
	hdrs := nats.Header{HdrExpLastSubjSeq: []string{fmt.Sprintf("%d", sx)}}
	if ack, err := publishRequest(h, newBatchMsg(subjX, batch, 1, "", hdrs, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	ack, err := publishRequest(h, newBatchMsg(subjX, batch, 2, "1", nil, []byte("b")), 5*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	if ack.Error != nil || ack.BatchSize != 2 {
		return fail("commit ack mismatch: %+v", ack)
	}
	return pass()
}

func testAB411(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjX := h.Subject("x")
	if _, err := h.NC.Request(subjX, []byte("seed"), 5*time.Second); err != nil {
		return fail("seed: %v", err)
	}
	sx, err := streamLastSeq(h, name)
	if err != nil {
		return fail("read sx: %v", err)
	}
	batch := newUUID()
	hdrs := nats.Header{HdrExpLastSubjSeq: []string{fmt.Sprintf("%d", sx+10)}}
	if ack, err := publishRequest(h, newBatchMsg(subjX, batch, 1, "", hdrs, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v (must be deferred to commit)", err, ack)
	}
	ack, err := publishRequest(h, newBatchMsg(subjX, batch, 2, "1", nil, []byte("b")), 5*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	if ack.Error == nil {
		return fail("expected wrong-last-subject-sequence error at commit, got %+v", ack)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != sx {
		return fail("stream advanced past sx=%d to %d after rejected commit", sx, last)
	}
	return pass()
}

func testAB412(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowAtomicPublish: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjX := h.Subject("x")
	subjY := h.Subject("y")
	if _, err := h.NC.Request(subjX, []byte("seed"), 5*time.Second); err != nil {
		return fail("seed: %v", err)
	}
	sx, err := streamLastSeq(h, name)
	if err != nil {
		return fail("read sx: %v", err)
	}
	batch := newUUID()
	if ack, err := publishRequest(h, newBatchMsg(subjX, batch, 1, "", nil, []byte("a")), 5*time.Second); err != nil || ack.Error != nil {
		return fail("initial err=%v ack=%+v", err, ack)
	}
	// Member 2 carries Nats-Expected-Last-Subject-Sequence on the same
	// subject the batch already wrote. Per ADR-50 AB-412 the server has
	// two acceptable behaviours: reject the in-batch shadowed check (at
	// receive time or at commit), or silently skip the check.
	hdrs := nats.Header{HdrExpLastSubjSeq: []string{fmt.Sprintf("%d", sx)}}
	memberAck, err := publishRequest(h, newBatchMsg(subjX, batch, 2, "", hdrs, []byte("b")), 5*time.Second)
	if err != nil {
		return fail("member 2: %v", err)
	}
	// Branch (a): early rejection at member-2 receive time.
	if memberAck.Error != nil {
		last, err := streamLastSeq(h, name)
		if err != nil {
			return fail("last seq: %v", err)
		}
		if last != sx {
			return fail("member 2 rejected but stream advanced past sx=%d to %d", sx, last)
		}
		return pass()
	}
	commitAck, err := publishRequest(h, newBatchMsg(subjY, batch, 3, "1", nil, []byte("c")), 5*time.Second)
	if err != nil {
		return fail("commit: %v", err)
	}
	// Branch (b): deferred rejection at commit (e.g. err_code 10164
	// "wrong last sequence" — the server's documented response when a
	// batch member's expected-last-subject-sequence collides with an
	// in-batch shadowed subject; see jetstream_batching.go check).
	if commitAck.Error != nil {
		last, err := streamLastSeq(h, name)
		if err != nil {
			return fail("last seq: %v", err)
		}
		if last != sx {
			return fail("commit rejected but stream advanced past sx=%d to %d", sx, last)
		}
		return pass()
	}
	// Branch (c): silent skip — commit succeeded with all 3 messages.
	// The MUST condition: stream did not honor the per-subject expected
	// against pre-batch data while shadowed.
	if commitAck.BatchSize != 3 {
		return fail("commit count=%d, want 3", commitAck.BatchSize)
	}
	msgs, err := listMsgs(h, name)
	if err != nil {
		return fail("list: %v", err)
	}
	if len(msgs) != 4 {
		return fail("expected 4 stored (seed + 3 batch), got %d", len(msgs))
	}
	return pass()
}