package prospection

import (
	"testing"
	"time"
)

var yearly = RuleYearly

func ptrTime(t time.Time) *time.Time { return &t }

// TestReminderVerdict_RecurringOccurrenceInClockZone pins D1's third About
// rule: fire_at is stored in UTC, and NextOccurrence reads its zone from its
// argument, so the occurrence must be derived from fire_at expressed in the
// clock's zone. A birthday captured on the morning of the day arms at once, so
// its fire_at is 10:00 on 10 October in UTC-3, which is 13:00 UTC: read in
// UTC, the day's noon occurrence has already gone by and the next one is a
// year away, so the reminder would never be called passed.
func TestReminderVerdict_RecurringOccurrenceInClockZone(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	fireAt := time.Date(2026, 10, 10, 13, 0, 0, 0, time.UTC)
	r := Reminder{FireAt: fireAt, Rule: &yearly, Anchor: &Anchor{Month: time.October, Day: 10}, Waiting: true}
	q := DefaultQuietHours()

	if v, reason := q.ReminderVerdict(r, time.Date(2026, 10, 10, 23, 0, 0, 0, zone)); v != VerdictDeliver || reason != "" {
		t.Errorf("at 23:00 on the day = %q/%q, want deliver: the local day has not ended", v, reason)
	}
	if v, reason := q.ReminderVerdict(r, time.Date(2026, 10, 11, 1, 0, 0, 0, zone)); v != VerdictStale || reason != LapsePassed {
		t.Errorf("at 01:00 the next local day = %q/%q, want stale/passed: the occurrence was today's", v, reason)
	}
}

// TestReminderVerdict_DayGranularEventsPassWhenTheirDayEnds: a recurring
// occurrence (anchored at noon) and a date-only event (stored as local
// midnight, the way reminderInstants reads one) name a day, not an instant, so
// they are reminded about all of that day and passed only from the next local
// midnight. A timed event keeps the instant rule.
func TestReminderVerdict_DayGranularEventsPassWhenTheirDayEnds(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	q := DefaultQuietHours()
	dateOnly := at(15, 0, 0)
	cases := []struct {
		name string
		r    Reminder
		now  time.Time
		want Verdict
	}{
		{"a date-only event, at ten on its day", Reminder{FireAt: at(14, 9, 0), EventAt: dateOnly, Waiting: true}, at(15, 10, 0), VerdictDeliver},
		{"a date-only event, one minute before its day ends", Reminder{FireAt: at(14, 9, 0), EventAt: dateOnly, Waiting: true}, at(15, 23, 59), VerdictDeliver},
		{"a date-only event, the next local midnight", Reminder{FireAt: at(14, 9, 0), EventAt: dateOnly, Waiting: true}, at(16, 0, 0), VerdictStale},
		{"a date-only event known by the lead alone", Reminder{FireAt: at(14, 9, 0), LeadMinutes: 15 * 60, Waiting: true}, at(15, 10, 0), VerdictDeliver},
		{"a date-only event known by its unit alone", Reminder{FireAt: at(14, 9, 0), Current: ptrTime(dateOnly), Waiting: true}, at(15, 10, 0), VerdictDeliver},
		{"a timed event, the instant", Reminder{FireAt: at(14, 9, 0), EventAt: at(15, 9, 30), Waiting: true}, at(15, 9, 30), VerdictStale},
		{"a birthday after its noon", Reminder{FireAt: at(8, 12, 0), Rule: &yearly, Anchor: &Anchor{Month: time.October, Day: 15}, Waiting: true}, at(15, 13, 0), VerdictDeliver},
		{"a birthday the next local midnight", Reminder{FireAt: at(8, 12, 0), Rule: &yearly, Anchor: &Anchor{Month: time.October, Day: 15}, Waiting: true}, at(16, 0, 0), VerdictStale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if v, _ := q.ReminderVerdict(tc.r, tc.now); v != tc.want {
				t.Errorf("verdict = %q, want %q", v, tc.want)
			}
		})
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

// TestReminderVerdict_EventStartedAndQuietHours carries what EventReminderVerdict
// pinned (ADR-0030 point 4 and 5) now that the gate is the only verdict: a
// timed event that has started is never sent; before it the reminder is
// TriggerVerdict's, so the night still defers it, in the user's own night too.
func TestReminderVerdict_EventStartedAndQuietHours(t *testing.T) {
	event := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	fireAt := event.Add(-time.Hour)
	r := Reminder{FireAt: fireAt, LeadMinutes: 60}
	q := DefaultQuietHours()
	for _, tc := range []struct {
		now  time.Time
		want Verdict
	}{
		{fireAt.Add(-time.Minute), VerdictPending},
		{event.Add(-time.Nanosecond), VerdictDeliver},
		{event, VerdictStale},
		{event.Add(30 * time.Minute), VerdictStale},
	} {
		if got, _ := q.ReminderVerdict(r, tc.now); got != tc.want {
			t.Errorf("at %v = %s, want %s", tc.now, got, tc.want)
		}
	}
	quietFire := time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
	if got, _ := q.ReminderVerdict(Reminder{FireAt: quietFire, LeadMinutes: 300}, quietFire.Add(time.Minute)); got != VerdictDefer {
		t.Errorf("in quiet hours = %s, want defer", got)
	}
	late := QuietHours{End: TimeOfDay{Hour: 9}}
	if got, _ := late.ReminderVerdict(Reminder{FireAt: time.Date(2026, 10, 16, 7, 30, 0, 0, time.UTC), LeadMinutes: 270}, time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)); got != VerdictDefer {
		t.Errorf("before the stored quiet end = %s, want defer", got)
	}
}

func TestReminder_NamesEvent(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    Reminder
		want bool
	}{
		{"nothing", Reminder{}, false},
		{"a payload event", Reminder{EventAt: time.Unix(1, 0)}, true},
		{"a lead", Reminder{LeadMinutes: 5}, true},
		{"a rule", Reminder{Rule: &yearly}, true},
		{"only the unit's event, which the caller reads on demand", Reminder{Current: ptrTime(time.Unix(1, 0))}, false},
	} {
		if got := tc.r.NamesEvent(); got != tc.want {
			t.Errorf("%s: NamesEvent = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestIsDateOnly: the one test for "a bare date, stored as local midnight",
// shared by reminderInstants and the delivery gate. Any sub-midnight component
// makes the event timed.
func TestIsDateOnly(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	midnight := time.Date(2026, 10, 15, 0, 0, 0, 0, zone)
	for _, tc := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"local midnight", midnight, true},
		{"local midnight read from UTC", midnight.UTC(), true},
		{"a minute past", midnight.Add(time.Minute), false},
		{"a second past", midnight.Add(time.Second), false},
		{"a nanosecond past", midnight.Add(time.Nanosecond), false},
		{"an hour past", midnight.Add(time.Hour), false},
	} {
		if got := isDateOnly(tc.at.In(zone)); got != tc.want {
			t.Errorf("%s: isDateOnly = %v, want %v", tc.name, got, tc.want)
		}
	}
}
