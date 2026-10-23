package graph

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

func TestNeighbours_RanksByStrongestEdgeThenConfidenceThenID(t *testing.T) {
	edges := []Edge{
		{ID: "e1", From: "c", To: "weak", Strength: 0.2, Confidence: 0.9},
		{ID: "e2", From: "strong", To: "c", Strength: 0.9, Confidence: 0.4},
		// "twice" has a weak edge and a strong one: the strongest decides.
		{ID: "e4", From: "twice", To: "c", Strength: 0.6, Confidence: 0.5},
		{ID: "e3", From: "c", To: "twice", Strength: 0.1, Confidence: 0.9},
		// "tie-b" and "tie-a" tie on strength; confidence breaks it.
		{ID: "e5", From: "c", To: "tie-b", Strength: 0.4, Confidence: 0.8},
		{ID: "e6", From: "c", To: "tie-a", Strength: 0.4, Confidence: 0.3},
		// "same-z" and "same-y" tie on both; the id breaks it.
		{ID: "e7", From: "c", To: "same-z", Strength: 0.3, Confidence: 0.5},
		{ID: "e8", From: "c", To: "same-y", Strength: 0.3, Confidence: 0.5},
		// "pair-x" ties "pair-w" on strength; its highest confidence, not
		// its last, puts it first.
		{ID: "e9", From: "c", To: "pair-x", Strength: 0.35, Confidence: 0.9},
		{ID: "e10", From: "pair-x", To: "c", Strength: 0.35, Confidence: 0.1},
		{ID: "e11", From: "c", To: "pair-w", Strength: 0.35, Confidence: 0.5},
	}

	ids, omitted := Neighbours("c", edges)

	want := []string{"strong", "twice", "tie-b", "tie-a", "pair-x", "pair-w", "same-y", "same-z", "weak"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("Neighbours = %v, want %v", ids, want)
	}
	if omitted != 0 {
		t.Fatalf("omitted = %d, want 0", omitted)
	}
}

func TestNeighbours_IgnoresEdgesNotTouchingTheCentreAndSelfLoops(t *testing.T) {
	edges := []Edge{
		{ID: "e1", From: "a", To: "b", Strength: 1},
		{ID: "e2", From: "c", To: "c", Strength: 1},
		{ID: "e3", From: "c", To: "n", Strength: 0.5},
	}

	ids, omitted := Neighbours("c", edges)

	if !reflect.DeepEqual(ids, []string{"n"}) || omitted != 0 {
		t.Fatalf("Neighbours = %v, %d, want [n], 0", ids, omitted)
	}
}

func TestNeighbours_CapsAtMaxNeighboursAndCountsTheRest(t *testing.T) {
	var edges []Edge
	total := MaxNeighbours + 3
	for i := 0; i < total; i++ {
		// Strength rises with i, so the last MaxNeighbours are kept.
		edges = append(edges, Edge{
			ID: fmt.Sprintf("e%02d", i), From: "c", To: fmt.Sprintf("n%02d", i),
			Strength: float64(i) / float64(total),
		})
	}

	ids, omitted := Neighbours("c", edges)

	if len(ids) != MaxNeighbours {
		t.Fatalf("len(ids) = %d, want %d", len(ids), MaxNeighbours)
	}
	if omitted != 3 {
		t.Fatalf("omitted = %d, want 3", omitted)
	}
	if ids[0] != fmt.Sprintf("n%02d", total-1) || ids[MaxNeighbours-1] != "n03" {
		t.Fatalf("kept %v, want the %d strongest, n%02d first and n03 last", ids, MaxNeighbours, total-1)
	}
}

// A NaN strength (relations.strength is a REAL column with no CHECK) ranks
// last rather than breaking the order: a sort whose comparison is not a
// strict weak ordering returns an order that depends on the input's.
func TestNeighbours_NaNStrengthRanksLastWhateverTheInputOrder(t *testing.T) {
	a := Edge{ID: "e1", From: "c", To: "nan", Strength: math.NaN(), Confidence: 1}
	b := Edge{ID: "e2", From: "c", To: "low", Strength: 0.1}
	c := Edge{ID: "e3", From: "c", To: "high", Strength: 0.9}
	want := []string{"high", "low", "nan"}

	for _, edges := range [][]Edge{{a, b, c}, {c, b, a}, {b, a, c}} {
		ids, _ := Neighbours("c", edges)
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("Neighbours(%v) = %v, want %v", edges, ids, want)
		}
	}
}

func TestNeighbours_NoEdgesIsNoNeighbours(t *testing.T) {
	ids, omitted := Neighbours("c", nil)
	if len(ids) != 0 || omitted != 0 {
		t.Fatalf("Neighbours(nil) = %v, %d, want none", ids, omitted)
	}
}

func TestWithin_KeepsEdgesBetweenNodesOnceOrderedByID(t *testing.T) {
	edges := []Edge{
		{ID: "e3", From: "a", To: "b"},
		{ID: "e1", From: "c", To: "a"},
		{ID: "e2", From: "a", To: "outside"},
		// The same edge read twice, once from each endpoint's relations.
		{ID: "e3", From: "a", To: "b"},
		{ID: "e4", From: "b", To: "c"},
		// A relation from a unit to itself joins nothing to draw.
		{ID: "e0", From: "a", To: "a"},
	}

	got := Within([]string{"c", "a", "b"}, edges)

	var ids []string
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	if !reflect.DeepEqual(ids, []string{"e1", "e3", "e4"}) {
		t.Fatalf("Within = %v, want [e1 e3 e4]", ids)
	}
}
