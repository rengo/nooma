// Package consolidation (this file) holds the scheduler's pure decisions —
// design.md §4: nothing under internal/scheduler decides anything itself,
// it only asks these three functions and obeys.
package consolidation

import "time"

// CatchUpStalenessHours is ADR-0009's boot catch-up threshold (doc 02
// §13): "if config.consolidation_last_run_at is more than 24 h old,
// consolidation is [caught up]". Untyped, deliberately not a
// time.Duration — test/conformance/calibration_doc_test.go's Default-cell
// regex reads the leading bare number off §13's row, and a Duration would
// hold 86400000000000, not 24 (design §3.2, D2).
const CatchUpStalenessHours = 24

// CatchUpDue answers ADR-0009's boot catch-up gate for one lastRunAt read
// against one now (spec R2.1).
//
// A nil lastRunAt is always due — a vault with no recorded run has never
// consolidated, which is at least as stale as any staleness threshold.
//
// The comparison is strict, mirroring ADR-0009's own "more than
// stalenessHours" wording and doc 02 §6's strict-comparison convention
// elsewhere in this package (ExpireIncomplete): elapsed hours must exceed
// stalenessHours, not merely reach it, so exactly stalenessHours old is
// not yet due.
//
// A future lastRunAt (elapsed < 0, e.g. a clock set backward since the
// last run) is never due. This is a signed comparison, not a clamp —
// unlike ExpireIncomplete's own clamp-at-zero, no repair is invented here
// for a lastRunAt ahead of now; ADR-0009 is silent on that case and this
// function does not extend it.
func CatchUpDue(lastRunAt *time.Time, now time.Time, stalenessHours int) bool {
	if lastRunAt == nil {
		return true
	}
	elapsed := now.Sub(*lastRunAt).Hours()
	return elapsed > float64(stalenessHours)
}
