package prospection

import (
	"testing"
	"time"
)

// TestDigestDue proves owner ruling 2's cadence: once daily, at DigestHour,
// and a vault that was off for three days owes exactly one digest rather
// than three (ADR-0014's "a late delivery is a normal case" applied to the
// cadence itself).
func TestDigestDue(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, 8, day, hour, minute, 0, 0, loc)
	}
	ptr := func(t time.Time) *time.Time { return &t }

	tests := []struct {
		name       string
		lastDigest *time.Time
		now        time.Time
		want       bool
	}{
		{
			name:       "no digest ever, now at DigestHour",
			lastDigest: nil,
			now:        at(7, DigestHour, 0),
			want:       true,
		},
		{
			name:       "no digest ever, now before DigestHour",
			lastDigest: nil,
			now:        at(7, DigestHour-1, 59),
			want:       false,
		},
		{
			name:       "today's digest already sent, later the same day",
			lastDigest: ptr(at(7, DigestHour, 0)),
			now:        at(7, 18, 0),
			want:       false,
		},
		{
			name:       "yesterday's digest, now at today's DigestHour",
			lastDigest: ptr(at(6, DigestHour, 0)),
			now:        at(7, DigestHour, 0),
			want:       true,
		},
		{
			// Downtime is a normal case, and the cadence must not accrue a
			// backlog: three days off owes one digest, not three. Asserted
			// as the verdict at a single instant, which is all a pure
			// predicate can say — that one call returns true once, and
			// false again after the digest is recorded, is the pair that
			// makes "exactly one" observable.
			name:       "three days of downtime owes one digest",
			lastDigest: ptr(at(4, DigestHour, 0)),
			now:        at(7, DigestHour+3, 0),
			want:       true,
		},
		{
			name:       "and once that digest is recorded, no more today",
			lastDigest: ptr(at(7, DigestHour+3, 0)),
			now:        at(7, 23, 59),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DigestDue(tt.lastDigest, tt.now); got != tt.want {
				t.Errorf("DigestDue(%v, %v) = %v, want %v", tt.lastDigest, tt.now, got, tt.want)
			}
		})
	}
}
