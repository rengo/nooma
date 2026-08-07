package consolidation

import (
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// ConnectSourceLimit and ConnectCandidateK bound connect's per-night
// provider cost as ONE product — design.md §4.4, doc 02 §6.4:
// ConnectSourceLimit * ConnectCandidateK judge calls at most, the number
// the owner actually calibrates. Both CHOSEN, each with its own §13 row
// (m2a's own stated reason for not collapsing two knobs that start equal:
// urgency_lead_days vs "Event lead time").
const (
	ConnectSourceLimit = 20
	ConnectCandidateK  = 5
)

// Source is one unit's decay-relevant read at the instant connect selects
// its sources (spec R4.1) — Cold's own field shapes, since both readers
// need the same weight.Effective inputs, plus the identity a caller ranks
// by.
type Source struct {
	UnitID        string
	Status        unit.Status
	Weight        float64
	DecayRate     float64
	LastTouchedAt time.Time
}

// SelectConnectSources returns the ids this pass runs recall for (spec
// R4.1, design.md §4.4): live units touched at or after since (every live
// unit when since is nil — the first pass over an existing vault), ranked
// by weight.Effective descending, ties broken by UnitID ascending, capped
// at ConnectSourceLimit.
//
// TODO(RED stub): implemented in the next commit.
func SelectConnectSources(ss []Source, since *time.Time, now time.Time) []string {
	return nil
}
