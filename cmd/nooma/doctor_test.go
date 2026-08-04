package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/config"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/goldenset"
	"github.com/rengo/nooma/testdata/llm"
)

// TestDoctorChecksGainsOneNewEntry is spec R5.1 (16a) and design D18b (16b):
// doctorChecks gains new rows, following the existing {name, run} shape,
// and every existing check keeps its own name and order — 16a added one row
// ("llm quality"), 16b adds two more ("task coverage", "vault coverage"),
// and none of this is a rewrite of runDoctor's own loop.
func TestDoctorChecksGainsOneNewEntry(t *testing.T) {
	want := []string{
		"configuration", "permissions", "database integrity", "schema version", "bind",
		"llm quality", "task coverage", "vault coverage",
	}
	if len(doctorChecks) != len(want) {
		t.Fatalf("doctorChecks has %d entries, want %d: %v", len(doctorChecks), len(want), doctorChecks)
	}
	for i, name := range want {
		if doctorChecks[i].name != name {
			t.Errorf("doctorChecks[%d].name = %q, want %q — an existing check's name or order changed", i, doctorChecks[i].name, name)
		}
	}
}

// TestRunLLMQualityCheck is the gate's own decision logic (spec R5.2, R5.3,
// R5.4), proven against a scripted ports.LLMProvider — no test in this
// codebase calls a real provider (spec R5.5).
func TestRunLLMQualityCheck(t *testing.T) {
	dir := testdataLLMCasesDir(t)
	load := func(id string) llm.Case { return loadCase(t, dir, id) }

	clean := load("classify-pick-up-dry-cleaning")
	optionalAbsent := load("classify-chitchat-hello")
	truncated := load("classify-truncated-response")
	wrongType := load("classify-wrong-typed-field")
	unknownEnum := load("classify-unknown-enum-value")
	fenced := load("classify-fenced-response")
	relationClean := load("relation-duplicate-high-confidence")

	tests := []struct {
		name    string
		scripts map[string][]string // task -> scripted case ids, one Fake per task
		cases   []llm.Case
		want    []taskQualityResult
	}{
		{
			name:    "clean pass",
			scripts: map[string][]string{"capture_processing": {clean.ID}},
			cases:   []llm.Case{clean},
			want:    []taskQualityResult{{task: "capture_processing", total: 1, clean: 1}},
		},
		{
			name:    "optional field absence is still a clean pass (Refinement 1)",
			scripts: map[string][]string{"capture_processing": {optionalAbsent.ID}},
			cases:   []llm.Case{optionalAbsent},
			want:    []taskQualityResult{{task: "capture_processing", total: 1, clean: 1}},
		},
		{
			name:    "truncated response is a formatting failure",
			scripts: map[string][]string{"capture_processing": {truncated.ID}},
			cases:   []llm.Case{truncated},
			want: []taskQualityResult{{
				task: "capture_processing", total: 1,
				failures: []llmQualityFailure{{caseID: truncated.ID, field: "decay_rate", reason: string(classify.ReasonTruncated)}},
			}},
		},
		{
			name:    "a wrong-typed field is a formatting failure",
			scripts: map[string][]string{"capture_processing": {wrongType.ID}},
			cases:   []llm.Case{wrongType},
			want: []taskQualityResult{{
				task: "capture_processing", total: 1,
				failures: []llmQualityFailure{{caseID: wrongType.ID, field: "nudge_outcome", reason: string(classify.ReasonWrongType)}},
			}},
		},
		{
			name:    "an unknown enum value is a vocabulary failure (Refinement 2)",
			scripts: map[string][]string{"capture_processing": {unknownEnum.ID}},
			cases:   []llm.Case{unknownEnum},
			want: []taskQualityResult{{
				task: "capture_processing", total: 1,
				failures: []llmQualityFailure{{caseID: unknownEnum.ID, field: "type", reason: string(classify.ReasonUnknownEnum)}},
			}},
		},
		{
			// The production defect this test guards against: a live
			// OpenAI key returned exactly this shape for every one of 20
			// prompts, and the pre-fix decoder salvaged zero fields from a
			// markdown-fenced object, reporting "(response) wrong_type" for
			// all 20 — never a "k of n" line at all. This is the case
			// checkLLMQuality must now grade clean.
			name:    "clean pass through a markdown-fenced response",
			scripts: map[string][]string{"capture_processing": {fenced.ID}},
			cases:   []llm.Case{fenced},
			want:    []taskQualityResult{{task: "capture_processing", total: 1, clean: 1}},
		},
		{
			name: "one task fails, a different task passes — reported separately (R5.2)",
			scripts: map[string][]string{
				"capture_processing":  {wrongType.ID},
				"relation_evaluation": {relationClean.ID},
			},
			cases: []llm.Case{wrongType, relationClean},
			want: []taskQualityResult{
				{
					task: "capture_processing", total: 1,
					failures: []llmQualityFailure{{caseID: wrongType.ID, field: "nudge_outcome", reason: string(classify.ReasonWrongType)}},
				},
				{task: "relation_evaluation", total: 1, clean: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := map[string]ports.LLMProvider{}
			for task, ids := range tt.scripts {
				providers[task] = fakeprovider.New(t, dir, ids...)
			}

			got := runLLMQualityCheck(context.Background(), providers, tt.cases, time.Now(), qualityGateTimeout)
			assertTaskResults(t, got, tt.want)
		})
	}
}

// TestCheckLLMQuality_SendsTheCorpusPromptVerbatimOnce is spec R5.3 (the
// gate's own live request equals the corpus case's prompt field, never its
// response/expected) and spec R5.4 ("each corpus prompt is sent once…
// never retried"). fakeprovider's own Cleanup hook already fails this test
// if runLLMQualityCheck asked for a second scripted case beyond the one
// given — that is exactly what a retry would do.
func TestCheckLLMQuality_SendsTheCorpusPromptVerbatimOnce(t *testing.T) {
	dir := testdataLLMCasesDir(t)
	c := loadCase(t, dir, "classify-pick-up-dry-cleaning")
	fake := fakeprovider.New(t, dir, c.ID)

	runLLMQualityCheck(context.Background(), map[string]ports.LLMProvider{"capture_processing": fake}, []llm.Case{c}, time.Now(), qualityGateTimeout)

	seen := fake.SeenPrompts()
	if len(seen) != 1 {
		t.Fatalf("provider saw %d Complete call(s) for one corpus case, want exactly 1 — spec R5.4 forbids a retry", len(seen))
	}
	if seen[0] != c.Prompt {
		t.Errorf("sent prompt %q, want the corpus case's own prompt field %q verbatim — spec R5.3", seen[0], c.Prompt)
	}
}

// TestQualityGateErrorNamesEachFailingTaskSeparately is spec R5.2's own
// MUST NOT at the level a user actually reads it: checkLLMQuality's
// returned error (the text `runDoctor`'s "FAIL" line prints verbatim) must
// name capture_processing's own failure and must never fold a second,
// independently-failing task into the same collapsed sentence — one task's
// bad JSON is never reported as "the provider" in general.
func TestQualityGateErrorNamesEachFailingTaskSeparately(t *testing.T) {
	results := []taskQualityResult{
		{
			task: "capture_processing", total: 1,
			failures: []llmQualityFailure{{caseID: "classify-wrong-typed-field", field: "nudge_outcome", reason: string(classify.ReasonWrongType)}},
		},
		{
			task: "relation_evaluation", total: 1,
			failures: []llmQualityFailure{{caseID: "relation-some-case", field: "outcome", reason: string(classify.ReasonUnknownEnum)}},
		},
	}

	err := qualityGateError(results)
	if err == nil {
		t.Fatal("qualityGateError returned nil for two failing tasks, want a non-nil error")
	}

	text := err.Error()
	if !strings.Contains(text, "capture_processing") {
		t.Errorf("error text %q does not name capture_processing specifically", text)
	}
	if !strings.Contains(text, "relation_evaluation") {
		t.Errorf("error text %q does not name relation_evaluation specifically", text)
	}
	if strings.Contains(strings.ToLower(text), "unsuitable") && !strings.Contains(text, "capture_processing: ") {
		t.Errorf("error text %q reads as one collapsed verdict rather than one line per task", text)
	}
}

// TestCheckLLMQualityReportsTheConfiguredTaskCountEvenAtZero is spec R5.6:
// a freshly `init`ed vault (M0's defaultConfig(), which ships providers:/
// tasks: fully commented) binds nothing, so qualityGateProviders resolves
// an empty map and runLLMQualityCheck's own loop over jsonTasks iterates
// zero times — a structural no-op, never a `len(tasks) == 0` branch
// deciding pass or fail. checkLLMQuality must still report success (never
// a FAIL, matching test/e2e/doctor_test.go's TestDoctorOnAHealthyVault),
// and the report must state the zero count so a reader can tell "passed"
// from "did not run".
func TestCheckLLMQualityReportsTheConfiguredTaskCountEvenAtZero(t *testing.T) {
	cfg := &config.Config{} // no Providers, no Tasks — the freshly-init'ed shape
	err := checkLLMQuality("", cfg)

	detail, ok := err.(*qualityGateDetail)
	if !ok {
		t.Fatalf("checkLLMQuality on a config with no tasks bound = %v (%T), want a *qualityGateDetail — a no-op is success, never a FAIL", err, err)
	}
	if detail.tasksConfigured != 0 {
		t.Errorf("detail.tasksConfigured = %d, want 0", detail.tasksConfigured)
	}
	text := detail.Error()
	if !strings.Contains(text, "0 tasks configured") {
		t.Errorf("report text %q does not state the zero count — spec R5.6", text)
	}
}

// TestRunLLMQualityCheckReportsATransportFailureAsUnreachable is spec R5.7:
// a transport-level failure for one task's provider is reported as the
// provider being unreachable — distinct in wording and in category from a
// JSON-degradation failure — and is never folded into, or counted toward,
// the "k of n prompts produced clean JSON" line above it (spec R5.4). A
// model cannot be judged bad at JSON on the strength of a network (or
// vendor status) that never delivered an answer to judge.
//
// classify-provider-rate-limited.json is testdata/llm/'s own recorded
// provider-level failure (format.md's error field) — exactly the shape a
// real provider's transport/HTTP failure surfaces as through
// ports.LLMProvider.Complete.
func TestRunLLMQualityCheckReportsATransportFailureAsUnreachable(t *testing.T) {
	dir := testdataLLMCasesDir(t)
	rateLimited := loadCase(t, dir, "classify-provider-rate-limited")
	fake := fakeprovider.New(t, dir, rateLimited.ID)

	got := runLLMQualityCheck(context.Background(), map[string]ports.LLMProvider{"capture_processing": fake}, []llm.Case{rateLimited}, time.Now(), qualityGateTimeout)

	if len(got) != 1 {
		t.Fatalf("got %d task result(s), want 1: %+v", len(got), got)
	}
	result := got[0]
	if result.unreachable == "" {
		t.Fatalf("task result = %+v, want a transport failure reported as unreachable, not folded into the JSON-degradation count", result)
	}
	if !strings.Contains(strings.ToLower(result.unreachable), "unreachable") {
		t.Errorf("unreachable text %q does not contain the word %q — doc 01's own existing category", result.unreachable, "unreachable")
	}
	if result.total != 0 {
		t.Errorf("result.total = %d, want 0 — a transport failure must not be counted toward the JSON-fitness denominator (spec R5.7)", result.total)
	}
	if text := result.String(); strings.Contains(text, "prompts produced clean JSON") {
		t.Errorf("report text %q reads as a JSON-fitness verdict, want it to read as unreachable only", text)
	}
}

// blockingProvider never answers unless its own context is canceled — the
// scripted-replay fakeprovider.Fake cannot exercise a hang, since it
// returns immediately by construction, so this is a second, purpose-built
// stub for TestRunLLMQualityCheckBoundsTheLiveCall alone. It still touches
// no network (CLAUDE.md non-negotiable #5): it is pure in-process
// select/channel logic.
type blockingProvider struct{}

func (blockingProvider) Complete(ctx context.Context, _ ports.LLMRequest) (ports.LLMResponse, error) {
	select {
	case <-ctx.Done():
		return ports.LLMResponse{}, ctx.Err()
	case <-time.After(2 * time.Second):
		// A correct caller cancels ctx well before this fires (the test
		// below uses a 50ms bound). This branch exists only so a
		// regression that drops the timeout fails this test in 2s
		// instead of hanging the whole suite.
		return ports.LLMResponse{}, errors.New("blockingProvider: no context deadline arrived within 2s")
	}
}

var _ ports.LLMProvider = blockingProvider{}

// TestRunLLMQualityCheckBoundsTheLiveCall is spec R5.7's second MUST: the
// live call carries a bounded timeout, so a single unreachable provider
// cannot make nooma doctor hang. Proven by measuring wall-clock time
// against a provider that would otherwise never return — if
// runLLMQualityCheck forgot to bound the context it hands to Complete,
// this test still finishes (blockingProvider's own 2s fallback), but the
// elapsed-time assertion below fails.
func TestRunLLMQualityCheckBoundsTheLiveCall(t *testing.T) {
	c := llm.Case{ID: "capture-processing-blocks-forever", Task: "classify", Prompt: "p"}

	start := time.Now()
	got := runLLMQualityCheck(context.Background(), map[string]ports.LLMProvider{"capture_processing": blockingProvider{}}, []llm.Case{c}, time.Now(), 50*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed >= 500*time.Millisecond {
		t.Fatalf("runLLMQualityCheck took %s against a 50ms timeout bound — the live call is not bounded (spec R5.7)", elapsed)
	}
	if len(got) != 1 {
		t.Fatalf("got %d task result(s), want 1: %+v", len(got), got)
	}
	if got[0].unreachable == "" {
		t.Errorf("task result = %+v, want the timed-out call reported as unreachable", got[0])
	}
}

// TestCorpusCoversEveryQualityGateTask is spec R5.8: testdata/llm/cases/
// holds at least one case tagged with the corpus label for every jsonTasks
// member the gate checks — verified against the corpus as it stands today,
// not assumed.
func TestCorpusCoversEveryQualityGateTask(t *testing.T) {
	cases, err := llm.Cases()
	if err != nil {
		t.Fatalf("llm.Cases(): %v", err)
	}
	for _, task := range jsonTasks {
		label := corpusTaskLabel(task)
		found := false
		for _, c := range cases {
			if c.Task == label {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("testdata/llm/cases/ has no case tagged task %q (needed for jsonTasks member %q) — spec R5.8", label, task)
		}
	}
}

// TestCheckTaskCoverageReportsOKOnAFreshVault is design D18b row 1's own
// "no providers configured at all" case — the same shape 16a-ii's R5.6
// already established, applied here to a different check. A fresh vault
// (M0's defaultConfig(), providers:/tasks: fully commented) must report ok,
// not FAIL, so test/e2e/doctor_test.go's TestDoctorOnAHealthyVault stays
// green unchanged.
func TestCheckTaskCoverageReportsOKOnAFreshVault(t *testing.T) {
	cfg := &config.Config{} // no Providers, no Tasks
	err := checkTaskCoverage("", cfg)

	detail, ok := err.(taskCoverageDetail)
	if !ok {
		t.Fatalf("checkTaskCoverage on a config with no providers configured = %v (%T), want a taskCoverageDetail — a fresh vault is not broken", err, err)
	}
	if !strings.Contains(detail.Error(), "no providers configured") {
		t.Errorf("report text %q does not say no providers are configured", detail.Error())
	}
}

// TestCheckTaskCoverageReportsOKWhenEveryTaskIsBound is design D18b row 1's
// "providers configured, every member of tasksM1Consumes bound" case.
func TestCheckTaskCoverageReportsOKWhenEveryTaskIsBound(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"local": {Type: "ollama", Model: "test-model"}},
		Tasks: map[string]config.TaskBinding{
			"capture_processing":  {Provider: "local"},
			"relation_evaluation": {Provider: "local"},
			"embedding":           {Provider: "local"},
		},
	}
	if err := checkTaskCoverage("", cfg); err != nil {
		t.Errorf("checkTaskCoverage with every task bound = %v, want nil", err)
	}
}

// TestCheckTaskCoverageCatchesAnUnboundEmbeddingTask is design D18b row 1's
// own reason for existing: "this row is what would have caught C9 before a
// single capture ran." A Cloud-configured vault with capture_processing and
// relation_evaluation bound but nothing bound to embedding must FAIL,
// naming the task and the actual consequence — asserted against the report
// text a user actually reads (16a-i's own lesson: assert the string, not
// only the value behind it).
//
// The consequence text asserted here is 13d's fail-closed one (503 on both
// routes), not design D18b row 1's own stale quoted wording ("capture will
// store units with no vector...", m1b D8's outage degrade) — tasks.md's
// C21.1 records the correction; a coordinator-caught review found the
// original wording would have told a user their captures were landing
// without vectors when in fact nothing was being captured at all.
func TestCheckTaskCoverageCatchesAnUnboundEmbeddingTask(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"local": {Type: "anthropic", Model: "test-model", APIKeyEnv: "TEST_KEY"}},
		Tasks: map[string]config.TaskBinding{
			"capture_processing":  {Provider: "local"},
			"relation_evaluation": {Provider: "local"},
			// embedding deliberately left unbound — the C9 shape.
		},
	}

	err := checkTaskCoverage("", cfg)
	if err == nil {
		t.Fatal("checkTaskCoverage reported ok with embedding unbound — this is the exact shape C9 shipped undetected")
	}
	text := err.Error()
	if !strings.Contains(text, "embedding") {
		t.Errorf("error %q does not name the unbound task, embedding", text)
	}
	if !strings.Contains(text, "503") {
		t.Errorf("error %q does not say the actual consequence (503 on both routes) — spec R6.3/design D18b row 1, corrected per tasks.md C21.1", text)
	}
	if !strings.Contains(text, "POST /capture") || !strings.Contains(text, "POST /recall") {
		t.Errorf("error %q does not name both routes that go down", text)
	}
	if strings.Contains(text, "no vector") || strings.Contains(text, "lexical leg") {
		t.Errorf("error %q still carries the stale D8-degradation wording this fix removed", text)
	}
	if strings.Contains(text, "capture_processing") || strings.Contains(text, "relation_evaluation") {
		t.Errorf("error %q names a bound task as if it were unbound too", text)
	}
}

