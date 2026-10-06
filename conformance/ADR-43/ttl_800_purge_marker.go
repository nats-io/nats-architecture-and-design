// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr43

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ttl800Tests covers TTL-800: markers placed by the subject-scoped
// purge API. ADR-43 lists this as a future feature; until a server is
// observed implementing it the suite reports INCONCLUSIVE.
func ttl800Tests() []harness.Test {
	return []harness.Test{
		{ID: "TTL-801", Title: "Purge subject places a marker", Section: "TTL-800", Tags: []string{"marker", "future"}, Run: testTTL801},
		{ID: "TTL-802", Title: "Purge with keep does NOT place a marker", Section: "TTL-800", Tags: []string{"marker"}, Run: testTTL802},
	}
}

func testTTL801(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	for i := 0; i < 3; i++ {
		if ack, err := publishWithTTL(h, subj, "", []byte{byte('a' + i)}); err != nil || ack.Error != nil {
			return fail("publish %d: err=%v ack=%+v", i, err, ack)
		}
	}
	if err := purgeSubject(h, name, subj, 0); err != nil {
		return fail("purge: %v", err)
	}
	marker := waitForMarker(h, name, subj, MarkerReasonPurge, 3*time.Second)
	if marker == nil {
		last, err := lastMsgFor(h, name, subj)
		if err != nil {
			return fail("last for %s: %v", subj, err)
		}
		if last != nil {
			return fail("purge left a non-marker residual on %s: seq=%d data=%q", subj, last.Sequence, string(last.Data))
		}
		return inconclusive("no Purge marker observed (ADR-43 marks purge-marker as future); subject is correctly empty")
	}
	if got := marker.Header.Get(HdrMarkerReason); got != MarkerReasonPurge {
		return fail("marker Nats-Marker-Reason=%q, want %q", got, MarkerReasonPurge)
	}
	if hdrTTL := marker.Header.Get(HdrTTL); !markerTTLMatchesConfig(hdrTTL, markerTTL) {
		return fail("marker Nats-TTL=%q, want value equal to SubjectDeleteMarkerTTL=%s", hdrTTL, markerTTL)
	}
	return pass()
}

func testTTL802(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
	}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	for i := 0; i < 3; i++ {
		if ack, err := publishWithTTL(h, subj, "", []byte{byte('a' + i)}); err != nil || ack.Error != nil {
			return fail("publish %d: err=%v ack=%+v", i, err, ack)
		}
	}
	if err := purgeSubject(h, name, subj, 1); err != nil {
		return fail("purge keep=1: %v", err)
	}
	last, err := lastMsgFor(h, name, subj)
	if err != nil || last == nil {
		return fail("last for %s: err=%v nil=%v", subj, err, last == nil)
	}
	if reason := last.Header.Get(HdrMarkerReason); reason != "" {
		return fail("marker placed on %s with reason %q despite purge keeping 1 message", subj, reason)
	}
	if string(last.Data) != "c" {
		return fail("last for %s payload=%q, want %q (most recent original publish)", subj, string(last.Data), "c")
	}
	return pass()
}
