// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ctr500Tests covers CTR-500: subject transforms — counter accounting
// uses the rewritten subject.
func ctr500Tests() []harness.Test {
	return []harness.Test{
		{ID: "CTR-501", Title: "Subject transform — accounting uses rewritten subject", Section: "CTR-500", Tags: []string{"transform"}, Run: testCTR501},
	}
}

func testCTR501(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	srcSubj := h.Subject("es") + ".>"
	destSubj := h.Subject("agg") + ".>"

	cfg := streamConfig{
		Name:            name,
		Subjects:        []string{srcSubj},
		AllowMsgCounter: true,
		SubjectTransform: &subjectTransform{
			Src:  h.Subject("es") + ".>",
			Dest: h.Subject("agg") + ".>",
		},
	}
	_ = destSubj
	if err := createStream(h, cfg); err != nil {
		return fail("stream create: %v", err)
	}

	publishSubj := h.Subject("es") + ".hits"
	rewrittenSubj := h.Subject("agg") + ".hits"

	if ack, err := publishIncr(h, publishSubj, "+1", nil); err != nil || ack.Error != nil {
		return fail("first publish err=%v ack=%+v", err, ack)
	} else if !bigEq(ack.Value, "1") {
		return fail("first ack.val=%q, want 1", ack.Value)
	}

	ack, err := publishIncr(h, publishSubj, "+1", nil)
	if err != nil || ack.Error != nil {
		return fail("second publish err=%v ack=%+v", err, ack)
	}
	if !bigEq(ack.Value, "2") {
		return fail("second ack.val=%q, want 2 (counter must accumulate against rewritten subject)", ack.Value)
	}

	last, err := lastMsgFor(h, name, rewrittenSubj)
	if err != nil || last == nil {
		return fail("last for rewritten subject %s: msg=%v err=%v", rewrittenSubj, last, err)
	}
	if last.Subject != rewrittenSubj {
		return fail("stored subject=%q, want %q (rewritten)", last.Subject, rewrittenSubj)
	}
	val, err := decodeVal(last.Data)
	if err != nil {
		return fail("decode body: %v", err)
	}
	if !bigEq(val, "2") {
		return fail("stored val=%q, want 2", val)
	}
	return pass()
}