// TestVaultCoverageError is design D18b row 2's own report text (spec
// R6.3): checkVaultCoverage's decision logic, split out so it is testable
// without a real vault — the same shape checkLLMQuality's qualityGateError
// already takes.
func TestVaultCoverageError(t *testing.T) {
	if err := vaultCoverageError(0); err != nil {
		t.Errorf("vaultCoverageError(0) = %v, want nil — zero is the healthy answer", err)
	}

	err := vaultCoverageError(3)
	if err == nil {
		t.Fatal("vaultCoverageError(3) = nil, want a FAIL naming the count")
	}
	text := err.Error()
	if !strings.Contains(text, "3 live units have no embedding") {
		t.Errorf("error %q does not name the count", text)
	}
	if !strings.Contains(text, "semantic recall") {
		t.Errorf("error %q does not say what breaks", text)
	}
}

// TestLLMQualityFailureStringNamesTheFailureKind is spec R5.4 Refinement 2
// at the level a user actually reads it: a wrong-shaped value and an
// out-of-vocabulary value are recorded as different events (doc 02 §5.1)
// and must render as different words — a gate that merges "formatting" and
// "vocabulary" into one "bad JSON" message throws away the distinction the
// two different pieces of user advice depend on.
func TestLLMQualityFailureStringNamesTheFailureKind(t *testing.T) {
	formatting := llmQualityFailure{caseID: "classify-wrong-typed-field", field: "nudge_outcome", reason: string(classify.ReasonWrongType)}
	vocabulary := llmQualityFailure{caseID: "classify-unknown-enum-value", field: "type", reason: string(classify.ReasonUnknownEnum)}

	if got := formatting.String(); !strings.Contains(got, "formatting") {
		t.Errorf("a wrong-typed field's report is %q, want it to name a formatting failure", got)
	}
	if got := vocabulary.String(); !strings.Contains(got, "vocabulary") {
		t.Errorf("an unknown-enum field's report is %q, want it to name a vocabulary failure", got)
	}
	if formatting.String() == vocabulary.String() {
		t.Error("a formatting failure and a vocabulary failure render identically — the distinction doc 02 §5.1 draws is lost")
	}
}

