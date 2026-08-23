// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// telegramHost is assembled from parts on purpose, and the reason is this
// test's own subject: it asserts the literal appears in NO _test.go file,
// and this is a _test.go file. Written as one string it would be the first
// violation of the rule it exists to enforce.
//
// The seam is a real constraint, not a trick — anything that reconstructs
// the host at runtime evades the scan, and this file is the one place
// where doing so is correct. The comment is what keeps that from being
// copied somewhere it is not.
var telegramHost = "api." + "telegram" + ".org"

// hostConstName is where the one permitted occurrence must live.
const hostConstName = "defaultBaseURL"

// TestTelegramHostLiteralAppearsOnceAndNeverInATest is proposal §9's risk
// R5, closed structurally.
//
// M2's discharge #4 recorded that the network half of CLAUDE.md
// non-negotiable #5 — "no test touches the network or a real LLM" — has no
// guard at all: it is discipline, and nothing fails when discipline does.
// Until this chain there was nothing for a test to dial. **This is the
// first slice where a copy-pasted real URL would pass CI**, so it is the
// slice that owes the guard.
//
// Two legs, and they fail for different reasons:
//
//   - Exactly one occurrence outside tests, and it is the named default
//     constant. More than one means a second code path builds its own
//     URL and will not be redirected by a test's httptest server.
//   - Zero occurrences in any _test.go file, anywhere. A test naming the
//     real host is a test that will eventually dial it.
//
// It parses rather than greps: a comment mentioning the host is
// documentation, and m2d's JD-4-01 found a byte-comparing scan being
// defeated by exactly that distinction.
func TestTelegramHostLiteralAppearsOnceAndNeverInATest(t *testing.T) {
	root := repoRootFromCaller(t)

	type occurrence struct {
		file string
		spec string
	}
	var production, inTests []occurrence

	fset := token.NewFileSet()
	scanned := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name == ".git" || name == "openspec" || name == "docs" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		scanned++

		rel, _ := filepath.Rel(root, path)
		isTest := strings.HasSuffix(path, "_test.go")

		// Track the innermost value-spec name so a found literal can be
		// attributed to the constant it initialises.
		specName := map[ast.Node]string{}
		ast.Inspect(file, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok || len(vs.Names) == 0 {
				return true
			}
			for _, v := range vs.Values {
				specName[v] = vs.Names[0].Name
			}
			return true
		})

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if !strings.Contains(lit.Value, telegramHost) {
				return true
			}
			found := occurrence{file: rel, spec: specName[ast.Node(lit)]}
			if isTest {
				inTests = append(inTests, found)
			} else {
				production = append(production, found)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if scanned == 0 {
		t.Fatal("scanned zero Go files — nothing was checked")
	}

	if len(inTests) != 0 {
		t.Errorf("the Telegram API host appears in %d test file(s) %v — a test naming the real host is a test that will eventually dial it", len(inTests), inTests)
	}

	switch {
	case len(production) == 0:
		t.Fatalf("the Telegram API host appears nowhere outside tests, want exactly once at %s — the constant every client must be redirected from", hostConstName)
	case len(production) > 1:
		t.Fatalf("the Telegram API host appears %d times outside tests %v, want exactly once — a second occurrence is a code path an httptest server cannot redirect", len(production), production)
	case production[0].spec != hostConstName:
		t.Fatalf("the Telegram API host is at %s in %s, want the constant %s", production[0].spec, production[0].file, hostConstName)
	}
}
