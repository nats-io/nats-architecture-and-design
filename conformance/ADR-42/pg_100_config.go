// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr42

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// pg100Tests covers PG-100: consumer-configuration toggles for
// PriorityGroups / PriorityPolicy / PriorityTimeout, plus the
// constraints around updates and AckPolicy.
func pg100Tests() []harness.Test {
	return []harness.Test{
		{ID: "PG-101", Title: "priority_policy:overflow accepted with single group", Section: "PG-100", Tags: []string{"config"}, Run: testPG101},
		{ID: "PG-102", Title: "priority_policy:pinned_client accepted with timeout", Section: "PG-100", Tags: []string{"config"}, Run: testPG102},
		{ID: "PG-103", Title: "priority_policy:prioritized accepted with single group", Section: "PG-100", Tags: []string{"config"}, Run: testPG103},
		{ID: "PG-104", Title: "Unknown priority_policy is rejected", Section: "PG-100", Tags: []string{"config"}, Run: testPG104},
		{ID: "PG-105", Title: "priority_groups without priority_policy is rejected", Section: "PG-100", Tags: []string{"config"}, Run: testPG105},
		{ID: "PG-106", Title: "priority_policy without priority_groups is rejected", Section: "PG-100", Tags: []string{"config"}, Run: testPG106},
		{ID: "PG-107", Title: "Multiple groups in priority_groups is rejected", Section: "PG-100", Tags: []string{"config"}, Run: testPG107},
		{ID: "PG-108", Title: "Group name length and charset validation", Section: "PG-100", Tags: []string{"config"}, Run: testPG108},
		{ID: "PG-109", Title: "Push consumer with priority_policy is rejected", Section: "PG-100", Tags: []string{"config"}, Run: testPG109},
		{ID: "PG-110", Title: "overflow requires AckPolicy:explicit (pedantic)", Section: "PG-100", Tags: []string{"config"}, Run: testPG110},
		{ID: "PG-111", Title: "pinned_client requires AckPolicy:explicit (pedantic)", Section: "PG-100", Tags: []string{"config"}, Run: testPG111},
		{ID: "PG-112", Title: "Cannot add priority groups via update", Section: "PG-100", Tags: []string{"config"}, Run: testPG112},
		{ID: "PG-113", Title: "Cannot remove priority groups via update", Section: "PG-100", Tags: []string{"config"}, Run: testPG113},
		{ID: "PG-114", Title: "Cannot switch between policies via update", Section: "PG-100", Tags: []string{"config"}, Run: testPG114},
		{ID: "PG-115", Title: "priority_timeout is updatable on pinned_client", Section: "PG-100", Tags: []string{"config"}, Run: testPG115},
	}
}

func testPG101(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	info, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: PolicyOverflow,
	})
	if err != nil {
		return fail("consumer create: %v", err)
	}
	if info.Config.PriorityPolicy != PolicyOverflow {
		return fail("priority_policy mismatch: got %q want %q", info.Config.PriorityPolicy, PolicyOverflow)
	}
	if len(info.Config.PriorityGroups) != 1 || info.Config.PriorityGroups[0] != "jobs" {
		return fail("priority_groups mismatch: got %v", info.Config.PriorityGroups)
	}
	return pass()
}

func testPG102(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	info, err := createConsumer(h, stream, consumerConfig{
		Name:            cname,
		AckPolicy:       "explicit",
		PriorityGroups:  []string{"jobs"},
		PriorityPolicy:  PolicyPinnedClient,
		PriorityTimeout: int64(2 * time.Minute),
	})
	if err != nil {
		return fail("consumer create: %v", err)
	}
	if info.Config.PriorityPolicy != PolicyPinnedClient {
		return fail("priority_policy mismatch: got %q want %q", info.Config.PriorityPolicy, PolicyPinnedClient)
	}
	if info.Config.PriorityTimeout != int64(2*time.Minute) {
		return fail("priority_timeout mismatch: got %d ns want %d ns", info.Config.PriorityTimeout, int64(2*time.Minute))
	}
	return pass()
}

