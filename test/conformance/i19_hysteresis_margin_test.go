// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"testing"

	"github.com/rengo/nooma/internal/core/focus"
)

// TestI19_ChallengerMustExceedRelativeMargin proves invariant I19
// (docs/06-harness.md §4, docs/02-cognitive-core.md §3): a challenger must
// beat the incumbent by MORE than hysteresis_margin — relative, as a ratio
// of the incumbent's own score (spec R4.3, design D8, owner ruling 6) — to
// displace it from the focus.
//
// This is written at both L1 (internal/core/focus/hysteresis_test.go's
// TestDisplaces_RelativeMarginBoundaryTable) and here at L2, per task 4a.1:
// both prove the identical rule, at two levels. m2a ships no store and no
// brain — focus.Displaces is a pure function with nothing yet wired around
// it — so unlike I02 or I13, this invariant's L2 proof cannot yet be a
// whole-pipeline or DDL-parsing test; it is the invariant re-asserted
// through the conformance suite's own naming and registration discipline
// (docs/06-harness.md §4: "each test carries in its name the section it
// verifies"), calling the package's real exported surface directly rather
// than reflecting over it structurally. The DDL half of this invariant's
// story — pinning focus.DefaultHysteresisMargin to migration 0002's
// column DEFAULT — is test/conformance/focus_margin_ddl_test.go, PR 4b's
// own file, once the constant exists to pin.
func TestI19_ChallengerMustExceedRelativeMargin(t *testing.T) {
	const incumbent = 0.60
	const margin = 0.05

	tests := []struct {
		name       string
		challenger float64
		want       bool
	}{
		{"at the incumbent's own score", incumbent, false},
		{"exactly at the margin", incumbent * (1 + margin), false},
		{"beyond the margin", incumbent*(1+margin) + 1e-9, true},
		{"below the incumbent", incumbent - 0.01, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := focus.Displaces(tc.challenger, incumbent, margin)
			if got != tc.want {
				t.Errorf("focus.Displaces(%v, %v, %v) = %v, want %v — I19: a challenger must exceed the incumbent by more than hysteresis_margin, relative",
					tc.challenger, incumbent, margin, got, tc.want)
			}
		})
	}
}
