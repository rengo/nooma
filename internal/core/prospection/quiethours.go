package prospection

import "time"

// QuietHoursStartHour is the local hour at which quiet hours open,
// inclusive. Untyped, deliberately not a time.Duration —
// test/conformance/calibration_doc_test.go's Default-cell regex reads the
// leading bare number off doc 02 §13's row, and a Duration would hold
// nanoseconds, not the hour (consolidation.CatchUpStalenessHours's own
// doc comment gives the same reason).
const QuietHoursStartHour = 0

// QuietHoursEndHour is the local hour at which quiet hours close,
// exclusive.
const QuietHoursEndHour = 7

// InQuietHours reports whether now's local wall clock — read in
// now.Location(), never a configured or global zone — falls in
// [QuietHoursStartHour, QuietHoursEndHour) (spec R2.1).
//
// The zone is not a parameter and not a config key: now.Hour() reads the
// wall clock in now.Location(), and the location travels inside the
// instant ports.Clock.Now() produced (docs/02-cognitive-core.md:600-610;
// the same mechanism classify/prompt.go documents for capture).
func InQuietHours(now time.Time) bool {
	return DefaultQuietHours().Contains(now)
}

// DeliverableFrom returns the first instant at or after t at which a
// trigger may actually be delivered: t itself when t is outside quiet
// hours, and that day's QuietHoursEndHour otherwise (design §3.1/§3.3).
//
// Built with time.Date rather than AddDate, the house pattern
// (consolidation.NextDailyRun's own discipline): the shift never needs to
// cross a calendar day, so no loop is needed here, but the same
// out-of-range-field construction keeps the wall clock's own
// normalization in one place.
func DeliverableFrom(t time.Time) time.Time {
	return DefaultQuietHours().DeliverableFrom(t)
}

// QuietHours is one user's night: [QuietHoursStartHour, End) on the local
// wall clock, End being their quiet end (ADR-0030 point 5). The package
// functions InQuietHours, DeliverableFrom, TriggerVerdict,
// EventReminderVerdict and DigestDue are DefaultQuietHours'; the due scan
// uses the user's own.
type QuietHours struct{ End TimeOfDay }

// DefaultQuietHours is the night nobody chose: it ends at QuietHoursEndHour.
func DefaultQuietHours() QuietHours {
	return QuietHours{End: TimeOfDay{Hour: QuietHoursEndHour}}
}

// Contains reports whether t's wall clock, in t's own location, is in the
// night — InQuietHours' rule with the user's end.
func (q QuietHours) Contains(t time.Time) bool {
	minute := t.Hour()*60 + t.Minute()
	return minute >= QuietHoursStartHour*60 && minute < q.End.Hour*60+q.End.Minute
}

// DeliverableFrom is t outside the night, and that day's quiet end inside
// it — DeliverableFrom's rule with the user's end.
func (q QuietHours) DeliverableFrom(t time.Time) time.Time {
	if !q.Contains(t) {
		return t
	}
	return q.End.on(t, 0, t.Location())
}
