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

// TimerStalenessHours is ADR-0009's catch-up threshold for an ephemeral
// timer — tighter than a trigger's because a timer's value is purely
// temporal (doc 02 §8) and decays faster than a memory-backed nudge.
const TimerStalenessHours = 3

// DelayCaveatMinutes is the overdue amount at which a delivered-but-late
// item must say so (spec R1.3, ADR-0009, design §3.3): below it, the
// lateness is the scheduler's own granularity and is not a fact about the
// user's world; at or above it, ADR-0009's rule against out-of-context
// nudges applies. Three shipped proactive_check ticks
// (*/5 * * * *, docs/01-architecture.md:227) — the derivation, and its
// docs/config assertion deferred to m3d #1, are recorded in design §3.3
// (owner-review R4).
const DelayCaveatMinutes = 15

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

// verdict is the one decision shared by TriggerVerdict and TimerVerdict
// (design §3.3), evaluated in this fixed order:
//
//  1. now.Before(fireAt) → Pending. Not yet due.
//  2. deferInQuietHours && InQuietHours(now) → Defer. Quiet hours are
//     evaluated before staleness, so an item is never declared stale
//     during a window in which it was refused delivery. Trigger-only —
//     TimerVerdict passes deferInQuietHours = false and never reaches
//     this branch, which is how the timer stays the one push exception
//     to quiet hours (design §3.2).
//  3. now.Sub(from) > stalenessHours → Stale. Strict, matching
//     consolidation.CatchUpDue's own "more than" convention and
//     ADR-0009's wording: exactly stalenessHours overdue still delivers.
//  4. → Deliver.
//
// deferInQuietHours is the one parameter design §3.3's own four-argument
// sketch does not name explicitly, alongside its "trigger only" prose for
// step 2 — a shared helper cannot honor that exception from (fireAt,
// from, stalenessHours, now) alone, since a timer whose fireAt equals its
// own from is indistinguishable from a trigger armed outside quiet hours
// on those four values. Behavior matches design's own worked table and
// scenarios exactly; only this private, unexported parameter is added to
// make the "trigger only" sentence executable.
func verdict(fireAt, from time.Time, stalenessHours int, now time.Time, deferInQuietHours bool) Verdict {
	if now.Before(fireAt) {
		return VerdictPending
	}
	if deferInQuietHours && InQuietHours(now) {
		return VerdictDefer
	}
	overdue := now.Sub(from)
	if overdue > time.Duration(stalenessHours)*time.Hour {
		return VerdictStale
	}
	return VerdictDeliver
}

// TriggerVerdict decides deliver vs. stale vs. defer vs. pending for one
// armed time_based trigger (spec R1.1). overdue is measured from
// DeliverableFrom(fireAt) — the first instant the trigger could actually
// have been delivered — not from fireAt itself: the quiet-hours window is
// longer than the trigger staleness threshold, so measuring from fireAt
// directly would expire every trigger armed inside quiet hours before the
// user woke, every night (design §3.3, Finding F1).
func TriggerVerdict(fireAt, now time.Time) Verdict {
	return verdict(fireAt, DeliverableFrom(fireAt), TriggerStalenessHours, now, true)
}

// TimerVerdict mirrors TriggerVerdict with the timer's own threshold and
// without the quiet-hours gate (spec R1.2): a timer's instant was set by
// an explicit user instruction at capture, so it is the one exception to
// quiet hours and never accrues policy delay — overdue is measured from
// fireAt directly, with no DeliverableFrom shift.
func TimerVerdict(fireAt, now time.Time) Verdict {
	return verdict(fireAt, fireAt, TimerStalenessHours, now, false)
}

// DelayCaveat reports whether a delivered item's overdue duration is
// large enough that the fire-time rendering (m3d's job) must mention the
// delay explicitly (spec R1.3, ADR-0009). The comparison is inclusive —
// exactly DelayCaveatMinutes late already caveats — deliberately the
// opposite of verdict's own strict staleness comparison above: there the
// inclusive side is destructive (expiring is unrecoverable, delivering
// late is not), while here a caveat that was not strictly necessary
// costs one clause of politeness and a missing one on a genuinely late
// delivery is the exact failure ADR-0009 exists to prevent, so the
// boundary belongs on the cheap side (design §3.3, Finding F6). It also
// matches doc 02 §7's own permissive-side convention,
// interrupt_level >= 0.70.
//
// DelayCaveat reports the fact; the render layer picks the words (doc 02
// §7's own division of labour).
func DelayCaveat(overdue time.Duration) bool {
	return overdue >= DelayCaveatMinutes*time.Minute
}
