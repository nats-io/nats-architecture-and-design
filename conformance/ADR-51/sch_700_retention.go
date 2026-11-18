// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr51

import (
	"context"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// sch700Tests covers SCH-700: stream retention interaction. Each
// retention mode (Limits, WorkQueue, Interest) has its own caveats; the
// tests here exercise the table in ADR-51 §"Stream retention
// interaction".
func sch700Tests() []harness.Test {
	slow := requiresSlow()
	return []harness.Test{
		{ID: "SCH-701", Title: "Limits retention works as documented", Section: "SCH-700", Tags: []string{"retention", "limits", "slow"}, SkipReason: slow, Run: testSCH701},
		{ID: "SCH-702", Title: "WorkQueue retention with no consumer keeps schedules firing", Section: "SCH-700", Tags: []string{"retention", "workqueue", "slow"}, SkipReason: slow, Run: testSCH702},
		{ID: "SCH-703", Title: "WorkQueue retention: ack on schedule subject removes the schedule", Section: "SCH-700", Tags: []string{"retention", "workqueue", "slow"}, SkipReason: slow, Run: testSCH703},
		{ID: "SCH-704", Title: "Interest retention with pinning consumer keeps schedules firing", Section: "SCH-700", Tags: []string{"retention", "interest", "slow"}, SkipReason: slow, Run: testSCH704},
		{ID: "SCH-705", Title: "Interest retention without consumer does not store schedule", Section: "SCH-700", Tags: []string{"retention", "interest"}, Run: testSCH705},
		{ID: "SCH-706", Title: "MaxAge shorter than firing interval removes schedule before firing", Section: "SCH-700", Tags: []string{"retention", "limits", "slow"}, SkipReason: slow, Run: testSCH706},
		{ID: "SCH-707", Title: "Two-stream composition: WorkQueue source + Interest dest", Section: "SCH-700", Tags: []string{"retention", "interest", "sources", "slow"}, SkipReason: slow, Run: testSCH707},
	}
}

// makeStreamWith constructs a stream with the supplied retention/extras
// applied on top of the default schedule subjects.
func makeStreamWith(h *harness.Harness, suffix string, mut func(*streamConfig)) (string, error) {
	name := h.MintStreamName(strings.ReplaceAll(h.TestID, "-", "_") + suffix)
	cfg := streamConfig{
		Name:              name,
		Subjects:          []string{h.SubjectPrefix() + ".>"},
		AllowMsgSchedules: true,
		AllowMsgTTL:       true,
	}
	if mut != nil {
		mut(&cfg)
	}
	if err := createStream(h, cfg); err != nil {
		return "", err
	}
	return name, nil
}

func testSCH701(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := makeStreamWith(h, "", func(c *streamConfig) {
		c.Retention = "limits"
	})
	if err != nil {
		return fail("stream create: %v", err)
	}
	tgt := h.Subject("target.limits.a")
	if ack, err := publishSchedule(h, h.Subject("schedules.limits.a"), []byte("body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if _, err := waitForCountOn(h, name, tgt, 3, 10*time.Second); err != nil {
		return fail("3 firings: %v", err)
	}
	return pass()
}

func testSCH702(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := makeStreamWith(h, "", func(c *streamConfig) {
		c.Retention = "workqueue"
	})
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.wq.simple")
	tgt := h.Subject("target.wq.simple")

	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if _, err := waitForCountOn(h, name, tgt, 3, 10*time.Second); err != nil {
		return fail("3 firings on WorkQueue: %v", err)
	}
	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil || stored == nil {
		return fail("schedule was removed without consumer ack — should persist on WorkQueue without consumer interest: %v", err)
	}
	return pass()
}

func testSCH703(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := makeStreamWith(h, "", func(c *streamConfig) {
		c.Retention = "workqueue"
	})
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.wq.acked")
	tgt := h.Subject("target.wq.acked")

	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 5s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	str, err := h.JS.Stream(ctx, name)
	if err != nil {
		return fail("get stream: %v", err)
	}
	cons, err := str.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "ack_consumer",
		FilterSubject: schedSubj,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fail("create consumer: %v", err)
	}
	msg, err := cons.Next(jetstream.FetchMaxWait(5 * time.Second))
	if err != nil {
		return fail("consumer next: %v", err)
	}
	if err := msg.Ack(); err != nil {
		return fail("ack: %v", err)
	}
	gone := waitFor(8*time.Second, func() bool {
		m, err := lastMsgFor(h, name, schedSubj)
		return err == nil && m == nil
	})
	if !gone {
		return fail("schedule remained on %s after consumer ack — WorkQueue retention should remove it", schedSubj)
	}
	// At most one in-flight firing is allowed after the ack.
	preCount, _ := subjectCount(h, name, tgt)
	time.Sleep(8 * time.Second)
	postCount, _ := subjectCount(h, name, tgt)
	if postCount > preCount+1 {
		return fail("schedule kept firing after WorkQueue ack: %d -> %d", preCount, postCount)
	}
	return pass()
}

func testSCH704(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := makeStreamWith(h, "", func(c *streamConfig) {
		c.Retention = "interest"
	})
	if err != nil {
		return fail("stream create: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	str, err := h.JS.Stream(ctx, name)
	if err != nil {
		return fail("get stream: %v", err)
	}
	// Two pinning consumers, one per subject pattern. On Interest
	// retention every subject we want to retain needs at least one
	// consumer expressing interest:
	//   * schedules.> keeps the schedule message itself (per ADR-51
	//     §"Stream retention interaction" option 1).
	//   * target.>    keeps each generated firing long enough for the
	//     harness to observe it. Without this, firings are produced
	//     and immediately discarded because nothing has interest.
	if _, err := str.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "pinning_schedules",
		FilterSubject: h.Subject("schedules.>"),
		AckPolicy:     jetstream.AckNonePolicy,
	}); err != nil {
		return fail("create schedule pinning consumer: %v", err)
	}
	if _, err := str.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "pinning_targets",
		FilterSubject: h.Subject("target.>"),
		AckPolicy:     jetstream.AckNonePolicy,
	}); err != nil {
		return fail("create target pinning consumer: %v", err)
	}

	schedSubj := h.Subject("schedules.interest.pinned")
	tgt := h.Subject("target.interest.pinned")
	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if _, err := waitForCountOn(h, name, tgt, 3, 10*time.Second); err != nil {
		return fail("3 firings on Interest+pinning: %v", err)
	}
	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil || stored == nil {
		return fail("schedule removed despite pinning consumer: %v", err)
	}
	return pass()
}

