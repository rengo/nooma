package prospection

import (
	"testing"
	"time"
	_ "time/tzdata"
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

	// Two walks: one over an ordinary run of years, and one crossing 2100,
	// which is NOT a leap year despite being divisible by four. Without the
	// second, the century rule in this test's own expectation below is a
	// branch the suite never reaches — an expectation nothing checks.
	for _, start := range []int{2027, 2098} {
		assertLeapCycle(t, anchor, start)
	}
}

func assertLeapCycle(t *testing.T, anchor Anchor, startYear int) {
	t.Helper()

	cursor := recAt(startYear, time.January, 1, 0)
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
	fromJanuary := NextOccurrence(RuleYearly, anchor, recAt(startYear+1, time.January, 1, 0))
	fromFebruary := NextOccurrence(RuleYearly, anchor, recAt(startYear+1, time.February, 1, 0))
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

	// The property, asserted separately from the constant, because the check
	// above reads RecurrenceAnchorHour to build its own expectation and would
	// therefore move with a recalibration to midnight — passing on exactly
	// the change it is here to prevent.
	//
	// Eleven hours of clearance in each direction is what makes noon safe:
	// it is off by more than any transition shorter than twelve hours, and
	// the only known transitions of twelve or more delete the whole calendar
	// date (Pacific/Apia skipped 2011-12-30, Pacific/Kiritimati 1994-12-31),
	// where no instant on that date exists at any hour.
	const minClearance = 11
	if RecurrenceAnchorHour < minClearance || RecurrenceAnchorHour > 24-minClearance {
		t.Errorf("RecurrenceAnchorHour = %d, want between %d and %d — an anniversary within an "+
			"hour of midnight can normalise BACKWARD through a DST gap onto the previous "+
			"calendar date and nudge a day early, once a year, in whichever zone puts its "+
			"transition there (consolidation.NextDailyRun records Havana doing exactly this)",
			RecurrenceAnchorHour, minClearance, 24-minClearance)
	}
	if got.Location().String() != recLoc.String() {
		t.Errorf("occurrence in zone %v, want the zone the instant arrived in (%v) — the zone "+
			"travels inside the instant, never from configuration",
			got.Location(), recLoc)
	}
}

// TestNextOccurrence_DegenerateAnchorStaysInsideItsMonth covers the anchor
// values that reach this function from a row rather than from a person.
//
// recurrence_anchor is nullable, and Arm builds it from a classification
// that may not carry one, so the zero Anchor is reachable. Without a guard
// it is actively wrong rather than merely odd: time.Date(y, m, 0, …)
// normalises BACKWARD to the last day of the previous month, so a day-0
// anchor would silently fire in a month the user never named. A day past
// the month's end clamps, which is rule 1 doing its job.
func TestNextOccurrence_DegenerateAnchorStaysInsideItsMonth(t *testing.T) {
	after := recAt(2027, time.March, 5, 0)

	tests := map[string]struct {
		anchor Anchor
		want   time.Time
	}{
		"day zero is the first of the month": {
			anchor: Anchor{Month: time.June, Day: 0},
			want:   recAt(2027, time.June, 1, RecurrenceAnchorHour),
		},
		"a negative day is the first of the month": {
			anchor: Anchor{Month: time.June, Day: -3},
			want:   recAt(2027, time.June, 1, RecurrenceAnchorHour),
		},
		"a day past the month's end clamps to its last": {
			anchor: Anchor{Month: time.June, Day: 99},
			want:   recAt(2027, time.June, 30, RecurrenceAnchorHour),
		},
		// Month has the same shape as Day and the same reachability: the
		// zero Anchor carries month 0, which time.Date normalises BACKWARD
		// into December of the previous year — an anniversary landing in a
		// year the user never named, which is worse than the wrong day.
		"month zero is January": {
			anchor: Anchor{Month: 0, Day: 15},
			want:   recAt(2028, time.January, 15, RecurrenceAnchorHour),
		},
		"a month past December is December": {
			anchor: Anchor{Month: time.Month(14), Day: 15},
			want:   recAt(2027, time.December, 15, RecurrenceAnchorHour),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := NextOccurrence(RuleYearly, tt.anchor, after)
			if !got.Equal(tt.want) {
				t.Errorf("NextOccurrence(yearly, %+v, %v) = %v, want %v — an anchor this "+
					"function cannot honour must stay inside the month it names",
					tt.anchor, after, got, tt.want)
			}
			if got.Month() != tt.want.Month() {
				t.Errorf("occurrence landed in %v, want %v — a degenerate anchor must never "+
					"move the occurrence into another month or another year",
					got.Month(), tt.want.Month())
			}
			if got.Year() != tt.want.Year() {
				t.Errorf("occurrence landed in %d, want %d", got.Year(), tt.want.Year())
			}
		})
	}
}

