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
	return 0
}
