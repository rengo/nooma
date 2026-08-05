package focus

import (
	"math"
	"testing"

	"github.com/rengo/nooma/internal/core/weight"
)

// TestAdjacencyStrengths_SingleEdge proves spec R3.7's simplest case: a
// unit joined by one edge to a previous-focus member gets that edge's
// strength as its adjacency.
func TestAdjacencyStrengths_SingleEdge(t *testing.T) {
	previous := Selection{Kind: KindTask, Members: []string{"member-a"}}
	edges := []weight.Edge{{From: "unit-v", To: "member-a", Strength: 0.7}}

	got := AdjacencyStrengths(previous, edges)
	if got["unit-v"] != 0.7 {
		t.Errorf("adjacency[unit-v] = %v, want 0.7", got["unit-v"])
	}
}

// TestAdjacencyStrengths_TwoEdges_TakesMaxNotSum proves spec R3.7's own
// MUST: two edges joining the same unit to two different previous-focus
// members combine by max (0.7), never sum (1.1) — the identical rule
// weight.Resurface's buildAdjacency already uses for the same reason (a
// sum measures graph topology, not relevance, and would need an
// unspecified normalization to stay bounded).
func TestAdjacencyStrengths_TwoEdges_TakesMaxNotSum(t *testing.T) {
	previous := Selection{Kind: KindTask, Members: []string{"member-a", "member-b"}}
	edges := []weight.Edge{
		{From: "unit-v", To: "member-a", Strength: 0.7},
		{From: "unit-v", To: "member-b", Strength: 0.4},
	}

	got := AdjacencyStrengths(previous, edges)
	if got["unit-v"] != 0.7 {
		t.Errorf("adjacency[unit-v] = %v, want 0.7 (max), not 1.1 (sum)", got["unit-v"])
	}
}

// TestAdjacencyStrengths_UndirectedEdge proves spec R3.7's own MUST:
// traversal is undirected — an edge stored with the previous-focus member
// as From and the candidate as To gives the identical result as the
// opposite storage direction, reusing weight.Edge rather than a second
// edge type.
func TestAdjacencyStrengths_UndirectedEdge(t *testing.T) {
	previous := Selection{Kind: KindTask, Members: []string{"member-a"}}
	edges := []weight.Edge{{From: "member-a", To: "unit-v", Strength: 0.7}}

	got := AdjacencyStrengths(previous, edges)
	if got["unit-v"] != 0.7 {
		t.Errorf("adjacency[unit-v] = %v, want 0.7 regardless of edge storage direction", got["unit-v"])
	}
}

// TestAdjacencyStrengths_EmptyPrevious_ReturnsEmptyMap proves spec R3.7's
// restart boundary: an empty previous.Members (the first ranking after a
// process restart) returns an empty map, not one key per edge endpoint —
// every candidate's adjacency reads as 0 (rank.go's own missing-key
// contract), and the term vanishes entirely.
func TestAdjacencyStrengths_EmptyPrevious_ReturnsEmptyMap(t *testing.T) {
	edges := []weight.Edge{{From: "unit-v", To: "unit-w", Strength: 0.9}}

	got := AdjacencyStrengths(Selection{}, edges)
	if len(got) != 0 {
		t.Errorf("AdjacencyStrengths(empty previous, ...) = %v, want an empty map", got)
	}
}

// TestAdjacencyStrengths_UnrelatedUnit_TouchesNoMember proves the other
// half of spec R3.7's "= 0" clause: a unit with an edge to something that
// is NOT a previous-focus member is simply absent from the map.
func TestAdjacencyStrengths_UnrelatedUnit_TouchesNoMember(t *testing.T) {
	previous := Selection{Kind: KindTask, Members: []string{"member-a"}}
	edges := []weight.Edge{{From: "unit-v", To: "unit-w", Strength: 0.9}}

	got := AdjacencyStrengths(previous, edges)
	if _, ok := got["unit-v"]; ok {
		t.Errorf("adjacency[unit-v] = %v, want absent — unit-v touches no previous-focus member", got["unit-v"])
	}
}

// TestAdjacencyStrengths_NaNStrengthEdge_VanishesWithoutAnExplicitGuard
// decides this PR's highest-risk boundary (adjacency.go's own doc
// comment): unlike weight.Resurface's buildAdjacency, which needs an
// explicit math.IsNaN guard because it ALSO reports a corrupt edge
// through a second return value, AdjacencyStrengths' fixed single-value
// signature (spec R3.7) has no room for that report, and needs no guard
// for correctness either — every IEEE 754 comparison against NaN is
// false, so a NaN-strength edge can never win the running max, whether it
// is the only edge seen for a unit or one among several, in either order.
func TestAdjacencyStrengths_NaNStrengthEdge_VanishesWithoutAnExplicitGuard(t *testing.T) {
	previous := Selection{Kind: KindTask, Members: []string{"member-a", "member-b"}}

	t.Run("NaN-only edge leaves the unit absent, not NaN", func(t *testing.T) {
		edges := []weight.Edge{{From: "unit-v", To: "member-a", Strength: math.NaN()}}
		got := AdjacencyStrengths(previous, edges)
		if v, ok := got["unit-v"]; ok {
			t.Errorf("adjacency[unit-v] = %v, want absent — a NaN-only edge must not surface a NaN adjacency", v)
		}
	})

	t.Run("a NaN edge does not block a genuine edge seen after it", func(t *testing.T) {
		edges := []weight.Edge{
			{From: "unit-v", To: "member-a", Strength: math.NaN()},
			{From: "unit-v", To: "member-b", Strength: 0.6},
		}
		got := AdjacencyStrengths(previous, edges)
		if got["unit-v"] != 0.6 {
			t.Errorf("adjacency[unit-v] = %v, want 0.6 — a NaN edge must not suppress a later genuine one", got["unit-v"])
		}
	})

	t.Run("a NaN edge does not overwrite a genuine edge seen before it", func(t *testing.T) {
		edges := []weight.Edge{
			{From: "unit-v", To: "member-b", Strength: 0.6},
			{From: "unit-v", To: "member-a", Strength: math.NaN()},
		}
		got := AdjacencyStrengths(previous, edges)
		if got["unit-v"] != 0.6 {
			t.Errorf("adjacency[unit-v] = %v, want 0.6 — a NaN edge must not overwrite an earlier genuine one", got["unit-v"])
		}
	})
}
