package classify

import "testing"

// TestPriors_AreNotZeroValues is the property that matters about these two
// numbers, and the reason they exist at all.
//
// units.weight and units.weight_decay_rate are NOT NULL, so a degraded weight
// has to become *something*. The failure this guards against is that
// something being 0: a unit born with weight 0 is indistinguishable from one
// the user has ignored for months, and a λ of 0 never decays at all — it
// would sit in the pool forever, immune to §6's archiving pass. Both are
// silent, both look like ordinary data, and neither would fail a NOT NULL
// constraint.
//
// This is what a degraded weight must NOT be. What it must BE is pinned
// against migration 0001's own column defaults by
// TestClassifyPriorsMatchMigrationDefaults (L2, test/conformance) — that test
// reads the SQL off disk, which depguard forbids here.
func TestPriors_AreNotZeroValues(t *testing.T) {
	if PriorWeight == 0 {
		t.Error("PriorWeight is 0 — a unit born at zero weight is indistinguishable from one " +
			"decayed to nothing, and §6's archiving pass would collect it on its first night")
	}
	if PriorDecayRate == 0 {
		t.Error("PriorDecayRate is 0 — exp(-0*Δt) is 1 for every Δt, so the unit never decays " +
			"and never becomes archivable (doc 02 §2's formula)")
	}
	if PriorWeight < 0 || PriorDecayRate < 0 {
		t.Errorf("priors must be non-negative; got weight=%v decay=%v", PriorWeight, PriorDecayRate)
	}
}

// TestPriors_AreTwoNumbersNotEighteen guards design D3's actual decision,
// which is about the count rather than the values.
//
// Doc 02 §13 names the knob as "prior per type, base 0.01/day" and enumerates
// no per-type table anywhere; §2 makes personalizing the value the model's
// job, through the prompt. The tempting mistake is to read "prior per type"
// as an invitation to write nine Go constants — inventing calibration doc 02
// never stated, in the one place it says the model decides.
//
// A test cannot assert the absence of constants somebody might add later.
// What it can do is state the decision where the next person to feel that
// temptation will read it, and pin the two that are legitimate.
func TestPriors_AreTwoNumbersNotEighteen(t *testing.T) {
	priors := map[string]float64{
		"weight":     PriorWeight,
		"decay_rate": PriorDecayRate,
	}
	if len(priors) != 2 {
		t.Fatalf("classify declares %d priors, want exactly 2 — per-type values are the "+
			"model's job through the prompt (doc 02 §2), not a table in Go (design D3)",
			len(priors))
	}
	for name, v := range priors {
		if v != v { // NaN check without importing math
			t.Errorf("prior %q is NaN", name)
		}
	}
}
