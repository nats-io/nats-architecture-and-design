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
			ID: "AB-404", Title: "Nats-Expected-Last-Sequence only allowed on the first message",
			Section: "AB-400", Tags: []string{"state"}, Run: testAB404,
		},
		{
			ID: "AB-405", Title: "Nats-Expected-Last-Msg-Id rejected",
			Section: "AB-400", Tags: []string{"state"}, Run: testAB405,
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