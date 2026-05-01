// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// runRun is the shared implementation of every `run *` sub-command.
// Reads runner-level flags from the package-level runShared global so
// the per-subcommand wiring stays minimal.
func runRun(groups []*harness.Group, opts *harness.Options) error {
	if len(groups) == 0 {
		return fmt.Errorf("no groups to run")
	}

	tagFilter := splitCSV(runShared.tags)

	nctx, err := connectContext(runShared.context)
	if err != nil {
		return err
	}
	nc, err := nctx.Connect(nats.Name("nats-adr-conformance"))
	if err != nil {
		return fmt.Errorf("nats connect: %w", err)
	}
	defer nc.Drain()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("jetstream context: %w", err)
	}

	h := &harness.Harness{
		NC:      nc,
		JS:      js,
		Opts:    opts,
		Verbose: runShared.verbose,
	}

	parentCtx, cancel := installSignalHandler(context.Background())
	defer cancel()

	h.Detect(parentCtx)

	report := &Report{
		StartedAt:     time.Now(),
		Context:       runShared.context,
		ServerVersion: h.ServerVersion,
		APILevel:      h.APILevel,
		ConnectedURL:  nc.ConnectedAddr(),
		Options:       opts.Snapshot(),
	}

	var prog Progress
	if !runShared.noProgress {
		prog = NewProgress(os.Stderr, !runShared.noColor)
	}

	for _, g := range groups {
		runGroup(parentCtx, h, g, &runFilter{matchRE: runShared.match, tags: tagFilter}, report, prog, runShared.testTimeout)
		// Final per-group sweep: any stream still listening on the
		// shared subject namespace is a leak from a panic'd test.
		if names, err := h.StreamsBySubject("nats.adr.conformance.>"); err == nil {
			for _, n := range names {
				h.DeleteStream(n)
			}
		}
	}

	if prog != nil {
		prog.Finish()
	}
	report.FinishedAt = time.Now()

	if err := writeReport(report, os.Stdout); err != nil {
		return err
	}

	if report.HasFailures() {
		fmt.Fprintln(os.Stderr, "conformance failed")
		os.Exit(1)
	}
	if runShared.failOnSkip && report.Counts()[harness.StatusSkip] > 0 {
		fmt.Fprintln(os.Stderr, "conformance has skipped tests (--fail-on-skip)")
		os.Exit(1)
	}
	return nil
}

type runFilter struct {
	matchRE *regexp.Regexp
	tags    []string
}

// runGroup executes every test in g, applying the filter and any
// per-test SkipReason. Each test is wrapped with per-test cleanup so
// state cannot leak from one test into the next.
func runGroup(ctx context.Context, h *harness.Harness, g *harness.Group, f *runFilter, report *Report, prog Progress, testTimeout time.Duration) {
	h.Group = g.Name

	if g.Setup != nil {
		if err := g.Setup(ctx, h); err != nil {
			report.Results = append(report.Results, harness.Result{
				Group:     g.Name,
				ID:        g.Name + ".setup",
				Title:     "Group setup",
				Status:    harness.StatusError,
				Detail:    err.Error(),
				StartedAt: time.Now(),
			})
			return
		}
	}

	tests := selectTests(g.Tests, f)
	total := len(tests)

	for i, t := range tests {
		idx := i + 1
		if prog != nil {
			prog.Start(idx, total, g.Name, t)
		}

		started := time.Now()
		res := executeTest(ctx, h, g, t, testTimeout, started)

		if prog != nil {
			prog.Done(idx, total, res)
		}
		report.Results = append(report.Results, res)

		// Per-test cleanup. Always runs, even on panic.
		h.Cleanup()
	}

	if g.Teardown != nil {
		_ = g.Teardown(ctx, h)
	}
	h.Group = ""
	h.TestID = ""
}