func testSCH705(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := makeStreamWith(h, "", func(c *streamConfig) {
		c.Retention = "interest"
	})
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.interest.unheld")
	tgt := h.Subject("target.interest.unheld")
	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		// Interest-without-consumer may also reject the publish; record
		// either branch but assert nothing fires.
		_ = ack
	}
	time.Sleep(3 * time.Second)
	if c, err := subjectCount(h, name, schedSubj); err != nil {
		return fail("schedule count: %v", err)
	} else if c != 0 {
		return fail("schedule subject %s has %d stored messages on Interest with no consumer; expected 0", schedSubj, c)
	}
	if c, err := subjectCount(h, name, tgt); err != nil {
		return fail("target count: %v", err)
	} else if c != 0 {
		return fail("target %s received %d messages; expected 0 (Interest with no consumer should not fire schedules)", tgt, c)
	}
	return pass()
}

func testSCH706(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := makeStreamWith(h, "", func(c *streamConfig) {
		c.MaxAge = int64(2 * time.Second)
	})
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.maxage")
	tgt := h.Subject("target.maxage")
	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 10s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	time.Sleep(5 * time.Second)
	if m, err := lastMsgFor(h, name, schedSubj); err != nil {
		return fail("schedule lookup: %v", err)
	} else if m != nil {
		return fail("schedule still present after MaxAge of 2s — should have been evicted")
	}
	if m, err := lastMsgFor(h, name, tgt); err != nil {
		return fail("target lookup: %v", err)
	} else if m != nil {
		return fail("target message appeared on %s — schedule fired despite MaxAge eviction", tgt)
	}
	return pass()
}

func testSCH707(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	tag := strings.ReplaceAll(h.TestID, "-", "_")
	wq := h.MintStreamName(tag + "_WQ")
	intStream := h.MintStreamName(tag + "_INT")

	wqSubjects := []string{
		h.Subject("schedules.composed"),
		h.Subject("target.composed"),
	}
	if err := createStream(h, streamConfig{
		Name:              wq,
		Subjects:          wqSubjects,
		Retention:         "workqueue",
		AllowMsgSchedules: true,
		AllowMsgTTL:       true,
	}); err != nil {
		return fail("create WQ: %v", err)
	}
	if err := createStream(h, streamConfig{
		Name:      intStream,
		Subjects:  []string{h.Subject("composed.>")},
		Retention: "interest",
		Sources:   []source{{Name: wq, FilterSubject: h.Subject("target.composed")}},
	}); err != nil {
		return fail("create INT: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	intS, err := h.JS.Stream(ctx, intStream)
	if err != nil {
		return fail("get INT: %v", err)
	}
	// AckExplicit + never ack: under Interest retention this keeps
	// sourced messages stored in INT for the duration of the test. With
	// AckNone the messages would be drained as soon as they were
	// delivered (the consumer's interest is satisfied immediately) and
	// `waitForCountOn` would race against drainage, producing flakes.
	cons, err := intS.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "interest_consumer",
		FilterSubject: h.Subject("target.composed"),
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fail("create INT consumer: %v", err)
	}

	schedSubj := h.Subject("schedules.composed")
	tgt := h.Subject("target.composed")
	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}

	// Target messages on WQ are drained by INT's sourcing (WorkQueue
	// retention removes a message once it's been sourced), so the
	// observable count is on INT, not WQ. WQ's role is asserted by
	// checking the schedule itself remains stored.
	if _, err := waitForCountOn(h, intStream, tgt, 3, 20*time.Second); err != nil {
		return fail("INT did not source 3 firings via WQ: %v", err)
	}
	if m, err := lastMsgFor(h, wq, schedSubj); err != nil || m == nil {
		return fail("schedule not retained in WQ: err=%v msg=%v", err, m)
	}
	// Confirm the consumer can actually deliver one of the sourced
	// messages (the doc requires "the consumer on INT can deliver
	// them"). We pull a single message and ack it; that proves the
	// delivery path works without draining the rest.
	msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	if err != nil {
		return fail("fetch from INT consumer: %v", err)
	}
	for m := range msgs.Messages() {
		if m.Subject() != tgt {
			return fail("delivered message subject=%q, want %q", m.Subject(), tgt)
		}
		_ = m.Ack()
		return pass()
	}
	if msgs.Error() != nil {
		return fail("fetch error: %v", msgs.Error())
	}
	return fail("INT consumer delivered no messages within fetch window despite sourced count >= 3")
}
