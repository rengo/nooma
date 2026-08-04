package weight

import (
	"math"
	"testing"
	"time"
)

// TestResurfaceMaxHops_IsPinnedToItsCalibratedValue and
// TestResurfaceAttenuation_IsPinnedToItsCalibratedValue guard the two new
// constants against a wrong VALUE, independent of every shape test in this
// file — the same defence boost_test.go's TestReviveGain_IsPinnedToItsCalibratedValue
// and TestWeightCeiling_IsPinnedToItsCalibratedValue built for ReviveGain
// and WeightCeiling in 2a, per C7 (openspec/changes/m2a-weight-focus/tasks.md):
// every shape test in this file derives its expectation from
// ResurfaceMaxHops/ResurfaceAttenuation themselves, so a mutated constant
// flows into "want" the same way it flows into Resurface's output and
// none of them would notice it moved. Only a comparison against an
// independent literal does.
//
// Disclosure (this project's own convention, tasks.md's intro): these two
// checks cannot be RED at the point this commit lands. The stub in
// spread.go already declares the correct literal values, so the assertion
// is true the instant the stub compiles — it is not a missing-symbol red,
// it is a permanent guard against a future recalibration silently moving
// the constant without updating docs/02-cognitive-core.md §13's matching
// row, the same shape TestI05_BoostHasExactlyTwoProducers below already
// carries for a structural claim.
func TestResurfaceMaxHops_IsPinnedToItsCalibratedValue(t *testing.T) {
	const want = 2 // docs/02-cognitive-core.md §13, row "resurface_max_hops"
	if ResurfaceMaxHops != want {
		t.Errorf("ResurfaceMaxHops = %v, want %v — update docs/02-cognitive-core.md §13's resurface_max_hops row in the same change", ResurfaceMaxHops, want)
	}
}

func TestResurfaceAttenuation_IsPinnedToItsCalibratedValue(t *testing.T) {
	const want = 0.5 // docs/02-cognitive-core.md §13, row "resurface_attenuation"
	if ResurfaceAttenuation != want {
		t.Errorf("ResurfaceAttenuation = %v, want %v — update docs/02-cognitive-core.md §13's resurface_attenuation row in the same change", ResurfaceAttenuation, want)
	}
}

// mustResurface calls Resurface and fails the test if it reports any
// corrupted neighbour. Every fixture in this file besides
// TestResurface_NonFiniteState_RefusesRatherThanCoerces is built from
// finite state on purpose, so a non-empty corrupted list there is itself a
// bug in the fixture, or a regression in Resurface, not something to
// silently ignore (Judgment Day round 1 on PR #140, C11).
func mustResurface(t *testing.T, n Neighbourhood, now time.Time) []Boost {
	t.Helper()
	got, corrupted := Resurface(n, now)
	if len(corrupted) != 0 {
		t.Fatalf("Resurface(...) corrupted = %v, want none — this fixture is built from finite state only", corrupted)
	}
	return got
}

// assertBoosts compares got against a slice of (UnitID, Weight) pairs, in
// order — Resurface's own contract is a slice sorted by UnitID, so an
// out-of-order want here is itself a bug in the fixture, not tolerance.
// Every entry is also checked to carry LastTouchedAt == now exactly (spec
// R2.6's "where Resurface does write, it resets last_touched_at" — the
// mutant this catches is a Resurface that boosts weight correctly but
// forgets the timestamp half of I24's pair).
func assertBoosts(t *testing.T, got []Boost, now time.Time, want []struct {
	id     string
	weight float64
}) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Resurface(...) returned %d boosts %+v, want %d %+v", len(got), got, len(want), want)
	}
	for i, w := range want {
		if got[i].UnitID != w.id {
			t.Errorf("boost[%d].UnitID = %q, want %q (result must be sorted by UnitID)", i, got[i].UnitID, w.id)
		}
		if math.Abs(got[i].Weight-w.weight) > 1e-9 {
			t.Errorf("boost[%d] (%s).Weight = %v, want %v", i, got[i].UnitID, got[i].Weight, w.weight)
		}
		if got[i].LastTouchedAt != now {
			t.Errorf("boost[%d] (%s).LastTouchedAt = %v, want %v", i, got[i].UnitID, got[i].LastTouchedAt, now)
		}
	}
}

// zeroState is a fully-decayed fixture (e == 0 at any now, since
// DecayRate == 0 makes Effective return Weight unchanged): it isolates
// gain/target arithmetic from decay arithmetic, already pinned separately
// in decay_test.go and boost_test.go.
func zeroState(id string) Current {
	return Current{UnitID: id, Weight: 0, DecayRate: 0}
}

