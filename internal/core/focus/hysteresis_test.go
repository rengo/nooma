package focus

import (
	"math"
	"testing"
)

// TestDisplaces_RelativeMarginBoundaryTable proves spec R4.3's boundary,
// verbatim: equality never displaces, exactly at the margin never
// displaces, and any amount beyond the margin does. All four cases must be
// checked together — a stub that always returns false would pass the first
// two trivially and only the last two discriminate it from a real
// implementation.
func TestDisplaces_RelativeMarginBoundaryTable(t *testing.T) {
	const incumbent = 0.60
	const margin = 0.05

	tests := []struct {
		name       string
		challenger float64
		want       bool
	}{
		{"challenger == incumbent", incumbent, false},
		{"challenger == incumbent*(1+margin) exactly", incumbent * (1 + margin), false},
		{"challenger == incumbent*(1+margin) + epsilon", incumbent*(1+margin) + 1e-9, true},
		{"challenger < incumbent", incumbent - 0.01, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Displaces(tc.challenger, incumbent, margin)
			if got != tc.want {
				t.Errorf("Displaces(%v, %v, %v) = %v, want %v", tc.challenger, incumbent, margin, got, tc.want)
			}
		})
	}
}

// TestDisplaces_NonFinite proves the decision this package takes about a
// NaN or +Inf Score reaching Displaces (Rank's own doc comment: a Score can
// be either). Every case below is chosen so a naive `challenger >
// incumbent*(1+margin)` — with no scoreKey remap — gives the WRONG answer
// for at least one of the first three, which is what actually discriminates
// this from an unguarded implementation; the fourth (NaN challenger) is
// included for completeness even though a naive comparison happens to agree
// with it too (both give false), stated so this test is not mistaken for
// proving more than it does on that one case alone.
func TestDisplaces_NonFinite(t *testing.T) {
	const margin = 0.05

	t.Run("NaN incumbent is displaced by any finite challenger", func(t *testing.T) {
		// A naive `challenger > math.NaN()*(1+margin)` is false for every
		// challenger (IEEE 754: nothing is > NaN), which would make a
		// corrupted incumbent an unbeatable permanent occupant. The correct
		// answer is true: a corrupted incumbent holds no seat against real
		// data.
		if !Displaces(0.01, math.NaN(), margin) {
			t.Error("Displaces(0.01, NaN, margin) = false, want true — a NaN incumbent must not be a permanent occupant")
		}
	})

	t.Run("NaN challenger never displaces, even a NaN incumbent", func(t *testing.T) {
		if Displaces(math.NaN(), 0.60, margin) {
			t.Error("Displaces(NaN, 0.60, margin) = true, want false — a corrupted challenger contributes no promotion")
		}
		if Displaces(math.NaN(), math.NaN(), margin) {
			t.Error("Displaces(NaN, NaN, margin) = true, want false — when neither side compares, the incumbent keeps its seat")
		}
	})

	t.Run("equal +Inf does not displace", func(t *testing.T) {
		if Displaces(math.Inf(1), math.Inf(1), margin) {
			t.Error("Displaces(+Inf, +Inf, margin) = true, want false — equality never displaces, even at infinity")
		}
	})

	t.Run("+Inf challenger displaces a finite incumbent", func(t *testing.T) {
		if !Displaces(math.Inf(1), 0.60, margin) {
			t.Error("Displaces(+Inf, 0.60, margin) = false, want true")
		}
	})
}

