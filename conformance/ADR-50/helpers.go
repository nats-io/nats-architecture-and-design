// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ---- ADR-50 wire-level identifiers ----

const (
	HdrBatchID         = "Nats-Batch-Id"
	HdrBatchSequence   = "Nats-Batch-Sequence"
	HdrBatchCommit     = "Nats-Batch-Commit"
	HdrRequiredAPILvl  = "Nats-Required-Api-Level"
	HdrExpLastSeq      = "Nats-Expected-Last-Sequence"
	HdrExpLastSubjSeq  = "Nats-Expected-Last-Subject-Sequence"
	HdrExpLastMsgID    = "Nats-Expected-Last-Msg-Id"
	HdrMsgID           = "Nats-Msg-Id"

	ErrCodeNotEnabled     = 10174
	ErrCodeMissingSeq     = 10175
	ErrCodeIncomplete     = 10176
	ErrCodeUnsupportedHdr = 10177
	ErrCodeBadID          = 10179
	ErrCodeSeqLimit       = 10199
	ErrCodeDuplicate      = 10201

	AdvisorySubjectPrefix = "$JS.EVENT.ADVISORY.>"
	AdvisoryBatchAbandon  = "io.nats.jetstream.advisory.v1.stream_batch_abandoned"
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
	Stream    string    `json:"stream"`
	Sequence  uint64    `json:"seq"`
	Duplicate bool      `json:"duplicate,omitempty"`
	BatchID   string    `json:"batch,omitempty"`
	BatchSize int       `json:"count,omitempty"`
	Error     *apiError `json:"error,omitempty"`
}

type streamConfig struct {
	Name               string   `json:"name"`
	Subjects           []string `json:"subjects,omitempty"`
	Storage            string   `json:"storage,omitempty"`
	Replicas           int      `json:"num_replicas,omitempty"`
	AllowAtomicPublish bool     `json:"allow_atomic,omitempty"`
	AllowBatchPublish  bool     `json:"allow_batched,omitempty"`
	PersistMode        string   `json:"persist_mode,omitempty"`
	Mirror             *source  `json:"mirror,omitempty"`
	Sources            []source `json:"sources,omitempty"`
	Duplicates         int64    `json:"duplicate_window,omitempty"` // ns
}

type source struct {
	Name string `json:"name"`
}

type streamCreateResp struct {
	Type   string         `json:"type"`
	Error  *apiError      `json:"error,omitempty"`
	Config *streamConfig  `json:"config,omitempty"`
	State  map[string]any `json:"state,omitempty"`
}

type batchAdvisory struct {
	Type    string `json:"type"`
	Stream  string `json:"stream"`
	BatchID string `json:"batch"`
	Reason  string `json:"reason"`
}

// ---- Stream lifecycle (raw $JS.API to avoid library coupling) ----

func createStream(h *harness.Harness, cfg streamConfig) error {
	if cfg.Storage == "" {
		cfg.Storage = "file"
	}
	if cfg.Replicas == 0 {
		cfg.Replicas = 1
	}
	if len(cfg.Subjects) == 0 && cfg.Mirror == nil {
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

// ---- Batch publishing primitives ----

// newBatchMsg constructs a *nats.Msg with the standard batch headers
// pre-populated. hdrs are merged in first so callers can supply
// per-test headers like Nats-Expected-Last-Sequence.
func newBatchMsg(subject, batchID string, seq int, commit string, hdrs nats.Header, body []byte) *nats.Msg {
	m := nats.NewMsg(subject)
	if hdrs != nil {
		for k, v := range hdrs {
			m.Header[k] = append([]string(nil), v...)
		}
	}
	m.Header.Set(HdrBatchID, batchID)
	m.Header.Set(HdrBatchSequence, fmt.Sprintf("%d", seq))
	if commit != "" {
		m.Header.Set(HdrBatchCommit, commit)
	}
	m.Data = body
	return m
}

// publishRequest publishes msg via NATS Request and decodes the pub ack.
// A zero-byte reply (used for non-final batch members) returns an empty
// pubAck with no error — callers check ack.Error / ack.BatchID.
func publishRequest(h *harness.Harness, m *nats.Msg, timeout time.Duration) (*pubAck, error) {
	resp, err := h.NC.RequestMsg(m, timeout)
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

func publishFireAndForget(h *harness.Harness, m *nats.Msg) error {
	return h.NC.PublishMsg(m)
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

// ---- Advisories ----

// captureAdvisories starts a background subscription on
// $JS.EVENT.ADVISORY.> filtering for batch-abandoned events. Returns a
// getter that snapshots events seen so far and a cancel func that
// unsubscribes when the test is done.
func captureAdvisories(h *harness.Harness) (func() []batchAdvisory, func()) {
	mu := sync.Mutex{}
	var captured []batchAdvisory
	sub, err := h.NC.Subscribe(AdvisorySubjectPrefix, func(m *nats.Msg) {
		var a batchAdvisory
		if err := json.Unmarshal(m.Data, &a); err != nil {
			return
		}
		if a.Type != AdvisoryBatchAbandon {
			return
		}
		mu.Lock()
		captured = append(captured, a)
		mu.Unlock()
	})
	if err != nil {
		return func() []batchAdvisory { return nil }, func() {}
	}
	get := func() []batchAdvisory {
		mu.Lock()
		defer mu.Unlock()
		out := make([]batchAdvisory, len(captured))
		copy(out, captured)
		return out
	}
	return get, func() { _ = sub.Unsubscribe() }
}

// waitFor polls check at 50ms intervals until d elapses, returning the
// final result of check at expiry. Used to wait for asynchronous
// conditions like advisories arriving.
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
	// 16 random bytes formatted as 8-4-4-4-12 — independent of any
	// uuid package so this file has zero non-stdlib deps beyond nats.
	var b [16]byte
	_, _ = readRand(b[:])
	b[6] = (b[6] & 0x0F) | 0x40
	b[8] = (b[8] & 0x3F) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// streamName returns a deterministic stream name for the current test
// and tracks it for cleanup. The name encodes the test ID so leftovers
// (from a panicked test) are easy to attribute.
func streamName(h *harness.Harness) string {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	return h.MintStreamName(tag)
}

// ---- Result helpers wrapping harness.{Pass,Fail,...} ----

// pass / fail / skip / inconclusive thin wrappers so test bodies stay
// short and consistent. The harness package returns the (Status,
// detail, error) triple expected by Test.Run.

func pass() (harness.Status, string, error)                     { return harness.Pass() }
func fail(format string, a ...any) (harness.Status, string, error) { return harness.Fail(format, a...) }
func skip(format string, a ...any) (harness.Status, string, error) { return harness.Skip(format, a...) }
func inconclusive(format string, a ...any) (harness.Status, string, error) {
	return harness.Inconclusive(format, a...)
}

// ---- internal: crypto/rand without an explicit import in this file ----

// readRand is a thin wrapper so we can swap implementations in tests if
// needed; for now it just delegates to crypto/rand via a closure set
// once at init.
var readRand func(p []byte) (int, error) = func(p []byte) (int, error) {
	return cryptoRandRead(p)
}

// cryptoRandRead is defined in helpers_rand.go to keep this file's
// import list focused on protocol-level concerns.