// TestResurface_TwoHopWorkedExample pins R2.5's boundary-table row (2
// hops, strength 1.0 each -> gain 0.25, target 0.50) with an expected
// Boost weight computed independently of Resurface's own expression:
// ReviveGain * target = 0.35 * 0.50 = 0.175. It also exercises the 1-hop
// neighbour on the same path (target 1.00, weight 0.35) and the sorted,
// origin-excluded contract in one fixture: a straight line a -> b -> c.
func TestResurface_TwoHopWorkedExample(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "a",
		States: []Current{zeroState("b"), zeroState("c")},
		Edges: []Edge{
			{From: "a", To: "b", Strength: 1.0},
			{From: "b", To: "c", Strength: 1.0},
		},
	}

	got := mustResurface(t, n, now)
	assertBoosts(t, got, now, []struct {
		id     string
		weight float64
	}{
		{"b", 0.35},  // 1 hop: gain 0.5, target 1.00, 0.35*(1.00-0)
		{"c", 0.175}, // 2 hops: gain 0.25, target 0.50, 0.35*(0.50-0)
	})
	for _, b := range got {
		if b.UnitID == "a" {
			t.Errorf("Resurface(...) included the origin %q in its output — the origin is never a recipient (R2.5)", "a")
		}
	}
}

// TestResurface_TwoHopDistinctStrengths_ProductNotLastEdge pins R2.5's own
// product-across-the-path formula against a mutant that reads only the
// path's last edge. Judgment Day round 1 on PR #140 (both judges,
// independently, C12) found that replacing
//
//	nextProduct := product * strength   // correct
//
// with
//
//	nextProduct := strength             // mutant: only the last edge survives
//
// in spread.go's spreadGains leaves the entire suite green, because every
// other multi-hop fixture in this file uses Strength: 1.0 on every edge —
// where "product across the path" and "nearest edge only" are numerically
// identical (1.0 * 1.0 == 1.0). This fixture uses distinct, non-1.0
// strengths per edge (a→b 0.9, b→c 0.6) so the two shapes diverge:
//
//	correct product:        0.9 * 0.6 = 0.54
//	last-edge-only mutant:        0.6      (a first-edge-only mutant: 0.9)
//
// gain(c) = 0.54 * ResurfaceAttenuation^2 = 0.54*0.25 = 0.135, target =
// 0.135*WeightCeiling = 0.27, weight' = ReviveGain*0.27 = 0.0945 —
// distinguishable from the last-edge-only mutant's 0.6*0.25=0.15 (weight
// 0.105) and from a first-edge-only mutant's 0.9*0.25=0.225 (weight
// 0.1575).
//
// Mutation-verified: replacing `nextProduct := product * strength` with
// `nextProduct := strength` in spread.go's spreadGains makes this test's
// boost[1] (c) fail (got 0.105, want 0.0945) while every other test in
// this file — all built on strength-1.0 fixtures — stays green, matching
// this entry's own diagnosis. Reverted after; tree clean. This is a
// mutation-driven regression test, not a TDD red step: spreadGains's
// product accumulation was already correct, so there is no missing-symbol
// red available here (this project's own convention, tasks.md's intro;
// the same disclosure C7 gave its own mutation-driven addition).
func TestResurface_TwoHopDistinctStrengths_ProductNotLastEdge(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "a",
		States: []Current{zeroState("b"), zeroState("c")},
		Edges: []Edge{
			{From: "a", To: "b", Strength: 0.9},
			{From: "b", To: "c", Strength: 0.6},
		},
	}

	got := mustResurface(t, n, now)
	assertBoosts(t, got, now, []struct {
		id     string
		weight float64
	}{
		{"b", 0.315},  // 1 hop: gain 0.9*0.5=0.45, target 0.90, 0.35*0.90
		{"c", 0.0945}, // 2 hops: gain 0.9*0.6*0.25=0.135, target 0.27, 0.35*0.27
	})
}

// TestResurface_CyclicGraph_TerminatesAndTakesMaxNotSum is R2.5's own
// scenario (a source with two distinct paths to a neighbour, one gain
// 0.20/0.12-shaped) generalized to a genuine 3-cycle a-b-c-a: b and c are
// each reachable both directly (1 hop, gain 0.5) and via the other (2
// hops, gain 0.25). A mutant that sums instead of maxing would compute
// 0.5+0.25=0.75 (clamped or not) instead of 0.5 — this fixture discriminates
// that directly, and a cyclic graph with no hop bound would never
// terminate, so completing at all proves the bound stops expansion instead
// of a runtime timeout.
func TestResurface_CyclicGraph_TerminatesAndTakesMaxNotSum(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "a",
		States: []Current{zeroState("b"), zeroState("c")},
		Edges: []Edge{
			{From: "a", To: "b", Strength: 1.0},
			{From: "b", To: "c", Strength: 1.0},
			{From: "c", To: "a", Strength: 1.0},
		},
	}

	got := mustResurface(t, n, now)
	assertBoosts(t, got, now, []struct {
		id     string
		weight float64
	}{
		{"b", 0.35}, // max(1-hop 0.5, 2-hop-via-c 0.25) = 0.5, not the sum 0.75
		{"c", 0.35}, // max(1-hop 0.5, 2-hop-via-b 0.25) = 0.5, not the sum 0.75
	})
}

