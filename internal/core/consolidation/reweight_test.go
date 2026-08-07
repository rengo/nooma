package consolidation

import (
	"math"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/weight"
)

// hop1Boost mirrors weight.Resurface's own arithmetic for a unit reached at
// exactly one hop with a direct edge of the given strength, starting from
// an effective weight of exactly zero (Weight: 0, DecayRate: 0) — computed
// from the same named constants Resurface itself uses, in the same
// operation order, so the two are bit-for-bit comparable rather than
// independently-rounded decimal literals.
func hop1Boost(strength float64) float64 {
	gain := strength * weight.ResurfaceAttenuation
	target := gain * weight.WeightCeiling
	return weight.ReviveGain * target
}

// TestReweight_NewEdgeBoostsBothEndpoints is the C14 length guard: it must
// fail on length before any content assertion runs, against a nil stub.
func TestReweight_NewEdgeBoostsBothEndpoints(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	states := map[string]weight.Current{
		"a": {UnitID: "a", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"b": {UnitID: "b", Weight: 0, DecayRate: 0, LastTouchedAt: now},
	}
	edges := []weight.Edge{{From: "a", To: "b", Strength: 0.9}}

	boosts, corrupted := Reweight(states, edges, now)
	if len(corrupted) != 0 {
		t.Fatalf("Reweight() corrupted = %v, want none", corrupted)
	}
	if len(boosts) != 2 {
		t.Fatalf("Reweight() returned %d boosts, want 2 (both endpoints of the new edge)", len(boosts))
	}

	want := hop1Boost(0.9)
	byUnit := map[string]weight.Boost{}
	for _, b := range boosts {
		byUnit[b.UnitID] = b
	}
	for _, id := range []string{"a", "b"} {
		b, ok := byUnit[id]
		if !ok {
			t.Fatalf("Reweight() boosts missing unit %q", id)
		}
		if b.Weight != want {
			t.Errorf("Reweight() boost for %q = %v, want %v", id, b.Weight, want)
		}
		if !b.LastTouchedAt.Equal(now) {
			t.Errorf("Reweight() boost for %q LastTouchedAt = %v, want %v", id, b.LastTouchedAt, now)
		}
	}
}

// TestReweight_MultiOriginResultsMergeByMax proves the per-unit merge takes
// the highest boosted weight across origins, not the last one processed.
//
// hub is connected directly to leaf1 (strength 0.9) and leaf2 (strength
// 0.3). Sorted origin order is hub, leaf1, leaf2, so leaf2's boost — first
// set to its own direct value (0.105) by origin=hub's call — is later
// re-visited by origin=leaf1's call via the two-hop path leaf1->hub->leaf2,
// whose attenuated gain is strictly smaller. A merge that took "last call
// wins" instead of max would incorrectly shrink leaf2's boost (and,
// symmetrically, leaf1's, when origin=leaf2's call runs last). Asserting
// the direct (larger) value for all three survives is the mutation guard.
func TestReweight_MultiOriginResultsMergeByMax(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	states := map[string]weight.Current{
		"hub":   {UnitID: "hub", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"leaf1": {UnitID: "leaf1", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"leaf2": {UnitID: "leaf2", Weight: 0, DecayRate: 0, LastTouchedAt: now},
	}
	edges := []weight.Edge{
		{From: "leaf1", To: "hub", Strength: 0.9},
		{From: "leaf2", To: "hub", Strength: 0.3},
	}

	boosts, corrupted := Reweight(states, edges, now)
	if len(corrupted) != 0 {
		t.Fatalf("Reweight() corrupted = %v, want none", corrupted)
	}
	if len(boosts) != 3 {
		t.Fatalf("Reweight() returned %d boosts, want 3", len(boosts))
	}

	byUnit := map[string]float64{}
	for _, b := range boosts {
		byUnit[b.UnitID] = b.Weight
	}

	wantHub := hop1Boost(0.9)   // from origin=leaf1's direct edge
	wantLeaf1 := hop1Boost(0.9) // from origin=hub's direct edge
	wantLeaf2 := hop1Boost(0.3) // from origin=hub's direct edge

	if byUnit["hub"] != wantHub {
		t.Errorf("Reweight() boost for hub = %v, want %v (the direct value, not the smaller two-hop one)", byUnit["hub"], wantHub)
	}
	if byUnit["leaf1"] != wantLeaf1 {
		t.Errorf("Reweight() boost for leaf1 = %v, want %v (the direct value, not the smaller two-hop one)", byUnit["leaf1"], wantLeaf1)
	}
	if byUnit["leaf2"] != wantLeaf2 {
		t.Errorf("Reweight() boost for leaf2 = %v, want %v (the direct value, not the smaller two-hop one)", byUnit["leaf2"], wantLeaf2)
	}
}

// TestReweight_CorruptEdgeStrength_RefusedAtReweightsOwnDoor proves the
// C15/C19 entry-point rule: an edge strength Reweight cannot interpret is
// refused before weight.clampStrength or any comparison downstream can
// skip past it, and never produces a boost.
//
// The +Inf/-Inf cases still earn their names after Fix E (Judgment Day
// round 1) removed the redundant math.IsInf check from
// invalidEdgeStrength: +Inf > 1 and -Inf < 0 already trigger the range
// comparison under IEEE 754, so these specific values are still refused —
// only the branch that catches them changed, not the behaviour asserted.
func TestReweight_CorruptEdgeStrength_RefusedAtReweightsOwnDoor(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	states := map[string]weight.Current{
		"a": {UnitID: "a", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"b": {UnitID: "b", Weight: 0, DecayRate: 0, LastTouchedAt: now},
	}

	tests := []struct {
		name     string
		strength float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
		{"negative", -0.5},
		{"above 1", 1.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := []weight.Edge{{From: "a", To: "b", Strength: tt.strength}}

			boosts, corrupted := Reweight(states, edges, now)
			if len(boosts) != 0 {
				t.Fatalf("Reweight() boosts = %v, want none — a corrupt edge strength must never compute a boost", boosts)
			}
			if len(corrupted) != 2 || corrupted[0] != "a" || corrupted[1] != "b" {
				t.Fatalf("Reweight() corrupted = %v, want [a b]", corrupted)
			}
		})
	}
}

// TestReweight_CorruptEdgeToAnUnloadedUnitIsNotReported proves Reweight's
// own edge-strength door aligns with weight.Resurface's settled policy
// (TestResurface_CorruptEdgeToAnUnloadedUnit_IsNotReported): reporting an
// endpoint the caller holds no Current for would put an id in corrupted
// the caller cannot act on, so it is not reported. Fix D, Judgment Day
// round 1 — Reweight previously marked BOTH corruptSet[e.From] and
// corruptSet[e.To] from invalidEdgeStrength alone, with no states
// membership check, silently differing from Resurface's own rule in the
// same package family.
//
// states only holds "a"; "ghost" is an edge endpoint the caller never
// loaded a Current for.
func TestReweight_CorruptEdgeToAnUnloadedUnitIsNotReported(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	states := map[string]weight.Current{
		"a": {UnitID: "a", Weight: 0, DecayRate: 0, LastTouchedAt: now},
	}
	edges := []weight.Edge{{From: "a", To: "ghost", Strength: math.NaN()}}

	boosts, corrupted := Reweight(states, edges, now)
	if len(boosts) != 0 {
		t.Fatalf("Reweight() boosts = %v, want none", boosts)
	}
	if len(corrupted) != 1 || corrupted[0] != "a" {
		t.Fatalf("Reweight() corrupted = %v, want [a] only — \"ghost\" has no Current in states and must not be reported", corrupted)
	}
}

// TestReweight_CorruptedMergedByUnionNotCount proves the pass-wide merge
// rule: a unit reported corrupted by more than one origin's Resurface call
// appears at most once in Reweight's own output, never once per reporting
// origin.
//
// corrupt1's own Weight is NaN. Two independent, valid edges each connect
// a different origin (origin1, origin2) directly to corrupt1, so both
// origin1's and origin2's Resurface calls independently discover and
// report corrupt1's non-finite state — the same unit, reported twice
// internally, must merge into one entry.
func TestReweight_CorruptedMergedByUnionNotCount(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	states := map[string]weight.Current{
		"origin1":  {UnitID: "origin1", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"origin2":  {UnitID: "origin2", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"corrupt1": {UnitID: "corrupt1", Weight: math.NaN(), DecayRate: 0, LastTouchedAt: now},
	}
	edges := []weight.Edge{
		{From: "origin1", To: "corrupt1", Strength: 0.9},
		{From: "origin2", To: "corrupt1", Strength: 0.9},
	}

	_, corrupted := Reweight(states, edges, now)
	if len(corrupted) != 1 || corrupted[0] != "corrupt1" {
		t.Fatalf("Reweight() corrupted = %v, want exactly [corrupt1] — reported by two origins, merged into one", corrupted)
	}
}

// TestReweight_UnitMayAppearInBothBoostsAndCorrupted proves neither output
// suppresses the other: x is legitimately boosted through a clean edge to
// a, and separately reported corrupted because a DIFFERENT edge (b-x) has
// a NaN strength Reweight refuses at its own door — both facts about x are
// true and both are reported.
func TestReweight_UnitMayAppearInBothBoostsAndCorrupted(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	states := map[string]weight.Current{
		"a": {UnitID: "a", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"b": {UnitID: "b", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"x": {UnitID: "x", Weight: 0, DecayRate: 0, LastTouchedAt: now},
	}
	edges := []weight.Edge{
		{From: "a", To: "x", Strength: 0.9},
		{From: "b", To: "x", Strength: math.NaN()},
	}

	boosts, corrupted := Reweight(states, edges, now)

	boostedUnits := map[string]bool{}
	for _, b := range boosts {
		boostedUnits[b.UnitID] = true
	}
	corruptedUnits := map[string]bool{}
	for _, id := range corrupted {
		corruptedUnits[id] = true
	}

	if !boostedUnits["x"] {
		t.Errorf("Reweight() boosts = %v, want x present (boosted through the clean a-x edge)", boosts)
	}
	if !corruptedUnits["x"] {
		t.Errorf("Reweight() corrupted = %v, want x present (refused through the corrupt b-x edge) — neither output should suppress the other", corrupted)
	}
	if !corruptedUnits["b"] {
		t.Errorf("Reweight() corrupted = %v, want b present (the corrupt edge's other endpoint)", corrupted)
	}
}

// TestReweight_BoostsAndCorruptedSortedByUnitID is the mutation guard
// against a missing final sort, on both returned slices.
//
// Judgment Day round 1, Fix C: an earlier version of this fixture used only
// three entries per slice. With `boosts`/`corrupted` built by iterating Go
// maps, a 3-element random permutation lands already sorted 1-in-6 of the
// time by chance alone — measured at a 43-57% kill rate across repeated
// removals of the trailing sort, worse than a coin flip and unfit to call a
// guard. This fixture uses eight units per slice (1-in-40320 chance of an
// accidentally-sorted permutation) specifically so the guard is
// deterministic rather than probabilistic. Verified by removing
// Reweight's trailing sort.Slice/sort.Strings and running this test 40
// times: 40/40 FAIL (0/40 accidental PASS).
func TestReweight_BoostsAndCorruptedSortedByUnitID(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)

	// n1..n8 form a chain (n1-n2-n3-...-n8) of clean units. Every unit in
	// the chain is also an origin (every edge endpoint is an origin), and
	// ResurfaceMaxHops == 2 means every unit is reached by at least one
	// neighbour's Resurface call — all eight end up in boosts.
	//
	// c1..c8 must ALSO have a states entry (Fix D, Judgment Day round 1):
	// Reweight's own door only reports a corrupt edge's endpoint into
	// corrupted when the caller holds a Current for it, mirroring
	// weight.Resurface's own settled policy
	// (TestResurface_CorruptEdgeToAnUnloadedUnit_IsNotReported) — an id
	// the caller has no state for is not actionable and is not reported.
	states := map[string]weight.Current{
		"n1": {UnitID: "n1", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"n2": {UnitID: "n2", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"n3": {UnitID: "n3", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"n4": {UnitID: "n4", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"n5": {UnitID: "n5", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"n6": {UnitID: "n6", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"n7": {UnitID: "n7", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"n8": {UnitID: "n8", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"c1": {UnitID: "c1", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"c2": {UnitID: "c2", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"c3": {UnitID: "c3", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"c4": {UnitID: "c4", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"c5": {UnitID: "c5", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"c6": {UnitID: "c6", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"c7": {UnitID: "c7", Weight: 0, DecayRate: 0, LastTouchedAt: now},
		"c8": {UnitID: "c8", Weight: 0, DecayRate: 0, LastTouchedAt: now},
	}
	edges := []weight.Edge{
		{From: "n1", To: "n2", Strength: 0.9},
		{From: "n2", To: "n3", Strength: 0.9},
		{From: "n3", To: "n4", Strength: 0.9},
		{From: "n4", To: "n5", Strength: 0.9},
		{From: "n5", To: "n6", Strength: 0.9},
		{From: "n6", To: "n7", Strength: 0.9},
		{From: "n7", To: "n8", Strength: 0.9},
		// Four disjoint NaN-strength edges, refused at Reweight's own
		// door — eight distinct corrupted ids, none overlapping the
		// chain above.
		{From: "c1", To: "c2", Strength: math.NaN()},
		{From: "c3", To: "c4", Strength: math.NaN()},
		{From: "c5", To: "c6", Strength: math.NaN()},
		{From: "c7", To: "c8", Strength: math.NaN()},
	}

	boosts, corrupted := Reweight(states, edges, now)
	if len(boosts) != 8 {
		t.Fatalf("Reweight() returned %d boosts, want 8 (n1..n8)", len(boosts))
	}
	for i := 1; i < len(boosts); i++ {
		if boosts[i-1].UnitID >= boosts[i].UnitID {
			t.Fatalf("Reweight() boosts not sorted by UnitID: %v", boosts)
		}
	}

	if len(corrupted) != 8 {
		t.Fatalf("Reweight() returned %d corrupted, want 8 (c1..c8)", len(corrupted))
	}
	want := []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8"}
	for i, id := range want {
		if corrupted[i] != id {
			t.Fatalf("Reweight() corrupted[%d] = %q, want %q — must be sorted", i, corrupted[i], id)
		}
	}
}