// TestCasesMatchesTheSourceFiles proves the embedded corpus
// (testdata/llm/corpus.go) carries the same id/task/prompt as the source
// files on disk — the anti-drift check ADR-0002's "written once, used in
// two places" actually needs: a bug in the embed step would otherwise send
// a live provider a DIFFERENT prompt than the one the golden-set tests
// exercise, and nothing would notice.
func TestCasesMatchesTheSourceFiles(t *testing.T) {
	dir := testdataLLMCasesDir(t)
	allEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	// .gitkeep (format.md's own convention for an otherwise-empty corpus
	// directory) is not a case file — filtered out rather than embedded.
	var entries []os.DirEntry
	for _, entry := range allEntries {
		if strings.HasSuffix(entry.Name(), ".json") {
			entries = append(entries, entry)
		}
	}

	embedded, err := llm.Cases()
	if err != nil {
		t.Fatalf("llm.Cases(): %v", err)
	}
	byID := make(map[string]llm.Case, len(embedded))
	for _, c := range embedded {
		byID[c.ID] = c
	}

	if len(embedded) != len(entries) {
		t.Fatalf("llm.Cases() returned %d case(s), testdata/llm/cases/ has %d .json file(s)", len(embedded), len(entries))
	}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		want := loadCase(t, dir, id)
		got, ok := byID[id]
		if !ok {
			t.Errorf("testdata/llm/cases/%s has no embedded counterpart", entry.Name())
			continue
		}
		if got != want {
			t.Errorf("embedded case %q = %+v, want %+v (source file)", id, got, want)
		}
	}
}