// TestResurface_ChainLongerThanMaxHops_ExcludesUnitsBeyondTheLimit is the
// hop-limit-off-by-one discriminator: a straight chain a-b-c-d-e puts c
// exactly at ResurfaceMaxHops and d one hop past it. A mutant using
// hops <= ResurfaceMaxHops as its expansion guard (instead of the correct
// hops < ResurfaceMaxHops before advancing) would leak d into the result;
// a mutant with the bound off by one the other way would drop c. This
// fixture requires exactly {b, c} and neither d nor e.
func TestResurface_ChainLongerThanMaxHops_ExcludesUnitsBeyondTheLimit(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "a",
		States: []Current{zeroState("b"), zeroState("c"), zeroState("d"), zeroState("e")},
		Edges: []Edge{
			{From: "a", To: "b", Strength: 1.0},
			{From: "b", To: "c", Strength: 1.0},
			{From: "c", To: "d", Strength: 1.0},
			{From: "d", To: "e", Strength: 1.0},
		},
	}

	got := mustResurface(t, n, now)
	assertBoosts(t, got, now, []struct {
		id     string
		weight float64
	}{
		{"b", 0.35},
		{"c", 0.175},
	})
	for _, b := range got {
		if b.UnitID == "d" || b.UnitID == "e" {
			t.Errorf("Resurface(...) included %q, more than ResurfaceMaxHops (%d) hops from the origin", b.UnitID, ResurfaceMaxHops)
		}
	}
}

// TestResurface_UndirectedTraversal_SameResultEitherStoredDirection proves
// an edge conducts activation regardless of which way it was stored — R2.5's
// undirected requirement, and doc 02 §4's "direction is what the judge
// said, not a canonical form."
func TestResurface_UndirectedTraversal_SameResultEitherStoredDirection(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	states := []Current{zeroState("n")}

	forward := mustResurface(t, Neighbourhood{
		Origin: "a",
		States: states,
		Edges:  []Edge{{From: "a", To: "n", Strength: 0.8}},
	}, now)
	backward := mustResurface(t, Neighbourhood{
		Origin: "a",
		States: states,
		Edges:  []Edge{{From: "n", To: "a", Strength: 0.8}},
	}, now)

	want := []struct {
		id     string
		weight float64
	}{{"n", 0.28}} // gain 0.8*0.5=0.4, target 0.8, 0.35*(0.8-0)
	assertBoosts(t, forward, now, want)
	assertBoosts(t, backward, now, want)
}

// TestResurface_MultipleEdgesBetweenSamePair_UsesTheStrongest proves the
// "strongest wins" rule for a pair joined by more than one edge (different
// relation types stored between the same two units). A mutant that sums or
// averages the two strengths (0.2+0.6=0.8, or 0.4) would diverge from the
// expected 0.21 pinned here (strongest 0.6, gain 0.3, target 0.6,
// 0.35*0.6).
func TestResurface_MultipleEdgesBetweenSamePair_UsesTheStrongest(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "a",
		States: []Current{zeroState("n")},
		Edges: []Edge{
			{From: "a", To: "n", Strength: 0.2},
			{From: "a", To: "n", Strength: 0.6},
		},
	}

	got := mustResurface(t, n, now)
	assertBoosts(t, got, now, []struct {
		id     string
		weight float64
	}{{"n", 0.21}})
}

// TestResurface_OutputSortedByUnitID uses two neighbours deliberately out
// of alphabetical order in every fixture list (Edges, States) to prove the
// sort is real and not an accident of insertion order — the suite runs
// -shuffle=on -race (Makefile:48), and any map-backed implementation
// (a visited set, in particular) needs an explicit sort to be
// reproducible.
func TestResurface_OutputSortedByUnitID(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "origin",
		States: []Current{zeroState("zeta"), zeroState("alpha")},
		Edges: []Edge{
			{From: "origin", To: "zeta", Strength: 1.0},
			{From: "origin", To: "alpha", Strength: 1.0},
		},
	}

	got := mustResurface(t, n, now)
	assertBoosts(t, got, now, []struct {
		id     string
		weight float64
	}{
		{"alpha", 0.35},
		{"zeta", 0.35},
	})
}

// TestResurface_AtOrAboveTarget_EmitsNoBoost proves R2.6's no-op branch: a
// neighbour whose effective weight already meets or exceeds its target
// gets no Boost at all — a shorter slice, not a zero-delta entry with an
// unchanged weight.
//
// Disclosure (Judgment Day round 1 on PR #140, Judge A, C14): checked out
// at its introducing RED commit (456cbbb), against a `return nil` stub,
// the original version of this test — `if len(got) != 0 { t.Errorf(...) }`
// alone — was trivially green: len(nil) != 0 is false for every input, so
// it never distinguished "correctly refused" from "did nothing at all."
// It carried no disclosure saying so, unlike the tests in this file that
// are legitimately unable to be red. Fixed here, rather than only
// disclosed, by adding a companion neighbour genuinely below its own
// target in the same Neighbourhood: a nil-returning stub now fails this
// test because the companion's boost is absent too, so the assertion can
// no longer pass by doing nothing.
func TestResurface_AtOrAboveTarget_EmitsNoBoost(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		weight float64
	}{
		{"exactly at target", 1.0}, // 1 hop, strength 1.0 -> target exactly 1.0
		{"above target", 1.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := Neighbourhood{
				Origin: "a",
				States: []Current{
					{UnitID: "n", Weight: tc.weight, DecayRate: 0, LastTouchedAt: now},
					zeroState("companion"), // below its own target — proves the call actually ran
				},
				Edges: []Edge{
					{From: "a", To: "n", Strength: 1.0},
					{From: "a", To: "companion", Strength: 1.0},
				},
			}
			got := mustResurface(t, n, now)
			assertBoosts(t, got, now, []struct {
				id     string
				weight float64
			}{{"companion", 0.35}})
			for _, b := range got {
				if b.UnitID == "n" {
					t.Errorf("Resurface(weight=%v at target) included %q, want it absent (R2.6 — no Boost, not a zero-delta entry)", tc.weight, "n")
				}
			}
		})
	}
}

