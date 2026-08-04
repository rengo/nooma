package weight

import (
	"math"
	"time"
)

// ReviveGain is the fraction of the remaining gap to WeightCeiling that a
// single Revive closes. Default 0.35 (doc 02 §13).
const ReviveGain = 0.35

// WeightCeiling is the asymptote Revive and Resurface both boost toward:
// for e < WeightCeiling, the boosted weight never reaches or exceeds it.
// That bound is over the boost's own contribution, not over every input —
// when e is already at or above WeightCeiling, Revive's gain term floors
// at zero and the returned weight equals e exactly (see Revive's doc
// comment), which does not lower an e that started above the ceiling
// back down to it. Default 2.0 (doc 02 §13).
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
// Revive returns (Boost{}, false) — refusing to produce a persistable
// value — when c.Weight or c.DecayRate is NaN or ±Inf and that
// non-finiteness survives into the computed weight. This is unreachable
// through capture today: encoding/json cannot decode a NaN or Infinity
// token. But weight and weight_decay_rate carry no CHECK constraint, so a
// corrupted row or a future arithmetic slip elsewhere could still produce
// one, and unlike Effective — whose NaN is a transient read result — a
// Boost is what a caller persists (I24's structural guarantee, above).
// Coercing that result to a finite number, 0 in particular, would be worse
// than refusing it: a coerced 0 could drive the unit under
// weight_threshold and archive it, a destructive state transition caused
// by nothing more than a read error. Refusing instead leaves the
// corruption visible and untouched, so doctor or a later repair path can
// still find it — and since nothing in the vault is ever deleted, a
// silently coerced value would otherwise make the corruption durable:
// every later read of that unit would return NaN forever.
//
// This is a genuine second false case for the bool the reconciliation's
// ruling 2 removed. Ruling 2 reasoned that "once a direct use always
// writes, the bool has no false case and carrying it is a lie in the
// type" — but that reasoning was about the ceiling edge, where a direct
// use really does always write. A non-finite input is a different case
// ruling 2 never considered; reintroducing the bool for it is an addition
// to ruling 2, not a reversal (recorded as C4,
// openspec/changes/m2a-weight-focus/tasks.md's Conflicts).
func Revive(c Current, now time.Time) (Boost, bool) {
	e := Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)

	gain := WeightCeiling - e
	if gain < 0 {
		gain = 0
	}

	w := e + ReviveGain*gain
	if math.IsNaN(w) || math.IsInf(w, 0) {
		return Boost{}, false
	}

	return Boost{
		UnitID:        c.UnitID,
		Weight:        w,
		LastTouchedAt: now,
	}, true
}
