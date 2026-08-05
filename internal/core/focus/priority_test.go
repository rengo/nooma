package focus

import (
	"testing"
	"time"
)

// dueIn returns a *time.Time exactly d days after now — a small helper so
// every UrgencyRamp fixture below states its intent (days until due) rather
// than a raw time.Duration.
func dueIn(now time.Time, days float64) *time.Time {
	t := now.Add(time.Duration(days * 24 * float64(time.Hour)))
	return &t
}

// TestUrgencyRamp_Table proves spec R3.3's boundary table: nil is exactly
// 0, not the d -> infinity limit; the ramp is 0 at or beyond the lead
// window, rises linearly to 1 at d = 0, and clamps at 1 once a unit is
// overdue, never growing further no matter how overdue it gets.
func TestUrgencyRamp_Table(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		dueAt *time.Time
		want  float64
	}{
		{"nil due date", nil, 0},
		{"due at exactly the lead window", dueIn(now, UrgencyLeadDays), 0},
		{"due well beyond the lead window", dueIn(now, 30), 0},
		{"due halfway through the lead window", dueIn(now, 3.5), 0.5},
		{"due exactly now", dueIn(now, 0), 1},
		{"overdue by one day", dueIn(now, -1), 1},
		{"overdue by 1000 days — does not grow past 1", dueIn(now, -1000), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := UrgencyRamp(c.dueAt, now)
			if got != c.want {
				t.Errorf("UrgencyRamp(%v, now) = %v, want %v", c.dueAt, got, c.want)
			}
		})
	}
}