// TestResurface_BelowTarget_ResetsLastTouchedAtToNow isolates R2.6's
// "where Resurface DOES write, it resets last_touched_at" half from the
// weight-boosting arithmetic: a mutant that computes the right boosted
// weight but leaves LastTouchedAt at its old value (dropping I24's pair
// discipline) is caught here specifically, using a LastTouchedAt genuinely
// earlier than now so a no-op mutant is distinguishable from the correct
// behaviour.
func TestResurface_BelowTarget_ResetsLastTouchedAtToNow(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	staleLastTouchedAt := now.AddDate(0, 0, -100)

	n := Neighbourhood{
		Origin: "a",
		States: []Current{{UnitID: "n", Weight: 0, DecayRate: 0, LastTouchedAt: staleLastTouchedAt}},
		Edges:  []Edge{{From: "a", To: "n", Strength: 1.0}},
	}

	got := mustResurface(t, n, now)
	if len(got) != 1 {
		t.Fatalf("Resurface(...) = %+v, want exactly one boost", got)
	}
	if got[0].LastTouchedAt.Equal(staleLastTouchedAt) {
		t.Errorf("Resurface(...).LastTouchedAt = %v, want it to have moved away from %v — a genuine lift resets the clock (R2.6)", got[0].LastTouchedAt, staleLastTouchedAt)
	}
	if got[0].LastTouchedAt != now {
		t.Errorf("Resurface(...).LastTouchedAt = %v, want %v exactly", got[0].LastTouchedAt, now)
	}
}

// TestResurface_DiscriminatesEffectiveFromPersistedWeight is R2.5's
// analogue of boost_test.go's TestRevive_BelowCeiling_PinsExactBoostFromEffectiveWeight:
// every other fixture in this file uses DecayRate == 0, where the
// effective and persisted weight coincide, so a mutant anchoring the boost
// on the persisted v.Weight instead of the decayed effective weight would
// pass every one of them undetected. This fixture sets DecayRate > 0 and
// Δt > 0 so the two values genuinely differ, and pins an exact expected
// weight computed independently via math.Exp directly (the same formula
// Effective implements, verified separately in decay_test.go — not by
// calling Resurface's own gain/target expression).
func TestResurface_DiscriminatesEffectiveFromPersistedWeight(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := lastTouchedAt.AddDate(0, 0, 30)

	const weight = 1.0
	const decayRate = 0.02
	n := Neighbourhood{
		Origin: "a",
		States: []Current{{UnitID: "n", Weight: weight, DecayRate: decayRate, LastTouchedAt: lastTouchedAt}},
		Edges:  []Edge{{From: "a", To: "n", Strength: 1.0}}, // 1 hop -> target 1.00
	}

	e := weight * math.Exp(-decayRate*30)
	const target = 1.0
	if e >= target {
		t.Fatalf("test fixture error: effective weight %v is not strictly below target %v", e, target)
	}
	if math.Abs(e-weight) < 0.05 {
		t.Fatalf("test fixture error: effective weight %v is too close to persisted weight %v to discriminate a persisted-weight mutant", e, weight)
	}
	want := e + ReviveGain*(target-e)

	got := mustResurface(t, n, now)
	if len(got) != 1 {
		t.Fatalf("Resurface(...) = %+v, want exactly one boost", got)
	}
	if math.Abs(got[0].Weight-want) > 1e-9 {
		t.Errorf("Resurface(weight=%v, decayRate=%v, Δt=30d).Weight = %v, want %v exactly (boosted from the effective weight %v, not the persisted weight %v)", weight, decayRate, got[0].Weight, want, e, weight)
	}
}

// TestResurface_NeighbourWithNoMatchingState_IsSkipped covers a
// Neighbourhood whose Edges reference a unit id absent from States: a
// malformed-input shape the spec does not name a MUST for, but one
// Resurface must not panic on, since a caller building Neighbourhood from
// a real graph query can genuinely have an edge to a unit it did not also
// fetch decay-relevant state for. Resurface skips it — no boost, no crash
// — while a properly-stated neighbour on the same graph still gets one.
//
// Disclosure (this project's own convention, C2's own lesson): this test
// was added AFTER the GREEN commit, driven by a core-coverage report — the
// `if !ok { continue }` branch it exercises was already correct in the
// implementation the RED/GREEN commits shipped, and this test proves it
// rather than driving new behaviour. C2 flagged the earlier instance of
// this pattern (`ab9e172`, undisclosed); this one is named as what it is
// instead of presented as part of the red/green cycle.
func TestResurface_NeighbourWithNoMatchingState_IsSkipped(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "a",
		States: []Current{zeroState("b")}, // "ghost" is reachable but has no State entry
		Edges: []Edge{
			{From: "a", To: "b", Strength: 1.0},
			{From: "a", To: "ghost", Strength: 1.0},
		},
	}

	got := mustResurface(t, n, now)
	assertBoosts(t, got, now, []struct {
		id     string
		weight float64
	}{{"b", 0.35}})
}

