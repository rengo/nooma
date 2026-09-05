package relation

import (
	"math"
	"testing"
)

// confirmTestThresholds sweeps a handful of Thresholds shapes, including one
// whose Persist and Surface are equal (a degenerate but legal band) — the
// three properties below must hold for every one of them, not just the
// package defaults.
func confirmTestThresholds() []Thresholds {
	return []Thresholds{
		{Persist: 0.30, Surface: 0.50},
		{Persist: 0.0, Surface: 1.0},
		{Persist: 0.42, Surface: 0.42},
		{Persist: 0.10, Surface: 0.90},
	}
}

// confirmTestConfidences sweeps confidence values relative to each
// threshold pair: below persist, at persist, inside the band, at surface,
// above surface, and NaN — relation.confidence is a REAL column with no
// CHECK, so a NaN can be read back from it (design §3.3's own reasoning).
func confirmTestConfidences(t Thresholds) []float64 {
	return []float64{
		t.Persist - 0.2,
		t.Persist,
		(t.Persist + t.Surface) / 2,
		t.Surface,
		t.Surface + 0.2,
		math.NaN(),
	}
}

// TestConfirmedConfidence_AlwaysLeavesTheBand is design §3.3's property 1:
// Decide(ConfirmedConfidence(c, t), t) == Asserted for every c and every t —
// confirming always leaves the band, which is the whole point of the
// function and the thing a future edit could break.
func TestConfirmedConfidence_AlwaysLeavesTheBand(t *testing.T) {
	for _, thresholds := range confirmTestThresholds() {
		for _, c := range confirmTestConfidences(thresholds) {
			got := ConfirmedConfidence(c, thresholds)
			verdict := Decide(got, thresholds)
			if verdict != Asserted {
				t.Errorf("Decide(ConfirmedConfidence(%v, %+v), %+v) = %v, want Asserted — confirming must always leave the band",
					c, thresholds, thresholds, verdict)
			}
		}
	}
}

// TestConfirmedConfidence_Idempotent is design §3.3's property 2:
// ConfirmedConfidence(ConfirmedConfidence(c, t), t) == ConfirmedConfidence(c, t) —
// applying it twice is the same as applying it once, which is what makes a
// second "yes" answer retry-safe (design §3.6).
func TestConfirmedConfidence_Idempotent(t *testing.T) {
	for _, thresholds := range confirmTestThresholds() {
		for _, c := range confirmTestConfidences(thresholds) {
			once := ConfirmedConfidence(c, thresholds)
			twice := ConfirmedConfidence(once, thresholds)
			if once != twice && !(math.IsNaN(once) && math.IsNaN(twice)) {
				t.Errorf("ConfirmedConfidence(ConfirmedConfidence(%v, %+v), %+v) = %v, want %v (idempotent)",
					c, thresholds, thresholds, twice, once)
			}
		}
	}
}

// TestConfirmedConfidence_NeverLowers is design §3.3's property 3:
// ConfirmedConfidence(c, t) >= c for every non-NaN c — confirming never
// lowers a confidence. A relation already above the floor keeps its own
// number.
func TestConfirmedConfidence_NeverLowers(t *testing.T) {
	for _, thresholds := range confirmTestThresholds() {
		for _, c := range confirmTestConfidences(thresholds) {
			if math.IsNaN(c) {
				continue
			}
			got := ConfirmedConfidence(c, thresholds)
			if got < c {
				t.Errorf("ConfirmedConfidence(%v, %+v) = %v, want >= %v — confirming must never lower a confidence",
					c, thresholds, got, c)
			}
		}
	}
}

// TestConfirmedConfidence_NaNLandsOnTheFloor pins design §3.3's own stated
// reason for the `!(current > t.Surface)` form over math.Max: math.Max
// propagates a NaN, and Decide would then read NaN as Asserted by ACCIDENT
// (both of its comparisons against a NaN confidence fail, so it falls
// through to the default arm) — which would make property 1 above pass for
// the wrong reason. This form lands NaN on the floor instead, where the
// row is honest.
//
// Mutation (task 2.1's own line): swapping the body for
// `return math.Max(current, t.Surface)` must fail THIS test, even though
// TestConfirmedConfidence_AlwaysLeavesTheBand would still pass — math.Max
// leaves Decide(NaN, t) landing on Asserted by the same accidental
// fallthrough, so only a direct assertion on the returned value (not on
// Decide's verdict) catches the regression.
func TestConfirmedConfidence_NaNLandsOnTheFloor(t *testing.T) {
	for _, thresholds := range confirmTestThresholds() {
		got := ConfirmedConfidence(math.NaN(), thresholds)
		if got != thresholds.Surface {
			t.Errorf("ConfirmedConfidence(NaN, %+v) = %v, want %v (the floor) — math.Max would have propagated the NaN instead",
				thresholds, got, thresholds.Surface)
		}
	}
}
