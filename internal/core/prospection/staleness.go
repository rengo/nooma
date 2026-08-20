package prospection

import "time"

// TriggerStalenessHours is ADR-0009's catch-up threshold for a time_based
// trigger (doc 02 §13): overdue by more than this many hours, measured
// from the first instant the trigger could actually have been delivered,
// and the trigger expires rather than fires. Untyped, deliberately not a
// time.Duration — test/conformance/calibration_doc_test.go's Default-cell
// regex reads the leading bare number off doc 02 §13's row, and a
// Duration would hold nanoseconds, not the hour (QuietHoursStartHour's
// own doc comment gives the same reason).
const TriggerStalenessHours = 6

// Verdict is the whole delivery gate's output for one pending trigger or
// timer. Core states only this neutral vocabulary — never a schema
// status. brain maps VerdictStale to "expired" on a trigger (I15) and to
// "cancelled" on a timer (doc 02 §8's own pending|fired|cancelled); that
// mapping decision belongs to brain, not to this package (design §3.3).
type Verdict string

const (
	// VerdictPending means fire_at is still in the future — no delivery
	// decision has been made yet.
	VerdictPending Verdict = "pending"
	// VerdictDefer means the item is deliverable later today, once quiet
	// hours end. Triggers only — a timer is never deferred (spec R1.2).
	VerdictDefer Verdict = "defer"
	// VerdictStale means the item is past its staleness window.
	VerdictStale Verdict = "stale"
	// VerdictDeliver means the item should be delivered now.
	VerdictDeliver Verdict = "deliver"
)

// TriggerVerdict decides deliver vs. stale vs. defer vs. pending for one
// armed time_based trigger (spec R1.1).
//
// TODO(PR 2, task 2.2): implement via DeliverableFrom-measured overdue.
func TriggerVerdict(fireAt, now time.Time) Verdict {
	return ""
}
