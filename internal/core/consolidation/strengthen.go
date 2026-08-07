package consolidation

import (
	"math"
	"sort"
	"time"
)

// StrengthenGain is doc 02 §6.3's reinforcement rate (spec R3.2, design.md
// §4.3) — CHOSEN, not derived. The identity that pins n(g) = 21 nightly
// co-use passes to a gain of 0.10 runs backwards: it solves how many
// passes a given gain takes, not which gain a fixed pass count entails, and
// any gain in roughly [0.0994, 0.1040) produces the same n(g) = 21. That
// identity, checked against DefaultGoalStagnationDays rather than the
// literal 21, is a compatibility check against the DEFAULT — it does not
// track a personalized per-user goal_stagnation_days — and is pinned in
// feat/core-consolidation-pattern-eval (task 5.7), the PR where
// DefaultGoalStagnationDays is declared. Default 0.10 (doc 02 §13).
const StrengthenGain = 0.10

// RelationEvidence is one relation's accumulated co-use evidence at the
// instant strengthen runs: the strength the judge last set, plus both
// endpoints' last_touched_at — a join no port declares today
// (design.md §8's RelationRepo handoff).
type RelationEvidence struct {
	RelationID        string
	Strength          float64
	FromLastTouchedAt time.Time
	ToLastTouchedAt   time.Time
}

// StrengthChange is the planned new strength for one relation.
type StrengthChange struct {
	RelationID string
	Strength   float64
}

// Strengthen re-evaluates relation strength with accumulated co-use
// evidence (doc 02 §6.3, spec R3.1): a relation whose BOTH endpoints were
// touched at or after since gets its strength raised toward 1,
// asymptotically, at StrengthenGain's rate — the same reinforcement law
// every other "accumulated evidence" phase in this package uses.
//
// since == nil means the vault has never consolidated
// (consolidation_last_run_at is NULL): "accumulated evidence over no
// interval" is no evidence, so Strengthen(es, nil) returns nothing at
// all — no changes and no corrupted report, because there is no pass to
// evaluate evidence against.
//
// For a non-nil since, a relation qualifies only when NEITHER endpoint's
// LastTouchedAt is before since (Before is strict, so a touch at exactly
// since qualifies). Within that qualifying set, a Strength that is NaN,
// ±Inf, or finite but outside [0,1] is refused — not coerced — and
// reported through corrupted, before the == 1 check or the reinforcement
// formula ever runs (C15/C22/C24's rule, applied at Strengthen's own
// door): a StrengthChange is a value a caller persists, exactly the line
// m2a drew between Revive, which refuses, and Effective, which does not.
//
// A relation already at strength 1 produces no row (doc 02 §11 — a
// decision with no effect writes nothing), and the formula itself never
// reaches 1 under repetition (asymptotic, strictly increasing). Strengthen
// never lowers a strength: doc 02 §4 gives exactly two ways strength moves
// down — the user rejects the relation, which deletes it — and decay is
// never consulted here.
//
// Output is sorted by RelationID.
func Strengthen(es []RelationEvidence, since *time.Time) (changes []StrengthChange, corrupted []string) {
	if since == nil {
		return nil, nil
	}

	for _, e := range es {
		if e.FromLastTouchedAt.Before(*since) || e.ToLastTouchedAt.Before(*since) {
			continue
		}

		if math.IsNaN(e.Strength) || math.IsInf(e.Strength, 0) || e.Strength < 0 || e.Strength > 1 {
			corrupted = append(corrupted, e.RelationID)
			continue
		}

		if e.Strength >= 1 {
			continue
		}

		changes = append(changes, StrengthChange{
			RelationID: e.RelationID,
			Strength:   e.Strength + StrengthenGain*(1-e.Strength),
		})
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].RelationID < changes[j].RelationID })
	sort.Strings(corrupted)
	return changes, corrupted
}
