// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestCapture_OrphanActionsNowHaveCallers is design D8's own half of
// 13a.2: decision_log actions declared since PR 10a and never called
// outside test/support/repocontract's own fixture data. This proves each
// has a real production caller inside captureRunner.at, before
// classify.ToUnit is ever reached (the same "route before ToUnit" shape
// m1b-pipeline's R4.6 established for the timer refusal).
//
// **It used to drive four cases, and drives two.** chitchat and
// out_of_scope both landed on capture.discarded here, which is precisely
// the collapse ADR-0021 undid: they now leave different rows, answer
// differently, and only one of them calls a provider — none of which this
// table's shape can express, since it asserts one action and nothing about
// the reply. TestI25_ChitchatIsAnsweredAndOutOfScopeIsRefused owns them
// now. What is left here is the pair this test was always the right shape
// for: a response that could not be parsed, and one with no type in it.
func TestCapture_OrphanActionsNowHaveCallers(t *testing.T) {
	tests := []struct {
		name       string
		llmCase    string
		wantAction ports.DecisionAction
		wantErr    bool
	}{
		{"an unparseable response is capture.classify.unparseable", "classify-empty-response", ports.ActionCaptureUnparseable, true},
		{"an out-of-vocabulary type degrades to unclassifiable", "classify-unknown-enum-value", ports.ActionCaptureUnclassifiable, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
			units := memrepo.NewUnits()
			decisions := memrepo.NewDecisionLog()
			embeddings := memrepo.NewEmbeddings()
			lexical := memrepo.NewLexical()
			relations := memrepo.NewRelations()
			llm := fakeprovider.New(t, testdataLLMCasesDir(t), tt.llmCase)
			embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

			idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
			if err != nil {
				t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
			}
			svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())

			_, err = svc.Capture(ctx, brain.CaptureInput{
				Text:    "irrelevant — the scripted LLM case drives the classification",
				Channel: "chat",
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("Capture error = nil, want an error — nothing can be built from this classification")
				}
			} else if err != nil {
				t.Fatalf("Capture error = %v, want nil", err)
			}
			if got := units.Count(); got != 0 {
				t.Errorf("units.Count() = %d, want 0 — none of this table's cases ever reach classify.ToUnit", got)
			}

			rows, err := decisions.Since(ctx, now.Add(-time.Hour), -1)
			if err != nil {
				t.Fatalf("decisions.Since: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("decision_log has %d row(s), want exactly 1: %+v", len(rows), rows)
			}
			if rows[0].Action != tt.wantAction {
				t.Errorf("Action = %q, want %q", rows[0].Action, tt.wantAction)
			}
			if rows[0].Rationale == "" {
				t.Error("Rationale is empty — doc 02 §11 requires a human-readable sentence")
			}
		})
	}
}
