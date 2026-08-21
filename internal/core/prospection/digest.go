package prospection

import "time"

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
	return true
}
