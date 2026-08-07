package consolidation

import (
	"math"
	"testing"
	"time"
)

// TestStrengthen_SinceNil_ReturnsNothing is the C14 length guard: since ==
// nil must short-circuit to (nil, nil) for any input, including one that
// would otherwise qualify — "accumulated evidence over no interval is no
// evidence" (design.md §4.3).
func TestStrengthen_SinceNil_ReturnsNothing(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	es := []RelationEvidence{
		{RelationID: "r1", Strength: 0.5, FromLastTouchedAt: now, ToLastTouchedAt: now},
		{RelationID: "r2", Strength: math.NaN(), FromLastTouchedAt: now, ToLastTouchedAt: now},
	}

	changes, corrupted := Strengthen(es, nil)
	if changes != nil || corrupted != nil {
		t.Fatalf("Strengthen(es, nil) = (%v, %v), want (nil, nil) for any input", changes, corrupted)
	}
}

// TestStrengthen_OneEndpointBeforeSince_ProducesNoRow proves the co-active
// gate requires BOTH endpoints.
func TestStrengthen_OneEndpointBeforeSince_ProducesNoRow(t *testing.T) {
	since := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	before := since.Add(-time.Hour)
	after := since.Add(time.Hour)

	es := []RelationEvidence{
		{RelationID: "r1", Strength: 0.5, FromLastTouchedAt: before, ToLastTouchedAt: after},
	}

	changes, corrupted := Strengthen(es, &since)
	if len(changes) != 0 || len(corrupted) != 0 {
		t.Fatalf("Strengthen() = (%v, %v), want (nil, nil) when one endpoint is before since", changes, corrupted)
	}
}

// TestStrengthen_BothEndpointsAtExactlySince_Qualifies proves Before is
// strict: a touch at exactly since counts as co-active.
func TestStrengthen_BothEndpointsAtExactlySince_Qualifies(t *testing.T) {
	since := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	es := []RelationEvidence{
		{RelationID: "r1", Strength: 0.5, FromLastTouchedAt: since, ToLastTouchedAt: since},
	}

	changes, corrupted := Strengthen(es, &since)
	if len(corrupted) != 0 {
		t.Fatalf("Strengthen() corrupted = %v, want none", corrupted)
	}
	if len(changes) != 1 {
		t.Fatalf("Strengthen() returned %d changes, want 1 — a touch at exactly since must qualify", len(changes))
	}
	want := 0.5 + StrengthenGain*(1-0.5)
	if changes[0] != (StrengthChange{RelationID: "r1", Strength: want}) {
		t.Errorf("Strengthen()[0] = %+v, want {r1 %v}", changes[0], want)
	}
}

// TestStrengthen_NeverReachesOne proves the law is asymptotic: repeated
// application approaches but never reaches 1.
func TestStrengthen_NeverReachesOne(t *testing.T) {
	since := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	s := 0.5

	for i := 0; i < 500; i++ {
		es := []RelationEvidence{{RelationID: "r1", Strength: s, FromLastTouchedAt: since, ToLastTouchedAt: since}}
		changes, corrupted := Strengthen(es, &since)
		if len(corrupted) != 0 {
			t.Fatalf("Strengthen() corrupted = %v at iteration %d, want none", corrupted, i)
		}
		if len(changes) != 1 {
			t.Fatalf("Strengthen() returned %d changes at iteration %d, want 1", len(changes), i)
		}
		s = changes[0].Strength
		if s >= 1 {
			t.Fatalf("Strengthen() reached or exceeded strength 1 at iteration %d: %v", i, s)
		}
	}
	if s < 0.99 {
		t.Errorf("after 500 nightly passes, strength = %v, want it to have converged close to 1", s)
	}
}

// TestStrengthen_AlreadyAtOne_ProducesNoRow proves doc 02 §11's "no effect,
// no write" for a relation that has already converged.
func TestStrengthen_AlreadyAtOne_ProducesNoRow(t *testing.T) {
	since := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	es := []RelationEvidence{{RelationID: "r1", Strength: 1, FromLastTouchedAt: since, ToLastTouchedAt: since}}

	changes, corrupted := Strengthen(es, &since)
	if len(changes) != 0 || len(corrupted) != 0 {
		t.Fatalf("Strengthen() = (%v, %v), want (nil, nil) for a relation already at strength 1", changes, corrupted)
	}
}

