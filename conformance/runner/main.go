// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

// Command conformance runs registered conformance test groups against a
// running NATS deployment.
//
// Each group is exposed as a sub-command of `run`, with only that
// group's flags attached — adding new groups doesn't bloat a single
// flag list. `run all` runs every registered group with each group's
// default flag values.
//
// Usage examples:
//
//	conformance list
//	conformance list -t                            # detailed test table
//	conformance run all                            # every group, defaults
//	conformance run adr-50-ab                      # one group with its flags
//	conformance run adr-50-ab --dedup --eob --match "^AB-2"
//	conformance run adr-50-fb --slow=false
//	conformance run adr-50-ab -c devcluster --json -o report.json
//
// Each conformance group lives in its own package under
// conformance/<NAME>/ and is wired in via the blank imports below.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/choria-io/fisk"
	"github.com/nats-io/jsm.go/natscontext"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"

	// Blank imports trigger each group's init() so it lands in the
	// shared registry. Add a new group by adding a line here.
	_ "github.com/nats-io/nats-architecture-and-design/conformance/ADR-31"
	_ "github.com/nats-io/nats-architecture-and-design/conformance/ADR-42"
	_ "github.com/nats-io/nats-architecture-and-design/conformance/ADR-49"
	_ "github.com/nats-io/nats-architecture-and-design/conformance/ADR-50"
	_ "github.com/nats-io/nats-architecture-and-design/conformance/ADR-51"
)

var version = "dev"

// listOpts holds parsed flag/arg values for the `list` subcommand.
var listOpts struct {
	group     string
	tags      string
	showTests bool
}

// runShared holds flags that apply to every `run` sub-command. They
// are declared on the `run` parent so fisk makes them available on
// any descendant — `conformance run adr-50-ab --context X` works.
var runShared struct {
	context     string
	match       string
	tags        string
	json        bool
	output      string
	verbose     bool
	noColor     bool
	noProgress  bool
	testTimeout time.Duration
	failOnSkip  bool
}

func main() {
	app := fisk.New("conformance", "Run NATS conformance test groups against a deployment")
	app.Author("The NATS Authors <info@nats.io>")
	app.Version(version)
	app.HelpFlag.Short('h')
	app.UsageWriter(os.Stdout)

	listCmd := app.Command("list", "List registered conformance groups and their tests").Alias("ls").Action(listAction)
	listCmd.Arg("group", "Limit to a single group (substring match)").StringVar(&listOpts.group)
	listCmd.Flag("tags", "Comma-separated tag filter (any-of)").StringVar(&listOpts.tags)
	listCmd.Flag("tests", "Show per-test detail").Short('t').BoolVar(&listOpts.showTests)

	app.Command("readme", "Emit a Markdown index of every registered conformance group to stdout").Action(readmeAction)

	runCmd := app.Command("run", "Execute conformance groups (use a sub-command per group, or `all`)")
	runCmd.Flag("context", "NATS context to connect with (default: selected context)").Short('c').StringVar(&runShared.context)
	runCmd.Flag("match", "Regex applied to test IDs within selected groups").Short('m').StringVar(&runShared.match)
	runCmd.Flag("tags", "Comma-separated tag filter (any-of)").StringVar(&runShared.tags)
	runCmd.Flag("json", "Emit a machine-readable JSON report instead of a table").BoolVar(&runShared.json)
	runCmd.Flag("output", "Write report to a file (alongside stdout)").Short('o').StringVar(&runShared.output)
	runCmd.Flag("verbose", "Show PASS rows in addition to other statuses").Short('v').BoolVar(&runShared.verbose)
	runCmd.Flag("no-color", "Disable colored output").BoolVar(&runShared.noColor)
	runCmd.Flag("no-progress", "Disable live progress on stderr").BoolVar(&runShared.noProgress)
	runCmd.Flag("test-timeout", "Per-test hard timeout").Default("60s").DurationVar(&runShared.testTimeout)
	runCmd.Flag("fail-on-skip", "Exit non-zero when any test is SKIP").BoolVar(&runShared.failOnSkip)

	// `all` runs every registered group with each group's default flag values.
	runCmd.Command("all", "Run every registered conformance group with each group's default flag values").Action(runAllAction)

	// One sub-command per registered group, populated dynamically from
	// the registry. Each sub-command shows only its own flags.
	for _, g := range harness.Groups() {
		registerGroupCommand(runCmd, g)
	}

	app.MustParseWithUsage(os.Args[1:])
}

// listAction is the entrypoint for `conformance list`.
func listAction(_ *fisk.ParseContext) error {
	return runList(listOpts.group, listOpts.tags, listOpts.showTests)
}

// readmeAction is the entrypoint for `conformance readme`.
func readmeAction(_ *fisk.ParseContext) error {
	return writeReadme(os.Stdout, harness.Groups())
}

// runAllAction is the entrypoint for `conformance run all`. Each group
// runs with its declared default flag values.
func runAllAction(_ *fisk.ParseContext) error {
	opts := harness.NewOptions()
	for _, g := range harness.Groups() {
		for _, f := range g.Flags {
			opts.Set(f.Name, f.Default)
		}
	}
	return runRun(harness.Groups(), opts)
}

// registerGroupCommand creates a `conformance run <group>` sub-command
// that accepts only that group's declared flags. Group flag names live
// in their own per-subcommand namespace, so two groups can declare a
// `--cluster` without colliding — `run adr-50-ab --cluster` and
// `run adr-50-fb --cluster` are independent toggles.
func registerGroupCommand(parent *fisk.CmdClause, g *harness.Group) {
	sub := parent.Command(strings.ToLower(g.Name), g.Title)

	getters := map[string]func() string{}

	for _, f := range g.Flags {
		fc := sub.Flag(f.Name, f.Help).Default(f.Default)
		switch f.Type {
		case harness.FlagBool:
			var v bool
			fc.BoolVar(&v)
			getters[f.Name] = func() string { return fmt.Sprintf("%t", v) }
		case harness.FlagInt:
			var v int
			fc.IntVar(&v)
			getters[f.Name] = func() string { return fmt.Sprintf("%d", v) }
		case harness.FlagDuration:
			var v time.Duration
			fc.DurationVar(&v)
			getters[f.Name] = func() string { return v.String() }
		default:
			var v string
			fc.StringVar(&v)
			getters[f.Name] = func() string { return v }
		}
	}

	groupRef := g
	sub.Action(func(_ *fisk.ParseContext) error {
		opts := harness.NewOptions()
		for name, get := range getters {
			opts.Set(name, get())
		}
		return runRun([]*harness.Group{groupRef}, opts)
	})
}

// connectContext loads the named context (or the selected one if name
// is empty) and returns it.
func connectContext(name string) (*natscontext.Context, error) {
	nctx, err := natscontext.New(name, true)
	if err != nil {
		return nil, fmt.Errorf("load context %q: %w", name, err)
	}
	return nctx, nil
}

// installSignalHandler returns a context cancelled on SIGINT/SIGTERM
// so a long-running suite can be aborted with Ctrl+C and still emit a
// report for what completed.
func installSignalHandler(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}