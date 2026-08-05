package focus

import (
	"math"
	"reflect"
	"testing"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestTypes proves spec R4.1's vocabulary: KindLoad selects exactly
// mental_load, KindTask selects exactly task and event, and every call
// returns a fresh slice — mutating one call's result must never affect the
// next (Types is a function, never an exported var, R4.1's own MUST).
func TestTypes(t *testing.T) {
	t.Run("KindLoad is exactly mental_load", func(t *testing.T) {
		got := Types(KindLoad)
		want := []unit.Type{unit.TypeMentalLoad}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Types(KindLoad) = %v, want %v", got, want)
		}
	})

	t.Run("KindTask is exactly task and event", func(t *testing.T) {
		got := Types(KindTask)
		want := []unit.Type{unit.TypeTask, unit.TypeEvent}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Types(KindTask) = %v, want %v", got, want)
		}
	})

	t.Run("fresh slice each call", func(t *testing.T) {
		first := Types(KindTask)
		if len(first) == 0 {
			t.Fatal("Types(KindTask) returned zero elements — nothing to mutate, the guard this subtest needs")
		}
		first[0] = "mutated"

		second := Types(KindTask)
		if second[0] == "mutated" {
			t.Errorf("Types(KindTask) returned a shared backing array — mutating one call's result changed the next call's, want a fresh slice each time")
		}
	})
}

// TestAllKinds_IsExhaustive proves AllKinds enumerates exactly the Kind
// vocabulary, in declaration order — the same contract unit.AllTypes and
// unit.AllStatuses already keep.
func TestAllKinds_IsExhaustive(t *testing.T) {
	got := AllKinds()
	want := []Kind{KindTask, KindLoad}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AllKinds() = %v, want %v", got, want)
	}
}

// TestSelect_TypeCriterionOverridesRawScore proves spec R4.1: Select(KindTask, …)
// returns only task/event members even when a mental_load unit outranks
// them by score, and Select(KindLoad, …) returns only mental_load members.
// "load-high" carries by far the highest Score of the three — if type
// filtering did not apply, it would appear in the task focus's top slots.
func TestSelect_TypeCriterionOverridesRawScore(t *testing.T) {
	ranked := []Ranked{
		{Candidate: Candidate{ID: "load-high", Type: unit.TypeMentalLoad}, Score: 5.0},
		{Candidate: Candidate{ID: "event-mid", Type: unit.TypeEvent}, Score: 0.8},
		{Candidate: Candidate{ID: "task-low", Type: unit.TypeTask}, Score: 0.5},
	}

	gotTask := Select(KindTask, ranked, Selection{}, 0.05, DefaultSize)
	wantTask := []string{"event-mid", "task-low"}
	if !reflect.DeepEqual(gotTask.Members, wantTask) {
		t.Errorf("Select(KindTask, …).Members = %v, want %v (load-high must never appear in the task focus)", gotTask.Members, wantTask)
	}
	if gotTask.Kind != KindTask {
		t.Errorf("Select(KindTask, …).Kind = %v, want %v", gotTask.Kind, KindTask)
	}

	gotLoad := Select(KindLoad, ranked, Selection{}, 0.05, DefaultSize)
	wantLoad := []string{"load-high"}
	if !reflect.DeepEqual(gotLoad.Members, wantLoad) {
		t.Errorf("Select(KindLoad, …).Members = %v, want %v (task/event members must never appear in the load focus)", gotLoad.Members, wantLoad)
	}
}

// TestSelect_EmptyPreviousReducesToPlainTopN proves spec R4.5: an empty
// previous (the first computation after a process start) performs no
// hysteresis comparison at all — it is a plain top-size by Score.
func TestSelect_EmptyPreviousReducesToPlainTopN(t *testing.T) {
	ranked := []Ranked{
		{Candidate: Candidate{ID: "a", Type: unit.TypeTask}, Score: 3.0},
		{Candidate: Candidate{ID: "b", Type: unit.TypeTask}, Score: 2.0},
		{Candidate: Candidate{ID: "c", Type: unit.TypeTask}, Score: 1.0},
	}

	got := Select(KindTask, ranked, Selection{}, 0.05, 2)
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got.Members, want) {
		t.Errorf("Select(KindTask, ranked, Selection{}, 0.05, 2).Members = %v, want %v", got.Members, want)
	}
}

