// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr43

import (
	"context"
	"strings"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ttl500Tests covers TTL-500: server-placed markers when MaxAge or
// per-message TTL empties a subject.
func ttl500Tests() []harness.Test {
	slow := requiresSlow()
	return []harness.Test{
		{ID: "TTL-501", Title: "MaxAge removal of last value places a marker", Section: "TTL-500", Tags: []string{"marker", "maxage", "slow"}, SkipReason: slow, Run: testTTL501},
		{ID: "TTL-502", Title: "MaxAge removal that does not empty the subject does not place a marker", Section: "TTL-500", Tags: []string{"marker", "maxage", "slow"}, SkipReason: slow, Run: testTTL502},
		{ID: "TTL-503", Title: "Marker is itself subject to its own Nats-TTL", Section: "TTL-500", Tags: []string{"marker", "maxage", "slow"}, SkipReason: slow, Run: testTTL503},
		{ID: "TTL-504", Title: "Nats-TTL removal of last value places a marker", Section: "TTL-500", Tags: []string{"marker", "ttl-expiry", "slow"}, SkipReason: slow, Run: testTTL504},
		{ID: "TTL-505", Title: "Markers off when SubjectDeleteMarkerTTL is unset", Section: "TTL-500", Tags: []string{"marker", "slow"}, SkipReason: slow, Run: testTTL505},
		{ID: "TTL-506", Title: "SubjectDeleteMarkerTTL is a soft floor for Nats-TTL", Section: "TTL-500", Tags: []string{"config"}, Run: testTTL506},
	}
}

// markerTTLMatchesConfig is true when the marker's Nats-TTL header
// represents the configured SubjectDeleteMarkerTTL. Accepts both
// duration strings (e.g. "60s", "1m0s") and bare integer-second forms
// (e.g. "60").
func markerTTLMatchesConfig(headerVal string, want time.Duration) bool {
	if headerVal == "" {
		return false
	}
	if d, err := time.ParseDuration(headerVal); err == nil {
		return d == want
	}
	// Bare integer = seconds.
	if d, err := time.ParseDuration(headerVal + "s"); err == nil {
		return d == want
	}
	return false
}

func testTTL501(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	markerTTL := 60 * time.Second
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(markerTTL),
		MaxAge:                 int64(3 * time.Second),
	}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	ack, err := publishWithTTL(h, subj, "", []byte("x"))
	if err != nil || ack.Error != nil {
		return fail("publish: err=%v ack=%+v", err, ack)
	}
	marker := waitForMarker(h, name, subj, MarkerReasonMaxAge, 10*time.Second)
	if marker == nil {
		return fail("no MaxAge marker observed on %s within 10s after MaxAge expiry", subj)
	}
	if got := marker.Header.Get(HdrMarkerReason); got != MarkerReasonMaxAge {
		return fail("marker Nats-Marker-Reason=%q, want %q", got, MarkerReasonMaxAge)
	}
	if len(marker.Data) != 0 {
		return fail("marker payload non-empty (%d bytes)", len(marker.Data))
	}
	if hdrTTL := marker.Header.Get(HdrTTL); !markerTTLMatchesConfig(hdrTTL, markerTTL) {
		return fail("marker Nats-TTL=%q, want a value equal to SubjectDeleteMarkerTTL=%s", hdrTTL, markerTTL)
	}
	return pass()
}

func testTTL502(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
		MaxAge:                 int64(4 * time.Second),
	}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	ackA, err := publishWithTTL(h, subj, "", []byte("A"))
	if err != nil || ackA.Error != nil {
		return fail("publish A: err=%v ack=%+v", err, ackA)
	}
	time.Sleep(2 * time.Second)
	ackB, err := publishWithTTL(h, subj, "", []byte("B"))
	if err != nil || ackB.Error != nil {
		return fail("publish B: err=%v ack=%+v", err, ackB)
	}
	// Wait until A has had time to be removed by MaxAge but B has not.
	time.Sleep(4 * time.Second)

	last, err := lastMsgFor(h, name, subj)
	if err != nil || last == nil {
		return fail("last for %s: err=%v nil=%v", subj, err, last == nil)
	}
	if last.Header.Get(HdrMarkerReason) != "" {
		return fail("a marker was placed on %s but the subject was not empty (msg B should still be live)", subj)
	}
	if string(last.Data) != "B" {
		return fail("last for %s payload=%q, want %q", subj, string(last.Data), "B")
	}
	return pass()
}

