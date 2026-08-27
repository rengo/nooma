package main

import (
	"slices"
	"strings"
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
// (none of the real tasks are bound in this config); reading the
// var live resolves successfully, because the one task it actually asked
// about — the swapped-in name — IS bound.
func TestResolveTaskProvidersReadsTheSharedListNotACopy(t *testing.T) {
	original := tasksM1Consumes
	t.Cleanup(func() { tasksM1Consumes = original })
	// "image_description" is a real config.DocumentedTaskNames member with
	// no consumer — deliberately distinct from every one of
	// tasksM1Consumes' own real members, so a hardcoded copy of the
	// original list cannot accidentally still satisfy this swap. This
	// stand-in used to be "chat"; ADR-0021 gave "chat" a consumer and put
	// it in the real list, which is exactly what disqualifies it here.
	tasksM1Consumes = []string{"image_description"}

	cfg := &config.Config{
		Providers: map[string]config.Provider{"local": {Type: "ollama", Model: "test-model"}},
		Tasks:     map[string]config.TaskBinding{"image_description": {Provider: "local"}},
	}

	_, _, _, _, _, ok := resolveTaskProviders(cfg, func(string) (string, bool) { return "", false })
	if !ok {
		t.Fatal("resolveTaskProviders did not resolve against the overridden tasksM1Consumes — it reads a hardcoded list instead of the package var")
	}
}

// TestResolveTaskProvidersFailsClosedWhenATaskIsUnbound pins the "all or
// nothing" half of resolveTaskProviders' own contract (wiring.go): a
// config binding only some of the real tasks must not resolve a
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
			"chat":                {Provider: "local"},
			// embedding is deliberately left unbound.
		},
	}

	_, _, _, _, _, ok := resolveTaskProviders(cfg, func(string) (string, bool) { return "", false })
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
	tasksM1Consumes = []string{"image_description"}

	bindings := bindTasks("embed-entry", "chat-entry")

	// The liveness claim is unchanged: swapping the var must change what
	// bindTasks produces. What moved is the count — bindTasks now reads the
	// union of both task lists, so the swapped-in member arrives ALONGSIDE
	// tasksConsolidateConsumes rather than instead of everything.
	if len(bindings) != len(tasksTheBinaryRuns()) {
		t.Fatalf("bindTasks returned %d bindings for a union of %d: %+v",
			len(bindings), len(tasksTheBinaryRuns()), bindings)
	}
	if bindings["image_description"] != "chat-entry" {
		t.Errorf(`bindTasks["image_description"] = %q, want "chat-entry" — a hardcoded copy of the original task list would not have bound "image_description" at all`, bindings["image_description"])
	}
	if _, bound := bindings["capture_processing"]; bound {
		t.Error(`bindTasks bound "capture_processing", which the swapped-in list no longer names — it is reading a hardcoded copy`)
	}
}

