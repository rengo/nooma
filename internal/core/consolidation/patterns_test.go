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

// TestEvaluateLoad_AtOrAboveThreshold_FiresWithNilLastHypothesisAt is the
// C14 length/presence guard: a qualifying count with no prior hypothesis
// must fire against a stub that always returns false.
func TestEvaluateLoad_AtOrAboveThreshold_FiresWithNilLastHypothesisAt(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)

	got, ok := EvaluateLoad(7, 7, nil, now)
	if !ok {
		t.Fatalf("EvaluateLoad(7, 7, nil, now) ok = false, want true — at-threshold with no prior hypothesis must fire")
	}
	want := LoadFinding{OpenCount: 7, Threshold: 7}
	if got != want {
		t.Errorf("EvaluateLoad(7, 7, nil, now) = %+v, want %+v", got, want)
	}
}

// TestEvaluateLoad_OneBelowThreshold_NeverFires proves the other side of
// R5.2's threshold boundary, with no cooldown in play.
func TestEvaluateLoad_OneBelowThreshold_NeverFires(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)

	_, ok := EvaluateLoad(6, 7, nil, now)
	if ok {
		t.Fatalf("EvaluateLoad(6, 7, nil, now) ok = true, want false — below threshold must never fire")
	}
}

// TestEvaluateLoad_InsideCooldown_ReturnsFalseEvenAboveThreshold proves a
// count above threshold, inside the cooldown, is a decision with no effect
// and writes nothing (doc 02 §11).
func TestEvaluateLoad_InsideCooldown_ReturnsFalseEvenAboveThreshold(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	lastHypothesisAt := now.Add(-3 * 24 * time.Hour)

	_, ok := EvaluateLoad(9, 7, &lastHypothesisAt, now)
	if ok {
		t.Fatalf("EvaluateLoad(9, 7, ...) ok = true, want false — 3 days into a 7-day cooldown must not fire")
	}
}

// TestEvaluateLoad_CooldownBoundary_ExactlyLoadCooldownDaysFires proves
// R5.2's second boundary is inclusive: exactly LoadCooldownDays elapsed
// fires, a hair under does not.
func TestEvaluateLoad_CooldownBoundary_ExactlyLoadCooldownDaysFires(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)

	t.Run("ExactlyAtCooldown_Fires", func(t *testing.T) {
		lastHypothesisAt := now.Add(-time.Duration(LoadCooldownDays) * 24 * time.Hour)
		_, ok := EvaluateLoad(9, 7, &lastHypothesisAt, now)
		if !ok {
			t.Fatalf("EvaluateLoad ok = false, want true — exactly LoadCooldownDays elapsed must fire")
		}
	})

	t.Run("AHairUnderCooldown_DoesNotFire", func(t *testing.T) {
		lastHypothesisAt := now.Add(-time.Duration(LoadCooldownDays)*24*time.Hour + time.Hour)
		_, ok := EvaluateLoad(9, 7, &lastHypothesisAt, now)
		if ok {
			t.Fatalf("EvaluateLoad ok = true, want false — a hair under LoadCooldownDays must not fire")
		}
	})
}
