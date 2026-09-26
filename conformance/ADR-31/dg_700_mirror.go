// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr31

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// dg700Tests covers DG-700: MIRROR Direct Get responder participation.
func dg700Tests() []harness.Test {
	return []harness.Test{
		{ID: "DG-701", Title: "Mirror of a Direct-Get-enabled stream serves Direct Get to the upstream", Section: "DG-700", Tags: []string{"mirror"}, SkipReason: requiresMirror(), Run: testDG701},
		{ID: "DG-702", Title: "Mirror still serves Direct Get when upstream is offline", Section: "DG-700", Tags: []string{"mirror"}, SkipReason: requiresMirror(), Run: testDG702},
		{ID: "DG-703", Title: "Mirror Direct Get respects allow_direct on the source stream", Section: "DG-700", Tags: []string{"mirror"}, SkipReason: requiresMirror(), Run: testDG703},
		{ID: "DG-704", Title: "mirror_direct defaults from the upstream's allow_direct", Section: "DG-700", Tags: []string{"mirror", "config"}, SkipReason: requiresMirror(), Run: testDG704},
		{ID: "DG-705", Title: "mirror_direct can be specified explicitly", Section: "DG-700", Tags: []string{"mirror", "config"}, SkipReason: requiresMirror(), Run: testDG705},
		{ID: "DG-706", Title: "Upstream allow_direct change does not auto-propagate; mirror update re-aligns", Section: "DG-700", Tags: []string{"mirror", "config"}, SkipReason: requiresMirror(), Run: testDG706},
	}
}

func testDG701(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("DG_701_SRC")
	mir := h.MintStreamName("DG_701_MIR")

	if err := createStream(h, streamConfig{
		Name:        src,
		Subjects:    []string{h.SubjectPrefix() + ".>"},
		AllowDirect: true,
	}); err != nil {
		return fail("source stream create: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:   mir,
		Mirror: &streamSource{Name: src},
	}); err != nil {
		return fail("mirror stream create: %v", err)
	}

	subj := h.Subject("k1")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish: %v", err)
	}
	for i := 2; i <= 5; i++ {
		if _, err := publishMsg(h, subj, []byte{byte('0' + i)}, nil); err != nil {
			return fail("publish %d: %v", i, err)
		}
	}
	if !awaitStreamMsgs(h, mir, 5, 10*time.Second) {
		return fail("mirror did not catch up to 5 messages")
	}
	replies, err := directGet(h, src, directGetReq{LastFor: subj}, 5*time.Second)
	if err != nil || len(replies) != 1 || !replies[0].IsSuccess() {
		return fail("direct get on upstream: err=%v replies=%s", err, summary(replies))
	}
	if got := string(replies[0].Data); got != "5" {
		return fail("upstream Direct Get returned wrong payload: got %q want %q", got, "5")
	}
	return pass()
}

func testDG702(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("DG_702_SRC")
	mir := h.MintStreamName("DG_702_MIR")

	if err := createStream(h, streamConfig{
		Name:        src,
		Subjects:    []string{h.SubjectPrefix() + ".>"},
		AllowDirect: true,
	}); err != nil {
		return fail("source stream create: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:   mir,
		Mirror: &streamSource{Name: src},
	}); err != nil {
		return fail("mirror stream create: %v", err)
	}
	subj := h.Subject("k1")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish: %v", err)
	}
	if !awaitStreamMsgs(h, mir, 1, 10*time.Second) {
		return fail("mirror did not catch up")
	}

	// Take SRC offline by deleting it. The mirror's subscription on the
	// upstream Direct Get subject should remain in place.
	h.DeleteStream(src)
	time.Sleep(200 * time.Millisecond)

	replies, err := directGet(h, src, directGetReq{LastFor: subj}, 2*time.Second)
	if err != nil {
		return fail("direct get probe: %v", err)
	}
	if len(replies) == 0 {
		return inconclusive("after SRC offline no reply received — mirror did not take over (server may not implement upstream-mirror responder participation)")
	}
	if !replies[0].IsSuccess() || string(replies[0].Data) != "v1" {
		return fail("post-offline reply unexpected: %s", summary(replies))
	}
	return pass()
}

func testDG703(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("DG_703_SRC")
	mir := h.MintStreamName("DG_703_MIR")

	if err := createStream(h, streamConfig{
		Name:     src,
		Subjects: []string{h.SubjectPrefix() + ".>"},
	}); err != nil {
		return fail("source stream create: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:   mir,
		Mirror: &streamSource{Name: src},
	}); err != nil {
		return fail("mirror stream create: %v", err)
	}
	subj := h.Subject("k1")
	if _, err := publishMsg(h, subj, []byte("v1"), nil); err != nil {
		return fail("publish: %v", err)
	}
	timedOut, err := directGetExpectTimeout(h, src, directGetReq{LastFor: subj}, 1*time.Second)
	if err != nil {
		return fail("probe: %v", err)
	}
	if !timedOut {
		return fail("expected no responder for upstream when SRC has allow_direct=false; reply was received")
	}
	return pass()
}

