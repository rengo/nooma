// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCalibrationTableStaysUnused proves the L2 half of spec R2.5: the
// calibration table (migration 0002:37) stays fully unused through the
// whole of m2c. goal_stagnation_days has exactly one schema home for M2 —
// config.goal_stagnation_days — and calibration exists for M5's learning
// module to write arbitrary future per-user knobs that have no dedicated
// config column; giving goal_stagnation_days two writers would be the same
// drift ruling Q1 already declined to accept for a different pair of
// sources.
//
// Same shape as I03's own DELETE-scan (i03_units_never_deleted_test.go): a
// source-tree scan over .go files under internal/, matching a SQL keyword
// immediately followed by the table name "calibration". Unlike that scan,
// this one matches over each file's whole content rather than one physical
// line at a time, with an identifier boundary check so "calibration_history"
// (a hypothetical future table) does not false-positive against this one's
// name. This is genuinely red for the right reason if any earlier task had
// accidentally referenced the table — it passes today because nothing does;
// migrations are .sql files, embedded via go:embed, and are naturally
// outside this scan (design D1).
//
// The matcher is whitespace-tolerant and quote-aware: calibrationQuotedPatterns
// and calibrationBarePattern accept any run of whitespace — including a
// newline — between the SQL keyword and "calibration", and accept the
// identifier bare or delimited by a matching pair of double quotes or
// backticks. That closes three gaps confirmed against an earlier, per-line,
// single-space substring version of this scan: irregular whitespace
// ("FROM   calibration"), double- or backtick-quoted identifiers, and a
// keyword/identifier split across two source lines.
//
// What this scan cannot catch, and does not claim to: a table name
// assembled at runtime, e.g. fmt.Sprintf("... FROM %s", "calibration"), or
// one built from a variable or constant rather than written as a source
// literal next to its keyword. No source-tree scan can evaluate a running
// program's string composition — catching that shape would require
// executing (or fully abstract-interpreting) the code, which is exactly what
// design D1 keeps this whole family of conformance tests from having to do.
// docs/02-cognitive-core.md §13's "verified by a source-tree scan" sentence
// is accurate under that limit: it claims the scan, not exhaustive detection
// of every way source code could name the table.
func TestCalibrationTableStaysUnused(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	scanned := scanGoTreeForCalibrationReference(t, filepath.Join(repoRoot, "internal"))
	if scanned == 0 {
		t.Fatal("scanned zero .go files under internal/ — D10's guard: nothing to check yet")
	}
}

// calibrationKeywords are the SQL keywords that make "calibration" a table
// reference rather than an ordinary English word — the same set
// containsCalibrationTableReference's predecessor used.
const calibrationKeywords = `FROM|INTO|UPDATE|JOIN|TABLE`

// calibrationQuotedPatterns match a keyword followed by whitespace of any
// kind — including a newline, since \s already matches one in Go's regexp
// package, which is what makes a keyword/identifier split across two source
// lines catchable without special-casing it — then the identifier delimited
// by a matching pair of double quotes or backticks. A closing quote
// delimits the identifier unambiguously, so unlike calibrationBarePattern
// below, these carry no trailing \b: \b only fires at a \w/\W transition,
// and a quote followed by ordinary punctuation or whitespace (both \W) is
// never such a transition — an earlier version of this pattern silently
// matched nothing for exactly that reason, caught by running it against the
// quoted shapes below and finding zero matches where matches were expected.
var calibrationQuotedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(` + calibrationKeywords + `)\s+"calibration"`),
	regexp.MustCompile("(?i)\\b(" + calibrationKeywords + ")\\s+`calibration`"),
}

// calibrationBarePattern is calibrationQuotedPatterns' unquoted counterpart.
// Its match does not by itself rule out "calibration" being the prefix of a
// longer identifier (e.g. "FROM calibration_history"); isCalibrationIdentBoundary
// below does that check against the character immediately following the
// match, mirroring containsUnitsDeleteStatement's own boundary check in
// i03_units_never_deleted_test.go.
var calibrationBarePattern = regexp.MustCompile(`(?i)\b(` + calibrationKeywords + `)\s+calibration`)

// isCalibrationIdentBoundary reports whether the byte at text[pos] (or the
// end of text, when pos == len(text)) is not a continuation of the
// identifier calibrationBarePattern just matched — i.e. not a letter,
// digit, or underscore.
func isCalibrationIdentBoundary(text string, pos int) bool {
	if pos >= len(text) {
		return true
	}
	c := text[pos]
	return c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9')
}

// scanGoTreeForCalibrationReference walks root for .go files and reports
// every calibration table reference found in a file's whole content —
// deliberately not tree_scan_test.go's shared scanGoTree, which matches one
// physical line at a time and so cannot see a match spanning two lines.
// D10's non-empty-corpus guard is the caller's job, mirroring scanGoTree's
// own division of labor.
func scanGoTreeForCalibrationReference(t *testing.T, root string) (scanned int) {
	t.Helper()

	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0
	}

	report := func(path, text string, start, end int) {
		line := 1 + strings.Count(text[:start], "\n")
		// Fields+Join collapses the match's own internal whitespace run
		// (which may include the newline this scan is built to tolerate)
		// into one space, for a readable single-line report.
		matched := strings.Join(strings.Fields(text[start:end]), " ")
		t.Errorf(
			"%s:%d: %q — the calibration table stays fully unused through the whole of m2c "+
				"(spec R2.5); goal_stagnation_days reads config.goal_stagnation_days, never "+
				"calibration's own generic key/value row",
			path, line, matched,
		)
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		scanned++

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)

		for _, pattern := range calibrationQuotedPatterns {
			for _, loc := range pattern.FindAllStringIndex(text, -1) {
				report(path, text, loc[0], loc[1])
			}
		}
		for _, loc := range calibrationBarePattern.FindAllStringIndex(text, -1) {
			if isCalibrationIdentBoundary(text, loc[1]) {
				report(path, text, loc[0], loc[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return scanned
}