func testTTL503(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	markerTTL := 2 * time.Second
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(markerTTL),
		MaxAge:                 int64(3 * time.Second),
	}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	if ack, err := publishWithTTL(h, subj, "", []byte("x")); err != nil || ack.Error != nil {
		return fail("publish: err=%v ack=%+v", err, ack)
	}
	marker := waitForMarker(h, name, subj, MarkerReasonMaxAge, 10*time.Second)
	if marker == nil {
		return fail("no MaxAge marker observed within 10s")
	}
	// Wait long enough for the marker itself to expire.
	time.Sleep(4 * time.Second)
	last, err := lastMsgFor(h, name, subj)
	if err != nil {
		return fail("last for %s: %v", subj, err)
	}
	if last != nil && last.Header.Get(HdrMarkerReason) == MarkerReasonMaxAge && last.Sequence == marker.Sequence {
		return fail("marker at seq %d still present after marker TTL of %s elapsed", marker.Sequence, markerTTL)
	}
	return pass()
}

func testTTL504(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// Per-message TTL expiry that empties a subject must place a marker
	// with reason "MaxAge". SubjectDeleteMarkerTTL is a soft floor on
	// Nats-TTL (see TTL-506), so it must be <= the per-message TTL or
	// the server will silently raise Nats-TTL to the floor and the
	// observation window will be wrong.
	name := streamName(h)
	markerTTL := 2 * time.Second
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(markerTTL),
	}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	if ack, err := publishWithTTL(h, subj, "2s", []byte("x")); err != nil || ack.Error != nil {
		return fail("publish: err=%v ack=%+v", err, ack)
	}
	marker := waitForMarker(h, name, subj, MarkerReasonMaxAge, 10*time.Second)
	if marker == nil {
		return fail("no MaxAge marker observed within 10s after Nats-TTL expiry on %s", subj)
	}
	if hdrTTL := marker.Header.Get(HdrTTL); !markerTTLMatchesConfig(hdrTTL, markerTTL) {
		return fail("marker Nats-TTL=%q, want value equal to SubjectDeleteMarkerTTL=%s", hdrTTL, markerTTL)
	}
	return pass()
}

func testTTL505(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{
		Name:        name,
		AllowMsgTTL: true,
		MaxAge:      int64(3 * time.Second),
	}); err != nil {
		return fail("stream create: %v", err)
	}
	subj := h.Subject("k")
	if ack, err := publishWithTTL(h, subj, "", []byte("x")); err != nil || ack.Error != nil {
		return fail("publish: err=%v ack=%+v", err, ack)
	}
	time.Sleep(6 * time.Second)
	last, err := lastMsgFor(h, name, subj)
	if err != nil {
		return fail("last for %s: %v", subj, err)
	}
	if last != nil {
		if reason := last.Header.Get(HdrMarkerReason); reason != "" {
			return fail("marker placed on %s with reason %q but SubjectDeleteMarkerTTL is unset", subj, reason)
		}
		// A regular leftover message would fail the MaxAge expectation.
		return fail("subject %s has a residual message after MaxAge — SubjectDeleteMarkerTTL is unset, no marker expected, but no message expected either", subj)
	}
	return pass()
}

func testTTL506(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// Per ADR-43: when SubjectDeleteMarkerTTL is set (and MaxMsgsPer != 1)
	// the server raises a sub-floor Nats-TTL to the floor and rewrites the
	// stored header. The publish must not be rejected.
	name := streamName(h)
	floor := 60 * time.Second
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(floor),
	}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishWithTTL(h, h.Subject("a"), "2s", []byte("x"))
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error != nil {
		desc := strings.ToLower(ack.Error.Description)
		if strings.Contains(desc, "minimum") || strings.Contains(desc, "marker") || strings.Contains(desc, "floor") {
			return fail("publish with Nats-TTL=2s rejected because of marker floor: %s — ADR-43 requires clamping, not rejection", ack.Error)
		}
		return fail("publish errored unexpectedly: %s", ack.Error)
	}
	m, err := getMsg(h, name, ack.Sequence)
	if err != nil || m == nil {
		return fail("get msg seq %d: err=%v nil=%v", ack.Sequence, err, m == nil)
	}
	stored := m.Header.Get(HdrTTL)
	if !markerTTLMatchesConfig(stored, floor) {
		return fail("stored Nats-TTL=%q, want clamped value matching SubjectDeleteMarkerTTL=%s (server should rewrite the header to the floor)", stored, floor)
	}
	return pass()
}
