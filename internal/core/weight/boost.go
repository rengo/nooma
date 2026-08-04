package weight

import "time"

// ReviveGain is the fraction of the remaining gap to WeightCeiling that a
// single Revive closes. Default 0.35 (doc 02 §13).
const ReviveGain = 0.35

// WeightCeiling is the asymptote Revive and Resurface both boost toward,
// never reach, and never exceed. Default 2.0 (doc 02 §13).
const WeightCeiling = 2.0

// Current is a unit's decay-relevant state at the instant a boost is
// computed — everything Effective needs, plus the identity a Boost must
// carry back to the caller.
type Current struct {
	UnitID        string
	Weight        float64
	DecayRate     float64
	LastTouchedAt time.Time
}

// Boost is the only shape this package lets a caller persist: a new
// weight paired with the last_touched_at it belongs to. There is no
// constructor, and no exported function in this package, that produces a
// weight without a matching timestamp — I24's structural guarantee
// (spec R2.1, docs/06-harness.md §4).
type Boost struct {
	UnitID        string
	Weight        float64
	LastTouchedAt time.Time
}

// Revive computes the direct-use boost: doc 02 §2's "writes a new boosted
// weight and resets last_touched_at", made a formula (spec R2.2/R2.3,
// design D3/F2).
//
// STUB — returns the zero value. Implemented in the next commit.
func Revive(c Current, now time.Time) Boost {
	return Boost{}
}
