package consolidation

import (
	"testing"
	"time"

	// tzdata is imported for its side effect only, and only by this test
	// file: TestNextDailyRun_DST loads a real IANA zone by name
	// (time.LoadLocation), which has no zone database to read from on
	// Windows without it. This repo cross-compiles for Windows (ADR-0013),
	// so the import stays out of the shipped binary — schedule.go itself
	// never imports it.
	_ "time/tzdata"
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

// TestNextDailyRun_DST is an edge of TestNextDailyRun over a real IANA
// zone (Europe/Berlin) instead of time.FixedZone, covering both 2026 DST
// transitions. It runs against 1.6's already-committed NextDailyRun — no
// production change accompanies this test; the function already delegates
// entirely to time.Date's own normalization, so this is a proof, not a
// fix.
//
// go doc time.Date is explicit that an ambiguous repeated local time
// resolves to "one of the two zones involved in the transition", without
// guaranteeing which — so the fall-back assertion below pins this
// toolchain's and this tzdata release's observed choice, not a documented
// Go contract. If a future Go/tzdata upgrade changes it, this test is the
// place that will say so.
func TestNextDailyRun_DST(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("time.LoadLocation(%q): %v", "Europe/Berlin", err)
	}

	t.Run("spring forward: non-existent local 02:00 normalizes forward to 03:00 CEST", func(t *testing.T) {
		// 2026-03-29: Berlin's clocks jump from 01:59:59 CET straight to
		// 03:00:00 CEST — local 02:00-02:59 never occurs.
		after := time.Date(2026, 3, 29, 1, 0, 0, 0, berlin)
		got := NextDailyRun(after, 2)
		want := time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC) // = 03:00 CEST
		if !got.Equal(want) {
			t.Errorf("NextDailyRun(%v, 2) = %v, want %v (03:00 CEST, the normalized instant)", after, got, want)
		}
	})

	t.Run("fall back: the repeated local 02:00 resolves to one deterministic instant", func(t *testing.T) {
		// 2026-10-25: Berlin's local 02:00 occurs twice — first as 02:00
		// CEST, then again as 02:00 CET an hour later. after sits before
		// both occurrences (01:00 CEST, unambiguous).
		after := time.Date(2026, 10, 25, 1, 0, 0, 0, berlin)
		got := NextDailyRun(after, 2)
		want := time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC) // = 02:00 CET, the second occurrence
		if !got.Equal(want) {
			t.Errorf("NextDailyRun(%v, 2) = %v, want %v (the second 02:00, this toolchain's observed pick)", after, got, want)
		}
	})
}

// timePtr returns a pointer to t — time.Time has no address to take
// directly off a composite literal field.
func timePtr(t time.Time) *time.Time {
	return &t
}
