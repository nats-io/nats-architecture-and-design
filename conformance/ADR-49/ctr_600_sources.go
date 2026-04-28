// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ctr600Tests covers CTR-600: source-based aggregation. These tests
// require sourcing to converge in bounded time so they share a
// SkipReason gate (--sources, default on).
func ctr600Tests() []harness.Test {
	skip := requiresSources()
	return []harness.Test{
		{
			ID: "CTR-601", Title: "Sourced counter messages produce Nats-Counter-Sources",
			Section: "CTR-600", Tags: []string{"sources"}, SkipReason: skip, Run: testCTR601,
		},
		{
			ID: "CTR-603", Title: "Adding a source whose counter is already non-zero",
			Section: "CTR-600", Tags: []string{"sources"}, SkipReason: skip, Run: testCTR603,
		},
	}
}

// counterSourcesValueFor returns the recorded "val" string for the
// given (sourceStream, sourceSubject) entry in the Nats-Counter-Sources
// JSON header, or "" if the entry is absent. The header shape is
// {"<stream>": {"<subject>": "<val>", ...}, ...}.
func counterSourcesValueFor(headerVal, sourceStream, sourceSubject string) string {
	if headerVal == "" {
		return ""
	}
	var m map[string]map[string]string
	if err := json.Unmarshal([]byte(headerVal), &m); err != nil {
		return ""
	}
	if subj, ok := m[sourceStream]; ok {
		return subj[sourceSubject]
	}
	return ""
}

func testCTR601(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	agg := h.MintStreamName(tag + "_AGG")

	srcSubj := h.Subject("src") + ".>"
	if err := createStream(h, streamConfig{
		Name:            src,
		Subjects:        []string{srcSubj},
		AllowMsgCounter: true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:            agg,
		AllowMsgCounter: true,
		Sources:         []source{{Name: src}},
	}); err != nil {
		return fail("create aggregate: %v", err)
	}

	hitsSubj := h.Subject("src") + ".hits"

	if ack, err := publishIncr(h, hitsSubj, "+3", nil); err != nil || ack.Error != nil {
		return fail("src +3 err=%v ack=%+v", err, ack)
	}
	caught := waitFor(10*time.Second, func() bool {
		last, err := lastMsgFor(h, agg, hitsSubj)
		if err != nil || last == nil {
			return false
		}
		val, err := decodeVal(last.Data)
		return err == nil && bigEq(val, "3")
	})
	if !caught {
		return fail("aggregate did not converge to val=3 after first source publish")
	}

	if ack, err := publishIncr(h, hitsSubj, "+4", nil); err != nil || ack.Error != nil {
		return fail("src +4 err=%v ack=%+v", err, ack)
	}
	caught = waitFor(10*time.Second, func() bool {
		last, err := lastMsgFor(h, agg, hitsSubj)
		if err != nil || last == nil {
			return false
		}
		val, err := decodeVal(last.Data)
		return err == nil && bigEq(val, "7")
	})
	if !caught {
		return fail("aggregate did not converge to val=7 after second source publish")
	}

	last, err := lastMsgFor(h, agg, hitsSubj)
	if err != nil || last == nil {
		return fail("aggregate last for %s: %v", hitsSubj, err)
	}
	hdr := last.Header.Get(HdrCounterSources)
	if hdr == "" {
		return fail("aggregate last message has no Nats-Counter-Sources header")
	}
	if got := counterSourcesValueFor(hdr, src, hitsSubj); !bigEq(got, "7") {
		return fail("Nats-Counter-Sources entry for %s/%s = %q, want 7 (header=%q)", src, hitsSubj, got, hdr)
	}
	if got := last.Header.Get(HdrIncr); !bigEq(got, "4") {
		return fail("aggregate Nats-Incr=%q, want delta 4 (7-3)", got)
	}
	return pass()
}

func testCTR603(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	agg := h.MintStreamName(tag + "_AGG")

	srcSubj := h.Subject("src") + ".>"
	if err := createStream(h, streamConfig{
		Name:            src,
		Subjects:        []string{srcSubj},
		AllowMsgCounter: true,
	}); err != nil {
		return fail("create source: %v", err)
	}

	hitsSubj := h.Subject("src") + ".hits"

	// Drive the source to value 10 BEFORE adding it to the aggregate.
	if ack, err := publishIncr(h, hitsSubj, "+10", nil); err != nil || ack.Error != nil {
		return fail("src seed err=%v ack=%+v", err, ack)
	}

	// Give the aggregate its own disjoint subject space so the
	// initial create (no Sources yet) doesn't collide with the
	// source's subject filter on the same default prefix.
	aggSubj := h.Subject("agg") + ".>"
	if err := createStream(h, streamConfig{
		Name:            agg,
		Subjects:        []string{aggSubj},
		AllowMsgCounter: true,
	}); err != nil {
		return fail("create aggregate: %v", err)
	}
	if err := updateStream(h, streamConfig{
		Name:            agg,
		Subjects:        []string{aggSubj},
		AllowMsgCounter: true,
		Sources:         []source{{Name: src}},
	}); err != nil {
		return fail("update aggregate to add source: %v", err)
	}

	caught := waitFor(15*time.Second, func() bool {
		last, err := lastMsgFor(h, agg, hitsSubj)
		if err != nil || last == nil {
			return false
		}
		val, err := decodeVal(last.Data)
		return err == nil && bigEq(val, "10")
	})
	if !caught {
		return fail("aggregate did not converge to val=10 after sourcing pre-existing value")
	}

	last, err := lastMsgFor(h, agg, hitsSubj)
	if err != nil || last == nil {
		return fail("aggregate last for %s: %v", hitsSubj, err)
	}
	if got := last.Header.Get(HdrIncr); !bigEq(got, "10") {
		return fail("aggregate Nats-Incr=%q, want 10 (initial source value rolled into first delta)", got)
	}
	return pass()
}