// executeTest runs a single test with a hard timeout, recovering from
// panics so a single buggy test cannot abort the run.
func executeTest(ctx context.Context, h *harness.Harness, g *harness.Group, t harness.Test, timeout time.Duration, started time.Time) harness.Result {
	res := harness.Result{
		Group:     g.Name,
		ID:        t.ID,
		Title:     t.Title,
		Section:   t.Section,
		Tags:      append([]string(nil), t.Tags...),
		StartedAt: started,
	}

	if reason := skipReason(t, h.Opts); reason != "" {
		res.Status = harness.StatusSkip
		res.Detail = reason
		res.Elapsed = time.Since(started)
		return res
	}

	select {
	case <-ctx.Done():
		res.Status = harness.StatusSkip
		res.Detail = "canceled before run"
		res.Elapsed = time.Since(started)
		return res
	default:
	}

	h.TestID = t.ID

	type outcome struct {
		status harness.Status
		detail string
		err    error
	}
	done := make(chan outcome, 1)
	var panicMsg atomic.Value

	go func() {
		defer func() {
			if rv := recover(); rv != nil {
				panicMsg.Store(fmt.Sprintf("panic: %v", rv))
				done <- outcome{}
			}
		}()
		s, d, err := t.Run(ctx, h)
		done <- outcome{status: s, detail: d, err: err}
	}()

	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case o := <-done:
		if pv := panicMsg.Load(); pv != nil {
			res.Status = harness.StatusError
			res.Detail = pv.(string)
		} else if o.err != nil {
			res.Status = harness.StatusFail
			if o.detail != "" {
				res.Detail = o.detail + ": " + o.err.Error()
			} else {
				res.Detail = o.err.Error()
			}
		} else {
			res.Status = o.status
			res.Detail = o.detail
		}
	case <-tctx.Done():
		res.Status = harness.StatusFail
		res.Detail = fmt.Sprintf("timed out after %s", timeout)
	}

	res.Elapsed = time.Since(started)
	return res
}

// skipReason returns "" when the test should run.
func skipReason(t harness.Test, opts *harness.Options) string {
	if t.SkipReason == nil {
		return ""
	}
	return t.SkipReason(opts)
}

// selectGroups resolves user-provided selectors (substring match against
// group names) into the actual list of Groups to run. The literal "all"
// selects every registered group; an empty selector list returns no
// groups (the runner uses Required() to force an explicit choice).
func selectGroups(selectors []string) ([]*harness.Group, error) {
	all := harness.Groups()
	if len(selectors) == 0 {
		return nil, nil
	}

	for _, sel := range selectors {
		if strings.EqualFold(sel, "all") {
			return all, nil
		}
	}

	var out []*harness.Group
	matched := map[string]bool{}
	for _, sel := range selectors {
		for _, g := range all {
			if matched[g.Name] {
				continue
			}
			if g.Name == sel || strings.Contains(strings.ToLower(g.Name), strings.ToLower(sel)) {
				out = append(out, g)
				matched[g.Name] = true
			}
		}
	}
	return out, nil
}

// selectTests applies the per-run filter to a group's test list.
func selectTests(tests []harness.Test, f *runFilter) []harness.Test {
	if f == nil || (f.matchRE == nil && len(f.tags) == 0) {
		return tests
	}
	var out []harness.Test
	for _, t := range tests {
		if f.matchRE != nil && !f.matchRE.MatchString(t.ID) {
			continue
		}
		if len(f.tags) > 0 && !anyTagMatch(t.Tags, f.tags) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func anyTagMatch(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if h == w {
				return true
			}
		}
	}
	return false
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runList implements `conformance list`. An empty group selector
// lists every registered group; a non-empty selector substring-matches
// against group names.
func runList(groupSel, tagSel string, showTests bool) error {
	var groups []*harness.Group
	if groupSel == "" {
		groups = harness.Groups()
	} else {
		var err error
		groups, err = selectGroups([]string{groupSel})
		if err != nil {
			return err
		}
		if len(groups) == 0 {
			return fmt.Errorf("no groups matched %q", groupSel)
		}
	}
	tagFilter := splitCSV(tagSel)
	return writeListing(os.Stdout, groups, tagFilter, showTests)
}