// TestNextOccurrence_AdvancesPastMonthsAndYears covers the two steps the
// clamp cases never reach, because each of those starts on the 1st and
// finds its answer in the same month.
//
// Both are ordinary: a monthly reminder is asked about most often on a day
// its anchor has already passed, and every December recurrence rolls into
// the next year.
func TestNextOccurrence_AdvancesPastMonthsAndYears(t *testing.T) {
	tests := map[string]struct {
		rule   Rule
		anchor Anchor
		after  time.Time
		want   time.Time
	}{
		"monthly, this month's day already gone": {
			rule:   RuleMonthly,
			anchor: Anchor{Day: 5},
			after:  recAt(2027, time.March, 20, 9),
			want:   recAt(2027, time.April, 5, RecurrenceAnchorHour),
		},
		"monthly rolls December into January of the next year": {
			rule:   RuleMonthly,
			anchor: Anchor{Day: 15},
			after:  recAt(2027, time.December, 20, 9),
			want:   recAt(2028, time.January, 15, RecurrenceAnchorHour),
		},
		"monthly day 31 crossing December keeps the 31st": {
			rule:   RuleMonthly,
			anchor: Anchor{Day: 31},
			after:  recAt(2027, time.December, 31, 23),
			want:   recAt(2028, time.January, 31, RecurrenceAnchorHour),
		},
		"yearly, a December anchor already past": {
			rule:   RuleYearly,
			anchor: Anchor{Month: time.December, Day: 25},
			after:  recAt(2027, time.December, 26, 9),
			want:   recAt(2028, time.December, 25, RecurrenceAnchorHour),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := NextOccurrence(tt.rule, tt.anchor, tt.after)
			if !got.Equal(tt.want) {
				t.Errorf("NextOccurrence(%v, %+v, %v) = %v, want %v",
					tt.rule, tt.anchor, tt.after, got, tt.want)
			}
		})
	}
}