func testPG103(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	info, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: PolicyPrioritized,
	})
	if err != nil {
		return fail("consumer create: %v", err)
	}
	if info.Config.PriorityPolicy != PolicyPrioritized {
		return fail("priority_policy mismatch: got %q want %q", info.Config.PriorityPolicy, PolicyPrioritized)
	}
	return pass()
}

func testPG104(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	_, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: "bogus",
	})
	if err == nil {
		return fail("expected error creating consumer with unknown priority_policy 'bogus', got success")
	}
	return pass()
}

func testPG105(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	_, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityGroups: []string{"jobs"},
		// No PriorityPolicy.
	})
	if err == nil {
		return fail("expected error creating consumer with priority_groups but no priority_policy, got success")
	}
	return pass()
}

func testPG106(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	_, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityPolicy: PolicyOverflow,
		// No PriorityGroups.
	})
	if err == nil {
		return fail("expected error creating consumer with priority_policy but no priority_groups, got success")
	}
	return pass()
}

func testPG107(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	_, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityGroups: []string{"a", "b"},
		PriorityPolicy: PolicyOverflow,
	})
	if err == nil {
		return fail("expected error creating consumer with multiple priority_groups (initial impl limit), got success")
	}
	return pass()
}

func testPG108(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	type tc struct {
		name      string
		shouldOK  bool
	}
	cases := []tc{
		{"jobs", true},
		{"abcdefghij012345", true},   // 16 chars
		{"abcdefghij0123456", false}, // 17 chars
		{"bad name", false},
		{"bad.name", false},
		{"", false},
	}
	for i, c := range cases {
		cname := consumerName(h, "C") + "_" + itoa(i)
		_, err := createConsumer(h, stream, consumerConfig{
			Name:           cname,
			AckPolicy:      "explicit",
			PriorityGroups: []string{c.name},
			PriorityPolicy: PolicyOverflow,
		})
		if c.shouldOK && err != nil {
			return fail("group name %q expected to succeed but got error: %v", c.name, err)
		}
		if !c.shouldOK && err == nil {
			return fail("group name %q expected to be rejected but server accepted it", c.name)
		}
	}
	return pass()
}

func testPG109(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	_, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		DeliverSubject: h.Subject("push"),
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: PolicyOverflow,
	})
	if err == nil {
		return fail("expected error creating push consumer with priority_policy, got success")
	}
	return pass()
}

func testPG110(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	// Pedantic mode MUST reject AckPolicy != explicit.
	cname := consumerName(h, "Cped")
	_, err := createConsumerPedantic(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "none",
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: PolicyOverflow,
	})
	if err == nil {
		return fail("pedantic create with AckPolicy:none expected to be rejected, got success")
	}

	// Non-pedantic: server may either reject OR coerce to explicit.
	// Both branches are acceptable per ADR — record which one happens.
	cname2 := consumerName(h, "Cnp")
	info, err := createConsumer(h, stream, consumerConfig{
		Name:           cname2,
		AckPolicy:      "none",
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: PolicyOverflow,
	})
	if err != nil {
		// Server rejected even in non-pedantic mode — also acceptable.
		return pass()
	}
	if info.Config.AckPolicy != "explicit" {
		return fail("non-pedantic create with AckPolicy:none expected to be rejected or coerced to explicit; got AckPolicy=%q", info.Config.AckPolicy)
	}
	return pass()
}

func testPG111(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "Cped")
	_, err := createConsumerPedantic(h, stream, consumerConfig{
		Name:            cname,
		AckPolicy:       "none",
		PriorityGroups:  []string{"jobs"},
		PriorityPolicy:  PolicyPinnedClient,
		PriorityTimeout: int64(30 * time.Second),
	})
	if err == nil {
		return fail("pedantic create with AckPolicy:none on pinned_client expected to be rejected, got success")
	}

	cname2 := consumerName(h, "Cnp")
	info, err := createConsumer(h, stream, consumerConfig{
		Name:            cname2,
		AckPolicy:       "none",
		PriorityGroups:  []string{"jobs"},
		PriorityPolicy:  PolicyPinnedClient,
		PriorityTimeout: int64(30 * time.Second),
	})
	if err != nil {
		return pass()
	}
	if info.Config.AckPolicy != "explicit" {
		return fail("non-pedantic create with AckPolicy:none on pinned_client expected reject or coerce to explicit; got AckPolicy=%q", info.Config.AckPolicy)
	}
	return pass()
}

