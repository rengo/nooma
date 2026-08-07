package consolidation

import (
	"math"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
)

// TestArchive_BelowThreshold_ArchivesTheUnit is the C14 length guard: it
// must fail on the below-threshold fixture's length before any content
// assertion runs, against a nil stub.
func TestArchive_BelowThreshold_ArchivesTheUnit(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	cs := []Cold{{UnitID: "u1", Status: unit.StatusPool, Weight: 0.4, DecayRate: 0, LastTouchedAt: now}}

	transitions, corrupted := Archive(cs, 0.5, now)
	if len(corrupted) != 0 {
		t.Fatalf("Archive() corrupted = %v, want none", corrupted)
	}
	if len(transitions) != 1 {
		t.Fatalf("Archive() returned %d transitions, want 1", len(transitions))
	}
	want := Transition{UnitID: "u1", From: unit.StatusPool, To: unit.StatusArchived, Reason: ReasonBelowWeightThreshold}
	if transitions[0] != want {
		t.Errorf("Archive()[0] = %+v, want %+v", transitions[0], want)
	}
}

// TestArchive_AtExactlyThreshold_DoesNotArchive proves R2.2's boundary is
// load-bearing: e == threshold is not below it.
func TestArchive_AtExactlyThreshold_DoesNotArchive(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	cs := []Cold{{UnitID: "u1", Status: unit.StatusPool, Weight: 0.5, DecayRate: 0, LastTouchedAt: now}}

	transitions, corrupted := Archive(cs, 0.5, now)
	if len(transitions) != 0 || len(corrupted) != 0 {
		t.Fatalf("Archive() = (%v, %v), want (nil, nil) — e == threshold must not archive", transitions, corrupted)
	}
}

// TestArchive_AboveThreshold_DoesNotArchive proves the other side of the
// boundary.
func TestArchive_AboveThreshold_DoesNotArchive(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	cs := []Cold{{UnitID: "u1", Status: unit.StatusPool, Weight: 0.6, DecayRate: 0, LastTouchedAt: now}}

	transitions, corrupted := Archive(cs, 0.5, now)
	if len(transitions) != 0 || len(corrupted) != 0 {
		t.Fatalf("Archive() = (%v, %v), want (nil, nil) for e > threshold", transitions, corrupted)
	}
}

// TestArchive_NonPoolStatus_ProducesNeitherOutput proves Archive only ever
// cools a live unit, even when the effective weight would otherwise
// qualify.
func TestArchive_NonPoolStatus_ProducesNeitherOutput(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	for _, s := range []unit.Status{unit.StatusArchived, unit.StatusSuperseded, unit.StatusIncomplete} {
		t.Run(string(s), func(t *testing.T) {
			cs := []Cold{{UnitID: "u1", Status: s, Weight: 0.1, DecayRate: 0, LastTouchedAt: now}}

			transitions, corrupted := Archive(cs, 0.5, now)
			if len(transitions) != 0 || len(corrupted) != 0 {
				t.Fatalf("Archive() = (%v, %v), want (nil, nil) for status %v even though weight is below threshold",
					transitions, corrupted, s)
			}
		})
	}
}

// TestArchive_NonFiniteWeightOrDecayRate_RefusesIntoCorrupted proves C15's
// rule at Archive's own door: a corrupt read is refused, never archived.
func TestArchive_NonFiniteWeightOrDecayRate_RefusesIntoCorrupted(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		weight    float64
		decayRate float64
	}{
		{"weight NaN", math.NaN(), 0},
		{"weight +Inf", math.Inf(1), 0},
		{"weight -Inf", math.Inf(-1), 0},
		{"decayRate NaN", 0.1, math.NaN()},
		{"decayRate +Inf", 0.1, math.Inf(1)},
		{"decayRate -Inf", 0.1, math.Inf(-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := []Cold{{UnitID: "u1", Status: unit.StatusPool, Weight: tt.weight, DecayRate: tt.decayRate, LastTouchedAt: now}}

			transitions, corrupted := Archive(cs, 0.5, now)
			if len(transitions) != 0 {
				t.Fatalf("Archive() transitions = %v, want none — a corrupt read must never archive", transitions)
			}
			if len(corrupted) != 1 || corrupted[0] != "u1" {
				t.Fatalf("Archive() corrupted = %v, want [u1]", corrupted)
			}
		})
	}
}

