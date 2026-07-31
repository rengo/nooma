// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoTestSupportImportOutsideTests guards the boundary design.md §7 Risk
// #10 and its own test matrix state: test/support exists to be crossed by
// L2/L3/L4 and by internal/brain's own package tests, never by production
// code. No non-_test.go file under internal/ or cmd/ may import
// github.com/rengo/nooma/test/support/... .
//
// This guard has no pre-implementation red — nothing under internal/ or
// cmd/ imports test/support at the moment this test is written. Its own
// temporary-break check (this PR's own verification step) is what proves
// it fails for the right reason, per docs/06-harness.md §4's discipline
// for a guard with no natural red.
//
// Non-test .go files only: internal/store/sqlite/*_integration_test.go
// (PR 4) and internal/brain's own tests are meant to import
// test/support/repocontract and friends — scanning _test.go files here
// would make this guard fail on exactly the imports design D6 intends.
func TestNoTestSupportImportOutsideTests(t *testing.T) {
	const forbiddenImport = `"github.com/rengo/nooma/test/support/`
	repoRoot := repoRootFromCaller(t)

	scanned := 0
	scan := func(root string) {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			scanned++

			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(content), "\n") {
				if strings.Contains(line, forbiddenImport) {
					t.Errorf(
						"%s:%d: %q — production code must not import test/support "+
							"(design.md §7 Risk #10: test/support is for L2/L3/L4 and "+
							"internal/brain's own tests, never for the binary)",
						path, i+1, strings.TrimSpace(line),
					)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	scan(filepath.Join(repoRoot, "internal"))
	scan(filepath.Join(repoRoot, "cmd"))
	if scanned == 0 {
		t.Fatal("scanned zero non-test .go files under internal/ and cmd/ — nothing to check yet")
	}
}
