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

// timeFreeFuncs is the set of internal/core/consolidation's exported
// functions R0.1 requires to take no time.Time parameter at all: doc 02's
// "accumulated evidence" phases — the ones that reason only about strength
// or confidence already crossed some threshold, never about how much time
// has elapsed since — need no instant to decide (spec R0.1, design.md
// §5.4). MergeProposals and Reinforce do not exist yet (PR 4's
// derive.go); this scaffold is extended there (task 4.22), the same
// scaffold-then-extend shape m2a's own i05 test used (m2a-weight-focus
// task 1.5).
var timeFreeFuncs = map[string]bool{
	"Strengthen": true,
}

// TestR01_TimeFreeFunctionsTakeNoTimeParameter proves R0.1's first of
// three: Strengthen has no time.Time parameter among
// internal/core/consolidation's exported functions.
//
// Not a missing-symbol RED: Strengthen already compiles and passes its own
// tests, added earlier in this same PR — disclosed per this project's own
// convention (m2a C9) as a structural pin, not a TDD red step.
func TestR01_TimeFreeFunctionsTakeNoTimeParameter(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	dir := filepath.Join(repoRoot, "internal", "core", "consolidation")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	found := map[string]bool{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		path := filepath.Join(dir, name)
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			if !timeFreeFuncs[fn.Name.Name] {
				continue
			}
			found[fn.Name.Name] = true

			if fn.Type.Params == nil {
				continue
			}
			for _, field := range fn.Type.Params.List {
				if isTimeTimeType(field.Type) {
					t.Errorf("%s (%s) has a time.Time parameter — R0.1 requires it take none: this is one of doc 02's "+
						"accumulated-evidence decisions, which reasons about a threshold already crossed, never about "+
						"an instant", fn.Name.Name, path)
				}
			}
		}
	}

	for name := range timeFreeFuncs {
		if !found[name] {
			t.Errorf("expected to find function %q declared in %s, found none — nothing was checked", name, dir)
		}
	}
}

// isTimeTimeType reports whether expr is exactly the type time.Time BY
// VALUE, referenced through its package selector. It deliberately does
// NOT match *time.Time (an *ast.StarExpr wrapping the same selector):
// R0.1 forbids the instant travelling into these functions as a value,
// not a resolved-absence sentinel. Strengthen's own since *time.Time
// parameter is exactly that sentinel — nil means "never consolidated",
// the same shape focus.ResolveMargin(configured *float64) already uses —
// and design.md §5.4 lists Strengthen among the functions that "take no
// clock at all" for this reason. Do not widen this to unwrap StarExpr:
// that would fail a signature R0.1 and design.md §5.4 both call correct.
func isTimeTimeType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "time" && sel.Sel.Name == "Time"
}
