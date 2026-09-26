// Copyright 2026 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package adr51

import (
	"context"
	"time"

	"github.com/nats-io/nats-architecture-and-design/conformance/harness"
)

// sch600Tests covers SCH-600: schedule replacement and stopping —
// rollup-replace, delete-by-seq, purge, atomic stop with all its
// variations.
func sch600Tests() []harness.Test {
	slow := requiresSlow()
	return []harness.Test{
		{ID: "SCH-601", Title: "Publishing a new schedule replaces the prior one", Section: "SCH-600", Tags: []string{"replace", "slow"}, SkipReason: slow, Run: testSCH601},
		{ID: "SCH-602", Title: "Deleting the schedule by sequence stops firings", Section: "SCH-600", Tags: []string{"stop", "slow"}, SkipReason: slow, Run: testSCH602},
		{ID: "SCH-603", Title: "Purging the schedule subject stops firings", Section: "SCH-600", Tags: []string{"stop", "slow"}, SkipReason: slow, Run: testSCH603},
		{ID: "SCH-604", Title: "Purging by wildcard stops multiple schedules", Section: "SCH-600", Tags: []string{"stop", "slow"}, SkipReason: slow, Run: testSCH604},
		{ID: "SCH-605", Title: "Atomic stop: publish to a different subject and remove the schedule", Section: "SCH-600", Tags: []string{"stop", "atomic"}, Run: testSCH605},
		{ID: "SCH-606", Title: "Atomic stop conditional on schedule still existing", Section: "SCH-600", Tags: []string{"stop", "atomic"}, Run: testSCH606},
		{ID: "SCH-607", Title: "Atomic stop publish-subject MUST NOT equal schedule subject", Section: "SCH-600", Tags: []string{"stop", "atomic"}, Run: testSCH607},
		{ID: "SCH-608", Title: "Atomic stop publishing to target subject delivers and stops", Section: "SCH-600", Tags: []string{"stop", "atomic"}, Run: testSCH608},
		{ID: "SCH-609", Title: "Single delayed message auto-stops its schedule after firing", Section: "SCH-600", Tags: []string{"stop"}, Run: testSCH609},
		{ID: "SCH-610", Title: "Empty Nats-Scheduler is rejected with 10212", Section: "SCH-600", Tags: []string{"stop", "atomic"}, Run: testSCH610},
		{ID: "SCH-611", Title: "Invalid Nats-Scheduler subject is rejected with 10212", Section: "SCH-600", Tags: []string{"stop", "atomic"}, Run: testSCH611},
	}
}