// TestResurface_BoundaryTable pins R2.5/R2.7's own boundary table exactly,
// each row an independent fixture rather than a shared one: 1 hop at
// strength 1.0 (target exactly WeightCeiling/2 = 1.00, doc 02 §13's base
// weight and no higher); 2 hops at strength 1.0 each (target exactly 0.50,
// the archive threshold); 1 hop at strength 0.1 (doc 02 §4's "a passing
// mention", target 0.10 — cannot keep anything alive); 3+ hops
// (unreachable, absent from the result).
func TestResurface_BoundaryTable(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	t.Run("1 hop strength 1.0 -> target 1.00", func(t *testing.T) {
		n := Neighbourhood{
			Origin: "a",
			States: []Current{zeroState("n")},
			Edges:  []Edge{{From: "a", To: "n", Strength: 1.0}},
		}
		got := mustResurface(t, n, now)
		assertBoosts(t, got, now, []struct {
			id     string
			weight float64
		}{{"n", 0.35}}) // 0.35 * 1.00
	})

	t.Run("2 hops strength 1.0 each -> target 0.50", func(t *testing.T) {
		n := Neighbourhood{
			Origin: "a",
			States: []Current{zeroState("mid"), zeroState("n")},
			Edges: []Edge{
				{From: "a", To: "mid", Strength: 1.0},
				{From: "mid", To: "n", Strength: 1.0},
			},
		}
		got := mustResurface(t, n, now)
		var nBoost *Boost
		for i := range got {
			if got[i].UnitID == "n" {
				nBoost = &got[i]
			}
		}
		if nBoost == nil {
			t.Fatalf("Resurface(...) = %+v, want an entry for %q", got, "n")
		}
		if math.Abs(nBoost.Weight-0.175) > 1e-9 { // 0.35 * 0.50
			t.Errorf("boost(n).Weight = %v, want %v", nBoost.Weight, 0.175)
		}
	})

	t.Run("1 hop strength 0.1 -> target 0.10", func(t *testing.T) {
		n := Neighbourhood{
			Origin: "a",
			States: []Current{zeroState("n")},
			Edges:  []Edge{{From: "a", To: "n", Strength: 0.1}},
		}
		got := mustResurface(t, n, now)
		assertBoosts(t, got, now, []struct {
			id     string
			weight float64
		}{{"n", 0.035}}) // 0.35 * 0.10
	})

	// Disclosure (Judgment Day round 1 on PR #140, Judge A, C14): the
	// original version of this subtest only checked that "d" was absent
	// from got — trivially satisfied by a nil-returning stub too, the same
	// shape flaw TestResurface_AtOrAboveTarget_EmitsNoBoost carried. Fixed
	// here with an explicit non-empty check: b and c are both already
	// pinned reachable on the identical chain shape by
	// TestResurface_ChainLongerThanMaxHops_ExcludesUnitsBeyondTheLimit, so
	// requiring len(got) > 0 proves the call actually computed them rather
	// than returning nothing.
	t.Run("3+ hops -> unreachable", func(t *testing.T) {
		n := Neighbourhood{
			Origin: "a",
			States: []Current{zeroState("b"), zeroState("c"), zeroState("d")},
			Edges: []Edge{
				{From: "a", To: "b", Strength: 1.0},
				{From: "b", To: "c", Strength: 1.0},
				{From: "c", To: "d", Strength: 1.0},
			},
		}
		got := mustResurface(t, n, now)
		if len(got) == 0 {
			t.Fatalf("Resurface(...) returned no boosts at all, want at least %q and %q reachable within ResurfaceMaxHops — an empty result here would also be produced by a nil-returning stub", "b", "c")
		}
		for _, b := range got {
			if b.UnitID == "d" {
				t.Errorf("Resurface(...) included %q at 3 hops, want it absent (ResurfaceMaxHops = %d)", "d", ResurfaceMaxHops)
			}
		}
	})
}

