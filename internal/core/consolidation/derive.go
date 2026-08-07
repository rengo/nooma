package consolidation

import (
	"errors"
	"math"
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
// A non-finite vector component (NaN, +-Inf) IS validated at this entry
// point, on both sides, and the two sides are handled differently
// (Judgment Day round 1, C-series NaN-comparator pattern: clamp, Displaces,
// Rank, focus.clamp, and now this).
//
// On the EXISTING side, a non-finite component fails the whole call:
// recall.Normalize refuses it via recall.ErrNonFiniteVector while building
// the comparison index, the same treatment ErrZeroVector already gets one
// line above. This is deliberate, not incidental: a NaN component makes
// norm itself NaN, ErrZeroVector's "norm == 0" guard does not catch it
// (NaN == 0 is false), so an uncaught NaN existing vector would carry a
// NaN score into recall.Search's sort.Slice — whose comparator
// (scored[i].Score > scored[j].Score) is not a strict weak ordering once
// any score is NaN. A NaN-scored entry can then sort ahead of the
// genuinely nearest finite match, so scored[0] stops being the nearest and
// a real qualifying merge is missed — silently, and dependent on the
// corrupted entry's position in existing, not on anything about the
// proposal being compared. Failing the call surfaces that corruption
// instead of returning a wrong answer; ruling Q2 re-embeds every belief
// fresh each night, so this is one embedder hiccup on one stored belief,
// not a permanent failure.
//
// On the PROPOSED side, a non-finite component still resolves to "create a
// new belief" without aborting the call, unchanged from before this fix:
// recall.Normalize returns the same recall.ErrNonFiniteVector, but
// MergeProposals recognizes it here and treats it like any other
// unqualifying candidate rather than propagating it — the same
// safe-default posture Strengthen and Archive use for their own corrupted
// inputs. A corrupted incoming proposal costs a possible duplicate belief,
// never a wrong merge, and never blocks every other proposal in the same
// nightly pass. Pinned by TestMergeProposals_NonFiniteSimilarityNeverMerges
// (proposed side, unchanged) and
// TestMergeProposals_ExistingNonFiniteVectorSurfacesError (existing side,
// this fix — verified in both position orderings, since the bug this
// closes was position-dependent).
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
			if errors.Is(err, recall.ErrNonFiniteVector) {
				// A corrupted proposed vector never merges (decisions[i]
				// keeps its zero-value MergeInto == "" == "create"), but
				// it does not abort the rest of the nightly pass either —
				// see the doc comment above.
				continue
			}
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

// BeliefReinforceGain is doc 02 §6.5's confidence-raising rate for a
// merged belief (spec R4.5, design.md §4.1/§6.8) — CHOSEN, inheriting
// §4.1's shared reinforcement-law argument (the same asymptotic form
// StrengthenGain uses), with no compatibility check attached: unlike
// StrengthenGain, nothing in doc 02 ties a belief's confidence horizon to
// a fixed night count. Default 0.10 (doc 02 §13).
const BeliefReinforceGain = 0.10

// Reinforce raises a merged belief's confidence toward 1 by §4.1's shared
// reinforcement law (spec R4.5): c' = c + BeliefReinforceGain*(1-c),
// asymptotic and never reaching 1 under repetition — the same law
// Strengthen applies to relation strength, applied here to belief
// confidence. It refuses a NaN, +-Inf, or finite-but-outside-[0,1]
// confidence outright — (confidence, false), the input echoed back
// unchanged rather than coerced (C15/C22/C24's rule, Strengthen's own
// convention applied here) — and returns (confidence, false) for a belief
// already at exactly 1 (doc 02 §11 — a decision with no effect writes
// nothing).
func Reinforce(confidence float64) (float64, bool) {
	if math.IsNaN(confidence) || confidence < 0 || confidence > 1 {
		return confidence, false
	}
	if confidence >= 1 {
		return confidence, false
	}
	return confidence + BeliefReinforceGain*(1-confidence), true
}
