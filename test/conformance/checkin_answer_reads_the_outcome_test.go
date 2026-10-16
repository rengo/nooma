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
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

var checkInAnswerNow = time.Date(2026, 10, 15, 21, 30, 0, 0, time.UTC)

// The classify responses gpt-6-luna gave on 2026-10-15 for a nudge taken up
// ("dale, contame") and a task said to be done ("ya lo hice"), quoted from the
// reminder phrasing set as it was recorded against the prompt before link 1b.
// Neither carries an outcome: that prompt named the outcome values and never
// said what any of them means. The third is the drop answer from the same
// session, the one check-in answer that prompt did get read.
const (
	luna2026EngagedNoOutcome = `{"type":"chitchat","normalized_content":"Dale, contame.","weight":0.2,"decay_rate":0.3,"language":"es","remind_request":false,"reminder_intent":"none"}`
	luna2026DoneNoOutcome    = `{"type":"chitchat","normalized_content":"Ya lo hice.","weight":0.2,"decay_rate":0.3,"language":"es","remind_request":false,"reminder_intent":"none"}`
	luna2026Drop             = `{"type":"chitchat","normalized_content":"Ya no hace falta; déjalo.","weight":0.2,"decay_rate":0.3,"language":"es","nudge_outcome":"declined","task_checkin_outcome":"drop","remind_request":false,"reminder_intent":"none"}`
)

// checkInAnswerCapture captures text with one delivered check-in open, the
// model answering response, and returns the triggers and the log it left.
// It drives the real pipeline (brain.Capture, classify.Decode, the real
// check-in resolution), because the property is about what reaches the code
// from the model's words.
func checkInAnswerCapture(t *testing.T, text, response string) (*memrepo.Triggers, *memrepo.DecisionLog) {
	t.Helper()
	ctx := context.Background()

	dir := t.TempDir()
	for id, c := range map[string]map[string]string{
		"classify-answer": {"provider": "openai", "model": "gpt-6-luna", "task": "classify", "message": text, "response": response},
		"chat-answer":     {"provider": "openai", "model": "gpt-6-luna", "task": "chat", "message": text, "response": "Dale."},
	} {
		c["id"] = id
		body, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	triggers := memrepo.NewTriggers()
	unitID := "u-checkin"
	fireAt := checkInAnswerNow.Add(-time.Hour)
	if err := triggers.Create(ctx, ports.Trigger{ID: "t-open", UnitID: &unitID, Kind: ports.TriggerKindTimeBased, FireAt: &fireAt}); err != nil {
		t.Fatal(err)
	}
	if err := triggers.Fire(ctx, "t-open", fireAt); err != nil {
		t.Fatal(err)
	}
	if err := triggers.Surface(ctx, "t-open", fireAt); err != nil {
		t.Fatal(err)
	}

	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, dir, "classify-answer", "chat-answer")
	svc := brain.NewCaptureService(fixedClock{now: checkInAnswerNow}, &counterIDs{},
		memrepo.NewUnits(), embeddings, memrepo.NewLexical(), memrepo.NewRelations(), decisions,
		llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx),
		memrepo.NewSignals(), triggers, memrepo.NewTimers(), 0.5, memrepo.NewPendingQuestions())
	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: text, Channel: "chat"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return triggers, decisions
}

func openCheckIns(t *testing.T, triggers *memrepo.Triggers) int {
	t.Helper()
	open, err := triggers.Delivered(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(open)
}

// TestCheckInAnswer_TheCodeActsOnlyOnTheModelsOutcome confirms the M3 bug the
// recorded set found, and where it lives. With the reading gpt-6-luna gave a
// nudge taken up and a task done, the open check-in stays open and nothing is
// logged: the code holds no word list (ADR-0033), so an answer the model did
// not read as an outcome answers nothing. The same pipeline closes the check-in
// the moment the model does read one, so the defect is the prompt's, not the
// code's (doc 02 §5 step 1).
func TestCheckInAnswer_TheCodeActsOnlyOnTheModelsOutcome(t *testing.T) {
	for _, tc := range []struct{ text, response string }{
		{"dale, contame", luna2026EngagedNoOutcome},
		{"ya lo hice", luna2026DoneNoOutcome},
	} {
		t.Run(tc.text, func(t *testing.T) {
			triggers, decisions := checkInAnswerCapture(t, tc.text, tc.response)
			if n := openCheckIns(t, triggers); n != 1 {
				t.Errorf("%d check-in(s) open, want the one still open: no outcome was read", n)
			}
			rows, err := decisions.Since(context.Background(), checkInAnswerNow.Add(-time.Hour), -1)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.Action == ports.ActionCaptureCheckInResolved || row.Action == ports.ActionCaptureCheckInUnmatched {
					t.Errorf("a reading with no outcome wrote %q", row.Action)
				}
			}
		})
	}

	triggers, _ := checkInAnswerCapture(t, "dejalo, ya no hace falta", luna2026Drop)
	if n := openCheckIns(t, triggers); n != 0 {
		t.Errorf("%d check-in(s) open after a reading with an outcome, want it closed", n)
	}
}
