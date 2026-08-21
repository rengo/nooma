// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
)

// TestI17_EveryOccurrenceCarriesTheAnchorNotThePrevious proves invariant
// I17's arithmetic half (docs/06-harness.md §4, docs/02-cognitive-core.md
// §7): firing a recurring trigger creates the next one from the anchor, so
// the series never drifts off the date the user named.
//
// The "same source unit" half is a caller obligation and is not provable
// here — prospection holds no unit — so this test states the half that is,
// and says which one it is not, rather than implying both (spec R5.1's own
// scoping).
//
// It is written as a walk rather than as a pair of examples. The drift this
// guards against is invisible for one step and only appears across a leap
// cycle: advance 29 February by a year and you get 28 February; advance
// THAT and you get 28 February forever. A two-step test passes on the
// broken implementation.
func TestI17_EveryOccurrenceCarriesTheAnchorNotThePrevious(t *testing.T) {
	loc := time.FixedZone("UTC-3", -3*60*60)

	cases := map[string]struct {
		rule   prospection.Rule
		anchor prospection.Anchor
		// wantDay reports the day of month the series must land on in the
		// given occurrence, computed from the calendar rather than from the
		// implementation.
		wantDay func(t time.Time) int
	}{
		"a 29 February anniversary": {
			rule:   prospection.RuleYearly,
			anchor: prospection.Anchor{Month: time.February, Day: 29},
			wantDay: func(t time.Time) int {
				y := t.Year()
				if y%4 == 0 && (y%100 != 0 || y%400 == 0) {
					return 29
				}
				return 28
			},
		},
		"a day-31 monthly reminder": {
			rule:   prospection.RuleMonthly,
			anchor: prospection.Anchor{Day: 31},
			wantDay: func(t time.Time) int {
				// The last day of t's own month, found the same way the
				// calendar finds it and not the way the code under test does.
				first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
				return first.AddDate(0, 1, -1).Day()
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cursor := time.Date(2027, time.January, 1, 0, 0, 0, 0, loc)

			for step := range 26 {
				next := prospection.NextOccurrence(tc.rule, tc.anchor, cursor)

				if !next.After(cursor) {
					t.Fatalf("step %d: occurrence %v does not advance past %v — a trigger that "+
						"re-arms onto its own instant fires forever", step, next, cursor)
				}
				if want := tc.wantDay(next); next.Day() != want {
					t.Fatalf("step %d: occurrence %v landed on day %d, want %d — the series has "+
						"drifted off its anchor, which is what re-deriving exists to prevent",
						step, next, next.Day(), want)
				}
				if tc.rule == prospection.RuleYearly && next.Month() != tc.anchor.Month {
					t.Fatalf("step %d: occurrence %v is in %v, want %v — a yearly series must "+
						"never leave the anchor's month", step, next, next.Month(), tc.anchor.Month)
				}

				cursor = next
			}
		})
	}
}
