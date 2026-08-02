// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// fixedClock is a ports.Clock returning a single, fixed instant — used here
// where the clock-read-once property itself is not under test (that is
// capture_clock_test.go's job); this file cares about what the one instant
// ends up doing.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// TestCapture_OrdinaryClassificationPersistsAUnit is spec R4.2's own
// scenario, driven end-to-end against test/support/memrepo (Phase A's
// in-memory ports.UnitRepo/ports.DecisionLog fakes) and
// test/support/fakeprovider (replaying a real testdata/llm/ recording) —
// design D4's pipeline diagram, restricted to this slice's scope: BuildPrompt
// -> llm.Complete -> Decode -> ToUnit -> units.Create, then one
// decision_log row.
//
// "classify-pick-up-dry-cleaning" (testdata/llm/cases) is an ordinary task
// classification with no event_at/due_at — the R4.6/R4.7 refusal paths
// (timer, ambiguous person) are a different slice (design's own table
// assigns CaptureResult.Deferred to PR 10c) and are not exercised here.
func TestCapture_OrdinaryClassificationPersistsAUnit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	units := memrepo.NewUnits()
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-pick-up-dry-cleaning")
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, decisions, llm, embed, brain.NewIndex(idx))

	result, err := svc.Capture(ctx, brain.CaptureInput{
		Text:    "Pick up the dry cleaning on Friday",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture error = %v, want nil", err)
	}
	if result.UnitID == "" {
		t.Fatal("CaptureResult.UnitID is empty — the caller has no way to find the unit this capture persisted")
	}

	u, err := units.ByID(ctx, result.UnitID)
	if err != nil {
		t.Fatalf("units.ByID(%q): %v — the pipeline reported success but persisted nothing", result.UnitID, err)
	}

	if u.Status != unit.StatusPool {
		t.Errorf("Status = %q, want %q — every capture in this PR produces a live unit (Q3a)", u.Status, unit.StatusPool)
	}
	if u.Type != unit.TypeTask {
		t.Errorf("Type = %q, want %q", u.Type, unit.TypeTask)
	}
	if u.Content != "Pick up the dry cleaning" {
		t.Errorf("Content = %q, want the model's normalized_content verbatim", u.Content)
	}

	// The fixture supplies weight/decay_rate itself (0.6/0.1), so PR 7b's
	// priors must not have overridden them — the prior is a fallback, not a
	// default.
	if u.Weight != 0.6 {
		t.Errorf("Weight = %v, want the classification's own 0.6, not classify.PriorWeight (%v)", u.Weight, classify.PriorWeight)
	}
	if u.WeightDecayRate != 0.1 {
		t.Errorf("WeightDecayRate = %v, want the classification's own 0.1, not classify.PriorDecayRate (%v)", u.WeightDecayRate, classify.PriorDecayRate)
	}

	// I18: all three timestamps are the single clock read from 10b.1, and
	// none of them is the zero time.
	for name, got := range map[string]time.Time{
		"CreatedAt":     u.CreatedAt,
		"UpdatedAt":     u.UpdatedAt,
		"LastTouchedAt": u.LastTouchedAt,
	} {
		if !got.Equal(now) {
			t.Errorf("%s = %v, want the single clock read %v", name, got, now)
		}
	}

	// R4.5/I12: exactly one relevant decision_log row, written by
	// internal/brain (the fake enforces nothing about the writer's
	// package — that half is proven by internal/core's depguard denying
	// it internal/ports at all, spec R4.5's own MUST NOT).
	rows, err := decisions.Since(ctx, now.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("decision_log has %d rows, want exactly 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.Action != ports.ActionCaptureClassify {
		t.Errorf("Action = %q, want %q", row.Action, ports.ActionCaptureClassify)
	}
	if row.Rationale == "" {
		t.Error("Rationale is empty — doc 02 §11 requires a human-readable sentence, not a bare code")
	}
	if !row.OccurredAt.Equal(now) {
		t.Errorf("OccurredAt = %v, want the single clock read %v", row.OccurredAt, now)
	}
	var context struct {
		Kind   string `json:"kind"`
		UnitID string `json:"unit_id"`
	}
	if err := json.Unmarshal(row.Context, &context); err != nil {
		t.Fatalf("decision_log row Context is not valid JSON: %v (%s)", err, row.Context)
	}
	if context.UnitID != result.UnitID {
		t.Errorf("decision_log Context.unit_id = %q, want %q", context.UnitID, result.UnitID)
	}
}
