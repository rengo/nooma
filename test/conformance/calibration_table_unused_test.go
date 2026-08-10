// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"path/filepath"
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
// Same shape as I03's own DELETE-scan (i03_units_never_deleted_test.go):
// a source-tree scan over .go files under internal/, matching a SQL
// keyword immediately followed by the table name "calibration", with an
// identifier-boundary check so "calibration_history" (a hypothetical
// future table) would not false-positive against this one's name. This is
// genuinely red for the right reason if any earlier task had accidentally
// referenced the table — it passes today because nothing does; migrations
// are .sql files, embedded via go:embed, and are naturally outside this
// scan (design D1).
func TestCalibrationTableStaysUnused(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	report := func(path string, lineNum int, line string) {
		t.Errorf(
			"%s:%d: %q — the calibration table stays fully unused through the whole of m2c "+
				"(spec R2.5); goal_stagnation_days reads config.goal_stagnation_days, never "+
				"calibration's own generic key/value row",
			path, lineNum, strings.TrimSpace(line),
		)
	}

	scanned := scanGoTree(t, filepath.Join(repoRoot, "internal"), containsCalibrationTableReference, report)
	if scanned == 0 {
		t.Fatal("scanned zero .go files under internal/ — D10's guard: nothing to check yet")
	}
}

// calibrationSQLMarkers are the SQL keyword + table name pairs that would
// make "calibration" a table reference rather than an ordinary English
// word — this repository's comments say "calibration table",
// "recalibration" and "calibration data" throughout doc 02 and core
// package comments, none of which this scan may flag.
var calibrationSQLMarkers = []string{
	"FROM CALIBRATION",
	"INTO CALIBRATION",
	"UPDATE CALIBRATION",
	"JOIN CALIBRATION",
	"TABLE CALIBRATION",
}

// containsCalibrationTableReference reports whether line contains any of
// calibrationSQLMarkers, case-insensitive, rejecting a match whose next
// character would extend the identifier — mirroring
// containsUnitsDeleteStatement's own boundary check in
// i03_units_never_deleted_test.go.
func containsCalibrationTableReference(line string) bool {
	upper := strings.ToUpper(line)
	for _, marker := range calibrationSQLMarkers {
		idx := strings.Index(upper, marker)
		if idx == -1 {
			continue
		}
		after := idx + len(marker)
		if after < len(upper) {
			c := upper[after]
			isIdentTail := c == '_' || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
			if isIdentTail {
				continue
			}
		}
		return true
	}
	return false
}
