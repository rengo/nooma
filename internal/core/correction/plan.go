package correction

import (
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
)

// PlanEdit decides which field a correction changes — doc 02 §5 step 4,
// design D3, the C6 ruling that overruled spec R1.8's prior revision:
//
//	event_at present, due_at absent  -> [NewEventAtEdit(*c.EventAt)], true
//	due_at present, event_at absent  -> [NewDueAtEdit(*c.DueAt)], true
//	neither date, content survived   -> [NewContentEdit(*c.NormalizedContent)], true
//	both dates present               -> nil, false (ask)
//	neither date nor content survived -> nil, false (ask)
//
// false means there is nothing unambiguous to write; the caller asks
// instead of guessing, the same ask-shaped result an ambiguous referent
// (Referent) already produces.
//
// Dates win over content whenever either date is present, with one
// exception: new text that states the new instant travels with the date
// (withText), so a correction that changes the wording and the time at once
// keeps both. Beyond that exception, writing event_at
// from Classification.EventAt requires no inference — the field means the
// same thing on both sides of the pipeline — while writing content from
// NormalizedContent requires inferring that the model's normalization of
// the correction *utterance* is the referent's new *body*, which this
// package licenses only when there is nothing else to write. A moved date
// still carries the body's own statement of it, but that edit is derived
// afterwards by CarryText, not decided here (doc 02 §5 step 4, I29).
//
// The returned slice holds one edit, or a date edit and the text that
// states it (see plan_test.go's own invariant test), and stays a slice
// rather than a single Edit on purpose:
// the shape was introduced before the C6 ruling precisely so the ruling
// would cost one function body and this table — not the port, not the
// pre-image shape, not dispatchEdits. Collapsing it to a single Edit now
// would re-hardcode today's one-field answer into every caller and make
// the next ruling expensive again (design D3).
//
// c is the whole Classification, not three positional values — passing
// (c.NormalizedContent, c.EventAt, c.DueAt) would put two *time.Time
// arguments side by side, I18's exact failure mode with nothing guarding
// it. The cost is that core/correction imports core/classify, the same
// accepted smell m1b D7 named for the same reversal criterion.
//
// target is the unit being corrected, and zone the user's frame. A date
// equal to target's own value is an echo (I30): the correction prompt
// shows the model the unit's current instants, so a text-only correction
// can come back with them repeated. An echo yields to content that is a
// real change and still states the instant the unit keeps (KeepsInstant):
// letting it win would discard the text the correction spoke about and
// record a change that never happened. Otherwise the echo stands — a
// same-date correction, which changes nothing in the unit and is how
// repeating a correction repairs a reminder a failed step left behind
// (I29).
func PlanEdit(c classify.Classification, target unit.Unit, zone *time.Location) ([]Edit, bool) {
	eventEcho, dueEcho := echoes(c.EventAt, target.EventAt), echoes(c.DueAt, target.DueAt)
	newDate := c.EventAt != nil && !eventEcho || c.DueAt != nil && !dueEcho
	if (eventEcho || dueEcho) && !newDate && c.NormalizedContent != nil &&
		*c.NormalizedContent != target.Content && KeepsInstant(*c.NormalizedContent, target, zone) {
		return []Edit{NewContentEdit(*c.NormalizedContent)}, true
	}
	if newDate {
		if eventEcho {
			c.EventAt = nil
		}
		if dueEcho {
			c.DueAt = nil
		}
	}
	hasEvent := c.EventAt != nil
	hasDue := c.DueAt != nil

	switch {
	case hasEvent && hasDue:
		return nil, false
	case hasEvent:
		return withText(NewEventAtEdit(*c.EventAt), *c.EventAt, target.EventAt, c.NormalizedContent, target, zone), true
	case hasDue:
		return withText(NewDueAtEdit(*c.DueAt), *c.DueAt, target.DueAt, c.NormalizedContent, target, zone), true
	case c.NormalizedContent != nil:
		return []Edit{NewContentEdit(*c.NormalizedContent)}, true
	default:
		return nil, false
	}
}

// withText is a date edit and, when the correction changed the wording as
// well, the text that goes with it. The model's text is taken only when it
// is a real change that states the new instant — date and anchored time, in
// the user's zone — and no longer states the previous one: then it speaks
// for the whole unit, and the old body has nothing left to contribute.
// Otherwise the date stands alone and CarryText moves the old body's own
// statement of it.
func withText(date Edit, next time.Time, previous *time.Time, text *string, target unit.Unit, zone *time.Location) []Edit {
	if text != nil && *text != target.Content && StatesInstant(*text, next, zone) &&
		(previous == nil || !StatesInstant(*text, *previous, zone)) {
		return []Edit{date, NewContentEdit(*text)}
	}
	return []Edit{date}
}

// echoes reports whether a classified date repeats the unit's current one:
// the same instant, whatever frame either is written in.
func echoes(classified, current *time.Time) bool {
	return classified != nil && current != nil && classified.Equal(*current)
}
