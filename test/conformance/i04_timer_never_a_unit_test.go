// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI04_TimerAndRecurringReminderNeverPersistAUnit is I04's own
// conformance test (docs/06-harness.md §4's table already lists I04; no
// test existed for it before this PR — design D9, spec R4.6, Q3a).
//
// docs/02-cognitive-core.md §8 states in bold "A timer is NEVER a unit: no
// weight, no decay, no graph, no belief derivation." An unarmed timer is
// still, in substance, a timer — this is what closes C3/design's own C2
// callout: the question was never open, it was unsearched (spec R4.6's own
// recorded reasoning).
//
// A timer or recurring_reminder classification MUST leave:
//   - zero units rows,
//   - zero timers rows, zero triggers rows (arming is M3 — proposal §3.3's
//     explicit non-goal),
//   - exactly one decision_log row (capture.hook.deferred), and
//   - a CaptureResult distinguishable from an ordinary successful capture
//     (Outcome: OutcomeDeferred, Deferred naming the refused Kind and a
//     plain-words Message).
//
// timers/triggers have no fake, and no port, at all: grepping
// internal/ports/ finds no TimerRepo or TriggerRepo (confirmed at the time
// this test was written — internal/ports holds exactly clock.go,
// decisionlog.go, doc.go, embeddingrepo.go, lexicalsearch.go, provider.go,
// unitrepo.go). captureRunner therefore has no port through which it could
// even attempt a timers/triggers write, so "zero timers rows, zero triggers
// rows" holds structurally, not merely by an assertion this test can make —
// there is nothing here to query. What this test actually asserts is zero
// units rows (via memrepo.Units.Count, a real observation) and the
// decision_log/CaptureResult shape; the timers/triggers half of the MUST is
// true by construction, and is recorded here rather than pretended away.
func TestI04_TimerAndRecurringReminderNeverPersistAUnit(t *testing.T) {
	tests := []struct {
		name     string
		llmCase  string
		wantKind classify.Kind
	}{
		{
			name:     "timer",
			llmCase:  "classify-timer-set-a-timer",
			wantKind: classify.KindTimer,
		},
		{
			name:     "recurring_reminder",
			llmCase:  "classify-recurring-reminder-water-plants",
			wantKind: classify.KindRecurringReminder,
		},
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
			svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals())

			result, err := svc.Capture(ctx, brain.CaptureInput{
				Text:    "irrelevant — the fake replays by case id, not prompt text",
				Channel: "chat",
			})
			if err != nil {
				t.Fatalf("Capture error = %v, want nil — a timer/recurring_reminder classification is a refusal, never a Go error (Q3a)", err)
			}

			// Zero units rows (spec R4.6's MUST NOT).
			if got := units.Count(); got != 0 {
				t.Fatalf("units.Count() = %d, want 0 — a %s classification must never persist a unit (doc 02 §8)", got, tt.wantKind)
			}

			// A caller-visible refusal, distinguishable from success.
			if result.Outcome != brain.OutcomeDeferred {
				t.Fatalf("CaptureResult.Outcome = %q, want %q — a timer/recurring_reminder classification is a refusal, not a success", result.Outcome, brain.OutcomeDeferred)
			}
			if result.Deferred == nil {
				t.Fatal("CaptureResult.Deferred = nil, want a non-nil Deferred naming the refusal")
			}
			if result.Deferred.Kind != tt.wantKind {
				t.Errorf("Deferred.Kind = %q, want %q", result.Deferred.Kind, tt.wantKind)
			}
			if result.Deferred.Message == "" {
				t.Error("Deferred.Message is empty — Q3a requires the caller be told 'not yet' in plain words")
			}

			// Exactly one decision_log row, capture.hook.deferred.
			rows, err := decisions.Since(ctx, now.Add(-time.Hour), -1)
			if err != nil {
				t.Fatalf("decisions.Since: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("decision_log has %d rows, want exactly 1: %+v", len(rows), rows)
			}
			row := rows[0]
			if row.Action != ports.ActionCaptureHookDeferred {
				t.Errorf("Action = %q, want %q", row.Action, ports.ActionCaptureHookDeferred)
			}
			if row.Rationale == "" {
				t.Error("Rationale is empty — doc 02 §11 requires a human-readable sentence naming the refusal")
			}
			if !row.OccurredAt.Equal(now) {
				t.Errorf("OccurredAt = %v, want the single clock read %v", row.OccurredAt, now)
			}

			// context carries the classification verbatim (design D9: "a
			// truthful account of what was understood, not merely of what
			// was refused").
			var rowContext struct {
				Kind           string                  `json:"kind"`
				Classification classify.Classification `json:"classification"`
				Reason         string                  `json:"reason"`
				Milestone      string                  `json:"milestone"`
			}
			if err := json.Unmarshal(row.Context, &rowContext); err != nil {
				t.Fatalf("decision_log row Context is not valid JSON: %v (%s)", err, row.Context)
			}
			if rowContext.Kind != string(tt.wantKind) {
				t.Errorf("Context.kind = %q, want %q", rowContext.Kind, tt.wantKind)
			}
			if rowContext.Classification.Kind == nil || *rowContext.Classification.Kind != tt.wantKind {
				t.Errorf("Context.classification.Kind = %v, want %q — the classification must be embedded verbatim", rowContext.Classification.Kind, tt.wantKind)
			}
			if rowContext.Reason == "" {
				t.Error("Context.reason is empty — the record must say why this was refused")
			}
			if rowContext.Milestone == "" {
				t.Error("Context.milestone is empty — the record must say when this capability arrives")
			}
		})
	}
}
