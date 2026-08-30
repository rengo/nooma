package classify

import (
	"strings"
	"testing"
	"time"
)

// TestAllLanguages_IsTheClosedVocabulary pins the list itself. It is not a
// tautology: AllLanguages is what the decoder matches "language" against,
// so a member added here without a sentence written for it is a value the
// renderer can receive and cannot answer.
func TestAllLanguages_IsTheClosedVocabulary(t *testing.T) {
	got := AllLanguages()
	want := []Language{LanguageEN, LanguageES}

	if len(got) != len(want) {
		t.Fatalf("AllLanguages() has %d members, want %d: %v", len(got), len(want), got)
	}
	for i, l := range want {
		if got[i] != l {
			t.Errorf("AllLanguages()[%d] = %q, want %q", i, got[i], l)
		}
	}

	// A fresh slice, not a shared one — mutating the result must not
	// change what the next caller sees.
	got[0] = "mutated"
	if AllLanguages()[0] != LanguageEN {
		t.Error("AllLanguages() returns a shared slice — a caller mutated the vocabulary")
	}
}

// TestLanguageOr_NilIsTheOrdinaryCase is the whole point of the method. An
// absent language is what most classifications will carry until every
// provider reliably emits the field, so nil has to be a rendering
// instruction rather than something each caller nil-checks its own way.
func TestLanguageOr_NilIsTheOrdinaryCase(t *testing.T) {
	var absent *Language
	if got := absent.Or(); got != Fallback() {
		t.Errorf("(*Language)(nil).Or() = %q, want %q", got, Fallback())
	}

	for _, l := range AllLanguages() {
		named := l
		if got := (&named).Or(); got != l {
			t.Errorf("(&%q).Or() = %q, want %q — a named language must survive", l, got, l)
		}
	}
}

// TestFallback_IsEnglishAndReadsNothing guards the decision ADR-0022 makes
// explicitly. A fallback that consulted configuration would reintroduce
// the per-vault setting on exactly the path where the classifier already
// failed, and it would do it invisibly.
func TestFallback_IsEnglishAndReadsNothing(t *testing.T) {
	if Fallback() != LanguageEN {
		t.Errorf("Fallback() = %q, want %q — English is the one language every fixed sentence exists in", Fallback(), LanguageEN)
	}
	// That it reads nothing is not asserted here, because a Go test
	// cannot prove it: calling it twice only shows it is deterministic.
	// The guarantee is the lint over internal/core — forbidigo denies
	// time.Now and os.Getenv, depguard denies internal/config — which is
	// a stronger check than any assertion in this file could be.
}

// TestDecode_Language covers the decoder row, including the case that
// makes the field optional: an out-of-vocabulary value degrades to null
// rather than failing the classification (I14).
func TestDecode_Language(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	base := `"type":"chitchat","normalized_content":"hola","weight":0.1,"decay_rate":0.5`

	tests := []struct {
		name  string
		field string
		want  Language
		// degraded says the response named a language and the decoder
		// refused it, which must be recorded rather than silently dropped.
		degraded bool
	}{
		{"spanish survives", `,"language":"es"`, LanguageES, false},
		{"english survives", `,"language":"en"`, LanguageEN, false},
		{"absent falls back", ``, Fallback(), false},
		{"out of vocabulary degrades and falls back", `,"language":"fr"`, Fallback(), true},
		{"wrong type degrades and falls back", `,"language":7`, Fallback(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Decode("{"+base+tt.field+"}", now)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got := c.Language.Or(); got != tt.want {
				t.Errorf("Language.Or() = %q, want %q", got, tt.want)
			}

			named := false
			for _, d := range c.Degradations {
				if d.Field == "language" {
					named = true
				}
			}
			if named != tt.degraded {
				t.Errorf("a %q degradation was recorded = %v, want %v — I14 requires the loss be reported, not just absorbed: %+v",
					"language", named, tt.degraded, c.Degradations)
			}
		})
	}
}

// TestBuildPrompt_AsksForTheMessagesLanguage guards the one line that
// makes the field answerable. The model is reading two languages at once
// here — these English instructions, and often a message in something else
// — so a prompt that merely named the field would be asking an ambiguous
// question, and the ambiguity resolves toward the instructions.
func TestBuildPrompt_AsksForTheMessagesLanguage(t *testing.T) {
	got := BuildPrompt("hola, todo bien?", nil, time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC), 0.5)

	// **Where it is asked, not merely that it is asked.** The first
	// version of this test checked only that the word appeared, and
	// passed while the field sat under "Optional fields — omit any that
	// do not apply" — where a real model read the instruction, obeyed it,
	// and answered a Spanish message in English. A test that cannot tell
	// those two prompts apart is not testing the thing that broke.
	required, _, found := strings.Cut(got, "Optional fields")
	if !found {
		t.Fatalf("BuildPrompt has no optional-fields section to divide on:\n%s", got)
	}
	if !strings.Contains(required, "language") {
		t.Errorf("language is not among the required fields — every message is written in some language, so it always applies:\n%s", got)
	}
	// Whitespace-normalised before matching: the prompt is hard-wrapped
	// for the model to read, and a clause that moves across a line break
	// is the same instruction. A test that broke on rewrapping would
	// train its next reader to weaken it rather than fix the prompt.
	flat := strings.Join(strings.Fields(got), " ")
	if !strings.Contains(flat, "not the language of these instructions") {
		t.Errorf("BuildPrompt does not distinguish the message's language from the prompt's — the model has both in front of it:\n%s", got)
	}
	for _, l := range AllLanguages() {
		if !strings.Contains(got, string(l)) {
			t.Errorf("BuildPrompt does not offer %q as a value — a vocabulary the model is never shown is one it cannot answer with", l)
		}
	}
}
