package prospection

import (
	"testing"
	"time"
)

// TestTriggerVerdict_WorkedBoundaryTable proves design §3.3's own worked
// boundary table: overdue is measured from DeliverableFrom(fireAt), not
// from fireAt directly (Finding F1), and the comparison against
// TriggerStalenessHours is strict.
func TestTriggerVerdict_WorkedBoundaryTable(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	day := func(hour, minute int) time.Time {
		return time.Date(2026, 8, 7, hour, minute, 0, 0, loc)
	}
	nextDay := func(hour, minute int) time.Time {
		return time.Date(2026, 8, 8, hour, minute, 0, 0, loc)
	}

	tests := []struct {
		name   string
		fireAt time.Time
		now    time.Time
		want   Verdict
	}{
		{
			name:   "00:30 armed, 03:00 pass — Defer, quiet hours evaluated first",
			fireAt: day(0, 30),
			now:    day(3, 0),
			want:   VerdictDefer,
		},
		{
			name:   "00:30 armed, 07:00 pass — Deliver, overdue 0",
			fireAt: day(0, 30),
			now:    day(7, 0),
			want:   VerdictDeliver,
		},
		{
			name:   "00:00 armed (the worst case the naive formula killed), 07:00 pass — Deliver",
			fireAt: day(0, 0),
			now:    day(7, 0),
			want:   VerdictDeliver,
		},
		{
			name:   "20:00 armed, next day 10:00 pass (downtime) — Stale, 14h overdue",
			fireAt: day(20, 0),
			now:    nextDay(10, 0),
			want:   VerdictStale,
		},
		{
			name:   "23:30 armed, next day 07:00 pass (downtime) — Stale, 7.5h overdue",
			fireAt: day(23, 30),
			now:    nextDay(7, 0),
			want:   VerdictStale,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TriggerVerdict(tt.fireAt, tt.now); got != tt.want {
				t.Errorf("TriggerVerdict(%v, %v) = %v, want %v", tt.fireAt, tt.now, got, tt.want)
			}
		})
	}
}

// TestTriggerVerdict_ArmedInsideQuietHoursDefersThenDelivers proves the
// worked table's own last row as a two-step sequence: a trigger armed
// inside quiet hours defers while they are open, and delivers with zero
// overdue the instant they end.
func TestTriggerVerdict_ArmedInsideQuietHoursDefersThenDelivers(t *testing.T) {
	loc := time.UTC
	fireAt := time.Date(2026, 8, 7, 6, 0, 0, 0, loc)

	firstPass := time.Date(2026, 8, 7, 6, 5, 0, 0, loc)
	if got := TriggerVerdict(fireAt, firstPass); got != VerdictDefer {
		t.Fatalf("TriggerVerdict(%v, %v) = %v, want VerdictDefer", fireAt, firstPass, got)
	}

	secondPass := time.Date(2026, 8, 7, 7, 0, 0, 0, loc)
	if got := TriggerVerdict(fireAt, secondPass); got != VerdictDeliver {
		t.Fatalf("TriggerVerdict(%v, %v) = %v, want VerdictDeliver — zero overdue, none of the "+
			"elapsed wall clock was deliverable", fireAt, secondPass, got)
	}
}

// TestTriggerVerdict_NotYetDueIsPending proves a fireAt still in the
// future never produces a delivery decision.
func TestTriggerVerdict_NotYetDueIsPending(t *testing.T) {
	loc := time.UTC
	fireAt := time.Date(2026, 8, 7, 12, 0, 0, 0, loc)
	now := time.Date(2026, 8, 7, 11, 59, 59, 0, loc)

	if got := TriggerVerdict(fireAt, now); got != VerdictPending {
		t.Errorf("TriggerVerdict(%v, %v) = %v, want VerdictPending", fireAt, now, got)
	}
}

