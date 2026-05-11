// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr51

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

// ---- ADR-51 wire-level identifiers ----

const (
	// Schedule-defining headers (set by the publisher of a schedule).
	HdrSchedule         = "Nats-Schedule"
	HdrScheduleTarget   = "Nats-Schedule-Target"
	HdrScheduleSource   = "Nats-Schedule-Source"
	HdrScheduleTTL      = "Nats-Schedule-TTL"
	HdrScheduleTimeZone = "Nats-Schedule-Time-Zone"
	HdrScheduleRollup   = "Nats-Schedule-Rollup"

	// Headers the server adds to messages produced from a schedule.
	HdrScheduler    = "Nats-Scheduler"
	HdrScheduleNext = "Nats-Schedule-Next"

	// Standard JetStream headers reused by schedules.
	HdrTTL                  = "Nats-TTL"
	HdrRollup               = "Nats-Rollup"
	HdrExpLastSubjectSeq    = "Nats-Expected-Last-Subject-Sequence"
	HdrExpLastSubjectSeqSub = "Nats-Expected-Last-Subject-Sequence-Subject"

	// Sentinel values.
	ScheduleNextPurge = "purge"
	RollupSub         = "sub"

	// Server error codes.
	ErrCodeSchedulerInvalid        = 10212 // JSMessageSchedulesSchedulerInvalidErr — Nats-Scheduler self-target / empty / not a valid publish subject.
	ErrCodeScheduleTimeZoneInvalid = 10223 // JSMessageSchedulesTimeZoneInvalidErr — Nats-Schedule-Time-Zone empty value, fixed offset, or unresolvable name.
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

type subjectTransform struct {
	Src  string `json:"src"`
	Dest string `json:"dest"`
}

type source struct {
	Name              string             `json:"name"`
	FilterSubject     string             `json:"filter_subject,omitempty"`
	SubjectTransforms []subjectTransform `json:"subject_transforms,omitempty"`
}

type mirror struct {
	Name string `json:"name"`
}

// streamConfig only carries fields the ADR-51 tests touch. Everything
// here uses the canonical JetStream JSON tags so the harness talks
// directly to $JS.API.STREAM.* without library coupling.
type streamConfig struct {
	Name              string   `json:"name"`
	Subjects          []string `json:"subjects,omitempty"`
	Storage           string   `json:"storage,omitempty"`
	Retention         string   `json:"retention,omitempty"`
	Discard           string   `json:"discard,omitempty"`
	Replicas          int      `json:"num_replicas,omitempty"`
	MaxAge            int64    `json:"max_age,omitempty"` // ns
	AllowMsgSchedules bool     `json:"allow_msg_schedules,omitempty"`
	AllowMsgTTL       bool     `json:"allow_msg_ttl,omitempty"`
	AllowRollup       bool     `json:"allow_rollup_hdrs,omitempty"`
	DenyPurge         bool     `json:"deny_purge,omitempty"`
	Mirror            *mirror  `json:"mirror,omitempty"`
	Sources           []source `json:"sources,omitempty"`
}

type streamInfoResp struct {
	Type   string         `json:"type"`
	Error  *apiError      `json:"error,omitempty"`
	Config *streamConfig  `json:"config,omitempty"`
	State  map[string]any `json:"state,omitempty"`
}

// ---- Stream lifecycle (raw $JS.API to avoid library coupling) ----

// createStream creates a stream and tracks it for cleanup. Returns the
// underlying API error wrapped in a Go error so tests can branch on
// success or specific failure modes.
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
// (if any) wrapped in a Go error.
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

// purgeSubject purges a subject (or wildcard) on a stream.
func purgeSubject(h *harness.Harness, stream, subject string) error {
	req := struct {
		Filter string `json:"filter,omitempty"`
	}{Filter: subject}
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

// deleteMsg removes a message from the stream by sequence.
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

// ---- Schedule publishing primitives ----

// schedHeader holds one header to inject on the publish; using a slice
// keeps test bodies linear and avoids accidentally setting an ambiguous
// nats.Header literal across files.
type schedHeader struct{ K, V string }

// newScheduleMsg builds a nats.Msg with the supplied headers in order.
// Later entries overwrite earlier ones for the same key.
func newScheduleMsg(subject string, body []byte, hdrs ...schedHeader) *nats.Msg {
	m := nats.NewMsg(subject)
	for _, hh := range hdrs {
		m.Header.Set(hh.K, hh.V)
	}
	m.Data = body
	return m
}

// publishSchedule publishes a message via NATS Request and decodes the
// pub ack. Used both for placing schedules and for atomic-stop publishes.
func publishSchedule(h *harness.Harness, subject string, body []byte, hdrs ...schedHeader) (*pubAck, error) {
	return publishMsg(h, newScheduleMsg(subject, body, hdrs...))
}

// publishMsg publishes any *nats.Msg and decodes the pub ack — used for
// raw publishes that prepare source-subject state for sampling tests.
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

// subjectCount reports how many messages on `subject` are currently
// stored in `stream`. Implemented via STREAM.INFO with a subjects
// filter; missing entries indicate zero messages.
func subjectCount(h *harness.Harness, stream, subject string) (uint64, error) {
	body, _ := json.Marshal(struct {
		Subject string `json:"subjects_filter"`
	}{Subject: subject})
	resp, err := h.NC.Request("$JS.API.STREAM.INFO."+stream, body, 5*time.Second)
	if err != nil {
		return 0, err
	}
	var out struct {
		State struct {
			Subjects map[string]uint64 `json:"subjects"`
		} `json:"state"`
		Error *apiError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return 0, err
	}
	if out.Error != nil {
		return 0, fmt.Errorf("stream info error: %s", out.Error)
	}
	if c, ok := out.State.Subjects[subject]; ok {
		return c, nil
	}
	// Fall back to walking subjects when the filter returned a wildcard
	// expansion: we sum every concrete subject matching the requested
	// pattern. For non-wildcard inputs this is equivalent to the lookup
	// above, which short-circuits.
	if !strings.ContainsAny(subject, "*>") {
		return 0, nil
	}
	var total uint64
	for _, c := range out.State.Subjects {
		total += c
	}
	return total, nil
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

// waitForLastMsgOn polls for the latest message on subject in stream
// until timeout elapses. Returns the message if it arrives, or an error
// describing the timeout.
func waitForLastMsgOn(h *harness.Harness, stream, subject string, timeout time.Duration) (*jetstream.RawStreamMsg, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m, err := lastMsgFor(h, stream, subject)
		if err != nil {
			return nil, err
		}
		if m != nil {
			return m, nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return nil, fmt.Errorf("no message on %s within %s", subject, timeout)
}

// waitForCountOn polls until at least n messages have been observed on
// subject in stream, or until the timeout fires. Returns the latest
// observed count.
func waitForCountOn(h *harness.Harness, stream, subject string, n uint64, timeout time.Duration) (uint64, error) {
	deadline := time.Now().Add(timeout)
	var count uint64
	for time.Now().Before(deadline) {
		c, err := subjectCount(h, stream, subject)
		if err == nil {
			count = c
			if count >= n {
				return count, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return count, fmt.Errorf("only saw %d/%d messages on %s within %s", count, n, subject, timeout)
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

// rfc3339In returns an RFC3339-formatted timestamp d in the future from
// now, in UTC.
func rfc3339In(d time.Duration) string {
	return time.Now().UTC().Add(d).Format(time.RFC3339)
}

// rfc3339InZone returns an RFC3339 timestamp d in the future, expressed
// in the supplied location (the timezone offset is preserved verbatim
// in the formatted output). Falls back to UTC if loc is nil.
func rfc3339InZone(d time.Duration, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Add(d).Format(time.RFC3339)
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

// readRand is a thin wrapper so tests can stub randomness if needed.
var readRand = func(p []byte) (int, error) {
	return cryptoRandRead(p)
}

// silence "imported and not used" warnings when only a subset of
// helpers is referenced by the currently-built test list.
var (
	_ = skip
	_ = inconclusive
	_ = listMsgs
	_ = waitForCountOn
	_ = newUUID
)
