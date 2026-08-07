package consolidation

import (
	"time"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/selfmodel"
)

// DeriveTopicKey renders doc 02 §10's derived belief key format (spec
// R4.6): "derived/{facet}/{key}".
func DeriveTopicKey(f selfmodel.Facet, key string) string {
	return "derived/" + string(f) + "/" + key
}

// BeliefMergeCosine is doc 02 §6.5's second dedup defense's threshold
// (spec R4.4, design.md §6.8) — an existing §13 row, first Go home here.
// Default 0.85 (doc 02 §13).
const BeliefMergeCosine = 0.85

// Belief is one self-belief at the instant a phase reads it. Declared here
// rather than in patterns.go because design.md §5.1's file-layout table
// places it in derive.go; feat/core-consolidation-pattern-eval's
// EvaluateStagnation is its only consumer in this package, and does not
// exist yet — this is a forward declaration, the same shape design gave
// Source (connect.go) ahead of its own second reader.
type Belief struct {
	ID               string
	Facet            selfmodel.Facet
	TopicKey         string
	Confidence       float64
	LastReinforcedAt time.Time
}

// BeliefVector is one belief's embedding at the instant derive runs it in
// memory (ruling Q2, option A — design.md §8's handoff row): computed
// fresh at the start of the phase and discarded after, never persisted
// (doc 02 §6.5).
type BeliefVector struct {
	BeliefID string
	Vector   []float32
}

// MergeDecision is MergeProposals's per-proposed-belief outcome.
// MergeInto == "" (its own zero value) means create a new belief;
// otherwise it names the existing belief this proposal merges into, at
// Similarity.
type MergeDecision struct {
	ProposedIndex int
	MergeInto     string
	Similarity    float64
}

// mergeQualifies reports whether similarity is at or above BeliefMergeCosine
// — the inclusive boundary spec R4.4 states. Isolated from MergeProposals
// so the boundary itself is testable directly against the literal
// constant: 0.85 has no exact binary floating representation, so no
// vector geometry can be hand-derived to land on it exactly by
// construction, while this predicate can be asserted against the constant
// directly.
func mergeQualifies(similarity float64) bool {
	return similarity >= BeliefMergeCosine
}

// MergeProposals is doc 02 §6.5's SECOND dedup defense (the first is the
// prompt-side one, brain's). For each proposed belief it finds the
// nearest existing belief and merges when cosine >= BeliefMergeCosine
// (spec R4.4).
//
// It builds the comparison through recall.NewVectorIndex + recall.Search
// rather than a second similarity implementation: Search is a dot
// product, which IS cosine once both sides are unit-normalized, and it
// carries I21's model filter with it (ErrModelMismatch) — so belief
// vectors inherit "embeddings from two models never compare" at no cost
// (design.md §6.8). MergeProposals normalizes every vector itself via
// recall.Normalize, so normalization is structural here rather than a
// caller obligation; a zero-magnitude vector is refused
// (recall.ErrZeroVector), never scored.
//
// A non-finite vector component (NaN, +-Inf) is not itself validated at
// this entry point: recall.Normalize does not catch it either (only a
// zero magnitude is, via ErrZeroVector), so a corrupted embedding
// produces a NaN similarity. Cosine's mathematical domain is [-1, 1], not
// [0, 1] (Cauchy-Schwarz over unit vectors) — NaN is outside any domain,
// and mergeQualifies's >= comparison is not total over it: NaN >=
// BeliefMergeCosine is always false under IEEE 754. That resolves the
// decision to "create a new belief" rather than a merge this package
// cannot justify — the same safe-default posture Strengthen and Archive
// use for their own corrupted inputs, applied here through comparison
// semantics rather than an explicit refusal branch, because this
// function's signature carries no corrupted output to report into (design
// stub above). Pinned by TestMergeProposals_NonFiniteSimilarityNeverMerges.
//
// recall.ErrModelMismatch is part of recall.Search's own contract and
// would propagate unchanged if it ever fired, but it cannot be produced
// through MergeProposals's own call surface as shipped: the index built
// from existing and every query built from proposed both use the SAME
// model parameter, so idx.Model and q.Model are never different values
// within one call — BeliefVector carries no per-entry model tag. The
// protection is inherited for a future caller that might misuse
// recall.Search directly (already exercised in
// internal/core/recall/vector_test.go), not independently reachable at
// m2b's single-model call boundary. Not fabricated as a passing test here
// — see derive_test.go's own comment naming this explicitly, per this
// project's convention against claiming a verification that was not
// performed.
//
// Ruling Q2 (option A): brain embeds every active belief in memory at the
// start of the phase and discards after. No schema change, and the
// nightly provider cost is written into doc 02 §6.5 as part of this
// change.
func MergeProposals(model string, existing, proposed []BeliefVector) ([]MergeDecision, error) {
	existingIDs := make([]string, len(existing))
	existingVectors := make([][]float32, len(existing))
	for i, e := range existing {
		v, err := recall.Normalize(e.Vector)
		if err != nil {
			return nil, err
		}
		existingIDs[i] = e.BeliefID
		existingVectors[i] = v
	}

	idx, err := recall.NewVectorIndex(model, existingIDs, existingVectors)
	if err != nil {
		return nil, err
	}

	decisions := make([]MergeDecision, len(proposed))
	for i, p := range proposed {
		decisions[i] = MergeDecision{ProposedIndex: i}

		v, err := recall.Normalize(p.Vector)
		if err != nil {
			return nil, err
		}

		if len(existing) == 0 {
			continue
		}

		scored, err := recall.Search(idx, recall.VectorQuery{Model: model, Vector: v})
		if err != nil {
			return nil, err
		}

		best := scored[0]
		if mergeQualifies(float64(best.Score)) {
			decisions[i] = MergeDecision{
				ProposedIndex: i,
				MergeInto:     best.ID,
				Similarity:    float64(best.Score),
			}
		}
	}

	return decisions, nil
}
