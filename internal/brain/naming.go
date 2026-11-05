package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// taskUnitTitle is the LLMRequest.Task value the naming step sends. It is a
// label, not a configurable task: the call goes through the provider bound to
// capture_processing, since naming a unit is understanding it as capture does
// (ADR-0065, ADR-0033).
const taskUnitTitle = "unit_title"

// dated is whether a unit states an instant of its own, the case where its
// content carries a date a reply would then repeat (ADR-0065 point 1).
func dated(u unit.Unit) bool { return u.EventAt != nil || u.DueAt != nil }

// nameUnit asks the model for u's title and writes it where the unit still has
// none (ADR-0065). It returns the title it wrote, or "" when it wrote none: the
// provider failed, the answer was not a usable title, or someone else named the
// unit first. None of those is an error, since a unit with no title is the state
// the step improves on and a failed attempt leaves it exactly as it was. A write
// that lands is logged as action, with the previous (null) and next title the way
// a correction's title edit is, and the text of the unit's armed reminders then
// follows the new title. No reply waits for this call (ADR-0064 point 1): it runs
// where the gap appears, at capture and at correction.
func (r correctionRunner) nameUnit(ctx context.Context, u unit.Unit, action ports.DecisionAction, now time.Time) (string, error) {
	if r.llm == nil {
		return "", nil
	}
	resp, err := r.llm.Complete(ctx, ports.LLMRequest{Prompt: classify.BuildTitlePrompt(u), Task: taskUnitTitle, JSONOnly: true})
	if err != nil {
		slog.Warn("a unit could not be named, so it stays untitled", "unit", u.ID, "error", err)
		return "", nil
	}
	title, _ := classify.DecodeTitle([]byte(resp.Text))
	if title == nil {
		return "", nil
	}
	named, err := r.units.NameIfUntitled(ctx, u.ID, *title, now)
	if err != nil {
		slog.Warn("a unit's title could not be written, so it stays untitled", "unit", u.ID, "error", err)
		return "", nil
	}
	if !named {
		return "", nil // someone titled it meanwhile; theirs stands
	}
	contextJSON, err := json.Marshal(struct {
		UnitID   string         `json:"unit_id"`
		Fields   []string       `json:"fields"`
		Previous map[string]any `json:"previous"`
		Next     map[string]any `json:"next"`
	}{UnitID: u.ID, Fields: []string{"title"}, Previous: map[string]any{"title": nil}, Next: map[string]any{"title": *title}})
	if err != nil {
		return "", fmt.Errorf("naming: encode %s context: %w", action, err)
	}
	if err := r.log.Record(ctx, ports.Decision{ID: r.ids.New(), Action: action,
		Rationale: fmt.Sprintf("gave the dated unit %q, which had no title, the title %q", u.ID, *title),
		Context:   contextJSON, OccurredAt: now}); err != nil {
		return "", fmt.Errorf("naming: record %s for unit %q: %w", action, u.ID, err)
	}
	if err := r.retextReminders(ctx, u, *title, now); err != nil {
		return "", err
	}
	return *title, nil
}

// clearsTheTitle is whether plan empties the title of a unit.
func clearsTheTitle(plan []correction.Edit) bool {
	for _, e := range plan {
		if v, ok := e.Title(); ok && v == "" {
			return true
		}
	}
	return false
}

// edited is target as plan leaves it: its content, instants and title.
func edited(target unit.Unit, plan []correction.Edit) unit.Unit {
	for _, e := range plan {
		if v, ok := e.Content(); ok {
			target.Content = v
		}
		if v, ok := e.EventAt(); ok {
			target.EventAt = &v
		}
		if v, ok := e.DueAt(); ok {
			target.DueAt = &v
		}
		if v, ok := e.Title(); ok {
			target.Title = v
		}
	}
	return target
}
