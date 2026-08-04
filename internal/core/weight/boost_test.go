package weight

import (
	"math"
	"testing"
	"time"
)

// TestRevive_MatchesSpecWorkedExample pins R2.2's own scenario: a unit
// decayed to an effective weight of exactly 0.0 gets a revive of exactly
// ReviveGain * WeightCeiling = 0.35 * 2.0 = 0.70. This value is derived
// independently from the spec's own worked example, not by re-running
// Revive's own expression, so an implementation that (for instance)
// clamps additively instead of boosting asymptotically from zero would
// have to land on this same number by coincidence.
func TestRevive_MatchesSpecWorkedExample(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	c := Current{UnitID: "u1", Weight: 0, DecayRate: 0, LastTouchedAt: now}

	got := Revive(c, now)
	const want = 0.70
	if math.Abs(got.Weight-want) > 1e-9 {
		t.Errorf("Revive(weight=0, now).Weight = %v, want %v (0.35 * 2.0, spec R2.2's worked example)", got.Weight, want)
	}
	if got.LastTouchedAt != now {
		t.Errorf("Revive(...).LastTouchedAt = %v, want %v", got.LastTouchedAt, now)
	}
}

// TestRevive_ConvergesGeometricallyToCeiling pins the *shape* of repeated
// revives against a closed form derived independently of the
// implementation: writing g = ReviveGain and C = WeightCeiling, one
// revive step is e' = e + g*(C-e) = C - (1-g)*(C-e). Iterating k times
// from a starting effective weight e0 (at Δt=0 each time, so decay never
// intervenes) therefore gives
//
//	e_k = C - (C - e0) * (1-g)^k
//
// This is the test PR1's mutation review found missing for Effective: a
// property test alone ("strictly increasing", "never exceeds the
// ceiling") is satisfied by a linear ramp, a step function, or any other
// monotone-bounded shape. Pinning the exact geometric sequence
// discriminates the asymptotic formula from those alternatives — see the
// mutation check in this task's report.
func TestRevive_ConvergesGeometricallyToCeiling(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	const e0 = 0.5

	c := Current{UnitID: "u1", Weight: e0, DecayRate: 0, LastTouchedAt: now}
	for k := 1; k <= 6; k++ {
		got := Revive(c, now)
		want := WeightCeiling - (WeightCeiling-e0)*math.Pow(1-ReviveGain, float64(k))
		if math.Abs(got.Weight-want) > 1e-9 {
			t.Fatalf("iteration %d: Revive(...).Weight = %v, want %v (closed-form geometric convergence)", k, got.Weight, want)
		}
		if got.Weight >= WeightCeiling {
			t.Fatalf("iteration %d: Revive(...).Weight = %v, want strictly less than WeightCeiling = %v", k, got.Weight, WeightCeiling)
		}
		// Feed the result back in at the same instant (Δt = 0), so decay
		// never intervenes between iterations and the closed form holds
		// exactly.
		c = Current{UnitID: c.UnitID, Weight: got.Weight, DecayRate: 0, LastTouchedAt: got.LastTouchedAt}
	}
}

// TestRevive_StrictlyIncreasingUnderRepetition proves the property
// R2.2's own verification line asks for directly: applying Revive
// repeatedly at a fixed instant to its own output never plateaus, never
// reverses, and never crosses WeightCeiling — the qualitative envelope
// the closed-form test above already pins exactly, restated here as the
// property so a future change to the sampled starting point still holds.
func TestRevive_StrictlyIncreasingUnderRepetition(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	starts := []float64{0, 0.1, 0.5, 1.0, 1.9}

	for _, e0 := range starts {
		c := Current{UnitID: "u1", Weight: e0, DecayRate: 0, LastTouchedAt: now}
		prev := e0
		for i := 0; i < 50; i++ {
			got := Revive(c, now)
			if got.Weight <= prev {
				t.Fatalf("start %v, iteration %d: Revive(...).Weight = %v, want strictly greater than previous %v", e0, i, got.Weight, prev)
			}
			if got.Weight >= WeightCeiling {
				t.Fatalf("start %v, iteration %d: Revive(...).Weight = %v, want strictly less than WeightCeiling = %v", e0, i, got.Weight, WeightCeiling)
			}
			prev = got.Weight
			c = Current{UnitID: c.UnitID, Weight: got.Weight, DecayRate: 0, LastTouchedAt: got.LastTouchedAt}
		}
	}
}

