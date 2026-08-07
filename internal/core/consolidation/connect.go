package consolidation

import (
	"sort"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
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
func SelectConnectSources(ss []Source, since *time.Time, now time.Time) []string {
	type ranked struct {
		id        string
		effective float64
	}

	var eligible []ranked
	for _, s := range ss {
		if s.Status != unit.StatusPool {
			continue
		}
		if since != nil && s.LastTouchedAt.Before(*since) {
			continue
		}
		eligible = append(eligible, ranked{
			id:        s.UnitID,
			effective: weight.Effective(s.Weight, s.DecayRate, s.LastTouchedAt, now),
		})
	}

	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].effective != eligible[j].effective {
			return eligible[i].effective > eligible[j].effective
		}
		return eligible[i].id < eligible[j].id
	})

	if len(eligible) > ConnectSourceLimit {
		eligible = eligible[:ConnectSourceLimit]
	}

	out := make([]string, len(eligible))
	for i, r := range eligible {
		out[i] = r.id
	}
	return out
}
