// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// i28Case writes one classify recording into a fresh directory and returns
// that directory. The cases live here rather than in testdata/llm/cases on
// purpose: that corpus is also what `nooma doctor` sends to a live model,
// and these two replays only exist to put a non-correction type beside an
// explicit referent.
func i28Case(t *testing.T, id, message, response string) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := json.Marshal(map[string]string{
		"id": id, "provider": "openai", "model": "gpt-4o-mini", "task": "classify",
		"message": message, "response": response,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestI28_ExplicitReferentIsAlwaysACorrection is doc 02 §5 step 4's
// identifier rule, I28: a capture that names its referent is a correction of
// that unit whatever type the model gives the text standalone. Its control
// subtest is what makes it discriminate: the same classification without a
// referent still persists a unit and arms a trigger, so the referent — not
// the recording — is what the outcome turns on.
func TestI28_ExplicitReferentIsAlwaysACorrection(t *testing.T) {
	const message = "en realidad es hoy viernes 9 a las 10"
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	original := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	corrected := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)

	type fixture struct {
		svc       *brain.CaptureService
		units     *memrepo.Units
		triggers  *memrepo.Triggers
		relations *memrepo.Relations
		decisions *memrepo.DecisionLog
	}
	build := func(t *testing.T, id, response string) fixture {
		t.Helper()
		ctx := context.Background()
		f := fixture{
			units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(),
			relations: memrepo.NewRelations(), decisions: memrepo.NewDecisionLog(),
		}
		if err := f.units.Create(ctx, unit.Unit{
			ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool,
			Content: "Dentista el viernes a las 10", EventAt: &original,
			Source: "cli", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seeding the referent: %v", err)
		}
		embeddings := memrepo.NewEmbeddings()
		idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
		if err != nil {
			t.Fatalf("LoadIndex: %v", err)
		}
		llm := fakeprovider.New(t, i28Case(t, id, message, response), id)
		f.svc = brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, f.units, embeddings, memrepo.NewLexical(), f.relations, f.decisions, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx), memrepo.NewSignals(), f.triggers, memrepo.NewTimers(), 0.5, nil)
		return f
	}
	actions := func(t *testing.T, d *memrepo.DecisionLog) []ports.DecisionAction {
		t.Helper()
		rows, err := d.Since(context.Background(), time.Time{}, 100)
		if err != nil {
			t.Fatalf("decisions.Since: %v", err)
		}
		out := make([]ports.DecisionAction, len(rows))
		for i, r := range rows {
			out[i] = r.Action
		}
		return out
	}
	// assertNoNewMemory is R1's MUST NOT: no unit, no trigger, no relation.
	assertNoNewMemory := func(t *testing.T, f fixture) {
		t.Helper()
		if got := f.units.Count(); got != 1 {
			t.Errorf("units = %d, want 1 — a capture naming its referent must never persist a unit", got)
		}
		if got := f.triggers.Count(); got != 0 {
			t.Errorf("triggers = %d, want 0 — a capture naming its referent must never arm", got)
		}
		rels, err := f.relations.ByUnit(context.Background(), "dentist")
		if err != nil {
			t.Fatalf("relations.ByUnit: %v", err)
		}
		if len(rels) != 0 {
			t.Errorf("relations on the referent = %d, want 0", len(rels))
		}
	}

	const eventResponse = `{"type":"event","normalized_content":"Dentista hoy viernes 9 a las 10","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T10:00:00Z","language":"es"}`

	t.Run("an event-typed text corrects the named unit's date", func(t *testing.T) {
		f := build(t, "i28-event-dated", eventResponse)
		result, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: message, Channel: "ui", ReferentID: "dentist"})
		if err != nil {
			t.Fatalf("Capture: %v", err)
		}
		if result.Outcome != brain.OutcomeCorrected || result.Correction == nil || result.Correction.UnitID != "dentist" {
			t.Fatalf("result = %+v, want OutcomeCorrected of unit %q", result, "dentist")
		}
		got, err := f.units.ByID(context.Background(), "dentist")
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.EventAt == nil || !got.EventAt.Equal(corrected) {
			t.Errorf("EventAt = %v, want %v", got.EventAt, corrected)
		}
		assertNoNewMemory(t, f)
		if acts := actions(t, f.decisions); len(acts) != 1 || acts[0] != ports.ActionCorrectionApplied {
			t.Errorf("decision actions = %v, want exactly [%s]", acts, ports.ActionCorrectionApplied)
		}
	})

	t.Run("a text resolving no single edit asks and changes nothing", func(t *testing.T) {
		f := build(t, "i28-event-two-dates", `{"type":"event","normalized_content":"Dentista","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T10:00:00Z","due_at":"2026-10-08T10:00:00Z"}`)
		result, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: message, Channel: "ui", ReferentID: "dentist"})
		if err != nil {
			t.Fatalf("Capture: %v", err)
		}
		if result.Outcome != brain.OutcomeAsked || result.Correction == nil || result.Correction.UnitID != "dentist" {
			t.Fatalf("result = %+v, want OutcomeAsked naming unit %q", result, "dentist")
		}
		got, err := f.units.ByID(context.Background(), "dentist")
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.EventAt == nil || !got.EventAt.Equal(original) || got.DueAt != nil {
			t.Errorf("unit = event_at %v due_at %v, want it untouched", got.EventAt, got.DueAt)
		}
		assertNoNewMemory(t, f)
		if acts := actions(t, f.decisions); len(acts) != 1 || acts[0] != ports.ActionCorrectionAmbiguous {
			t.Errorf("decision actions = %v, want exactly [%s]", acts, ports.ActionCorrectionAmbiguous)
		}
	})

	t.Run("control: the same text without a referent is a new capture", func(t *testing.T) {
		f := build(t, "i28-event-dated", eventResponse)
		if _, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: message, Channel: "ui"}); err != nil {
			t.Fatalf("Capture: %v", err)
		}
		if got := f.units.Count(); got != 2 {
			t.Errorf("units = %d, want 2 — free capture must still persist the event", got)
		}
		if got := f.triggers.Count(); got != 1 {
			t.Errorf("triggers = %d, want 1 — free capture must still arm the event", got)
		}
	})
}