// TestResurface_NonFiniteState_RefusesRatherThanCoerces pins C11's posture
// (Judgment Day round 1 on PR #140, both judges, independently): Effective
// deliberately does not sanitize NaN/±Inf (decay.go's own doc comment),
// and "e >= target" is false for every comparison involving NaN under
// IEEE 754 — so R2.6's skip branch never fires on its own, and a
// corrupted Current flows straight into an ordinary Boost. Boost is "the
// only shape this package lets a caller persist" (boost.go), and nothing
// in the vault is ever deleted, so a persisted NaN would make every later
// Effective on that unit return NaN forever.
//
// Mirroring Revive's own posture (boost.go's Revive doc comment, C4):
// Resurface must refuse to emit a Boost for a corrupted neighbour rather
// than coerce it to a finite number — coercing to 0 could drive the unit
// under weight_threshold and archive it on the strength of a read error.
// The refusal is reported through corrupted, Resurface's second return
// value, not folded silently into a shorter boosts slice: a caller must
// be able to tell "no boost because n is already at target" (R2.6, a
// legitimate no-op) apart from "no boost because n's own state is
// corrupt" (this refusal, which m2c should write to decision_log).
//
// Five reachable-looking non-finite shapes, matching decay.go's own
// enumeration plus the one it explicitly says does NOT reach NaN through
// Effective alone: Weight NaN; DecayRate NaN; Weight +Inf combined with a
// DecayRate/Δt product large enough for exp(...) to underflow to exactly
// 0.0 (+Inf * 0.0 = NaN); DecayRate +Inf with Δt = 0, the textbook
// Inf*0 = NaN shape (exp(-Inf*0) = exp(NaN) = NaN); and Weight +Inf ALONE,
// with DecayRate/Δt too small to underflow exp — decay.go's own comment
// names this shape explicitly ("a bare Weight = +Inf alone stays +Inf,
// not NaN") but the round-1 fixture set never included it.
//
// Judgment Day round 2 on PR #140 (both judges, independently, C15) found
// that last shape reaches Resurface's target/e comparison, not Effective's
// NaN arithmetic: target is always finite (<= WeightCeiling), and
// "+Inf >= target" is a perfectly valid TRUE comparison under IEEE 754 —
// not the NaN quirk this file's docs otherwise invoke — so R2.6's own skip
// branch fired before w was ever computed, and this unit vanished from
// both boosts and corrupted instead of being refused. The round-1 fixture
// set happened to pick only the +Inf variant that DOES underflow to NaN
// (row 3 below), leaving row 5 — the variant decay.go's own comment names
// as the one that does not — untested. This case closes that gap.
//
// Red at this commit: Resurface's stub always returns an empty
// corrupted, and still appends every computed w to boosts regardless of
// its finiteness, so this fails both assertions below.
func TestResurface_NonFiniteState_RefusesRatherThanCoerces(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name          string
		weight        float64
		decayRate     float64
		lastTouchedAt time.Time
	}{
		{"weight NaN", math.NaN(), 0, now},
		{"decayRate NaN", 1.0, math.NaN(), now},
		{"weight +Inf, decayRate*Δt underflows exp to 0", math.Inf(1), 100, now.AddDate(0, 0, -1000)},
		{"decayRate +Inf, zero Δt", 1.0, math.Inf(1), now},
		{"weight +Inf alone, no underflow (Effective returns +Inf, not NaN)", math.Inf(1), 0, now},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := Neighbourhood{
				Origin: "a",
				States: []Current{{UnitID: "n", Weight: tc.weight, DecayRate: tc.decayRate, LastTouchedAt: tc.lastTouchedAt}},
				Edges:  []Edge{{From: "a", To: "n", Strength: 1.0}},
			}

			got, corrupted := Resurface(n, now)
			for _, b := range got {
				if b.UnitID == "n" {
					t.Fatalf("Resurface(...) = %+v, persisted a Boost with weight %v for a non-finite state — want it refused, not coerced", got, b.Weight)
				}
			}
			if len(corrupted) != 1 || corrupted[0] != "n" {
				t.Errorf("Resurface(...) corrupted = %v, want exactly [\"n\"] — a corrupt neighbour must be reported, not silently dropped", corrupted)
			}
		})
	}
}

// TestResurface_EdgeStrengthAboveOne_ClampsToOne pins C13's sanitization
// of Edge.Strength (Judgment Day round 1 on PR #140, Judge A):
// `strength REAL NOT NULL DEFAULT 0.5` carries no CHECK
// (0001_core_tables.sql:35), the relation judge's JSON decode validates
// only that Strength is a number (internal/core/relation/judgment.go), and
// internal/brain/capture.go writes it through unclamped — the same
// "no sign, no range" LLM threat model Effective already assumes for
// weight and decay_rate. Left unclamped, Strength: 100.0 on a single edge
// reached Weight: 35.65, 17.8x WeightCeiling, falsifying R2.7's own
// boundedness argument, which assumes strength <= 1 explicitly. Clamped
// to 1, a strength of 100 must produce the exact same result as a
// strength of 1.0 exactly — TestResurface_BoundaryTable's own "1 hop
// strength 1.0" row.
//
// Red at this commit: buildAdjacency does not clamp Strength yet, so the
// unbounded 100.0 flows straight into the gain/target arithmetic.
func TestResurface_EdgeStrengthAboveOne_ClampsToOne(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "a",
		States: []Current{zeroState("n")},
		Edges:  []Edge{{From: "a", To: "n", Strength: 100.0}},
	}

	got := mustResurface(t, n, now)
	assertBoosts(t, got, now, []struct {
		id     string
		weight float64
	}{{"n", 0.35}})
}

