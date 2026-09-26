// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
	terminal "golang.org/x/term"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// Progress receives live notifications as the runner works through tests.
// A nil Progress disables live output entirely.
type Progress interface {
	Start(idx, total int, group string, t harness.Test)
	Done(idx, total int, r harness.Result)
	Finish()
}

// NewProgress returns a TTY renderer for terminals and a plain
// line-by-line renderer otherwise.
func NewProgress(w io.Writer, color bool) Progress {
	if w == nil {
		return nil
	}
	if isTerminal(w) {
		return &ttyProgress{w: w, color: color}
	}
	return &lineProgress{w: w}
}

type ttyProgress struct {
	w     io.Writer
	color bool
}

func (p *ttyProgress) Start(idx, total int, group string, t harness.Test) {
	fmt.Fprintf(p.w, "\r\x1b[K     [%*d/%d] %s / %s ...",
		digits(total), idx, total, group, t.ID)
}

func (p *ttyProgress) Done(idx, total int, r harness.Result) {
	status := renderStatus(r.Status, p.color)
	detail := ""
	if r.Status != harness.StatusPass && r.Detail != "" {
		detail = "  " + r.Detail
	}
	fmt.Fprintf(p.w, "\r\x1b[K%s [%*d/%d] %s / %s  (%s)%s\n",
		status, digits(total), idx, total, r.Group, r.ID,
		r.Elapsed.Round(time.Millisecond), detail)
}

func (p *ttyProgress) Finish() {
	fmt.Fprint(p.w, "\r\x1b[K")
}

type lineProgress struct{ w io.Writer }

func (p *lineProgress) Start(int, int, string, harness.Test) {}

func (p *lineProgress) Done(idx, total int, r harness.Result) {
	detail := ""
	if r.Status != harness.StatusPass && r.Detail != "" {
		detail = "  " + r.Detail
	}
	fmt.Fprintf(p.w, "%s [%*d/%d] %s / %s  (%s)%s\n",
		statusLabel(r.Status), digits(total), idx, total, r.Group, r.ID,
		r.Elapsed.Round(time.Millisecond), detail)
}

func (p *lineProgress) Finish() {}

// statusLabel returns a fixed-width 4-character display label for s,
// so progress lines and the table's Status column align cleanly. The
// canonical Status name stays in the JSON report; this is purely
// presentation.
func statusLabel(s harness.Status) string {
	switch s {
	case harness.StatusInconclusive:
		return "INCO"
	case harness.StatusError:
		return "ERR "
	default:
		// PASS, FAIL, SKIP, WARN are already 4 chars.
		return string(s)
	}
}

func renderStatus(s harness.Status, useColor bool) string {
	label := statusLabel(s)
	if !useColor {
		return label
	}
	switch s {
	case harness.StatusPass:
		return text.FgGreen.Sprint(label)
	case harness.StatusWarn, harness.StatusInconclusive:
		return text.FgYellow.Sprint(label)
	case harness.StatusFail, harness.StatusError:
		return text.FgRed.Sprint(label)
	case harness.StatusSkip:
		return text.FgCyan.Sprint(label)
	}
	return label
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return terminal.IsTerminal(int(f.Fd()))
}

func digits(n int) int {
	d := 1
	for n >= 10 {
		n /= 10
		d++
	}
	return d
}