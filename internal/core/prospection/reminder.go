package prospection

import "time"

// LapseReason is why a reminder is not sent (ADR-0032). It travels with a
// Stale verdict so the trail says which of the two ended the reminder.
type LapseReason string

const (
	// LapsePassed: the event the reminder is about is no longer ahead.
	LapsePassed LapseReason = "passed"
	// LapseOverdue: the reminder is late past ADR-0009's window.
	LapseOverdue LapseReason = "overdue"
)

// Reminder is what the delivery gate judges: the facts a stored trigger and
// its unit give, unjudged. The gate resolves the event the reminder is about
// itself, so every caller (the due scan, the digest, Today) reads it one way.
type Reminder struct {
	// FireAt is when the reminder came due, in any zone: it is stored in UTC.
	FireAt time.Time
	// EventAt is the event instant the trigger's payload carries, zero when
	// it carries none (a row armed before ADR-0029 or at once).
	EventAt time.Time
	// LeadMinutes is the payload's lead: FireAt plus it is the event, when
	// positive. Zero means a reminder armed at once, which names no event.
	LeadMinutes int
	// Rule and Anchor are set for a recurring reminder, whose event is its
	// next occurrence after FireAt.
	Rule   *Rule
	Anchor *Anchor
	// Current is the instant the reminder's unit holds for its event now,
	// nil when unknown. It is read only for a one-shot reminder: a recurring
	// unit's event_at is an anchor, its year discarded (doc 02 §7).
	Current *time.Time
	// Waiting is a reminder fired and waiting for its digest. ADR-0009's
	// window belongs to a push, so lateness does not judge it.
	Waiting bool
}

// ReminderVerdict decides whether a reminder may be sent, in q's night
// (ADR-0032 point 1), in this order:
//
//  1. Its event is past: Stale, LapsePassed.
//  2. It is waiting for the digest: Deliver. Only the event judges it.
//  3. Otherwise TriggerVerdict, whose Stale is LapseOverdue. Quiet hours
//     still answer Defer first.
//
// The event is, in order: the payload's own; FireAt plus a positive lead; for
// a recurring reminder, the occurrence NextOccurrence derives from FireAt read
// in now's zone (FireAt is stored in UTC and NextOccurrence takes its zone from
// its argument, so the UTC reading lands on UTC noon, hours off the local
// calendar day), which assumes a period longer than the lead; and for a
// one-shot reminder with none of those, Current. A reminder naming no event is
// never passed: lead_minutes 0 is not an event at FireAt.
//
// An event is past once its instant has gone, except a day-granular one: a
// recurring occurrence (anchored at noon, but a calendar day) and a date-only
// event (stored as local midnight, the test reminderInstants applies) are
// reminded about all of their day, and are past from the next local midnight in
// now's zone. A timed event keeps the instant rule.
func (q QuietHours) ReminderVerdict(r Reminder, now time.Time) (Verdict, LapseReason) {
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

// NamesEvent reports whether the reminder carries its own event: a payload
// instant, a positive lead or a recurrence rule. Only a reminder that does not
// (a row armed before ADR-0029, or at once) needs the unit's event read.
func (r Reminder) NamesEvent() bool {
	return !r.EventAt.IsZero() || r.LeadMinutes > 0 || r.Rule != nil
}

// passed is whether the event at about is behind now, by the day for a
// day-granular one.
func (r Reminder) passed(about, now time.Time) bool {
	local := about.In(now.Location())
	if r.Rule != nil || (local.Hour() == 0 && local.Minute() == 0 && local.Second() == 0 && local.Nanosecond() == 0) {
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
