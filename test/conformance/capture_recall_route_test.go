// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestCapture_RecallClassificationNeverPersistsAUnit is spec R2.3's own
// negative half (design D9; Conflicts §C11): a `recall`-classified capture
// never reaches classify.ToUnit or ports.UnitRepo.Create — I22 already
// proves the positive half (the two entrances agree); this proves the
// MUST NOT directly, the same "route before ToUnit" shape R1.1's own test
// proves for corrections.
func TestCapture_RecallClassificationNeverPersistsAUnit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 3, 9, 30, 0, 0, time.UTC)

	units := memrepo.NewUnits()
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()
	relations := memrepo.NewRelations()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-recall-leaky-faucet")
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())

	result, err := svc.Capture(ctx, brain.CaptureInput{
		Text:    "how do I fix the leaky faucet",
		Channel: "chat",
	})
	if err != nil {
		t.Fatalf("Capture error = %v, want nil — a recall is an entrance to the existing mechanism, never a failure", err)
	}
	if result.Outcome != brain.OutcomeRecalled {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, brain.OutcomeRecalled)
	}
	if got := units.Count(); got != 0 {
		t.Errorf("units.Count() = %d, want 0 — a recall must never persist a unit (R2.3's own MUST NOT)", got)
	}

	rows, err := decisions.Since(ctx, now.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("decision_log has %d row(s), want 0 — recall is a read, and reads write no row (I12, design D9)", len(rows))
	}
}
