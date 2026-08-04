package weight

import (
	"sort"
	"time"
)

// Edge is one relation edge inside a Neighbourhood, carrying the strength
// Resurface propagates along. Storage direction does not matter — doc 02
// §4 states a relation's direction is what the judge said, not a
// canonical form, so Resurface treats every edge as undirected (spec
// R2.5).
type Edge struct {
	From, To string
	Strength float64
}

// Neighbourhood is Resurface's whole input: the origin unit's id, the
// decay-relevant state of every unit the caller wants considered, and
// every edge among them.
type Neighbourhood struct {
	Origin string
	States []Current
	Edges  []Edge
}

// ResurfaceMaxHops bounds how far a boost propagates from
// Neighbourhood.Origin. Default 2 (doc 02 §13): one hop is not spreading,
// three reaches most of a personal graph and costs branching cubed.
const ResurfaceMaxHops = 2

// ResurfaceAttenuation is the per-hop cost gain pays, independent of how
// strong the judge said any single edge was. Default 0.5 (doc 02 §13).
const ResurfaceAttenuation = 0.5

// Resurface computes doc 02 §2's "propagates a boost along the graph
// edges" as a formula (spec R2.5-R2.7, design F3): F2's asymptotic boost
// applied to a TARGET scaled by graph distance, in place of Revive's
// fixed WeightCeiling.
//
// For every unit v reachable from n.Origin within ResurfaceMaxHops hops
// (other than the origin itself):
//
//	gain(v)   = max over paths p, |p| <= ResurfaceMaxHops, of
//	              (product of strength(e) for e in p) * ResurfaceAttenuation^|p|
//	target(v) = gain(v) * WeightCeiling
//	e_v       = Effective(v.Weight, v.DecayRate, v.LastTouchedAt, now)
//
// Resurface emits Boost{v, e_v + ReviveGain*(target(v)-e_v), now} only
// when e_v < target(v); when e_v >= target(v) it emits nothing for v — a
// shorter slice, never a zero-delta entry (spec R2.6).
//
// The gain scales the TARGET, never the step. Scaling the step instead —
// e + ReviveGain*gain*(WeightCeiling-e) — would let a unit merely adjacent
// to something used daily converge on the full ceiling: each pass closes a
// fraction of the remaining gap, while one day of decay at a typical λ
// removes only about 1% of it, so the neighbourhood of anything hot would
// become permanently hot and decay would never bite. Scaling the target
// caps *where* propagation can hold a unit, which is what makes spreading
// activation safe (spec R2.5).
//
// A unit reachable by more than one path takes the maximum gain among
// them, never the sum — the same rule design's F1/F3 use for combining
// graph evidence, so a unit's boost never depends on how many redundant
// edges happen to exist between it and the origin (spec R2.5). Traversal
// is undirected — a relation's storage direction is what the judge said,
// not a canonical form (doc 02 §4) — and where two units are joined by
// more than one edge, the strongest is used, by the same max rule. The
// origin is never a recipient: it already received its own direct revive,
// and a cycle back to it would double-count.
//
// Termination on a cyclic graph is by the hop bound alone, never a runtime
// timeout: gain is strictly decreasing along a path (ResurfaceAttenuation
// < 1, strength <= 1) and depth is capped at ResurfaceMaxHops, so a cycle
// can only ever produce a strictly worse path that the max comparison
// discards.
//
// Where Resurface DOES write, it resets LastTouchedAt to now — weight is
// *defined* as the value at last_touched_at, so writing one without the
// other would let the very next read re-apply the whole stale Δt to a
// value that was never true at its own timestamp (I24, spec R2.6). The
// worry that this makes a resurfaced unit look directly used is answered
// by the target cap, not by leaving the timestamp alone: a resurfaced unit
// converges on gain*WeightCeiling, never on WeightCeiling itself, so the
// clock resetting is harmless because the level it resets from is bounded
// by graph distance.
//
// Both returned slices are sorted by UnitID: the suite runs -shuffle=on
// with -race (Makefile:48), any implementation here uses maps internally,
// and m2c needs a reproducible decision_log order for the demo.
//
// TODO(C11): the second return value, corrupted, is always empty as of
// this commit. Judgment Day round 1 on PR #140 found Resurface persisted a
// NaN Boost for a corrupted neighbour instead of refusing it, the same bug
// C4 fixed for Revive one PR earlier — see the RED test this commit adds
// and the following commit's fix.
func Resurface(n Neighbourhood, now time.Time) (boosts []Boost, corrupted []string) {
	adjacency := buildAdjacency(n.Edges)
	gains := spreadGains(n.Origin, adjacency)

	states := make(map[string]Current, len(n.States))
	for _, c := range n.States {
		states[c.UnitID] = c
	}

	for unitID, gain := range gains {
		c, ok := states[unitID]
		if !ok {
			continue
		}

		target := gain * WeightCeiling
		e := Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
		if e >= target {
			continue
		}

		boosts = append(boosts, Boost{
			UnitID:        unitID,
			Weight:        e + ReviveGain*(target-e),
			LastTouchedAt: now,
		})
	}

	sort.Slice(boosts, func(i, j int) bool { return boosts[i].UnitID < boosts[j].UnitID })
	return boosts, corrupted
}

// buildAdjacency turns Edges into an undirected map: for every ordered
// pair (from, to), the strongest strength seen for that pair in EITHER
// stored direction (spec R2.5's "the strongest is used").
func buildAdjacency(edges []Edge) map[string]map[string]float64 {
	adjacency := make(map[string]map[string]float64)
	add := func(from, to string, strength float64) {
		if adjacency[from] == nil {
			adjacency[from] = make(map[string]float64)
		}
		if strength > adjacency[from][to] {
			adjacency[from][to] = strength
		}
	}
	for _, e := range edges {
		add(e.From, e.To, e.Strength)
		add(e.To, e.From, e.Strength)
	}
	return adjacency
}

// spreadGains returns, for every node other than origin reachable within
// ResurfaceMaxHops hops of origin, the maximum gain among all paths from
// origin to it. Depth is bounded by ResurfaceMaxHops on every branch, so
// this terminates on a cyclic adjacency graph regardless of its size —
// see Resurface's own doc comment for why a cycle can only ever worsen a
// path, never extend the search past the bound.
func spreadGains(origin string, adjacency map[string]map[string]float64) map[string]float64 {
	gains := make(map[string]float64)

	var walk func(node string, product float64, hops int)
	walk = func(node string, product float64, hops int) {
		if hops >= ResurfaceMaxHops {
			return
		}
		for neighbour, strength := range adjacency[node] {
			nextProduct := product * strength
			nextHops := hops + 1
			gain := nextProduct * attenuationPow(nextHops)
			if neighbour != origin && gain > gains[neighbour] {
				gains[neighbour] = gain
			}
			walk(neighbour, nextProduct, nextHops)
		}
	}
	walk(origin, 1, 0)

	return gains
}

// attenuationPow raises ResurfaceAttenuation to a small non-negative
// integer exponent (bounded by ResurfaceMaxHops, 2 by default) — a loop is
// simpler here than reaching for math.Pow for a single, tightly-bounded
// call site.
func attenuationPow(hops int) float64 {
	result := 1.0
	for i := 0; i < hops; i++ {
		result *= ResurfaceAttenuation
	}
	return result
}
