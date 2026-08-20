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
