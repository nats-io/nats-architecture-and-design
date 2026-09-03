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

// ttl100Tests covers TTL-100: stream-configuration toggles and
// constraints around AllowMsgTTL and SubjectDeleteMarkerTTL.
func ttl100Tests() []harness.Test {
	return []harness.Test{
		{ID: "TTL-101", Title: "Enabling AllowMsgTTL works", Section: "TTL-100", Tags: []string{"config"}, Run: testTTL101},
		{ID: "TTL-102", Title: "AllowMsgTTL defaults off", Section: "TTL-100", Tags: []string{"config"}, Run: testTTL102},
		{ID: "TTL-103", Title: "AllowMsgTTL can be enabled on an existing stream", Section: "TTL-100", Tags: []string{"config"}, Run: testTTL103},
		{ID: "TTL-104", Title: "AllowMsgTTL cannot be disabled once enabled", Section: "TTL-100", Tags: []string{"config"}, Run: testTTL104},
		{ID: "TTL-105", Title: "SubjectDeleteMarkerTTL minimum value is 1s", Section: "TTL-100", Tags: []string{"config"}, Run: testTTL105},
		{ID: "TTL-106", Title: "SubjectDeleteMarkerTTL rejected on a Mirror", Section: "TTL-100", Tags: []string{"config"}, Run: testTTL106},
		{ID: "TTL-107", Title: "SubjectDeleteMarkerTTL auto-sets AllowRollup (non-pedantic)", Section: "TTL-100", Tags: []string{"config"}, Run: testTTL107},
		{ID: "TTL-108", Title: "SubjectDeleteMarkerTTL requires AllowRollup (pedantic rejects)", Section: "TTL-100", Tags: []string{"config", "pedantic"}, Run: testTTL108},
		{ID: "TTL-109", Title: "SubjectDeleteMarkerTTL auto-clears DenyPurge (non-pedantic)", Section: "TTL-100", Tags: []string{"config"}, Run: testTTL109},
		{ID: "TTL-110", Title: "SubjectDeleteMarkerTTL requires DenyPurge:false (pedantic rejects)", Section: "TTL-100", Tags: []string{"config", "pedantic"}, Run: testTTL110},
		{ID: "TTL-111", Title: "SubjectDeleteMarkerTTL set raises API level to >= 1", Section: "TTL-100", Tags: []string{"config"}, Run: testTTL111},
	}
}

func testTTL101(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowMsgTTL {
		return fail("AllowMsgTTL not reported as true (config=%+v)", cfg)
	}
	if h.APILevel > 0 && h.APILevel < 1 {
		return fail("API level %d below required 1", h.APILevel)
	}
	return pass()
}

func testTTL102(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	ack, err := publishWithTTL(h, h.Subject("a"), "60s", []byte("x"))
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack publishing Nats-TTL to non-TTL stream, got %+v", ack)
	}
	if ack.Error.ErrCode != ErrCodeMessageTTLDisabled {
		return fail("err_code=%d, want %d (per-message TTL is disabled)", ack.Error.ErrCode, ErrCodeMessageTTLDisabled)
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

func testTTL103(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name}); err != nil {
		return fail("stream create: %v", err)
	}
	subjects := []string{h.SubjectPrefix() + ".>"}
	if err := updateStream(h, streamConfig{Name: name, Subjects: subjects, AllowMsgTTL: true}); err != nil {
		return fail("enable update: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowMsgTTL {
		return fail("AllowMsgTTL not reported as true after update (config=%+v)", cfg)
	}
	ack, err := publishWithTTL(h, h.Subject("a"), "60s", []byte("x"))
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error != nil {
		return fail("post-enable publish errored: %s", ack.Error)
	}
	return pass()
}

func testTTL104(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{Name: name, AllowMsgTTL: true}); err != nil {
		return fail("stream create: %v", err)
	}
	subjects := []string{h.SubjectPrefix() + ".>"}
	apiErr, err := tryStreamUpdate(h, streamConfig{Name: name, Subjects: subjects, AllowMsgTTL: false}, false)
	if err != nil {
		return fail("update transport: %v", err)
	}
	if apiErr == nil {
		cfg, _ := streamInfo(h, name)
		if cfg != nil && cfg.AllowMsgTTL {
			return inconclusive("update returned success but AllowMsgTTL remained true (server silently kept the flag)")
		}
		return fail("update to disable AllowMsgTTL unexpectedly succeeded")
	}
	if apiErr.ErrCode != ErrCodeStreamInvalidConfig {
		return fail("err_code=%d, want %d (stream config invalid)", apiErr.ErrCode, ErrCodeStreamInvalidConfig)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("post-update stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowMsgTTL {
		return fail("after rejected update, AllowMsgTTL should still be true (config=%+v)", cfg)
	}
	return pass()
}

