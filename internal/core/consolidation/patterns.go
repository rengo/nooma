package consolidation

import (
	"time"
)

// DefaultGoalStagnationDays is doc 02 §7/§13's goal-stagnation window
// default (spec R5.1) — recalibratable per user (⚙), so ResolveGoalStagnationDays
// falls back to it only for an absent or non-positive configured value.
// Pinned to migration 0002's config.goal_stagnation_days column DEFAULT by
// test/conformance/consolidation_defaults_ddl_test.go (spec R5.4).
const DefaultGoalStagnationDays = 21

// StagnationFinding is one goal-facet belief EvaluateStagnation judges
// unreinforced for at least the stagnation window (spec R5.1).
type StagnationFinding struct {
	BeliefID     string
	TopicKey     string
	StagnantDays float64
}

// EvaluateStagnation returns one StagnationFinding per Belief.
func EvaluateStagnation(bs []Belief, stagnationDays int, now time.Time) []StagnationFinding {
	return nil
}
