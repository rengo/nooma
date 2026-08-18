package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/test/support/goldenset"
)

// goldenSetDirs are the golden-set directories docs/06-harness.md §5 and
// spec R10.1 require, plus testdata/consolidation/ (spec R4.1, design
// §8.2, m2d-scheduler-demo) — the newest addition, and the only one of the
// four still populated by real cases in this change rather than emptied of
// them (recall/classify/llm are M1's own responsibility, spec R10.1's MUST
// NOT).
var goldenSetDirs = []string{"recall", "classify", "llm", "consolidation"}

// TestHarness_GoldenSetFormatsDeclared proves every directory in
// goldenSetDirs exists and each carries a documented, machine-checkable
// format (spec R10.1/R10.2/R10.4, R4.1): a format.md with a valid fenced
// JSON shape, a format_example.json sibling of cases/ (never inside it),
// and cases/ matching casesDirMustBeEmpty's rule for that directory.
//
// Design D10's guard: it asserts it found every directory before asserting
// anything about their content, so a renamed or moved directory fails this
// test loudly instead of the per-directory subtests silently
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
			assertCasesDirEmptiness(t, dir, name)
		})
	}
}

// formatToType maps each golden-set directory name to a constructor for the
// exact Go type test/support/goldenset declares for it (types.go) — the
// pairing TestHarness_GoldenSetFormatMatchesType proves format.md's fenced
// example still agrees with.
var formatToType = map[string]func() any{
	"recall":        func() any { return &goldenset.RecallExample{} },
	"classify":      func() any { return &goldenset.ClassifyExample{} },
	"llm":           func() any { return &goldenset.LLMExample{} },
	"consolidation": func() any { return &goldenset.ConsolidationExample{} },
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
// It delegates to validateFormatMDShape so the check itself is directly
// testable without a *testing.T (TestValidateFormatMDShape_RejectsTwoFences).
func assertFormatMDDeclaresShape(t *testing.T, dir, name string) {
	t.Helper()
	if err := validateFormatMDShape(dir, name); err != nil {
		t.Fatal(err)
	}
}

// validateFormatMDShape is the pure check assertFormatMDDeclaresShape runs:
// it routes fence extraction through goldenset.ExtractJSONFence — the SAME
// fence parser TestHarness_GoldenSetFormatMatchesType uses — rather than a
// second, independently-configured one. Two fence parsers in the same file
// could silently drift (four-lens pre-PR review, CRITICAL finding 1): the
// original ad hoc `fencedJSONBlock` regexp used `FindSubmatch`, which
// silently picked the FIRST of two fences instead of erroring — exactly the
// trap ExtractJSONFence was written to close, reintroduced a few lines away
// from its own fix.
func validateFormatMDShape(dir, name string) error {
	path := filepath.Join(dir, "format.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	fence, err := goldenset.ExtractJSONFence(content)
	if err != nil {
		return fmt.Errorf("goldenset.ExtractJSONFence(%s) = _, %w, want nil error", path, err)
	}
	body := strings.TrimSpace(string(fence))
	if body == "" {
		return fmt.Errorf("%s's fenced json block is empty", path)
	}
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return fmt.Errorf("%s's fenced json block is not valid JSON: %w", path, err)
	}

	if name != "classify" {
		return nil
	}

	// The three-phrase check below is a LITERAL-SUBSTRING PROXY for a
	// documentation commitment (spec R10.2), not a semantic check (four-lens
	// pre-PR review, WARNING finding 8): it fails on a faithful rewording
	// that avoids these exact words (e.g. "an intentionally malformed
	// payload" instead of "wrong type"), and it would pass if the three
	// phrases appeared in an unrelated sentence that never actually commits
	// to shipping broken cases. No general mechanized replacement was found
	// that verifies the SEMANTIC commitment without either (a) requiring an
	// LLM judge at test time, which CLAUDE.md non-negotiable #5 forbids, or
	// (b) hand-encoding a slightly larger set of literal synonyms that is
	// still a proxy, just a wider one. This check is kept as a cheap
	// tripwire against the preamble being deleted or never written, not as
	// proof the preamble's prose is faithful to spec R10.2.
	fenceIdx := strings.Index(string(content), "```json")
	if fenceIdx < 0 {
		return fmt.Errorf("%s: fenced json block not found for the up-front check", path)
	}
	preamble := strings.ToLower(string(content[:fenceIdx]))
	var missing []string
	for _, phrase := range []string{"truncated", "wrong type", "unknown enum"} {
		if !strings.Contains(preamble, phrase) {
			missing = append(missing, phrase)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"%s's preamble (the text before the fenced json block) does not mention %v — "+
				"R10.2 requires stating up front that the corpus must include deliberately "+
				"broken cases, because those are what prove I14",
			path, missing,
		)
	}
	return nil
}

// TestValidateFormatMDShape_RejectsTwoFences proves validateFormatMDShape —
// the check TestHarness_GoldenSetFormatsDeclared runs per directory via
// assertFormatMDDeclaresShape — now fails loudly on a format.md with two
// fenced ```json``` blocks, instead of silently validating whichever one
// came first (four-lens pre-PR review, CRITICAL finding 1). Before this
// fix, the ad hoc `fencedJSONBlock` regexp's `FindSubmatch` silently picked
// the first fence and this same scenario passed green.
func TestValidateFormatMDShape_RejectsTwoFences(t *testing.T) {
	dir := t.TempDir()
	twoFences := "# doc\n\n" +
		"```json\n" +
		"{\"a\": 1}\n" +
		"```\n\n" +
		"```json\n" +
		"{\"b\": 2}\n" +
		"```\n"
	if err := os.WriteFile(filepath.Join(dir, "format.md"), []byte(twoFences), 0o644); err != nil {
		t.Fatalf("write format.md: %v", err)
	}

	err := validateFormatMDShape(dir, "recall")
	if err == nil {
		t.Fatal(
			"validateFormatMDShape(...) = nil, want an error — a format.md with two ```json``` " +
				"fences must never be silently validated against whichever one came first",
		)
	}
	if !strings.Contains(err.Error(), "found 2 fenced") {
		t.Fatalf("validateFormatMDShape(...) error = %v, want it to mention the ambiguous fence count", err)
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

// casesDirMustBeEmpty maps each golden-set directory name to whether its
// cases/ subdirectory (beyond .gitkeep) is still required to hold nothing,
// or must already hold at least one real case.
//
// All four are now false, and the map stays rather than collapsing into a
// single "every corpus must be non-empty" check. Its job was never only the
// asymmetry: an emptied-out or relocated corpus must fail LOUDLY rather than
// pass vacuously, and that half applies to every directory here forever.
// The classify entry was the last true one — inverted by m1b-pipeline PR 7c,
// the PR that populates it (spec R1.6), after recall's (PR 8c) and llm's
// (R5.3). consolidation is added already false (spec R4.1, m2d-scheduler-
// demo): unlike the other three, this golden set ships its first real case
// in the same PR that registers it. Rules-as-data, design D10's pattern.
var casesDirMustBeEmpty = map[string]bool{
	"recall":        false,
	"classify":      false,
	"llm":           false,
	"consolidation": false,
}

// assertCasesDirEmptiness checks dir/cases/ against casesDirMustBeEmpty for
// name: a directory required to be empty but holding anything beyond
// .gitkeep fails (spec R10.1's MUST NOT — real corpora are M1's
// responsibility), and a directory required to be non-empty but holding
// only .gitkeep fails too (spec R5.4 — a moved or emptied-out corpus must
// fail loudly, not pass vacuously).
func assertCasesDirEmptiness(t *testing.T, dir, name string) {
	t.Helper()

	mustBeEmpty, ok := casesDirMustBeEmpty[name]
	if !ok {
		t.Fatalf("no cases/ emptiness rule registered for golden-set directory %q", name)
	}

	casesDir := filepath.Join(dir, "cases")
	info, err := os.Stat(casesDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("%s is not a directory: %v", casesDir, err)
	}

	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatalf("read dir %s: %v", casesDir, err)
	}

	realCases := 0
	for _, e := range entries {
		if e.Name() == ".gitkeep" {
			continue
		}
		realCases++
		if mustBeEmpty {
			t.Errorf(
				"%s contains %q — this change ships an empty corpus (R10.1's MUST NOT); "+
					"real cases are M1's responsibility",
				casesDir, e.Name(),
			)
		}
	}
	if !mustBeEmpty && realCases == 0 {
		t.Errorf(
			"%s holds only .gitkeep — expected at least one real case beyond .gitkeep (R5.4); "+
				"a moved or emptied-out corpus must fail loudly, not pass vacuously",
			casesDir,
		)
	}
}
