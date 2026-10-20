package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// closeDone closes the task a done follow-up named (doc 02 §5 step 4,
// ADR-0040): archived, since nothing is deleted, its armed reminders expired,
// and its open check-ins answered engaged, as a check-in done answers one.
//
// handled is false when the item is not a task a done closes (another kind,
// or a series a done of one occurrence must not end, prospection.DoneCloses):
// the capture then takes the path it took before, which answers an open
// check-in if there is one. A task that left the pool between the prompt and
// the write closed nothing, and the reply says so.
func (r captureRunner) closeDone(ctx context.Context, id string, now time.Time) (CaptureResult, bool, error) {
	u, err := r.correction.explicitReferent(ctx, id)
	if err != nil {
		return CaptureResult{}, false, fmt.Errorf("capture: done: %w", err)
	}
	armed, err := r.triggers.ArmedForUnit(ctx, id)
	if err != nil {
		return CaptureResult{}, false, fmt.Errorf("capture: done: reminders of %q: %w", id, err)
	}
	recurring := slices.ContainsFunc(armed, func(t ports.DueTrigger) bool { return t.RecurrenceRule != nil })
	if !prospection.DoneCloses(u.Type, u.Status, recurring) {
		return CaptureResult{}, false, nil
	}
	if err := unit.ValidateTransition(u.Status, unit.StatusArchived); err != nil {
		return CaptureResult{}, false, fmt.Errorf("capture: done: %w", err)
	}
	if err := r.units.SetStatus(ctx, id, u.Status, unit.StatusArchived, now); err != nil {
		if errors.Is(err, ports.ErrStatusConflict) {
			return CaptureResult{Outcome: OutcomeCorrected, Correction: &Correction{NothingClosed: true}}, true, nil
		}
		return CaptureResult{}, false, fmt.Errorf("capture: done: archive %q: %w", id, err)
	}

	change := taskClosed{UnitID: id, Fields: []string{"status"},
		Previous: map[string]string{"status": string(u.Status)}, Next: map[string]string{"status": string(unit.StatusArchived)}}
	for _, t := range armed {
		// One that fired meanwhile is no longer armed, and its check-in is
		// answered below if it reached the user.
		if err := r.triggers.Expire(ctx, t.ID); err != nil && !errors.Is(err, ports.ErrTriggerStatusConflict) {
			return CaptureResult{}, false, fmt.Errorf("capture: done: expire reminder %q: %w", t.ID, err)
		} else if err == nil {
			change.RemindersExpired = append(change.RemindersExpired, t.ID)
		}
	}
	open, err := r.triggers.Delivered(ctx)
	if err != nil {
		return CaptureResult{}, false, fmt.Errorf("capture: done: reading open check-ins: %w", err)
	}
	for _, o := range open {
		if o.UnitID == nil || *o.UnitID != id {
			continue
		}
		if err := r.triggers.Resolve(ctx, o.ID, ports.ResolutionEngaged, now); err != nil && !errors.Is(err, ports.ErrTriggerStatusConflict) {
			return CaptureResult{}, false, fmt.Errorf("capture: done: answer check-in %q: %w", o.ID, err)
		} else if err == nil {
			change.CheckInsAnswered = append(change.CheckInsAnswered, o.ID)
		}
	}

	contextJSON, err := json.Marshal(change)
	if err != nil {
		return CaptureResult{}, false, fmt.Errorf("capture: done: encode %s context: %w", ports.ActionCaptureTaskClosed, err)
	}
	if err := r.log.Record(ctx, ports.Decision{ID: r.ids.New(), Action: ports.ActionCaptureTaskClosed,
		Rationale: fmt.Sprintf("the user said task %q is done; it was archived, %d reminder(s) expired and %d check-in(s) answered",
			id, len(change.RemindersExpired), len(change.CheckInsAnswered)),
		Context: contextJSON, OccurredAt: now}); err != nil {
		return CaptureResult{}, false, fmt.Errorf("capture: done: record %s: %w", ports.ActionCaptureTaskClosed, err)
	}
	return CaptureResult{Outcome: OutcomeCorrected, Correction: &Correction{UnitID: id, Closed: clip(shownText(u))}}, true, nil
}

// taskClosed is the capture.task.closed row's context: change-shaped, so
// /ui/activity shows the status before and after, with what went with it.
type taskClosed struct {
	UnitID           string            `json:"unit_id"`
	Fields           []string          `json:"fields"`
	Previous         map[string]string `json:"previous"`
	Next             map[string]string `json:"next"`
	RemindersExpired []string          `json:"reminders_expired,omitempty"`
	CheckInsAnswered []string          `json:"checkins_answered,omitempty"`
}

// saidDoneOfNothing is a chat message, read as a chitchat that says something
// is done, that answered no check-in: it closed nothing, and the reply must
// not sound as if it had (ADR-0040). With or without turns shown: a done read
// alone, in a chat with no turns, closes nothing either. A capture from
// the UI or the API is no chat answer and keeps the chitchat's reply.
func saidDoneOfNothing(c classify.Classification, chat, answered bool) bool {
	return chat && !answered && c.TaskCheckinOutcome != nil && *c.TaskCheckinOutcome == classify.TaskCheckinOutcomeDone
}