func assertTaskResults(t *testing.T, got, want []taskQualityResult) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d task result(s), want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.task != w.task || g.total != w.total || g.clean != w.clean {
			t.Errorf("result[%d] = %+v, want %+v", i, g, w)
		}
		if len(g.failures) != len(w.failures) {
			t.Fatalf("result[%d] has %d failure(s), want %d: %+v", i, len(g.failures), len(w.failures), g.failures)
		}
		for j := range w.failures {
			if g.failures[j] != w.failures[j] {
				t.Errorf("result[%d].failures[%d] = %+v, want %+v", i, j, g.failures[j], w.failures[j])
			}
		}
	}
}

func loadCase(t *testing.T, dir, id string) llm.Case {
	t.Helper()
	var ex goldenset.LLMExample
	if err := goldenset.Load(filepath.Join(dir, id+".json"), &ex); err != nil {
		t.Fatalf("loading corpus case %q: %v", id, err)
	}
	return llm.Case{ID: ex.ID, Task: ex.Task, Prompt: ex.Prompt}
}

// testdataLLMCasesDir locates testdata/llm/cases from this test file's own
// path (two directories up from cmd/nooma/) rather than the working
// directory `go test` happens to use — test/conformance/store_api_test.go's
// own repoRootFromCaller does the same, restated here because it is
// unexported to that package.
func testdataLLMCasesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to report this test file's path")
	}
	// thisFile is .../cmd/nooma/doctor_test.go
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "testdata", "llm", "cases")
}
