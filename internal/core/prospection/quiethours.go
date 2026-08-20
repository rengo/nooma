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
    hour := now.Hour()
    return hour >= QuietHoursStartHour && hour < QuietHoursEndHour
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
    if !InQuietHours(t) {
        return t
    }
    y, m, d := t.Date()
    return time.Date(y, m, d, QuietHoursEndHour, 0, 0, 0, t.Location())
}
