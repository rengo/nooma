package brain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
)

// isGoalCheckIn reports whether an open check-in asks about a goal (doc 02
// §7, ADR-0054): it carries the goal's belief. Such a check-in is answered
// only by naming it, never by the generic check-in answer.
func isGoalCheckIn(o ports.OpenCheckIn) bool { return o.Payload.BeliefID != "" }

// isGoalAsk reports whether an armed trigger is a goal check-in waiting for
// the morning digest to ask it.
func isGoalAsk(t ports.DueTrigger) bool { return t.Payload.BeliefID != "" }

// nextGoalAsk is the one goal check-in this digest may ask, the longest
// waiting, or nil: armed and due, never yet asked. Like the relation question
// it competes with no ranked item and is asked one a morning.
func nextGoalAsk(ctx context.Context, triggers ports.TriggerRepo, now time.Time) (*ports.DueTrigger, error) {
	due, err := triggers.Due(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("goal check-ins to ask: %w", err)
	}
	for i := range due {
		if isGoalAsk(due[i]) {
			return &due[i], nil
		}
	}
	return nil, nil
}

// openGoalCheckIns is the open goal check-ins a chat message may answer, in
// Delivered's order (most recently asked first), at most the positions the
// prompt can list.
func (r captureRunner) openGoalCheckIns(ctx context.Context) ([]ports.OpenCheckIn, error) {
	open, err := r.triggers.Delivered(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture: reading open goal check-ins: %w", err)
	}
	var goals []ports.OpenCheckIn
	for _, o := range open {
		if isGoalCheckIn(o) && len(goals) < classify.MaxReminderChoices {
			goals = append(goals, o)
		}
	}
	return goals, nil
}

// goalEntries is what the prompt lists of each: what Nooma asked.
func goalEntries(goals []ports.OpenCheckIn) []classify.GoalEntry {
	out := make([]classify.GoalEntry, len(goals))
	for i, g := range goals {
		out[i] = classify.GoalEntry{Text: g.Payload.ActionText}
	}
	return out
}

// resolveGoalCheckIn applies a typed goal answer to the listed check-in it
// names (ADR-0054). Engaged and declined close it; a snooze leaves it open.
// Each writes its row, then its signal (I12, I47). It reports whether an
// answer bound, so the generic check-in answer is not also applied.
func (r captureRunner) resolveGoalCheckIn(ctx context.Context, c classify.Classification, goals []ports.OpenCheckIn, now time.Time) (bool, error) {
	pos, outcome, ok := classify.ReadGoalAnswer(c, len(goals))
	if !ok {
		return false, nil
	}
	target := goals[pos-1]
	if outcome == classify.GoalOutcomeSnooze {
		if err := r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInSnoozed,
			fmt.Sprintf("goal check-in %q was put off", target.ID), target.ID, "", len(goals)); err != nil {
			return false, err
		}
		return true, recordNudgeSignal(ctx, r.signals, r.ids, ports.SignalNudgeAck, target.ID, nil, target.Payload.BeliefID, now)
	}
	resolution, verdict := ports.ResolutionEngaged, ports.SignalNudgeEngaged
	if outcome == classify.GoalOutcomeDeclined {
		resolution, verdict = ports.ResolutionDeclined, ports.SignalNudgeDeclined
	}
	if err := r.triggers.Resolve(ctx, target.ID, resolution, now); err != nil {
		if !errors.Is(err, ports.ErrTriggerStatusConflict) {
			return false, fmt.Errorf("capture: resolving goal check-in %q: %w", target.ID, err)
		}
		// Closed meanwhile, unanswered: the answer reaches nothing.
		return true, r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInUnmatched,
			fmt.Sprintf("a goal answer of %q arrived as check-in %q closed unanswered", outcome, target.ID), target.ID, resolution, len(goals))
	}
	if err := r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInResolved,
		fmt.Sprintf("goal check-in %q resolved as %q", target.ID, resolution), target.ID, resolution, len(goals)); err != nil {
		return false, err
	}
	return true, recordNudgeSignal(ctx, r.signals, r.ids, verdict, target.ID, nil, target.Payload.BeliefID, now)
}

// withoutGoalCheckIns is the generic check-in answer's pool (ADR-0054).
func withoutGoalCheckIns(open []ports.OpenCheckIn) []ports.OpenCheckIn {
	var out []ports.OpenCheckIn
	for _, o := range open {
		if !isGoalCheckIn(o) {
			out = append(out, o)
		}
	}
	return out
}
