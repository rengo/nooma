package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/goldenset"
	"github.com/rengo/nooma/testdata/llm"
)

// TestDoctorChecksGainsOneNewEntry is spec R5.1: doctorChecks gains exactly
// one new row, following the existing {name, run} shape, and every
// existing check keeps its own name and order — this PR is one new row in
// an existing table, never a rewrite of runDoctor's own loop.
func TestDoctorChecksGainsOneNewEntry(t *testing.T) {
	want := []string{"configuration", "permissions", "database integrity", "schema version", "bind", "llm quality"}
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

			got := runLLMQualityCheck(context.Background(), providers, tt.cases, time.Now())
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

	runLLMQualityCheck(context.Background(), map[string]ports.LLMProvider{"capture_processing": fake}, []llm.Case{c}, time.Now())

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
