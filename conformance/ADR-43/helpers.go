// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr43

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ---- ADR-43 wire-level identifiers ----

const (
	HdrTTL          = "Nats-TTL"
	HdrMarkerReason = "Nats-Marker-Reason"

	MarkerReasonMaxAge = "MaxAge"
	MarkerReasonRemove = "Remove"
	MarkerReasonPurge  = "Purge"

	// err_code values that ADR-43 enumerates for the rejection paths in
	// this suite. Stream-config rejections all share JSStreamInvalidConfigF.
	ErrCodeMessageTTLInvalid   = 10165
	ErrCodeMessageTTLDisabled  = 10166
	ErrCodeStreamInvalidConfig = 10052
)

// ---- Wire types ----

type apiError struct {
	Code        int    `json:"code"`
	ErrCode     int    `json:"err_code,omitempty"`
	Description string `json:"description,omitempty"`
}

func (e *apiError) String() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("code=%d err_code=%d desc=%q", e.Code, e.ErrCode, e.Description)
}

type pubAck struct {
	Stream   string    `json:"stream"`
	Sequence uint64    `json:"seq"`
	Error    *apiError `json:"error,omitempty"`
}

type source struct {
	Name string `json:"name"`
}

type mirror struct {
	Name string `json:"name"`
}

// streamConfig only carries fields the ADR-43 tests touch. JSON tags
// match the JetStream wire format so the harness talks directly to
// $JS.API.STREAM.* without library coupling.
type streamConfig struct {
	Name                   string   `json:"name"`
	Subjects               []string `json:"subjects,omitempty"`
	Storage                string   `json:"storage,omitempty"`
	Retention              string   `json:"retention,omitempty"`
	Replicas               int      `json:"num_replicas,omitempty"`
	MaxAge                 int64    `json:"max_age,omitempty"`             // ns
	MaxMsgsPerSubject      int64    `json:"max_msgs_per_subject,omitempty"`
	AllowMsgTTL            bool     `json:"allow_msg_ttl,omitempty"`
	SubjectDeleteMarkerTTL int64    `json:"subject_delete_marker_ttl,omitempty"` // ns
	AllowRollup            bool     `json:"allow_rollup_hdrs,omitempty"`
	DenyPurge              bool     `json:"deny_purge,omitempty"`
	Mirror                 *mirror  `json:"mirror,omitempty"`
	Sources                []source `json:"sources,omitempty"`
}

// streamConfigReq is the on-the-wire stream create / update payload.
// Pedantic is a request-scoped flag flattened next to the config in JSON.
type streamConfigReq struct {
	streamConfig
	Pedantic bool `json:"pedantic,omitempty"`
}

type streamInfoResp struct {
	Type   string         `json:"type"`
	Error  *apiError      `json:"error,omitempty"`
	Config *streamConfig  `json:"config,omitempty"`
	State  map[string]any `json:"state,omitempty"`
}

// ---- Stream lifecycle (raw $JS.API to avoid library coupling) ----

func createStream(h *harness.Harness, cfg streamConfig) error {
	return doStreamCreate(h, cfg, false)
}

func createStreamPedantic(h *harness.Harness, cfg streamConfig) error {
	return doStreamCreate(h, cfg, true)
}

func doStreamCreate(h *harness.Harness, cfg streamConfig, pedantic bool) error {
	apiErr, err := tryStreamCreate(h, cfg, pedantic)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return fmt.Errorf("stream create error: %s", apiErr)
	}
	return nil
}

// tryStreamCreate returns the apiError separately from transport errors so
// tests asserting on err_code values can inspect it directly.
func tryStreamCreate(h *harness.Harness, cfg streamConfig, pedantic bool) (*apiError, error) {
	if cfg.Storage == "" {
		cfg.Storage = "file"
	}
	if cfg.Replicas == 0 {
		cfg.Replicas = 1
	}
	if len(cfg.Subjects) == 0 && cfg.Mirror == nil && len(cfg.Sources) == 0 {
		cfg.Subjects = []string{h.SubjectPrefix() + ".>"}
	}
	body, err := json.Marshal(streamConfigReq{streamConfig: cfg, Pedantic: pedantic})
	if err != nil {
		return nil, err
	}
	resp, err := h.NC.Request("$JS.API.STREAM.CREATE."+cfg.Name, body, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var sresp streamInfoResp
	if err := json.Unmarshal(resp.Data, &sresp); err != nil {
		return nil, err
	}
	if sresp.Error != nil {
		return sresp.Error, nil
	}
	h.TrackStream(cfg.Name)
	return nil, nil
}

func updateStream(h *harness.Harness, cfg streamConfig) error {
	return doStreamUpdate(h, cfg, false)
}

func doStreamUpdate(h *harness.Harness, cfg streamConfig, pedantic bool) error {
	apiErr, err := tryStreamUpdate(h, cfg, pedantic)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return fmt.Errorf("stream update error: %s", apiErr)
	}
	return nil
}

