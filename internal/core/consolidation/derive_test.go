package consolidation

import (
	"errors"
	"math"
	"testing"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/selfmodel"
)

// TestDeriveTopicKey_RendersDerivedFormatForEveryFacet proves R4.6:
// DeriveTopicKey renders doc 02 §10's derived key format,
// "derived/{facet}/{key}", for every member of selfmodel.AllFacets() —
// driven by the vocabulary itself, so a sixth facet added later is
// exercised automatically rather than requiring a new case here.
func TestDeriveTopicKey_RendersDerivedFormatForEveryFacet(t *testing.T) {
	facets := selfmodel.AllFacets()
	if len(facets) == 0 {
		t.Fatal("selfmodel.AllFacets() returned zero members — nothing to drive this test")
	}

	for _, f := range facets {
		got := DeriveTopicKey(f, "example-key")
		want := "derived/" + string(f) + "/example-key"
		if got != want {
			t.Errorf("DeriveTopicKey(%q, %q) = %q, want %q", f, "example-key", got, want)
		}
	}
}

// TestBeliefMergeCosine_MatchesTheDocumentedDefault pins BeliefMergeCosine
// against an INDEPENDENT literal (doc 02 §13's "Semantic belief merge:
// cosine >= 0.85" row) rather than anything derived from the constant
// itself — this repository's own convention (C7/C28) for every calibrated
// constant, restated here because it has now failed to transfer four
// consecutive times (focus's seven constants, IncompleteExpiryHours,
// StrengthenGain, connect's two budget knobs) and this PR introduces two
// more (this one and BeliefReinforceGain, below).
func TestBeliefMergeCosine_MatchesTheDocumentedDefault(t *testing.T) {
	const want = 0.85
	if BeliefMergeCosine != want {
		t.Errorf("BeliefMergeCosine = %v, want %v — doc 02 §13/§6.5's documented threshold; recalibrating means editing the §13 row and this literal together", BeliefMergeCosine, want)
	}
}

// TestMergeQualifies_BoundaryIsInclusive proves the R4.4 boundary directly
// against the literal constant, rather than by constructing vectors whose
// normalized cosine hits 0.85 exactly: 0.85 has no exact binary floating
// representation, so no vector geometry can be hand-derived to land on it
// exactly — the predicate itself can, and is tested in isolation from
// recall.Normalize/Search's own floating-point rounding.
func TestMergeQualifies_BoundaryIsInclusive(t *testing.T) {
	if !mergeQualifies(BeliefMergeCosine) {
		t.Errorf("mergeQualifies(%v) = false, want true — the boundary is inclusive (spec R4.4)", BeliefMergeCosine)
	}

	hairBelow := math.Nextafter(BeliefMergeCosine, 0)
	if mergeQualifies(hairBelow) {
		t.Errorf("mergeQualifies(%v) = true, want false — one ULP below the boundary must not qualify", hairBelow)
	}
}

// TestMergeDecision_ZeroValueMeansCreate proves the MergeInto == "" ==
// "create a new belief" convention holds even for MergeDecision's own zero
// value, not only for values MergeProposals explicitly constructs.
func TestMergeDecision_ZeroValueMeansCreate(t *testing.T) {
	var d MergeDecision
	if d.MergeInto != "" {
		t.Errorf("zero-value MergeDecision.MergeInto = %q, want \"\" (create)", d.MergeInto)
	}
}

// TestMergeProposals_NearestExistingBeliefWins proves R4.4: among several
// existing beliefs, the proposed belief merges into the nearest one by
// cosine, not merely the first one above the threshold.
func TestMergeProposals_NearestExistingBeliefWins(t *testing.T) {
	existing := []BeliefVector{
		{BeliefID: "far", Vector: []float32{0, 1}},
		{BeliefID: "near", Vector: []float32{1, 0}},
	}
	proposed := []BeliefVector{
		{BeliefID: "p1", Vector: []float32{1, 0}},
	}

	got, err := MergeProposals("model-a", existing, proposed)
	if err != nil {
		t.Fatalf("MergeProposals returned error %v, want nil", err)
	}
	if len(got) != len(proposed) {
		t.Fatalf("MergeProposals returned %d decisions, want %d", len(got), len(proposed))
	}
	if got[0].MergeInto != "near" {
		t.Errorf("MergeProposals merged proposed[0] into %q, want %q (the closer of the two existing beliefs)", got[0].MergeInto, "near")
	}
}

// TestMergeProposals_EmptyExistingAlwaysCreates proves R4.4: with no
// existing beliefs at all, every proposed belief creates (MergeInto == "").
func TestMergeProposals_EmptyExistingAlwaysCreates(t *testing.T) {
	proposed := []BeliefVector{
		{BeliefID: "p1", Vector: []float32{1, 0}},
		{BeliefID: "p2", Vector: []float32{0, 1}},
	}

	got, err := MergeProposals("model-a", nil, proposed)
	if err != nil {
		t.Fatalf("MergeProposals returned error %v, want nil", err)
	}
	if len(got) != len(proposed) {
		t.Fatalf("MergeProposals returned %d decisions, want %d", len(got), len(proposed))
	}
	for i, d := range got {
		if d.MergeInto != "" {
			t.Errorf("decisions[%d].MergeInto = %q, want \"\" — an empty existing slice must always create", i, d.MergeInto)
		}
	}
}

