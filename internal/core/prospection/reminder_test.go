package prospection

import (
	"testing"
	"time"
)

var (
	yearly      = RuleYearly
	oct11Anchor = Anchor{Month: time.October, Day: 11}
)

func ptrTime(t time.Time) *time.Time { return &t }

// TestReminderVerdict_RecurringOccurrenceInClockZone pins D1's third About
// rule: fire_at is stored in UTC, and NextOccurrence reads its zone from its
// argument, so the occurrence must be derived from fire_at expressed in the
// clock's zone. Here fire_at is 22:00 on 10 October in UTC-3, which is 01:00 on
// 11 October UTC: read in UTC the occurrence is noon UTC on the 11th, three
// hours before the local noon it really is.
func TestReminderVerdict_RecurringOccurrenceInClockZone(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	fireAt := time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC)
	r := Reminder{FireAt: fireAt, Rule: &yearly, Anchor: &oct11Anchor, Waiting: true}
	q := DefaultQuietHours()

	// 10:00 local on the day: the local noon occurrence is still ahead.
	if v, reason := q.ReminderVerdict(r, time.Date(2026, 10, 11, 10, 0, 0, 0, zone)); v != VerdictDeliver || reason != "" {
		t.Errorf("at 10:00 local = %q/%q, want deliver: the occurrence is local noon, not noon UTC", v, reason)
	}
	// 13:00 local: it has passed.
	if v, reason := q.ReminderVerdict(r, time.Date(2026, 10, 11, 13, 0, 0, 0, zone)); v != VerdictStale || reason != LapsePassed {
		t.Errorf("at 13:00 local = %q/%q, want stale/passed", v, reason)
	}
}

// TestReminderVerdict_ZeroLeadIsNotPassed: a reminder armed at once carries
// lead_minutes 0 and no event_at, so fire_at + 0 is its firing instant and not
// an event. Reading it as one would call every at-once reminder passed the
// moment it comes due.
func TestReminderVerdict_ZeroLeadIsNotPassed(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	r := Reminder{FireAt: now.Add(-time.Minute)}
	if v, reason := DefaultQuietHours().ReminderVerdict(r, now); v != VerdictDeliver || reason != "" {
		t.Errorf("verdict = %q/%q, want deliver: the event is unknown", v, reason)
	}
}

func TestReminderVerdict_JudgesThePassedEvent(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	q := DefaultQuietHours()
	hour := func(h int) time.Time { return now.Add(time.Duration(h) * time.Hour) }

	cases := []struct {
		name       string
		r          Reminder
		wantV      Verdict
		wantReason LapseReason
	}{
		{"a lead puts the event ahead", Reminder{FireAt: hour(-1), LeadMinutes: 120}, VerdictDeliver, ""},
		{"a lead puts the event behind", Reminder{FireAt: hour(-3), LeadMinutes: 120}, VerdictStale, LapsePassed},
		{"the event at this very instant has started", Reminder{FireAt: hour(-2), LeadMinutes: 120}, VerdictStale, LapsePassed},
		{"the payload's event wins over fire_at plus the lead", Reminder{FireAt: hour(-1), LeadMinutes: 600, EventAt: hour(-1)}, VerdictStale, LapsePassed},
		{"an unknown event falls back to the unit's current one: behind", Reminder{FireAt: hour(-1), Current: ptrTime(hour(-5))}, VerdictStale, LapsePassed},
		{"an unknown event falls back to the unit's current one: ahead", Reminder{FireAt: hour(-1), Current: ptrTime(hour(5))}, VerdictDeliver, ""},
		{"a recurring reminder never reads the unit's anchor year as an event",
			Reminder{FireAt: hour(-1), Rule: &yearly, Anchor: &Anchor{Month: time.October, Day: 21}, Current: ptrTime(time.Date(1990, 10, 21, 0, 0, 0, 0, time.UTC))}, VerdictDeliver, ""},
		{"overdue past the window is stale, and says so", Reminder{FireAt: hour(-12), Current: ptrTime(hour(5))}, VerdictStale, LapseOverdue},
		{"a waiting reminder ignores lateness", Reminder{FireAt: hour(-30), Current: ptrTime(hour(5)), Waiting: true}, VerdictDeliver, ""},
		{"a waiting reminder is still judged on its event", Reminder{FireAt: hour(-30), Current: ptrTime(hour(-5)), Waiting: true}, VerdictStale, LapsePassed},
		{"not yet due is pending", Reminder{FireAt: hour(1), Current: ptrTime(hour(5))}, VerdictPending, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if v, reason := q.ReminderVerdict(tc.r, now); v != tc.wantV || reason != tc.wantReason {
				t.Errorf("verdict = %q/%q, want %q/%q", v, reason, tc.wantV, tc.wantReason)
			}
		})
	}
}
