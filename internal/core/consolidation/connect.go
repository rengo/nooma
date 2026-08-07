package consolidation

import (
	"sort"
	"time"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/core/relation"
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

// Pair is an ordered pair. Storage direction is what the judge said (doc 02
// §4), so a planned edge always runs source -> candidate; CanonicalPair is
// used for existing's LOOKUP only, never for the value ConnectPairs
// returns (spec R4.2, design.md §4.4).
type Pair struct {
	From string
	To   string
}

// CanonicalPair returns a and b as a symmetric, lexicographically ordered
// Pair — CanonicalPair(a, b) == CanonicalPair(b, a) — used only to key the
// existing lookup, never as a stored or returned relation direction.
func CanonicalPair(a, b string) Pair {
	if a <= b {
		return Pair{From: a, To: b}
	}
	return Pair{From: b, To: a}
}

// ConnectPairs turns one source's fused recall result into the pairs the
// judge is asked about this pass (spec R4.2, design.md §4.4): the first
// ConnectCandidateK candidates from fused, in fused's own order, excluding
// source itself and excluding any candidate whose CanonicalPair with
// source is already in existing. Every returned Pair runs {From: source,
// To: candidate} — CanonicalPair is never the storage direction.
func ConnectPairs(source string, fused []recall.FusedCandidate, existing map[Pair]bool) []Pair {
	var out []Pair
	for _, c := range fused {
		if len(out) >= ConnectCandidateK {
			break
		}
		if c.ID == source {
			continue
		}
		if existing[CanonicalPair(source, c.ID)] {
			continue
		}
		out = append(out, Pair{From: source, To: c.ID})
	}
	return out
}

// ProposedRelation is a relation plan brain can persist with
// created_by='consolidation' (spec R4.3, design.md §4.4).
type ProposedRelation struct {
	From       string
	To         string
	Type       string
	Strength   float64
	Confidence float64
	CreatedBy  relation.CreatedBy
}

// ProposeRelation applies doc 02 §4's persist decision — unchanged, through
// relation.Decide/relation.Resolve — to one judged pair (spec R4.3,
// design.md §4.4). It returns (_, false) — no plan, no decision_log row —
// for outcome "new", for relation.Discard (I08), and for a judgment
// missing any of TargetUnitID, Type, Strength or Confidence after
// tolerant decode ("a judgment that decided nothing writes nothing", doc
// 02 §4). relation.Uncertain and relation.Asserted return (_, true): the
// Uncertain band is stored AND asked about (I09), and the asking is M3's.
// The returned ProposedRelation.CreatedBy is always
// relation.CreatedByConsolidation.
//
// TODO(RED stub): implemented in the next commit.
func ProposeRelation(from string, j relation.Judgment, t relation.Thresholds) (ProposedRelation, bool) {
	return ProposedRelation{}, false
}
