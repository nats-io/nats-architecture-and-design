// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0

// Package harness defines the shared types every conformance group plugs
// into: Status / Result, Test, Group, FlagSpec, Options, Harness, plus a
// process-global Group registry that the runner enumerates.
//
// Each conformance group (one per ADR or feature area) lives in its own
// package under conformance/<NAME>/ and registers itself with Register()
// from a package-level init(). The runner imports those packages purely
// for their side effects so the user can run any combination via the CLI.
package harness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// ---- Status and Result ----------------------------------------------------

// Status is the outcome of a single conformance test.
type Status string

const (
	StatusPass         Status = "PASS"
	StatusFail         Status = "FAIL"
	StatusSkip         Status = "SKIP"
	StatusWarn         Status = "WARN"
	StatusInconclusive Status = "INCONCLUSIVE"
	StatusError        Status = "ERROR" // panic / harness fault distinct from FAIL
)

// Result is the per-test outcome captured in the run report.
type Result struct {
	Group     string        `json:"group"`
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Section   string        `json:"section,omitempty"`
	Tags      []string      `json:"tags,omitempty"`
	Status    Status        `json:"status"`
	Detail    string        `json:"detail,omitempty"`
	StartedAt time.Time     `json:"started_at"`
	Elapsed   time.Duration `json:"elapsed_ns"`
}

// ---- Tests, Groups, Flags -----------------------------------------------

// FlagType is the wire type of a group-registered flag.
type FlagType string

const (
	FlagString   FlagType = "string"
	FlagBool     FlagType = "bool"
	FlagInt      FlagType = "int"
	FlagDuration FlagType = "duration"
)

// FlagSpec describes a CLI flag registered by a Group. The runner adds
// every group's flags to the `run` subcommand so that any group can be
// configured directly: `conformance run ADR-50-AB --dedup=false`. Names
// MUST be globally unique across groups; collisions panic at startup.
type FlagSpec struct {
	Name    string
	Help    string
	Type    FlagType
	Default string

	// Group is filled in by Register so reports can attribute a flag
	// back to its owning group; callers should leave it zero.
	Group string
}

// Test is a single conformance check. The Run function returns the
// observed Status, an optional human-readable detail, and an error;
// non-nil err is reported as StatusFail with err.Error() as detail.
//
// SkipReason is consulted by the runner before Run executes: a non-empty
// string yields a Skip with that reason. This is the place to gate on
// server features (e.g. "requires API level 4") so skipped tests still
// appear in reports.
type Test struct {
	ID         string
	Title      string
	Section    string
	Tags       []string
	SkipReason func(opts *Options) string
	Run        func(ctx context.Context, h *Harness) (Status, string, error)
}

// Group is a named collection of Tests sharing setup and flags. Groups
// register themselves into the global registry from init().
type Group struct {
	Name        string   // unique stable identifier, e.g. "ADR-50-AB"
	Title       string   // one-line human-readable title
	Description string   // multi-line guidance shown by `conformance list`
	References  []string // links to ADRs or conformance docs
	Flags       []FlagSpec
	Tests       []Test

	// Setup runs once before the group's tests. Useful for per-run
	// resource setup (e.g. wiping leftover streams). Optional.
	Setup func(ctx context.Context, h *Harness) error

	// Teardown runs once after all of the group's tests (in addition
	// to Harness.Cleanup, which always runs). Optional.
	Teardown func(ctx context.Context, h *Harness) error
}

// ---- Options ----------------------------------------------------------

// Options is the parsed flag bag passed to SkipReason callbacks and
// reachable from any Test via h.Opts. Keys are flag names from FlagSpec;
// every key looked up via the typed accessors falls back to the supplied
// default when unset, so tests don't need to reason about flag presence.
type Options struct {
	mu   sync.RWMutex
	vals map[string]string
}

// NewOptions returns an empty Options bag.
func NewOptions() *Options {
	return &Options{vals: map[string]string{}}
}

// Set stores a string value for a flag.
func (o *Options) Set(name, value string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.vals == nil {
		o.vals = map[string]string{}
	}
	o.vals[name] = value
}

// String reads a flag as a string, falling back to def when unset.
func (o *Options) String(name, def string) string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if v, ok := o.vals[name]; ok {
		return v
	}
	return def
}

// Bool reads a flag as a bool. Missing/invalid values fall back to def.
func (o *Options) Bool(name string, def bool) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if v, ok := o.vals[name]; ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

// Int reads a flag as int. Missing/invalid values fall back to def.
func (o *Options) Int(name string, def int) int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if v, ok := o.vals[name]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Duration reads a flag as time.Duration. Missing/invalid values fall back to def.
func (o *Options) Duration(name string, def time.Duration) time.Duration {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if v, ok := o.vals[name]; ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Snapshot returns a copy of all stored values.
func (o *Options) Snapshot() map[string]string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make(map[string]string, len(o.vals))
	for k, v := range o.vals {
		out[k] = v
	}
	return out
}

// ---- Harness ---------------------------------------------------------

// Harness is the per-run context handed to each test. It owns the NATS
// connection, the JetStream context, parsed Options, and stream tracking
// for cleanup. The runner sets Group/TestID before calling each test so
// helpers like SubjectPrefix() can produce per-test subject namespaces.
type Harness struct {
	NC *nats.Conn
	JS jetstream.JetStream

	Opts    *Options
	Group   string // currently-running group name
	TestID  string // currently-running test id
	Verbose bool

	// APILevel and ServerVersion are populated by the runner via Detect.
	APILevel      int
	ServerVersion string

	mu      sync.Mutex
	streams []string
}

