// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI26_ACaptureIsAnsweredInTheMessagesLanguage is I26
// (docs/06-harness.md §4), over docs/02-cognitive-core.md §5 step 1 and
// docs/adr/0022-reply-language.md.
//
// The unit tests below this prove the vocabulary is complete and that the
// classification decodes a language. Neither proves the two are connected,
// and the gap between them is exactly where this feature can be finished
// and still not work: a language decoded, carried nowhere, and every reply
// still English.
//
// So this runs a real capture through the real pipeline and renders the
// real result — the same three steps the Telegram runner performs.
func TestI26_ACaptureIsAnsweredInTheMessagesLanguage(t *testing.T) {
	tests := []struct {
		name string
		// llmCase drives what language the classification names, if any.
		llmCase  string
		wantLang classify.Language
		// wantReply is the whole sentence, not a fragment. A test
		// asserting "contains Anotado" would pass on a reply that was
		// Spanish in one clause and English in the next.
		wantReply string
	}{
		{
			name:      "a spanish message is answered in spanish",
			llmCase:   "classify-spanish-task",
			wantLang:  classify.LanguageES,
			wantReply: "Anotado.",
		},
		{
			name:      "a classification naming no language falls back, it does not fall silent",
			llmCase:   "classify-pick-up-dry-cleaning",
			wantLang:  classify.Fallback(),
			wantReply: "Noted.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 8, 27, 9, 30, 0, 0, time.UTC)
			embeddings := memrepo.NewEmbeddings()
			llm := fakeprovider.New(t, testdataLLMCasesDir(t), tt.llmCase)
			embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

			idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
			if err != nil {
				t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
			}
			svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, memrepo.NewUnits(),
				embeddings, memrepo.NewLexical(), memrepo.NewRelations(), memrepo.NewDecisionLog(),
				llm, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(),
				memrepo.NewTriggers(), memrepo.NewTimers())

			result, err := svc.Capture(ctx, brain.CaptureInput{Text: "acordate de comprar café", Channel: "chat"})
			if err != nil {
				t.Fatalf("Capture: %v", err)
			}

			// The language reaches the result at all — the step that
			// would silently be missing if the stamp were added to some
			// return paths and not others.
			if result.Language != tt.wantLang {
				t.Errorf("CaptureResult.Language = %q, want %q", result.Language, tt.wantLang)
			}

			if got := channels.RenderReply(result); got != tt.wantReply {
				t.Errorf("RenderReply = %q, want %q", got, tt.wantReply)
			}
		})
	}
}

// TestI26_TheGlassBoxStaysEnglish is ADR-0022's other half, and the one a
// reader is most likely to "fix" by mistake.
//
// decision_log serves an auditor, not the person who sent the message, and
// CLAUDE.md settles that audience's language. A rationale that followed
// the message would make the trail unreadable to anyone who does not speak
// every language its owner does — including a contributor reading a bug
// report.
func TestI26_TheGlassBoxStaysEnglish(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 27, 9, 30, 0, 0, time.UTC)
	decisions := memrepo.NewDecisionLog()
	embeddings := memrepo.NewEmbeddings()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), "classify-spanish-task")

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, memrepo.NewUnits(),
		embeddings, memrepo.NewLexical(), memrepo.NewRelations(), decisions,
		llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx),
		memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())

	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: "acordate de comprar café", Channel: "chat"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	rows, err := decisions.Since(ctx, now.Add(-time.Hour), -1)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no decision_log rows — a capture with no trail cannot prove anything about the trail's language")
	}
	for _, row := range rows {
		if row.Rationale == "" {
			t.Errorf("action %q has an empty rationale — doc 02 §11 requires a human-readable sentence", row.Action)
		}
		// The Spanish reply's own words must not have leaked into the
		// trail's prose. Content quoted from the message is a different
		// thing and is not what this checks: these are the rendered
		// sentences from internal/core/phrase.
		for _, spanish := range []string{"Anotado", "Encontré", "No lo programé"} {
			if strings.Contains(row.Rationale, spanish) {
				t.Errorf("action %q renders a person-facing Spanish sentence into the trail: %q", row.Action, row.Rationale)
			}
		}
	}
}

// TestI26_TheTrailRecordsWhatLanguageWasRead is the instrument, not the
// feature — and it exists because the feature shipped without it.
//
// `language` is optional (ADR-0022), so its absence is not a Degradation
// and leaves no other trace. The first live Spanish capture after that
// decision came back "Noted.", and the decision_log could not say whether
// the model had omitted the field, named something outside the
// vocabulary, or named "en". Three different facts about the provider,
// one indistinguishable row.
//
// It asserts the RAW reading rather than the resolved one. A stamped "en"
// and an absent language render the identical sentence and are different
// facts, which is exactly the distinction the trail exists to keep.
func TestI26_TheTrailRecordsWhatLanguageWasRead(t *testing.T) {
	tests := []struct {
		name    string
		llmCase string
		want    string
	}{
		{"a named language is recorded as itself", "classify-spanish-task", "es"},
		{"an absent language is recorded as empty, not as the fallback", "classify-pick-up-dry-cleaning", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 8, 27, 9, 30, 0, 0, time.UTC)
			decisions := memrepo.NewDecisionLog()
			embeddings := memrepo.NewEmbeddings()
			llm := fakeprovider.New(t, testdataLLMCasesDir(t), tt.llmCase)

			idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
			if err != nil {
				t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
			}
			svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, memrepo.NewUnits(),
				embeddings, memrepo.NewLexical(), memrepo.NewRelations(), decisions,
				llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx),
				memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers())

			if _, err := svc.Capture(ctx, brain.CaptureInput{Text: "acordate de comprar café", Channel: "chat"}); err != nil {
				t.Fatalf("Capture: %v", err)
			}

			rows, err := decisions.Since(ctx, now.Add(-time.Hour), -1)
			if err != nil {
				t.Fatalf("decisions.Since: %v", err)
			}

			found := false
			for _, row := range rows {
				if row.Action != ports.ActionCaptureClassify {
					continue
				}
				found = true
				var ctxValue struct {
					Language *string `json:"language"`
				}
				if err := json.Unmarshal(row.Context, &ctxValue); err != nil {
					t.Fatalf("decoding %s context %s: %v", row.Action, row.Context, err)
				}
				if ctxValue.Language == nil {
					t.Fatalf("%s context has no language key at all: %s — an absent key and an empty one are the same silence this test exists to end", row.Action, row.Context)
				}
				if *ctxValue.Language != tt.want {
					t.Errorf("%s context language = %q, want %q", row.Action, *ctxValue.Language, tt.want)
				}
			}
			if !found {
				t.Fatalf("no %s row was written", ports.ActionCaptureClassify)
			}
		})
	}
}
