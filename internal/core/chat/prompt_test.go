package chat

import (
	"strings"
	"testing"
)

// TestBuildPrompt_CarriesTheMessageVerbatim is the property the whole path
// exists for. The reply comes back in the sender's language because the
// model is holding the sender's words — not a translation of them, not a
// normalization of them, and not a description of them.
func TestBuildPrompt_CarriesTheMessageVerbatim(t *testing.T) {
	messages := []string{
		"hola, todo bien?",
		"good morning!",
		"ça va ?",
		"元気ですか",
	}

	for _, m := range messages {
		got := BuildPrompt(m)
		if !strings.Contains(got, m) {
			t.Errorf("BuildPrompt(%q) does not contain the message:\n%s", m, got)
		}
	}
}

// TestBuildPrompt_IsPure pins what makes this function core code at all:
// same argument, same string, with no clock, no environment and no vault
// behind it (docs/06-harness.md §1).
func TestBuildPrompt_IsPure(t *testing.T) {
	first := BuildPrompt("hola")
	second := BuildPrompt("hola")
	if first != second {
		t.Errorf("BuildPrompt is not pure:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestBuildPrompt_StatesTheLanguageRule guards the one line ADR-0021 is
// actually buying. Nooma has no locale setting anywhere, so if this
// instruction goes missing the model answers in whatever language the rest
// of the prompt is written in — English — and the failure looks exactly
// like the fixed sentence this path replaced. That is a regression no test
// asserting "some non-empty prompt" would catch.
func TestBuildPrompt_StatesTheLanguageRule(t *testing.T) {
	got := BuildPrompt("hola")
	if !strings.Contains(got, "same language") {
		t.Errorf("BuildPrompt does not instruct the model to answer in the message's own language:\n%s", got)
	}
}

// TestBuildPrompt_RefusesToPromiseWhatNoomaCannotDo guards the other half.
// The classifier decides what reaches this prompt and will sometimes be
// wrong: an out_of_scope request read as small talk lands here, where a
// model with no tools answers "I'll check the weather for you". The prompt
// naming what Nooma cannot reach is what keeps that from being a promise.
func TestBuildPrompt_RefusesToPromiseWhatNoomaCannotDo(t *testing.T) {
	got := BuildPrompt("what's the weather like?")
	for _, want := range []string{"cannot browse", "say so plainly"} {
		if !strings.Contains(got, want) {
			t.Errorf("BuildPrompt does not carry %q — nothing stops the model promising a capability nobody built:\n%s", want, got)
		}
	}
}

// TestBuildPrompt_CarriesNoVaultContent is ADR-0021's cost decision stated
// as a test. A chitchat is the kind that had nothing worth keeping in it;
// paying for recall to decorate a greeting is the thing this path
// deliberately does not do. If a later milestone wants a conversation that
// remembers, it adds a parameter — and this test is what makes that an
// explicit act rather than a quiet one.
func TestBuildPrompt_CarriesNoVaultContent(t *testing.T) {
	got := BuildPrompt("hola")
	for _, absent := range []string{"Beliefs", "Candidates", "Recall", "Context"} {
		if strings.Contains(got, absent) {
			t.Errorf("BuildPrompt carries a %q section — this prompt is deliberately thin:\n%s", absent, got)
		}
	}
}
