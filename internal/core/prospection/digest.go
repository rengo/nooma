package prospection

import (
	"time"

	"github.com/rengo/nooma/internal/core/focus"
)

// DigestHour is the local hour at which the daily digest becomes due
// (owner ruling 2, design §3.5). Untyped and not a time.Duration, for the
// reason QuietHoursStartHour's own comment gives.
//
// It equals QuietHoursEndHour, and it is a second constant with a second
// §13 row rather than a reference to that one, following
// focus.UrgencyLeadDays' precedent: one is a delivery window's edge and the
// other is a cadence. Collapsing two knobs because they agree today is how
// a calibration table stops being tunable. TestDigestHourIsNotBeforeQuietHoursEnd
// asserts the relation that actually matters between them.
const DigestHour = 7

// DigestDue reports whether a digest is owed at now, given the instant the
// last one was delivered (nil when none ever was) — owner ruling 2, design
// §3.5.
//
// Due iff now's local hour is at or past DigestHour AND no digest has been
// delivered since today's DigestHour instant. Written that way so downtime
// is a normal case rather than a backlog: a vault that was off for three
// days owes exactly one digest, because the question asked is "has today's
// digest gone out", never "how many are outstanding".
//
// Built with time.Date in now's own location, so the day boundary is the
// user's local one — the zone travels inside the instant, as everywhere
// else in this package.
func DigestDue(lastDigestAt *time.Time, now time.Time) bool {
	if now.Hour() < DigestHour {
		return false
	}
	y, m, d := now.Date()
	dueAt := time.Date(y, m, d, DigestHour, 0, 0, 0, now.Location())
	return lastDigestAt == nil || lastDigestAt.Before(dueAt)
}

// LowEnergyMax is the level below which energy reads as low (design §3.5).
// Chosen, not derived: energy is declared on [0,1] (doc 02 §10) with no
// calibration data behind it, and the midpoint is the only point on such a
// scale that is not an invention — the same reading that put
// weight_threshold at 0.5.
const LowEnergyMax = 0.5

// EnergyReadingMaxAgeHours is how old a reading may be and still count as
// "recent" (doc 02 §7). Derived from the cadence: the digest is once daily
// (owner ruling 2), so its input must be no older than one digest cycle — a
// reading from two digests ago would hold items back on a day it never
// observed. It equals incomplete_expiry_hours and catch_up_staleness_hours
// by coincidence, not by relation, and no test ties them.
const EnergyReadingMaxAgeHours = 24

// EnergyReading is one current_state row as the care gate sees it. Both
// fields are required because doc 02 §7's gate is "low (recent reading)" —
// two conditions, not one.
type EnergyReading struct {
	Level      float64
	RecordedAt time.Time
}

// LowEnergy reports doc 02 §7's own two-part condition.
//
// A nil reading is not low: no observation is not an observation of
// depletion. That direction is deliberate — this gate suppresses delivery,
// so silence must never be read as consent to suppress.
//
// The level comparison is strict for the same reason: the burden of proof
// is on "low", and exactly the midpoint is not low.
func LowEnergy(r *EnergyReading, now time.Time) bool {
	if r == nil {
		return false
	}
	if r.Level >= LowEnergyMax {
		return false
	}
	return now.Sub(r.RecordedAt) <= EnergyReadingMaxAgeHours*time.Hour
}

// LowEnergyDigestSize is how many items a low-energy digest carries. Half
// the human attention bound, by the same reading that puts LowEnergyMax at
// the midpoint of the [0,1] energy scale.
//
// Written as the expression rather than as 3, so the derivation lives in
// the code instead of in a comment about the code: a recalibration of
// focus_size carries it. §13 documents the resulting value, which is what
// the calibration gate compares.
const LowEnergyDigestSize = focus.DefaultSize / 2

// MaxDigestDeferrals is how many consecutive digests may hold one item back
// before it is carried regardless of rank. Owner ruling 2 fixes the unit:
// one deferral is one day.
//
// Chosen inside a derived band, and the weakest derivation in this package
// (owner-review R2). The band: more than 1, or the care gate is a one-day
// delay wearing the word anti-starvation; and strictly less than
// consolidation.LoadCooldownDays (7), because the load watcher will not
// re-open a state hypothesis for seven days after one resolves — an item
// suppressible for that whole window could be silenced across exactly the
// period in which the brain has stopped looking.
const MaxDigestDeferrals = 3

// DigestItem is one pending trigger as the digest gate sees it.
//
// Candidate is nil for a trigger with no source unit — triggers.unit_id is
// nullable, so a pattern watcher has none. Carry handles that without a nil
// check of its own; see its own comment for why the arithmetic already
// answers it.
type DigestItem struct {
	ID        string
	Candidate *focus.Candidate
	Deferrals int
}

// Carry splits pending items into what this digest delivers and what it
// holds back (spec R4.2 and R4.3 as one function — Finding F4; design §3.5).
//
// The rule, in order:
//
//  1. Not low energy: carry everything. The care gate is the only thing that
//     ever holds an item back.
//  2. An item already deferred MaxDigestDeferrals times is carried
//     REGARDLESS of rank, and in addition to the truncation below — never
//     as one of its slots. That is what "regardless" has to mean for
//     anti-starvation to bound anything: if force-carried items competed for
//     the same slots, a low-ranked item could be starved by fresher
//     high-ranked ones forever, which is the failure the rule exists to
//     prevent.
//  3. Everything else is ranked by focus.Rank and the top
//     LowEnergyDigestSize are carried.
//
// A nil Candidate enters focus.Rank as the zero focus.Candidate carrying
// only its own ID, and needs no special case: every term of focus.Priority
// is multiplicative in effective weight, which is 0 for a zero candidate, so
// its score is exactly 0.0 and Rank's own tie-break orders it last,
// deterministically, by ID. A "still on this goal, or shall we let it rest?"
// nudge is therefore the first thing a depleted user stops being asked —
// which is what doc 02 §7 asks for, reached with no special case and no
// invented number.
//
// Carry takes no position on whether an empty result is delivered: it
// returns two slices and says nothing about publishing. Whether a digest
// carrying nothing is sent at all is m3d's decision (design §11 Q1).
func Carry(items []DigestItem, adjacency map[string]float64, lowEnergy bool, now time.Time) (carry, held []DigestItem) {
	return nil, nil
}
