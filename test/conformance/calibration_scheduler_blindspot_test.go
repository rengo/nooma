// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCalibrationRowsNameTheirOwnBlindSpot is R1.2, and it guards a
// property no other test can: that a §13 row which is NOT checked says so.
//
// calibration_doc_test.go verifies rows that name an internal/core
// constant. Rows naming internal/scheduler constants are invisible to it —
// its regex never reaches them — so those rows are unverified AND
// unmarked, which reads to a future maintainer as coverage.
//
// The row's own prose used to blame the "03:00" parse and say splitting it
// was M3's job. M3 split it and the diagnosis was wrong: the gate never
// reaches internal/scheduler at all, so a constant name buys nothing.
// This asserts the corrected prose stays corrected.
func TestCalibrationRowsNameTheirOwnBlindSpot(t *testing.T) {
	root := repoRootFromCaller(t)
	body, err := os.ReadFile(filepath.Join(root, "docs", "02-cognitive-core.md"))
	if err != nil {
		t.Fatalf("reading doc 02: %v", err)
	}
	doc := string(body)

	for _, constant := range []string{
		"internal/scheduler.ConsolidationHour",
		"internal/scheduler.ProactiveCheckInterval",
	} {
		idx := strings.Index(doc, constant)
		if idx == -1 {
			t.Errorf("§13 names no row for %s", constant)
			continue
		}
		// The row is the line the constant appears on.
		start := strings.LastIndexByte(doc[:idx], '\n') + 1
		end := strings.IndexByte(doc[idx:], '\n')
		row := doc[start : idx+end]

		if !strings.Contains(row, "not checked") && !strings.Contains(row, "outside the gate") {
			t.Errorf("§13's row for %s does not say it is unchecked:\n%s\n\nA row that is neither verified nor marked reads as coverage.", constant, row)
		}
	}
}
