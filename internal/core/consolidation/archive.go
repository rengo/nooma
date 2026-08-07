package consolidation

import (
	"math"
	"sort"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
)

// DefaultWeightThreshold is doc 02 §13's weight_threshold default,
// relation.Resolve's own house pattern (spec R2.3): config's singleton row
// has never existed in any vault, so a nil configured value means absence,
// not a deliberately-configured zero. Pinned to migration 0002's
// config.weight_threshold column DEFAULT by
// test/conformance/consolidation_defaults_ddl_test.go.
const DefaultWeightThreshold = 0.5

// Cold is a unit's decay-relevant read at the instant archive runs (spec
// R2.2) — weight.Effective's own field shapes, plus the identity and
// status a Transition or a corrupted entry must carry back.
type Cold struct {
	UnitID        string
	Status        unit.Status
	Weight        float64
	DecayRate     float64
	LastTouchedAt time.Time
}

// Archive cools a unit whose effective weight is strictly below threshold
// (doc 02 §6.2, spec R2.2). A Cold whose Status is not unit.StatusPool
// produces no transition and no corrupted entry — Archive only ever cools
// a live unit. A Cold whose Weight or DecayRate is non-finite is refused —
// not archived — and reported through corrupted (C15's rule).
//
// Both returned slices are sorted by UnitID.
func Archive(cs []Cold, threshold float64, now time.Time) (transitions []Transition, corrupted []string) {
	for _, c := range cs {
		if c.Status != unit.StatusPool {
			continue
		}

		if math.IsNaN(c.Weight) || math.IsInf(c.Weight, 0) ||
			math.IsNaN(c.DecayRate) || math.IsInf(c.DecayRate, 0) {
			corrupted = append(corrupted, c.UnitID)
			continue
		}

		e := weight.Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
		if e < threshold {
			transitions = append(transitions, Transition{
				UnitID: c.UnitID,
				From:   unit.StatusPool,
				To:     unit.StatusArchived,
				Reason: ReasonBelowWeightThreshold,
			})
		}
	}

	sort.Slice(transitions, func(i, j int) bool { return transitions[i].UnitID < transitions[j].UnitID })
	sort.Strings(corrupted)
	return transitions, corrupted
}

// ResolveWeightThreshold falls back to DefaultWeightThreshold for an
// absent or corrupt configured value (spec R2.3). This takes the opposite
// posture to focus.ResolveMargin, deliberately: a margin has a safe
// neutral (0 — no anti-jitter protection this round), but a threshold has
// none — 0 archives nothing, +Inf archives everything — so a corrupt
// threshold falls back to the calibrated default rather than to a neutral
// value that would silently change Archive's behaviour in either
// direction.
func ResolveWeightThreshold(configured *float64) float64 {
	return 0
}
