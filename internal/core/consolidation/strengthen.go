package consolidation

import "time"

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
// evidence (doc 02 §6.3, spec R3.1). See design.md §4.3 for the full
// contract; implemented in the next commit.
func Strengthen(es []RelationEvidence, since *time.Time) (changes []StrengthChange, corrupted []string) {
	return nil, nil
}
