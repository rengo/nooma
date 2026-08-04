package weight

import (
	"math"
	"time"
)

// Effective computes doc 02 §2's Ebbinghaus decay curve exactly:
//
//	effective_weight = weight * exp(-decay_rate * Δt)
//
// Δt is now.Sub(lastTouchedAt) expressed in fractional days, not truncated
// to whole days (a unit touched 12 hours ago has Δt = 0.5), computed as
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
// zero and Effective returns weight undecayed.
//
// decayRate and weight are sanitized the same way, and for the same
// reason: they arrive from an LLM's JSON via classify's decode, which
// validates only that the value is a number — no sign, no range — and the
// schema declares neither column CHECK-constrained, so core cannot vouch
// for either any more than it can vouch for now. A negative decayRate is
// treated as 0 (no decay, mirroring the Δt clamp); a negative weight is
// treated as 0 (weight is how much something matters, not a signed
// magnitude, so a negative value has no meaning in this model).
//
// Effective(w, λ, lt, now) <= max(w, 0) holds for every input, after this
// sanitization, including λ <= 0 and every ordering of lt and now: this is
// a postcondition over the sanitized inputs, not a claim about whatever
// was passed in (spec R1.2, design D1).
func Effective(weight, decayRate float64, lastTouchedAt, now time.Time) float64 {
	if decayRate < 0 {
		decayRate = 0
	}
	if weight < 0 {
		weight = 0
	}
	deltaDays := now.Sub(lastTouchedAt).Hours() / 24
	if deltaDays < 0 {
		deltaDays = 0
	}
	return weight * math.Exp(-decayRate*deltaDays)
}
