// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr49

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ---- ADR-49 wire-level identifiers ----

const (
	HdrIncr           = "Nats-Incr"
	HdrCounterSources = "Nats-Counter-Sources"
	HdrStreamSource   = "Nats-Stream-Source"
	HdrRollup         = "Nats-Rollup"
	HdrExpLastSeq     = "Nats-Expected-Last-Sequence"
	HdrExpLastSubjSeq = "Nats-Expected-Last-Subject-Sequence"
	HdrExpStream      = "Nats-Expected-Stream"
	HdrExpLastMsgID   = "Nats-Expected-Last-Msg-Id"
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

// pubAck is the JetStream pub ack with the ADR-49 `val` extension.
type pubAck struct {
	Stream   string    `json:"stream"`
	Sequence uint64    `json:"seq"`
	Domain   string    `json:"domain,omitempty"`
	Value    string    `json:"val,omitempty"`
	Error    *apiError `json:"error,omitempty"`
}

// counterValue is the ADR-49 stored body shape: {"val":"<decimal>"}.
type counterValue struct {
	Value string `json:"val"`
}

type subjectTransform struct {
	Src  string `json:"src"`
	Dest string `json:"dest"`
}

type source struct {
	Name              string             `json:"name"`
	SubjectTransforms []subjectTransform `json:"subject_transforms,omitempty"`
}

type mirror struct {
	Name string `json:"name"`
}

type streamConfig struct {
	Name              string             `json:"name"`
	Subjects          []string           `json:"subjects,omitempty"`
	Storage           string             `json:"storage,omitempty"`
	Retention         string             `json:"retention,omitempty"`
	Discard           string             `json:"discard,omitempty"`
	Replicas          int                `json:"num_replicas,omitempty"`
	MaxMsgsPerSubject int64              `json:"max_msgs_per_subject,omitempty"`
	AllowMsgCounter   bool               `json:"allow_msg_counter,omitempty"`
	AllowMsgTTL       bool               `json:"allow_msg_ttl,omitempty"`
	Mirror            *mirror            `json:"mirror,omitempty"`
	Sources           []source           `json:"sources,omitempty"`
	SubjectTransform  *subjectTransform  `json:"subject_transform,omitempty"`
}

type streamInfoResp struct {
	Type   string         `json:"type"`
	Error  *apiError      `json:"error,omitempty"`
	Config *streamConfig  `json:"config,omitempty"`
	State  map[string]any `json:"state,omitempty"`
}

// ---- Stream lifecycle (raw $JS.API to avoid library coupling) ----

// createStream creates a stream and tracks it for cleanup. Returns the
// underlying API error (if any) wrapped in a Go error so test bodies
// can branch on success or specific failure modes.
func createStream(h *harness.Harness, cfg streamConfig) error {
	if cfg.Storage == "" {
		cfg.Storage = "file"
	}
	if cfg.Replicas == 0 {
		cfg.Replicas = 1
	}
	if len(cfg.Subjects) == 0 && cfg.Mirror == nil && len(cfg.Sources) == 0 {
		cfg.Subjects = []string{h.SubjectPrefix() + ".>"}
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	resp, err := h.NC.Request("$JS.API.STREAM.CREATE."+cfg.Name, body, 5*time.Second)
	if err != nil {
		return err
	}
	var sresp streamInfoResp
	if err := json.Unmarshal(resp.Data, &sresp); err != nil {
		return err
	}
	if sresp.Error != nil {
		return fmt.Errorf("stream create error: %s", sresp.Error)
	}
	h.TrackStream(cfg.Name)
	return nil
}

// updateStream issues a stream config update and returns the API error
// (if any).
func updateStream(h *harness.Harness, cfg streamConfig) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	resp, err := h.NC.Request("$JS.API.STREAM.UPDATE."+cfg.Name, body, 5*time.Second)
	if err != nil {
		return err
	}
	var sresp streamInfoResp
	if err := json.Unmarshal(resp.Data, &sresp); err != nil {
		return err
	}
	if sresp.Error != nil {
		return fmt.Errorf("stream update error: %s", sresp.Error)
	}
	return nil
}

// streamInfo fetches the stream's current configuration.
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

// purgeSubject purges a subject. If keep > 0, retains the most recent
// keep messages; if keep == 0, removes everything on the subject.
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

// ---- Counter publishing primitives ----

// newIncrMsg constructs a *nats.Msg with Nats-Incr set. extra headers
// are merged in first so callers can supply forbidden headers (for
// rejection tests) or audit metadata.
func newIncrMsg(subject, incr string, extra nats.Header) *nats.Msg {
	m := nats.NewMsg(subject)
	if extra != nil {
		for k, v := range extra {
			m.Header[k] = append([]string(nil), v...)
		}
	}
	m.Header.Set(HdrIncr, incr)
	return m
}

// publishIncr publishes an increment via NATS Request and decodes the
// pub ack. Caller checks ack.Error / ack.Value.
func publishIncr(h *harness.Harness, subject, incr string, extra nats.Header) (*pubAck, error) {
	return publishMsg(h, newIncrMsg(subject, incr, extra))
}

// publishMsg publishes an arbitrary *nats.Msg via Request and decodes
// the pub ack — used both for raw / no-Incr publishes and for crafted
// header-combination tests.
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

// listMsgs returns the stored messages of a stream in order.
func listMsgs(h *harness.Harness, streamName string) ([]*jetstream.RawStreamMsg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	str, err := h.JS.Stream(ctx, streamName)
	if err != nil {
		return nil, err
	}
	info, err := str.Info(ctx)
	if err != nil {
		return nil, err
	}
	var out []*jetstream.RawStreamMsg
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq; seq++ {
		m, err := str.GetMsg(ctx, seq)
		if err != nil {
			if errors.Is(err, jetstream.ErrMsgNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// lastMsgFor fetches the most recent message stored on subject in stream.
// Returns (nil, nil) when the subject has no message.
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

// ---- Counter-value helpers ----

// decodeVal parses {"val":"<decimal>"} and returns the value as a string,
// or an error if the body isn't a valid counter body.
func decodeVal(data []byte) (string, error) {
	var v counterValue
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("decode counter value: %w (raw=%q)", err, string(data))
	}
	return v.Value, nil
}

// bigEq returns true when two decimal strings represent the same
// arbitrary-precision integer. Used for value comparisons that must
// not depend on lexical formatting (leading zeros, sign on zero, ...).
func bigEq(a, b string) bool {
	ax, ok := new(big.Int).SetString(strings.TrimSpace(a), 10)
	if !ok {
		return false
	}
	bx, ok := new(big.Int).SetString(strings.TrimSpace(b), 10)
	if !ok {
		return false
	}
	return ax.Cmp(bx) == 0
}

// waitFor polls check at 50ms intervals until d elapses, returning the
// final result of check at expiry.
func waitFor(d time.Duration, check func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return check()
}

// ---- ID and naming utilities ----

func newUUID() string {
	var b [16]byte
	_, _ = readRand(b[:])
	b[6] = (b[6] & 0x0F) | 0x40
	b[8] = (b[8] & 0x3F) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// streamName returns a deterministic stream name for the current test
// and tracks it for cleanup.
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

// readRand is a thin wrapper so we can swap implementations in tests if
// needed; for now it just delegates to crypto/rand.
var readRand = func(p []byte) (int, error) {
	return cryptoRandRead(p)
}

// silence "imported and not used" when none of the optional helpers are
// referenced in the current test slice. The build still pulls these into
// dependency closure for future tests.
var _ = skip
var _ = inconclusive