package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rengo/nooma/test/support/goldenset"
)

// fencedJSONBlock matches a fenced ```json ... ``` block in a format.md,
// capturing only its body (spec R10.2/R10.3).
var fencedJSONBlock = regexp.MustCompile("(?s)```json\\n(.*?)\\n```")

// goldenSetDirs are the three golden-set directories docs/06-harness.md §5
// and spec R10.1 require — empty of real cases in this change, since
// populating cases/ is M1's responsibility (spec R10.1's MUST NOT).
var goldenSetDirs = []string{"recall", "classify", "llm"}

// TestHarness_GoldenSetFormatsDeclared proves testdata/{recall,classify,llm}/
// exist and each carries a documented, machine-checkable format (spec
// R10.1/R10.2/R10.4): a format.md with a valid fenced JSON shape, a
// format_example.json sibling of cases/ (never inside it), and an empty
// cases/ directory.
//
// Design D10's guard: it asserts it found all three directories before
// asserting anything about their content, so a renamed or moved directory
// fails this test loudly instead of the per-directory subtests silently
// iterating zero directories and reporting green.
func TestHarness_GoldenSetFormatsDeclared(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	testdataDir := filepath.Join(repoRoot, "testdata")

	found := 0
	for _, name := range goldenSetDirs {
		info, err := os.Stat(filepath.Join(testdataDir, name))
		if err == nil && info.IsDir() {
			found++
		}
	}
	if found != len(goldenSetDirs) {
		t.Fatalf(
			"found %d of %d golden-set directories under %s (want %v) — "+
				"a directory is missing or renamed (design D10's guard)",
			found, len(goldenSetDirs), testdataDir, goldenSetDirs,
		)
	}

	for _, name := range goldenSetDirs {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(testdataDir, name)
			assertFormatMDDeclaresShape(t, dir, name)
			assertFormatExampleIsSiblingOfCases(t, dir)
			assertCasesDirIsEmpty(t, dir)
		})
	}
}

// formatToType maps each golden-set directory name to a constructor for the
// exact Go type test/support/goldenset declares for it (types.go) — the
// pairing TestHarness_GoldenSetFormatMatchesType proves format.md's fenced
// example still agrees with.
var formatToType = map[string]func() any{
	"recall":   func() any { return &goldenset.RecallExample{} },
	"classify": func() any { return &goldenset.ClassifyExample{} },
	"llm":      func() any { return &goldenset.LLMExample{} },
}

// TestHarness_GoldenSetFormatMatchesType proves each format.md's fenced
// ```json``` example — the shape a future author reads before writing the
// first real case — still decodes into the exact Go type
// test/support/goldenset declares for it (spec R10.2/R10.3), through the
// same decoder configuration Load applies to real cases
// (goldenset.DecodeStrict, json.Decoder.DisallowUnknownFields).
//
// Without this gate, format.md's prose and the Go type it describes could
// drift silently: TestGoldenSetFormatExamples above only checks
// format_example.json, a sibling fixture that is never regenerated from
// format.md's own fenced block, so a stale format.md would keep passing
// every other test in this change while quietly documenting a shape the
// loader no longer accepts — exactly the doc-vs-code drift this repo has
// been bitten by before (docs/03-data-model.md's own comparison,
// TestHarness_SchemaMatchesDoc03, is the precedent this test mirrors).
func TestHarness_GoldenSetFormatMatchesType(t *testing.T) {
	repoRoot := repoRootFromCaller(t)

	for _, name := range goldenSetDirs {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(repoRoot, "testdata", name, "format.md")
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}

			fence, err := goldenset.ExtractJSONFence(content)
			if err != nil {
				t.Fatalf("goldenset.ExtractJSONFence(%s) = _, %v, want nil error", path, err)
			}

			newValue, ok := formatToType[name]
			if !ok {
				t.Fatalf("no Go type registered in formatToType for golden-set directory %q", name)
			}

			v := newValue()
			if err := goldenset.DecodeStrict(fence, v); err != nil {
				t.Errorf(
					"%s's fenced ```json``` example does not decode into goldenset's Go type for %q: %v — "+
						"either format.md documents a field the type does not have, or the type has "+
						"a field format.md never mentions; fix whichever side is wrong",
					path, name, err,
				)
			}
		})
	}
}

// assertFormatMDDeclaresShape checks dir/format.md carries a non-empty,
// syntactically valid fenced ```json block (spec R10.2), and — for
// "classify" only — that its preamble (the text before that fence) states
// up front that the eventual corpus must include deliberately broken
// cases, since those are what prove I14 (spec R10.2, docs/06-harness.md §5).
func assertFormatMDDeclaresShape(t *testing.T, dir, name string) {
	t.Helper()

	path := filepath.Join(dir, "format.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	match := fencedJSONBlock.FindSubmatch(content)
	if match == nil {
		t.Fatalf("%s has no fenced ```json block", path)
	}
	body := strings.TrimSpace(string(match[1]))
	if body == "" {
		t.Fatalf("%s's fenced json block is empty", path)
	}
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("%s's fenced json block is not valid JSON: %v", path, err)
	}

	if name != "classify" {
		return
	}

	fenceIdx := strings.Index(string(content), "```json")
	if fenceIdx < 0 {
		t.Fatalf("%s: fenced json block not found for the up-front check", path)
	}
	preamble := strings.ToLower(string(content[:fenceIdx]))
	for _, phrase := range []string{"truncated", "wrong type", "unknown enum"} {
		if !strings.Contains(preamble, phrase) {
			t.Errorf(
				"%s's preamble (the text before the fenced json block) does not mention %q — "+
					"R10.2 requires stating up front that the corpus must include deliberately "+
					"broken cases, because those are what prove I14",
				path, phrase,
			)
		}
	}
}

// assertFormatExampleIsSiblingOfCases checks dir/format_example.json exists
// and dir/cases/format_example.json does not (spec R10.4): nothing that
// iterates cases/ for real corpus data may mistake the example for a case.
func assertFormatExampleIsSiblingOfCases(t *testing.T, dir string) {
	t.Helper()

	siblingPath := filepath.Join(dir, "format_example.json")
	if _, err := os.Stat(siblingPath); err != nil {
		t.Fatalf("stat %s: %v", siblingPath, err)
	}

	insideCasesPath := filepath.Join(dir, "cases", "format_example.json")
	if _, err := os.Stat(insideCasesPath); err == nil {
		t.Fatalf(
			"%s exists — format_example.json must be a sibling of cases/, not inside it (R10.4)",
			insideCasesPath,
		)
	}
}

// assertCasesDirIsEmpty checks dir/cases/ exists and holds nothing but
// .gitkeep (spec R10.1's MUST NOT — real corpora are M1's responsibility).
func assertCasesDirIsEmpty(t *testing.T, dir string) {
	t.Helper()

	casesDir := filepath.Join(dir, "cases")
	info, err := os.Stat(casesDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("%s is not a directory: %v", casesDir, err)
	}

	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatalf("read dir %s: %v", casesDir, err)
	}
	for _, e := range entries {
		if e.Name() != ".gitkeep" {
			t.Errorf(
				"%s contains %q — this change ships an empty corpus (R10.1's MUST NOT); "+
					"real cases are M1's responsibility",
				casesDir, e.Name(),
			)
		}
	}
}