// TestMergeProposals_ZeroVectorSurfacesErrZeroVector proves R4.4: a
// zero-magnitude vector has no direction to normalize, and MergeProposals
// surfaces recall.ErrZeroVector unchanged rather than dividing by zero.
func TestMergeProposals_ZeroVectorSurfacesErrZeroVector(t *testing.T) {
	existing := []BeliefVector{{BeliefID: "e1", Vector: []float32{1, 0}}}
	proposed := []BeliefVector{{BeliefID: "p1", Vector: []float32{0, 0}}}

	_, err := MergeProposals("model-a", existing, proposed)
	if !errors.Is(err, recall.ErrZeroVector) {
		t.Fatalf("MergeProposals error = %v, want errors.Is(_, recall.ErrZeroVector)", err)
	}
}

// TestMergeProposals_UnNormalizedInputStillScoresAsCosine proves R4.4:
// normalization happens inside MergeProposals, never a caller obligation —
// existing and proposed here are parallel but at very different
// magnitudes, and must still resolve to (near) cosine 1.0 and qualify.
func TestMergeProposals_UnNormalizedInputStillScoresAsCosine(t *testing.T) {
	existing := []BeliefVector{{BeliefID: "e1", Vector: []float32{2, 0}}}
	proposed := []BeliefVector{{BeliefID: "p1", Vector: []float32{500, 0}}}

	got, err := MergeProposals("model-a", existing, proposed)
	if err != nil {
		t.Fatalf("MergeProposals returned error %v, want nil", err)
	}
	if len(got) != len(proposed) {
		t.Fatalf("MergeProposals returned %d decisions, want %d", len(got), len(proposed))
	}
	if got[0].MergeInto != "e1" {
		t.Errorf("MergeProposals did not merge un-normalized parallel vectors: MergeInto = %q, want %q", got[0].MergeInto, "e1")
	}
}

// TestMergeProposals_NonFiniteSimilarityNeverMerges pins a deliberate
// decision (this repo's own C15/C22/C24 convention, applied here): a NaN
// vector component is not caught by recall.Normalize (only a zero
// magnitude is, via ErrZeroVector) — it propagates into a NaN similarity.
// mergeQualifies's >= comparison is not total over NaN (NaN >=
// BeliefMergeCosine is always false in IEEE 754), so the decision resolves
// to "create a new belief" rather than a silent, wrong merge. That is the
// safe default this package uses throughout: a signal too corrupted to
// interpret never causes a write it cannot justify — it costs a possible
// duplicate belief, never a wrong merge.
func TestMergeProposals_NonFiniteSimilarityNeverMerges(t *testing.T) {
	existing := []BeliefVector{{BeliefID: "e1", Vector: []float32{1, 0}}}
	proposed := []BeliefVector{{BeliefID: "p1", Vector: []float32{float32(math.NaN()), float32(math.NaN())}}}

	got, err := MergeProposals("model-a", existing, proposed)
	if err != nil {
		t.Fatalf("MergeProposals returned error %v, want nil — a non-finite proposed vector never merges, it does not abort the call", err)
	}
	if len(got) != len(proposed) {
		t.Fatalf("MergeProposals returned %d decisions, want %d", len(got), len(proposed))
	}
	if got[0].MergeInto != "" {
		t.Errorf("MergeProposals merged a non-finite proposed vector into %q, want \"\" (never merge on a corrupted signal)", got[0].MergeInto)
	}
}

// TestBeliefReinforceGain_MatchesTheDocumentedDefault pins
// BeliefReinforceGain against an INDEPENDENT literal (doc 02 §13), the
// same convention TestBeliefMergeCosine_MatchesTheDocumentedDefault
// applies above — this PR introduces two calibrated constants, and both
// get their own pin in the commit that declares them.
func TestBeliefReinforceGain_MatchesTheDocumentedDefault(t *testing.T) {
	const want = 0.10
	if BeliefReinforceGain != want {
		t.Errorf("BeliefReinforceGain = %v, want %v — doc 02 §13's documented rate; recalibrating means editing the §13 row and this literal together", BeliefReinforceGain, want)
	}
}

// TestReinforce_AsymptoticAndNeverReaches1 proves R4.5: repeated
// reinforcement approaches but never reaches or exceeds 1 — Strengthen's
// own TestStrengthen_NeverReachesOne shape (500 iterations, converged-close
// check at the end rather than a per-iteration strict-increase assertion,
// which floating-point precision defeats once c is within one ULP of 1).
func TestReinforce_AsymptoticAndNeverReaches1(t *testing.T) {
	c := 0.5
	for i := 0; i < 500; i++ {
		next, ok := Reinforce(c)
		if !ok {
			t.Fatalf("Reinforce(%v) returned ok=false at iteration %d, want true", c, i)
		}
		if next >= 1 {
			t.Fatalf("Reinforce(%v) = %v at iteration %d, must never reach or exceed 1", c, next, i)
		}
		c = next
	}
	if c < 0.99 {
		t.Errorf("after 500 reinforcements, confidence = %v, want it to have converged close to 1", c)
	}
}

// TestReinforce_NoWriteAtExactly1 proves R4.5: a belief already at
// confidence 1 gets no row (doc 02 §11 — a decision with no effect writes
// nothing).
func TestReinforce_NoWriteAtExactly1(t *testing.T) {
	_, ok := Reinforce(1)
	if ok {
		t.Error("Reinforce(1) returned ok=true, want false — a belief already at 1 gets no row")
	}
}

// TestReinforce_RefusesNonFiniteAndOutOfDomain proves R4.5: Reinforce
// refuses a NaN, +-Inf, negative, or greater-than-1 confidence outright,
// rather than computing a change for a corrupted input.
func TestReinforce_RefusesNonFiniteAndOutOfDomain(t *testing.T) {
	cases := []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.5, 1.5}
	for _, c := range cases {
		_, ok := Reinforce(c)
		if ok {
			t.Errorf("Reinforce(%v) returned ok=true, want false — refused as non-finite or out of [0,1]", c)
		}
	}
}