// TestRevive_AtOrAboveCeiling_ReturnsEffectiveWeightUnchanged pins R2.3's
// edge exactly: when the effective weight at now is already at or above
// WeightCeiling, Revive neither raises nor lowers it — the returned
// Weight equals e exactly — but LastTouchedAt still moves to now.
func TestRevive_AtOrAboveCeiling_ReturnsEffectiveWeightUnchanged(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		weight float64
	}{
		{"exactly at the ceiling", WeightCeiling},
		{"above the ceiling", 3.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cur := Current{UnitID: "u1", Weight: c.weight, DecayRate: 0, LastTouchedAt: now}
			got := Revive(cur, now)
			if got.Weight != c.weight {
				t.Errorf("Revive(weight=%v, Δt=0).Weight = %v, want %v exactly (neither raised nor lowered)", c.weight, got.Weight, c.weight)
			}
			if got.LastTouchedAt != now {
				t.Errorf("Revive(weight=%v, Δt=0).LastTouchedAt = %v, want %v — a direct use still moves the clock at the ceiling", c.weight, got.LastTouchedAt, now)
			}
		})
	}
}

// TestRevive_NeverLowersAWeight proves the other half of R2.2's
// verification line: for any input, the returned weight is never less
// than the effective weight Revive started from.
func TestRevive_NeverLowersAWeight(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	lastTouchedAt := now.AddDate(0, 0, -30)

	cases := []Current{
		{UnitID: "u1", Weight: 0, DecayRate: 0.01, LastTouchedAt: lastTouchedAt},
		{UnitID: "u2", Weight: 1.0, DecayRate: 0.01, LastTouchedAt: lastTouchedAt},
		{UnitID: "u3", Weight: 2.5, DecayRate: 0.01, LastTouchedAt: lastTouchedAt},
		{UnitID: "u4", Weight: 1.0, DecayRate: 0, LastTouchedAt: now},
	}
	for _, c := range cases {
		e := Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
		got := Revive(c, now)
		if got.Weight < e {
			t.Errorf("Revive(%+v).Weight = %v, want >= effective weight %v (a boost never lowers)", c, got.Weight, e)
		}
	}
}

// TestRevive_AlwaysReturnsLastTouchedAtNow proves R2.3's signature-level
// consequence directly: Revive returns a bare Boost, never a (Boost,
// bool), and LastTouchedAt is now on every path, including the no-raise
// ceiling case exercised separately above.
func TestRevive_AlwaysReturnsLastTouchedAtNow(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	lastTouchedAt := now.AddDate(0, 0, -10)

	cases := []Current{
		{UnitID: "u1", Weight: 0.3, DecayRate: 0.01, LastTouchedAt: lastTouchedAt},
		{UnitID: "u2", Weight: WeightCeiling, DecayRate: 0, LastTouchedAt: now},
	}
	for _, c := range cases {
		got := Revive(c, now)
		if got.LastTouchedAt != now {
			t.Errorf("Revive(%+v).LastTouchedAt = %v, want %v", c, got.LastTouchedAt, now)
		}
		if got.UnitID != c.UnitID {
			t.Errorf("Revive(%+v).UnitID = %v, want %v", c, got.UnitID, c.UnitID)
		}
	}
}

// TestRevive_AtCeiling_IsEffectiveWeightNeutral proves R2.3's neutrality
// claim, independently derived rather than re-running Revive's own
// expression: when e >= WeightCeiling, Revive returns (e, now), and since
// e = w * exp(-λ*(now-lt)), the pairs (w, lt) and (e, now) denote the
// *same* decay curve — Effective must return the same value at every
// future instant from either pair. This is what justifies moving
// LastTouchedAt at the ceiling without materially changing the unit's
// future trajectory: the write is neutral by construction, not a
// coincidence of this one implementation.
func TestRevive_AtCeiling_IsEffectiveWeightNeutral(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := lastTouchedAt.AddDate(0, 0, 40)
	future := now.AddDate(0, 0, 365)

	c := Current{UnitID: "u1", Weight: 5.0, DecayRate: 0.01, LastTouchedAt: lastTouchedAt}
	e := Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
	if e < WeightCeiling {
		t.Fatalf("test fixture error: effective weight %v is not at or above WeightCeiling %v", e, WeightCeiling)
	}

	got := Revive(c, now)

	wantAtFuture := Effective(c.Weight, c.DecayRate, c.LastTouchedAt, future)
	gotAtFuture := Effective(got.Weight, c.DecayRate, got.LastTouchedAt, future)
	if math.Abs(gotAtFuture-wantAtFuture) > 1e-9 {
		t.Errorf("Effective(boosted pair, future) = %v, want %v (Effective(original pair, future)) — the ceiling write must be effective-weight-neutral", gotAtFuture, wantAtFuture)
	}
}