// TestTriggerVerdict_StalenessBoundaryIsStrict proves the deliver side is
// inclusive at exactly TriggerStalenessHours and the stale side begins
// one nanosecond later — matching consolidation.CatchUpDue's own "more
// than" convention.
func TestTriggerVerdict_StalenessBoundaryIsStrict(t *testing.T) {
	loc := time.UTC
	fireAt := time.Date(2026, 8, 7, 12, 0, 0, 0, loc) // outside quiet hours, from == fireAt

	atThreshold := fireAt.Add(time.Duration(TriggerStalenessHours) * time.Hour)
	if got := TriggerVerdict(fireAt, atThreshold); got != VerdictDeliver {
		t.Errorf("TriggerVerdict at exactly TriggerStalenessHours overdue = %v, want VerdictDeliver "+
			"(inclusive deliver side)", got)
	}

	pastThreshold := atThreshold.Add(time.Nanosecond)
	if got := TriggerVerdict(fireAt, pastThreshold); got != VerdictStale {
		t.Errorf("TriggerVerdict one nanosecond past TriggerStalenessHours = %v, want VerdictStale", got)
	}
}

// TestTriggerVerdict_NeverStaleAtQuietHoursEnd proves design §3.3's own
// property, swept across the whole quiet-hours window rather than
// sampled at a few boundaries: no trigger armed anywhere inside
// [QuietHoursStartHour, QuietHoursEndHour) is ever Stale the instant
// quiet hours end. This is the property DeliverableFrom exists for
// (Finding F1) — it must survive any future recalibration of either
// threshold.
func TestTriggerVerdict_NeverStaleAtQuietHoursEnd(t *testing.T) {
	loc := time.FixedZone("UTC+1", 1*60*60)
	day := time.Date(2026, 8, 7, 0, 0, 0, 0, loc)
	endOfQuietHours := time.Date(2026, 8, 7, QuietHoursEndHour, 0, 0, 0, loc)

	minutesInWindow := (QuietHoursEndHour - QuietHoursStartHour) * 60
	for minute := 0; minute < minutesInWindow; minute++ {
		fireAt := day.Add(time.Duration(minute) * time.Minute)
		if got := TriggerVerdict(fireAt, endOfQuietHours); got == VerdictStale {
			t.Fatalf("TriggerVerdict(%v, %v) = VerdictStale, want anything else — a trigger armed "+
				"inside quiet hours must never be stale the instant they end", fireAt, endOfQuietHours)
		}
	}
}

// TestTimerVerdict_NotYetDueIsPending mirrors TriggerVerdict's own
// not-yet-due case.
func TestTimerVerdict_NotYetDueIsPending(t *testing.T) {
	loc := time.UTC
	fireAt := time.Date(2026, 8, 7, 12, 0, 0, 0, loc)
	now := time.Date(2026, 8, 7, 11, 0, 0, 0, loc)

	if got := TimerVerdict(fireAt, now); got != VerdictPending {
		t.Errorf("TimerVerdict(%v, %v) = %v, want VerdictPending", fireAt, now, got)
	}
}

// TestTimerVerdict_OverdueLessThanThresholdDelivers proves spec R1.2's
// own scenario: a timer 2 hours overdue against a 3-hour threshold still
// delivers.
func TestTimerVerdict_OverdueLessThanThresholdDelivers(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, loc)
	fireAt := now.Add(-2 * time.Hour)

	if got := TimerVerdict(fireAt, now); got != VerdictDeliver {
		t.Errorf("TimerVerdict(%v, %v) = %v, want VerdictDeliver", fireAt, now, got)
	}
}

// TestTimerVerdict_DeliversInsideQuietHours proves spec R1.2's exception:
// a timer due while now is inside quiet hours still delivers — the
// timer, not a level, not a threshold, is the one exception (design
// §3.2), and this is the direct proof that InQuietHours never gates it.
func TestTimerVerdict_DeliversInsideQuietHours(t *testing.T) {
	loc := time.UTC
	fireAt := time.Date(2026, 8, 7, 3, 0, 0, 0, loc)
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, loc)

	if !InQuietHours(now) {
		t.Fatalf("test fixture is broken: %v must be inside quiet hours", now)
	}
	if got := TimerVerdict(fireAt, now); got != VerdictDeliver {
		t.Errorf("TimerVerdict(%v, %v) = %v, want VerdictDeliver — the timer is the one push "+
			"exception to quiet hours, never deferred", fireAt, now, got)
	}
}

