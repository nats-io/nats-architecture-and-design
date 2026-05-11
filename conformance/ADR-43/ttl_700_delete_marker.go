// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr43

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ttl700Tests covers TTL-700: markers placed by the per-message delete
// API. ADR-43 lists this as a future feature; until a server is
// observed implementing it the suite reports INCONCLUSIVE.
func ttl700Tests() []harness.Test {
	return []harness.Test{
		{ID: "TTL-701", Title: "Delete API places a marker on the now-empty subject", Section: "TTL-700", Tags: []string{"marker", "future"}, Run: testTTL701},
		{ID: "TTL-702", Title: "Delete API: deleting non-last message does NOT place a marker", Section: "TTL-700", Tags: []string{"marker"}, Run: testTTL702},
	}
}

func testTTL701(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	markerTTL := 60 * time.Second
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(markerTTL),
	}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	ack, err := publishWithTTL(h, subj, "", []byte("x"))
	if err != nil || ack.Error != nil {
		return fail("publish: err=%v ack=%+v", err, ack)
	}
	if err := deleteMsg(h, name, ack.Sequence); err != nil {
		return fail("delete msg: %v", err)
	}
	// Briefly poll for the marker. ADR-43 marks this as future.
	marker := waitForMarker(h, name, subj, MarkerReasonRemove, 3*time.Second)
	if marker == nil {
		// No marker observed. Confirm the subject is empty (i.e. the
		// delete worked even without a marker).
		last, err := lastMsgFor(h, name, subj)
		if err != nil {
			return fail("last for %s: %v", subj, err)
		}
		if last != nil {
			return fail("delete left a non-marker residual on %s: seq=%d data=%q", subj, last.Sequence, string(last.Data))
		}
		return inconclusive("no Remove marker observed (ADR-43 marks delete-marker as future); subject is correctly empty")
	}
	if got := marker.Header.Get(HdrMarkerReason); got != MarkerReasonRemove {
		return fail("marker Nats-Marker-Reason=%q, want %q", got, MarkerReasonRemove)
	}
	if hdrTTL := marker.Header.Get(HdrTTL); !markerTTLMatchesConfig(hdrTTL, markerTTL) {
		return fail("marker Nats-TTL=%q, want value equal to SubjectDeleteMarkerTTL=%s", hdrTTL, markerTTL)
	}
	return pass()
}

func testTTL702(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
	}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	ackA, err := publishWithTTL(h, subj, "", []byte("A"))
	if err != nil || ackA.Error != nil {
		return fail("publish A: err=%v ack=%+v", err, ackA)
	}
	ackB, err := publishWithTTL(h, subj, "", []byte("B"))
	if err != nil || ackB.Error != nil {
		return fail("publish B: err=%v ack=%+v", err, ackB)
	}
	if err := deleteMsg(h, name, ackA.Sequence); err != nil {
		return fail("delete msg A: %v", err)
	}
	last, err := lastMsgFor(h, name, subj)
	if err != nil || last == nil {
		return fail("last for %s: err=%v nil=%v", subj, err, last == nil)
	}
	if reason := last.Header.Get(HdrMarkerReason); reason != "" {
		return fail("marker placed on %s with reason %q despite the subject still having a live message", subj, reason)
	}
	if string(last.Data) != "B" {
		return fail("last for %s payload=%q, want %q", subj, string(last.Data), "B")
	}
	return pass()
}
