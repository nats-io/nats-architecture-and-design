// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/nats-io/natscli/columns"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// Report is the aggregate outcome of a `conformance run`. It is the
// canonical persisted artifact: writing the same Report through
// WriteJSON repeatedly yields a stable, replayable history of runs.
type Report struct {
	Context       string            `json:"context,omitempty"`
	ConnectedURL  string            `json:"connected_url,omitempty"`
	ServerVersion string            `json:"server_version,omitempty"`
	APILevel      int               `json:"api_level,omitempty"`
	Options       map[string]string `json:"options,omitempty"`
	StartedAt     time.Time         `json:"started_at"`
	FinishedAt    time.Time         `json:"finished_at"`
	Results       []harness.Result  `json:"results"`
}

// HasFailures returns true if any FAIL or ERROR result was recorded.
func (r *Report) HasFailures() bool {
	for _, res := range r.Results {
		if res.Status == harness.StatusFail || res.Status == harness.StatusError {
			return true
		}
	}
	return false
}

// Counts tallies per-Status totals.
func (r *Report) Counts() map[harness.Status]int {
	out := map[harness.Status]int{
		harness.StatusPass:         0,
		harness.StatusWarn:         0,
		harness.StatusFail:         0,
		harness.StatusSkip:         0,
		harness.StatusInconclusive: 0,
		harness.StatusError:        0,
	}
	for _, res := range r.Results {
		out[res.Status]++
	}
	return out
}

// GroupCounts tallies per-status counts within each group.
func (r *Report) GroupCounts() map[string]map[harness.Status]int {
	out := map[string]map[harness.Status]int{}
	for _, res := range r.Results {
		m, ok := out[res.Group]
		if !ok {
			m = map[harness.Status]int{}
			out[res.Group] = m
		}
		m[res.Status]++
	}
	return out
}

