package consolidation

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestAllReasons_HasExactlyThreeMembers is the C14 length guard: it must
// fail on AllReasons()'s length before any content assertion runs, against
// a zero-value stub that returns nil (spec R1.4).
func TestAllReasons_HasExactlyThreeMembers(t *testing.T) {
	reasons := AllReasons()
	if len(reasons) != 3 {
		t.Fatalf("AllReasons() returned %d reasons, want 3: %v", len(reasons), reasons)
	}

	want := map[Reason]bool{
		ReasonIncompletePromoted:   true,
		ReasonIncompleteExpired:    true,
		ReasonBelowWeightThreshold: true,
	}
	for _, r := range reasons {
		if !want[r] {
			t.Errorf("AllReasons() contains unexpected reason %q", r)
		}
		delete(want, r)
	}
	if len(want) != 0 {
		t.Errorf("AllReasons() is missing reasons: %v", want)
	}
}

// TestAllReasons_ReturnsAFreshSlice guards against a shared backing array:
// mutating one call's result must not affect a later call.
func TestAllReasons_ReturnsAFreshSlice(t *testing.T) {
	a := AllReasons()
	if len(a) == 0 {
		t.Fatal("AllReasons() returned zero reasons — nothing to mutate-guard")
	}
	a[0] = Reason("mutated")

	b := AllReasons()
	if len(b) == 0 {
		t.Fatal("AllReasons() returned zero reasons on the second call — nothing to compare")
	}
	if b[0] == Reason("mutated") {
		t.Fatal("AllReasons() does not return a fresh slice — mutating one call's result affected a later call")
	}
}

// TestEveryEmittedTransitionIsLegal drives every Transition
// ExpireIncomplete and Archive can produce through unit.ValidateTransition
// (spec R1.4), instead of re-asserting the legal (From, To) pairs by hand a
// second time.
//
// Not a missing-symbol red step: both producers already compile and pass
// their own tests earlier in this PR — disclosed per this project's own
// convention (m2a C9) as an exhaustiveness check, not a TDD red step.
func TestEveryEmittedTransitionIsLegal(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)

	var all []Transition
	all = append(all, ExpireIncomplete([]Incomplete{
		{UnitID: "u1", CreatedAt: now.Add(-48 * time.Hour), Unresolved: true},
		{UnitID: "u2", CreatedAt: now.Add(-48 * time.Hour), Unresolved: false},
	}, now)...)

	archived, _ := Archive([]Cold{
		{UnitID: "u3", Status: unit.StatusPool, Weight: 0.1, DecayRate: 0, LastTouchedAt: now},
	}, 0.5, now)
	all = append(all, archived...)

	if len(all) != 3 {
		t.Fatalf("fixture produced %d transitions, want 3 — nothing to exhaustively check", len(all))
	}
	for _, tr := range all {
		if err := unit.ValidateTransition(tr.From, tr.To); err != nil {
			t.Errorf("Transition{From: %v, To: %v} (Reason %v) is not a legal transition: %v", tr.From, tr.To, tr.Reason, err)
		}
	}
}
