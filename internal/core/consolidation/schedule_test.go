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

// TestResolveConsolidationEnabled proves R1.2's gate resolution mirrors
// migration 0002:65's own consolidation_enabled DEFAULT 1: a config row
// that has never set the column reads nil, not false, so nil must resolve
// to enabled.
func TestResolveConsolidationEnabled(t *testing.T) {
	tru := true
	fls := false

	tests := []struct {
		name       string
		configured *bool
		want       bool
	}{
		{"nil resolves to enabled — DEFAULT 1", nil, true},
		{"explicit false stays disabled", &fls, false},
		{"explicit true stays enabled", &tru, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveConsolidationEnabled(tt.configured)
			if got != tt.want {
				t.Errorf("ResolveConsolidationEnabled(%v) = %v, want %v", tt.configured, got, tt.want)
			}
		})
	}
}

// TestNextDailyRun proves the cron's next-fire computation is strictly
// after the given instant: exactly on the hour returns tomorrow, not
// today (design §4's own "strictly after" comment), and the computation
// crosses a month/year boundary the same way a plain calendar day would.
// time.FixedZone, not time.LoadLocation, so this test carries no tzdata
// dependency.
func TestNextDailyRun(t *testing.T) {
	loc := time.FixedZone("FIXED-5", -5*60*60)

	tests := []struct {
		name  string
		after time.Time
		hour  int
		want  time.Time
	}{
		{
			name:  "before the hour today fires later today",
			after: time.Date(2026, 8, 7, 2, 0, 0, 0, loc),
			hour:  3,
			want:  time.Date(2026, 8, 7, 3, 0, 0, 0, loc),
		},
		{
			name:  "after the hour today fires tomorrow",
			after: time.Date(2026, 8, 7, 4, 0, 0, 0, loc),
			hour:  3,
			want:  time.Date(2026, 8, 8, 3, 0, 0, 0, loc),
		},
		{
			name:  "exactly on the hour fires tomorrow — strictly after",
			after: time.Date(2026, 8, 7, 3, 0, 0, 0, loc),
			hour:  3,
			want:  time.Date(2026, 8, 8, 3, 0, 0, 0, loc),
		},
		{
			name:  "crosses a month and year boundary",
			after: time.Date(2026, 12, 31, 23, 0, 0, 0, loc),
			hour:  3,
			want:  time.Date(2027, 1, 1, 3, 0, 0, 0, loc),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextDailyRun(tt.after, tt.hour)
			if !got.Equal(tt.want) {
				t.Errorf("NextDailyRun(%v, %d) = %v, want %v", tt.after, tt.hour, got, tt.want)
			}
		})
	}
}

// timePtr returns a pointer to t — time.Time has no address to take
// directly off a composite literal field.
func timePtr(t time.Time) *time.Time {
	return &t
}
