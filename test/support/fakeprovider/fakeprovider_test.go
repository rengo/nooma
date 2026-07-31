package fakeprovider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/goldenset"
)

// The EmbeddingProvider compile-time assertion lives in embed_test.go
// (task 5.5) — it is the red that task's own Embed implementation answers.
var _ ports.LLMProvider = (*fakeprovider.Fake)(nil)

// writeCase writes ex as dir/<ex.ID>.json, following testdata/llm/format.md's
// file-naming convention (test/support/goldenset). No test in this file
// depends on testdata/llm/cases/ — that corpus arrives in PR 5b — so every
// case here is its own throwaway fixture under t.TempDir(), exercised
// through fakeprovider.New's real loader path (goldenset.Load).
func writeCase(t *testing.T, dir string, ex goldenset.LLMExample) {
	t.Helper()
	data, err := json.Marshal(ex)
	if err != nil {
		t.Fatalf("marshal case %q: %v", ex.ID, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ex.ID+".json"), data, 0o644); err != nil {
		t.Fatalf("write case %q: %v", ex.ID, err)
	}
}

func TestFake_ReplaysScriptedCasesInOrder(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, goldenset.LLMExample{
		ID: "case-a", Provider: "anthropic", Model: "claude-sonnet", Task: "classify",
		Prompt: "recorded prompt a", Response: "response a",
	})
	writeCase(t, dir, goldenset.LLMExample{
		ID: "case-b", Provider: "anthropic", Model: "claude-sonnet", Task: "classify",
		Prompt: "recorded prompt b", Response: "response b",
	})

	f := fakeprovider.New(t, dir, "case-a", "case-b")

	got1, err := f.Complete(context.Background(), ports.LLMRequest{Prompt: "live prompt a", Task: "classify"})
	if err != nil {
		t.Fatalf("Complete(case-a): %v", err)
	}
	if got1.Text != "response a" || got1.Model != "claude-sonnet" {
		t.Fatalf("Complete(case-a) = %+v, want text %q model %q", got1, "response a", "claude-sonnet")
	}

	got2, err := f.Complete(context.Background(), ports.LLMRequest{Prompt: "live prompt b", Task: "classify"})
	if err != nil {
		t.Fatalf("Complete(case-b): %v", err)
	}
	if got2.Text != "response b" {
		t.Fatalf("Complete(case-b) = %+v, want text %q", got2, "response b")
	}
}

func TestFake_SelectsByIDNeverByPromptText(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, goldenset.LLMExample{
		ID: "case-x", Provider: "anthropic", Model: "claude-sonnet", Task: "classify",
		Prompt: "the prompt recorded when this case was captured", Response: "the recorded response",
	})

	f := fakeprovider.New(t, dir, "case-x")

	liveCallPrompt := "a completely different live prompt — different beliefs, different clock reading"
	got, err := f.Complete(context.Background(), ports.LLMRequest{Prompt: liveCallPrompt, Task: "classify"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.Text != "the recorded response" {
		t.Fatalf("replay was affected by the live/recorded prompt mismatch: got %q", got.Text)
	}

	seen := f.SeenPrompts()
	if len(seen) != 1 || seen[0] != liveCallPrompt {
		t.Fatalf("SeenPrompts() = %v, want [%q] — the live prompt, not the recording's own", seen, liveCallPrompt)
	}
}

func TestFake_RecordedErrorSurfacesAsGoError(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, goldenset.LLMExample{
		ID: "case-err", Provider: "anthropic", Model: "claude-sonnet", Task: "classify",
		Prompt: "prompt", Error: "rate limited",
	})

	f := fakeprovider.New(t, dir, "case-err")

	got, err := f.Complete(context.Background(), ports.LLMRequest{Prompt: "prompt", Task: "classify"})
	if err == nil {
		t.Fatalf("Complete returned a nil error for a recorded error case; response was %+v", got)
	}
	if got.Text != "" {
		t.Fatalf("Complete returned a non-empty successful response alongside an error: %+v", got)
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("error = %q, want it to name the recorded error text", err.Error())
	}
}

// spyT is a minimal fakeprovider.TestingT double. It exists to observe
// Fake's own failure behavior (the unscripted-call and under-run
// properties design D7 names) without ending this file's own test run — a
// real *testing.T's Fatalf halts the calling goroutine, which is exactly
// the behavior under test below, so a real *testing.T cannot also be the
// thing asserting on it.
type spyT struct {
	fatalMsgs []string
	errorMsgs []string
	cleanups  []func()
}

func (s *spyT) Helper()          {}
func (s *spyT) Cleanup(f func()) { s.cleanups = append(s.cleanups, f) }
func (s *spyT) Errorf(format string, args ...any) {
	s.errorMsgs = append(s.errorMsgs, fmt.Sprintf(format, args...))
}
func (s *spyT) Fatalf(format string, args ...any) {
	s.fatalMsgs = append(s.fatalMsgs, fmt.Sprintf(format, args...))
}

func TestFake_UnscriptedExtraCallFailsImmediately(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, goldenset.LLMExample{
		ID: "case-a", Provider: "anthropic", Model: "claude-sonnet", Task: "classify",
		Prompt: "p", Response: "r",
	})

	spy := &spyT{}
	f := fakeprovider.New(spy, dir, "case-a")

	if _, err := f.Complete(context.Background(), ports.LLMRequest{Prompt: "p", Task: "classify"}); err != nil {
		t.Fatalf("scripted call: %v", err)
	}
	if len(spy.fatalMsgs) != 0 {
		t.Fatalf("spy.Fatalf called before any unscripted call: %v", spy.fatalMsgs)
	}

	got, err := f.Complete(context.Background(), ports.LLMRequest{Prompt: "extra", Task: "classify"})
	if len(spy.fatalMsgs) != 1 {
		t.Fatalf("unscripted Complete call did not fail t immediately: fatalMsgs=%v response=%+v err=%v", spy.fatalMsgs, got, err)
	}
}

func TestFake_UnderRunFailsAtCleanup(t *testing.T) {
	dir := t.TempDir()
	writeCase(t, dir, goldenset.LLMExample{
		ID: "case-a", Provider: "anthropic", Model: "claude-sonnet", Task: "classify",
		Prompt: "p", Response: "r",
	})

	spy := &spyT{}
	f := fakeprovider.New(spy, dir, "case-a", "case-b")

	if _, err := f.Complete(context.Background(), ports.LLMRequest{Prompt: "p", Task: "classify"}); err != nil {
		t.Fatalf("scripted call: %v", err)
	}
	if len(spy.errorMsgs) != 0 {
		t.Fatalf("cleanup ran before it was invoked: %v", spy.errorMsgs)
	}

	for _, c := range spy.cleanups {
		c()
	}

	if len(spy.errorMsgs) != 1 {
		t.Fatalf("scripting more calls than the pipeline made did not fail at cleanup: errorMsgs=%v", spy.errorMsgs)
	}
}
