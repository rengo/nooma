package weight

import (
	"math"
	"testing"
	"time"
)

// TestEffective_ZeroDeltaReturnsWeightUnchanged proves R1.1's Δt=0 anchor:
// a unit read at the exact instant it was last touched has not decayed at
// all.
func TestEffective_ZeroDeltaReturnsWeightUnchanged(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	got := Effective(1.0, 0.01, now, now)
	if got != 1.0 {
		t.Errorf("Effective(1.0, 0.01, t, t) = %v, want 1.0", got)
	}
}

// TestEffective_StrictlyDecreasesAsNowMovesLater proves R1.1: for any
// decayRate > 0, reading later always returns a smaller effective weight
// than reading earlier — the curve never plateaus or reverses.
func TestEffective_StrictlyDecreasesAsNowMovesLater(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	decayRate := 0.01

	prev := Effective(1.0, decayRate, lastTouchedAt, lastTouchedAt)
	for days := 1; days <= 200; days++ {
		now := lastTouchedAt.Add(time.Duration(days) * 24 * time.Hour)
		got := Effective(1.0, decayRate, lastTouchedAt, now)
		if got >= prev {
			t.Fatalf("Effective at day %d = %v, want strictly less than day %d's %v", days, got, days-1, prev)
		}
		prev = got
	}
}

// TestEffective_ZeroDecayRateReturnsWeightUnchanged proves R1.1's other
// anchor, matching doc 02 §2's "λ of 0 never decays": decayRate == 0
// returns weight unchanged regardless of Δt.
func TestEffective_ZeroDecayRateReturnsWeightUnchanged(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		now  time.Time
	}{
		{"same instant", lastTouchedAt},
		{"100 days later", lastTouchedAt.AddDate(0, 0, 100)},
		{"10000 days later", lastTouchedAt.AddDate(0, 0, 10000)},
		{"before lastTouchedAt", lastTouchedAt.Add(-time.Hour)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Effective(0.73, 0, lastTouchedAt, c.now)
			if got != 0.73 {
				t.Errorf("Effective(0.73, 0, lt, now) = %v, want 0.73 unchanged", got)
			}
		})
	}
}

// TestEffective_NegativeDeltaClampsAtZero proves R1.2's clamp: when now is
// before lastTouchedAt — clock skew across a restart, a backdated import, a
// fake clock wound backwards in a test — Effective behaves as though Δt
// were 0 and returns weight undecayed, exactly.
func TestEffective_NegativeDeltaClampsAtZero(t *testing.T) {
	lastTouchedAt := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	now := lastTouchedAt.Add(-time.Hour)

	got := Effective(1.0, 0.01, lastTouchedAt, now)
	if got != 1.0 {
		t.Errorf("Effective(1.0, 0.01, lt, now one hour before lt) = %v, want 1.0 exactly (negative-Δt clamp)", got)
	}
}

// splitmix64 is a fixed-seed, deterministic pseudo-random generator used
// only to synthesize varied test fixtures below. It is not math/rand:
// forbidigo forbids the `rand.*` call pattern inside internal/core
// (docs/06-harness.md §2, nooma-core hard rule 2), and importing math/rand
// under an alias to dodge that pattern by spelling would defeat the rule's
// intent — core decisions, and the tests that pin them, must stay
// deterministic. A property test needs varied inputs, not entropy, and a
// fixed seed gives exactly that: the same cases every run, reproducible
// under -shuffle=on -race.
func splitmix64(state *uint64) uint64 {
	*state += 0x9E3779B97F4A7C15
	z := *state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// TestEffective_NeverExceedsWeight_Property proves R1.2's postcondition as
// a property, not a single fixture: Effective(w, λ, lt, now) ≤ w for every
// input, including λ = 0 and every ordering of lt and now.
func TestEffective_NeverExceedsWeight_Property(t *testing.T) {
	state := uint64(0x2545F4914F6CDD1D)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	const iterations = 2000
	for i := 0; i < iterations; i++ {
		w := float64(splitmix64(&state)%2_000_001) / 1_000_000.0    // [0, 2.0]
		lambda := float64(splitmix64(&state)%100_001) / 1_000_000.0 // [0, 0.1]
		// offsetHours ranges over roughly [-1000h, 1000h], so both
		// orderings of lt/now are exercised, including lt == now.
		offsetHours := int64(splitmix64(&state)%2001) - 1000
		now := base.Add(time.Duration(offsetHours) * time.Hour)

		got := Effective(w, lambda, base, now)
		if got > w {
			t.Fatalf("iteration %d: Effective(%v, %v, lt, lt%+dh) = %v, want <= %v", i, w, lambda, offsetHours, got, w)
		}
		if math.IsNaN(got) || math.IsInf(got, 0) {
			t.Fatalf("iteration %d: Effective(%v, %v, lt, lt%+dh) = %v, want a finite number", i, w, lambda, offsetHours, got)
		}
	}
}