// TestSelect_IncumbentAbsentFromRanked_DroppedWithNoContest proves spec
// R4.5's second half: an incumbent no longer present in ranked — archived
// since, or now the wrong type for this Kind — is simply absent from the
// result and blocks nobody else's slot.
func TestSelect_IncumbentAbsentFromRanked_DroppedWithNoContest(t *testing.T) {
	previous := Selection{Kind: KindTask, Members: []string{"gone"}}
	ranked := []Ranked{
		{Candidate: Candidate{ID: "a", Type: unit.TypeTask}, Score: 1.0},
	}

	got := Select(KindTask, ranked, previous, 0.05, DefaultSize)
	want := []string{"a"}
	if !reflect.DeepEqual(got.Members, want) {
		t.Errorf("Select(...).Members = %v, want %v ('gone' must not block 'a' or appear itself)", got.Members, want)
	}
}

// TestSelect_AgreesWithDisplaces proves spec R4.8: Select's adjusted-sort
// implementation and the Displaces predicate are two spellings of one
// rule, and they must agree over a boundary table including all three of
// R4.3's edge cases, plus the non-finite cases hysteresis_test.go's
// TestDisplaces_NonFinite decided for Displaces directly — a deliberate
// widening beyond R4.8's own literal ask, so the two spellings stay
// provably equal on the highest-risk boundary too, not only on the finite
// one spec.md states.
//
// Each case is a one-slot contest between exactly one incumbent ("inc")
// and one challenger ("chal"): whichever Displaces(chal, inc, margin)
// declares should be the sole survivor.
func TestSelect_AgreesWithDisplaces(t *testing.T) {
	const incumbentScore = 0.60
	const margin = 0.05

	tests := []struct {
		name            string
		challengerScore float64
	}{
		{"challenger == incumbent", incumbentScore},
		{"challenger == incumbent*(1+margin) exactly", incumbentScore * (1 + margin)},
		{"challenger == incumbent*(1+margin) + epsilon", incumbentScore*(1+margin) + 1e-9},
		{"challenger < incumbent", incumbentScore - 0.01},
		{"NaN incumbent, finite challenger", 0.01},
		{"NaN challenger, finite incumbent", math.NaN()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inc := incumbentScore
			if tc.name == "NaN incumbent, finite challenger" {
				inc = math.NaN()
			}

			displaces := Displaces(tc.challengerScore, inc, margin)
			wantID := "inc"
			if displaces {
				wantID = "chal"
			}

			ranked := []Ranked{
				{Candidate: Candidate{ID: "inc", Type: unit.TypeTask}, Score: inc},
				{Candidate: Candidate{ID: "chal", Type: unit.TypeTask}, Score: tc.challengerScore},
			}
			previous := Selection{Kind: KindTask, Members: []string{"inc"}}

			got := Select(KindTask, ranked, previous, margin, 1)
			if len(got.Members) != 1 || got.Members[0] != wantID {
				t.Errorf("Select(...).Members = %v, want exactly [%q] (Displaces(%v, %v, %v) = %v)",
					got.Members, wantID, tc.challengerScore, inc, margin, displaces)
			}
		})
	}
}

// TestSelect_IndependentIncumbentSets proves spec R4.7: the task focus and
// the load focus each keep their own incumbent set, even when computed
// from the same ranked slice in the same call sequence. "l-chal" displaces
// "l-inc" in the load focus (Score 2.0 against 1.0, well beyond the 0.05
// margin); "t-inc" must be entirely unaffected by that same call.
func TestSelect_IndependentIncumbentSets(t *testing.T) {
	ranked := []Ranked{
		{Candidate: Candidate{ID: "t-inc", Type: unit.TypeTask}, Score: 1.0},
		{Candidate: Candidate{ID: "t-chal", Type: unit.TypeTask}, Score: 0.9},
		{Candidate: Candidate{ID: "l-inc", Type: unit.TypeMentalLoad}, Score: 1.0},
		{Candidate: Candidate{ID: "l-chal", Type: unit.TypeMentalLoad}, Score: 2.0},
	}

	gotTask := Select(KindTask, ranked, Selection{Kind: KindTask, Members: []string{"t-inc"}}, 0.05, 1)
	if len(gotTask.Members) != 1 || gotTask.Members[0] != "t-inc" {
		t.Errorf("task focus Members = %v, want [\"t-inc\"] — unaffected by the load focus's own displacement in the same call sequence", gotTask.Members)
	}

	gotLoad := Select(KindLoad, ranked, Selection{Kind: KindLoad, Members: []string{"l-inc"}}, 0.05, 1)
	if len(gotLoad.Members) != 1 || gotLoad.Members[0] != "l-chal" {
		t.Errorf("load focus Members = %v, want [\"l-chal\"] — l-chal must displace l-inc", gotLoad.Members)
	}
}
