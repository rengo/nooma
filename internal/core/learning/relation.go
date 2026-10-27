package learning

import (
	"math"
	"slices"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/relation"
)

// The relation threshold rule's numbers (doc 02 §9, §13; ADR-0053).
const (
	// RelationSampleSize is how many of a type's newest verdicts are read.
	RelationSampleSize = 8
	// RelationVerdictsToMove is how many of the sample must agree to move
	// the bar: doc 02 §9's "6 of the last 8". A majority, so one sample can
	// move it one way only.
	RelationVerdictsToMove = 6
	// RelationThresholdStep is how far one change moves the persist bar.
	RelationThresholdStep = 0.05
	// MinLearnedPersist and MaxLearnedPersist bound the persist bar learning
	// may set: below the floor the graph fills with what the judge barely
	// believed, and above the ceiling a type is all but switched off, which
	// is a decision for the user, not for a count.
	MinLearnedPersist = 0.20
	MaxLearnedPersist = 0.90
	// RelationCooldownDays is how long a changed type waits before it is
	// judged again, so the new bar has nights to show its effect.
	RelationCooldownDays = 7
)

// Verdict is one user verdict on a relation of the type being judged: a
// relation_reject or relation_confirm signal (doc 02 §9).
type Verdict struct {
	SignalID string
	Rejected bool
	// CreatedBy is who made the relation the verdict is about. Only Nooma's
	// own (system, consolidation) are evidence about how Nooma judges.
	CreatedBy relation.CreatedBy
	At        time.Time
}

// TypeState is what the vault holds about one relation type's thresholds.
type TypeState struct {
	// Current is the type's resolved thresholds (relation.Resolve).
	Current relation.Thresholds
	// Pinned is the user's word: learning never writes a pinned type.
	Pinned bool
	// AdjustedAt is the type's last change, nil when it never had one.
	AdjustedAt *time.Time
}

// Direction is which way a change moved the persist bar.
type Direction string

const (
	Raised  Direction = "raised"
	Lowered Direction = "lowered"
)

// Change is one step of a type's thresholds and the evidence it was decided
// on: the counts and the sample's signal ids, newest first.
type Change struct {
	Direction Direction
	Previous  relation.Thresholds
	Next      relation.Thresholds
	Rejected  int
	Confirmed int
	SignalIDs []string
}

// StepRelation is the learn pass's rule for one relation type (doc 02 §9,
// ADR-0053). It reads the newest RelationSampleSize verdicts on Nooma's own
// relations of the type, made strictly after its last change and no later
// than now. RelationVerdictsToMove rejections raise the persist bar by
// RelationThresholdStep, as many confirmations lower it, and anything between
// holds. The surface bar rises with persist when persist would pass it, and
// is never lowered here. It returns no change for a pinned type, inside
// RelationCooldownDays of the last change, for a bar that is not a finite
// number, and when the step would leave the bar where it is or move it the
// wrong way at a bound.
//
// Because the evidence is only what came after the last change and the
// caller records that change at now, running it again on the same vault
// changes nothing: the rule is idempotent.
func StepRelation(s TypeState, verdicts []Verdict, now time.Time) (Change, bool) {
	cur := s.Current
	if s.Pinned || !finite(cur.Persist) || !finite(cur.Surface) {
		return Change{}, false
	}
	if s.AdjustedAt != nil && now.Sub(*s.AdjustedAt) < RelationCooldownDays*24*time.Hour {
		return Change{}, false
	}

	sample := make([]Verdict, 0, len(verdicts))
	for _, v := range verdicts {
		if v.CreatedBy == relation.CreatedByUser || v.At.After(now) || s.AdjustedAt != nil && !v.At.After(*s.AdjustedAt) {
			continue
		}
		sample = append(sample, v)
	}
	slices.SortFunc(sample, func(a, b Verdict) int {
		if c := b.At.Compare(a.At); c != 0 {
			return c
		}
		return strings.Compare(b.SignalID, a.SignalID)
	})
	sample = sample[:min(len(sample), RelationSampleSize)]

	c := Change{Previous: cur, SignalIDs: make([]string, len(sample))}
	for i, v := range sample {
		c.SignalIDs[i] = v.SignalID
		if v.Rejected {
			c.Rejected++
		} else {
			c.Confirmed++
		}
	}

	var next float64
	switch {
	case c.Rejected >= RelationVerdictsToMove:
		c.Direction, next = Raised, learnedPersist(cur.Persist+RelationThresholdStep)
		if next <= cur.Persist {
			return Change{}, false
		}
	case c.Confirmed >= RelationVerdictsToMove:
		c.Direction, next = Lowered, learnedPersist(cur.Persist-RelationThresholdStep)
		if next >= cur.Persist {
			return Change{}, false
		}
	default:
		return Change{}, false
	}
	c.Next = relation.Thresholds{Persist: next, Surface: max(cur.Surface, next)}
	return c, true
}

// learnedPersist is p on a hundredth, within the learned bounds.
func learnedPersist(p float64) float64 {
	return min(max(math.Round(p*100)/100, MinLearnedPersist), MaxLearnedPersist)
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
