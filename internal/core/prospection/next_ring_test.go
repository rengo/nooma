package prospection

import (
	"testing"
	"time"
)

// TestNextRing_IsWhenTheDueScanRingsIt: a remind-me of a recurring reminder
// names when its armed row will actually ring, as the due scan will judge it
// (ADR-0034, ADR-0030), never a past instant: its own instant while ahead and
// outside the night; quiet end for one the night holds; now for one due and
// not yet sent; the next occurrence for one that lapses. The rule is said at
// the occurrence's clock, whatever moved the ring.
func TestNextRing_IsWhenTheDueScanRingsIt(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 11, day, hour, minute, 0, 0, zone) }
	night := QuietHours{End: TimeOfDay{Hour: 7}}
	daily := RuleDaily
	row := func(fire time.Time, nightExempt bool) Reminder {
		clock := TimeOfDay{Hour: fire.Hour(), Minute: fire.Minute()}
		return Reminder{FireAt: fire.UTC(), Rule: &daily, Anchor: &Anchor{Month: fire.Month(), Day: fire.Day(), At: &clock}, NightExempt: nightExempt}
	}
	for _, tc := range []struct {
		name                 string
		r                    Reminder
		now                  time.Time
		occurrence, wantRing time.Time
	}{
		{"ahead, outside the night", row(at(10, 21, 30), false), at(10, 9, 51), at(10, 21, 30), at(10, 21, 30)},
		{"ahead, held by the night", row(at(11, 6, 0), false), at(10, 18, 0), at(11, 6, 0), at(11, 7, 0)},
		{"ahead in the night, exempt", row(at(11, 6, 0), true), at(10, 18, 0), at(11, 6, 0), at(11, 6, 0)},
		{"due, not yet sent", row(at(10, 9, 0), false), at(10, 9, 5), at(10, 9, 0), at(10, 9, 5)},
		{"due, deferred by the night", row(at(10, 6, 30), false), at(10, 6, 40), at(10, 6, 30), at(10, 7, 0)},
		{"no rule: its instant, out of the night", Reminder{FireAt: at(11, 6, 0)}, at(10, 18, 0), at(11, 6, 0), at(11, 7, 0)},
		{"overdue past the window, lapses to the next", row(at(10, 9, 0), false), at(10, 15, 1), at(11, 9, 0), at(11, 9, 0)},
	} {
		occurrence, ring := night.NextRing(tc.r, tc.now)
		if !occurrence.Equal(tc.occurrence) || !ring.Equal(tc.wantRing) {
			t.Errorf("%s: NextRing = %v, %v; want occurrence %v, ring %v", tc.name, occurrence.In(zone), ring.In(zone), tc.occurrence, tc.wantRing)
		}
		if ring.Before(tc.now) {
			t.Errorf("%s: ring %v is behind now %v", tc.name, ring, tc.now)
		}
	}
}