// TestNextOccurrence_RealZoneTransitions is design §8's two mandatory
// fixtures, plus a fixed-offset control. tzdata is imported by this test
// file only: the shipped binary must not carry it, because this repository
// cross-compiles for Windows (ADR-0013).
//
// These are the zones that make RecurrenceAnchorHour a decision instead of
// a preference, and the numbers below were read off the real database
// rather than assumed:
//
//	America/Havana  2026-03-08 00:00 -> 2026-03-07 23:00  (the day before)
//	America/Havana  2027-03-14 00:00 -> 2027-03-13 23:00  (the day before)
//	Pacific/Apia    2011-12-30 00:00 -> 2011-12-29 00:00  (the day before)
//	Pacific/Apia    2011-12-30 12:00 -> 2011-12-31 12:00  (the day after)
//
// At midnight both zones normalise BACKWARD, so an anniversary fires a day
// EARLY — the failure mode this constant exists to avoid, because a
// reminder that arrives before the thing it is about is not late, it is
// wrong. At noon Havana is nowhere near its gap, and Apia — whose calendar
// date genuinely does not exist — resolves forward, a day late, which is
// recoverable.
func TestNextOccurrence_RealZoneTransitions(t *testing.T) {
	havana, err := time.LoadLocation("America/Havana")
	if err != nil {
		t.Fatalf("America/Havana: %v", err)
	}
	apia, err := time.LoadLocation("Pacific/Apia")
	if err != nil {
		t.Fatalf("Pacific/Apia: %v", err)
	}

	t.Run("Havana's spring-forward date keeps its own calendar day", func(t *testing.T) {
		// Midnight on this date does not exist and rolls back to the 7th.
		if midnight := time.Date(2026, time.March, 8, 0, 0, 0, 0, havana); midnight.Day() != 7 {
			t.Fatalf("fixture is stale: 2026-03-08 00:00 in Havana resolved to day %d, want 7 — "+
				"this test's whole premise is that it normalises backward", midnight.Day())
		}

		got := NextOccurrence(RuleYearly, Anchor{Month: time.March, Day: 8},
			time.Date(2026, time.January, 1, 0, 0, 0, 0, havana))

		if got.Day() != 8 || got.Month() != time.March {
			t.Errorf("occurrence = %v, want 8 March — an anniversary must land on its own date, "+
				"and at midnight this one would have landed on the 7th", got)
		}
	})

	t.Run("Apia's non-existent date resolves forward, never backward", func(t *testing.T) {
		if midnight := time.Date(2011, time.December, 30, 0, 0, 0, 0, apia); midnight.Day() != 29 {
			t.Fatalf("fixture is stale: 2011-12-30 00:00 in Apia resolved to day %d, want 29",
				midnight.Day())
		}

		got := NextOccurrence(RuleYearly, Anchor{Month: time.December, Day: 30},
			time.Date(2011, time.December, 1, 0, 0, 0, 0, apia))

		if got.Before(time.Date(2011, time.December, 30, 0, 0, 0, 0, apia)) {
			t.Errorf("occurrence = %v, which precedes the anchor's own date — a date that does "+
				"not exist must resolve forward, so the nudge is late rather than early", got)
		}
		if got.Day() != 31 {
			t.Errorf("occurrence = %v, want 31 December — 30 December 2011 does not exist in "+
				"this zone, and forward is the only safe answer", got)
		}
	})

	t.Run("clamping and a real zone's normalisation compose", func(t *testing.T) {
		// Neither mandatory fixture above is out of range for its month, so
		// neither reaches the clamp. This one does both at once: a 29
		// February anchor in a common year, in a zone with real transitions,
		// so the clamped day and the zone's own normalisation are exercised
		// together rather than one at a time.
		got := NextOccurrence(RuleYearly, Anchor{Month: time.February, Day: 29},
			time.Date(2027, time.January, 1, 0, 0, 0, 0, havana))

		if got.Month() != time.February || got.Day() != 28 {
			t.Errorf("occurrence = %v, want 28 February 2027 — a 29 February anchor clamps in a "+
				"common year, and the zone must not move it out of its own month", got)
		}
		if got.Hour() != RecurrenceAnchorHour {
			t.Errorf("occurrence = %v, want hour %d", got, RecurrenceAnchorHour)
		}
	})

	t.Run("a month whose own last day was deleted still clamps correctly", func(t *testing.T) {
		// Pacific/Kiritimati skipped 1994-12-31 entirely when it crossed the
		// date line — and 31 December is December's own last day, which is
		// exactly what the clamp has to look up.
		//
		// Asking the ZONE for that length is the trap: the probe lands on a
		// date that does not exist there, normalises forward into January,
		// and reports "1" as December's last day. Every anchor above the 1st
		// would then clamp to the 1st, so a 25 December anniversary fires on
		// 1 December — three weeks early, once, in one zone, silently.
		//
		// A month's length is a property of the calendar, not of a zone.
		kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
		if err != nil {
			t.Fatalf("Pacific/Kiritimati: %v", err)
		}
		if probe := time.Date(1995, time.January, 0, RecurrenceAnchorHour, 0, 0, 0, kiritimati); probe.Day() != 1 {
			t.Fatalf("fixture is stale: the zone-local probe for December 1994's last day "+
				"resolved to day %d, want 1 — this test's premise is that it is wrong there",
				probe.Day())
		}

		got := NextOccurrence(RuleYearly, Anchor{Month: time.December, Day: 25},
			time.Date(1994, time.December, 1, 0, 0, 0, 0, kiritimati))

		if got.Day() != 25 || got.Month() != time.December {
			t.Errorf("occurrence = %v, want 25 December 1994 — a deleted day elsewhere in the "+
				"month must not change how many days the month has", got)
		}
	})

	t.Run("a fixed-offset zone is unaffected", func(t *testing.T) {
		got := NextOccurrence(RuleYearly, Anchor{Month: time.March, Day: 8},
			time.Date(2026, time.January, 1, 0, 0, 0, 0, recLoc))
		if want := recAt(2026, time.March, 8, RecurrenceAnchorHour); !got.Equal(want) {
			t.Errorf("control zone: occurrence = %v, want %v", got, want)
		}
	})
}

// TestNextOccurrence_OrdinaryAnchorBaseline is spec R5.1's own sanity
// baseline, and the test this file most needed: every other case here is an
// edge — 29 February, day 31, a DST gap — and a suite made only of corners
// can be green while the middle is broken.
//
// An anchor that exists in every month and every year should simply advance,
// one step at a time, landing on its own day each time.
func TestNextOccurrence_OrdinaryAnchorBaseline(t *testing.T) {
	t.Run("yearly, 15 March across five years", func(t *testing.T) {
		anchor := Anchor{Month: time.March, Day: 15}
		cursor := recAt(2027, time.January, 1, 0)

		for year := 2027; year < 2032; year++ {
			got := NextOccurrence(RuleYearly, anchor, cursor)
			want := recAt(year, time.March, 15, RecurrenceAnchorHour)
			if !got.Equal(want) {
				t.Fatalf("year %d: NextOccurrence = %v, want %v", year, got, want)
			}
			cursor = got
		}
	})

	t.Run("monthly, day 10 across fourteen months", func(t *testing.T) {
		anchor := Anchor{Day: 10}
		cursor := recAt(2027, time.January, 1, 0)

		year, month := 2027, time.January
		for range 14 {
			got := NextOccurrence(RuleMonthly, anchor, cursor)
			want := time.Date(year, month, 10, RecurrenceAnchorHour, 0, 0, 0, recLoc)
			if !got.Equal(want) {
				t.Fatalf("NextOccurrence = %v, want %v — an ordinary day-10 reminder should "+
					"land on the 10th of every month, including across the year boundary",
					got, want)
			}
			cursor = got
			year, month = nextMonth(year, month)
		}
	})
}

