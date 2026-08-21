package prospection

import (
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

// EventLeadDays is doc 02 §7's notification horizon: how far before a dated
// event its trigger fires. Untyped and not a time.Duration, for the reason
// QuietHoursStartHour's own comment gives.
const EventLeadDays = 7

// LeadTime returns the instant EventLeadDays before eventAt, in eventAt's
// own location.
//
// Deliberately unclamped (spec R5.2, Finding F5): it answers "when is the
// horizon for this event", which is a fact about the event, and returns an
// instant in the past when the event is closer than the horizon. Arm applies
// the now-clamp at its own call site, because "do not arm in the past" is a
// fact about arming rather than about the horizon. Two layers, so each is
// testable on its own terms.
func LeadTime(eventAt time.Time) time.Time {
	y, m, d := eventAt.Date()
	return time.Date(y, m, d-EventLeadDays, eventAt.Hour(), eventAt.Minute(),
		eventAt.Second(), eventAt.Nanosecond(), eventAt.Location())
}

// Armament is what a classification arms, if anything.
type Armament string

const (
	ArmNothing   Armament = "nothing"
	ArmTimer     Armament = "timer"
	ArmTrigger   Armament = "trigger"
	ArmRecurring Armament = "recurring_trigger"
)

// Plan is Arm's decision: what to arm and with what.
//
// Rule and Anchor are meaningful only for ArmRecurring, and LeadDays only
// for the two trigger kinds — a timer fires at the instant the user named,
// with no horizon in front of it.
type Plan struct {
	What      Armament
	FireAt    time.Time
	LeadDays  int
	Rule      Rule
	Anchor    Anchor
	Interrupt Interrupt
}

// Arm decides what one classification arms at one instant (spec R6.1,
// design §3.7). It is the same shape classify.Kind's own mapping already
// has: a decision about a value, taking the instant as an argument.
func Arm(c classify.Classification, now time.Time) (Plan, bool) {
	// Resolved once, for every branch including the ones that arm nothing.
	// A zero Interrupt would report itself degraded and therefore look
	// correct, which is exactly why it is not good enough: only a real
	// resolution keeps a claimed 0.0 apart from an absent reading, and doc
	// 02 §7 makes that distinction decide whether brain persists NULL.
	interrupt := ResolveInterrupt(c.InterruptLevel)
	nothing := Plan{What: ArmNothing, Interrupt: interrupt}

	if c.Kind == nil {
		return nothing, false
	}

	switch *c.Kind {
	case classify.KindTimer:
		// due_at, never event_at (I18). event_at is when a thing happens in
		// the world and a timer has no world event; due_at is when this is
		// owed, and a timer is owed at its fire instant.
		if c.DueAt == nil || !c.DueAt.After(now) {
			return nothing, false
		}
		return Plan{What: ArmTimer, FireAt: *c.DueAt, Interrupt: interrupt}, true

	case classify.KindRecurringReminder:
		if c.EventAt == nil {
			return nothing, false
		}
		if c.RecurrenceRule == nil {
			// The rule degraded, or the model never claimed one. The capture
			// is honoured as its dated occurrence; the recurrence is not
			// invented (doc 02 §5.1's own posture toward a degraded field).
			return datedTrigger(*c.EventAt, now, interrupt)
		}

		anchor := Anchor{Month: c.EventAt.Month(), Day: c.EventAt.Day()}
		rule := recurrenceRule(*c.RecurrenceRule)
		return Plan{
			What:      ArmRecurring,
			FireAt:    clampToNow(LeadTime(NextOccurrence(rule, anchor, now)), now),
			LeadDays:  EventLeadDays,
			Rule:      rule,
			Anchor:    anchor,
			Interrupt: interrupt,
		}, true

	case classify.KindEvent:
		if c.EventAt == nil {
			return nothing, false
		}
		return datedTrigger(*c.EventAt, now, interrupt)
	}

	return nothing, false
}

// datedTrigger arms the one-shot trigger a dated event owns, or nothing
// when that event is already over — doc 02 §5.1 refuses to arm on a date it
// cannot trust, and a nudge for something already finished is the same
// refusal pointing the other way.
func datedTrigger(eventAt, now time.Time, interrupt Interrupt) (Plan, bool) {
	if !eventAt.After(now) {
		return Plan{What: ArmNothing, Interrupt: interrupt}, false
	}
	return Plan{
		What:      ArmTrigger,
		FireAt:    clampToNow(LeadTime(eventAt), now),
		LeadDays:  EventLeadDays,
		Interrupt: interrupt,
	}, true
}

// clampToNow is the layer LeadTime deliberately does not have (Finding F5).
// An event captured two days before it happens has a horizon five days in
// the past, and arming there hands the staleness gate something born
// expired. The lead time is a notification horizon, and the system is not
// late for an event it only just learned about.
func clampToNow(fireAt, now time.Time) time.Time {
	if fireAt.Before(now) {
		return now
	}
	return fireAt
}

// recurrenceRule converts classify's own vocabulary into this package's.
// The conversion lives here because prospection imports classify and never
// the reverse (Finding F3), and it is total: classify has already degraded
// anything outside its own closed set to nil, and a nil rule never reaches
// this function.
func recurrenceRule(r classify.RecurrenceRule) Rule {
	if r == classify.RecurrenceRuleMonthly {
		return RuleMonthly
	}
	return RuleYearly
}
