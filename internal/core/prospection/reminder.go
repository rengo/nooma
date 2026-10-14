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
	// Only a reminder that names no event can be superseded by it.
	Newer bool
}

// ReminderVerdict decides whether a reminder may be sent, in q's night
// (ADR-0032 point 1), in this order:
//
//  1. It is superseded: Stale, LapseSuperseded. Its payload's event is not the
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
		return VerdictStale, LapseSuperseded
	}
	if about, known := r.about(now.Location()); known && r.passed(about, now) {
		return VerdictStale, LapsePassed
	}
	if r.Waiting {
		return VerdictDeliver, ""
	}
	v := q.TriggerVerdict(r.FireAt, now)
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
// instant, a positive lead or a rule); only one that does not needs its unit read.
func (r Reminder) NamesEvent() bool {
	return !r.EventAt.IsZero() || r.LeadMinutes > 0 || r.Rule != nil
}

// passed is whether the event at about is behind now, by the day for a
// day-granular one.
func (r Reminder) passed(about, now time.Time) bool {
	local := about.In(now.Location())
	if r.Rule != nil || isDateOnly(local) {
		y, m, d := local.Date()
		return !now.Before(time.Date(y, m, d+1, 0, 0, 0, 0, now.Location()))
	}
	return !now.Before(about)
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
