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

// ttl900Tests covers TTL-900: sources / mirrors interaction with the
// Nats-TTL header and SubjectDeleteMarkerTTL configuration.
func ttl900Tests() []harness.Test {
	srcs := requiresSources()
	srcsAndSlow := func(opts *harness.Options) string {
		if r := srcs(opts); r != "" {
			return r
		}
		return requiresSlow()(opts)
	}
	return []harness.Test{
		{ID: "TTL-901", Title: "Mirror always stores Nats-TTL messages even with AllowMsgTTL disabled", Section: "TTL-900", Tags: []string{"mirrors"}, SkipReason: srcs, Run: testTTL901},
		{ID: "TTL-902", Title: "Mirror with AllowMsgTTL:true honours Nats-TTL on mirrored messages", Section: "TTL-900", Tags: []string{"mirrors", "slow"}, SkipReason: srcsAndSlow, Run: testTTL902},
		{ID: "TTL-903", Title: "Mirror cannot enable SubjectDeleteMarkerTTL", Section: "TTL-900", Tags: []string{"mirrors", "config"}, Run: testTTL903},
		{ID: "TTL-904", Title: "Source can enable SubjectDeleteMarkerTTL", Section: "TTL-900", Tags: []string{"sources", "marker", "slow"}, SkipReason: srcsAndSlow, Run: testTTL904},
	}
}

func testTTL901(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	mir := h.MintStreamName(tag + "_MIR")

	srcSubj := h.Subject("src") + ".>"
	if err := createStream(h, streamConfig{
		Name:        src,
		Subjects:    []string{srcSubj},
		AllowMsgTTL: true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:        mir,
		Mirror:      &mirror{Name: src},
		AllowMsgTTL: false,
	}); err != nil {
		return fail("create mirror: %v", err)
	}

	publishSubj := h.Subject("src") + ".a"
	ack, err := publishWithTTL(h, publishSubj, "60s", []byte("payload"))
	if err != nil || ack.Error != nil {
		return fail("publish to src: err=%v ack=%+v", err, ack)
	}

	if !waitFor(10*time.Second, func() bool {
		last, err := streamLastSeq(h, mir)
		return err == nil && last >= 1
	}) {
		return fail("mirror did not receive the message within 10s")
	}

	last, err := lastMsgFor(h, mir, publishSubj)
	if err != nil || last == nil {
		return fail("mirror last for %s: err=%v nil=%v", publishSubj, err, last == nil)
	}
	if got := last.Header.Get(HdrTTL); got != "60s" {
		return fail("mirror Nats-TTL=%q, want %q (header must be preserved verbatim)", got, "60s")
	}
	// On the mirror, AllowMsgTTL is false — the message must be stored
	// (not rejected) and must NOT be subject to TTL expiry.
	time.Sleep(2 * time.Second)
	stillThere, err := lastMsgFor(h, mir, publishSubj)
	if err != nil {
		return fail("mirror last (after wait): %v", err)
	}
	if stillThere == nil {
		return fail("mirror dropped the message — with AllowMsgTTL:false the mirror must store and NOT expire it")
	}
	return pass()
}

func testTTL902(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	mir := h.MintStreamName(tag + "_MIR")

	srcSubj := h.Subject("src") + ".>"
	if err := createStream(h, streamConfig{
		Name:        src,
		Subjects:    []string{srcSubj},
		AllowMsgTTL: true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:        mir,
		Mirror:      &mirror{Name: src},
		AllowMsgTTL: true,
	}); err != nil {
		return fail("create mirror: %v", err)
	}

	publishSubj := h.Subject("src") + ".a"
	ack, err := publishWithTTL(h, publishSubj, "3s", []byte("payload"))
	if err != nil || ack.Error != nil {
		return fail("publish to src: err=%v ack=%+v", err, ack)
	}

	// Wait for the mirror to receive the message.
	if !waitFor(5*time.Second, func() bool {
		m, err := lastMsgFor(h, mir, publishSubj)
		return err == nil && m != nil
	}) {
		return fail("mirror did not receive the message within 5s")
	}

	// Now wait for the TTL to expire on the mirror.
	if !waitFor(8*time.Second, func() bool {
		m, err := lastMsgFor(h, mir, publishSubj)
		return err == nil && m == nil
	}) {
		return fail("message still present on mirror after 8s — with AllowMsgTTL:true the mirror should expire it")
	}
	return pass()
}

func testTTL903(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	if err := createStream(h, streamConfig{
		Name:        src,
		AllowMsgTTL: true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	mir := h.MintStreamName(tag + "_MIR")
	err := createStream(h, streamConfig{
		Name:                   mir,
		Mirror:                 &mirror{Name: src},
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
	})
	if err == nil {
		return fail("expected error creating mirror with SubjectDeleteMarkerTTL, got success")
	}
	return pass()
}

func testTTL904(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	dst := h.MintStreamName(tag + "_DST")

	srcSubj := h.Subject("src") + ".>"
	if err := createStream(h, streamConfig{
		Name:        src,
		Subjects:    []string{srcSubj},
		AllowMsgTTL: true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:                   dst,
		Sources:                []source{{Name: src}},
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
		MaxAge:                 int64(3 * time.Second),
	}); err != nil {
		return fail("create destination: %v", err)
	}

	publishSubj := h.Subject("src") + ".k"
	if ack, err := publishWithTTL(h, publishSubj, "", []byte("x")); err != nil || ack.Error != nil {
		return fail("publish to src: err=%v ack=%+v", err, ack)
	}

	// Wait for sourcing + MaxAge expiry on dst, then check for marker.
	marker := waitForMarker(h, dst, publishSubj, MarkerReasonMaxAge, 12*time.Second)
	if marker == nil {
		return fail("no MaxAge marker observed on dst within 12s after MaxAge expiry — sources should support SubjectDeleteMarkerTTL")
	}
	return pass()
}