// TestNextOccurrence_IsDeterministic is spec R5.1's determinism clause: the
// same (rule, anchor, after) computed twice returns the same instant.
//
// It is a different property from TestNextOccurrence_AnchorIsIdempotent's
// tail, which shows two DIFFERENT starting points converging. This one
// pins that the function reads nothing outside its arguments — no clock, no
// package state, no map iteration order — which is what makes it safe for
// a caller to recompute an occurrence instead of storing it.
func TestNextOccurrence_IsDeterministic(t *testing.T) {
	cases := []struct {
		rule   Rule
		anchor Anchor
		after  time.Time
	}{
		{RuleYearly, Anchor{Month: time.February, Day: 29}, recAt(2027, time.January, 1, 0)},
		{RuleMonthly, Anchor{Day: 31}, recAt(2027, time.February, 1, 0)},
		{RuleYearly, Anchor{Month: time.March, Day: 15}, recAt(2031, time.December, 31, 23)},
	}

	for _, c := range cases {
		first := NextOccurrence(c.rule, c.anchor, c.after)
		for range 5 {
			if again := NextOccurrence(c.rule, c.anchor, c.after); !again.Equal(first) {
				t.Errorf("NextOccurrence(%v, %+v, %v) returned %v then %v — the same arguments "+
					"must give the same instant, or a caller cannot recompute an occurrence "+
					"instead of storing it", c.rule, c.anchor, c.after, first, again)
			}
		}
	}
}

// TestNextOccurrence_MonthlyBoundaryIsAlsoStrict mirrors
// TestNextOccurrence_IsStrictlyAfter for the monthly branch, which runs its
// own independent loop and its own comparison — the yearly test proves
// nothing about it.
func TestNextOccurrence_MonthlyBoundaryIsAlsoStrict(t *testing.T) {
	anchor := Anchor{Day: 10}
	occurrence := recAt(2027, time.June, 10, RecurrenceAnchorHour)

	got := NextOccurrence(RuleMonthly, anchor, occurrence)
	if !got.After(occurrence) {
		t.Fatalf("at the occurrence itself, NextOccurrence = %v, want strictly after %v — a "+
			"trigger that re-arms onto its own instant fires forever", got, occurrence)
	}
	if want := recAt(2027, time.July, 10, RecurrenceAnchorHour); !got.Equal(want) {
		t.Errorf("NextOccurrence = %v, want %v", got, want)
	}
}

// TestNextOccurrence_MonthlyIgnoresTheAnchorMonth proves what Anchor's own
// doc comment asserts. Every other monthly fixture in this file leaves
// Month at its zero value, so a regression that started reading it on the
// monthly path would go unnoticed.
func TestNextOccurrence_MonthlyIgnoresTheAnchorMonth(t *testing.T) {
	after := recAt(2027, time.March, 1, 0)
	want := recAt(2027, time.March, 20, RecurrenceAnchorHour)

	for _, month := range []time.Month{0, time.January, time.July, time.December} {
		got := NextOccurrence(RuleMonthly, Anchor{Month: month, Day: 20}, after)
		if !got.Equal(want) {
			t.Errorf("with Anchor.Month = %v, NextOccurrence = %v, want %v — a monthly "+
				"recurrence is \"this day, every month\" and must not read the anchor's month",
				month, got, want)
		}
	}
}

// TestNextOccurrence_UnknownRuleFallsBackToYearly pins what an
// out-of-vocabulary Rule does, because a corrupt or future row can carry
// one and silence is the worst answer.
//
// It resolves as yearly rather than panicking or returning a zero instant.
// The reasoning: this is a pure function with no error return, a zero
// time.Time would arm a trigger in year 1, and classify already degrades an
// unknown recurrence_rule to nil upstream — so a value reaching here at all
// means the row predates that decoding, and the anchor it carries is still
// the user's own stated month and day.
func TestNextOccurrence_UnknownRuleFallsBackToYearly(t *testing.T) {
	anchor := Anchor{Month: time.September, Day: 4}
	after := recAt(2027, time.January, 1, 0)
	want := recAt(2027, time.September, 4, RecurrenceAnchorHour)

	for _, rule := range []Rule{"", "weekly", "YEARLY", "daily"} {
		if got := NextOccurrence(rule, anchor, after); !got.Equal(want) {
			t.Errorf("Rule(%q): NextOccurrence = %v, want %v — an unrecognised rule resolves as "+
				"yearly, which keeps the user's own anchor rather than inventing a cadence",
				rule, got, want)
		}
	}
}
