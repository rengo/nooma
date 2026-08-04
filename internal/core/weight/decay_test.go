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
// than reading earlier — the curve never plateaus or reverses. Steps are
// half-day (12h) increments rather than whole days, so most offsets are not
// 24h multiples: a floored Δt (math.Floor(Hours()/24) instead of the exact
// fraction) collapses two consecutive half-day steps onto the same value
// and fails this test, which a whole-day-only sequence would not catch.
func TestEffective_StrictlyDecreasesAsNowMovesLater(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	decayRate := 0.01

	prev := Effective(1.0, decayRate, lastTouchedAt, lastTouchedAt)
	for halfDays := 1; halfDays <= 400; halfDays++ {
		now := lastTouchedAt.Add(time.Duration(halfDays) * 12 * time.Hour)
		got := Effective(1.0, decayRate, lastTouchedAt, now)
		if got >= prev {
			t.Fatalf("Effective at half-day step %d = %v, want strictly less than step %d's %v", halfDays, got, halfDays-1, prev)
		}
		prev = got
	}
}

// TestEffective_MatchesEbbinghausWorkedExample pins the exponential shape
// against spec.md R1.1's own worked example: weight=1.0, decayRate=0.01,
// Δt=100 days → ≈0.3679 (exp(-1)). A linear decay implementation
// (weight * (1 - decayRate*deltaDays)) would return 0.0 here instead, so
// this test discriminates the formula's shape, not just its monotonicity.
func TestEffective_MatchesEbbinghausWorkedExample(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := lastTouchedAt.AddDate(0, 0, 100)

	want := math.Exp(-1) // exp(-0.01 * 100)
	got := Effective(1.0, 0.01, lastTouchedAt, now)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("Effective(1.0, 0.01, lt, lt+100d) = %v, want %v (exp(-1))", got, want)
	}
}

// TestEffective_SubDayDeltaIsFractionalNotTruncated pins spec.md's other
// worked example: a unit touched 12 hours ago has Δt = 0.5, not 0. An
// implementation that truncates Δt to whole days (e.g.
// math.Floor(now.Sub(lastTouchedAt).Hours()/24)) — the calendar-day count
// design D1 explicitly rejects — would return the undecayed weight here
// instead, so this test discriminates fractional-day handling directly.
func TestEffective_SubDayDeltaIsFractionalNotTruncated(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := lastTouchedAt.Add(12 * time.Hour)

	want := math.Exp(-0.005) // exp(-0.01 * 0.5)
	got := Effective(1.0, 0.01, lastTouchedAt, now)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("Effective(1.0, 0.01, lt, lt+12h) = %v, want %v (exp(-0.005), Δt = 0.5)", got, want)
	}
	if got == 1.0 {
		t.Errorf("Effective(1.0, 0.01, lt, lt+12h) = 1.0, want a decayed value: Δt truncated to 0 instead of 0.5")
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

// TestEffective_NegativeDecayRateClampsAtZero proves the input-sanitization
// half of R1.2's postcondition: weight and decay_rate arrive from an LLM's
// JSON via classify's decode, which validates only that the value is a
// number (no sign, no range), and the schema declares neither column
// CHECK-constrained. A negative decayRate is treated as 0 — no decay — the
// same posture as the Δt clamp: core cannot vouch for a float it did not
// compute. Without this clamp, Effective(1.0, -0.01, lt, lt+100d) returns
// exp(1) ≈ 2.718, an effective weight larger than any weight the schema
// ever stored.
func TestEffective_NegativeDecayRateClampsAtZero(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := lastTouchedAt.AddDate(0, 0, 100)

	got := Effective(1.0, -0.01, lastTouchedAt, now)
	if got != 1.0 {
		t.Errorf("Effective(1.0, -0.01, lt, lt+100d) = %v, want 1.0 exactly (negative-decayRate clamp)", got)
	}
}

// TestEffective_NegativeWeightClampsAtZero proves R1.2's other
// input-sanitization half: a negative weight is meaningless in this model —
// weight is how much something matters, not a signed magnitude — so it is
// treated as 0. Without this clamp, Effective(-1.0, 0.01, lt, lt+100d)
// returns -exp(-1) ≈ -0.368, which is greater than the persisted weight
// -1.0: the postcondition "effective_weight <= weight" would not hold.
func TestEffective_NegativeWeightClampsAtZero(t *testing.T) {
	lastTouchedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := lastTouchedAt.AddDate(0, 0, 100)

	got := Effective(-1.0, 0.01, lastTouchedAt, now)
	if got != 0 {
		t.Errorf("Effective(-1.0, 0.01, lt, lt+100d) = %v, want 0 exactly (negative-weight clamp)", got)
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
// a property, not a single fixture: Effective(w, λ, lt, now) ≤ max(w, 0)
// for every input, including negative λ, negative w, λ = 0, and every
// ordering of lt and now. The sampling range includes negative w and
// negative λ — a range of [0, 2.0] and [0, 0.1] would silently avoid the
// two reachable counterexamples (a negative decayRate makes the curve grow;
// a negative weight decays toward, not away from, zero), giving false
// confidence that the postcondition holds when it was never exercised
// against the values that break it. The assertion is against the
// *sanitized* weight (max(w, 0)), matching what Effective actually
// clamps a negative weight to — not against the raw sampled w, which a
// negative-weight input can legitimately exceed once sanitized to 0.
func TestEffective_NeverExceedsWeight_Property(t *testing.T) {
	state := uint64(0x2545F4914F6CDD1D)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	const iterations = 2000
	for i := 0; i < iterations; i++ {
		w := float64(splitmix64(&state)%4_000_001)/1_000_000.0 - 2.0     // [-2.0, 2.0]
		lambda := float64(splitmix64(&state)%200_001)/1_000_000.0 - 0.1  // [-0.1, 0.1]
		// offsetHours ranges over roughly [-1000h, 1000h], so both
		// orderings of lt/now are exercised, including lt == now.
		offsetHours := int64(splitmix64(&state)%2001) - 1000
		now := base.Add(time.Duration(offsetHours) * time.Hour)

		sanitizedWeight := math.Max(w, 0)
		got := Effective(w, lambda, base, now)
		if got > sanitizedWeight {
			t.Fatalf("iteration %d: Effective(%v, %v, lt, lt%+dh) = %v, want <= %v (sanitized weight)", i, w, lambda, offsetHours, got, sanitizedWeight)
		}
		if math.IsNaN(got) || math.IsInf(got, 0) {
			t.Fatalf("iteration %d: Effective(%v, %v, lt, lt%+dh) = %v, want a finite number", i, w, lambda, offsetHours, got)
		}
	}
}
