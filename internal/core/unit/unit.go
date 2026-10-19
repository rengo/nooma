package unit

import (
	"encoding/json"
	"time"
)

// Unit is the atom of memory — doc 02 §1, the units table (migration
// 0001). It carries no behavior of its own in Phase A (no constructor, no
// validation): its round-trip fidelity is proven by PR 3's in-memory fake
// contract suite and PR 4's SQLite implementation, not by an L1 test here.
//
// Nullable columns are pointers and json.RawMessage, never sql.NullX
// (design §4): database/sql is denied inside internal/core by depguard's
// core-purity rule, and the pointer form is the precedent
// goldenset.ClassifyExpected already set — "absent" and "a legitimate
// zero" must not decode to the same value.
type Unit struct {
	ID              string
	Type            Type
	Content         string
	Status          Status
	Weight          float64
	WeightDecayRate float64
	LastTouchedAt   time.Time
	StructuredData  json.RawMessage // nil when the column is NULL
	Source          string
	EventAt         *time.Time // nil is not the zero time — I18
	DueAt           *time.Time
	Confidence      *float64 // always nil in Phase A — proposal §8 Q2
	// Language is the language the capture was written in, "" when none is known
	// (stored as NULL). Written once at capture; a correction never changes it.
	Language string
	// Title is the short, date-free name a reminder says for the unit, "" when
	// it has none (stored as NULL; spec R12). A reminder reads Title, falling
	// back to Content. A re-classifying correction rewrites it.
	Title string
	// RemindMe is whether the user's own words asked to be reminded of this unit
	// (spec R1): the classification's remind_request when it was stored. It is a
	// fact about the capture, never a verdict on whether anything was armed.
	RemindMe bool
	// ReminderLeads are how many minutes before its event_at this event's own
	// reminders ring, set by a correction (I40, ADR-0038); nil follows the
	// user's preferences (ADR-0029). Stored as a JSON array, NULL for nil.
	ReminderLeads []int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
