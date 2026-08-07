package consolidation

import (
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

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
	return nil, nil
}
