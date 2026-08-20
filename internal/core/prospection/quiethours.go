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
// TODO(PR 1, task 1.2): implement via now.Hour() in now.Location().
func InQuietHours(now time.Time) bool {
    return false
}
