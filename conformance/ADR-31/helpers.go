// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr31

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ---- ADR-31 wire-level identifiers ----

const (
	DirectGetAPIPrefix = "$JS.API.DIRECT.GET."

	// Reply headers populated by Direct Get.
	HdrStream      = "Nats-Stream"
	HdrSubject     = "Nats-Subject"
	HdrSequence    = "Nats-Sequence"
	HdrTimeStamp   = "Nats-Time-Stamp"
	HdrNumPending  = "Nats-Num-Pending"
	HdrLastSeq     = "Nats-Last-Sequence"
	HdrUpToSeq     = "Nats-UpTo-Sequence"
	HdrStatus      = "Status"
	HdrDescription = "Description"

	StatusEOB         = "204"
	StatusNotFound    = "404"
	StatusBadRequest  = "408"
	StatusTooMany     = "413"
	DescriptionEOB    = "EOB"

	defaultDirectGetTimeout = 3 * time.Second
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

// pubAck is the standard JetStream pub ack — used to capture sequences
// of pre-published test messages.
type pubAck struct {
	Stream    string    `json:"stream"`
	Sequence  uint64    `json:"seq"`
	Duplicate bool      `json:"duplicate,omitempty"`
	Error     *apiError `json:"error,omitempty"`
}

// streamSource and streamMirror are the small subset of stream
// source / mirror config used by these tests.
type streamSource struct {
	Name string `json:"name"`
}

type streamConfig struct {
	Name              string          `json:"name"`
	Subjects          []string        `json:"subjects,omitempty"`
	Storage           string          `json:"storage,omitempty"`
	Replicas          int             `json:"num_replicas,omitempty"`
	AllowDirect       bool            `json:"allow_direct,omitempty"`
	MirrorDirect      bool            `json:"mirror_direct,omitempty"`
	MaxMsgsPerSubject int64           `json:"max_msgs_per_subject,omitempty"`
	Mirror            *streamSource   `json:"mirror,omitempty"`
	Sources           []streamSource  `json:"sources,omitempty"`
}

type streamCreateResp struct {
	Type   string         `json:"type"`
	Error  *apiError      `json:"error,omitempty"`
	Config *streamConfig  `json:"config,omitempty"`
	State  map[string]any `json:"state,omitempty"`
}

// directGetReq is the JSON body for $JS.API.DIRECT.GET.<stream>.
type directGetReq struct {
	Seq          uint64     `json:"seq,omitempty"`
	LastFor      string     `json:"last_by_subj,omitempty"`
	NextFor      string     `json:"next_by_subj,omitempty"`
	Batch        int        `json:"batch,omitempty"`
	MaxBytes     int        `json:"max_bytes,omitempty"`
	StartTime    *time.Time `json:"start_time,omitempty"`
	MultiLastFor []string   `json:"multi_last,omitempty"`
	UpToSeq      uint64     `json:"up_to_seq,omitempty"`
	UpToTime     *time.Time `json:"up_to_time,omitempty"`
}

// dgReply is one wire reply on the Direct Get inbox. Status is "" for a
// success message (status line `NATS/1.0`); 204/404/408/413 for the
// documented sentinels. Headers retain the full original header map so
// tests may inspect anything (`Nats-*`, plus user-set headers like a
// pre-publish `X-Custom`).
type dgReply struct {
	Status      string
	Description string
	Headers     nats.Header
	Data        []byte
	Subject     string
}

func newDGReply(m *nats.Msg) *dgReply {
	return &dgReply{
		Status:      m.Header.Get(HdrStatus),
		Description: m.Header.Get(HdrDescription),
		Headers:     m.Header,
		Data:        m.Data,
		Subject:     m.Subject,
	}
}

// IsEOB returns true when the reply is the end-of-batch sentinel.
func (r *dgReply) IsEOB() bool { return r.Status == StatusEOB }

// IsSuccess returns true when the reply is a stored-message body.
func (r *dgReply) IsSuccess() bool { return r.Status == "" }

// HeaderSeq parses the Nats-Sequence header.
func (r *dgReply) HeaderSeq() (uint64, error) {
	v := r.Headers.Get(HdrSequence)
	if v == "" {
		return 0, fmt.Errorf("missing %s header", HdrSequence)
	}
	return strconv.ParseUint(v, 10, 64)
}

// HeaderUint parses an arbitrary unsigned integer header. Returns
// (0, false) when missing.
func (r *dgReply) HeaderUint(name string) (uint64, bool) {
	v := r.Headers.Get(name)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ---- Stream lifecycle (raw $JS.API to avoid library coupling) ----

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
	var sresp streamCreateResp
	if err := json.Unmarshal(resp.Data, &sresp); err != nil {
		return err
	}
	if sresp.Error != nil {
		return fmt.Errorf("stream create error: %s", sresp.Error)
	}
	h.TrackStream(cfg.Name)
	return nil
}

func updateStream(h *harness.Harness, cfg streamConfig) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	resp, err := h.NC.Request("$JS.API.STREAM.UPDATE."+cfg.Name, body, 5*time.Second)
	if err != nil {
		return err
	}
	var sresp streamCreateResp
	if err := json.Unmarshal(resp.Data, &sresp); err != nil {
		return err
	}
	if sresp.Error != nil {
		return fmt.Errorf("stream update error: %s", sresp.Error)
	}
	return nil
}

