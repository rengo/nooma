package prospection

import "time"

// LapseReason is why a Stale reminder is not sent (ADR-0032).
type LapseReason string

const (
	// LapsePassed: the event the reminder is about is no longer ahead.
	LapsePassed LapseReason = "passed"
	// LapseOverdue: the reminder is late past ADR-0009's window.
	LapseOverdue LapseReason = "overdue"
	// LapseSuperseded: the unit's event is no longer the one it is about.
	LapseSuperseded LapseReason = "superseded"
)

// Reminder is what the delivery gate judges: the facts a stored trigger and
// its unit give. The gate resolves the event itself, so every caller reads it
// one way.
type Reminder struct {
	// FireAt is when the reminder came due, in any zone: it is stored in UTC.
	FireAt time.Time
	// EventAt is the event instant the payload carries, zero when none.
	EventAt time.Time
	// LeadMinutes: FireAt plus it is the event, when positive. Zero is a
	// reminder armed at once, which names no event.
	LeadMinutes int
	// Rule and Anchor are set for a recurring reminder.
	Rule   *Rule
	Anchor *Anchor
	// Current is the unit's event now, nil when unknown. Never read for a
	// recurring reminder: its unit's event_at is an anchor (doc 02 §7).
	Current *time.Time
	// Waiting is a fired reminder waiting for its digest; lateness (a
	// push's window) does not judge it.
	Waiting bool
	// Newer: the unit has armed a one-shot reminder of its own since (D8).
	// Only a reminder that names no event can be superseded by it. "Since" is
	// not known (a trigger carries no creation instant), so a legacy row can be
	// held beside a reminder that was in fact armed before it: accepted.
	Newer bool
	// Asked, DayOnly and NightExempt are the payload's keys of the same name
	// (ADR-0031 D5): the reminder was asked for; its event is a bare date;
	// quiet hours do not hold it.
	Asked, DayOnly, NightExempt bool
}

// ReminderVerdict decides whether a reminder may be sent, in q's night
// (ADR-0032 point 1), in this order:
//
//  1. It is superseded: Stale, LapseSuperseded, or LapsePassed when the unit's
//     new event is past too. Its payload's event is not the
//     unit's current one (compared to the second), or it names no event and
//     the unit has armed a newer reminder. A lead, a rule or no event cannot
//     be contradicted otherwise.
//  2. Its event is past: Stale, LapsePassed.
//  3. It is waiting for the digest: Deliver.
//  4. Otherwise TriggerVerdict, whose Stale is LapseOverdue (Defer still first).
//
// The event is, in order: the payload's; FireAt plus a positive lead; for a
// recurring reminder, the occurrence NextOccurrence derives from FireAt read in
// now's zone (FireAt is stored in UTC, and the UTC reading lands on UTC noon,
// hours off the local day), assuming a period longer than the lead; and for a
// one-shot reminder with none of those, Current. A reminder naming no event is
// never passed: lead_minutes 0 is not an event at FireAt.
//
// An event is past once its instant has gone, except a day-granular one (a
// recurring occurrence or a date-only event): past from the next local midnight.
func (q QuietHours) ReminderVerdict(r Reminder, now time.Time) (Verdict, LapseReason) {
	if r.superseded() {
		// Held for a re-plan while the unit's new event is ahead; when it is
		// past too there is nothing to re-plan and the row has passed.
		if r.Current != nil && r.passed(*r.Current, now) {
			return VerdictStale, LapsePassed
		}
		return VerdictStale, LapseSuperseded
	}
	if about, known := r.about(now.Location()); known && r.passed(about, now) {
		return VerdictStale, LapsePassed
	}
	if r.Waiting {
		return VerdictDeliver, ""
	}
	trigger := q.TriggerVerdict
	if r.NightExempt {
		// A named time, or an at-once arming, rings through the night, and is
		// late from its own instant, as a timer is (ADR-0031 point 6).
		trigger = func(fireAt, now time.Time) Verdict { return verdict(fireAt, fireAt, TriggerStalenessHours, now, nil) }
	}
	v := trigger(r.FireAt, now)
	if v == VerdictStale {
		return v, LapseOverdue
	}
	return v, ""
}

func (r Reminder) superseded() bool {
	if !r.EventAt.IsZero() {
		return r.Current != nil && !r.EventAt.Truncate(time.Second).Equal(r.Current.Truncate(time.Second))
	}
	return !r.NamesEvent() && r.Newer
}

// NamesEvent reports whether the reminder carries its own event (a payload
// instant, a positive lead or a rule). One that does not needs its unit's event
// to be judged at all; one whose event is the payload's reads the unit too, to
// see whether it moved.
func (r Reminder) NamesEvent() bool {
	return !r.EventAt.IsZero() || r.LeadMinutes > 0 || r.Rule != nil
}

// passed is whether the event at about is behind now, by the day for a
// day-granular one.
func (r Reminder) passed(about, now time.Time) bool {
	local := about.In(now.Location())
	if r.dayGranular(local) {
		y, m, d := local.Date()
		return !now.Before(time.Date(y, m, d+1, 0, 0, 0, 0, now.Location()))
	}
	return !now.Before(about)
}

// dayGranular is whether local, the reminder's event in the user's zone, is a
// whole day rather than a moment. An asked row is a day only when its payload
// says so: an asked named midnight passes at midnight. A row nobody asked about
// keeps the midnight heuristic (ADR-0031 D5).
func (r Reminder) dayGranular(local time.Time) bool {
	return r.Rule != nil || r.DayOnly || (!r.Asked && isDateOnly(local))
}

// Occasion is the instant a message about the reminder names, in zone, whether
// it is a whole day (no clock time is said), and whether any is known (ADR-0032
// point 4): the unit's event now, or for a recurring one its next occurrence.
func (r Reminder) Occasion(zone *time.Location) (about time.Time, dayOnly, known bool) {
	if r.Rule == nil && r.Current != nil {
		about, known = *r.Current, true
	} else {
		about, known = r.about(zone)
	}
	if !known {
		return time.Time{}, false, false
	}
	return about, r.dayGranular(about.In(zone)), true
}

// about is the event instant the reminder is about, and whether it is known.
func (r Reminder) about(zone *time.Location) (time.Time, bool) {
	switch {
	case !r.EventAt.IsZero():
		return r.EventAt, true
	case r.LeadMinutes > 0:
		return r.FireAt.Add(time.Duration(r.LeadMinutes) * time.Minute), true
	case r.Rule != nil:
		if r.Anchor == nil {
			return time.Time{}, false
		}
		return NextOccurrence(*r.Rule, *r.Anchor, r.FireAt.In(zone)), true
	case r.Current != nil:
		return *r.Current, true
	}
	return time.Time{}, false
}
