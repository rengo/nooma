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

// Refusal is why Arm armed nothing. Spec R6.1 requires the causes to be
// told apart rather than collapsed into one branch, and the reason is not
// bookkeeping: m3b writes decision_log from this plan, so a refusal that
// cannot say why is a hole in the glass box. An undated event is a decoding
// failure worth surfacing; a chitchat capture arming nothing is the system
// working correctly. They must not read alike.
type Refusal string

const (
	// RefusalNone is the zero value, carried by every plan that armed
	// something. The field answers "why not", and an armed plan has no
	// answer to give.
	RefusalNone Refusal = ""
	// RefusalNoKind — the classification carries no type at all, so there
	// is nothing to decide from (doc 02 §5.1: no unit is created either).
	RefusalNoKind Refusal = "no_kind"
	// RefusalKindNotArming — this kind arms nothing by design. Ten of the
	// thirteen are in this group, and it is the system working.
	RefusalKindNotArming Refusal = "kind_not_arming"
	// RefusalNoDate — the arming instant degraded to absent. doc 02 §5.1:
	// "arming a trigger on a guessed date is worse than not arming one."
	RefusalNoDate Refusal = "no_date"
	// RefusalAlreadyPast — the instant exists and is behind now. The same
	// refusal pointing the other way: a nudge for something already over.
	RefusalAlreadyPast Refusal = "already_past"
)

// Plan is Arm's decision: what to arm and with what.
//
// Rule and Anchor are meaningful only for ArmRecurring, and LeadDays only
// for the two trigger kinds — a timer fires at the instant the user named,
// with no horizon in front of it.
type Plan struct {
	What      Armament
	Why       Refusal // RefusalNone unless What == ArmNothing
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
	nothing := func(why Refusal) (Plan, bool) {
		return Plan{What: ArmNothing, Why: why, Interrupt: interrupt}, false
	}

	if c.Kind == nil {
		return nothing(RefusalNoKind)
	}

	switch *c.Kind {
	case classify.KindTimer:
		// due_at, never event_at (I18). event_at is when a thing happens in
		// the world and a timer has no world event; due_at is when this is
		// owed, and a timer is owed at its fire instant.
		if c.DueAt == nil {
			return nothing(RefusalNoDate)
		}
		if !c.DueAt.After(now) {
			return nothing(RefusalAlreadyPast)
		}
		return Plan{What: ArmTimer, FireAt: *c.DueAt, Interrupt: interrupt}, true

	case classify.KindRecurringReminder:
		if c.EventAt == nil {
			return nothing(RefusalNoDate)
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
			return nothing(RefusalNoDate)
		}
		return datedTrigger(*c.EventAt, now, interrupt)
	}

	return nothing(RefusalKindNotArming)
}

// datedTrigger arms the one-shot trigger a dated event owns, or nothing
// when that event is already over — doc 02 §5.1 refuses to arm on a date it
// cannot trust, and a nudge for something already finished is the same
// refusal pointing the other way.
func datedTrigger(eventAt, now time.Time, interrupt Interrupt) (Plan, bool) {
	if !eventAt.After(now) {
		return Plan{What: ArmNothing, Why: RefusalAlreadyPast, Interrupt: interrupt}, false
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
// the reverse (Finding F3).
//
// **One case per member, and the default is for a value that is in neither
// vocabulary.** This was an `if r == Monthly { … }; return RuleYearly`,
// described as total. It was total only because the set had exactly two
// members and the `if` covered both: a third would have fallen through to
// RuleYearly with no error, no degradation and no decision_log row, arming
// a weekly reminder once a year. A conversion between two closed sets has
// to be written so that adding a member somewhere else breaks something
// here, and the sweep in arm_test.go over classify.AllRecurrenceRules() is
// what breaks.
//
// The default remains yearly rather than panicking or returning a zero
// value — this is a pure function with no error return, and classify has
// already degraded anything outside its own set to nil, so a value reaching
// the default at all is a stored row predating that decoding, whose anchor
// is still the user's own stated month and day.
func recurrenceRule(r classify.RecurrenceRule) Rule {
	switch r {
	case classify.RecurrenceRuleYearly:
		return RuleYearly
	case classify.RecurrenceRuleMonthly:
		return RuleMonthly
	}
	return RuleYearly
}
