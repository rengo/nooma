// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreNeverReadsTheClockDirectly is the store's half of the clock
// discipline docs/06-harness.md §2 states: the current instant is data that
// arrives through a port, never something a layer helps itself to.
//
// Why a test and not a linter. `forbidigo` already forbids time.Now inside
// internal/core, and it is scoped there deliberately — a time.Now() in
// internal/brain is legal and correct, since brain is where the instant
// enters. But the store is not brain: ports.UnitRepo's UpdateContent and
// SetStatus both take `at time.Time` precisely so the caller decides what
// "now" means, and a store that read its own clock would write a timestamp
// nobody chose. `.golangci.yml`'s exclusions.rules cannot express a second
// forbidigo scope without silencing the first — measured on this repo's
// pinned golangci-lint v2.12.2 — so the gate lives here instead of there.
//
// It matches the AST, not the text, and that is not gold-plating. The
// obvious version of this test — scanning lines for the substring
// "time.Now(" — was written first and failed immediately on
// internal/store/sqlite/dsn.go:23, which mentions time.Now inside a comment
// explaining that internal/core never calls it. Design's own ground-truth
// row had already counted those two comment hits without saying what to do
// about them. A guard that cannot tell a call from a sentence about calls
// is the same weak-heuristic class this repo has been paying for elsewhere;
// go/ast is already used by store_api_test.go, so precision here costs a
// parser call and nothing else.
//
// This guard has no pre-implementation red: nothing under internal/store/
// calls time.Now at the moment it is written, and the code it guards was
// already correct. What proves it fails for the right reason is its
// temporary-break check, run and recorded in the PR that adds it, per
// docs/06-harness.md §4's discipline for a guard with no natural red.
//
// Non-test .go files only. A _test.go file under internal/store/ may read
// the real clock freely — an L3 test asserting that an updated_at moved
// needs a real instant to compare against.
func TestStoreNeverReadsTheClockDirectly(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	fset := token.NewFileSet()

	scanned := 0
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal", "store"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++

		// Parsed without ParseComments: a comment mentioning time.Now is
		// not a call, and must not read as one.
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Now" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "time" {
				return true
			}
			t.Errorf(
				"%s: %s.Now() — the store must not read the clock: the instant "+
					"arrives as a parameter (ports.UnitRepo takes `at time.Time`), "+
					"so the caller decides what now means "+
					"(docs/06-harness.md §2, design.md §7 Risk #10)",
				fset.Position(call.Pos()), pkg.Name,
			)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/store: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned zero non-test .go files under internal/store/ — nothing to check yet")
	}
}
