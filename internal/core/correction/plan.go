package correction

import "github.com/rengo/nooma/internal/core/classify"

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
// Dates win over content whenever either date is present: writing event_at
// from Classification.EventAt requires no inference — the field means the
// same thing on both sides of the pipeline — while writing content from
// NormalizedContent requires inferring that the model's normalization of
// the correction *utterance* is the referent's new *body*, which this
// package licenses only when there is nothing else to write. The accepted
// cost, owner-accepted and unmitigated: a correction that moves a date
// leaves the referent's body stale, naming whatever it named before, until
// a later correction touches the text itself.
//
// The returned slice holds at most one element (see plan_test.go's own
// invariant test) and stays a slice rather than a single Edit on purpose:
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
func PlanEdit(c classify.Classification) ([]Edit, bool) {
	hasEvent := c.EventAt != nil
	hasDue := c.DueAt != nil

	switch {
	case hasEvent && hasDue:
		return nil, false
	case hasEvent:
		return []Edit{NewEventAtEdit(*c.EventAt)}, true
	case hasDue:
		return []Edit{NewDueAtEdit(*c.DueAt)}, true
	case c.NormalizedContent != nil:
		return []Edit{NewContentEdit(*c.NormalizedContent)}, true
	default:
		return nil, false
	}
}
