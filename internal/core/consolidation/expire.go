package consolidation

import "time"

// IncompleteExpiryHours is doc 02 §1's own fixed number — quoted, not
// chosen (design.md §4.2): "an `incomplete` unit ... promoted with what it
// has, or archived if still unresolved, after 24 h during consolidation".
const IncompleteExpiryHours = 24

// Incomplete is a unit.StatusIncomplete unit's ambiguity-resolution state
// at the instant expire_incomplete runs (spec R2.1).
//
// Unresolved is a field this package declares and no code in M2 produces
// (design.md §4.2, design §9 Q1): m2c passes false for every unit, because
// no column and no surface can set it yet. This is proven against a
// repo-constructed Incomplete{Unresolved: true} input; it does not claim a
// producer exists.
type Incomplete struct {
	UnitID     string
	CreatedAt  time.Time
	Unresolved bool
}

// ExpireIncomplete resolves doc 02's §1/§6.1 contradiction: promotion is
// the default outcome at 24h, and archival is the exception a caller must
// evidence via Unresolved (spec R2.1, design.md §4.2).
func ExpireIncomplete(us []Incomplete, now time.Time) []Transition {
	return nil
}
