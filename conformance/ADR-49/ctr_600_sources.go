// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"
	"encoding/json"
	"math/big"
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
			ID: "CTR-602", Title: "Source delta calculation handles missed messages",
			Section: "CTR-600", Tags: []string{"sources"}, SkipReason: skip, Run: testCTR602,
		},
		{
			ID: "CTR-603", Title: "Adding a source whose counter is already non-zero",
			Section: "CTR-600", Tags: []string{"sources"}, SkipReason: skip, Run: testCTR603,
		},
		{
			ID: "CTR-604", Title: "Nats-Counter-Sources is preserved across local writes",
			Section: "CTR-600", Tags: []string{"sources"}, SkipReason: skip, Run: testCTR604,
		},
		{
			ID: "CTR-605", Title: "Removed source key persists in Nats-Counter-Sources",
			Section: "CTR-600", Tags: []string{"sources"}, SkipReason: skip, Run: testCTR605,
		},
		{
			ID: "CTR-606", Title: "Re-adding a previously-removed source resumes from the recorded value",
			Section: "CTR-600", Tags: []string{"sources"}, SkipReason: skip, Run: testCTR606,
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

func testCTR602(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	agg := h.MintStreamName(tag + "_AGG")

	srcSubj := h.Subject("src") + ".>"
	// MaxMsgsPerSubject:1 evicts older counter values on SRC so AGG
	// must rely on delta arithmetic to recount.
	if err := createStream(h, streamConfig{
		Name:              src,
		Subjects:          []string{srcSubj},
		AllowMsgCounter:   true,
		MaxMsgsPerSubject: 1,
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

	// Step 1: +2 — wait for AGG to see val=2 so the first source value
	// is recorded before SRC evicts it.
	if ack, err := publishIncr(h, hitsSubj, "+2", nil); err != nil || ack.Error != nil {
		return fail("src +2 err=%v ack=%+v", err, ack)
	}
	caught := waitFor(10*time.Second, func() bool {
		last, err := lastMsgFor(h, agg, hitsSubj)
		if err != nil || last == nil {
			return false
		}
		val, err := decodeVal(last.Data)
		return err == nil && bigEq(val, "2")
	})
	if !caught {
		return fail("aggregate did not converge to val=2 after first source publish")
	}

	// Step 2 + 3: +5 then +10. SRC now retains only the final val=17
	// message. AGG must compute the right deltas from whatever it sees.
	if ack, err := publishIncr(h, hitsSubj, "+5", nil); err != nil || ack.Error != nil {
		return fail("src +5 err=%v ack=%+v", err, ack)
	}
	if ack, err := publishIncr(h, hitsSubj, "+10", nil); err != nil || ack.Error != nil {
		return fail("src +10 err=%v ack=%+v", err, ack)
	}

	caught = waitFor(15*time.Second, func() bool {
		last, err := lastMsgFor(h, agg, hitsSubj)
		if err != nil || last == nil {
			return false
		}
		val, err := decodeVal(last.Data)
		return err == nil && bigEq(val, "17")
	})
	if !caught {
		return fail("aggregate did not converge to val=17 after eviction-driven source flow")
	}

	// Recount property: stored Nats-Incr headers on AGG must sum to 17.
	msgs, err := listMsgs(h, agg)
	if err != nil {
		return fail("list aggregate msgs: %v", err)
	}
	sum := big.NewInt(0)
	for _, m := range msgs {
		if m.Subject != hitsSubj {
			continue
		}
		incr := m.Header.Get(HdrIncr)
		if incr == "" {
			return fail("aggregate msg seq=%d has no Nats-Incr", m.Sequence)
		}
		v, ok := new(big.Int).SetString(strings.TrimPrefix(incr, "+"), 10)
		if !ok {
			return fail("could not parse stored Nats-Incr %q at seq=%d", incr, m.Sequence)
		}
		sum.Add(sum, v)
	}
	if sum.String() != "17" {
		return fail("sum of stored Nats-Incr deltas on aggregate = %s, want 17", sum.String())
	}
	return pass()
}

func testCTR604(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	agg := h.MintStreamName(tag + "_AGG")

	// SRC owns subjects under src.>; AGG owns subjects under agg.> and
	// sources from SRC with a transform that funnels src.* → agg.*. The
	// transformed subject is the same one local writes target — so the
	// per-subject Nats-Counter-Sources state has somewhere to land.
	srcSubj := h.Subject("src") + ".>"
	if err := createStream(h, streamConfig{
		Name:            src,
		Subjects:        []string{srcSubj},
		AllowMsgCounter: true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	aggSubj := h.Subject("agg") + ".>"
	if err := createStream(h, streamConfig{
		Name:            agg,
		Subjects:        []string{aggSubj},
		AllowMsgCounter: true,
		Sources: []source{{
			Name: src,
			SubjectTransforms: []subjectTransform{{
				Src:  h.Subject("src") + ".*",
				Dest: h.Subject("agg") + ".$1",
			}},
		}},
	}); err != nil {
		return fail("create aggregate: %v", err)
	}

	srcHits := h.Subject("src") + ".hits"
	aggHits := h.Subject("agg") + ".hits"

	if ack, err := publishIncr(h, srcHits, "+3", nil); err != nil || ack.Error != nil {
		return fail("seed source err=%v ack=%+v", err, ack)
	}
	caught := waitFor(10*time.Second, func() bool {
		last, err := lastMsgFor(h, agg, aggHits)
		if err != nil || last == nil {
			return false
		}
		return counterSourcesValueFor(last.Header.Get(HdrCounterSources), src, srcHits) != ""
	})
	if !caught {
		return fail("aggregate did not record Nats-Counter-Sources for %s/%s on subject %s", src, srcHits, aggHits)
	}

	// Local write to the SAME subject the sourced messages land on.
	if ack, err := publishIncr(h, aggHits, "+1", nil); err != nil || ack.Error != nil {
		return fail("local publish err=%v ack=%+v", err, ack)
	}
	last, err := lastMsgFor(h, agg, aggHits)
	if err != nil || last == nil {
		return fail("aggregate last for %s: msg=%v err=%v", aggHits, last, err)
	}
	hdr := last.Header.Get(HdrCounterSources)
	if hdr == "" {
		return fail("local message dropped Nats-Counter-Sources; ADR-49 requires it be inherited from the prior stream message on this subject")
	}
	if got := counterSourcesValueFor(hdr, src, srcHits); !bigEq(got, "3") {
		return fail("inherited Nats-Counter-Sources entry for %s/%s = %q, want 3 (header=%q)", src, srcHits, got, hdr)
	}
	return pass()
}

func testCTR605(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	agg := h.MintStreamName(tag + "_AGG")

	// Same setup as CTR-604: src.* funnelled into agg.* via source
	// transform, so per-subject sources state lives on agg.hits.
	srcSubj := h.Subject("src") + ".>"
	if err := createStream(h, streamConfig{
		Name:            src,
		Subjects:        []string{srcSubj},
		AllowMsgCounter: true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	aggSubj := h.Subject("agg") + ".>"
	if err := createStream(h, streamConfig{
		Name:            agg,
		Subjects:        []string{aggSubj},
		AllowMsgCounter: true,
		Sources: []source{{
			Name: src,
			SubjectTransforms: []subjectTransform{{
				Src:  h.Subject("src") + ".*",
				Dest: h.Subject("agg") + ".$1",
			}},
		}},
	}); err != nil {
		return fail("create aggregate: %v", err)
	}

	srcHits := h.Subject("src") + ".hits"
	aggHits := h.Subject("agg") + ".hits"

	if ack, err := publishIncr(h, srcHits, "+5", nil); err != nil || ack.Error != nil {
		return fail("seed source err=%v ack=%+v", err, ack)
	}
	caught := waitFor(10*time.Second, func() bool {
		last, err := lastMsgFor(h, agg, aggHits)
		if err != nil || last == nil {
			return false
		}
		return counterSourcesValueFor(last.Header.Get(HdrCounterSources), src, srcHits) == "5"
	})
	if !caught {
		return fail("aggregate did not record Nats-Counter-Sources entry val=5 before source removal")
	}

	// Drop the source from AGG.
	if err := updateStream(h, streamConfig{
		Name:            agg,
		Subjects:        []string{aggSubj},
		AllowMsgCounter: true,
	}); err != nil {
		return fail("update aggregate to drop sources: %v", err)
	}

	// Local write on the same subject the previous sourced message used.
	if ack, err := publishIncr(h, aggHits, "+1", nil); err != nil || ack.Error != nil {
		return fail("local publish err=%v ack=%+v", err, ack)
	}
	last, err := lastMsgFor(h, agg, aggHits)
	if err != nil || last == nil {
		return fail("aggregate last for %s: msg=%v err=%v", aggHits, last, err)
	}
	hdr := last.Header.Get(HdrCounterSources)
	if hdr == "" {
		return fail("local message after source removal dropped Nats-Counter-Sources entirely; ADR-49 requires the entry to persist")
	}
	if got := counterSourcesValueFor(hdr, src, srcHits); !bigEq(got, "5") {
		return fail("removed source entry should persist with last-seen val=5, got %q (header=%q)", got, hdr)
	}
	return pass()
}

func testCTR606(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
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
	localSubj := h.Subject("local") + ".>"
	if err := createStream(h, streamConfig{
		Name:            agg,
		Subjects:        []string{localSubj},
		AllowMsgCounter: true,
		Sources:         []source{{Name: src}},
	}); err != nil {
		return fail("create aggregate: %v", err)
	}

	srcHits := h.Subject("src") + ".hits"
	if ack, err := publishIncr(h, srcHits, "+5", nil); err != nil || ack.Error != nil {
		return fail("src seed err=%v ack=%+v", err, ack)
	}
	caught := waitFor(10*time.Second, func() bool {
		last, err := lastMsgFor(h, agg, srcHits)
		if err != nil || last == nil {
			return false
		}
		return counterSourcesValueFor(last.Header.Get(HdrCounterSources), src, srcHits) == "5"
	})
	if !caught {
		return fail("aggregate did not record source val=5 before removal")
	}

	if err := updateStream(h, streamConfig{
		Name:            agg,
		Subjects:        []string{localSubj},
		AllowMsgCounter: true,
	}); err != nil {
		return fail("update aggregate to drop source: %v", err)
	}

	// Drive SRC to 25 while AGG isn't sourcing it.
	for i := 0; i < 2; i++ {
		if ack, err := publishIncr(h, srcHits, "+10", nil); err != nil || ack.Error != nil {
			return fail("src +10 err=%v ack=%+v", err, ack)
		}
	}

	if err := updateStream(h, streamConfig{
		Name:            agg,
		Subjects:        []string{localSubj},
		AllowMsgCounter: true,
		Sources:         []source{{Name: src}},
	}); err != nil {
		return fail("update aggregate to re-add source: %v", err)
	}

	caught = waitFor(15*time.Second, func() bool {
		last, err := lastMsgFor(h, agg, srcHits)
		if err != nil || last == nil {
			return false
		}
		val, err := decodeVal(last.Data)
		return err == nil && bigEq(val, "25")
	})
	if !caught {
		return fail("aggregate did not converge to val=25 after re-adding source")
	}

	// Recount: deltas applied for srcHits since the resumption must
	// sum to 20 (25 - 5).
	msgs, err := listMsgs(h, agg)
	if err != nil {
		return fail("list aggregate msgs: %v", err)
	}
	sum := big.NewInt(0)
	for _, m := range msgs {
		if m.Subject != srcHits {
			continue
		}
		incr := m.Header.Get(HdrIncr)
		if incr == "" {
			continue
		}
		v, ok := new(big.Int).SetString(strings.TrimPrefix(incr, "+"), 10)
		if !ok {
			return fail("parse Nats-Incr %q at seq=%d", incr, m.Sequence)
		}
		sum.Add(sum, v)
	}
	if sum.String() != "25" {
		return fail("aggregate Nats-Incr deltas sum to %s, want 25 (5 from initial sourcing + 20 from resumption)", sum.String())
	}
	return pass()
}