// Detect populates the Harness with server-reported metadata. The runner
// calls this once after Connect, before any tests run.
func (h *Harness) Detect(ctx context.Context) {
	h.ServerVersion = h.NC.ConnectedServerVersion()
	resp, err := h.NC.Request("$JS.API.INFO", nil, 5*time.Second)
	if err != nil {
		h.APILevel = 0
		return
	}
	var info struct {
		API struct {
			Level int `json:"level"`
		} `json:"api"`
	}
	if err := json.Unmarshal(resp.Data, &info); err == nil {
		h.APILevel = info.API.Level
	}
}

// SubjectPrefix returns nats.adr.conformance.<TestID>. Tests publish
// under this prefix and streams declare it as their subject filter, so
// concurrent groups / tests don't collide on the wire.
func (h *Harness) SubjectPrefix() string {
	id := h.TestID
	if id == "" {
		id = "unknown"
	}
	return "nats.adr.conformance." + id
}

// Subject returns SubjectPrefix() + "." + leaf.
func (h *Harness) Subject(leaf string) string {
	if leaf == "" {
		return h.SubjectPrefix()
	}
	return h.SubjectPrefix() + "." + leaf
}

// MintStreamName returns a unique tracked stream name. Tag is folded in
// for readability so leftover names hint at which test minted them.
func (h *Harness) MintStreamName(tag string) string {
	var raw [4]byte
	_, _ = rand.Read(raw[:])
	name := fmt.Sprintf("CONF_%s_%s", sanitizeName(tag), hex.EncodeToString(raw[:]))
	h.TrackStream(name)
	return name
}

// TrackStream records name for cleanup at the end of the test/run.
func (h *Harness) TrackStream(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.streams = append(h.streams, name)
}

// SnapshotStreams returns the set of currently tracked stream names.
func (h *Harness) SnapshotStreams() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.streams))
	copy(out, h.streams)
	return out
}

// ResetStreams clears the tracked-stream set; the runner calls this
// after per-test cleanup so the same names aren't deleted twice.
func (h *Harness) ResetStreams() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.streams
	h.streams = nil
	return out
}

// DeleteStream is a best-effort delete via raw JS API (so it works
// regardless of which jetstream.StreamConfig fields are exposed).
func (h *Harness) DeleteStream(name string) {
	_, _ = h.NC.Request("$JS.API.STREAM.DELETE."+name, nil, 5*time.Second)
}

// StreamsBySubject lists streams whose subject filter matches subject.
func (h *Harness) StreamsBySubject(subject string) ([]string, error) {
	body, _ := json.Marshal(map[string]string{"subject": subject})
	resp, err := h.NC.Request("$JS.API.STREAM.NAMES", body, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var out struct {
		Streams []string `json:"streams"`
		Error   *struct {
			Code        int    `json:"code"`
			ErrCode     int    `json:"err_code,omitempty"`
			Description string `json:"description,omitempty"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp.Data, &out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, fmt.Errorf("stream names: code=%d err_code=%d desc=%q",
			out.Error.Code, out.Error.ErrCode, out.Error.Description)
	}
	return out.Streams, nil
}

// Cleanup deletes all tracked streams. The runner calls this after
// every test (per-test cleanup) and once more at end-of-run.
func (h *Harness) Cleanup() {
	for _, name := range h.ResetStreams() {
		h.DeleteStream(name)
	}
}

// ---- Registry --------------------------------------------------------

var (
	regMu sync.Mutex
	reg   = map[string]*Group{}
)

// Register installs a Group into the process-global registry. Called
// from each group package's init(). Panics on a duplicate Name.
func Register(g *Group) {
	regMu.Lock()
	defer regMu.Unlock()
	if g == nil || g.Name == "" {
		panic("harness.Register: nil group or empty Name")
	}
	if _, exists := reg[g.Name]; exists {
		panic(fmt.Sprintf("harness.Register: duplicate group name %q", g.Name))
	}
	for i := range g.Flags {
		g.Flags[i].Group = g.Name
	}
	reg[g.Name] = g
}

// Groups returns every registered Group, sorted by Name.
func Groups() []*Group {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]*Group, 0, len(reg))
	for _, g := range reg {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// FindGroup returns the named Group or nil.
func FindGroup(name string) *Group {
	regMu.Lock()
	defer regMu.Unlock()
	return reg[name]
}

// ---- Result helpers --------------------------------------------------

// Pass returns a Status, "", nil triple suitable for a Test.Run return.
// Helpers below are sugar so tests read cleanly:
//
//	return harness.Pass()
//	return harness.Fail("expected X, got %v", got)
func Pass() (Status, string, error) { return StatusPass, "", nil }

func Fail(format string, a ...any) (Status, string, error) {
	return StatusFail, fmt.Sprintf(format, a...), nil
}

func Skip(format string, a ...any) (Status, string, error) {
	return StatusSkip, fmt.Sprintf(format, a...), nil
}

func Inconclusive(format string, a ...any) (Status, string, error) {
	return StatusInconclusive, fmt.Sprintf(format, a...), nil
}

// ---- internal --------------------------------------------------------

func sanitizeName(in string) string {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		c := in[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}