// TestTriggerVerdict_ArmedInsideQuietHoursStillGoesStale closes the half
// of DeliverableFrom's argument the worked boundary table does not reach.
// That table's two Stale rows are both armed OUTSIDE quiet hours (20:00
// and 23:30), and its three quiet-hours rows all end in Defer or in
// Deliver at zero overdue — so nothing in it proves that the shift
// DeliverableFrom performs moves the starting line without also granting
// immunity.
//
// A trigger armed at 00:30 is deliverable from 07:00. Seven hours after
// that, past TriggerStalenessHours, it must be Stale exactly like any
// other trigger the system was down for: quiet hours excuse the policy
// window, and nothing after it.
func TestTriggerVerdict_ArmedInsideQuietHoursStillGoesStale(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	fireAt := time.Date(2026, 8, 7, 0, 30, 0, 0, loc)
	now := time.Date(2026, 8, 7, 14, 0, 0, 0, loc)

	if !InQuietHours(fireAt) {
		t.Fatalf("test fixture is broken: fireAt %v must be inside quiet hours", fireAt)
	}
	if InQuietHours(now) {
		t.Fatalf("test fixture is broken: now %v must be outside quiet hours, so this test "+
			"is about staleness and not about the Defer gate", now)
	}
	if overdue := now.Sub(DeliverableFrom(fireAt)); overdue <= time.Duration(TriggerStalenessHours)*time.Hour {
		t.Fatalf("test fixture is broken: overdue from the first deliverable instant is %v, "+
			"which does not exceed TriggerStalenessHours (%dh) — this case would prove nothing",
			overdue, TriggerStalenessHours)
	}

	if got := TriggerVerdict(fireAt, now); got != VerdictStale {
		t.Errorf("TriggerVerdict(%v, %v) = %v, want VerdictStale — DeliverableFrom moves where "+
			"overdue starts counting, it does not exempt a trigger from ever expiring", fireAt, now, got)
	}
}

// TestTimerVerdict_StaleInsideQuietHours proves the timer's exception is
// scoped to the quiet-hours gate alone. TestTimerVerdict_DeliversInsideQuietHours
// covers a timer due at that very instant (zero overdue); this covers the
// other side, where the timer is both inside quiet hours and genuinely
// past TimerStalenessHours.
//
// Being exempt from being deferred is not being exempt from going stale.
// If these two ever fused, a timer armed before a long downtime would
// fire in the middle of the night, hours late — which is the pair of
// failures ADR-0009 names, arriving together.
func TestTimerVerdict_StaleInsideQuietHours(t *testing.T) {
	loc := time.UTC
	fireAt := time.Date(2026, 8, 7, 0, 0, 0, 0, loc)
	now := time.Date(2026, 8, 7, 4, 0, 0, 0, loc)

	if !InQuietHours(now) {
		t.Fatalf("test fixture is broken: now %v must be inside quiet hours", now)
	}
	if overdue := now.Sub(fireAt); overdue <= time.Duration(TimerStalenessHours)*time.Hour {
		t.Fatalf("test fixture is broken: overdue is %v, which does not exceed "+
			"TimerStalenessHours (%dh)", overdue, TimerStalenessHours)
	}

	if got := TimerVerdict(fireAt, now); got != VerdictStale {
		t.Errorf("TimerVerdict(%v, %v) = %v, want VerdictStale — a timer is exempt from the "+
			"quiet-hours gate, not from its own staleness window", fireAt, now, got)
	}
}

// TestTimerVerdict_StalenessBoundaryIsStrict mirrors TriggerVerdict's own
// boundary with the timer's own threshold.
func TestTimerVerdict_StalenessBoundaryIsStrict(t *testing.T) {
	loc := time.UTC
	fireAt := time.Date(2026, 8, 7, 12, 0, 0, 0, loc)

	atThreshold := fireAt.Add(time.Duration(TimerStalenessHours) * time.Hour)
	if got := TimerVerdict(fireAt, atThreshold); got != VerdictDeliver {
		t.Errorf("TimerVerdict at exactly TimerStalenessHours overdue = %v, want VerdictDeliver", got)
	}

	pastThreshold := atThreshold.Add(time.Nanosecond)
	if got := TimerVerdict(fireAt, pastThreshold); got != VerdictStale {
		t.Errorf("TimerVerdict one nanosecond past TimerStalenessHours = %v, want VerdictStale", got)
	}
}