// TestResolveMargin_NilFallsBackToDefault_NonNilPassesThrough proves spec
// R4.4: a nil configured pointer means the config row has never been
// written and falls back to DefaultHysteresisMargin; a non-nil pointer
// passes its value through unchanged, including a value that differs from
// the default (proving pass-through rather than a second, hidden fallback).
func TestResolveMargin_NilFallsBackToDefault_NonNilPassesThrough(t *testing.T) {
	if got := ResolveMargin(nil); got != DefaultHysteresisMargin {
		t.Errorf("ResolveMargin(nil) = %v, want DefaultHysteresisMargin (%v)", got, DefaultHysteresisMargin)
	}

	configured := 0.12
	if got := ResolveMargin(&configured); got != 0.12 {
		t.Errorf("ResolveMargin(&0.12) = %v, want 0.12 (pass-through, not the default %v)", got, DefaultHysteresisMargin)
	}
}

// TestResolveMargin_InvalidValuesMapToZero proves ResolveMargin is total
// over every float64 a non-nil configured can hold, closing the gap
// Judgment Day round 1 found (both judges, independently): margin flows
// straight from config.hysteresis_margin (migration 0002, `REAL NOT NULL
// DEFAULT 0.05`, no `CHECK` constraint) through ResolveMargin with zero
// validation, unlike Score's own NaN boundary (scoreKey). Every value
// outside [0, +Inf) — NaN, ±Inf, and any negative value, not only
// margin <= -1 — resolves to 0, never to DefaultHysteresisMargin (this
// function's own doc comment justifies why 0).
func TestResolveMargin_InvalidValuesMapToZero(t *testing.T) {
	tests := []struct {
		name string
		in   float64
	}{
		{"NaN", math.NaN()},
		{"-1 exactly", -1},
		{"-1 minus epsilon", -1 - 1e-9},
		{"-2", -2},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
		{"a small negative value, not only <= -1", -0.01},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			if got := ResolveMargin(&in); got != 0 {
				t.Errorf("ResolveMargin(&%v) = %v, want 0", tc.in, got)
			}
		})
	}
}

// TestResolveMargin_ValidBoundaryValuesPassThroughUnchanged proves
// ResolveMargin's valid domain is exactly [0, +Inf) finite: -0.0 (IEEE
// 754: -0.0 == 0.0, never < 0), 0, and an arbitrarily large finite value
// are not corrupted input and must pass through unchanged, not collapse
// to 0 or to DefaultHysteresisMargin.
func TestResolveMargin_ValidBoundaryValuesPassThroughUnchanged(t *testing.T) {
	tests := []struct {
		name string
		in   float64
	}{
		{"negative zero", math.Copysign(0, -1)},
		{"zero", 0},
		{"a very large finite value", 1e300},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			got := ResolveMargin(&in)
			if got != tc.in {
				t.Errorf("ResolveMargin(&%v) = %v, want %v unchanged", tc.in, got, tc.in)
			}
		})
	}
}

// TestDisplaces_ResolvedInvalidMargin_NaNIncumbentStillDisplaced proves the
// fix end to end, through the actual production door: before this fix, a
// NaN incumbent combined with a raw config.hysteresis_margin of -1 made
// Displaces false for every challenger (margin*(1+margin) collapsed to
// -Inf*0 = NaN, and "x > NaN" is false under IEEE 754 for every x) — a
// corrupted incumbent became an unbeatable permanent occupant, exactly the
// class this function's own doc comment claims to have eliminated. Every
// raw value below reproduces a distinct way the unvalidated margin used to
// break the comparison; after ResolveMargin, all of them must still let a
// legitimate, non-NaN challenger through.
func TestDisplaces_ResolvedInvalidMargin_NaNIncumbentStillDisplaced(t *testing.T) {
	rawMargins := []struct {
		name string
		raw  float64
	}{
		{"NaN", math.NaN()},
		{"-1", -1},
		{"-2", -2},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	}

	for _, tc := range rawMargins {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.raw
			margin := ResolveMargin(&raw)
			if !Displaces(0.01, math.NaN(), margin) {
				t.Errorf("Displaces(0.01, NaN, ResolveMargin(&%v)=%v) = false, want true — a NaN incumbent must not be a permanent occupant regardless of a corrupted raw margin", tc.raw, margin)
			}
		})
	}
}
