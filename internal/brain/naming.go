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
// none (ADR-0065). It returns the title the unit carries after the attempt that
// the caller should say: the one it wrote, or, when someone else named the unit
// first, theirs (so a capture arms its reminders with the title that stands); ""
// when the provider failed or the answer was not a usable title. None of those is an error, since a unit with no title is the state
// the step improves on and a failed attempt leaves it exactly as it was. A write
// that lands is logged as action, with the previous (null) and next title the way
// a correction's title edit is, and the text of the unit's armed reminders then
// follows the new title. No reply waits for this call (ADR-0064 point 1): it runs
// where the gap appears, at capture and at correction.
func (r correctionRunner) nameUnit(ctx context.Context, u unit.Unit, action ports.DecisionAction, now time.Time) (string, error) {
	title, _, err := r.attemptName(ctx, u, action, now)
	return title, err
}

// attemptName is nameUnit that also says why no title came of it: why is "" when a
// title stands (written by this call or by a rival) and otherwise the reason the
// unit stays untitled, which the night's phase logs.
func (r correctionRunner) attemptName(ctx context.Context, u unit.Unit, action ports.DecisionAction, now time.Time) (title, why string, err error) {
	if r.llm == nil {
		return "", "no provider is bound to name units", nil
	}
	resp, err := r.llm.Complete(ctx, ports.LLMRequest{Prompt: classify.BuildTitlePrompt(u), Task: taskUnitTitle, JSONOnly: true})
	if err != nil {
		slog.Warn("a unit could not be named, so it stays untitled", "unit", u.ID, "error", err)
		return "", "the provider failed", nil
	}
	decoded, _ := classify.DecodeTitle([]byte(resp.Text))
	if decoded == nil {
		return "", "the answer was not a usable title", nil
	}
	named, err := r.units.NameIfUntitled(ctx, u.ID, *decoded, now)
	if err != nil {
		slog.Warn("a unit's title could not be written, so it stays untitled", "unit", u.ID, "error", err)
		return "", "the title could not be written", nil
	}
	if !named {
		// Someone titled it meanwhile; theirs stands, and is what a reminder armed
		// next should say. A failed read leaves the caller with the content.
		if cur, err := r.units.ByID(ctx, u.ID); err == nil {
			return cur.Title, "", nil
		}
		return "", "", nil
	}
	// The title is written before its decision_log row, not after: NameIfUntitled
	// is what says whether this call won, and a lost race must log nothing, so the
	// row cannot come first (ADR-0016's record-first order has no outcome to
	// condition on here). A failure of the row or of the reminder rewrite below
	// therefore leaves a title that is written and, for the row, unlogged; it is
	// returned as an error so the caller does not report a clean run.
	contextJSON, err := json.Marshal(struct {
		UnitID   string         `json:"unit_id"`
		Fields   []string       `json:"fields"`
		Previous map[string]any `json:"previous"`
		Next     map[string]any `json:"next"`
	}{UnitID: u.ID, Fields: []string{"title"}, Previous: map[string]any{"title": nil}, Next: map[string]any{"title": *decoded}})
	if err != nil {
		return "", "", fmt.Errorf("naming: encode %s context: %w", action, err)
	}
	if err := r.log.Record(ctx, ports.Decision{ID: r.ids.New(), Action: action,
		Rationale: fmt.Sprintf("gave the dated unit %q, which had no title, the title %q", u.ID, *decoded),
		Context:   contextJSON, OccurredAt: now}); err != nil {
		return "", "", fmt.Errorf("naming: record %s for unit %q: %w", action, u.ID, err)
	}
	if r.triggers != nil {
		if err := r.retextReminders(ctx, u, *decoded, now, retextAfterNaming(action)); err != nil {
			return "", "", err
		}
	}
	return *decoded, "", nil
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
