package consolidation

import (
	"time"

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
//
// Stub (RED, task 4.18): always false.
func mergeQualifies(similarity float64) bool {
	return false
}

// MergeProposals is doc 02 §6.5's SECOND dedup defense (the first is the
// prompt-side one, brain's). For each proposed belief it finds the
// nearest existing belief and merges when cosine >= BeliefMergeCosine
// (spec R4.4).
//
// Stub (RED, task 4.18): returns (nil, nil) — len(decisions) !=
// len(proposed) fails first on a non-empty proposed fixture.
func MergeProposals(model string, existing, proposed []BeliefVector) ([]MergeDecision, error) {
	return nil, nil
}
