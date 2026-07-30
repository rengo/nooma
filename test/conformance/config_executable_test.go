package conformance

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestI_ConfigNeverConsultsTheExecutable is spec R6.6 made into a gate.
//
// Vault resolution must never look for a vault beside the nooma binary.
// `os.Executable` returns the *resolved* path, so a symlinked install
// (`~/.local/bin/nooma -> /opt/nooma/nooma`) would search `/opt/nooma/` — a
// directory the user never typed and has no reason to suspect. A search location
// nobody can predict from the command they ran is a search location that will one
// day open the wrong brain.
//
// `internal/config`'s injected environment has no `executable` member, which
// makes the resolver structurally unable to ask. This test covers the rest of the
// package: nothing else may reach for it either, however innocent the reason.
//
// # Why this is a test and not a lint rule
//
// The obvious implementation is a `forbidigo` pattern scoped to
// `internal/config/`. It was tried, measured against the pinned
// `golangci-lint v2.12.2`, and rejected: `exclusions.rules` entries **OR
// together**, so adding a rule scoped to `internal/config` also suppresses the
// existing one scoped to `internal/core`. Both stopped reporting —
// `os.Getenv` in the core included, which is `CLAUDE.md` non-negotiable #3 and has
// been enforced since the first PR. The "additive" gate would have silently
// disabled a working one while appearing to add coverage.
//
// A configuration that does work exists (a `text:` filter on every exclusion
// rule) and is rejected too: it leaves the same trap latent, because the next
// `forbidigo` pattern added without its own `text:` entry is silently excluded
// everywhere. A mechanism whose failure mode is "the gate quietly stops gating" is
// the wrong mechanism for this project.
//
// A tree scan has no such failure mode. A missing scan is a visibly absent test,
// not a passing gate.
func TestI_ConfigNeverConsultsTheExecutable(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	dir := filepath.Join(repoRoot, "internal", "config")

	fset := token.NewFileSet()
	scanned := 0

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		scanned++

		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Executable" {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "os" {
				return true
			}
			t.Errorf(
				"%s:%d references os.Executable.\n"+
					"Vault resolution must never consult the binary's own directory (spec R6.6):\n"+
					"os.Executable returns the resolved path, so a symlinked install searches a\n"+
					"directory the user never typed.",
				filepath.Base(path), fset.Position(sel.Pos()).Line)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}

	// D10's non-empty-corpus rule. A scan that silently walked nothing would pass
	// forever, which is the shape of gate this project keeps finding.
	if scanned == 0 {
		t.Fatalf("scanned 0 non-test files under %s — the scan is broken, not the package", dir)
	}
}
