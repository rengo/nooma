// Package conformance — see test/conformance/doc.go for the package contract.
//
// This file carries no test of its own; it is a shared helper for I01 and
// I03, both untagged since Phase A. No file in this package carries the
// pendingimpl build tag any longer — I21, the last anchor, was promoted in
// m1b-pipeline PR 8a.
package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scanGoTree walks root for .go files and reports, via t.Errorf, any line
// for which match returns true — report renders the failure message for
// that line, given the file path, its 1-based line number, and the raw
// line text. It returns the number of .go files scanned, so callers can
// apply design D10's non-empty-corpus guard: a moved or renamed directory
// must fail loudly, not pass vacuously.
//
// Shared by I01 (i01_focus_never_persisted_test.go) and I03
// (i03_units_never_deleted_test.go), which differ only in their per-line
// predicate and failure message — both are coarse, line-based heuristics
// over Go source only. Migrations are .sql, embedded via go:embed, and are
// naturally outside this scan (design D1).
func scanGoTree(t *testing.T, root string, match func(line string) bool, report func(path string, lineNum int, line string)) (scanned int) {
	t.Helper()

	if _, err := os.Stat(root); os.IsNotExist(err) {
		return 0
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
		for i, line := range strings.Split(string(content), "\n") {
			if match(line) {
				report(path, i+1, line)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return scanned
}
