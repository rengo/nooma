// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestCapture_CorrectionPlanWritesExactlyOneField is spec R1.8's own L2
// half: a content-only correction leaves both dates unchanged, two
// resolved dates ask rather than guess, and at most one ports.UnitRepo
// Update* call ever reaches the repository per correction. R1.8's
// date-only row (dates win, content stays stale) is already proven by
// capture_correction_referent_test.go's "single strong match" subtest —
// not repeated here.
func TestCapture_CorrectionPlanWritesExactlyOneField(t *testing.T) {
	newSvc := func(t *testing.T, units *memrepo.Units, decisions *memrepo.DecisionLog, llmCase string) *brain.CaptureService {
		t.Helper()
		ctx := context.Background()
		embeddings := memrepo.NewEmbeddings()
		lexical := memrepo.NewLexical()
		relations := memrepo.NewRelations()
		llm := fakeprovider.New(t, testdataLLMCasesDir(t), llmCase)
		embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
		idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
		if err != nil {
			t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
		}
		return brain.NewCaptureService(fixedClock{now: fixedNow}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())
	}

	t.Run("content-only correction leaves both dates unchanged", func(t *testing.T) {
		ctx := context.Background()
		units := memrepo.NewUnits()
		originalEventAt := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
		originalDueAt := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
		if err := units.Create(ctx, unit.Unit{
			ID: "person-ref-1", Type: unit.TypeTask, Status: unit.StatusPool,
			Content: "Meeting with Anna on Tuesday", EventAt: &originalEventAt, DueAt: &originalDueAt,
			Source: "chat", CreatedAt: fixedNow, UpdatedAt: fixedNow,
		}); err != nil {
			t.Fatalf("seeding person-ref-1: %v", err)
		}

		svc := newSvc(t, units, memrepo.NewDecisionLog(), "classify-correction-content-only")
		result, err := svc.Capture(ctx, brain.CaptureInput{Text: "it's Ana, not Anna", Channel: "chat", ReferentID: "person-ref-1"})
		if err != nil {
			t.Fatalf("Capture error = %v, want nil", err)
		}
		if result.Outcome != brain.OutcomeCorrected {
			t.Fatalf("Outcome = %q, want %q", result.Outcome, brain.OutcomeCorrected)
		}

		got, err := units.ByID(ctx, "person-ref-1")
		if err != nil {
			t.Fatalf("units.ByID: %v", err)
		}
		if got.Content != "It's Ana, not Anna" {
			t.Errorf("Content = %q, want the corrected content — content is the no-date fallback (R1.8)", got.Content)
		}
		if got.EventAt == nil || !got.EventAt.Equal(originalEventAt) {
			t.Errorf("EventAt = %v, want unchanged %v", got.EventAt, originalEventAt)
		}
		if got.DueAt == nil || !got.DueAt.Equal(originalDueAt) {
			t.Errorf("DueAt = %v, want unchanged %v", got.DueAt, originalDueAt)
		}
	})

	t.Run("two resolved dates ask, editing nothing", func(t *testing.T) {
		ctx := context.Background()
		units := memrepo.NewUnits()
		originalEventAt := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
		originalDueAt := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
		if err := units.Create(ctx, unit.Unit{
			ID: "task-1", Type: unit.TypeTask, Status: unit.StatusPool,
			Content: "Renew the passport", EventAt: &originalEventAt, DueAt: &originalDueAt,
			Source: "chat", CreatedAt: fixedNow, UpdatedAt: fixedNow,
		}); err != nil {
			t.Fatalf("seeding task-1: %v", err)
		}

		decisions := memrepo.NewDecisionLog()
		svc := newSvc(t, units, decisions, "classify-correction-both-dates")
		result, err := svc.Capture(ctx, brain.CaptureInput{Text: "move it to the 15th and the due date to the 20th", Channel: "chat", ReferentID: "task-1"})
		if err != nil {
			t.Fatalf("Capture error = %v, want nil — an ambiguous plan asks, it does not fail", err)
		}
		if result.Outcome != brain.OutcomeAsked {
			t.Fatalf("Outcome = %q, want %q", result.Outcome, brain.OutcomeAsked)
		}
		if result.Correction == nil || !result.Correction.Ambiguous || result.Correction.UnitID != "task-1" {
			t.Fatalf("Correction = %+v, want {UnitID: task-1, Ambiguous: true} — the referent resolved, only the plan was ambiguous", result.Correction)
		}

		got, err := units.ByID(ctx, "task-1")
		if err != nil {
			t.Fatalf("units.ByID: %v", err)
		}
		if got.Content != "Renew the passport" || got.EventAt == nil || !got.EventAt.Equal(originalEventAt) || got.DueAt == nil || !got.DueAt.Equal(originalDueAt) {
			t.Errorf("task-1 = %+v, want byte-identical — both dates present must edit nothing (R1.8)", got)
		}
	})

	t.Run("at most one Update* call reaches ports.UnitRepo per correction", func(t *testing.T) {
		ctx := context.Background()
		inner := memrepo.NewUnits()
		originalEventAt := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
		if err := inner.Create(ctx, unit.Unit{
			ID: "target-1", Type: unit.TypeTask, Status: unit.StatusPool,
			Content: "Dentist appointment", EventAt: &originalEventAt,
			Source: "chat", CreatedAt: fixedNow, UpdatedAt: fixedNow,
		}); err != nil {
			t.Fatalf("seeding target-1: %v", err)
		}
		counting := &updateCallCountingUnits{Units: inner}

		embeddings := memrepo.NewEmbeddings()
		lexical := memrepo.NewLexical()
		relations := memrepo.NewRelations()
		decisions := memrepo.NewDecisionLog()
		llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-correction-dentist-date")
		embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
		idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
		if err != nil {
			t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
		}
		svc := brain.NewCaptureService(fixedClock{now: fixedNow}, &counterIDs{}, counting, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())

		result, err := svc.Capture(ctx, brain.CaptureInput{Text: "irrelevant", Channel: "chat", ReferentID: "target-1"})
		if err != nil {
			t.Fatalf("Capture error = %v, want nil", err)
		}
		if result.Outcome != brain.OutcomeCorrected {
			t.Fatalf("Outcome = %q, want %q", result.Outcome, brain.OutcomeCorrected)
		}
		if counting.updateCalls != 1 {
			t.Errorf("Update* calls = %d, want exactly 1 — C6's ruling: a correction writes exactly one field", counting.updateCalls)
		}
	})
}

// fixedNow is the single instant every test in this file drives its
// CaptureService with — none of these tests assert anything about time
// itself.
var fixedNow = time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

// updateCallCountingUnits wraps *memrepo.Units, counting every
// UpdateContent/UpdateEventAt/UpdateDueAt call — R1.8's own "at most one
// Update* call reaches ports.UnitRepo per correction" needs a call count,
// not merely a field-value assertion: a bug that called Update* twice but
// left the second call's value unobserved would still pass a value-only
// check.
type updateCallCountingUnits struct {
	*memrepo.Units
	updateCalls int
}

func (u *updateCallCountingUnits) UpdateContent(ctx context.Context, id, content string, at time.Time) error {
	u.updateCalls++
	return u.Units.UpdateContent(ctx, id, content, at)
}

func (u *updateCallCountingUnits) UpdateEventAt(ctx context.Context, id string, eventAt, at time.Time) error {
	u.updateCalls++
	return u.Units.UpdateEventAt(ctx, id, eventAt, at)
}

func (u *updateCallCountingUnits) UpdateDueAt(ctx context.Context, id string, dueAt, at time.Time) error {
	u.updateCalls++
	return u.Units.UpdateDueAt(ctx, id, dueAt, at)
}
