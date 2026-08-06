package focus

import "github.com/rengo/nooma/internal/core/weight"

// AdjacencyStrengths computes doc 02 §3's relation_to_active_focus input
// (spec R3.7, design §3.1): the circularity breaker that lets Priority read
// "how connected is this unit to what I was just focused on" without a
// fixpoint iteration.
//
//	adjacency[v] = max over edges e joining v to any member of
//	               previous.Members of strength(e)
//	             = 0 (v absent from the returned map) when previous is
//	               empty, or v touches no member
//
// MAX, not sum (spec R3.7's own MUST) — the identical rule
// weight.Resurface's buildAdjacency already uses for the same reason
// (spread.go's own doc comment): a sum lets a hub unit weakly connected to
// five focus members outrank a unit strongly connected to one, measuring
// graph topology rather than relevance, and would need an unspecified
// normalization (by what — the focus size?) to stay bounded.
//
// Traversal is undirected (spec R3.7's own MUST, R2.5's argument): edges
// reuses weight.Edge rather than a second edge type, and both of an edge's
// endpoints are checked against previous.Members regardless of which one
// is stored as From and which as To — a relation's storage direction is
// what the judge said, not a canonical form (doc 02 §4).
//
// previous and edges are ordinary parameters: AdjacencyStrengths reads no
// package-level or global state (spec R3.7, R3.8, R4.6).
//
// A returned map omits v entirely rather than storing an explicit 0 for
// it — the same contract Rank already relies on for a missing key
// (rank.go's own doc comment: "a Candidate's id missing from adjacency ...
// reads as 0", since indexing a Go map for an absent key always returns
// the zero value). An empty previous.Members therefore returns an empty
// map, never one key per edge endpoint pinned at 0 (spec R3.7's own
// boundary: on the first ranking after a process restart, previous is
// empty, so adjacency is 0 for every unit and the term vanishes entirely
// — doc 02 §3's second restart effect, together with R4.4/R4.5's empty-
// incumbent one).
//
// An edge whose Strength is NaN needs NO explicit math.IsNaN guard here,
// unlike weight.Edge's other consumer, weight.buildAdjacency (spread.go).
// That function's guard exists for a reason this signature's fixed,
// single map return value (spec R3.7's own MUST) has no room for: it ALSO
// reports a corrupt edge through a second return value, corruptEdges, so
// Resurface's caller can log it for decision_log — a concern with no
// equivalent here. Absent that reporting need, the max computed below is
// already exactly right for a NaN strength with no guard at all: each v's
// running maximum starts at a Go map's own zero value, 0.0, and is only
// ever replaced by `strength > current` — and every IEEE 754 comparison
// against NaN is false, in either operand position, so a NaN-strength
// edge can never win that comparison against 0.0 or against any real
// strength already recorded, whether it is the first edge seen for v or
// the last. Verified, not assumed: adjacency_test.go's
// TestAdjacencyStrengths_NaNStrengthEdge_VanishesWithoutAnExplicitGuard
// proves a NaN-only edge leaves v absent from the map (never present with
// a NaN value) and that a NaN edge neither suppresses nor overwrites a
// genuine one recorded before or after it — the claim above was also
// checked by temporarily adding an explicit math.IsNaN guard and
// confirming every test in this file still passes identically with or
// without it, this project's own C13 convention that a branch no fixture
// can tell apart from removing it should not exist.
//
// The same code shape raises a sibling question NaN's own resolution does
// not answer, and this function applies NO bound of its own to Strength —
// neither a ceiling at 1 nor a floor at 0 — which is deliberate, not an
// oversight (C31, reproducing weight's own open question C19 in a second
// package):
//
//   - A Strength of +Inf wins the running max unconditionally (+Inf is
//     greater than any finite current, and nothing later ever exceeds it)
//     and flows out of the returned map raw — adjacency["v"] = +Inf,
//     present = true — exactly as if it were a genuine, uncorrupted
//     maximal edge.
//   - A finite Strength above 1 behaves identically: raw, unclamped.
//   - A negative Strength can never win against the running max's own
//     zero-value baseline (`strength > adjacency[v]` starts comparing
//     against a Go map's zero value, 0.0), so a unit joined ONLY by
//     negative-strength edges is simply absent from the map — read as 0 by
//     every caller, indistinguishable from a unit with no edge to
//     previous.Members at all.
//   - A Strength of exactly 0.0 behaves the identical way, for the
//     identical reason (`0.0 > 0.0` is false against the zero-value
//     baseline): the unit is absent from the map, not present with an
//     explicit 0.0 value. This is the one case on this list that is NOT a
//     corrupt or out-of-range input — a relation the judge genuinely
//     assessed as zero relevance is a legitimate output, arguably the most
//     common one — and yet it is indistinguishable here from a negative
//     Strength or from no edge at all. Reading this map alone, a caller
//     cannot tell "assessed as irrelevant" apart from "corrupt" apart from
//     "never connected".
//
// This is safe ONLY because of who reads the result today: Priority
// clamps adjacency to [0,1] at its own door (priority.go,
// adjacency = clamp(adjacency, 0, 1)), and clamp is monotonic
// non-decreasing, so clamp(max(s1, ..., sn)) == max(clamp(s1), ...,
// clamp(sn)) — bounding after this function's max or bounding before it
// gives the identical final priority either way. Deferring the bound to
// Priority is therefore not a gap Priority merely happens to paper over;
// it is a property of clamp that makes the two orders equivalent, stated
// here so the equivalence is a checked claim rather than a coincidence
// relied upon silently.
//
// C19 already records this exact shape as an OPEN question for
// weight.Edge's other consumer, weight.buildAdjacency: "treating +Inf like
// 100.0 rather than like Weight = +Inf is an inconsistency, not a recorded
// decision." That question is NOT closed here — it is reproduced in this
// second package and closed only by an unrelated function's pre-existing
// clamp, a function this one does not call and has no control over. A
// second consumer of AdjacencyStrengths that reads the returned map
// directly, without routing it through Priority's clamp first, would see
// a raw, unbounded, possibly-infinite adjacency value with no guarantee
// at this function's own boundary — and would have to make its own
// decision about C19/C31 rather than inheriting Priority's answer for
// free. See adjacency_test.go's
// TestAdjacencyStrengths_UnclampedBoundaryStrengths for the pinned
// fixtures (+Inf, above 1, negative, and exactly 0.0).
func AdjacencyStrengths(previous Selection, edges []weight.Edge) map[string]float64 {
	adjacency := make(map[string]float64)
	if len(previous.Members) == 0 {
		return adjacency
	}

	member := make(map[string]bool, len(previous.Members))
	for _, id := range previous.Members {
		member[id] = true
	}

	consider := func(v, other string, strength float64) {
		if !member[other] {
			return
		}
		if strength > adjacency[v] {
			adjacency[v] = strength
		}
	}

	for _, e := range edges {
		consider(e.To, e.From, e.Strength)
		consider(e.From, e.To, e.Strength)
	}

	return adjacency
}
