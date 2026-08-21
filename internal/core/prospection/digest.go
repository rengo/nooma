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