// tryStreamUpdate returns the apiError separately from transport errors so
// tests asserting on err_code values can inspect it directly.
func tryStreamUpdate(h *harness.Harness, cfg streamConfig, pedantic bool) (*apiError, error) {
	body, err := json.Marshal(streamConfigReq{streamConfig: cfg, Pedantic: pedantic})
	if err != nil {
		return nil, err
	}
	resp, err := h.NC.Request("$JS.API.STREAM.UPDATE."+cfg.Name, body, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var sresp streamInfoResp
	if err := json.Unmarshal(resp.Data, &sresp); err != nil {
		return nil, err
	}
	if sresp.Error != nil {
		return sresp.Error, nil
	}
	return nil, nil
}

func streamInfo(h *harness.Harness, name string) (*streamConfig, error) {
	resp, err := h.NC.Request("$JS.API.STREAM.INFO."+name, nil, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var info streamInfoResp
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return nil, err
	}
	if info.Error != nil {
		return nil, fmt.Errorf("stream info error: %s", info.Error)
	}
	return info.Config, nil
}

func purgeSubject(h *harness.Harness, stream, subject string, keep int) error {
	req := struct {
		Filter string `json:"filter,omitempty"`
		Keep   int    `json:"keep,omitempty"`
	}{Filter: subject, Keep: keep}
	body, _ := json.Marshal(req)
	resp, err := h.NC.Request("$JS.API.STREAM.PURGE."+stream, body, 5*time.Second)
	if err != nil {
		return err
	}
	var out struct {
		Error *apiError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return err
	}
	if out.Error != nil {
		return fmt.Errorf("purge error: %s", out.Error)
	}
	return nil
}

func deleteMsg(h *harness.Harness, stream string, seq uint64) error {
	req := struct {
		Seq uint64 `json:"seq"`
	}{Seq: seq}
	body, _ := json.Marshal(req)
	resp, err := h.NC.Request("$JS.API.STREAM.MSG.DELETE."+stream, body, 5*time.Second)
	if err != nil {
		return err
	}
	var out struct {
		Success bool      `json:"success,omitempty"`
		Error   *apiError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return err
	}
	if out.Error != nil {
		return fmt.Errorf("delete msg error: %s", out.Error)
	}
	return nil
}

// ---- Publishing primitives ----

// newMsgWithTTL builds a *nats.Msg with the supplied TTL. ttl==""
// publishes without the header.
func newMsgWithTTL(subject, ttl string, body []byte) *nats.Msg {
	m := nats.NewMsg(subject)
	if ttl != "" {
		m.Header.Set(HdrTTL, ttl)
	}
	m.Data = body
	return m
}

func publishWithTTL(h *harness.Harness, subject, ttl string, body []byte) (*pubAck, error) {
	return publishMsg(h, newMsgWithTTL(subject, ttl, body))
}

func publishMsg(h *harness.Harness, m *nats.Msg) (*pubAck, error) {
	resp, err := h.NC.RequestMsg(m, 5*time.Second)
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return &pubAck{}, nil
	}
	var ack pubAck
	if err := json.Unmarshal(resp.Data, &ack); err != nil {
		return nil, fmt.Errorf("decode pub ack: %w (raw=%q)", err, string(resp.Data))
	}
	return &ack, nil
}

// ---- Stream inspection ----

func getMsg(h *harness.Harness, streamName string, seq uint64) (*jetstream.RawStreamMsg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	str, err := h.JS.Stream(ctx, streamName)
	if err != nil {
		return nil, err
	}
	m, err := str.GetMsg(ctx, seq)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

func lastMsgFor(h *harness.Harness, streamName, subject string) (*jetstream.RawStreamMsg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	str, err := h.JS.Stream(ctx, streamName)
	if err != nil {
		return nil, err
	}
	m, err := str.GetLastMsgForSubject(ctx, subject)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return m, nil
}

func streamLastSeq(h *harness.Harness, streamName string) (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	str, err := h.JS.Stream(ctx, streamName)
	if err != nil {
		return 0, err
	}
	info, err := str.Info(ctx)
	if err != nil {
		return 0, err
	}
	return info.State.LastSeq, nil
}

func streamMsgCount(h *harness.Harness, streamName string) (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	str, err := h.JS.Stream(ctx, streamName)
	if err != nil {
		return 0, err
	}
	info, err := str.Info(ctx)
	if err != nil {
		return 0, err
	}
	return info.State.Msgs, nil
}

// ---- Polling helpers ----

// waitFor polls check at 100ms intervals until d elapses, returning the
// final result of check at expiry.
func waitFor(d time.Duration, check func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return check()
}

// waitUntilGone polls until the message at seq is no longer retrievable,
// or the timeout fires. Returns true if the message is gone.
func waitUntilGone(h *harness.Harness, streamName string, seq uint64, d time.Duration) bool {
	return waitFor(d, func() bool {
		m, err := getMsg(h, streamName, seq)
		return err == nil && m == nil
	})
}

// waitForMarker polls lastMsgFor until a server-placed marker with the
// given Nats-Marker-Reason appears for subject. Returns the marker or nil.
func waitForMarker(h *harness.Harness, streamName, subject, reason string, d time.Duration) *jetstream.RawStreamMsg {
	var found *jetstream.RawStreamMsg
	waitFor(d, func() bool {
		m, err := lastMsgFor(h, streamName, subject)
		if err != nil || m == nil {
			return false
		}
		if m.Header.Get(HdrMarkerReason) == reason {
			found = m
			return true
		}
		return false
	})
	return found
}

// ---- ID and naming utilities ----

func newUUID() string {
	var b [16]byte
	_, _ = readRand(b[:])
	b[6] = (b[6] & 0x0F) | 0x40
	b[8] = (b[8] & 0x3F) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func streamName(h *harness.Harness) string {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	return h.MintStreamName(tag)
}

// ---- Result helpers wrapping harness.{Pass,Fail,...} ----

func pass() (harness.Status, string, error)                         { return harness.Pass() }
func fail(format string, a ...any) (harness.Status, string, error)  { return harness.Fail(format, a...) }
func skip(format string, a ...any) (harness.Status, string, error)  { return harness.Skip(format, a...) }
func inconclusive(format string, a ...any) (harness.Status, string, error) {
	return harness.Inconclusive(format, a...)
}

// readRand wraps crypto/rand for UUID generation.
var readRand = func(p []byte) (int, error) {
	return cryptoRandRead(p)
}

// silence "imported and not used" when not all helpers are referenced
// by the current test slice.
var _ = skip
var _ = newUUID