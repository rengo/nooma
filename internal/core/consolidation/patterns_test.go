package consolidation

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
)

// TestEvaluateStagnation_GoalFacetAtExactlyStagnationDays_Fires is the C14
// length guard: it must fail against a nil stub's zero-length result before
// any content assertion runs (spec R5.1).
func TestEvaluateStagnation_GoalFacetAtExactlyStagnationDays_Fires(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	stagnationDays := 21
	bs := []Belief{
		{ID: "b1", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/quit-smoking",
			LastReinforcedAt: now.Add(-21 * 24 * time.Hour)},
	}

	got := EvaluateStagnation(bs, stagnationDays, now)
	if len(got) != 1 {
		t.Fatalf("EvaluateStagnation() returned %d findings, want 1", len(got))
	}
	want := StagnationFinding{BeliefID: "b1", TopicKey: "derived/goal/quit-smoking", StagnantDays: 21}
	if got[0] != want {
		t.Errorf("EvaluateStagnation()[0] = %+v, want %+v", got[0], want)
	}
}

// TestEvaluateStagnation_AHairUnderStagnationDays_DoesNotFire proves R5.1's
// boundary is inclusive on the ">=" side only: one hour short of the window
// produces no finding.
func TestEvaluateStagnation_AHairUnderStagnationDays_DoesNotFire(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	stagnationDays := 21
	bs := []Belief{
		{ID: "b1", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/quit-smoking",
			LastReinforcedAt: now.Add(-21*24*time.Hour + time.Hour)},
	}

	got := EvaluateStagnation(bs, stagnationDays, now)
	if len(got) != 0 {
		t.Fatalf("EvaluateStagnation() = %+v, want none — a hair under the window must not fire", got)
	}
}

// TestEvaluateStagnation_NonGoalFacetSkippedRegardlessOfElapsedTime proves a
// belief of any other facet never produces a finding, however stagnant.
func TestEvaluateStagnation_NonGoalFacetSkippedRegardlessOfElapsedTime(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	longAgo := now.Add(-1000 * 24 * time.Hour)

	for _, f := range selfmodel.AllFacets() {
		if f == selfmodel.FacetGoal {
			continue
		}
		t.Run(string(f), func(t *testing.T) {
			bs := []Belief{{ID: "b1", Facet: f, TopicKey: "derived/" + string(f) + "/x", LastReinforcedAt: longAgo}}
			got := EvaluateStagnation(bs, 21, now)
			if len(got) != 0 {
				t.Fatalf("EvaluateStagnation() = %+v, want none for facet %v regardless of elapsed time", got, f)
			}
		})
	}
}

// TestEvaluateStagnation_FutureLastReinforcedAt_ClampsToZeroAndNeverStagnant
// proves clock skew (a backdated import, or a clock that moved backwards
// between passes) clamps elapsed time at zero rather than going negative —
// the same saturate-rather-than-invert rule Effective, AgeRamp and
// ExpireIncomplete's clock-skew guard already apply.
func TestEvaluateStagnation_FutureLastReinforcedAt_ClampsToZeroAndNeverStagnant(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	future := now.Add(10 * 24 * time.Hour)
	bs := []Belief{{ID: "b1", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/x", LastReinforcedAt: future}}

	got := EvaluateStagnation(bs, 21, now)
	if len(got) != 0 {
		t.Fatalf("EvaluateStagnation() = %+v, want none — a future LastReinforcedAt must clamp to zero elapsed", got)
	}
}

// TestEvaluateStagnation_OutputSortedByBeliefID is the mutation guard
// against a missing sort: three beliefs, all qualifying, handed in reverse
// order.
func TestEvaluateStagnation_OutputSortedByBeliefID(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * 24 * time.Hour)
	bs := []Belief{
		{ID: "charlie", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/c", LastReinforcedAt: stale},
		{ID: "alice", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/a", LastReinforcedAt: stale},
		{ID: "bob", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/b", LastReinforcedAt: stale},
	}

	got := EvaluateStagnation(bs, 21, now)
	if len(got) != 3 {
		t.Fatalf("EvaluateStagnation() returned %d findings, want 3", len(got))
	}
	want := []string{"alice", "bob", "charlie"}
	for i, id := range want {
		if got[i].BeliefID != id {
			t.Fatalf("EvaluateStagnation() findings[%d].BeliefID = %q, want %q — must be sorted", i, got[i].BeliefID, id)
		}
	}
}
