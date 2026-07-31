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
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
