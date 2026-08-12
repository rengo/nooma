package consolidation

import (
	"testing"
	"time"
)

// TestCatchUpDue proves R2.1's boot catch-up gate: a nil lastRunAt is
// always due, the 24h boundary is strict (ADR-0009's "more than 24h"), and
// a future lastRunAt (clock skew in the other direction) is never due —
// no repair is invented for it.
func TestCatchUpDue(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		lastRunAt *time.Time
		want      bool
	}{
		{
			name:      "nil lastRunAt is always due",
			lastRunAt: nil,
			want:      true,
		},
		{
			name:      "23h59m elapsed is not due",
			lastRunAt: timePtr(now.Add(-(23*time.Hour + 59*time.Minute))),
			want:      false,
		},
		{
			name:      "exactly 24h elapsed is not due — strict comparison",
			lastRunAt: timePtr(now.Add(-24 * time.Hour)),
			want:      false,
		},
		{
			name:      "24h and 1s elapsed is due",
			lastRunAt: timePtr(now.Add(-24*time.Hour - time.Second)),
			want:      true,
		},
		{
			name:      "a future lastRunAt is never due — no clock-skew repair",
			lastRunAt: timePtr(now.Add(time.Hour)),
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CatchUpDue(tt.lastRunAt, now, CatchUpStalenessHours)
			if got != tt.want {
				t.Errorf("CatchUpDue(%v, %v, %d) = %v, want %v",
					tt.lastRunAt, now, CatchUpStalenessHours, got, tt.want)
			}
		})
	}
}

// timePtr returns a pointer to t — time.Time has no address to take
// directly off a composite literal field.
func timePtr(t time.Time) *time.Time {
	return &t
}
