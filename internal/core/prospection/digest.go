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

// DigestDue reports whether a digest is owed at now, given the last one.
func DigestDue(lastDigestAt *time.Time, now time.Time) bool { return false }
