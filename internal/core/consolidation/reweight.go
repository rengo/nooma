package consolidation

import (
	"math"
	"sort"
	"time"

	"github.com/rengo/nooma/internal/core/weight"
)

// Reweight applies doc 02 §6.6's post-connection adjustment: every unit
// that gained a relation this pass spreads activation to the units it was
// just joined to, through weight.Resurface, over newEdges and no others
// (design.md §4.5(a), spec R3.3).
//
// states is a map, not a slice: a duplicate UnitID in a slice silently
// masks corruption and its outcome depends on slice order (m2a C18). The
// map makes the duplicate unrepresentable — that alone is what closes C18;
// unlike the map type, the order Reweight hands weight.Current values to
// Resurface in is NOT load-bearing, because Resurface immediately re-keys
// n.States into its own map (spread.go) before reading it, discarding
// whatever order it was given. Reweight's own two returned slices are
// still sorted by UnitID (below), which is what any caller actually
// observes.
//
// origins are every endpoint of newEdges. An Edge whose Strength is
// non-finite or outside [0,1] is refused at THIS door before
// weight.clampStrength or any comparison downstream can skip past it (m2a
// C15's rule; C19's asymmetry, where clampStrength coerces +Inf to 1
// rather than refusing it, is not inherited). An endpoint is reported into
// corrupted only when states holds a Current for it — the same guard
// weight.Resurface's own NaN-edge sweep applies (Fix D, Judgment Day
// round 1): reporting an id the caller holds no state for would put an id
// in corrupted the caller cannot act on. weight.Resurface is then called
// once per origin over the validated edge set only.
//
// Both slices are sorted by UnitID. boosts is merged per unit by the
// highest boosted weight across every origin's call — the same max rule
// Resurface and focus.AdjacencyStrengths use for combining graph evidence.
// corrupted is merged by UNION, deduplicated: a unit id refused — whether
// at Reweight's own edge-strength door or inside some origin's Resurface
// call, for a non-finite Current — is reported at most once in Reweight's
// output, regardless of how many of the pass's origin calls flag it (m2a
// C20/C21): a shared, unfiltered edge set means a corrupt state can be
// reached, and reported, by every origin call that does not otherwise
// explain that unit; the merge is where that is resolved, not the call.
//
// A unit id MAY appear in both boosts and corrupted: one origin's
// legitimate boost never cancels another origin's refusal, and neither
// suppresses the other. They are independent facts about the pass —
// "at least one origin moved this weight" and "at least one origin could
// not explain this unit" both hold at once, and both are reported
// (design.md §4.5(a)).
//
// There is no Materialize function — design.md §4.5(b) declines decay
// materialization for M2; doc 02 §6.6 states the option remains legal but
// unexercised.
func Reweight(states map[string]weight.Current, newEdges []weight.Edge, now time.Time) (boosts []weight.Boost, corrupted []string) {
	corruptSet := make(map[string]bool)
	originSet := make(map[string]bool, len(newEdges)*2)
	validEdges := make([]weight.Edge, 0, len(newEdges))

	for _, e := range newEdges {
		originSet[e.From] = true
		originSet[e.To] = true

		if invalidEdgeStrength(e.Strength) {
			// Report an endpoint only when the caller actually holds a
			// Current for it — the same guard weight.Resurface applies to
			// its own NaN-edge sweep (TestResurface_
			// CorruptEdgeToAnUnloadedUnit_IsNotReported): reporting an id
			// the caller has no state for would put an id in corrupted
			// the caller cannot act on. Fix D, Judgment Day round 1 — this
			// door previously reported both endpoints unconditionally,
			// silently differing from Resurface's settled policy.
			if _, ok := states[e.From]; ok {
				corruptSet[e.From] = true
			}
			if _, ok := states[e.To]; ok {
				corruptSet[e.To] = true
			}
			continue
		}
		validEdges = append(validEdges, e)
	}

	currentStates := currentsSlice(states)

	origins := make([]string, 0, len(originSet))
	for id := range originSet {
		origins = append(origins, id)
	}
	sort.Strings(origins)

	boostByUnit := make(map[string]weight.Boost)
	for _, origin := range origins {
		n := weight.Neighbourhood{Origin: origin, States: currentStates, Edges: validEdges}
		bs, cs := weight.Resurface(n, now)

		for _, b := range bs {
			if existing, ok := boostByUnit[b.UnitID]; !ok || b.Weight > existing.Weight {
				boostByUnit[b.UnitID] = b
			}
		}
		for _, id := range cs {
			corruptSet[id] = true
		}
	}

	for _, b := range boostByUnit {
		boosts = append(boosts, b)
	}
	for id := range corruptSet {
		corrupted = append(corrupted, id)
	}

	sort.Slice(boosts, func(i, j int) bool { return boosts[i].UnitID < boosts[j].UnitID })
	sort.Strings(corrupted)
	return boosts, corrupted
}

// invalidEdgeStrength reports whether s is non-finite, or finite but
// outside relation's own domain [0,1] — Reweight's own entry-point check,
// stricter than weight.clampStrength (which coerces a finite s > 1 to 1
// and never sees a NaN, since buildAdjacency intercepts it first — m2a
// C19). Reweight does not reach into clampStrength to change a shipped
// contract; it makes its own door consistent instead (design.md §4.5(a)).
func invalidEdgeStrength(s float64) bool {
	return math.IsNaN(s) || math.IsInf(s, 0) || s < 0 || s > 1
}

// currentsSlice returns states' values as a plain slice, in whatever order
// Go's map iteration happens to produce. No sort is applied here — a
// prior version sorted by UnitID, but weight.Resurface immediately
// re-keys Neighbourhood.States into its own map (spread.go), so no
// caller-observable outcome depends on this slice's order, and a duplicate
// UnitID is already unrepresentable in the map[string]weight.Current this
// reads from. A sort here would be a branch no fixture can tell apart
// from its absence (m2a C13's own convention), so it was removed
// (Judgment Day round 1, Fix B) rather than kept for a claim that was
// never actually tested. If a future change makes Resurface (or some
// replacement) order-sensitive over States, restore a sort here AND add a
// test that fails without it — do not assume order-independence still
// holds without re-checking spread.go.
func currentsSlice(states map[string]weight.Current) []weight.Current {
	out := make([]weight.Current, 0, len(states))
	for _, c := range states {
		out = append(out, c)
	}
	return out
}