// TestStrengthen_NonFiniteOrOutOfRangeStrength_RefusesIntoCorrupted proves
// C15's rule at Strengthen's own door: a co-active relation whose Strength
// core cannot interpret is refused, never computed into a change, each
// shape tested individually.
//
// The +Inf/-Inf cases still earn their names after Fix E (Judgment Day
// round 1) removed the redundant math.IsInf check from Strengthen: they
// assert that these specific values are refused into corrupted, which
// remains true because +Inf > 1 and -Inf < 0 already trigger the range
// comparison under IEEE 754 — the behaviour these subtests name did not
// change, only the implementation detail of which branch catches it.
func TestStrengthen_NonFiniteOrOutOfRangeStrength_RefusesIntoCorrupted(t *testing.T) {
	since := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

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
			es := []RelationEvidence{{RelationID: "r1", Strength: tt.strength, FromLastTouchedAt: since, ToLastTouchedAt: since}}

			changes, corrupted := Strengthen(es, &since)
			if len(changes) != 0 {
				t.Fatalf("Strengthen() changes = %v, want none — a corrupt strength must never compute a change", changes)
			}
			if len(corrupted) != 1 || corrupted[0] != "r1" {
				t.Fatalf("Strengthen() corrupted = %v, want [r1]", corrupted)
			}
		})
	}
}

// TestStrengthen_ChangesAndCorruptedSortedByRelationID is the mutation
// guard against a missing sort: inputs handed in reverse order, for both
// changes and corrupted, at least three of each.
func TestStrengthen_ChangesAndCorruptedSortedByRelationID(t *testing.T) {
	since := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	es := []RelationEvidence{
		{RelationID: "charlie", Strength: 0.5, FromLastTouchedAt: since, ToLastTouchedAt: since},
		{RelationID: "alice", Strength: 0.5, FromLastTouchedAt: since, ToLastTouchedAt: since},
		{RelationID: "bob", Strength: 0.5, FromLastTouchedAt: since, ToLastTouchedAt: since},
		{RelationID: "zulu", Strength: math.NaN(), FromLastTouchedAt: since, ToLastTouchedAt: since},
		{RelationID: "yankee", Strength: math.NaN(), FromLastTouchedAt: since, ToLastTouchedAt: since},
		{RelationID: "xray", Strength: math.NaN(), FromLastTouchedAt: since, ToLastTouchedAt: since},
	}

	changes, corrupted := Strengthen(es, &since)
	if len(changes) != 3 {
		t.Fatalf("Strengthen() returned %d changes, want 3", len(changes))
	}
	wantChanges := []string{"alice", "bob", "charlie"}
	for i, id := range wantChanges {
		if changes[i].RelationID != id {
			t.Fatalf("Strengthen() changes[%d].RelationID = %q, want %q — must be sorted", i, changes[i].RelationID, id)
		}
	}

	if len(corrupted) != 3 {
		t.Fatalf("Strengthen() returned %d corrupted, want 3", len(corrupted))
	}
	wantCorrupted := []string{"xray", "yankee", "zulu"}
	for i, id := range wantCorrupted {
		if corrupted[i] != id {
			t.Fatalf("Strengthen() corrupted[%d] = %q, want %q — must be sorted", i, corrupted[i], id)
		}
	}
}

// TestStrengthenGain_IsPinnedToItsCalibratedValue guards StrengthenGain
// against a wrong VALUE, independent of every shape test in this file. It
// exists because of C7 and C28 (openspec/changes/m2a-weight-focus/tasks.md):
// TestStrengthen_ConvergesAsymptotically and its siblings compute their
// expectation as e.Strength + StrengthenGain*(1-e.Strength) — correct for
// pinning the law's shape, but structurally incapable of noticing the
// constant moved, since a mutated StrengthenGain flows into their "want"
// exactly as it flows into Strengthen's output.
//
// C28 recorded that C7's convention did not transfer from internal/core/weight
// to internal/core/focus, and named m2b as the next place it would be needed.
// It did not transfer again: PR 2 of this very change shipped
// IncompleteExpiryHours with no pin, and mutating it 24 -> 23 or 24 -> 25 left
// the package green at 100% statement coverage.
//
// Note what this test does NOT claim. doc 02 §13 records strengthen_gain as
// CHOSEN, not derived: the 21-night convergence to goal_stagnation_days is a
// compatibility check against that knob's DEFAULT, and any gain in roughly
// [0.0994, 0.1040) satisfies the same identity. goal_stagnation_days is also
// per-user recalibratable while StrengthenGain reads no config, so the
// compatibility holds at the default and nowhere else. This literal is the
// chosen number, and recalibrating it means editing the §13 row and this line
// in the same PR.
func TestStrengthenGain_IsPinnedToItsCalibratedValue(t *testing.T) {
	const want = 0.10
	if StrengthenGain != want {
		t.Fatalf("StrengthenGain = %v, want %v — doc 02 §13's chosen value; recalibrating means editing the §13 row and this literal together", StrengthenGain, want)
	}
}
