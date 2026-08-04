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

	got := Resurface(n, now)
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

	got := Resurface(n, now)
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

	got := Resurface(n, now)
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

	forward := Resurface(Neighbourhood{
		Origin: "a",
		States: states,
		Edges:  []Edge{{From: "a", To: "n", Strength: 0.8}},
	}, now)
	backward := Resurface(Neighbourhood{
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

	got := Resurface(n, now)
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

	got := Resurface(n, now)
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
				States: []Current{{UnitID: "n", Weight: tc.weight, DecayRate: 0, LastTouchedAt: now}},
				Edges:  []Edge{{From: "a", To: "n", Strength: 1.0}},
			}
			got := Resurface(n, now)
			if len(got) != 0 {
				t.Errorf("Resurface(weight=%v at target) = %+v, want an empty slice (R2.6 — no Boost, not a zero-delta entry)", tc.weight, got)
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

	got := Resurface(n, now)
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

	got := Resurface(n, now)
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

	got := Resurface(n, now)
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
		got := Resurface(n, now)
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
		got := Resurface(n, now)
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
		got := Resurface(n, now)
		assertBoosts(t, got, now, []struct {
			id     string
			weight float64
		}{{"n", 0.035}}) // 0.35 * 0.10
	})

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
		got := Resurface(n, now)
		for _, b := range got {
			if b.UnitID == "d" {
				t.Errorf("Resurface(...) included %q at 3 hops, want it absent (ResurfaceMaxHops = %d)", "d", ResurfaceMaxHops)
			}
		}
	})
}
