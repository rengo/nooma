package learning

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/relation"
)

var learnNow = time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)

var defaults = relation.Thresholds{Persist: relation.DefaultMinConfidenceToPersist, Surface: relation.DefaultMinConfidenceToSurface}

// verdicts builds rejects then confirms on Nooma's relations, an hour apart
// and newest last, the oldest a day before now.
func verdicts(rejects, confirms int) []Verdict {
	var out []Verdict
	at := learnNow.Add(-24 * time.Hour)
	for i := range rejects + confirms {
		out = append(out, Verdict{SignalID: fmt.Sprintf("s-%02d", i), Rejected: i < rejects,
			CreatedBy: relation.CreatedByConsolidation, At: at.Add(time.Duration(i) * time.Hour)})
	}
	return out
}

func step(t *testing.T, s TypeState, vs []Verdict) (Change, bool) {
	t.Helper()
	return StepRelation(s, vs, learnNow)
}

// TestStepRelation_SixOfTheLastEightRejectedRaisesPersist is doc 02 §9's own
// example: 6 of the last 8 rejected raises the persist bar 0.30 -> 0.35.
func TestStepRelation_SixOfTheLastEightRejectedRaisesPersist(t *testing.T) {
	c, ok := step(t, TypeState{Current: defaults}, verdicts(6, 2))
	if !ok {
		t.Fatal("no change, want the bar raised")
	}
	if c.Direction != Raised || c.Previous != defaults || c.Next != (relation.Thresholds{Persist: 0.35, Surface: 0.50}) {
		t.Errorf("change = %+v, want raised 0.30 -> 0.35, surface kept", c)
	}
	if c.Rejected != 6 || c.Confirmed != 2 || len(c.SignalIDs) != 8 {
		t.Errorf("evidence = %d rejected, %d confirmed, %d signals; want 6, 2, 8", c.Rejected, c.Confirmed, len(c.SignalIDs))
	}
}

// TestStepRelation_DeletionsAloneAreEvidence: the client deletes and never
// confirms. Six deletions are enough on their own.
func TestStepRelation_DeletionsAloneAreEvidence(t *testing.T) {
	if c, ok := step(t, TypeState{Current: defaults}, verdicts(6, 0)); !ok || c.Next.Persist != 0.35 {
		t.Errorf("six deletions: %+v, %v; want raised to 0.35", c, ok)
	}
	if c, ok := step(t, TypeState{Current: defaults}, verdicts(5, 0)); ok {
		t.Errorf("five deletions moved the bar: %+v", c)
	}
}

// TestStepRelation_TheMiddleHolds is the hysteresis band: 3 to 5 rejections
// of 8 move nothing either way.
func TestStepRelation_TheMiddleHolds(t *testing.T) {
	for rejects := 3; rejects <= 5; rejects++ {
		if c, ok := step(t, TypeState{Current: defaults}, verdicts(rejects, 8-rejects)); ok {
			t.Errorf("%d of 8 rejected moved the bar: %+v", rejects, c)
		}
	}
}

// TestStepRelation_SixConfirmedLowersPersist: bolder for whoever accepts. The
// surface bar is not lowered by learning.
func TestStepRelation_SixConfirmedLowersPersist(t *testing.T) {
	c, ok := step(t, TypeState{Current: defaults}, verdicts(2, 6))
	if !ok || c.Direction != Lowered || c.Next != (relation.Thresholds{Persist: 0.25, Surface: 0.50}) {
		t.Errorf("change = %+v, %v; want lowered 0.30 -> 0.25, surface kept", c, ok)
	}
}

// TestStepRelation_OnlyTheNewestEightCount: older verdicts fall out of the
// sample. Ten verdicts whose oldest two are rejections leave 5 of the newest
// 8 rejected, which holds; counting all ten (7 rejected) would raise.
func TestStepRelation_OnlyTheNewestEightCount(t *testing.T) {
	vs := verdicts(7, 3)
	if c, ok := step(t, TypeState{Current: defaults}, vs); ok {
		t.Errorf("moved on verdicts outside the newest 8: %+v", c)
	}
	// Order of the input does not matter: the newest are found by time.
	reversed := make([]Verdict, len(vs))
	for i, v := range vs {
		reversed[len(vs)-1-i] = v
	}
	if c, ok := step(t, TypeState{Current: defaults}, reversed); ok {
		t.Errorf("input order changed the sample: %+v", c)
	}
}

