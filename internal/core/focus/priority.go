package focus

import "time"

// UrgencyLeadDays is the lead window UrgencyRamp ramps across: a unit due
// this many days out or further scores no urgency bonus at all. Default 7
// (doc 02 §13). This is a separate constant from prospection's "Event lead
// time" (also 7 days) even though the two start equal — one is the
// notification horizon, this is the ranking's, and collapsing two knobs
// because they happen to agree today is how a calibration table becomes
// un-tunable (spec R3.3).
const UrgencyLeadDays = 7

// UrgencyMax is the priority factor a unit gets at or past its due_at —
// UrgencyRamp's ceiling. Default 3.0 (doc 02 §13): a due-today unit at the
// archive floor (weight_threshold = 0.5) still outranks a healthy
// non-urgent unit at classify's base weight (0.5 * 3 = 1.5 > 1.0), which is
// the behaviour the value was chosen for, not derived from (design §3.1).
const UrgencyMax = 3.0

// UrgencyRamp computes doc 02 §3's temporal_urgency(due_at) term: a linear
// ramp inside the UrgencyLeadDays window, clamped to 1 once a unit is
// overdue and never growing past it (spec R3.3, design §3.1).
//
//	d = dueAt.Sub(now).Hours() / 24
//	UrgencyRamp = clamp((UrgencyLeadDays - d) / UrgencyLeadDays, 0, 1)
//
// dueAt == nil returns exactly 0, by definition, not the d -> infinity
// limit: units with no deadline are the majority and Priority must reduce
// to e * (1 + nudges) for them with no floating-point residue. nil is not
// the zero time.Time (I18) — a *time.Time distinguishes "no deadline" from
// "due at the epoch" the way a bare time.Time cannot.
//
// Without the overdue clamp a task overdue by three years would dominate
// the focus permanently and the focus would stop being a view of the
// present; with it, "overdue" is a single state rather than a growing one,
// and what removes an overdue task from the focus is decay or the user,
// not arithmetic.
func UrgencyRamp(dueAt *time.Time, now time.Time) float64 {
	if dueAt == nil {
		return 0
	}
	d := dueAt.Sub(now).Hours() / 24
	return clamp((UrgencyLeadDays-d)/UrgencyLeadDays, 0, 1)
}

// clamp restricts v to [lo, hi]. Every elapsed-time computation in this
// package saturates rather than inverting or overshooting — the same rule
// design D1 states once for weight.Effective's negative-Δt clamp and
// design §3.1 restates here for AgeRamp and UrgencyRamp.
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// AgeWeight is the maximum relative lift AgeRamp can contribute to
// Priority — at most one fifth, so age breaks close contests and never
// wins them (spec R3.5 P1/P2). Default 0.20 (doc 02 §13). Deliberately left
// alone by owner ruling 10, which moved AgeHorizonDays instead precisely
// because this constant sets P1's ceiling and P2's overturnable deficit —
// moving it would change how much power age has, not just when it arrives.
const AgeWeight = 0.20

// AgeHorizonDays is the age, in days, at which AgeRamp saturates at 1.
// Default 15 (owner ruling 10; it was 30 under ruling 9, and 30 was
// rejected because at doc 02 §13's base decay rate (0.01/day) it left an
// untouched unit's priority strictly decreasing across the entire horizon
// — the anti-starvation promise it was chosen under was false as
// specified. 15 puts the break-even decay rate (AgeWeight/AgeHorizonDays =
// 0.01333/day) above the base, so the rise is genuine (spec R3.4, R3.5 P4).
const AgeHorizonDays = 15

// AgeRamp computes doc 02 §3's age term: ANTI-STARVATION, rising 0 -> 1
// over AgeHorizonDays and never past it. Older ranks higher (spec R3.4,
// design §3.1, owner rulings 9 and 10).
//
//	ageDays = now.Sub(createdAt).Hours() / 24
//	AgeRamp = clamp(ageDays / AgeHorizonDays, 0, 1)
//
// The term reads created_at, never last_touched_at: last_touched_at is
// reset by use and created_at never is, so their difference is exactly
// "has this been revisited since capture". Reading last_touched_at here
// would count decay's own signal a second time under a different name.
//
// createdAt after now — clock skew, a backdated import — clamps at 0, the
// same negative-elapsed-time rule D1 states for Effective's Δt: a unit
// that does not yet exist has waited no time.
//
// Every fixture this package's tests express AgeRamp's boundaries as a
// multiple of AgeHorizonDays, never as a literal day count, so a future
// recalibration of AgeHorizonDays needs no fixture edit (nooma-core hard
// rule 4's discipline, paid off in a test rather than in production code).
func AgeRamp(createdAt, now time.Time) float64 {
	ageDays := now.Sub(createdAt).Hours() / 24
	return clamp(ageDays/AgeHorizonDays, 0, 1)
}
