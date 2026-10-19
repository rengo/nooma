package prospection

import (
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

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
	// RefusalQuietHours — the event is ahead, but every instant left before
	// it falls in quiet hours, when nothing is sent (ADR-0030 point 4).
	RefusalQuietHours Refusal = "quiet_hours"
	// RefusalCovered: every lead left is due at or before the last
	// reminder of the event that went out, which already covered it
	// (FollowSent; only a re-arm under new preferences gives it).
	RefusalCovered Refusal = "covered"
	// RefusalTooLate: an asked reminder would go out at once, but less than
	// AtOnceMarginMinutes remain before its instant or its local day ends
	// (ADR-0031 point 2).
	RefusalTooLate Refusal = "too_late"
)

// Plan is one row of Arm's decision: what to arm and with what. A dated
// event arms several, one Plan each (ADR-0029).
//
// Rule and Anchor are meaningful only for ArmRecurring, and
// LeadMinutes only for a one-shot ArmTrigger — a timer fires at the instant
// the user named, with no horizon in front of it.
type Plan struct {
	What Armament
	Why  Refusal // RefusalNone unless What == ArmNothing
	// FireAt is when the nudge goes out. About is what it is about — the
	// due instant of a timer, the event a trigger watches, the next
	// occurrence a recurrence lands on.
	//
	// **They are routinely different, and reporting one as the other is
	// how a correct reading looks like a misparse.** A lead moves a
	// firing ahead of its event and an at-once arming pulls it to the
	// capture instant, so "Reminder set for <FireAt>" answers a question
	// nobody asked. Zero when What is ArmNothing.
	FireAt time.Time
	About  time.Time
	// Immediate is true when the firing was moved to the capture instant,
	// because the notification horizon was already behind.
	//
	// It is carried rather than re-derived: a reader comparing FireAt to
	// About sees a gap of hours or days and describes the nudge as
	// arriving "the day before", when it arrives NOW. The duration is
	// right and the promise is false — which is exactly the class of
	// defect this whole change exists to end.
	Immediate bool
	// LeadMinutes is how far before About a one-shot event reminder was
	// armed to fire; 0 for one armed at once.
	LeadMinutes int
	// Leads are the user's leads this one-shot event reminder serves,
	// before any quiet-hours move: several when they landed on one instant
	// (a re-arm reads them back from the sent ones); none for the at-once
	// reminder.
	Leads     []time.Duration
	Rule      Rule
	Anchor    Anchor
	Interrupt Interrupt
	// Asked marks a reminder armed because the user's message asked for it.
	Asked bool
	// DayOnly marks an asked reminder whose instant is a bare date: About is
	// that day's local midnight and the reminder fires at DateOnlyAt.
	DayOnly bool
	// NightExempt marks a reminder that quiet hours neither move nor hold: an
	// asked one at a named time, or armed at once (ADR-0031 point 6).
	NightExempt bool
}

