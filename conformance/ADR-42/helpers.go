// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr42

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// ---- ADR-42 wire-level identifiers ----

const (
	// Headers added by ADR-42.
	HdrPinID = "Nats-Pin-Id"

	// Status header / description carriers used on pull replies.
	HdrStatus      = "Status"
	HdrDescription = "Description"

	// 423 is returned for pin mismatches; the description varies by
	// server version (2.11 returned a generic string; 2.12+ uses
	// "Nats-Wrong-Pin-Id" / "Nats-Pin-Id mismatch"). Tests assert on
	// the code only.
	StatusPinMismatch = "423"

	// Priority policies.
	PolicyOverflow      = "overflow"
	PolicyPinnedClient  = "pinned_client"
	PolicyPrioritized   = "prioritized"

	// API subject prefixes.
	APIConsumerCreate = "$JS.API.CONSUMER.CREATE."
	APIConsumerInfo   = "$JS.API.CONSUMER.INFO."
	APIConsumerDelete = "$JS.API.CONSUMER.DELETE."
	APIConsumerMsgNxt = "$JS.API.CONSUMER.MSG.NEXT."
	APIConsumerUnpin  = "$JS.API.CONSUMER.UNPIN."

	// Advisory subjects.
	AdvisoryPinnedPrefix   = "$JS.EVENT.ADVISORY.CONSUMER.PINNED."
	AdvisoryUnpinnedPrefix = "$JS.EVENT.ADVISORY.CONSUMER.UNPINNED."
	AdvisoryPinnedType     = "io.nats.jetstream.advisory.v1.consumer_group_pinned"
	AdvisoryUnpinnedType   = "io.nats.jetstream.advisory.v1.consumer_group_unpinned"
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

// pubAck is the JetStream pub ack returned for normal publishes.
type pubAck struct {
	Stream   string    `json:"stream"`
	Sequence uint64    `json:"seq"`
	Error    *apiError `json:"error,omitempty"`
}

type streamConfig struct {
	Name     string   `json:"name"`
	Subjects []string `json:"subjects,omitempty"`
	Storage  string   `json:"storage,omitempty"`
	Replicas int      `json:"num_replicas,omitempty"`
}

type streamCreateResp struct {
	Type   string         `json:"type"`
	Error  *apiError      `json:"error,omitempty"`
	Config *streamConfig  `json:"config,omitempty"`
	State  map[string]any `json:"state,omitempty"`
}

// consumerConfig is the subset of consumer configuration relevant to
// ADR-42. Encoded as JSON it goes into the create / update request
// payload's `config` field.
type consumerConfig struct {
	Durable         string   `json:"durable_name,omitempty"`
	Name            string   `json:"name,omitempty"`
	AckPolicy       string   `json:"ack_policy,omitempty"`
	DeliverSubject  string   `json:"deliver_subject,omitempty"`
	FilterSubject   string   `json:"filter_subject,omitempty"`
	PriorityGroups  []string `json:"priority_groups,omitempty"`
	PriorityPolicy  string   `json:"priority_policy,omitempty"`
	PriorityTimeout int64    `json:"priority_timeout,omitempty"` // ns
}

// consumerCreateReq is the wire payload accepted by
// $JS.API.CONSUMER.CREATE.<stream>[.<consumer>]. Pedantic toggles
// strict-mode validation server-side.
type consumerCreateReq struct {
	Stream   string         `json:"stream_name"`
	Config   consumerConfig `json:"config"`
	Action   string         `json:"action,omitempty"`
	Pedantic bool           `json:"pedantic,omitempty"`
}

// consumerInfoResp is the parsed response from $JS.API.CONSUMER.CREATE
// and $JS.API.CONSUMER.INFO. Only the fields we assert on are decoded.
type consumerInfoResp struct {
	Type   string `json:"type"`
	Stream string `json:"stream_name"`
	Name   string `json:"name"`
	Config struct {
		AckPolicy       string   `json:"ack_policy"`
		PriorityGroups  []string `json:"priority_groups,omitempty"`
		PriorityPolicy  string   `json:"priority_policy,omitempty"`
		PriorityTimeout int64    `json:"priority_timeout,omitempty"`
	} `json:"config"`
	PriorityGroups []priorityGroupState `json:"priority_groups,omitempty"`
	Error          *apiError            `json:"error,omitempty"`
}

type priorityGroupState struct {
	Group          string     `json:"name"`
	PinnedClientId string     `json:"pinned_id,omitempty"`
	PinnedTs       *time.Time `json:"pinned_ts,omitempty"`
}

// pullRequest is the JSON payload for $JS.API.CONSUMER.MSG.NEXT.<...>.
// Only the fields ADR-42 introduces (plus the standard batch / expires)
// are encoded.
type pullRequest struct {
	Batch         int    `json:"batch,omitempty"`
	NoWait        bool   `json:"no_wait,omitempty"`
	Expires       int64  `json:"expires,omitempty"` // ns
	Group         string `json:"group,omitempty"`
	MinPending    int64  `json:"min_pending,omitempty"`
	MinAckPending int64  `json:"min_ack_pending,omitempty"`
	Failover      int    `json:"failover,omitempty"`
	ID            string `json:"id,omitempty"`
	Priority      int    `json:"priority,omitempty"`
}

// pullReply is one reply received on a pull-request inbox. Status is
// "" for a successful message delivery; otherwise it carries the wire
// status code (e.g. "423", "408", "409", "100" for heartbeats).
type pullReply struct {
	Status      string
	Description string
	Headers     nats.Header
	Data        []byte
	Subject     string
}

func newPullReply(m *nats.Msg) *pullReply {
	return &pullReply{
		Status:      m.Header.Get(HdrStatus),
		Description: m.Header.Get(HdrDescription),
		Headers:     m.Header,
		Data:        m.Data,
		Subject:     m.Subject,
	}
}

// IsMessage returns true when the reply carries an actual stream
// message body (no Status header).
func (r *pullReply) IsMessage() bool { return r.Status == "" }

// IsHeartbeat returns true for the standard 100 idle heartbeat.
func (r *pullReply) IsHeartbeat() bool { return r.Status == "100" }

// PinID returns the value of the Nats-Pin-Id header, or "" if absent.
func (r *pullReply) PinID() string { return r.Headers.Get(HdrPinID) }

// ---- Stream lifecycle (raw $JS.API to avoid library coupling) ----

func createStream(h *harness.Harness, cfg streamConfig) error {
	if cfg.Storage == "" {
		cfg.Storage = "file"
	}
	if cfg.Replicas == 0 {
		cfg.Replicas = 1
	}
	if len(cfg.Subjects) == 0 {
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

// streamName mints and tracks a per-test stream name.
func streamName(h *harness.Harness) string {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	return h.MintStreamName(tag)
}

// ---- Consumer lifecycle (raw $JS.API) ----

// createConsumer issues $JS.API.CONSUMER.CREATE with the given config.
// If the config carries Durable or Name, the request is addressed to
// that explicit consumer (creates-or-updates depending on action).
func createConsumer(h *harness.Harness, stream string, cfg consumerConfig) (*consumerInfoResp, error) {
	return doConsumerCreate(h, stream, cfg, "", false)
}

// createConsumerPedantic is like createConsumer but sets pedantic=true,
// which makes the server enforce strict validation (e.g. reject silent
// AckPolicy coercion).
func createConsumerPedantic(h *harness.Harness, stream string, cfg consumerConfig) (*consumerInfoResp, error) {
	return doConsumerCreate(h, stream, cfg, "", true)
}

// updateConsumer issues a CONSUMER.CREATE with action="update" so the
// server treats it as a strict update rather than create-or-update.
func updateConsumer(h *harness.Harness, stream string, cfg consumerConfig) (*consumerInfoResp, error) {
	return doConsumerCreate(h, stream, cfg, "update", false)
}

func doConsumerCreate(h *harness.Harness, stream string, cfg consumerConfig, action string, pedantic bool) (*consumerInfoResp, error) {
	name := cfg.Name
	if name == "" {
		name = cfg.Durable
	}
	if name == "" {
		return nil, fmt.Errorf("consumer config requires Name or Durable")
	}
	subj := APIConsumerCreate + stream + "." + name
	body, err := json.Marshal(consumerCreateReq{
		Stream:   stream,
		Config:   cfg,
		Action:   action,
		Pedantic: pedantic,
	})
	if err != nil {
		return nil, err
	}
	resp, err := h.NC.Request(subj, body, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var info consumerInfoResp
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return nil, fmt.Errorf("decode consumer info: %w (raw=%q)", err, string(resp.Data))
	}
	if info.Error != nil {
		return &info, fmt.Errorf("consumer create error: %s", info.Error)
	}
	return &info, nil
}

// consumerInfo fetches the live consumer info via $JS.API.CONSUMER.INFO.
func consumerInfo(h *harness.Harness, stream, consumer string) (*consumerInfoResp, error) {
	resp, err := h.NC.Request(APIConsumerInfo+stream+"."+consumer, nil, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var info consumerInfoResp
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return nil, fmt.Errorf("decode consumer info: %w (raw=%q)", err, string(resp.Data))
	}
	if info.Error != nil {
		return &info, fmt.Errorf("consumer info error: %s", info.Error)
	}
	return &info, nil
}

// consumerName builds a deterministic per-test consumer name. Stable
// across retries within a single run, but isolated by test ID.
func consumerName(h *harness.Harness, tag string) string {
	id := strings.ReplaceAll(h.TestID, "-", "_")
	if tag == "" {
		return id + "_C"
	}
	return id + "_" + tag
}

// ---- Publishing test data ----

func publishMsg(h *harness.Harness, subject string, payload []byte) (uint64, error) {
	m := nats.NewMsg(subject)
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

// publishN publishes n messages on subject, payloads "0"…"n-1".
func publishN(h *harness.Harness, subject string, n int) error {
	for i := 0; i < n; i++ {
		if _, err := publishMsg(h, subject, []byte(fmt.Sprintf("%d", i))); err != nil {
			return err
		}
	}
	return nil
}

// ---- Pull request primitives ----

// pull sends a single pull request and drains replies until either
// `expectedReplies` are received OR the timeout fires. Heartbeats (100)
// are NOT included in the returned slice. The caller-supplied inbox is
// the pull's reply subject — re-using the same inbox across pulls
// models a single client; using fresh inboxes models distinct clients.
func pull(h *harness.Harness, stream, consumer string, req pullRequest, inbox string, timeout time.Duration) ([]*pullReply, error) {
	if inbox == "" {
		inbox = nats.NewInbox()
	}
	sub, err := h.NC.SubscribeSync(inbox)
	if err != nil {
		return nil, err
	}
	defer sub.Unsubscribe()
	if err := h.NC.Flush(); err != nil {
		return nil, err
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := h.NC.PublishRequest(APIConsumerMsgNxt+stream+"."+consumer, inbox, body); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	var out []*pullReply
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		msg, err := sub.NextMsg(remaining)
		if err != nil {
			break
		}
		rep := newPullReply(msg)
		if rep.IsHeartbeat() {
			continue
		}
		out = append(out, rep)
	}
	return out, nil
}

// pullStreaming opens a long-lived pull and returns a channel of replies
// (excluding heartbeats), plus a stop function that cancels the pull's
// subscription. Used by tests that need to sample what arrives on a
// given inbox while other operations happen elsewhere.
func pullStreaming(h *harness.Harness, stream, consumer string, req pullRequest, inbox string) (<-chan *pullReply, func(), error) {
	if inbox == "" {
		inbox = nats.NewInbox()
	}
	ch := make(chan *pullReply, 64)
	sub, err := h.NC.Subscribe(inbox, func(m *nats.Msg) {
		rep := newPullReply(m)
		if rep.IsHeartbeat() {
			return
		}
		select {
		case ch <- rep:
		default:
		}
	})
	if err != nil {
		return nil, nil, err
	}
	if err := h.NC.Flush(); err != nil {
		_ = sub.Unsubscribe()
		return nil, nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		_ = sub.Unsubscribe()
		return nil, nil, err
	}
	if err := h.NC.PublishRequest(APIConsumerMsgNxt+stream+"."+consumer, inbox, body); err != nil {
		_ = sub.Unsubscribe()
		return nil, nil, err
	}
	stop := func() {
		_ = sub.Unsubscribe()
	}
	return ch, stop, nil
}

// drainNext reads the next reply from a streaming channel within
// timeout. Returns (reply, true) on a delivery, (nil, false) on timeout
// or a closed channel.
func drainNext(ch <-chan *pullReply, timeout time.Duration) (*pullReply, bool) {
	select {
	case rep, ok := <-ch:
		if !ok {
			return nil, false
		}
		return rep, true
	case <-time.After(timeout):
		return nil, false
	}
}

// drainAll reads all replies that arrive within timeout from a
// streaming channel.
func drainAll(ch <-chan *pullReply, timeout time.Duration) []*pullReply {
	deadline := time.Now().Add(timeout)
	var out []*pullReply
	for time.Now().Before(deadline) {
		rep, ok := drainNext(ch, time.Until(deadline))
		if !ok {
			break
		}
		out = append(out, rep)
	}
	return out
}

// ---- UNPIN API ----

// unpin sends a request to $JS.API.CONSUMER.UNPIN.<stream>.<consumer>
// with the given JSON payload. Returns the raw reply body for the
// caller to assert error / success on.
func unpin(h *harness.Harness, stream, consumer string, payload []byte) ([]byte, error) {
	resp, err := h.NC.Request(APIConsumerUnpin+stream+"."+consumer, payload, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// unpinGroup is sugar for the most common UNPIN call shape.
func unpinGroup(h *harness.Harness, stream, consumer, group string) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{"group": group})
	return unpin(h, stream, consumer, body)
}

// ---- Advisories ----

// advisoryEvent decodes the common envelope of pinned/unpinned advisory
// messages. Only fields we assert on are present.
type advisoryEvent struct {
	Type     string `json:"type"`
	Stream   string `json:"stream"`
	Consumer string `json:"consumer"`
	Group    string `json:"group"`
	PinnedID string `json:"pinned_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// captureAdvisories subscribes to subject and accumulates advisory
// events whose `type` field matches one of wanted. Returns a snapshot
// getter and a cancel function.
func captureAdvisories(h *harness.Harness, subject string, wanted ...string) (func() []advisoryEvent, func(), error) {
	mu := sync.Mutex{}
	var got []advisoryEvent
	want := map[string]bool{}
	for _, w := range wanted {
		want[w] = true
	}
	sub, err := h.NC.Subscribe(subject, func(m *nats.Msg) {
		var ev advisoryEvent
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return
		}
		if len(want) > 0 && !want[ev.Type] {
			return
		}
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	})
	if err != nil {
		return nil, nil, err
	}
	if err := h.NC.Flush(); err != nil {
		_ = sub.Unsubscribe()
		return nil, nil, err
	}
	get := func() []advisoryEvent {
		mu.Lock()
		defer mu.Unlock()
		out := make([]advisoryEvent, len(got))
		copy(out, got)
		return out
	}
	return get, func() { _ = sub.Unsubscribe() }, nil
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

// readRand is a thin wrapper so we can swap implementations in tests.
var readRand = func(p []byte) (int, error) {
	return cryptoRandRead(p)
}

// silence "imported and not used" when not every helper is referenced
// by the current set of tests.
var _ = skip
var _ = readRand