// writeReport emits the report to stdout in the format requested by
// runShared, and optionally also to a file. The file is always written
// as JSON, regardless of the stdout format, so it is machine-replayable.
func writeReport(r *Report, stdout io.Writer) error {
	if runShared.json {
		if err := writeJSON(r, stdout); err != nil {
			return err
		}
	} else {
		if err := writeText(r, stdout, runShared.verbose, !runShared.noColor); err != nil {
			return err
		}
	}

	if runShared.output != "" {
		f, err := os.Create(runShared.output)
		if err != nil {
			return fmt.Errorf("open output: %w", err)
		}
		defer f.Close()
		if err := writeJSON(r, f); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	return nil
}

func writeJSON(r *Report, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// writeText renders the columns header + a per-test go-pretty table.
func writeText(r *Report, w io.Writer, verbose, color bool) error {
	counts := r.Counts()
	elapsed := r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond)

	cw := columns.New("NATS Conformance Run")
	if r.Context != "" {
		cw.AddRow("Context", r.Context)
	}
	if r.ConnectedURL != "" {
		cw.AddRow("Connected", r.ConnectedURL)
	}
	if r.ServerVersion != "" {
		cw.AddRow("Server Version", r.ServerVersion)
	}
	if r.APILevel > 0 {
		cw.AddRow("Server API Level", r.APILevel)
	}
	cw.AddRow("Started", r.StartedAt.Format(time.RFC3339))
	cw.AddRow("Elapsed", elapsed.String())

	if len(r.Options) > 0 {
		cw.AddSectionTitle("Options")
		keys := make([]string, 0, len(r.Options))
		for k := range r.Options {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			cw.AddRow(k, r.Options[k])
		}
	}

	cw.AddSectionTitle("Totals")
	cw.AddRow("Pass", counts[harness.StatusPass])
	cw.AddRow("Fail", counts[harness.StatusFail])
	cw.AddRow("Skip", counts[harness.StatusSkip])
	cw.AddRow("Inconclusive", counts[harness.StatusInconclusive])
	cw.AddRow("Warn", counts[harness.StatusWarn])
	cw.AddRow("Error", counts[harness.StatusError])

	if gc := r.GroupCounts(); len(gc) > 1 {
		cw.AddSectionTitle("Per Group")
		groupNames := make([]string, 0, len(gc))
		for n := range gc {
			groupNames = append(groupNames, n)
		}
		sort.Strings(groupNames)
		for _, n := range groupNames {
			c := gc[n]
			cw.AddRow(n, fmt.Sprintf("pass=%d fail=%d skip=%d inconclusive=%d",
				c[harness.StatusPass], c[harness.StatusFail],
				c[harness.StatusSkip], c[harness.StatusInconclusive]))
		}
	}

	if err := cw.Frender(w); err != nil {
		return err
	}
	fmt.Fprintln(w)

	tbl := table.NewWriter()
	tbl.SetStyle(table.StyleRounded)
	tbl.Style().Title.Align = text.AlignCenter
	tbl.Style().Format.Header = text.FormatDefault
	tbl.SetTitle("Conformance Results")
	tbl.AppendHeader(table.Row{"Status", "Group", "ID", "Title", "Detail", "Time"})

	useColor := color && isTerminal(w)
	var lastGroup string
	rendered := 0
	for _, res := range r.Results {
		if !verbose && res.Status == harness.StatusPass {
			continue
		}
		if res.Group != lastGroup && lastGroup != "" {
			tbl.AppendSeparator()
		}
		lastGroup = res.Group
		tbl.AppendRow(table.Row{
			renderStatus(res.Status, useColor),
			res.Group,
			res.ID,
			truncate(res.Title, 50),
			truncate(res.Detail, 80),
			res.Elapsed.Round(time.Millisecond),
		})
		rendered++
	}

	if rendered == 0 {
		fmt.Fprintln(w, "All checks passed. Re-run with --verbose for per-check detail.")
		return nil
	}

	fmt.Fprintln(w, tbl.Render())
	return nil
}

// writeListing implements `conformance list` output.
func writeListing(w io.Writer, groups []*harness.Group, tagFilter []string, showTests bool) error {
	cw := columns.New("Registered Conformance Groups")
	for _, g := range groups {
		cw.AddSectionTitle(g.Name)

		cw.AddRowIfNotEmpty("Title", g.Title)
		cw.AddRowIf("References", g.References, len(g.References) > 0)

		if len(g.Flags) > 0 {
			flagNames := make([]string, len(g.Flags))
			for i, f := range g.Flags {
				flagNames[i] = fmt.Sprintf("--%s (%s, default=%q)", f.Name, f.Type, f.Default)
			}
			cw.AddRow("Flags", flagNames)
		}
		cw.AddRow("Tests", len(g.Tests))
		cw.AddRowIfNotEmpty("Description", strings.TrimSpace(g.Description))
	}
	if err := cw.Frender(w); err != nil {
		return err
	}

	if !showTests {
		return nil
	}

	for _, g := range groups {
		fmt.Fprintln(w)
		tbl := table.NewWriter()
		tbl.SetStyle(table.StyleRounded)
		tbl.Style().Title.Align = text.AlignCenter
		tbl.SetTitle(g.Name + " — Tests")
		tbl.AppendHeader(table.Row{"ID", "Section", "Title", "Tags"})
		for _, t := range g.Tests {
			if !tagFilterMatch(t.Tags, tagFilter) {
				continue
			}
			tbl.AppendRow(table.Row{
				t.ID,
				t.Section,
				truncate(t.Title, 60),
				strings.Join(t.Tags, ","),
			})
		}
		fmt.Fprintln(w, tbl.Render())
	}
	return nil
}

func tagFilterMatch(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	return anyTagMatch(have, want)
}

// writeReadme emits a Markdown index of every registered conformance
// group, sorted in numerical ADR order. Per-group output is a short
// overview (title, references, flags, test count) followed by a
// pipe-delimited table of every test the group registers.
//
// Designed to be redirected straight to a file:
//
//	conformance readme > conformance/README.md
func writeReadme(w io.Writer, groups []*harness.Group) error {
	sortGroupsByADR(groups)

	fmt.Fprintln(w, "# NATS Conformance Tests")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This index lists every conformance test group registered by the runner.")
	fmt.Fprintln(w, "Each group corresponds to an ADR (or a sub-feature within one) and ships its")
	fmt.Fprintln(w, "own flag set; see the per-group sections below for details.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Generated by `conformance readme` — do not hand-edit.")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "## Groups at a glance")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Group | Title | Tests |")
	fmt.Fprintln(w, "|-------|-------|-------|")
	total := 0
	for _, g := range groups {
		fmt.Fprintf(w, "| [`%s`](#%s) | %s | %d |\n",
			g.Name, anchorID(g.Name), mdEscape(g.Title), len(g.Tests))
		total += len(g.Tests)
	}
	fmt.Fprintf(w, "| **Total** | | **%d** |\n", total)
	fmt.Fprintln(w)

	for _, g := range groups {
		fmt.Fprintf(w, "## %s\n\n", g.Name)
		if g.Title != "" {
			fmt.Fprintf(w, "**%s**\n\n", mdEscape(g.Title))
		}
		if len(g.References) > 0 {
			fmt.Fprintln(w, "**References**")
			fmt.Fprintln(w)
			for _, ref := range g.References {
				fmt.Fprintf(w, "- [`%s`](%s)\n", ref, ref)
			}
			fmt.Fprintln(w)
		}
		if len(g.Flags) > 0 {
			fmt.Fprintln(w, "**Flags**")
			fmt.Fprintln(w)
			fmt.Fprintln(w, "| Flag | Type | Default | Description |")
			fmt.Fprintln(w, "|------|------|---------|-------------|")
			for _, f := range g.Flags {
				fmt.Fprintf(w, "| `--%s` | %s | `%s` | %s |\n",
					f.Name, f.Type, f.Default, mdEscape(f.Help))
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "**Tests**: %d\n\n", len(g.Tests))

		fmt.Fprintln(w, "| ID | Section | Title | Tags |")
		fmt.Fprintln(w, "|----|---------|-------|------|")
		for _, t := range g.Tests {
			tags := strings.Join(t.Tags, ", ")
			fmt.Fprintf(w, "| `%s` | %s | %s | %s |\n",
				t.ID, t.Section, mdEscape(t.Title), tags)
		}
		fmt.Fprintln(w)
	}
	return nil
}

// sortGroupsByADR sorts groups in place by the numeric ADR identifier
// embedded in the group name, then by suffix. Group names that don't
// match the `ADR-N-...` pattern fall to the end in lexical order so a
// future non-ADR group does not silently disrupt ordering.
func sortGroupsByADR(groups []*harness.Group) {
	sort.SliceStable(groups, func(i, j int) bool {
		ai, si := parseADRName(groups[i].Name)
		aj, sj := parseADRName(groups[j].Name)
		switch {
		case ai != aj:
			return ai < aj
		case si != sj:
			return si < sj
		default:
			return groups[i].Name < groups[j].Name
		}
	})
}

// parseADRName extracts the numeric ADR id and the trailing suffix from
// a group name like "ADR-50-FB" -> (50, "FB"). Names that don't match
// return (math.MaxInt, name) so they sort to the end deterministically.
func parseADRName(name string) (int, string) {
	const prefix = "ADR-"
	if !strings.HasPrefix(name, prefix) {
		return adrSentinel, name
	}
	rest := name[len(prefix):]
	dash := strings.IndexByte(rest, '-')
	numStr := rest
	suffix := ""
	if dash >= 0 {
		numStr = rest[:dash]
		suffix = rest[dash+1:]
	}
	n := 0
	for _, c := range numStr {
		if c < '0' || c > '9' {
			return adrSentinel, name
		}
		n = n*10 + int(c-'0')
	}
	return n, suffix
}

const adrSentinel = 1 << 30

// anchorID converts a heading to its GitHub-style markdown anchor.
func anchorID(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
}

// mdEscape returns s with characters that have meaning in a Markdown
// table cell escaped (just `|` and embedded newlines for now — group
// titles, test titles, and flag help don't generally contain other
// markdown specials).
func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
