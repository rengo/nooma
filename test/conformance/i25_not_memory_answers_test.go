// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI25_ChitchatIsAnsweredAndOutOfScopeIsRefused is I25
// (docs/06-harness.md §4), over docs/02-cognitive-core.md §5 step 1's
// "two of the thirteen are not memory" bullet and
// docs/adr/0021-conversation-boundary.md.
//
// Both kinds persist nothing, and that half was already true. What this
// pins is the half that was not: they answer differently, and only one of
// them spends a completion to do it. A single discard fork answering both
// with one sentence is what made a Spanish greeting come back as "Nothing
// to keep there.", and it is a shape a test that only counted units could
// never see.
//
// The chat provider is a second fakeprovider.Fake rather than the
// classifier's, for the property its unscripted-call check gives for free:
// the out_of_scope subtest scripts it with no cases at all, so a chat call
// on that path fails the test by existing, which is the ADR's asymmetry
// stated as an assertion instead of as prose.
func TestI25_ChitchatIsAnsweredAndOutOfScopeIsRefused(t *testing.T) {
	const chatReply = "¡Todo bien! Acá estoy, listo para lo que quieras guardar o preguntar."

	tests := []struct {
		name string
		// classifyCase drives which of the two kinds comes back.
		classifyCase string
		// chatScript is what the chat task is allowed to answer. Empty
		// means the chat task must never be called at all.
		chatScript  []string
		wantOutcome brain.CaptureOutcome
		wantAction  ports.DecisionAction
		wantReply   string
	}{
		{
			name:         "a chitchat is answered by the chat task",
			classifyCase: "classify-chitchat-hello",
			chatScript:   []string{"chat-hello-reply"},
			wantOutcome:  brain.OutcomeConversed,
			wantAction:   ports.ActionCaptureConversed,
			wantReply:    chatReply,
		},
		{
			name:         "an out_of_scope is refused without a completion",
			classifyCase: "classify-out-of-scope-weather",
			chatScript:   nil,
			wantOutcome:  brain.OutcomeOutOfScope,
			wantAction:   ports.ActionCaptureOutOfScope,
			wantReply:    "",
		},
		{
			name:         "a chat outage degrades, it does not fail the capture",
			classifyCase: "classify-chitchat-hello",
			chatScript:   []string{"chat-provider-outage"},
			wantOutcome:  brain.OutcomeConversed,
			wantAction:   ports.ActionCaptureChatFailed,
			wantReply:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 8, 27, 9, 30, 0, 0, time.UTC)
			units := memrepo.NewUnits()
			decisions := memrepo.NewDecisionLog()
			embeddings := memrepo.NewEmbeddings()
			lexical := memrepo.NewLexical()
			relations := memrepo.NewRelations()
			classifier := fakeprovider.New(t, testdataLLMCasesDir(t), tt.classifyCase)
			chat := fakeprovider.New(t, testdataLLMCasesDir(t), tt.chatScript...)
			embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

			idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
			if err != nil {
				t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
			}
			svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, classifier, classifier, chat, embed, brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5)

			const message = "hola, todo bien?"
			result, err := svc.Capture(ctx, brain.CaptureInput{Text: message, Channel: "chat"})
			if err != nil {
				t.Fatalf("Capture error = %v, want nil — neither kind is a failure, and neither is a provider that did not answer one", err)
			}
			if result.Outcome != tt.wantOutcome {
				t.Errorf("Outcome = %q, want %q", result.Outcome, tt.wantOutcome)
			}
			if result.Reply != tt.wantReply {
				t.Errorf("Reply = %q, want %q", result.Reply, tt.wantReply)
			}
			if got := units.Count(); got != 0 {
				t.Errorf("units.Count() = %d, want 0 — neither kind is memory", got)
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

			// The chat prompt carries the person's own words. A prompt
			// built from normalized_content would answer the classifier's
			// paraphrase of the greeting instead of the greeting, and the
			// language property ADR-0021 buys would be the classifier's
			// language, not the sender's.
			if len(tt.chatScript) > 0 {
				prompts := chat.SeenPrompts()
				if len(prompts) != 1 {
					t.Fatalf("chat task received %d prompt(s), want exactly 1", len(prompts))
				}
				if !strings.Contains(prompts[0], message) {
					t.Errorf("chat prompt does not carry the raw message %q: %q", message, prompts[0])
				}
			}
		})
	}
}