// streamInfo returns the parsed stream config.
func streamInfo(h *harness.Harness, name string) (*streamConfig, error) {
	resp, err := h.NC.Request("$JS.API.STREAM.INFO."+name, nil, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var info struct {
		Config *streamConfig `json:"config"`
		Error  *apiError     `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return nil, err
	}
	if info.Error != nil {
		return nil, fmt.Errorf("stream info error: %s", info.Error)
	}
	return info.Config, nil
}

// streamLastSeq returns the stream's last persisted sequence.
func streamLastSeq(h *harness.Harness, name string) (uint64, error) {
	resp, err := h.NC.Request("$JS.API.STREAM.INFO."+name, nil, 5*time.Second)
	if err != nil {
		return 0, err
	}
	var info struct {
		State struct {
			LastSeq uint64 `json:"last_seq"`
		} `json:"state"`
		Error *apiError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return 0, err
	}
	if info.Error != nil {
		return 0, fmt.Errorf("stream info error: %s", info.Error)
	}
	return info.State.LastSeq, nil
}

// awaitStreamMsgs polls the stream's reported message count until it
// reaches at least want, or the timeout fires. Used to wait for source /
// mirror catch-up, where the propagation is asynchronous.
func awaitStreamMsgs(h *harness.Harness, name string, want uint64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := h.NC.Request("$JS.API.STREAM.INFO."+name, nil, 2*time.Second)
		if err == nil {
			var info struct {
				State struct {
					Messages uint64 `json:"messages"`
				} `json:"state"`
			}
			if json.Unmarshal(resp.Data, &info) == nil && info.State.Messages >= want {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// ---- Publishing test data ----

// publishMsg publishes a JetStream message via Request and returns the
// assigned sequence. Optional headers are merged before the publish.
func publishMsg(h *harness.Harness, subject string, payload []byte, hdrs nats.Header) (uint64, error) {
	m := nats.NewMsg(subject)
	if hdrs != nil {
		for k, v := range hdrs {
			m.Header[k] = append([]string(nil), v...)
		}
	}
	m.Data = payload
	resp, err := h.NC.RequestMsg(m, 5*time.Second)
	if err != nil {
		return 0, err
	}
	var ack pubAck
	if err := json.Unmarshal(resp.Data, &ack); err != nil {
		return 0, fmt.Errorf("decode pub ack: %w (raw=%q)", err, string(resp.Data))
	}
	if ack.Error != nil {
		return 0, fmt.Errorf("publish error: %s", ack.Error)
	}
	return ack.Sequence, nil
}

// ---- Direct Get primitives ----

func directGetSubject(stream string) string { return DirectGetAPIPrefix + stream }

// directGet sends a Direct Get request and drains the inbox until the
// first reply with a non-empty Status (EOB / 4xx) or until the timeout
// fires. Returns every reply seen, in order. For requests expected to
// produce a single reply (single-message Get, error responses), the
// caller indexes [0]; for batch / multi mode, the caller iterates and
// expects the last entry to be a 204 EOB.
func directGet(h *harness.Harness, stream string, req directGetReq, timeout time.Duration) ([]*dgReply, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return rawDirectGet(h, directGetSubject(stream), body, timeout)
}

// directGetSubjectAppended issues a request to the Subject-Appended
// endpoint $JS.API.DIRECT.GET.<stream>.<tokens>. Per ADR-31, this is
// expected to be invoked with an empty payload; tests that exercise the
// "non-empty payload returns 408" rule pass a non-nil body.
func directGetSubjectAppended(h *harness.Harness, stream, tokens string, payload []byte, timeout time.Duration) ([]*dgReply, error) {
	subj := directGetSubject(stream)
	if tokens != "" {
		subj += "." + tokens
	}
	return rawDirectGet(h, subj, payload, timeout)
}

// directGetRaw sends raw bytes (used by malformed-JSON tests).
func directGetRaw(h *harness.Harness, stream string, body []byte, timeout time.Duration) ([]*dgReply, error) {
	return rawDirectGet(h, directGetSubject(stream), body, timeout)
}

func rawDirectGet(h *harness.Harness, subj string, body []byte, timeout time.Duration) ([]*dgReply, error) {
	if timeout <= 0 {
		timeout = defaultDirectGetTimeout
	}
	inbox := nats.NewInbox()
	sub, err := h.NC.SubscribeSync(inbox)
	if err != nil {
		return nil, err
	}
	defer sub.Unsubscribe()
	if err := h.NC.Flush(); err != nil {
		return nil, err
	}

	m := nats.NewMsg(subj)
	m.Reply = inbox
	m.Data = body
	if err := h.NC.PublishMsg(m); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	var out []*dgReply
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		msg, err := sub.NextMsg(remaining)
		if err != nil {
			break
		}
		rep := newDGReply(msg)
		out = append(out, rep)
		// Any non-empty Status header terminates the batch — either an
		// EOB sentinel (204) or an error (404/408/413). Plain success
		// replies have no Status and continue draining.
		if rep.Status != "" {
			break
		}
	}
	return out, nil
}

// directGetExpectTimeout sends a request and asserts no reply arrives
// within timeout. Returns true on the expected timeout, false if any
// reply was received.
func directGetExpectTimeout(h *harness.Harness, stream string, req directGetReq, timeout time.Duration) (bool, error) {
	replies, err := directGet(h, stream, req, timeout)
	if err != nil {
		return false, err
	}
	return len(replies) == 0, nil
}

// summary renders a slice of dgReplies in a compact, log-friendly form.
// Used in failure messages so test output stays scannable when the
// observed behavior is unexpected.
func summary(replies []*dgReply) string {
	if len(replies) == 0 {
		return "<no replies>"
	}
	parts := make([]string, 0, len(replies))
	for i, r := range replies {
		seq, _ := r.HeaderUint(HdrSequence)
		status := r.Status
		if status == "" {
			status = "OK"
		}
		parts = append(parts,
			fmt.Sprintf("[%d status=%s desc=%q seq=%d data=%dB]",
				i, status, r.Description, seq, len(r.Data)))
	}
	return strings.Join(parts, " ")
}

// ---- Naming utilities ----

func streamName(h *harness.Harness) string {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	return h.MintStreamName(tag)
}

// mirrorStreamName produces a deterministic mirror-stream name tied to
// the same test ID; the harness tracks both for cleanup.
func mirrorStreamName(h *harness.Harness, tag string) string {
	return h.MintStreamName(strings.ReplaceAll(h.TestID, "-", "_") + "_" + tag)
}

// ---- Result helpers wrapping harness.{Pass,Fail,...} ----

func pass() (harness.Status, string, error) { return harness.Pass() }

func fail(format string, a ...any) (harness.Status, string, error) {
	return harness.Fail(format, a...)
}

func skip(format string, a ...any) (harness.Status, string, error) {
	return harness.Skip(format, a...)
}

func inconclusive(format string, a ...any) (harness.Status, string, error) {
	return harness.Inconclusive(format, a...)
}

// readRand is a thin wrapper so we can swap implementations in tests if
// needed; for now it just delegates to crypto/rand.
var readRand = func(p []byte) (int, error) {
	return cryptoRandRead(p)
}

// silence "imported and not used" when none of the optional helpers are
// referenced in the current test slice.
var _ = inconclusive
var _ = mirrorStreamName
var _ = readRand