// Arm decides what one classification arms at one instant (spec R6.1,
// design §3.7), one Plan per row to write: several for a dated event, one
// otherwise. When it arms nothing it returns one ArmNothing Plan saying
// why, and false. prefs are the user's event reminder preferences.
//
// asked is whether the user's own words asked to be reminded
// (decided by the caller, not by Arm). An asked unit of any kind that persists one arms
// its instant (its own field, else the other) through leadSet, as ADR-0031
// decides; a timer keeps its own branch below.
func Arm(c classify.Classification, asked bool, prefs ReminderPrefs, now time.Time) ([]Plan, bool) {
	// Resolved once, for every branch including the ones that arm nothing.
	// A zero Interrupt would report itself degraded and therefore look
	// correct, which is exactly why it is not good enough: only a real
	// resolution keeps a claimed 0.0 apart from an absent reading, and doc
	// 02 §7 makes that distinction decide whether brain persists NULL.
	interrupt := ResolveInterrupt(c.InterruptLevel)
	nothing := func(why Refusal) ([]Plan, bool) {
		return []Plan{{What: ArmNothing, Why: why, Interrupt: interrupt}}, false
	}

	if c.Kind == nil {
		return nothing(RefusalNoKind)
	}

	if _, persists := c.Kind.UnitType(); asked && persists {
		at, dayOnly, ok := askedInstant(c)
		if !ok {
			return nothing(RefusalNoDate)
		}
		// A recurring reminder with a rule keeps its recurring row (ADR-0034),
		// asked or not: a one-shot would drop the repetition the message asked
		// for. Its time is the asked instant, so a due_at alone is read too.
		// A task or event with a rule is, until a later link, its one-shot
		// occurrence (D3).
		if *c.Kind == classify.KindRecurringReminder && c.RecurrenceRule != nil {
			return []Plan{recurringTrigger(at, recurrenceRule(*c.RecurrenceRule), prefs, now, interrupt)}, true
		}
		leads := leadSet(*c.Kind, true, at, dayOnly, prefs, now.Location())
		return reminders(at, dayOnly, skipToldDay(leads, at, now, now), prefs, now, interrupt, nil)
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
		return []Plan{{What: ArmTimer, FireAt: *c.DueAt, About: *c.DueAt, Interrupt: interrupt}}, true

	case classify.KindRecurringReminder:
		if c.EventAt == nil {
			return nothing(RefusalNoDate)
		}
		if c.RecurrenceRule == nil {
			// The rule degraded, or the model never claimed one. The capture
			// is honoured as its dated occurrence; the recurrence is not
			// invented (doc 02 §5.1's own posture toward a degraded field).
			return eventReminders(*c.EventAt, prefs, now, now, interrupt, nil)
		}

		return []Plan{recurringTrigger(*c.EventAt, recurrenceRule(*c.RecurrenceRule), prefs, now, interrupt)}, true

	case classify.KindEvent:
		if c.EventAt == nil {
			return nothing(RefusalNoDate)
		}
		return eventReminders(*c.EventAt, prefs, now, now, interrupt, nil)
	}

	return nothing(RefusalKindNotArming)
}

// askedInstant is the instant an asked unit is reminded about: an event's
// event_at, any other kind's due_at, else the other field (spec terms), with
// whether the model wrote it as a bare date.
func askedInstant(c classify.Classification) (at time.Time, dayOnly, ok bool) {
	own, other, ownDay, otherDay := c.DueAt, c.EventAt, c.DueAtDayOnly, c.EventAtDayOnly
	if *c.Kind == classify.KindEvent || *c.Kind == classify.KindRecurringReminder {
		own, other, ownDay, otherDay = other, own, otherDay, ownDay
	}
	switch {
	case own != nil:
		return *own, ownDay, true
	case other != nil:
		return *other, otherDay, true
	}
	return time.Time{}, false, false
}

// recurringTrigger arms the recurring trigger a dated occurrence and a
// rule own. A function rather than a branch of Arm so that any other
// decision arming a recurrence re-anchors by the very rule capture uses.
//
// It fires ON the next occurrence, with no lead (ADR-0034): at the event's
// own time of day, or at prefs.DateOnlyAt for a bare date (local midnight,
// the reading an unasked one-shot gets). Firing re-arms the next one.
func recurringTrigger(eventAt time.Time, rule Rule, prefs ReminderPrefs, now time.Time, interrupt Interrupt) Plan {
	// The weekday is always stated, derived from the capture's own
	// dated occurrence — "every Sunday" arrives as an event_at that is
	// a Sunday. Set for every rule rather than only for weekly: it
	// costs nothing, and an anchor whose weekday depends on which rule
	// was decoded is an anchor that means different things in
	// different rows.
	weekday := eventAt.Weekday()
	at := TimeOfDay{Hour: eventAt.Hour(), Minute: eventAt.Minute()}
	if isDateOnly(eventAt) {
		at = prefs.DateOnlyAt
	}
	anchor := Anchor{Month: eventAt.Month(), Day: eventAt.Day(), Weekday: &weekday, At: &at}
	// The next occurrence, re-derived from the anchor rather than the
	// date the user stated: a birthday captured after this year's has
	// passed arms for next year's, and About must name the instant the
	// nudge is actually about.
	next := NextOccurrence(rule, anchor, now)
	return Plan{What: ArmRecurring, FireAt: next, About: next, Rule: rule, Anchor: anchor, Interrupt: interrupt}
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
	case classify.RecurrenceRuleDaily:
		return RuleDaily
	case classify.RecurrenceRuleWeekly:
		return RuleWeekly
	}
	return RuleYearly
}
