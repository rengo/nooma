// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestI04_ArmingNeverProducesAUnit is I04's pure half applied to
// internal/core/prospection: arming plans a trigger or a timer, and a timer
// is NEVER a unit (doc 02 §8).
//
// It lives at L2 rather than beside the code it guards, and that is forced
// rather than chosen: spec R6.1 asks for this as an L1 assertion, but
// depguard's own core-purity rule forbids `os` anywhere under
// internal/core — including its tests — so an L1 scan cannot read the
// files it would need to. The rule is right and the spec's layer was
// optimistic. Recorded as Finding F10.
//
// The scan reads syntax rather than behaviour, so it fails on a
// declaration instead of waiting for a call that constructs one. go/parser
// rather than a string search, because this test's own doc comment names
// unit.Unit twice: a scan defeated by its own documentation is the defect
// this repository already corrected once, in the scheduler boundary scan.
func TestI04_ArmingNeverProducesAUnit(t *testing.T) {
	const pkgDir = "../../internal/core/prospection"

	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("reading %s: %v", pkgDir, err)
	}

	fset := token.NewFileSet()
	var checked int

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(pkgDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Type.Results == nil {
				continue
			}
			checked++
			for _, result := range fn.Type.Results.List {
				if typ := types.ExprString(result.Type); strings.Contains(typ, "unit.") {
					t.Errorf("%s: %s returns %s — arming decides a trigger or a timer, and a "+
						"timer is NEVER a unit (doc 02 §8, I04). This package plans; it does "+
						"not construct what gets stored", name, fn.Name.Name, typ)
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("the scan inspected no function signatures at all — it would pass on an empty " +
			"package, which is not the same as passing on a correct one")
	}
}
