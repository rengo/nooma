package prospection

import "time"

// Rule is doc 02 §7's recurrence vocabulary as prospection reads it.
//
// classify decodes its own same-named vocabulary (classify.RecurrenceRule)
// and PR 7's Arm converts one into the other at its call site: prospection
// imports classify, so a classify field typed from here would be the
// reverse edge Go refuses (Finding F3).
type Rule string

const (
	RuleYearly  Rule = "yearly"
	RuleMonthly Rule = "monthly"
	RuleDaily   Rule = "daily"
	RuleWeekly  Rule = "weekly"
)

// AllRules returns a fresh slice of the Rule vocabulary, in doc 02's
// declared order.
//
// It exists for the same reason classify.AllRecurrenceRules() does, and it
// is the other half of the same set: Finding F3 split one vocabulary across
// a package boundary, and a closed set that only one side can enumerate is
// a set the two sides can drift apart on without anything noticing. A
// function rather than a var so no caller can append to the vocabulary the
// completeness checks sweep.
func AllRules() []Rule {
	return []Rule{RuleYearly, RuleMonthly, RuleDaily, RuleWeekly}
}

// Anchor is doc 02 §7's recurrence_anchor. Which fields a rule reads is the
// rule's own business: RuleMonthly ignores Month, whose recurrence is "this
// day, every month", and RuleDaily reads nothing at all, because "every day"
// names no day.
type Anchor struct {
	Month time.Month
	Day   int
	// Weekday is the day RuleWeekly repeats on, and it is a pointer
	// because time.Weekday's zero value is Sunday — a perfectly legal
	// answer. A plain value could not tell "the user said Sunday" from
	// "nobody said anything", so one of those two would silently take the
	// other's behaviour. nil is the second, and a weekly rule reaching
	// NextOccurrence with nil falls back rather than repeating on a Sunday
	// nobody asked for.
	//
	// Arm always states it, derived from the capture's own event_at, so
	// nil can only arrive from a row stored before weekly existed.
	Weekday *time.Weekday
}

// RecurrenceAnchorHour is the local wall clock an anniversary lands on.
const RecurrenceAnchorHour = 12

// NextOccurrence returns the first occurrence strictly after `after`,
// always re-derived from the anchor and never advanced from the previous
// occurrence (design §3.6; I17's arithmetic half).
//
// Two rules, and they depend on each other.
//
// **Clamp to the last day of the target month.** A 29 February anchor is 28
// February in a common year; a day-31 monthly anchor is 30 April and 28 or
// 29 February. Go's own time.Date normalisation is rejected here: it turns
// Feb 31 into 3 March, so a monthly reminder wanders forward and can miss a
// month entirely, and a February anniversary lands in March. Skipping the
// months that lack the day is rejected too — a day-31 reminder would fire
// seven times a year and never in February.
//
// **Always re-derive from the anchor.** This is what makes the clamp safe.
// Advancing 29 February by one year gives 28 February, and advancing that
// gives 28 February forever: the anniversary drifts off its own date after
// one leap cycle, and a day-31 monthly reminder does the same after its
// first February. Re-deriving means occurrence N is the same instant however
// many times the trigger has re-armed, so re-arming is a pure function of
// (rule, anchor, now) and never of the trigger's own history.
//
// The result lands at RecurrenceAnchorHour in after's own location — the
// zone travels inside the instant, as everywhere else in this package.
//
// An unrecognised Rule resolves as yearly rather than panicking or returning
// a zero instant, and that is a decision rather than a fallthrough: this is
// a pure function with no error return, a zero time.Time would arm a trigger
// in year 1, and classify already degrades an unknown recurrence_rule to nil
// upstream — so a value arriving here at all means a row that predates that
// decoding, whose anchor is still the user's own stated month and day.
// TestNextOccurrence_UnknownRuleFallsBackToYearly pins it.
func NextOccurrence(rule Rule, anchor Anchor, after time.Time) time.Time {
	loc := after.Location()

	// A boolean switch rather than one on rule alone, so "a weekly rule
	// with no weekday" is not a case that falls out of the weekly arm
	// halfway through — it simply never enters it, and lands in the same
	// default an unrecognised rule does.
	switch {
	case rule == RuleDaily:
		// No anchor is read: "every day" names no day, so there is nothing
		// to re-derive from but the calendar itself. Building the candidate
		// through time.Date rather than adding 24 hours keeps this on the
		// wall clock — a day on which the zone shifts is still one calendar
		// day, and the occurrence stays at the anchor hour rather than
		// sliding by the offset change.
		candidate := time.Date(after.Year(), after.Month(), after.Day(),
			RecurrenceAnchorHour, 0, 0, 0, loc)
		if candidate.After(after) {
			return candidate
		}
		return time.Date(after.Year(), after.Month(), after.Day()+1,
			RecurrenceAnchorHour, 0, 0, 0, loc)

	case rule == RuleWeekly && anchor.Weekday != nil:
		// Step forward to the named weekday, then past it if today's
		// occurrence has already gone. The +7 before the modulo is what
		// makes the step non-negative for every pair of weekdays; without
		// it a target earlier in the week than today's yields a negative
		// step and the occurrence lands in the past.
		step := (int(*anchor.Weekday) - int(after.Weekday()) + 7) % 7
		candidate := time.Date(after.Year(), after.Month(), after.Day()+step,
			RecurrenceAnchorHour, 0, 0, 0, loc)
		if candidate.After(after) {
			return candidate
		}
		return time.Date(after.Year(), after.Month(), after.Day()+step+7,
			RecurrenceAnchorHour, 0, 0, 0, loc)

	case rule == RuleMonthly:
		// Walk months from after's own, so the first candidate is either
		// this month's occurrence (when it is still ahead) or next month's.
		y, m := after.Year(), after.Month()
		for {
			if candidate := clampedDate(y, m, anchor.Day, loc); candidate.After(after) {
				return candidate
			}
			y, m = nextMonth(y, m)
		}
	default:
		year := after.Year()
		for {
			if candidate := clampedDate(year, anchor.Month, anchor.Day, loc); candidate.After(after) {
				return candidate
			}
			year++
		}
	}
}