// TestCheckTaskCoverageReadsTheSharedListNotACopy is design D18a's third
// reader (16b), made structural the same way the two tests above already
// are for the first two: temporarily replace tasksM1Consumes with one
// member no real path names, bind the REAL tasks (so a hardcoded copy
// of the original list would report every one of them satisfied) and leave
// the swapped-in member "image_description" deliberately unbound. A
// hardcoded copy would report ok (its own tasks are all bound); reading the
// var live reports the swapped-in task, "image_description", as unbound
// instead.
func TestCheckTaskCoverageReadsTheSharedListNotACopy(t *testing.T) {
	original := tasksM1Consumes
	t.Cleanup(func() { tasksM1Consumes = original })
	tasksM1Consumes = []string{"image_description"}

	cfg := &config.Config{
		Providers: map[string]config.Provider{"local": {Type: "ollama", Model: "test-model"}},
		Tasks: map[string]config.TaskBinding{
			"capture_processing":  {Provider: "local"},
			"relation_evaluation": {Provider: "local"},
			"chat":                {Provider: "local"},
			"embedding":           {Provider: "local"},
			// "image_description" — the swapped-in list's only member —
			// deliberately left unbound.
		},
	}

	err := checkTaskCoverage("", cfg)
	if err == nil {
		t.Fatal("checkTaskCoverage reported ok with the swapped-in task \"image_description\" left unbound — it reads a hardcoded copy of the original task list instead of the package var")
	}
	if !strings.Contains(err.Error(), "image_description") {
		t.Errorf("error %q does not name %q, the swapped-in task", err.Error(), "image_description")
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

	_, _, _, _, _, ok := resolveTaskProviders(cfg, func(string) (string, bool) { return "test-key-value", true })
	if ok {
		t.Fatal("resolveTaskProviders resolved embedding against an anthropic-typed provider, which does not implement ports.EmbeddingProvider")
	}
}

// TestTasksTheBinaryRunsIsTheUnionOfBothLists is design D18a applied to the
// gap between the two lists, rather than within each of them.
//
// tasksM1Consumes is what capture and recall need; tasksConsolidateConsumes
// is what one consolidation pass needs. Both were already read live by
// their own consumers, and neither list was wrong. What nobody owned was
// their UNION — so `nooma init` bound M1's three tasks, the scheduler
// needed belief_derivation, and the wizard produced a vault whose sleep
// phase could not start.
//
// Mutation: return tasksM1Consumes and this fails on belief_derivation.
func TestTasksTheBinaryRunsIsTheUnionOfBothLists(t *testing.T) {
	got := map[string]bool{}
	for _, task := range tasksTheBinaryRuns() {
		if got[task] {
			t.Errorf("task %q appears twice — the union is a set, and a caller building a "+
				"map from it would silently absorb the duplicate", task)
		}
		got[task] = true
	}

	for _, list := range [][]string{tasksM1Consumes, tasksConsolidateConsumes} {
		for _, task := range list {
			if !got[task] {
				t.Errorf("task %q is consumed by the binary and missing from the union — a "+
					"vault the wizard writes would leave it unbound", task)
			}
		}
	}
	if len(got) != len(tasksTheBinaryRuns()) {
		t.Errorf("the union has %d distinct members across %d entries", len(got), len(tasksTheBinaryRuns()))
	}
}

// TestBindTasksBindsEveryTaskTheBinaryRuns is the wizard half of the
// defect, and it is stated as the property rather than as a list of four
// names: what the wizard owes a new vault is that nothing the binary runs
// is left unbound, whatever that set becomes.
//
// Reported by a maintainer starting `nooma serve` on a freshly-initialised
// vault and getting "scheduler: consolidation not scheduled: task
// belief_derivation has no provider bound" — a message that is correct,
// actionable, and should never have been reachable from the wizard's own
// output.
//
// Mutation: bind only tasksM1Consumes and belief_derivation fails.
func TestBindTasksBindsEveryTaskTheBinaryRuns(t *testing.T) {
	bindings := bindTasks("embed-entry", "chat-entry")

	for _, task := range tasksTheBinaryRuns() {
		provider, bound := bindings[task]
		if !bound {
			t.Errorf("bindTasks left %q unbound — a wizard-written vault cannot run it", task)
			continue
		}
		if provider == "" {
			t.Errorf("bindTasks bound %q to an empty provider name", task)
		}
	}

	// embedding is the one task that must not take the chat entry: a chat
	// model is not an embedding model (design D15).
	if bindings["embedding"] != "embed-entry" {
		t.Errorf(`bindTasks["embedding"] = %q, want the embedding entry`, bindings["embedding"])
	}
	if bindings["belief_derivation"] != "chat-entry" {
		t.Errorf(`bindTasks["belief_derivation"] = %q, want the chat entry`, bindings["belief_derivation"])
	}
}

// TestCheckTaskCoverageCoversEveryTaskTheBinaryRuns is the doctor half.
//
// doctor reported "ok task coverage" on the very vault whose scheduler
// refused to start, because the check was scoped to M1's three tasks. A
// health check that passes a vault the binary cannot fully run is worse
// than no check: it is a check that says the opposite of the truth.
//
// Mutation: scope the check back to tasksM1Consumes and this fails.
func TestCheckTaskCoverageCoversEveryTaskTheBinaryRuns(t *testing.T) {
	// Every task bound EXCEPT belief_derivation — exactly the vault
	// `nooma init` used to write.
	tasks := map[string]config.TaskBinding{}
	for _, task := range tasksM1Consumes {
		tasks[task] = config.TaskBinding{Provider: "local"}
	}

	cfg := &config.Config{
		Providers: map[string]config.Provider{"local": {Type: "ollama", Model: "test-model"}},
		Tasks:     tasks,
	}

	err := checkTaskCoverage("", cfg)
	if err == nil {
		t.Fatal("checkTaskCoverage reported ok for a vault with belief_derivation unbound — " +
			"that is the vault the wizard wrote, and its scheduler refuses to start")
	}
	if !strings.Contains(err.Error(), "belief_derivation") {
		t.Errorf("error %q does not name belief_derivation", err.Error())
	}
}

// TestRenderProvidersWritesEveryTaskTheBinaryRuns asserts on the YAML the
// wizard actually writes, not on the bindings it computes.
//
// Those are different claims, and the difference is this defect's whole
// shape. bindTasks was fixed to return the union and its own test went
// green while `nooma init` kept writing three tasks — because
// renderProviders iterates the task list AGAIN, twice, and was still
// reading M1's. A test on the function that computes a value cannot notice
// that the function which writes it out drops half.
//
// Found by running the real command against a temp vault and reading the
// file, after the unit test said everything was fine.
//
// Mutation: point either loop in renderProviders back at tasksM1Consumes
// and this fails on belief_derivation.
func TestRenderProvidersWritesEveryTaskTheBinaryRuns(t *testing.T) {
	choices, bindings := promptProviderSetup(strings.NewReader("1\n\n"), &strings.Builder{})
	yml := renderProviders(choices, bindings)

	for _, task := range tasksTheBinaryRuns() {
		if !strings.Contains(yml, task+":") {
			t.Errorf("the generated tasks: block never names %q, so a wizard-written vault "+
				"leaves it unbound:\n%s", task, yml)
		}
	}

	// And the block must be decodable and complete as config, not merely
	// contain the right substrings — the same posture
	// TestWizardPopulatedVaultDecodesAndValidates already takes.
	cfg, err := config.Decode(strings.NewReader(defaultConfig(yml)))
	if err != nil {
		t.Fatalf("the generated config does not decode: %v\n%s", err, yml)
	}
	cfg.ApplyDefaults()
	for _, task := range tasksTheBinaryRuns() {
		binding, bound := cfg.Tasks[task]
		if !bound {
			t.Errorf("task %q is not bound in the decoded config", task)
			continue
		}
		if _, present := cfg.Providers[binding.Provider]; !present {
			t.Errorf("task %q binds provider %q, which the config does not declare", task, binding.Provider)
		}
	}
}

// TestTaskCoverageNamesTheRightConsequencePerTask: widening the check
// without widening its message made the message wrong.
//
// An unbound capture_processing answers 503 on POST /capture. An unbound
// belief_derivation does not — capture and recall work fine, and what
// stops is the consolidation pass, on a line printed to the serve console
// and nowhere else. Telling a user their captures will 503 when they will
// not sends them to debug the wrong half of the binary.
//
// This is R5.4's Refinement 2 applied one layer out: "a formatting failure
// and a vocabulary failure call for different advice, and a gate that
// merges them throws away the distinction". Same here, for two failures
// that share a check.
//
// Mutation: return one consequence for every task and this fails.
func TestTaskCoverageNamesTheRightConsequencePerTask(t *testing.T) {
	providers := map[string]config.Provider{"local": {Type: "ollama", Model: "m"}}

	bindAllBut := func(missing string) *config.Config {
		tasks := map[string]config.TaskBinding{}
		for _, task := range tasksTheBinaryRuns() {
			if task != missing {
				tasks[task] = config.TaskBinding{Provider: "local"}
			}
		}
		return &config.Config{Providers: providers, Tasks: tasks}
	}

	t.Run("an M1 task names capture and recall", func(t *testing.T) {
		got := checkTaskCoverage("", bindAllBut("capture_processing"))
		if got == nil {
			t.Fatal("checkTaskCoverage reported ok with capture_processing unbound")
		}
		if !strings.Contains(got.Error(), "503") {
			t.Errorf("message %q does not mention the 503 that capture and recall answer", got.Error())
		}
	})

	t.Run("a consolidation-only task names consolidation", func(t *testing.T) {
		got := checkTaskCoverage("", bindAllBut("belief_derivation"))
		if got == nil {
			t.Fatal("checkTaskCoverage reported ok with belief_derivation unbound")
		}
		if strings.Contains(got.Error(), "503") {
			t.Errorf("message %q claims capture and recall will answer 503, which they will "+
				"not — an unbound belief_derivation stops the consolidation pass and nothing "+
				"else: %q", "503", got.Error())
		}
		if !strings.Contains(got.Error(), "consolidat") {
			t.Errorf("message %q never mentions consolidation, which is what actually stops",
				got.Error())
		}
	})
}
