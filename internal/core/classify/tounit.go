package classify

import (
	"errors"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// The two conditions under which no unit can be built. They are distinct
// errors, not one, because internal/brain writes a different rationale into
// decision_log for each (I12): "the model classified this as something that
// is not memory" and "the model gave us nothing to remember" are different
// events, and a single error would make them one line in the audit trail.
var (
	// ErrNoUnitType — c.Kind is absent, or names one of the six taxonomy
	// values that persist no unit (doc 02 §8: "a timer is NEVER a unit").
	ErrNoUnitType = errors.New("classify: this classification persists no unit")
	// ErrNoContent — c.NormalizedContent degraded. units.content is NOT
	// NULL, and a unit with empty content is one no recall can reach: FTS
	// indexes nothing and the embedding is of the empty string. Doc 02 §5.1
	// calls this loss not survivable downstream.
	ErrNoContent = errors.New("classify: classification has no normalized content")
)

// Priors carries the fallback weight and λ for a classification that degraded
// either — design D3. It is a parameter rather than a package-level read so
// ToUnit stays a function of its arguments, and so a caller calibrating them
// per vault later is a call-site change and not a rewrite.
//
// PriorWeight and PriorDecayRate are the canonical values, pinned to
// migration 0001's column defaults.
type Priors struct {
	Weight    float64
	DecayRate float64
}

// ToUnit builds the persistable unit from a classification — design D4, and
// where I18 lands. It is pure.
//
// The three timestamps meet here and nowhere else, which is what makes I18
// testable at one point rather than reviewable at many: now becomes all three
// of CreatedAt, UpdatedAt and LastTouchedAt — ingestion time, the
// orchestrator's fact — while EventAt and DueAt are carried over from the
// model's own separately-named fields. Two thirds of I18 are already
// unrepresentable upstream, since Classification has no CreatedAt field for a
// model-supplied value to land in.
//
// source is the caller's fact (C10.1): units.source is NOT NULL and the
// column's DEFAULT never fires, because the repository passes the field
// explicitly. core does not name channels — hardcoding one here would be
// wrong from the UI, and it would not fail, it would lie.
//
// It returns an error on exactly two conditions, ErrNoUnitType and
// ErrNoContent, and a zero unit alongside either, so a caller that ignored
// the error still has nothing to persist.
func ToUnit(c Classification, id, source string, now time.Time, p Priors) (unit.Unit, error) {
	if c.Kind == nil {
		return unit.Unit{}, ErrNoUnitType
	}
	unitType, persists := c.Kind.UnitType()
	if !persists {
		return unit.Unit{}, ErrNoUnitType
	}
	if c.NormalizedContent == nil {
		return unit.Unit{}, ErrNoContent
	}

	return unit.Unit{
		ID:      id,
		Type:    unitType,
		Content: *c.NormalizedContent,
		// A fresh capture is live by definition — doc 02 §1. Nothing here
		// can produce incomplete: that status belongs to the ambiguous
		// person-reference path, which is brain's decision, not the
		// decoder's (spec R4.6, Q3a).
		Status:          unit.StatusPool,
		Weight:          orPrior(c.Weight, p.Weight),
		WeightDecayRate: orPrior(c.DecayRate, p.DecayRate),
		LastTouchedAt:   now,
		StructuredData:  c.StructuredData,
		Source:          source,
		EventAt:         c.EventAt,
		DueAt:           c.DueAt,
		// Phase B writes no perception confidence — proposal §8 Q2. nil
		// rather than 0.0, which would read as "measured, and zero".
		Confidence: nil,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// orPrior returns the model's value when it supplied one, and the prior when
// it did not. A model-supplied zero wins over the prior: the pointer is what
// distinguishes "said zero" from "said nothing", and collapsing the two is
// exactly what the pointer exists to prevent.
func orPrior(supplied *float64, prior float64) float64 {
	if supplied == nil {
		return prior
	}
	return *supplied
}