func testTTL105(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// SubjectDeleteMarkerTTL must be at least 1 second; sub-second values
	// are rejected with a stream-config error.
	name := streamName(h)
	apiErr, err := tryStreamCreate(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(500 * time.Millisecond),
	}, false)
	if err != nil {
		return fail("create transport: %v", err)
	}
	if apiErr == nil {
		return fail("expected error creating stream with SubjectDeleteMarkerTTL=500ms, got success")
	}
	if apiErr.ErrCode != ErrCodeStreamInvalidConfig {
		return fail("err_code=%d, want %d (stream config invalid)", apiErr.ErrCode, ErrCodeStreamInvalidConfig)
	}
	if !strings.Contains(strings.ToLower(apiErr.Description), "marker") {
		return fail("description %q should reference the marker constraint", apiErr.Description)
	}
	return pass()
}

func testTTL106(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	src := h.MintStreamName("TTL_106_SRC")
	if err := createStream(h, streamConfig{Name: src, AllowMsgTTL: true}); err != nil {
		return fail("create source: %v", err)
	}
	mir := h.MintStreamName("TTL_106_MIR")
	apiErr, err := tryStreamCreate(h, streamConfig{
		Name:                   mir,
		Mirror:                 &mirror{Name: src},
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
	}, false)
	if err != nil {
		return fail("create transport: %v", err)
	}
	if apiErr == nil {
		return fail("expected error creating mirror with SubjectDeleteMarkerTTL, got success")
	}
	if apiErr.ErrCode != ErrCodeStreamInvalidConfig {
		return fail("err_code=%d, want %d (stream config invalid)", apiErr.ErrCode, ErrCodeStreamInvalidConfig)
	}
	return pass()
}

func testTTL107(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// Non-pedantic: server should accept and auto-set AllowRollup=true.
	name := streamName(h)
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
		// Deliberately NOT setting AllowRollup; server should set it.
	}); err != nil {
		return fail("stream create: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil || !cfg.AllowRollup {
		return fail("AllowRollup not auto-set to true in non-pedantic mode (config=%+v)", cfg)
	}
	return pass()
}

func testTTL108(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// Pedantic mode: explicit AllowRollup:false must be rejected.
	name := streamName(h)
	apiErr, err := tryStreamCreate(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
		AllowRollup:            false,
	}, true)
	if err != nil {
		return fail("create transport: %v", err)
	}
	if apiErr == nil {
		return fail("pedantic create with AllowRollup:false + SubjectDeleteMarkerTTL expected to be rejected, got success")
	}
	if apiErr.ErrCode != ErrCodeStreamInvalidConfig {
		return fail("err_code=%d, want %d (stream config invalid)", apiErr.ErrCode, ErrCodeStreamInvalidConfig)
	}
	return pass()
}

func testTTL109(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// Non-pedantic: server should accept and auto-clear DenyPurge=false.
	name := streamName(h)
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
		DenyPurge:              true,
	}); err != nil {
		return fail("stream create: %v", err)
	}
	cfg, err := streamInfo(h, name)
	if err != nil {
		return fail("stream info: %v", err)
	}
	if cfg == nil {
		return fail("stream info returned no config")
	}
	if cfg.DenyPurge {
		return fail("DenyPurge not auto-cleared in non-pedantic mode (config=%+v)", cfg)
	}
	return pass()
}

func testTTL110(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// Pedantic mode: DenyPurge:true with SubjectDeleteMarkerTTL must be rejected.
	name := streamName(h)
	apiErr, err := tryStreamCreate(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
		DenyPurge:              true,
	}, true)
	if err != nil {
		return fail("create transport: %v", err)
	}
	if apiErr == nil {
		return fail("pedantic create with DenyPurge:true + SubjectDeleteMarkerTTL expected to be rejected, got success")
	}
	if apiErr.ErrCode != ErrCodeStreamInvalidConfig {
		return fail("err_code=%d, want %d (stream config invalid)", apiErr.ErrCode, ErrCodeStreamInvalidConfig)
	}
	return pass()
}

func testTTL111(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name := streamName(h)
	if err := createStream(h, streamConfig{
		Name:                   name,
		AllowMsgTTL:            true,
		SubjectDeleteMarkerTTL: int64(60 * time.Second),
		AllowRollup:            true,
		DenyPurge:              false,
	}); err != nil {
		return fail("stream create: %v", err)
	}
	if h.APILevel == 0 {
		return inconclusive("server did not report API level via $JS.API.INFO; cannot assert >=1")
	}
	if h.APILevel < 1 {
		return fail("API level %d below required 1", h.APILevel)
	}
	return pass()
}