// TestArchive_BothOutputsSortedByUnitID is the mutation guard against a
// missing sort: inputs are handed in reverse order, for both transitions
// and corrupted.
func TestArchive_BothOutputsSortedByUnitID(t *testing.T) {
	now := time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)
	cs := []Cold{
		{UnitID: "charlie", Status: unit.StatusPool, Weight: 0.1, DecayRate: 0, LastTouchedAt: now},
		{UnitID: "alice", Status: unit.StatusPool, Weight: 0.1, DecayRate: 0, LastTouchedAt: now},
		{UnitID: "bob", Status: unit.StatusPool, Weight: 0.1, DecayRate: 0, LastTouchedAt: now},
		{UnitID: "zulu", Status: unit.StatusPool, Weight: math.NaN(), DecayRate: 0, LastTouchedAt: now},
		{UnitID: "yankee", Status: unit.StatusPool, Weight: math.NaN(), DecayRate: 0, LastTouchedAt: now},
		{UnitID: "xray", Status: unit.StatusPool, Weight: math.NaN(), DecayRate: 0, LastTouchedAt: now},
	}

	transitions, corrupted := Archive(cs, 0.5, now)
	if len(transitions) != 3 {
		t.Fatalf("Archive() returned %d transitions, want 3", len(transitions))
	}
	wantTransitions := []string{"alice", "bob", "charlie"}
	for i, id := range wantTransitions {
		if transitions[i].UnitID != id {
			t.Fatalf("Archive() transitions[%d].UnitID = %q, want %q — must be sorted", i, transitions[i].UnitID, id)
		}
	}

	if len(corrupted) != 3 {
		t.Fatalf("Archive() returned %d corrupted, want 3", len(corrupted))
	}
	wantCorrupted := []string{"xray", "yankee", "zulu"}
	for i, id := range wantCorrupted {
		if corrupted[i] != id {
			t.Fatalf("Archive() corrupted[%d] = %q, want %q — must be sorted", i, corrupted[i], id)
		}
	}
}

// TestResolveWeightThreshold_Nil_ReturnsDefault is the C14 content guard:
// it must fail against a stub returning a bare 0 rather than
// DefaultWeightThreshold.
func TestResolveWeightThreshold_Nil_ReturnsDefault(t *testing.T) {
	got := ResolveWeightThreshold(nil)
	if got != DefaultWeightThreshold {
		t.Errorf("ResolveWeightThreshold(nil) = %v, want %v", got, DefaultWeightThreshold)
	}
}

// TestResolveWeightThreshold_FiniteInRangeValue_PassesThrough proves a
// well-formed configured value is never second-guessed.
func TestResolveWeightThreshold_FiniteInRangeValue_PassesThrough(t *testing.T) {
	v := 0.8
	got := ResolveWeightThreshold(&v)
	if got != v {
		t.Errorf("ResolveWeightThreshold(&%v) = %v, want %v unchanged", v, got, v)
	}
}

// TestResolveWeightThreshold_NonFiniteOrOutOfRange_FallsBackToDefault
// proves R2.3's domain restriction: a value core cannot interpret is
// treated identically to no value at all, never trusted as-is.
func TestResolveWeightThreshold_NonFiniteOrOutOfRange_FallsBackToDefault(t *testing.T) {
	tests := []struct {
		name string
		v    float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
		{"negative", -0.1},
		{"above WeightCeiling", weight.WeightCeiling + 0.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.v
			got := ResolveWeightThreshold(&v)
			if got != DefaultWeightThreshold {
				t.Errorf("ResolveWeightThreshold(&%v) = %v, want default %v", tt.v, got, DefaultWeightThreshold)
			}
		})
	}
}
