// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr50

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// fb1300Tests covers FB-1300: mirrors and sources don't propagate
// fast-batch state — sourced/mirrored messages are ordinary entries.
func fb1300Tests() []harness.Test {
	return []harness.Test{
		{ID: "FB-1302", Title: "Sources do not propagate fast-batch state", Section: "FB-1300", Tags: []string{"sources"}, Run: testFB1302},
	}
}

func testFB1302(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	// Mint two tracked stream names, one per stream we'll actually
	// create. Don't reuse a single name with suffixes — the harness
	// would otherwise track a phantom name that was never created.
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	src := h.MintStreamName(tag + "_SRC")
	dst := h.MintStreamName(tag + "_DST")

	// Disjoint subject namespaces so the two streams' filters don't
	// overlap (NATS rejects overlapping subjects within an account).
	// Sourced messages on DST keep SRC's subjects; that's allowed
	// because sourcing bypasses DST's subject filter.
	srcSubj := h.Subject("src") + ".>"
	dstSubj := h.Subject("dst") + ".>"

	if err := createStream(h, streamConfig{
		Name:               src,
		Subjects:           []string{srcSubj},
		AllowBatchPublish:  true,
	}); err != nil {
		return fail("create source: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:     dst,
		Subjects: []string{dstSubj},
		Sources:  []source{{Name: src}},
	}); err != nil {
		return fail("create dst: %v", err)
	}

	handle, err := openFastBatch(h, src, 10, "ok")
	if err != nil {
		return fail("open batch: %v", err)
	}
	defer handle.Close()

	pubSubj := h.Subject("src.a")
	if err := handle.publish(pubSubj, FBOpStart, nil, []byte("a")); err != nil {
		return fail("initial: %v", err)
	}
	for i := 2; i <= 4; i++ {
		if err := handle.publish(pubSubj, FBOpAppend, nil, []byte(fmt.Sprintf("%d", i))); err != nil {
			return fail("append seq %d: %v", i, err)
		}
	}
	if err := handle.publish(pubSubj, FBOpCommitStore, nil, []byte("e")); err != nil {
		return fail("commit: %v", err)
	}
	ack, err := handle.awaitPubAck(10 * time.Second)
	if err != nil {
		return fail("await pubAck: %v", err)
	}
	if ack.Error != nil || ack.BatchSize != 5 {
		return fail("commit ack mismatch: %+v", ack)
	}

	caught := waitFor(10*time.Second, func() bool {
		last, err := streamLastSeq(h, dst)
		return err == nil && last == 5
	})
	if !caught {
		last, _ := streamLastSeq(h, dst)
		return fail("DST did not catch up to 5 messages (last=%d)", last)
	}
	return pass()
}