// clampedDate builds the anchor's instant in the given month, with the day
// clamped to that month's last rather than overflowing into the next.
//
// The last day is found by asking for the zeroth day of the following month,
// which time.Date normalises backwards to the previous month's last — the
// one normalisation this file uses on purpose, because it is exact for every
// month and leap year and needs no table.
//
// The probe runs in UTC, deliberately, and this is not an optimisation. How
// many days a month has is a property of the calendar, not of a zone, and
// asking a zone is wrong wherever that zone deleted the very day being
// looked up: Pacific/Kiritimati skipped 1994-12-31, so a zone-local probe
// for December 1994 normalises forward into January and reports 1. Every
// anchor above the first would then clamp to the first — a 25 December
// anniversary firing on 1 December, silently, in one zone. Only the
// resulting date is built in the caller's location.
func clampedDate(year int, month time.Month, day int, loc *time.Location) time.Time {
	// Month is clamped for the reason Day is, and the cost is higher: the
	// zero Anchor carries month 0, which time.Date normalises BACKWARD into
	// December of the PREVIOUS YEAR — an anniversary landing in a year the
	// user never named. Clamped into [January, December] before anything
	// reads it, so a degenerate anchor stays a wrong day rather than
	// becoming a wrong year.
	if month < time.January {
		month = time.January
	}
	if month > time.December {
		month = time.December
	}

	ny, nm := nextMonth(year, month)
	last := time.Date(ny, nm, 0, RecurrenceAnchorHour, 0, 0, 0, time.UTC).Day()

	// Clamped at both ends, because both ends are reachable.
	//
	// recurrence_anchor is nullable and Arm builds it from a classification
	// that may carry no day, so the zero Anchor arrives here. A day of 0 or
	// less is not merely odd under time.Date: it normalises BACKWARD, the
	// same mechanism this function uses two lines up on purpose, and would
	// put the occurrence in the previous month — a reminder firing in a
	// month the user never named.
	if day < 1 {
		day = 1
	}
	if day > last {
		day = last
	}
	return time.Date(year, month, day, RecurrenceAnchorHour, 0, 0, 0, loc)
}

func nextMonth(year int, month time.Month) (int, time.Month) {
	if month == time.December {
		return year + 1, time.January
	}
	return year, month + 1
}