// TestResurface_EdgeStrengthNaN_UnreachableNeighbourReportedCorrupted pins
// the second CRITICAL from Judgment Day round 2 on PR #140 (both judges,
// independently, C15): buildAdjacency's own "strongest wins" comparison,
// `strength > adjacency[from][to]`, is false for NaN against any value —
// including the map's own zero-value default — so a NaN-strength edge
// never became a key in the adjacency map at all. When it is a neighbour's
// only edge, that neighbour was never visited by spreadGains: no boost, no
// corrupted, indistinguishable from a neighbour genuinely outside the
// graph. clampStrength's own doc comment claimed this was "still caught —
// Resurface's own non-finite refusal always sees it downstream" — false,
// and this fixture verifies it false: gains never contains "n", so the
// refusal check inside Resurface's main loop, which only runs for unit ids
// gains actually reaches, never runs for it either.
//
// Red at this commit: buildAdjacency silently drops the NaN-strength edge,
// "n" is unreachable, and Resurface returns corrupted = [] instead of ["n"].
func TestResurface_EdgeStrengthNaN_UnreachableNeighbourReportedCorrupted(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	t.Run("forward direction", func(t *testing.T) {
		n := Neighbourhood{
			Origin: "a",
			States: []Current{zeroState("n")},
			Edges:  []Edge{{From: "a", To: "n", Strength: math.NaN()}},
		}
		got, corrupted := Resurface(n, now)
		if len(got) != 0 {
			t.Fatalf("Resurface(...) = %+v, want no boosts for a neighbour reached only by a NaN-strength edge", got)
		}
		if len(corrupted) != 1 || corrupted[0] != "n" {
			t.Errorf("Resurface(...) corrupted = %v, want exactly [\"n\"] — a NaN-strength edge must not make its neighbour indistinguishable from unreachable-but-healthy", corrupted)
		}
	})

	t.Run("backward direction, undirected", func(t *testing.T) {
		n := Neighbourhood{
			Origin: "a",
			States: []Current{zeroState("n")},
			Edges:  []Edge{{From: "n", To: "a", Strength: math.NaN()}},
		}
		got, corrupted := Resurface(n, now)
		if len(got) != 0 {
			t.Fatalf("Resurface(...) = %+v, want no boosts for a neighbour reached only by a NaN-strength edge", got)
		}
		if len(corrupted) != 1 || corrupted[0] != "n" {
			t.Errorf("Resurface(...) corrupted = %v, want exactly [\"n\"] — traversal is undirected (R2.5), so the direction the corrupt edge was stored must not matter", corrupted)
		}
	})
}

// TestResurface_EdgeStrengthNaN_MultiHopUnreachableNeighbourReportedCorrupted
// generalizes the CRITICAL 2 fixture above beyond an edge directly touching
// the origin: a straight chain a-b-c where the only edge reaching c (b->c)
// carries a NaN strength. b is reachable through a healthy edge and must
// still get its ordinary boost; c must be reported corrupted rather than
// silently absent, proving the fix is not scoped to origin-adjacent edges
// only.
func TestResurface_EdgeStrengthNaN_MultiHopUnreachableNeighbourReportedCorrupted(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "a",
		States: []Current{zeroState("b"), zeroState("c")},
		Edges: []Edge{
			{From: "a", To: "b", Strength: 1.0},
			{From: "b", To: "c", Strength: math.NaN()},
		},
	}

	got, corrupted := Resurface(n, now)
	assertBoosts(t, got, now, []struct {
		id     string
		weight float64
	}{{"b", 0.35}})
	if len(corrupted) != 1 || corrupted[0] != "c" {
		t.Errorf("Resurface(...) corrupted = %v, want exactly [\"c\"] — its only edge carries a NaN strength", corrupted)
	}
}

// TestResurface_EdgeStrengthNaN_RedundantWithHealthyPath_StillBoosts
// proves the corrupted-edge report does not over-fire: when a neighbour is
// ALSO reachable through a genuinely healthy edge, a second, NaN-strength
// edge between the same pair must not suppress its boost or report it as
// corrupted — the healthy path's gain is what spreadGains finds, and the
// corrupt edge simply never became a competing adjacency entry, the same
// "the strongest wins, a redundant path changes nothing" rule R2.5 already
// uses for TestResurface_MultipleEdgesBetweenSamePair_UsesTheStrongest.
func TestResurface_EdgeStrengthNaN_RedundantWithHealthyPath_StillBoosts(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "a",
		States: []Current{zeroState("n")},
		Edges: []Edge{
			{From: "a", To: "n", Strength: 1.0},
			{From: "a", To: "n", Strength: math.NaN()},
		},
	}

	got, corrupted := Resurface(n, now)
	assertBoosts(t, got, now, []struct {
		id     string
		weight float64
	}{{"n", 0.35}})
	if len(corrupted) != 0 {
		t.Errorf("Resurface(...) corrupted = %v, want none — %q has a healthy edge too, so the redundant NaN-strength edge must not mark it corrupt", corrupted, "n")
	}
}

