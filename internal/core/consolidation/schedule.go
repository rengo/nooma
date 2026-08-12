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

// ResolveConsolidationEnabled falls back to enabled for an absent
// configured value (spec R1.2), mirroring migration 0002:65's own
// consolidation_enabled DEFAULT 1 — a vault that has never set the column
// reads nil, and nil must read the same as the schema's own default, not
// as "disabled".
func ResolveConsolidationEnabled(configured *bool) bool {
	if configured == nil {
		return true
	}
	return *configured
}

// NextDailyRun returns the next instant, strictly after after, at which
// the clock reads hour:00:00 in after's own location (spec R1.1's own
// cron loop calls this with the local clock; design §4). Strictly after,
// not at-or-after: an after that already reads exactly hour:00:00.000
// returns tomorrow's occurrence, never itself — a cron loop that just
// fired must schedule its next fire in the future, not fire again
// immediately.
//
// A non-existent or ambiguous local wall-clock time (a DST transition)
// resolves however time.Date's own normalization resolves it — this
// function adds no DST-specific handling.
//
// Tomorrow's candidate is built from hour again rather than advanced from
// today's, and that is the whole of the fix Judgment Day forced here. On a
// spring-forward day the requested hour may not exist, and time.Date
// normalizes it forward: ask for 02:00 where 02:00 was skipped and you get
// 03:00. Advancing THAT with AddDate carries 03:00 into the next day —
// AddDate reuses the receiver's own Clock(), not the argument this function
// was called with — and the next day has no gap, so the caller silently
// gets an hour it never asked for, once a year, in whichever zone puts its
// transition on the configured hour. Rebuilding from (day+1, hour) instead
// keeps each day's normalization a question about that day alone.
func NextDailyRun(after time.Time, hour int) time.Time {
	candidate := time.Date(after.Year(), after.Month(), after.Day(), hour, 0, 0, 0, after.Location())
	if candidate.After(after) {
		return candidate
	}

	tomorrow := after.AddDate(0, 0, 1)
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), hour, 0, 0, 0, after.Location())
}
