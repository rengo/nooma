package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// correctionRunner is the clockless worker owning the correction path
// (design D7) — no CorrectionService, no ports.Clock of its own; it
// receives now from captureRunner.at, which received it from the single
// CaptureService.Capture clock read (design D4 Layer 1, unaffected by this
// slice). This PR (12f-i) implements only applyWithPreImage/dispatchEdits
// (D5 Layers 1 and 3), so the struct carries only the fields they use.
//
// design D7's own struct literal lists two more fields — signals
// (ports.SignalRepo) and recall (*RecallService) — for the full type this
// package converges on by 12g. They are added exactly where each is first
// used (12f-ii's signals.Record call, 12g's referent-resolution routing),
// not declared ahead of a caller: the same incremental shape design D9
// already documents for captureRunner ("rels and judge are this PR's own
// two additions, landing where D4's diagram places them"), and the shape
// golangci-lint's unused check requires — a field nothing reads is a lint
// failure, not a forward-declared placeholder (tasks.md Conflicts §C7).
type correctionRunner struct {
	units ports.UnitRepo
	log   ports.DecisionLog
	ids   ports.IDGen
}

// referentSource records how applyWithPreImage's caller resolved target's
// id, for the pre-image's own "referent" object (design D5's JSON shape).
// Source is "recall" or "explicit"; the three score fields are populated
// only on the recall path and left nil on the explicit path — a zero score
// would be a claim about a ratio nobody computed, and an absent JSON key is
// the truth (design D5).
type referentSource struct {
	Source        string
	Score         *float64
	RunnerUpScore *float64
	Margin        *float64
}

// applyWithPreImage is the ONLY path in this package to a ports.UnitRepo
// Update* method — design D5 Layer 1, ADR-0016's ordering made structural.
// The pre-image row is written first; if that write fails, no Update* call
// runs at all (ADR-0016: "The row is written first. If it fails, the UPDATE
// does not happen"). Layer 2's AST guard (test/conformance) mechanizes both
// halves of this door: no Update* call exists outside dispatchEdits, and
// this function's own two calls appear in this order.
//
// One pre-image row covers the whole plan: under C6's ruling plan always
// holds at most one Edit, so the row names one field, but the shape does
// not change if a later milestone widens plan (design D5).
func (r correctionRunner) applyWithPreImage(ctx context.Context, target unit.Unit, plan []correction.Edit, ref referentSource, now time.Time) error {
	if err := r.recordPreImage(ctx, target, plan, ref, now); err != nil {
		return err // ADR-0016: the edit does not happen
	}
	return r.dispatchEdits(ctx, target.ID, plan, now)
}

// recordPreImage writes ADR-0016's pre-image: one decision_log row
// (ports.ActionCorrectionApplied) whose context carries the target unit's
// id, the field(s) about to change, their previous and next values (keyed
// by column name, per design D5's JSON shape), and how the referent was
// resolved.
func (r correctionRunner) recordPreImage(ctx context.Context, target unit.Unit, plan []correction.Edit, ref referentSource, now time.Time) error {
	fields := make([]string, 0, len(plan))
	previous := make(map[string]any, len(plan))
	next := make(map[string]any, len(plan))

	for _, e := range plan {
		f := string(e.Field())
		fields = append(fields, f)
		switch e.Field() {
		case correction.FieldContent:
			previous[f] = target.Content
			v, _ := e.Content()
			next[f] = v
		case correction.FieldEventAt:
			previous[f] = target.EventAt
			v, _ := e.EventAt()
			next[f] = v
		case correction.FieldDueAt:
			previous[f] = target.DueAt
			v, _ := e.DueAt()
			next[f] = v
		}
	}

	contextJSON, err := json.Marshal(struct {
		UnitID   string         `json:"unit_id"`
		Fields   []string       `json:"fields"`
		Previous map[string]any `json:"previous"`
		Next     map[string]any `json:"next"`
		Referent struct {
			Source        string   `json:"source"`
			Score         *float64 `json:"score,omitempty"`
			RunnerUpScore *float64 `json:"runner_up_score,omitempty"`
			Margin        *float64 `json:"margin,omitempty"`
		} `json:"referent"`
	}{
		UnitID:   target.ID,
		Fields:   fields,
		Previous: previous,
		Next:     next,
		Referent: struct {
			Source        string   `json:"source"`
			Score         *float64 `json:"score,omitempty"`
			RunnerUpScore *float64 `json:"runner_up_score,omitempty"`
			Margin        *float64 `json:"margin,omitempty"`
		}{Source: ref.Source, Score: ref.Score, RunnerUpScore: ref.RunnerUpScore, Margin: ref.Margin},
	})
	if err != nil {
		return fmt.Errorf("correction: encode pre-image context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     ports.ActionCorrectionApplied,
		Rationale:  fmt.Sprintf("correction about to write %v to unit %q; previous values recorded before the edit", fields, target.ID),
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("correction: record pre-image for unit %q: %w", target.ID, err)
	}
	return nil
}

// dispatchEdits applies plan's edits to id, one ports.UnitRepo Update* call
// per plan entry — a total switch over correction.Field, each arm reading
// the accessor named after the column its method writes (design D3's
// crossed-wiring argument). This is the ONLY function in this package
// permitted to call UpdateContent/UpdateEventAt/UpdateDueAt — design D5
// Layer 2's AST guard enforces that no other call site exists anywhere
// under internal/ outside internal/store/**/internal/ports/**.
func (r correctionRunner) dispatchEdits(ctx context.Context, id string, plan []correction.Edit, now time.Time) error {
	for _, e := range plan {
		switch e.Field() {
		case correction.FieldContent:
			v, _ := e.Content()
			if err := r.units.UpdateContent(ctx, id, v, now); err != nil {
				return fmt.Errorf("correction: update content for unit %q: %w", id, err)
			}
		case correction.FieldEventAt:
			v, _ := e.EventAt()
			if err := r.units.UpdateEventAt(ctx, id, v, now); err != nil {
				return fmt.Errorf("correction: update event_at for unit %q: %w", id, err)
			}
		case correction.FieldDueAt:
			v, _ := e.DueAt()
			if err := r.units.UpdateDueAt(ctx, id, v, now); err != nil {
				return fmt.Errorf("correction: update due_at for unit %q: %w", id, err)
			}
		default:
			return fmt.Errorf("correction: unknown edit field %q for unit %q", e.Field(), id)
		}
	}
	return nil
}
