package consolidation

import (
	"sort"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
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

// EvaluateStagnation finds every goal-facet Belief whose LastReinforcedAt is
// at least stagnationDays old (doc 02 §7, spec R5.1). A Belief of any other
// facet never produces a finding, however stagnant. The boundary is
// inclusive: stagnantFor >= stagnationDays fires, a hair under does not. A
// LastReinforcedAt after now (clock skew, a backdated import) clamps
// elapsed time at zero rather than going negative — never stagnant — the
// same saturate-rather-than-invert rule Effective, AgeRamp and
// ExpireIncomplete's clock-skew guard already apply.
//
// This reading of "related activity" (doc 02 §7's own words) is sound only
// because of the phase order design.md §3.2/§4.6 establishes: derive runs
// at slot five and refreshes last_reinforced_at for every belief it
// re-derives that pass; pattern_eval runs at slot seven and reads the
// value derive already refreshed THIS SAME PASS. Reorder the two phases and
// every belief the pass was about to reinforce would look stagnant for one
// more night — this is not alphabetical or historical ordering, it is a
// data dependency, and the first concrete payoff of I11 being more than
// bookkeeping.
//
// Output is sorted by BeliefID.
func EvaluateStagnation(bs []Belief, stagnationDays int, now time.Time) []StagnationFinding {
	var findings []StagnationFinding

	for _, b := range bs {
		if b.Facet != selfmodel.FacetGoal {
			continue
		}

		elapsed := now.Sub(b.LastReinforcedAt).Hours() / 24
		if elapsed < 0 {
			elapsed = 0
		}

		if elapsed >= float64(stagnationDays) {
			findings = append(findings, StagnationFinding{
				BeliefID:     b.ID,
				TopicKey:     b.TopicKey,
				StagnantDays: elapsed,
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].BeliefID < findings[j].BeliefID })
	return findings
}

// DefaultMentalLoadThreshold is doc 02 §7/§13's open-mental-load count
// default (spec R5.2) — ResolveMentalLoadThreshold falls back to it only
// for an absent or non-positive configured value. Pinned to migration
// 0002's config.mental_load_threshold column DEFAULT by
// test/conformance/consolidation_defaults_ddl_test.go (spec R5.4).
const DefaultMentalLoadThreshold = 7

// LoadCooldownDays is doc 02 §7's "cooldown of days after a resolved
// check-in" (spec R5.2) — CHOSEN, not derived: doc 02 names no number for
// it. Unrelated to DefaultMentalLoadThreshold's own coincidentally-equal 7
// — one is a duration, one is a count, and no test ties them, the same
// distinction m2a recorded for focus_size and mental_load_threshold.
const LoadCooldownDays = 7

// LoadFinding is doc 02 §7's tentative current_state hypothesis
// EvaluateLoad produces (spec R5.2).
type LoadFinding struct {
	OpenCount int
	Threshold int
}

// EvaluateLoad returns doc 02 §7's tentative current_state hypothesis.
func EvaluateLoad(openMentalLoad, threshold int, lastHypothesisAt *time.Time, now time.Time) (LoadFinding, bool) {
	return LoadFinding{}, false
}
