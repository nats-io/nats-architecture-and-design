// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ---- ADR-50 fast-batch wire identifiers ----

const (
	// Fast-batch error codes (atomic-batch codes are in helpers.go).
	FBErrCodeNotEnabled = 10205
	FBErrCodeBadPattern = 10206
	FBErrCodeBadID      = 10207
	FBErrCodeUnknownID  = 10208

	// Reply-subject sentinel.
	FBSentinel = "$FI"

	// Operation values used in the reply subject.
	FBOpStart       = 0
	FBOpAppend      = 1
	FBOpCommitStore = 2
	FBOpCommitEOB   = 3
	FBOpPing        = 4
)

// fbInboxMsg is a deserialized message received on the per-batch inbox.
// The shape is a union of BatchFlowAck / BatchFlowGap / BatchFlowErr /
// PubAck. Tests classify by the value of Type:
//   - "ack"  — flow ack (Sequence + Messages)
//   - "gap"  — flow gap (LastSeq + Sequence)
//   - "err"  — flow error (Sequence + Error)
//   - ""     — final PubAck (no type field, has BatchID/BatchSize/Stream)
type fbInboxMsg struct {
	Type string `json:"type"`

	// BatchFlowAck
	Sequence uint64 `json:"seq"`      // shared with gap/err/PubAck
	Messages uint16 `json:"msgs"`

	// BatchFlowGap
	LastSeq uint64 `json:"last_seq"`

	// BatchFlowErr / PubAck (PubAck error is rare; see ADR-50)
	Error *apiError `json:"error,omitempty"`

	// PubAck-only fields
	Stream    string `json:"stream,omitempty"`
	BatchID   string `json:"batch,omitempty"`
	BatchSize int    `json:"count,omitempty"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

// classify returns one of "ack" / "gap" / "err" / "pubAck".
func (m *fbInboxMsg) classify() string {
	switch m.Type {
	case "ack", "gap", "err":
		return m.Type
	default:
		return "pubAck"
	}
}

// fbHandle tracks the state of one open fast-ingest batch on the wire.
// Subscriptions are old-style (per ADR-50 §"Control Channel"): each
// handle owns an inbox `<inboxPrefix>.<batch_id>.>` and only that batch.
type fbHandle struct {
	nc          *nats.Conn
	sub         *nats.Subscription
	stream      string
	batchID     string
	inboxPrefix string // e.g. "_INBOX.abc123" — unique per batch
	flow        int
	gap         string // "ok" or "fail"

	// seq is the last-published batch sequence; the next op will be
	// seq+1 unless the test deliberately skips (gap) or pings (no
	// increment per ADR §"Client Design").
	seq int
}

// openFastBatch allocates a fresh inbox + batch ID and subscribes to
// the per-batch control channel. Caller must call Close() to drain the
// subscription.
func openFastBatch(h *harness.Harness, stream string, flow int, gap string) (*fbHandle, error) {
	if flow <= 0 {
		flow = 10
	}
	if gap == "" {
		gap = "ok"
	}
	prefix := nats.NewInbox()
	id := newUUID()
	sub, err := h.NC.SubscribeSync(prefix + "." + id + ".>")
	if err != nil {
		return nil, err
	}
	if err := h.NC.Flush(); err != nil {
		_ = sub.Unsubscribe()
		return nil, err
	}
	return &fbHandle{
		nc:          h.NC,
		sub:         sub,
		stream:      stream,
		batchID:     id,
		inboxPrefix: prefix,
		flow:        flow,
		gap:         gap,
	}, nil
}

// openFastBatchWithID is like openFastBatch but lets the test choose
// the batch ID — used by FB-403 / FB-404 to exercise length boundaries.
func openFastBatchWithID(h *harness.Harness, stream, id string, flow int, gap string) (*fbHandle, error) {
	if flow <= 0 {
		flow = 10
	}
	if gap == "" {
		gap = "ok"
	}
	prefix := nats.NewInbox()
	sub, err := h.NC.SubscribeSync(prefix + "." + id + ".>")
	if err != nil {
		return nil, err
	}
	if err := h.NC.Flush(); err != nil {
		_ = sub.Unsubscribe()
		return nil, err
	}
	return &fbHandle{
		nc:          h.NC,
		sub:         sub,
		stream:      stream,
		batchID:     id,
		inboxPrefix: prefix,
		flow:        flow,
		gap:         gap,
	}, nil
}

// Close drains the inbox subscription. Safe to call more than once.
func (f *fbHandle) Close() {
	if f.sub != nil {
		_ = f.sub.Unsubscribe()
		f.sub = nil
	}
}

// replySubject builds the reply subject per ADR-50 §"Client Design":
//
//	<inbox>.<batch_id>.<flow>.<gap>.<batch_seq>.<operation>.$FI
func (f *fbHandle) replySubject(seq, op int) string {
	return fmt.Sprintf("%s.%s.%d.%s.%d.%d.%s",
		f.inboxPrefix, f.batchID, f.flow, f.gap, seq, op, FBSentinel)
}

// publish performs an op at the next batch sequence.
func (f *fbHandle) publish(subject string, op int, hdrs nats.Header, body []byte) error {
	f.seq++
	return f.publishAtSeq(subject, op, f.seq, hdrs, body)
}

// publishAtSeq performs an op at an explicit batch sequence without
// advancing the local counter. Used for ping (op 4: must reuse the
// last sent seq) and for tests that deliberately skip sequences to
// trigger gap detection.
func (f *fbHandle) publishAtSeq(subject string, op, seq int, hdrs nats.Header, body []byte) error {
	m := nats.NewMsg(subject)
	if hdrs != nil {
		for k, v := range hdrs {
			m.Header[k] = append([]string(nil), v...)
		}
	}
	m.Reply = f.replySubject(seq, op)
	m.Data = body
	return f.nc.PublishMsg(m)
}

// publishWithRawReply lets a test override the reply subject directly
// — used by FB-401/FB-402/FB-406 to exercise malformed reply patterns.
func (f *fbHandle) publishWithRawReply(subject, reply string, hdrs nats.Header, body []byte) error {
	m := nats.NewMsg(subject)
	if hdrs != nil {
		for k, v := range hdrs {
			m.Header[k] = append([]string(nil), v...)
		}
	}
	m.Reply = reply
	m.Data = body
	return f.nc.PublishMsg(m)
}

// readNext returns the next inbox message, classified.
func (f *fbHandle) readNext(timeout time.Duration) (*fbInboxMsg, error) {
	msg, err := f.sub.NextMsg(timeout)
	if err != nil {
		return nil, err
	}
	if len(msg.Data) == 0 {
		// A zero-byte response is not part of the fast-batch wire
		// types; surface it explicitly so tests can flag it.
		return &fbInboxMsg{}, fmt.Errorf("unexpected zero-byte inbox message on %s", msg.Subject)
	}
	var fm fbInboxMsg
	if err := json.Unmarshal(msg.Data, &fm); err != nil {
		return nil, fmt.Errorf("decode fb inbox msg: %w (raw=%q)", err, string(msg.Data))
	}
	return &fm, nil
}

// awaitFlowAck reads from the inbox until a BatchFlowAck arrives,
// surfacing any preceding gap/err/PubAck as an error. Useful for
// asserting on the very first server response.
func (f *fbHandle) awaitFlowAck(timeout time.Duration) (*fbInboxMsg, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m, err := f.readNext(time.Until(deadline))
		if err != nil {
			return nil, err
		}
		if m.classify() == "ack" {
			return m, nil
		}
		// Anything else surfaces as the bug: tests that need to mix
		// will use readNext directly.
		return m, fmt.Errorf("expected BatchFlowAck, got %s (%+v)", m.classify(), m)
	}
	return nil, fmt.Errorf("timed out waiting for BatchFlowAck")
}

// awaitPubAck drains the inbox until the final PubAck arrives, ignoring
// flow acks/gaps/errs along the way.
func (f *fbHandle) awaitPubAck(timeout time.Duration) (*fbInboxMsg, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m, err := f.readNext(time.Until(deadline))
		if err != nil {
			return nil, err
		}
		if m.classify() == "pubAck" {
			return m, nil
		}
	}
	return nil, fmt.Errorf("timed out waiting for PubAck")
}

// drainUntilTypes reads until a message matching any of the requested
// types arrives or the timeout expires. Returns the matched message
// and the slice of intermediate (skipped) messages so a test can
// assert on them too.
func (f *fbHandle) drainUntilTypes(timeout time.Duration, want ...string) (*fbInboxMsg, []*fbInboxMsg, error) {
	deadline := time.Now().Add(timeout)
	var skipped []*fbInboxMsg
	for time.Now().Before(deadline) {
		m, err := f.readNext(time.Until(deadline))
		if err != nil {
			return nil, skipped, err
		}
		cls := m.classify()
		for _, w := range want {
			if cls == w {
				return m, skipped, nil
			}
		}
		skipped = append(skipped, m)
	}
	return nil, skipped, fmt.Errorf("timed out waiting for one of %v", want)
}