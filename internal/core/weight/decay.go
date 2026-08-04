package weight

import (
	"math"
	"time"
)

// Effective computes doc 02 §2's Ebbinghaus decay curve exactly:
//
//	effective_weight = weight * exp(-decay_rate * Δt)
//
// Δt is now.Sub(lastTouchedAt) expressed in whole days, fractional rather
// than truncated (a unit touched 12 hours ago has Δt = 0.5), computed as
// now.Sub(lastTouchedAt).Hours() / 24 rather than a calendar-day count —
// the latter would make the curve a step function and would depend on a
// timezone this package is forbidden to know (design D1).
//
// weight and lastTouchedAt are taken by value and Effective returns a bare
// float64: it has no pointer or interface parameter capable of writing
// back to a caller's unit, and no exported function in this package
// returns a unit.Unit, *unit.Unit, or []unit.Unit (spec R1.3, design D9 —
// I05's pure half). now and lastTouchedAt are both plain time.Time
// parameters; Effective calls neither time.Now nor any other clock source.
//
// When now is before lastTouchedAt — clock skew across a restart, a
// backdated import, a fake clock wound backwards in a test — Δt clamps at
// zero and Effective returns weight undecayed. Effective(w, λ, lt, now) <=
// w holds for every input, including λ = 0 and every ordering of lt and
// now: this is a postcondition, not a comment (spec R1.2, design D1).
func Effective(weight, decayRate float64, lastTouchedAt, now time.Time) float64 {
	deltaDays := now.Sub(lastTouchedAt).Hours() / 24
	if deltaDays < 0 {
		deltaDays = 0
	}
	return weight * math.Exp(-decayRate*deltaDays)
}
