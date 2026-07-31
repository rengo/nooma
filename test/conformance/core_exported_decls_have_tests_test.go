package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCoreExportedDeclsHaveTests is design D9's presence-guard proxy for
// the >=90% internal/core coverage floor (scripts/core-coverage.sh,
// make cover): the floor runs only in make check-all, invisible to the
// fast loop. This test runs in make check/make test and catches R1's
// dominant failure mode — an exported core symbol shipped with no L1 test
// at all — cheaply, in every build.
//
// It is announced as a proxy, not the floor, the way
// golden_sets_test.go:164-176 announces its own literal-substring proxy: it
// asserts only that each exported top-level declaration's own name appears
// somewhere in its directory's *_test.go source text. An identifier
// mentioned only in a comment satisfies it, and it says nothing about
// branch coverage — it catches exactly one thing, a name with no test
// reference anywhere in its own package (design D9, design §6 test matrix
// row 3, design §7 risk #4).
//
// Until internal/core/** holds an exported declaration, this reports
// "armed but vacuous", mirroring core-coverage.sh:102-105's own wording,
// rather than passing with a bare OK (design D10's non-empty-corpus
// guard).
func TestCoreExportedDeclsHaveTests(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	coreRoot := filepath.Join(repoRoot, "internal", "core")

	entries, err := os.ReadDir(coreRoot)
	if err != nil {
		t.Fatalf("read dir %s: %v", coreRoot, err)
	}

	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(coreRoot, e.Name()))
		}
	}
	sort.Strings(dirs)

	totalDecls := 0
	for _, dir := range dirs {
		names, testSource, err := exportedDeclsAndTestSource(dir)
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
		totalDecls += len(names)

		for _, name := range names {
			if !strings.Contains(testSource, name) {
				t.Errorf(
					"%s: exported declaration %q has no reference in any *_test.go "+
						"file in its own directory — every exported core declaration "+
						"needs an L1 test naming it (design D9)",
					dir, name,
				)
			}
		}
	}

	if totalDecls == 0 {
		t.Log("internal/core has no exported declarations yet — the presence guard is armed but vacuous (docs/06-harness.md §3)")
	}
}

// exportedDeclsAndTestSource returns the names of every exported top-level
// declaration (func, method, type, const, var) in dir's non-_test.go .go
// files, and the concatenated source text of dir's *_test.go files.
func exportedDeclsAndTestSource(dir string) (names []string, testSource string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", err
	}

	fset := token.NewFileSet()
	var testSourceBuilder strings.Builder

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())

		if strings.HasSuffix(e.Name(), "_test.go") {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, "", readErr
			}
			testSourceBuilder.Write(content)
			testSourceBuilder.WriteString("\n")
			continue
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil, "", parseErr
		}
		names = append(names, exportedTopLevelNames(file)...)
	}

	return names, testSourceBuilder.String(), nil
}

// exportedTopLevelNames returns every exported identifier declared at the
// top level of file: function and method names, and type/const/var names.
func exportedTopLevelNames(file *ast.File) []string {
	var names []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.IsExported() {
				names = append(names, d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						names = append(names, s.Name.Name)
					}
				case *ast.ValueSpec:
					for _, ident := range s.Names {
						if ident.IsExported() {
							names = append(names, ident.Name)
						}
					}
				}
			}
		}
	}
	return names
}
