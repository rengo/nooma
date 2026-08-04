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

	got, ok := Revive(c, now)
	if !ok {
		t.Fatalf("Revive(weight=0, now) refused a finite input, want ok = true")
	}
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
		got, ok := Revive(c, now)
		if !ok {
			t.Fatalf("iteration %d: Revive(...) refused a finite input, want ok = true", k)
		}
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
			got, ok := Revive(c, now)
			if !ok {
				t.Fatalf("start %v, iteration %d: Revive(...) refused a finite input, want ok = true", e0, i)
			}
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
			got, ok := Revive(cur, now)
			if !ok {
				t.Fatalf("Revive(weight=%v, Δt=0) refused a finite input, want ok = true", c.weight)
			}
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
		got, ok := Revive(c, now)
		if !ok {
			t.Fatalf("Revive(%+v) refused a finite input, want ok = true", c)
		}
		if got.Weight < e {
			t.Errorf("Revive(%+v).Weight = %v, want >= effective weight %v (a boost never lowers)", c, got.Weight, e)
		}
	}
}

// TestRevive_AlwaysReturnsLastTouchedAtNow proves R2.3's signature-level
// consequence directly: for every finite input Revive returns ok == true
// and LastTouchedAt is now on every path, including the no-raise ceiling
// case exercised separately above. TestRevive_NonFinite_RefusesToProduceABoost
// below covers the one case where ok is false.
func TestRevive_AlwaysReturnsLastTouchedAtNow(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	lastTouchedAt := now.AddDate(0, 0, -10)

	cases := []Current{
		{UnitID: "u1", Weight: 0.3, DecayRate: 0.01, LastTouchedAt: lastTouchedAt},
		{UnitID: "u2", Weight: WeightCeiling, DecayRate: 0, LastTouchedAt: now},
	}
	for _, c := range cases {
		got, ok := Revive(c, now)
		if !ok {
			t.Fatalf("Revive(%+v) refused a finite input, want ok = true", c)
		}
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

	got, ok := Revive(c, now)
	if !ok {
		t.Fatalf("Revive(%+v) refused a finite input, want ok = true", c)
	}

	wantAtFuture := Effective(c.Weight, c.DecayRate, c.LastTouchedAt, future)
	gotAtFuture := Effective(got.Weight, c.DecayRate, got.LastTouchedAt, future)
	if math.Abs(gotAtFuture-wantAtFuture) > 1e-9 {
		t.Errorf("Effective(boosted pair, future) = %v, want %v (Effective(original pair, future)) — the ceiling write must be effective-weight-neutral", gotAtFuture, wantAtFuture)
	}
}

// TestRevive_BelowCeiling_PinsExactBoostFromEffectiveWeight discriminates
// (a): a boost applied to the *effective* (decayed) weight versus the
// *persisted* one — R2.2's entire reason for existing. Every other below-
// ceiling fixture in this file uses DecayRate: 0 (TestRevive_
// MatchesSpecWorkedExample, TestRevive_ConvergesGeometricallyToCeiling,
// TestRevive_StrictlyIncreasingUnderRepetition), so e == c.Weight in all of
// them and a mutant that boosts from c.Weight instead of e — a hybrid
// taking the correct gain term from e but the wrong additive base from
// c.Weight — produces the exact same number and survives undetected. This
// fixture sets DecayRate > 0 and Δt > 0 so e is strictly below both
// WeightCeiling and c.Weight is not used anywhere in the expected value,
// and pins an *exact* expected Weight rather than an inequality:
// TestRevive_NeverLowersAWeight's "got.Weight >= e" is satisfied trivially
// by any mutant anchored on c.Weight, since c.Weight > e once decay has
// run.
func TestRevive_BelowCeiling_PinsExactBoostFromEffectiveWeight(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := lastTouchedAt.AddDate(0, 0, 50)

	const weight = 1.0
	const decayRate = 0.01
	c := Current{UnitID: "u1", Weight: weight, DecayRate: decayRate, LastTouchedAt: lastTouchedAt}

	e := weight * math.Exp(-decayRate*50)
	if e >= WeightCeiling {
		t.Fatalf("test fixture error: effective weight %v is not strictly below WeightCeiling %v", e, WeightCeiling)
	}
	if math.Abs(e-weight) < 0.1 {
		t.Fatalf("test fixture error: effective weight %v is too close to persisted weight %v to discriminate a persisted-weight mutant", e, weight)
	}
	want := e + ReviveGain*(WeightCeiling-e)

	got, ok := Revive(c, now)
	if !ok {
		t.Fatalf("Revive(%+v) refused a finite input, want ok = true", c)
	}
	if math.Abs(got.Weight-want) > 1e-9 {
		t.Errorf("Revive(weight=%v, decayRate=%v, Δt=50d).Weight = %v, want %v exactly (boosted from the effective weight %v, not the persisted weight %v)", weight, decayRate, got.Weight, want, e, weight)
	}
}

// TestRevive_AtCeiling_WithPriorTimestamp_MovesLastTouchedAtToNow
// discriminates (b): whether LastTouchedAt actually resets at or above the
// ceiling — the exact defect the reconciliation's ruling 2 overturned.
// Every other at-ceiling fixture in this file
// (TestRevive_AtOrAboveCeiling_ReturnsEffectiveWeightUnchanged,
// TestRevive_AtCeiling_IsEffectiveWeightNeutral) uses LastTouchedAt: now,
// so Δt = 0 and "the clock moved to now" is indistinguishable from "the
// clock was left alone" — a mutant that returns c.LastTouchedAt unchanged
// at the ceiling would still pass every one of them. This fixture uses a
// LastTouchedAt genuinely earlier than now and asserts the returned
// timestamp moved.
func TestRevive_AtCeiling_WithPriorTimestamp_MovesLastTouchedAtToNow(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := lastTouchedAt.AddDate(0, 0, 40)

	c := Current{UnitID: "u1", Weight: 5.0, DecayRate: 0.01, LastTouchedAt: lastTouchedAt}
	e := Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
	if e < WeightCeiling {
		t.Fatalf("test fixture error: effective weight %v is not at or above WeightCeiling %v", e, WeightCeiling)
	}

	got, ok := Revive(c, now)
	if !ok {
		t.Fatalf("Revive(%+v) refused a finite input, want ok = true", c)
	}
	if got.LastTouchedAt.Equal(lastTouchedAt) {
		t.Errorf("Revive(%+v).LastTouchedAt = %v, want it to have moved away from the original %v — a direct use at the ceiling still resets the clock (R2.3, ruling 2)", c, got.LastTouchedAt, lastTouchedAt)
	}
	if got.LastTouchedAt != now {
		t.Errorf("Revive(%+v).LastTouchedAt = %v, want %v exactly", c, got.LastTouchedAt, now)
	}
}

// TestRevive_NonFinite_RefusesToProduceABoost proves the owner ruling on a
// non-finite input: Revive refuses to persist the corruption rather than
// coercing it to a finite number. Coercing a NaN or ±Inf weight to 0 would
// drive the unit under weight_threshold and archive it — a destructive
// state transition caused by nothing more than a read error. Refusing
// instead leaves the corruption visible and untouched for doctor or a
// later repair path to find, and is a genuine second false case for the
// bool the reconciliation's ruling 2 removed — ruling 2 reasoned about the
// ceiling edge, where a direct use always writes; it never considered a
// non-finite input, so reintroducing the bool for this case is an addition
// to ruling 2, not a reversal (recorded as C4,
// openspec/changes/m2a-weight-focus/tasks.md).
//
// None of these four shapes is reachable through capture — encoding/json
// cannot decode a NaN or Infinity token — but the weight and
// weight_decay_rate columns carry no CHECK constraint, so a corrupted row
// or a future arithmetic slip elsewhere could still produce one
// (Effective's own doc comment enumerates the three that reach it; Weight
// = +Inf is a fourth shape that reaches Revive without going through
// Effective's own NaN cases at all — e is already +Inf, and gain clamps to
// 0, not NaN).
func TestRevive_NonFinite_RefusesToProduceABoost(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	lastTouchedAt := now.AddDate(0, 0, -10)

	cases := []struct {
		name string
		c    Current
		now  time.Time
	}{
		{
			name: "Weight is NaN",
			c:    Current{UnitID: "u1", Weight: math.NaN(), DecayRate: 0.01, LastTouchedAt: lastTouchedAt},
			now:  now,
		},
		{
			name: "DecayRate is NaN",
			c:    Current{UnitID: "u2", Weight: 1.0, DecayRate: math.NaN(), LastTouchedAt: lastTouchedAt},
			now:  now,
		},
		{
			name: "Weight is +Inf",
			c:    Current{UnitID: "u3", Weight: math.Inf(1), DecayRate: 0.01, LastTouchedAt: lastTouchedAt},
			now:  now,
		},
		{
			// DecayRate = +Inf with Δt = 0: IEEE 754 makes Inf*0 a NaN, the
			// third reachable-looking shape Effective's own doc comment
			// names.
			name: "DecayRate is +Inf with Δt = 0",
			c:    Current{UnitID: "u4", Weight: 1.0, DecayRate: math.Inf(1), LastTouchedAt: now},
			now:  now,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Revive(tc.c, tc.now)
			if ok {
				t.Fatalf("Revive(%+v) = (%+v, true), want ok = false — a non-finite result must not be persisted", tc.c, got)
			}
			if got != (Boost{}) {
				t.Errorf("Revive(%+v) = %+v, want the zero-value Boost when refusing", tc.c, got)
			}
		})
	}
}
