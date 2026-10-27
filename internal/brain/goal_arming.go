package brain

import (
	"context"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/ports"
)

// goalArmed is the consolidate.pattern_eval.goal_checkin_armed row's context.
type goalArmed struct {
	TriggerID    string  `json:"trigger_id"`
	BeliefID     string  `json:"belief_id"`
	TopicKey     string  `json:"topic_key"`
	StagnantDays float64 `json:"stagnant_days"`
}

// armGoalCheckIns arms a check-in for every stagnant goal that is due one
// (doc 02 §7, ADR-0054): at most one per goal per goal_stagnation_days, twice
// that after a declined answer (consolidation.GoalCheckInsDue). It is a
// pattern_based trigger hanging off no unit, carrying the goal's belief, due
// now, so the morning digest asks it under its own gates. It speaks the user's
// usual language. A goal stagnant but not due writes nothing (I12).
func (r consolidateRunner) armGoalCheckIns(ctx context.Context, findings []consolidation.StagnationFinding, active []ports.Belief, days int, now time.Time) error {
	if r.triggers == nil || len(findings) == 0 {
		return nil
	}
	asked, err := r.triggers.GoalCheckIns(ctx)
	if err != nil {
		return fmt.Errorf("consolidate: pattern_eval: read goal check-ins: %w", err)
	}
	last := make(map[string]consolidation.GoalCheckIn, len(asked))
	for _, g := range asked {
		last[g.BeliefID] = g
	}
	content := make(map[string]string, len(active))
	for _, b := range active {
		content[b.ID] = b.Content
	}
	set := proactiveSet(ctx, r.units, "")
	for _, f := range consolidation.GoalCheckInsDue(findings, last, days, now) {
		at := now
		trg := ports.Trigger{ID: r.ids.New(), Kind: ports.TriggerKindPatternBased, FireAt: &at, CreatedAt: now,
			Payload: ports.TriggerPayload{ActionText: set.Goal(clip(content[f.BeliefID])), GoalText: clip(content[f.BeliefID]), BeliefID: f.BeliefID,
				Rationale: fmt.Sprintf("goal %q has had no related activity for %.0f days", f.TopicKey, f.StagnantDays)}}
		if err := r.triggers.Create(ctx, trg); err != nil {
			return fmt.Errorf("consolidate: pattern_eval: arm the check-in of goal %q: %w", f.BeliefID, err)
		}
		rationale := fmt.Sprintf("pattern_eval: goal %q stagnant for %.1f days; a check-in was set for the morning digest", f.TopicKey, f.StagnantDays)
		if err := r.record(ctx, now, ports.ActionPatternEvalGoalCheckInArmed, rationale,
			goalArmed{TriggerID: trg.ID, BeliefID: f.BeliefID, TopicKey: f.TopicKey, StagnantDays: f.StagnantDays}); err != nil {
			return err
		}
	}
	return nil
}
