package prospection

import (
	"testing"
	"time"
)

var recLoc = time.FixedZone("UTC+2", 2*60*60)

func recAt(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, recLoc)
}

// TestNextOccurrence_ClampsToTheLastDayOfTheMonth is R10's first ordinary
// input that breaks naive code. The rule is to clamp, never to overflow and
// never to skip.
//
// Go's own time.Date normalisation is the trap: time.Date(y, Feb, 31, …)
// silently becomes 3 March. A monthly reminder built that way wanders
// forward and can miss a month entirely, and a February anniversary lands
// in March.
func TestNextOccurrence_ClampsToTheLastDayOfTheMonth(t *testing.T) {
	tests := []struct {
		name   string
		rule   Rule
		anchor Anchor
		after  time.Time
		want   time.Time
	}{
		{
			name:   "29 February in a non-leap year clamps to the 28th",
			rule:   RuleYearly,
			anchor: Anchor{Month: time.February, Day: 29},
			after:  recAt(2027, time.January, 1, 0),
			want:   recAt(2027, time.February, 28, RecurrenceAnchorHour),
		},
		{
			name:   "29 February in a leap year is the 29th",
			rule:   RuleYearly,
			anchor: Anchor{Month: time.February, Day: 29},
			after:  recAt(2028, time.January, 1, 0),
			want:   recAt(2028, time.February, 29, RecurrenceAnchorHour),
		},
		{
			name:   "day 31 monthly, from January, is 31 January",
			rule:   RuleMonthly,
			anchor: Anchor{Day: 31},
			after:  recAt(2027, time.January, 1, 0),
			want:   recAt(2027, time.January, 31, RecurrenceAnchorHour),
		},
		{
			name:   "day 31 monthly clamps in February, never skips it",
			rule:   RuleMonthly,
			anchor: Anchor{Day: 31},
			after:  recAt(2027, time.February, 1, 0),
			want:   recAt(2027, time.February, 28, RecurrenceAnchorHour),
		},
		{
			name:   "day 31 monthly is back to the 31st in March",
			rule:   RuleMonthly,
			anchor: Anchor{Day: 31},
			after:  recAt(2027, time.March, 1, 0),
			want:   recAt(2027, time.March, 31, RecurrenceAnchorHour),
		},
		{
			name:   "day 31 monthly clamps to 30 in April",
			rule:   RuleMonthly,
			anchor: Anchor{Day: 31},
			after:  recAt(2027, time.April, 1, 0),
			want:   recAt(2027, time.April, 30, RecurrenceAnchorHour),
		},
		{
			name:   "day 31 monthly in a leap February is the 29th",
			rule:   RuleMonthly,
			anchor: Anchor{Day: 31},
			after:  recAt(2028, time.February, 1, 0),
			want:   recAt(2028, time.February, 29, RecurrenceAnchorHour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextOccurrence(tt.rule, tt.anchor, tt.after)
			if !got.Equal(tt.want) {
				t.Errorf("NextOccurrence(%v, %+v, %v) = %v, want %v",
					tt.rule, tt.anchor, tt.after, got, tt.want)
			}
		})
	}
}

// TestNextOccurrence_IsStrictlyAfter pins the boundary: the occurrence
// currently firing is not its own next one, or a trigger would re-arm onto
// the instant it just fired and fire forever.
func TestNextOccurrence_IsStrictlyAfter(t *testing.T) {
	anchor := Anchor{Month: time.June, Day: 15}
	occurrence := recAt(2027, time.June, 15, RecurrenceAnchorHour)

	got := NextOccurrence(RuleYearly, anchor, occurrence)
	if !got.After(occurrence) {
		t.Fatalf("NextOccurrence at the occurrence itself = %v, want strictly after %v",
			got, occurrence)
	}
	if want := recAt(2028, time.June, 15, RecurrenceAnchorHour); !got.Equal(want) {
		t.Errorf("NextOccurrence = %v, want %v", got, want)
	}

	oneNanoBefore := occurrence.Add(-time.Nanosecond)
	if got := NextOccurrence(RuleYearly, anchor, oneNanoBefore); !got.Equal(occurrence) {
		t.Errorf("a nanosecond before the occurrence, NextOccurrence = %v, want the occurrence "+
			"itself (%v)", got, occurrence)
	}
}

// TestNextOccurrence_AnchorIsIdempotent is I17's arithmetic half, and the
// reason the clamp above is safe.
//
// The rule is to re-derive from the anchor every time, never to advance from
// the previous occurrence. Advancing 29 February by one year gives 28
// February, and advancing THAT gives 28 February forever — the anniversary
// drifts off its own date after a single leap cycle, and a day-31 monthly
// reminder does the same after its first February.
//
// The test walks a full leap cycle and asserts the anchor is recovered, so
// a drifting implementation fails on the year it drifts rather than on a
// shape the fixture chose.
func TestNextOccurrence_AnchorIsIdempotent(t *testing.T) {
	anchor := Anchor{Month: time.February, Day: 29}

	cursor := recAt(2027, time.January, 1, 0)
	seen := map[int]int{} // year -> day of month

	for range 6 {
		next := NextOccurrence(RuleYearly, anchor, cursor)
		seen[next.Year()] = next.Day()
		cursor = next
	}

	for year, day := range seen {
		want := 28
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			want = 29
		}
		if day != want {
			t.Errorf("in %d the occurrence landed on the %d, want the %d — the anchor must be "+
				"re-derived each time, never advanced from the previous occurrence", year, day, want)
		}
	}

	// The same instant, computed twice from two different starting points
	// that precede it, must agree — re-arming is a pure function of
	// (rule, anchor, now), never of the trigger's own history.
	fromJanuary := NextOccurrence(RuleYearly, anchor, recAt(2028, time.January, 1, 0))
	fromFebruary := NextOccurrence(RuleYearly, anchor, recAt(2028, time.February, 1, 0))
	if !fromJanuary.Equal(fromFebruary) {
		t.Errorf("the same occurrence computed from January (%v) and February (%v) disagree — "+
			"re-arming must not depend on when it is asked", fromJanuary, fromFebruary)
	}
}

// TestNextOccurrence_LandsAtNoonInTheInstantsOwnZone pins
// RecurrenceAnchorHour and the zone it is read in.
//
// Midnight is rejected on evidence already in the tree: a DST gap at local
// midnight normalises BACKWARD — consolidation.NextDailyRun's own comment
// records Havana mapping local 00:00 to 23:00 the previous evening. An
// anniversary would then carry the previous calendar date and nudge a day
// early, once a year, in whichever zone puts its transition at midnight.
func TestNextOccurrence_LandsAtNoonInTheInstantsOwnZone(t *testing.T) {
	anchor := Anchor{Month: time.June, Day: 15}
	after := recAt(2027, time.January, 1, 0)

	got := NextOccurrence(RuleYearly, anchor, after)
	if got.Hour() != RecurrenceAnchorHour {
		t.Errorf("occurrence at hour %d, want RecurrenceAnchorHour (%d)",
			got.Hour(), RecurrenceAnchorHour)
	}
	if got.Location().String() != recLoc.String() {
		t.Errorf("occurrence in zone %v, want the zone the instant arrived in (%v) — the zone "+
			"travels inside the instant, never from configuration",
			got.Location(), recLoc)
	}
}
