package main

import (
	"slices"
	"testing"

	"github.com/rengo/nooma/internal/config"
)

// TestTasksM1ConsumesAreAllDocumented is design D18a's own second half: every
// task this binary starts consuming must be one config.DocumentedTaskNames
// already carries — a typo or an aspirational task name here would silently
// never resolve, the same defect checkTasks (internal/config/validate.go)
// exists to catch on the user's own tasks: entries, applied here to this
// binary's own list.
func TestTasksM1ConsumesAreAllDocumented(t *testing.T) {
	for _, task := range tasksM1Consumes {
		if !slices.Contains(config.DocumentedTaskNames, task) {
			t.Errorf("tasksM1Consumes contains %q, which config.DocumentedTaskNames does not carry", task)
		}
	}
}

// TestResolveTaskProvidersReadsTheSharedListNotACopy is design D18a's own
// first half, made structural rather than descriptive: this binary's
// wiring (resolveTaskProviders, wiring.go) is proven to iterate
// tasksM1Consumes ITSELF, not a hardcoded restatement of it. The proof:
// temporarily replace the package-level list with one member no M1 code
// path names, bind ONLY that member to a provider, and leave the real
// three (capture_processing, relation_evaluation, embedding) entirely
// unconfigured. A hardcoded copy would ignore the swap and fail to resolve
// (none of the real three tasks are bound in this config); reading the
// var live resolves successfully, because the one task it actually asked
// about — the swapped-in name — IS bound.
func TestResolveTaskProvidersReadsTheSharedListNotACopy(t *testing.T) {
	original := tasksM1Consumes
	t.Cleanup(func() { tasksM1Consumes = original })
	// "chat" is a real config.DocumentedTaskNames member with no M1
	// consumer (design D15) — deliberately distinct from all three of
	// tasksM1Consumes' own real members, so a hardcoded copy of the
	// original list cannot accidentally still satisfy this swap.
	tasksM1Consumes = []string{"chat"}

	cfg := &config.Config{
		Providers: map[string]config.Provider{"local": {Type: "ollama", Model: "test-model"}},
		Tasks:     map[string]config.TaskBinding{"chat": {Provider: "local"}},
	}

	_, _, _, _, ok := resolveTaskProviders(cfg, func(string) (string, bool) { return "", false })
	if !ok {
		t.Fatal("resolveTaskProviders did not resolve against the overridden tasksM1Consumes — it reads a hardcoded list instead of the package var")
	}
}

// TestResolveTaskProvidersFailsClosedWhenATaskIsUnbound pins the "all or
// nothing" half of resolveTaskProviders' own contract (wiring.go): a
// config binding only two of the three real tasks must not resolve a
// partial *brain.RecallService — RecallService.ScoredFor dereferences its
// own embed field unconditionally (it has no nil guard the way Candidates'
// vector argument does), so a nil ports.EmbeddingProvider reaching it is
// not a degraded leg, it is a nil-interface panic on the first request.
func TestResolveTaskProvidersFailsClosedWhenATaskIsUnbound(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"local": {Type: "ollama", Model: "test-model"}},
		Tasks: map[string]config.TaskBinding{
			"capture_processing":  {Provider: "local"},
			"relation_evaluation": {Provider: "local"},
			// embedding is deliberately left unbound.
		},
	}

	_, _, _, _, ok := resolveTaskProviders(cfg, func(string) (string, bool) { return "", false })
	if ok {
		t.Fatal("resolveTaskProviders resolved with embedding unbound — this must fail closed, not hand back a partial wiring")
	}
}

// TestBindTasksReadsTheSharedListNotACopy is design D18a's second reader,
// made structural the same way TestResolveTaskProvidersReadsTheSharedListNotACopy
// above already is for the first: temporarily replace tasksM1Consumes with
// one member no real path names, and confirm bindTasks (init.go's own
// wizard helper) binds exactly that member rather than a hardcoded
// three-name literal that would ignore the swap. Run once with the fix
// reverted to a hardcoded map — this test fails with exactly one binding
// missing, confirming the guard catches it.
func TestBindTasksReadsTheSharedListNotACopy(t *testing.T) {
	original := tasksM1Consumes
	t.Cleanup(func() { tasksM1Consumes = original })
	tasksM1Consumes = []string{"chat"}

	bindings := bindTasks("embed-entry", "chat-entry")

	if len(bindings) != 1 {
		t.Fatalf("bindTasks returned %d bindings, want exactly 1 for the swapped-in list: %+v", len(bindings), bindings)
	}
	if bindings["chat"] != "chat-entry" {
		t.Errorf(`bindTasks["chat"] = %q, want "chat-entry" — a hardcoded copy of the original three-task list would not have bound "chat" at all`, bindings["chat"])
	}
}

// TestResolveTaskProvidersFailsClosedWhenTheProviderCannotEmbed pins the
// other real gap this function exists to catch: anthropic.Client and
// openai.Client both implement only ports.LLMProvider today (PR 17 is what
// gives openai.Client an Embed method) — binding "embedding" to an
// anthropic-typed provider must fail closed, the same way an unbound task
// does, rather than handing NewRecallService a nil ports.EmbeddingProvider.
func TestResolveTaskProvidersFailsClosedWhenTheProviderCannotEmbed(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"local": {Type: "anthropic", Model: "test-model", APIKeyEnv: "TEST_KEY"}},
		Tasks: map[string]config.TaskBinding{
			"capture_processing":  {Provider: "local"},
			"relation_evaluation": {Provider: "local"},
			"embedding":           {Provider: "local"},
		},
	}

	_, _, _, _, ok := resolveTaskProviders(cfg, func(string) (string, bool) { return "test-key-value", true })
	if ok {
		t.Fatal("resolveTaskProviders resolved embedding against an anthropic-typed provider, which does not implement ports.EmbeddingProvider")
	}
}