// TestStepRelation_TheSampleIsNewestFirstAndTieBrokenByID: the signal ids
// recorded are the sample, newest first; two at one instant order by id.
func TestStepRelation_TheSampleIsNewestFirstAndTieBrokenByID(t *testing.T) {
	vs := verdicts(6, 0)
	vs[4].At = vs[5].At // s-04 and s-05 at one instant
	c, ok := step(t, TypeState{Current: defaults}, vs)
	if !ok {
		t.Fatal("no change")
	}
	want := []string{"s-05", "s-04", "s-03", "s-02", "s-01", "s-00"}
	for i, id := range want {
		if c.SignalIDs[i] != id {
			t.Fatalf("sample = %v, want %v", c.SignalIDs, want)
		}
	}
}

// TestStepRelation_OnlyNoomasRelationsCount: a verdict on a relation the user
// made says nothing about how Nooma judges.
func TestStepRelation_OnlyNoomasRelationsCount(t *testing.T) {
	vs := verdicts(6, 0)
	for i := range vs {
		vs[i].CreatedBy = relation.CreatedByUser
	}
	if c, ok := step(t, TypeState{Current: defaults}, vs); ok {
		t.Errorf("moved on the user's own relations: %+v", c)
	}
	vs[0].CreatedBy = relation.CreatedBySystem // capture's
	for i := 1; i < len(vs); i++ {
		vs[i].CreatedBy = relation.CreatedByConsolidation
	}
	if _, ok := step(t, TypeState{Current: defaults}, vs); !ok {
		t.Error("capture's and the nightly pass's relations together did not count")
	}
}

// TestStepRelation_EvidenceResetsAtTheLastChange: what was counted for a
// change is not counted again. Only verdicts strictly after it are evidence.
func TestStepRelation_EvidenceResetsAtTheLastChange(t *testing.T) {
	vs := verdicts(6, 0)
	adjusted := vs[0].At // the oldest verdict is at the change: not after it
	s := TypeState{Current: defaults, AdjustedAt: &adjusted}
	if c, ok := StepRelation(s, vs, adjusted.Add(RelationCooldownDays*24*time.Hour)); ok {
		t.Errorf("counted a verdict from the instant of the last change: %+v", c)
	}
}

// TestStepRelation_CooldownHoldsForSevenDays: a type changed less than
// RelationCooldownDays ago is not judged again, however much fresh evidence
// it has. The boundary is inclusive: seven days to the second may move.
func TestStepRelation_CooldownHoldsForSevenDays(t *testing.T) {
	adjusted := learnNow.Add(-RelationCooldownDays*24*time.Hour + time.Second)
	fresh := verdicts(6, 0) // all within the last day, after the change
	if c, ok := step(t, TypeState{Current: defaults, AdjustedAt: &adjusted}, fresh); ok {
		t.Errorf("moved inside the cooldown: %+v", c)
	}
	adjusted = learnNow.Add(-RelationCooldownDays * 24 * time.Hour)
	if _, ok := step(t, TypeState{Current: defaults, AdjustedAt: &adjusted}, fresh); !ok {
		t.Error("held at exactly the end of the cooldown")
	}
}

// TestStepRelation_APinnedTypeIsNeverWritten: the user's word wins.
func TestStepRelation_APinnedTypeIsNeverWritten(t *testing.T) {
	for _, vs := range [][]Verdict{verdicts(8, 0), verdicts(0, 8)} {
		if c, ok := step(t, TypeState{Current: defaults, Pinned: true}, vs); ok {
			t.Errorf("a pinned type moved: %+v", c)
		}
	}
}

