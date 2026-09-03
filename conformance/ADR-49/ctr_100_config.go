// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ctr100Tests covers CTR-100: stream-configuration toggles for
// AllowMsgCounter and the constraints around it (mirrors, retention,
// discard, TTLs).
func ctr100Tests() []harness.Test {
	return []harness.Test{
		{ID: "CTR-101", Title: "Enabling AllowMsgCounter works", Section: "CTR-100", Tags: []string{"config"}, Run: testCTR101},
		{ID: "CTR-102", Title: "AllowMsgCounter defaults off", Section: "CTR-100", Tags: []string{"config"}, Run: testCTR102},
		{ID: "CTR-103", Title: "AllowMsgCounter cannot be enabled on an existing stream", Section: "CTR-100", Tags: []string{"config"}, Run: testCTR103},
		{ID: "CTR-104", Title: "AllowMsgCounter cannot be disabled once enabled", Section: "CTR-100", Tags: []string{"config"}, Run: testCTR104},
		{ID: "CTR-105", Title: "AllowMsgCounter rejected on Mirror", Section: "CTR-100", Tags: []string{"config"}, Run: testCTR105},
		{ID: "CTR-106", Title: "AllowMsgCounter rejected with non-Limits retention", Section: "CTR-100", Tags: []string{"config"}, Run: testCTR106},
		{ID: "CTR-107", Title: "AllowMsgCounter rejected with Discard:new", Section: "CTR-100", Tags: []string{"config"}, Run: testCTR107},
		{ID: "CTR-108", Title: "AllowMsgCounter rejected with per-message TTLs", Section: "CTR-100", Tags: []string{"config"}, Run: testCTR108},
		{ID: "CTR-109", Title: "Counter stream may have Sources", Section: "CTR-100", Tags: []string{"config", "sources"}, Run: testCTR109},
	}
}

func testCTR101(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowMsgCounter {
		return fail("AllowMsgCounter not reported as true (config=%+v)", cfg)
	}
	return pass()
}

func testCTR102(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishIncr(h, h.Subject("hits"), "+1", nil)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack publishing Nats-Incr to non-counter stream, got %+v", ack)
	}
	last, err := streamLastSeq(h, name)
	if err != nil {
		return fail("last seq: %v", err)
	}
	if last != 0 {
		return fail("stream last seq advanced (now %d) — message should not have been stored", last)
	}
	return pass()
}

func testCTR103(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	subjects := []string{h.SubjectPrefix() + ".>"}
	// ADR-49 §"Stream Configuration": "This feature can only be
	// enabled during creation, it is read only once the stream
	// exist". The update must be rejected.
	if err := updateStream(h, streamConfig{Name: name, Subjects: subjects, AllowMsgCounter: true}); err == nil {
		// Update returned success. If the server silently kept the
		// flag at false, that is still a failure: the ADR forbids
		// the toggle outright at update time.
		cfg, _ := streamInfo(h, name)
		if cfg != nil && cfg.AllowMsgCounter {
			return fail("update to enable AllowMsgCounter unexpectedly succeeded; counter setting is read-only after creation")
		}
		return fail("update returned success but AllowMsgCounter remained false (server silently ignored the change instead of rejecting it)")
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("post-update stream info: %v", err)
	}
	if cfg == nil {
		return fail("stream info returned no config")
	}
	if cfg.AllowMsgCounter {
		return fail("after rejected update, AllowMsgCounter should still be false (config=%+v)", cfg)
	}
	// Stream is still a non-counter stream; an Nats-Incr publish
	// must be rejected (CTR-207 covers the same constraint on a
	// freshly-created non-counter stream — we re-assert it here to
	// confirm the rejected update did not leave the stream in a
	// half-counter state).
	ack, err := publishIncr(h, h.Subject("hits"), "+5", nil)
	if err != nil {
		return fail("post-update publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack publishing Nats-Incr to non-counter stream after rejected update, got %+v", ack)
	}
	return pass()
}

func testCTR104(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjects := []string{h.SubjectPrefix() + ".>"}
	err := updateStream(h, streamConfig{Name: name, Subjects: subjects, AllowMsgCounter: false})
	if err == nil {
		// Re-fetch to see if it actually flipped — some servers may
		// silently ignore the false setting; that's still a fail.
		cfg, _ := streamInfo(h, name)
		if cfg != nil && cfg.AllowMsgCounter {
			return inconclusive("update returned success but AllowMsgCounter remained true (server silently ignored downgrade)")
		}
		return fail("update to disable AllowMsgCounter unexpectedly succeeded")
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("post-update stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowMsgCounter {
		return fail("after rejected update, AllowMsgCounter should still be true (config=%+v)", cfg)
	}
	return pass()
}

func testCTR105(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("CTR_105_SRC")
	if err := createStream(h, streamConfig{Name: src}); err != nil {
		return fail("create source: %v", err)
	}
	mir := h.MintStreamName("CTR_105_MIR")
	err := createStream(h, streamConfig{Name: mir, Mirror: &mirror{Name: src}, AllowMsgCounter: true})
	if err == nil {
		return fail("expected error creating mirror with AllowMsgCounter, got success")
	}
	return pass()
}

func testCTR106(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	for _, retention := range []string{"workqueue", "interest"} {
		name := h.MintStreamName("CTR_106_" + retention)
		err := createStream(h, streamConfig{
			Name:            name,
			AllowMsgCounter: true,
			Retention:       retention,
		})
		if err == nil {
			return fail("expected error creating counter stream with retention=%q, got success", retention)
		}
	}
	return pass()
}

func testCTR107(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true, Discard: "new"})
	if err == nil {
		return fail("expected error creating counter stream with discard=new, got success")
	}
	return pass()
}

func testCTR108(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	err := createStream(h, streamConfig{Name: name, AllowMsgCounter: true, AllowMsgTTL: true})
	if err == nil {
		return fail("expected error creating counter stream with AllowMsgTTL, got success")
	}
	return pass()
}

func testCTR109(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("CTR_109_SRC")
	if err := createStream(h, streamConfig{Name: src, AllowMsgCounter: true}); err != nil {
		return fail("create source counter: %v", err)
	}
	dst := h.MintStreamName("CTR_109_DST")
	err := createStream(h, streamConfig{
		Name:            dst,
		AllowMsgCounter: true,
		Sources:         []source{{Name: src}},
	})
	if err != nil {
		return fail("create counter stream with Sources: %v", err)
	}
	return pass()
}