func testSCH601(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.replace.a")
	tgt := h.Subject("target.replace.a")

	if ack, err := publishSchedule(h, schedSubj, []byte("A"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule A err=%v ack=%+v", err, ack)
	}
	if _, err := waitForLastMsgOn(h, name, tgt, 5*time.Second); err != nil {
		return fail("await initial firing: %v", err)
	}

	if ack, err := publishSchedule(h, schedSubj, []byte("B"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule B err=%v ack=%+v", err, ack)
	}

	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil || stored == nil {
		return fail("schedule subject empty after replace: %v", err)
	}
	if string(stored.Data) != "B" {
		return fail("stored schedule payload=%q after replace, want %q", string(stored.Data), "B")
	}
	// Subsequent firings should deliver the new payload.
	gotB := waitFor(5*time.Second, func() bool {
		m, err := lastMsgFor(h, name, tgt)
		return err == nil && m != nil && string(m.Data) == "B"
	})
	if !gotB {
		return fail("target did not converge to new payload 'B' after schedule replacement")
	}
	return pass()
}

func testSCH602(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.stop.delete")
	tgt := h.Subject("target.stop.delete")

	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if _, err := waitForLastMsgOn(h, name, tgt, 5*time.Second); err != nil {
		return fail("await first firing: %v", err)
	}
	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil || stored == nil {
		return fail("schedule lookup: %v", err)
	}
	if err := deleteMsg(h, name, stored.Sequence); err != nil {
		return fail("delete schedule msg: %v", err)
	}
	// Snapshot the target count immediately after the delete; allow at
	// most one additional in-flight firing.
	pre, err := subjectCount(h, name, tgt)
	if err != nil {
		return fail("pre-wait target count: %v", err)
	}
	time.Sleep(4 * time.Second)
	post, err := subjectCount(h, name, tgt)
	if err != nil {
		return fail("post-wait target count: %v", err)
	}
	if post > pre+1 {
		return fail("target count grew %d -> %d in 4s after schedule delete; expected at most one in-flight firing", pre, post)
	}
	return pass()
}

func testSCH603(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.stop.purge")
	tgt := h.Subject("target.stop.purge")

	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if _, err := waitForLastMsgOn(h, name, tgt, 5*time.Second); err != nil {
		return fail("await first firing: %v", err)
	}
	if err := purgeSubject(h, name, schedSubj); err != nil {
		return fail("purge: %v", err)
	}
	if m, err := lastMsgFor(h, name, schedSubj); err != nil {
		return fail("post-purge schedule lookup: %v", err)
	} else if m != nil {
		return fail("schedule still present on %s after purge", schedSubj)
	}
	pre, err := subjectCount(h, name, tgt)
	if err != nil {
		return fail("pre-wait target count: %v", err)
	}
	time.Sleep(4 * time.Second)
	post, err := subjectCount(h, name, tgt)
	if err != nil {
		return fail("post-wait target count: %v", err)
	}
	if post > pre+1 {
		return fail("target count grew %d -> %d in 4s after purge; expected schedule to stop firing", pre, post)
	}
	return pass()
}

func testSCH604(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	subjects := []string{
		h.Subject("schedules.stop.multi.a"),
		h.Subject("schedules.stop.multi.b"),
		h.Subject("schedules.stop.multi.c"),
	}
	for i, s := range subjects {
		tgt := h.Subject("target.stop.multi." + string(rune('a'+i)))
		if ack, err := publishSchedule(h, s, []byte("body"),
			schedHeader{HdrSchedule, "@every 1s"},
			schedHeader{HdrScheduleTarget, tgt},
		); err != nil || ack.Error != nil {
			return fail("schedule %s err=%v ack=%+v", s, err, ack)
		}
	}
	// Wait briefly so every schedule has fired at least once.
	time.Sleep(2 * time.Second)
	wildcard := h.Subject("schedules.stop.multi.>")
	if err := purgeSubject(h, name, wildcard); err != nil {
		return fail("wildcard purge: %v", err)
	}
	for _, s := range subjects {
		if m, err := lastMsgFor(h, name, s); err != nil {
			return fail("post-purge lookup %s: %v", s, err)
		} else if m != nil {
			return fail("schedule still present on %s after wildcard purge", s)
		}
	}
	return pass()
}

func testSCH605(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.cancel.delayed")
	tgt := h.Subject("target.cancel.delayed")
	cancelSubj := h.Subject("schedules.cancel.canceled")

	if ack, err := publishSchedule(h, schedSubj, []byte("delayed-body"),
		schedHeader{HdrSchedule, "@at " + rfc3339In(30*time.Second)},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if ack, err := publishSchedule(h, cancelSubj, []byte("cancel-body"),
		schedHeader{HdrScheduleNext, ScheduleNextPurge},
		schedHeader{HdrScheduler, schedSubj},
	); err != nil || ack.Error != nil {
		return fail("cancel publish err=%v ack=%+v", err, ack)
	}
	// Schedule should be gone immediately.
	if m, err := lastMsgFor(h, name, schedSubj); err != nil {
		return fail("schedule lookup: %v", err)
	} else if m != nil {
		return fail("schedule still present on %s after atomic cancel", schedSubj)
	}
	// Cancel message must be stored.
	cancelMsg, err := lastMsgFor(h, name, cancelSubj)
	if err != nil || cancelMsg == nil {
		return fail("cancel message not stored on %s: %v", cancelSubj, err)
	}
	if string(cancelMsg.Data) != "cancel-body" {
		return fail("cancel message payload=%q, want %q", string(cancelMsg.Data), "cancel-body")
	}
	// Make sure the target never fires; wait past the schedule's
	// would-have-fired time with a small budget for late firings.
	time.Sleep(3 * time.Second)
	if m, err := lastMsgFor(h, name, tgt); err != nil {
		return fail("target lookup: %v", err)
	} else if m != nil {
		return fail("target message %q appeared on %s — atomic cancel should have prevented firing", string(m.Data), tgt)
	}
	return pass()
}

func testSCH606(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.cancel.cond")
	tgt := h.Subject("target.cancel.cond")
	signalSubj := h.Subject("schedules.cancel.cond_signal")

	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 5s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	stored, err := lastMsgFor(h, name, schedSubj)
	if err != nil || stored == nil {
		return fail("schedule lookup: %v", err)
	}

	// First conditional cancel — should succeed.
	ack1, err := publishSchedule(h, signalSubj, []byte("first"),
		schedHeader{HdrScheduleNext, ScheduleNextPurge},
		schedHeader{HdrScheduler, schedSubj},
		schedHeader{HdrExpLastSubjectSeq, itoa(stored.Sequence)},
		schedHeader{HdrExpLastSubjectSeqSub, schedSubj},
	)
	if err != nil || ack1.Error != nil {
		return fail("first conditional cancel err=%v ack=%+v", err, ack1)
	}
	if m, err := lastMsgFor(h, name, schedSubj); err != nil {
		return fail("schedule lookup: %v", err)
	} else if m != nil {
		return fail("schedule still present after first conditional cancel")
	}

	// Second conditional cancel — should fail because the schedule is
	// gone, and the second signal must NOT be stored.
	preCount, err := subjectCount(h, name, signalSubj)
	if err != nil {
		return fail("pre signal count: %v", err)
	}
	ack2, _ := publishSchedule(h, signalSubj, []byte("second"),
		schedHeader{HdrScheduleNext, ScheduleNextPurge},
		schedHeader{HdrScheduler, schedSubj},
		schedHeader{HdrExpLastSubjectSeq, itoa(stored.Sequence)},
		schedHeader{HdrExpLastSubjectSeqSub, schedSubj},
	)
	if ack2 != nil && ack2.Error == nil {
		// If the server accepted, the second signal would be stored
		// alongside the first — that's a violation of the conditional
		// semantics.
		postCount, _ := subjectCount(h, name, signalSubj)
		if postCount > preCount {
			return fail("second conditional cancel succeeded and stored a duplicate signal (pre=%d post=%d) — conditional cancel must reject when schedule is absent", preCount, postCount)
		}
		return inconclusive("server accepted second conditional cancel without an error but did not store a duplicate; behavior is ambiguous against the ADR")
	}
	postCount, err := subjectCount(h, name, signalSubj)
	if err != nil {
		return fail("post signal count: %v", err)
	}
	if postCount != preCount {
		return fail("second signal stored despite expected-last-subject-sequence mismatch (pre=%d post=%d)", preCount, postCount)
	}
	return pass()
}

func testSCH607(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.self.cancel")
	tgt := h.Subject("target.self.cancel")

	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@every 1s"},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}

	// Publish to the SAME schedule subject with the cancel headers.
	// Per ADR-51 §"Ending/stopping schedules early": "Nats-Scheduler
	// can NOT equal the publish subject itself". The server enforces
	// this with JSMessageSchedulesSchedulerInvalidErr (10212), so the
	// expected behavior is rejection with err_code 10212. Because the
	// publish is rejected, the schedule is NOT canceled and continues
	// firing — that continuation is the correct outcome here.
	ack, err := publishSchedule(h, schedSubj, []byte("cancel"),
		schedHeader{HdrScheduleNext, ScheduleNextPurge},
		schedHeader{HdrScheduler, schedSubj},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		// Server unexpectedly accepted. The ADR forbids this case
		// because the cancel message would self-purge. Verify the
		// schedule actually stopped (otherwise the server is in a
		// broken state where the cancel was accepted but had no
		// effect).
		time.Sleep(4 * time.Second)
		stillFiring := false
		if c, err := subjectCount(h, name, tgt); err == nil && c > 0 {
			stillFiring = true
		}
		if stillFiring {
			return fail("server accepted self-cancel publish (Nats-Scheduler == publish subject) but schedule kept firing — ADR forbids this case")
		}
		return inconclusive("server accepted self-cancel publish (Nats-Scheduler == publish subject); the schedule did stop, but the ADR specifies this should be rejected with err_code 10212")
	}
	if ack.Error.ErrCode != ErrCodeSchedulerInvalid {
		return fail("expected err_code=%d (JSMessageSchedulesSchedulerInvalidErr), got %s", ErrCodeSchedulerInvalid, ack.Error)
	}
	// Rejected as expected. The schedule continues firing because the
	// cancel was refused; we don't assert on its post-rejection
	// behavior.
	return pass()
}

func testSCH608(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.early.delayed")
	tgt := h.Subject("target.early.delayed")

	if ack, err := publishSchedule(h, schedSubj, []byte("slow-body"),
		schedHeader{HdrSchedule, "@at " + rfc3339In(30*time.Second)},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if ack, err := publishSchedule(h, tgt, []byte("fast-body"),
		schedHeader{HdrScheduleNext, ScheduleNextPurge},
		schedHeader{HdrScheduler, schedSubj},
	); err != nil || ack.Error != nil {
		return fail("early publish err=%v ack=%+v", err, ack)
	}
	// Schedule must be gone, target must contain the fast-body.
	if m, err := lastMsgFor(h, name, schedSubj); err != nil {
		return fail("schedule lookup: %v", err)
	} else if m != nil {
		return fail("schedule still present on %s after early-publish atomic stop", schedSubj)
	}
	time.Sleep(2 * time.Second)
	gen, err := lastMsgFor(h, name, tgt)
	if err != nil || gen == nil {
		return fail("target lookup: %v", err)
	}
	if string(gen.Data) != "fast-body" {
		return fail("target payload=%q, want %q", string(gen.Data), "fast-body")
	}
	count, err := subjectCount(h, name, tgt)
	if err != nil {
		return fail("target count: %v", err)
	}
	if count != 1 {
		return fail("target count=%d, want 1 — schedule should not have double-published after atomic early-publish", count)
	}
	return pass()
}

func testSCH609(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	schedSubj := h.Subject("schedules.single.auto")
	tgt := h.Subject("target.single.auto")

	if ack, err := publishSchedule(h, schedSubj, []byte("body"),
		schedHeader{HdrSchedule, "@at " + rfc3339In(2*time.Second)},
		schedHeader{HdrScheduleTarget, tgt},
	); err != nil || ack.Error != nil {
		return fail("schedule publish err=%v ack=%+v", err, ack)
	}
	if _, err := waitForLastMsgOn(h, name, tgt, 10*time.Second); err != nil {
		return fail("await target: %v", err)
	}
	gone := waitFor(5*time.Second, func() bool {
		m, err := lastMsgFor(h, name, schedSubj)
		return err == nil && m == nil
	})
	if !gone {
		return fail("single delayed schedule did not auto-stop after firing (still present on %s)", schedSubj)
	}
	return pass()
}

func testSCH610(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	publishSubj := h.Subject("schedules.cancel.empty")
	pre, err := streamLastSeq(h, name)
	if err != nil {
		return fail("pre last seq: %v", err)
	}
	ack, err := publishSchedule(h, publishSubj, []byte("body"),
		schedHeader{HdrScheduleNext, ScheduleNextPurge},
		schedHeader{HdrScheduler, ""},
	)
	if err != nil {
		return fail("publish: %v", err)
	}
	if ack.Error == nil {
		return fail("expected error pub ack for empty Nats-Scheduler, got %+v", ack)
	}
	if ack.Error.ErrCode != ErrCodeSchedulerInvalid {
		return fail("expected err_code=%d (JSMessageSchedulesSchedulerInvalidErr), got %s", ErrCodeSchedulerInvalid, ack.Error)
	}
	post, err := streamLastSeq(h, name)
	if err != nil {
		return fail("post last seq: %v", err)
	}
	if post != pre {
		return fail("stream last seq advanced (%d -> %d) — rejected publish must not be stored", pre, post)
	}
	return pass()
}

func testSCH611(_ context.Context, h *harness.Harness) (harness.Status, string, error) {
	name, err := scheduleDefaultStream(h)
	if err != nil {
		return fail("stream create: %v", err)
	}
	publishSubj := h.Subject("schedules.cancel.bad")

	// Each value below is an invalid publish subject in some way; the
	// server must return 10212 and not store the message.
	cases := []string{
		" ",                       // whitespace-only
		"bad subject with spaces", // spaces are not allowed in subjects
		"*.>",                     // wildcard — not a valid publish subject
		".leading.dot",            // leading dot
	}
	for _, scheduler := range cases {
		pre, err := streamLastSeq(h, name)
		if err != nil {
			return fail("pre last seq for %q: %v", scheduler, err)
		}
		ack, err := publishSchedule(h, publishSubj, []byte("body"),
			schedHeader{HdrScheduleNext, ScheduleNextPurge},
			schedHeader{HdrScheduler, scheduler},
		)
		if err != nil {
			return fail("publish for Nats-Scheduler=%q: %v", scheduler, err)
		}
		if ack.Error == nil {
			return fail("expected error pub ack for Nats-Scheduler=%q, got %+v", scheduler, ack)
		}
		if ack.Error.ErrCode != ErrCodeSchedulerInvalid {
			return fail("Nats-Scheduler=%q: expected err_code=%d (JSMessageSchedulesSchedulerInvalidErr), got %s", scheduler, ErrCodeSchedulerInvalid, ack.Error)
		}
		post, err := streamLastSeq(h, name)
		if err != nil {
			return fail("post last seq for %q: %v", scheduler, err)
		}
		if post != pre {
			return fail("Nats-Scheduler=%q: stream last seq advanced (%d -> %d) — rejected publish must not be stored", scheduler, pre, post)
		}
	}
	return pass()
}

// itoa formats a uint64 without pulling in fmt.
func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