// TestStepRelation_TheBarStaysWithinItsBounds: persist never leaves
// [MinLearnedPersist, MaxLearnedPersist]; at a bound a step that would cross
// it changes nothing, and a step that would overshoot lands on it.
func TestStepRelation_TheBarStaysWithinItsBounds(t *testing.T) {
	cases := []struct {
		name        string
		persist     float64
		rejects     int
		wantOK      bool
		wantPersist float64
	}{
		{"raise at the ceiling", MaxLearnedPersist, 6, false, 0},
		{"raise near the ceiling", 0.88, 6, true, MaxLearnedPersist},
		{"lower at the floor", MinLearnedPersist, 0, false, 0},
		{"lower near the floor", 0.22, 0, true, MinLearnedPersist},
		{"lower from above the ceiling", 0.95, 0, true, MaxLearnedPersist},
		{"raise from below the floor", 0.10, 6, true, MinLearnedPersist},
		{"raise from above the ceiling never lowers", 0.95, 6, false, 0},
		{"lower from below the floor never raises", 0.10, 0, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs := verdicts(tc.rejects, 8-tc.rejects)
			if tc.rejects == 0 {
				vs = verdicts(0, 8)
			}
			c, ok := step(t, TypeState{Current: relation.Thresholds{Persist: tc.persist, Surface: 1}}, vs)
			if ok != tc.wantOK || ok && c.Next.Persist != tc.wantPersist {
				t.Errorf("persist %v: %+v, %v; want ok %v at %v", tc.persist, c, ok, tc.wantOK, tc.wantPersist)
			}
		})
	}
}

// TestStepRelation_SurfaceFollowsPersistUp: persist never rises above
// surface; the uncertain band empties rather than inverts.
func TestStepRelation_SurfaceFollowsPersistUp(t *testing.T) {
	c, ok := step(t, TypeState{Current: relation.Thresholds{Persist: 0.50, Surface: 0.50}}, verdicts(6, 0))
	if !ok || c.Next != (relation.Thresholds{Persist: 0.55, Surface: 0.55}) {
		t.Errorf("change = %+v, %v; want 0.55/0.55", c, ok)
	}
}

// TestStepRelation_StepsLandOnHundredths: 0.30 + 0.05 is 0.35, not
// 0.35000000000000003, so the stored and the shown value agree.
func TestStepRelation_StepsLandOnHundredths(t *testing.T) {
	s := TypeState{Current: defaults}
	for range 10 {
		c, ok := step(t, s, verdicts(6, 0))
		if !ok {
			break
		}
		if r := math.Round(c.Next.Persist*100) / 100; c.Next.Persist != r {
			t.Fatalf("persist %v is not on a hundredth", c.Next.Persist)
		}
		s.Current = c.Next
	}
}

// TestStepRelation_IsIdempotent: running the pass again on the same evidence,
// once its change is applied, changes nothing.
func TestStepRelation_IsIdempotent(t *testing.T) {
	vs := verdicts(6, 2)
	c, ok := step(t, TypeState{Current: defaults}, vs)
	if !ok {
		t.Fatal("no first change")
	}
	applied := learnNow
	again := TypeState{Current: c.Next, AdjustedAt: &applied}
	if c2, ok := step(t, again, vs); ok {
		t.Errorf("the same evidence moved the bar twice: %+v", c2)
	}
	if c2, ok := StepRelation(again, vs, learnNow.Add(30*24*time.Hour)); ok {
		t.Errorf("the same evidence moved the bar again after the cooldown: %+v", c2)
	}
}

// TestStepRelation_IgnoresWhatIsNotYetTrue: a verdict after now (a clock that
// went back) is not evidence, and a bar that is not a finite number is left
// for a person rather than stepped from.
func TestStepRelation_IgnoresWhatIsNotYetTrue(t *testing.T) {
	future := verdicts(6, 0)
	for i := range future {
		future[i].At = learnNow.Add(time.Duration(i+1) * time.Minute)
	}
	if c, ok := step(t, TypeState{Current: defaults}, future); ok {
		t.Errorf("moved on verdicts from the future: %+v", c)
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1)} {
		if c, ok := step(t, TypeState{Current: relation.Thresholds{Persist: bad, Surface: 0.5}}, verdicts(6, 0)); ok {
			t.Errorf("stepped from persist %v: %+v", bad, c)
		}
	}
}

// TestRelationLearningConstants_ArePinned: the shape tests size from the
// constants, so only literals catch a recalibration nobody meant to make
// (doc 02 §13).
func TestRelationLearningConstants_ArePinned(t *testing.T) {
	if RelationSampleSize != 8 || RelationVerdictsToMove != 6 || RelationThresholdStep != 0.05 ||
		MinLearnedPersist != 0.20 || MaxLearnedPersist != 0.90 || RelationCooldownDays != 7 {
		t.Error("a relation learning constant moved from doc 02 §13's value")
	}
	if RelationVerdictsToMove*2 <= RelationSampleSize {
		t.Error("both directions could move on one sample: the threshold to move must be a majority")
	}
}
