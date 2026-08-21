// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// embedFakeModel is the ports.EmbeddingProvider model name every test in
// this file scripts — never the real config default (I21's own concern:
// vector search filters on model, so a test picking a name that happened to
// collide with a real preset would be proving nothing).
const embedFakeModel = "conformance-embed-fake-v1"

// TestCapture_EmbedsThePersistedUnitExactlyOnce is spec R4.3's own scenario
// (design D8's persist-before-embed ordering): a captured unit is embedded
// exactly once, and the recorded Model matches the fake embedding
// provider's configured model.
func TestCapture_EmbedsThePersistedUnitExactlyOnce(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	units := memrepo.NewUnits()
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()
	relations := memrepo.NewRelations()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-pick-up-dry-cleaning")
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

	initialIdx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(initialIdx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())

	result, err := svc.Capture(ctx, brain.CaptureInput{
		Text:    "Pick up the dry cleaning on Friday",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture error = %v, want nil", err)
	}
	if !result.Embedded {
		t.Fatal("CaptureResult.Embedded = false, want true — the fake embedding provider did not fail")
	}

	if got := embed.EmbedCalls(); got != 1 {
		t.Fatalf("embed.EmbedCalls() = %d, want exactly 1 — a unit must be embedded exactly once", got)
	}

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	if len(idx.IDs) != 1 {
		t.Fatalf("LoadIndex returned %d entries, want exactly 1: %+v", len(idx.IDs), idx.IDs)
	}
	if idx.IDs[0] != result.UnitID {
		t.Errorf("LoadIndex entry ID = %q, want the captured unit's ID %q", idx.IDs[0], result.UnitID)
	}
	if idx.Model != embedFakeModel {
		t.Errorf("LoadIndex Model = %q, want the fake embedding provider's configured model %q", idx.Model, embedFakeModel)
	}
}

// TestCapture_NoEmbeddingWrittenForAUnitNotPersisted is spec R4.3's MUST
// NOT: Phase B creates no orphaned unit_embeddings row. Design D8's
// persist-before-embed ordering means the embed step is only reachable once
// units.Create has already succeeded — this test forces Create to fail (by
// colliding with an ID already present in the fake) and asserts the embed
// step is never reached at all, not merely that its result goes unwritten.
func TestCapture_NoEmbeddingWrittenForAUnitNotPersisted(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	units := memrepo.NewUnits()
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-pick-up-dry-cleaning")
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

	// counterIDs (capture_clock_test.go) hands out "id-1" first — pre-seed
	// the repo with that exact ID so units.Create collides and fails
	// (memrepo.Units.Create returns ports.ErrUnitExists), before the
	// pipeline ever reaches the embed step.
	if err := units.Create(ctx, unit.Unit{
		ID:      "id-1",
		Type:    unit.TypeTask,
		Status:  unit.StatusPool,
		Content: "already here",
		Source:  "chat",
	}); err != nil {
		t.Fatalf("seeding a colliding unit: %v", err)
	}

	initialIdx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, memrepo.NewLexical(), memrepo.NewRelations(), decisions, llm, llm, embed, brain.NewIndex(initialIdx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())

	_, err = svc.Capture(ctx, brain.CaptureInput{
		Text:    "Pick up the dry cleaning on Friday",
		Channel: "chat",
	})
	if err == nil {
		t.Fatal("Capture error = nil, want an error — the colliding ID must make units.Create fail")
	}

	if got := embed.EmbedCalls(); got != 0 {
		t.Fatalf("embed.EmbedCalls() = %d, want 0 — the pipeline must never reach the embed step for a unit it did not persist", got)
	}

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	if len(idx.IDs) != 0 {
		t.Fatalf("LoadIndex returned %d entries, want 0 — no embedding may exist for a unit this pipeline did not persist: %+v", len(idx.IDs), idx.IDs)
	}
}

// TestCapture_EmbeddingProviderFailureLeavesUnitPersisted is design D8's
// accepted, named gap (task 10b.8): a scripted embedding-provider failure
// leaves the unit persisted with CaptureResult.Embedded == false, writes a
// capture.embedding.failed decision_log row carrying the provider error in
// its context, and Capture itself does not return an error — the atomic
// alternative was rejected because it would refuse the capture on a local
// provider outage, which doc 02 §5's product rule forbids.
func TestCapture_EmbeddingProviderFailureLeavesUnitPersisted(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	units := memrepo.NewUnits()
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-pick-up-dry-cleaning")
	providerErr := errors.New("ollama: connection refused")
	embed := fakeprovider.NewEmbeddingFakeWithError(embedFakeModel, providerErr)

	initialIdx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, memrepo.NewLexical(), memrepo.NewRelations(), decisions, llm, llm, embed, brain.NewIndex(initialIdx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())

	result, err := svc.Capture(ctx, brain.CaptureInput{
		Text:    "Pick up the dry cleaning on Friday",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture error = %v, want nil — an embedding-provider outage must not refuse the capture (design D8, doc 02 §5)", err)
	}
	if result.Embedded {
		t.Fatal("CaptureResult.Embedded = true, want false — the scripted embedding provider failed")
	}
	if result.UnitID == "" {
		t.Fatal("CaptureResult.UnitID is empty — the unit must still be persisted despite the embedding failure")
	}

	if _, err := units.ByID(ctx, result.UnitID); err != nil {
		t.Fatalf("units.ByID(%q): %v — the unit must be persisted even though embedding failed", result.UnitID, err)
	}

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	if len(idx.IDs) != 0 {
		t.Fatalf("LoadIndex returned %d entries, want 0 — the failed embed must not have written a row: %+v", len(idx.IDs), idx.IDs)
	}

	rows, err := decisions.Since(ctx, now.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("decision_log has %d rows, want exactly 2 (capture.classify + capture.embedding.failed): %+v", len(rows), rows)
	}

	var failedRow *ports.Decision
	for i := range rows {
		if rows[i].Action == ports.ActionCaptureEmbeddingFailed {
			failedRow = &rows[i]
		}
	}
	if failedRow == nil {
		t.Fatalf("no %q row found among %+v", ports.ActionCaptureEmbeddingFailed, rows)
	}
	if failedRow.Rationale == "" {
		t.Error("capture.embedding.failed Rationale is empty — doc 02 §11 requires a human-readable sentence")
	}
	if !failedRow.OccurredAt.Equal(now) {
		t.Errorf("capture.embedding.failed OccurredAt = %v, want the single clock read %v", failedRow.OccurredAt, now)
	}

	var context struct {
		UnitID string `json:"unit_id"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(failedRow.Context, &context); err != nil {
		t.Fatalf("capture.embedding.failed Context is not valid JSON: %v (%s)", err, failedRow.Context)
	}
	if context.UnitID != result.UnitID {
		t.Errorf("capture.embedding.failed Context.unit_id = %q, want %q", context.UnitID, result.UnitID)
	}
	if context.Error != providerErr.Error() {
		t.Errorf("capture.embedding.failed Context.error = %q, want the provider error %q", context.Error, providerErr.Error())
	}
}
