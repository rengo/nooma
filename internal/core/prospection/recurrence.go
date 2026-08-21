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
)

// Anchor is doc 02 §7's recurrence_anchor. Month is ignored by RuleMonthly,
// whose recurrence is "this day, every month".
type Anchor struct {
	Month time.Month
	Day   int
}

// RecurrenceAnchorHour is the local wall clock an anniversary lands on.
const RecurrenceAnchorHour = 12

// NextOccurrence returns the first occurrence strictly after `after`.
func NextOccurrence(rule Rule, anchor Anchor, after time.Time) time.Time {
	return time.Time{}
}