// TestResurface_CorruptedIsSortedByUnitID proves corrupted's own ordering
// guarantee — the same one boosts already carries (TestResurface_Output-
// SortedByUnitID) — with a fixture built to actually exercise it: three
// refused units in one Neighbourhood, each reached by its own 1-hop edge,
// none in alphabetical insertion order.
//
// This guarantee was previously untested: removing sort.Strings(corrupted)
// from spread.go left the whole suite green, because every other fixture
// populating corrupted (TestResurface_NonFiniteState_RefusesRatherThanCoerces,
// the NaN-edge tests above) refuses exactly one unit — a single-element
// slice is "sorted" under any definition. corrupted is built by ranging
// over gains, a Go map, so append order is genuinely randomized per run
// once 2+ units are refused in the same call; this fixture's exact-order
// assertion below fails whenever a run's random map order does not
// happen to already be alphabetical.
//
// Mutation-verified: with sort.Strings(corrupted) removed from Resurface
// in spread.go, `go test ./internal/core/weight/ -run
// TestResurface_CorruptedIsSortedByUnitID -count=20` failed on several of
// the 20 runs (map iteration order is randomized per run, not fixed, so
// not every run fails) while every other test in this file stayed green;
// restored after, tree clean.
func TestResurface_CorruptedIsSortedByUnitID(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	n := Neighbourhood{
		Origin: "a",
		States: []Current{
			{UnitID: "zeta", Weight: math.NaN(), DecayRate: 0, LastTouchedAt: now},
			{UnitID: "alpha", Weight: math.NaN(), DecayRate: 0, LastTouchedAt: now},
			{UnitID: "mid", Weight: math.NaN(), DecayRate: 0, LastTouchedAt: now},
		},
		Edges: []Edge{
			{From: "a", To: "zeta", Strength: 1.0},
			{From: "a", To: "alpha", Strength: 1.0},
			{From: "a", To: "mid", Strength: 1.0},
		},
	}

	_, corrupted := Resurface(n, now)
	want := []string{"alpha", "mid", "zeta"}
	if len(corrupted) != len(want) {
		t.Fatalf("Resurface(...) corrupted = %v, want %v", corrupted, want)
	}
	for i := range want {
		if corrupted[i] != want[i] {
			t.Fatalf("Resurface(...) corrupted = %v, want %v exactly (sorted by UnitID, not map iteration order)", corrupted, want)
		}
	}
}

// TestResurface_RefusedUnitAlsoOnACorruptEdge_ReportedExactlyOnce pins that
// a unit corrupt on BOTH axes — a non-finite Current and an endpoint of a
// NaN-strength edge — appears in `corrupted` once, not twice.
//
// What actually enforces that is the sweep's `gains` membership check, NOT
// the `refused` guard beside it: `refused` is only ever assigned inside the
// loop over `gains`, so `refused` is a subset of `gains`, so anything the
// refused guard would catch the reachable guard catches first. Verified by
// removing the refused guard and watching this test still pass. Recorded as
// C17; the guard is dead and this comment says so rather than letting a
// future reader believe single-reporting rests on it.
//
// A duplicate is not cosmetic. `corrupted` exists so m2c can write one
// decision_log row per corrupt unit; two rows would state the vault holds two
// problems where it holds one, and this package's sorted order would place
// them adjacent, reading as a repeated fault rather than a double-count.
func TestResurface_RefusedUnitAlsoOnACorruptEdge_ReportedExactlyOnce(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	n := Neighbourhood{
		Origin: "a",
		States: []Current{
			{UnitID: "both", Weight: math.NaN(), DecayRate: 0, LastTouchedAt: now},
			zeroState("healthy"),
		},
		Edges: []Edge{
			{From: "a", To: "both", Strength: 1},                // healthy path: puts it in gains
			{From: "healthy", To: "both", Strength: math.NaN()}, // corrupt edge: puts it in corruptEdges
			{From: "a", To: "healthy", Strength: 1},
		},
	}

	_, corrupted := Resurface(n, now)

	occurrences := 0
	for _, id := range corrupted {
		if id == "both" {
			occurrences++
		}
	}
	if occurrences != 1 {
		t.Errorf("corrupted = %v, want %q exactly once, got it %d times", corrupted, "both", occurrences)
	}
}

// TestResurface_CorruptEdgeToAnUnloadedUnit_IsNotReported pins the sweep's
// states-membership guard.
//
// A neighbourhood is a bounded slice of the graph: m2c will load the states
// it intends to consider, and the relations table can name units outside that
// set. A NaN-strength edge pointing at one of them says nothing about a unit
// this call was ever asked to judge, so reporting it would put an id in
// `corrupted` that the caller holds no state for and cannot act on.
func TestResurface_CorruptEdgeToAnUnloadedUnit_IsNotReported(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	n := Neighbourhood{
		Origin: "a",
		States: []Current{zeroState("loaded")},
		Edges: []Edge{
			{From: "a", To: "loaded", Strength: 1},
			{From: "a", To: "not-loaded", Strength: math.NaN()},
		},
	}

	got, corrupted := Resurface(n, now)

	for _, id := range corrupted {
		if id == "not-loaded" {
			t.Errorf("corrupted = %v, want no entry for a unit this call holds no state for", corrupted)
		}
	}
	if len(got) != 1 || got[0].UnitID != "loaded" {
		t.Errorf("Resurface(...) = %+v, want the loaded neighbour boosted", got)
	}
}
