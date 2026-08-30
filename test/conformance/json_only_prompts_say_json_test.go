// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/consolidation"
	"github.com/rengo/nooma/internal/core/unit"
)

// TestJSONOnlyPromptsSayJSON pins a coupling between two files that never
// mention each other, and whose disagreement is silent.
//
// OpenAI refuses `response_format: {"type":"json_object"}` unless the word
// "json" appears somewhere in the messages, and answers 400. Worse than the
// 400: without that word the model is documented to emit an unending stream
// of whitespace until the token budget is exhausted — a request that costs
// the full context window and returns nothing.
//
// So every prompt sent with ports.LLMRequest.JSONOnly owes the adapter that
// word. Today all three say "Answer with one JSON object and nothing else",
// and they say it because a human wanted the model to behave, not because
// anything required it. Reword that line for clarity — an entirely
// reasonable thing to do — and the provider starts failing in a way that
// points at neither file.
//
// The assertion is case-insensitive and asks only for the substring, which
// is exactly what the vendor checks. It deliberately does not pin the
// sentence: the requirement is that the word survives, not that the wording
// is frozen.
func TestJSONOnlyPromptsSayJSON(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC)

	// One entry per call site that sets JSONOnly. A prompt builder added
	// later without a row here is not covered — which is why the count is
	// asserted below, against the number of JSON-parsing tasks the brain
	// actually has.
	prompts := map[string]string{
		"classify (capture_processing)": classify.BuildPrompt("pick up the dry cleaning", nil, now, consolidation.DefaultWeightThreshold),
		"judge (relation_evaluation)": brain.JudgePrompt(
			unit.Unit{ID: "u1", Content: "the new one"},
			[]unit.Unit{{ID: "u2", Content: "a candidate"}},
		),
		"derive (belief_derivation)": consolidation.BuildDerivePrompt(
			[]consolidation.DeriveSource{{UnitID: "u1", Content: "went running again"}}, nil,
		),
	}

	if len(prompts) != 3 {
		t.Fatalf("this test covers %d prompts; the brain has three JSON-parsing tasks "+
			"(capture_processing, relation_evaluation, belief_derivation) and each one's "+
			"builder needs a row here", len(prompts))
	}

	for name, prompt := range prompts {
		if !strings.Contains(strings.ToLower(prompt), "json") {
			t.Errorf("the %s prompt never says \"json\", so a provider asked for JSON mode "+
				"rejects it — and OpenAI's documented failure mode without that word is an "+
				"unending stream of whitespace, not an error a caller can read:\n%s", name, prompt)
		}
	}
}