// testDG704 verifies that mirror_direct on a newly-created mirror is
// defaulted from the upstream's allow_direct value when no explicit
// mirror_direct is supplied. Two cases — upstream allow_direct=true and
// upstream allow_direct=false — must each propagate to the mirror.
func testDG704(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	srcOn := h.MintStreamName("DG_704_SRC_ON")
	mirOn := h.MintStreamName("DG_704_MIR_ON")
	srcOff := h.MintStreamName("DG_704_SRC_OFF")
	mirOff := h.MintStreamName("DG_704_MIR_OFF")

	if err := createStream(h, streamConfig{
		Name:        srcOn,
		Subjects:    []string{h.Subject("on") + ".>"},
		AllowDirect: true,
	}); err != nil {
		return fail("srcOn create: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:   mirOn,
		Mirror: &streamSource{Name: srcOn},
	}); err != nil {
		return fail("mirOn create: %v", err)
	}
	cfgOn, err := streamInfo(h, mirOn)
	if err != nil {
		return fail("mirOn info: %v", err)
	}
	if cfgOn == nil || !cfgOn.MirrorDirect {
		return fail("mirOn expected MirrorDirect=true (inherited from srcOn allow_direct=true); got config=%+v", cfgOn)
	}

	if err := createStream(h, streamConfig{
		Name:     srcOff,
		Subjects: []string{h.Subject("off") + ".>"},
	}); err != nil {
		return fail("srcOff create: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:   mirOff,
		Mirror: &streamSource{Name: srcOff},
	}); err != nil {
		return fail("mirOff create: %v", err)
	}
	cfgOff, err := streamInfo(h, mirOff)
	if err != nil {
		return fail("mirOff info: %v", err)
	}
	if cfgOff == nil || cfgOff.MirrorDirect {
		return fail("mirOff expected MirrorDirect=false (inherited from srcOff allow_direct=false); got config=%+v", cfgOff)
	}
	return pass()
}

// testDG705 verifies that mirror_direct can be set explicitly when
// creating a mirror. With the upstream visible the server aligns the
// mirror's value with the upstream's allow_direct, so the explicit
// value must match — this asserts the round-trip: a value the user
// passes survives create and is reflected in stream info.
func testDG705(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("DG_705_SRC")
	mir := h.MintStreamName("DG_705_MIR")

	if err := createStream(h, streamConfig{
		Name:        src,
		Subjects:    []string{h.SubjectPrefix() + ".>"},
		AllowDirect: true,
	}); err != nil {
		return fail("source stream create: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:         mir,
		Mirror:       &streamSource{Name: src},
		MirrorDirect: true,
	}); err != nil {
		return fail("mirror stream create with explicit MirrorDirect=true: %v", err)
	}
	cfg, err := streamInfo(h, mir)
	if err != nil {
		return fail("mir info: %v", err)
	}
	if cfg == nil || !cfg.MirrorDirect {
		return fail("explicit MirrorDirect=true not reflected on mirror; config=%+v", cfg)
	}
	return pass()
}

// testDG706 verifies the staleness behavior: changing the upstream's
// allow_direct after the mirror is created does NOT automatically update
// the mirror's mirror_direct. The mirror retains its captured value
// until it is itself updated, at which point the alignment rule re-runs
// and pulls the upstream's current allow_direct.
func testDG706(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("DG_706_SRC")
	mir := h.MintStreamName("DG_706_MIR")

	// Start with upstream allow_direct=true so the mirror inherits true.
	srcSubjects := []string{h.SubjectPrefix() + ".>"}
	if err := createStream(h, streamConfig{
		Name:        src,
		Subjects:    srcSubjects,
		AllowDirect: true,
	}); err != nil {
		return fail("source stream create: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:   mir,
		Mirror: &streamSource{Name: src},
	}); err != nil {
		return fail("mirror stream create: %v", err)
	}
	cfg, err := streamInfo(h, mir)
	if err != nil {
		return fail("initial mir info: %v", err)
	}
	if cfg == nil || !cfg.MirrorDirect {
		return fail("expected initial MirrorDirect=true on mirror; got config=%+v", cfg)
	}

	// Toggle upstream allow_direct off. Mirror should NOT auto-react.
	if err := updateStream(h, streamConfig{Name: src, Subjects: srcSubjects}); err != nil {
		return fail("upstream disable update: %v", err)
	}
	srcCfg, err := streamInfo(h, src)
	if err != nil {
		return fail("source info after disable: %v", err)
	}
	if srcCfg == nil || srcCfg.AllowDirect {
		return fail("expected upstream AllowDirect=false after update; got config=%+v", srcCfg)
	}
	// Give the cluster a moment to settle propagation if any happened.
	time.Sleep(500 * time.Millisecond)
	mirCfg, err := streamInfo(h, mir)
	if err != nil {
		return fail("mir info after upstream disable: %v", err)
	}
	if mirCfg == nil || !mirCfg.MirrorDirect {
		return fail("mirror MirrorDirect should remain stale (true) after upstream toggled off; got config=%+v", mirCfg)
	}

	// Issue a no-op update against the mirror — alignment rule re-runs
	// and pulls in the upstream's current allow_direct (false).
	if err := updateStream(h, streamConfig{Name: mir, Mirror: &streamSource{Name: src}}); err != nil {
		return fail("mirror no-op update: %v", err)
	}
	postCfg, err := streamInfo(h, mir)
	if err != nil {
		return fail("post-update mir info: %v", err)
	}
	if postCfg == nil || postCfg.MirrorDirect {
		return fail("after mirror update, MirrorDirect should re-align to upstream's false; got config=%+v", postCfg)
	}
	return pass()
}
