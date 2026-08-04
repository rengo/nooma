// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestI05_WeightPackageReturnsNoUnitShapedValue proves I05's *pure* half
// (docs/06-harness.md §4, doc 02 §2: decay is computed on read, never
// written per read) as a structural property of internal/core/weight's
// exported surface, at the point this PR leaves it: no exported function
// returns unit.Unit, *unit.Unit, or []unit.Unit. A read path therefore
// holds no unit-shaped value it could hand to a repository, and
// "accidentally persist a decayed weight" has no syntax (spec R1.3,
// design D9).
//
// This is I05's structural half *started*, not finished. design D9's full
// statement also requires that Boost — the package's only persistable
// value — has exactly two producers, Revive and Resurface. Neither exists
// yet: this PR ships only Effective, ZoneOf, AllZones and Zone.String, so
// the "Boost has exactly two producers" clause is meaningless before Boost
// exists and is deliberately not asserted here. PR 2a extends this file
// with a one-producer check once Revive exists; PR 2b extends it again to
// the final two-producer check once Resurface exists. A reader who diffs
// this file against a later PR should expect it to grow, not to be
// rewritten from scratch.
//
// The full I05 guarantee — "no *read path* writes decay" — needs a store
// to prove a read path exists at all; that structural half is m2c's, once
// ports.UnitRepo exists (spec R1.3's own scope note).
func TestI05_WeightPackageReturnsNoUnitShapedValue(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	weightDir := filepath.Join(repoRoot, "internal", "core", "weight")

	names, resultTypes := exportedFuncResultTypes(t, weightDir)
	if len(names) == 0 {
		t.Fatal("internal/core/weight has zero exported functions — nothing to check (D10's non-empty-corpus guard)")
	}

	const banned = "unit.Unit"
	for i, name := range names {
		for _, resultType := range resultTypes[i] {
			if strings.Contains(resultType, banned) {
				t.Errorf(
					"%s returns %q, which contains %q — no exported function in "+
						"internal/core/weight may return a unit-shaped value; a read "+
						"path must have no value it could hand to a repository (I05, spec R1.3)",
					name, resultType, banned,
				)
			}
		}
	}
}

// exportedFuncResultTypes parses every non-test .go file in dir and
// returns, in parallel slices, each exported top-level function or
// method's qualified name and the textual rendering of each of its result
// types.
func exportedFuncResultTypes(t *testing.T, dir string) (names []string, resultTypes [][]string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || fn.Type.Results == nil {
				continue
			}

			var results []string
			for _, field := range fn.Type.Results.List {
				var sb strings.Builder
				if err := printer.Fprint(&sb, fset, field.Type); err != nil {
					t.Fatalf("render result type of %s: %v", fn.Name.Name, err)
				}
				results = append(results, sb.String())
			}

			names = append(names, fn.Name.Name)
			resultTypes = append(resultTypes, results)
		}
	}

	return names, resultTypes
}
