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
// For every finite result, Revive returns a write and the bool is true. It
// boosts the *effective* weight at now, never the persisted c.Weight —
// boosting the persisted value would make decay freely reversible and
// c.Weight a ratchet — and the boost is asymptotic: e + ReviveGain *
// (WeightCeiling - e), bounded by construction, strictly increasing in e,
// with no clamp and no discontinuity. An additive boost with a clamp
// collapses every unit above the clamp onto the same value, destroying the
// ordering hysteresis exists to protect; this shape never does (spec R2.2,
// the reconciliation's ruling 2).
//
// When e is already at or above WeightCeiling, the gain term is floored at
// zero and Revive returns Boost{c.UnitID, e, now} unchanged in weight — but
// LastTouchedAt still moves to now. That write is effective-weight-neutral
// by construction (the pairs (c.Weight, c.LastTouchedAt) and (e, now)
// denote the same decay curve, so Effective returns the same value at
// every future instant either way) and is not a no-op regardless:
// last_touched_at is the vault's record of *direct* use, and a direct use
// at the ceiling is still a decision with a real effect worth recording —
// doc 02 §11's "no effect, no write" does not apply to it (spec R2.3).
//
// c.DecayRate is read only to compute e and is never modified or returned:
// assigning λ is classify's job, and use does not make a thing decay more
// slowly.
//
// STUB — the second return value is always true. Non-finite refusal lands
// in the next commit.
func Revive(c Current, now time.Time) (Boost, bool) {
	e := Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)

	gain := WeightCeiling - e
	if gain < 0 {
		gain = 0
	}

	return Boost{
		UnitID:        c.UnitID,
		Weight:        e + ReviveGain*gain,
		LastTouchedAt: now,
	}, true
}