func testPG112(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	if _, err := createConsumer(h, stream, consumerConfig{
		Name:      cname,
		AckPolicy: "explicit",
	}); err != nil {
		return fail("baseline consumer create: %v", err)
	}
	_, err := updateConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: PolicyOverflow,
	})
	if err == nil {
		// Update returned success — re-check that the change really did
		// not take effect. If the server silently ignored the update,
		// that's still a failure of the ADR rule.
		info, _ := consumerInfo(h, stream, cname)
		if info != nil && info.Config.PriorityPolicy != "" {
			return fail("update unexpectedly added priority_policy=%q (groups=%v)", info.Config.PriorityPolicy, info.Config.PriorityGroups)
		}
		return fail("update returned success but priority_policy stayed empty — server silently ignored the change instead of rejecting it")
	}
	return pass()
}

func testPG113(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	if _, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: PolicyOverflow,
	}); err != nil {
		return fail("baseline consumer create: %v", err)
	}
	_, err := updateConsumer(h, stream, consumerConfig{
		Name:      cname,
		AckPolicy: "explicit",
		// PriorityGroups + PriorityPolicy intentionally cleared.
	})
	if err == nil {
		info, _ := consumerInfo(h, stream, cname)
		if info != nil && info.Config.PriorityPolicy == "" {
			return fail("update unexpectedly cleared priority_policy/priority_groups")
		}
		return fail("update returned success but priority_policy unchanged — server silently ignored the removal instead of rejecting it")
	}
	return pass()
}

func testPG114(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	if _, err := createConsumer(h, stream, consumerConfig{
		Name:           cname,
		AckPolicy:      "explicit",
		PriorityGroups: []string{"jobs"},
		PriorityPolicy: PolicyOverflow,
	}); err != nil {
		return fail("baseline consumer create: %v", err)
	}
	_, err := updateConsumer(h, stream, consumerConfig{
		Name:            cname,
		AckPolicy:       "explicit",
		PriorityGroups:  []string{"jobs"},
		PriorityPolicy:  PolicyPinnedClient,
		PriorityTimeout: int64(30 * time.Second),
	})
	if err == nil {
		info, _ := consumerInfo(h, stream, cname)
		if info != nil && info.Config.PriorityPolicy != PolicyOverflow {
			return fail("update unexpectedly switched priority_policy to %q", info.Config.PriorityPolicy)
		}
		return fail("update returned success but priority_policy unchanged — server silently ignored the policy switch instead of rejecting it")
	}
	return pass()
}

func testPG115(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	stream := streamName(h)
	if err := createStream(h, streamConfig{Name: stream}); err != nil {
		return fail("stream create: %v", err)
	}
	cname := consumerName(h, "C")
	if _, err := createConsumer(h, stream, consumerConfig{
		Name:            cname,
		AckPolicy:       "explicit",
		PriorityGroups:  []string{"jobs"},
		PriorityPolicy:  PolicyPinnedClient,
		PriorityTimeout: int64(1 * time.Minute),
	}); err != nil {
		return fail("baseline consumer create: %v", err)
	}
	if _, err := updateConsumer(h, stream, consumerConfig{
		Name:            cname,
		AckPolicy:       "explicit",
		PriorityGroups:  []string{"jobs"},
		PriorityPolicy:  PolicyPinnedClient,
		PriorityTimeout: int64(5 * time.Minute),
	}); err != nil {
		return fail("priority_timeout update failed: %v", err)
	}
	info, err := consumerInfo(h, stream, cname)
	if err != nil {
		return fail("consumer info: %v", err)
	}
	if info.Config.PriorityTimeout != int64(5*time.Minute) {
		return fail("priority_timeout not updated: got %d ns want %d ns", info.Config.PriorityTimeout, int64(5*time.Minute))
	}
	return pass()
}

// itoa formats a small integer without pulling fmt into the hot path.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