// TestDelayCaveat_BelowThreshold proves spec R1.3's own "a few seconds
// late" scenario: the scheduler's own granularity is not a caveat-worthy
// fact about the user's world.
func TestDelayCaveat_BelowThreshold(t *testing.T) {
	if got := DelayCaveat(3 * time.Second); got != false {
		t.Errorf("DelayCaveat(3s) = %v, want false", got)
	}
}

// TestDelayCaveat_BoundaryIsInclusive proves Finding F6: exactly
// DelayCaveatMinutes late already caveats. This is deliberately the
// opposite direction of TriggerVerdict/TimerVerdict's own strict
// staleness comparison above (design §3.3) — there the inclusive side is
// destructive (expiring is unrecoverable); here a caveat that was not
// strictly necessary costs one clause of politeness, while a missing one
// on a genuinely late delivery is the exact failure ADR-0009 exists to
// prevent, so the boundary belongs on the cheap side.
func TestDelayCaveat_BoundaryIsInclusive(t *testing.T) {
	if got := DelayCaveat(DelayCaveatMinutes * time.Minute); got != true {
		t.Errorf("DelayCaveat(%d min) = %v, want true (inclusive boundary)", DelayCaveatMinutes, got)
	}
}

// TestDelayCaveat_AboveThreshold proves the ordinary above-threshold
// case.
func TestDelayCaveat_AboveThreshold(t *testing.T) {
	overdue := DelayCaveatMinutes*time.Minute + time.Minute
	if got := DelayCaveat(overdue); got != true {
		t.Errorf("DelayCaveat(%v) = %v, want true", overdue, got)
	}
}

// TestAllVerdicts_HasExactlyTheFourMembers is AllVerdicts's own existence
// check: four members, in declared order, in a fresh slice.
//
// The order matters because it is the order every caller sweeping the
// vocabulary will report failures in, and the freshness matters because a
// completeness check run from outside this package must not be defeatable
// by an importer that scribbled on an earlier call's result.
func TestAllVerdicts_HasExactlyTheFourMembers(t *testing.T) {
	want := []Verdict{VerdictPending, VerdictDefer, VerdictStale, VerdictDeliver}

	got := AllVerdicts()
	if len(got) != len(want) {
		t.Fatalf("AllVerdicts() returned %d members, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q", i, got[i], want[i])
		}
	}

	first := AllVerdicts()
	first[0] = "scribbled"
	if second := AllVerdicts(); second[0] == "scribbled" {
		t.Fatal("AllVerdicts() shares its backing array across calls")
	}
}

// TestAllVerdicts_CoversEveryVerdictThatVerdictCanReturn is the half that
// keeps the accessor honest. A member added to the vocabulary and forgotten
// in AllVerdicts would make every sweep built on it quietly partial, and
// nothing else in the package would notice.
//
// It cannot enumerate the constants — that is what it is checking — so it
// drives the decision function across every input shape that reaches a
// distinct branch and asserts every verdict it observes is a member.
func TestAllVerdicts_CoversEveryVerdictThatVerdictCanReturn(t *testing.T) {
	members := map[Verdict]bool{}
	for _, v := range AllVerdicts() {
		members[v] = true
	}

	// Noon on a Wednesday, well outside quiet hours.
	fireAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	quiet := time.Date(2026, 8, 5, QuietHoursStartHour, 30, 0, 0, time.UTC)

	observed := map[Verdict]bool{}
	for _, now := range []time.Time{
		fireAt.Add(-time.Hour),                              // pending
		fireAt.Add(time.Minute),                             // deliver
		fireAt.Add((TriggerStalenessHours + 1) * time.Hour), // stale
		quiet, // defer, quiet hours
	} {
		for _, v := range []Verdict{TriggerVerdict(fireAt, now), TimerVerdict(fireAt, now)} {
			if !members[v] {
				t.Errorf("verdict %q is reachable but absent from AllVerdicts()", v)
			}
			observed[v] = true
		}
	}

	if len(observed) < 3 {
		t.Fatalf("only %d distinct verdicts were reached (%v) — this test stopped covering what it claims to", len(observed), observed)
	}
}
