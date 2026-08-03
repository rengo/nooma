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

// updateMethodNames is the closed set of ports.UnitRepo methods ADR-0016's
// ordering guards — design D5 Layer 2.
var updateMethodNames = map[string]bool{
	"UpdateContent": true,
	"UpdateEventAt": true,
	"UpdateDueAt":   true,
}

// TestCorrectionAuditPrecedesEveryUpdate is design D5 Layer 2, the AST
// mechanization of ADR-0016's structural guarantee ("applyWithPreImage is
// the ONLY path ... to a ports.UnitRepo Update* method"). It fails a
// non-test file under internal/**, excluding internal/store/** and
// internal/ports/** (which declare and implement the methods rather than
// calling them), on either:
//
//  1. a call to UpdateContent, UpdateEventAt or UpdateDueAt outside a
//     function named dispatchEdits; or
//  2. applyWithPreImage's own body calling dispatchEdits at a position
//     that is not strictly after recordPreImage — including either call
//     being absent.
//
// No pre-implementation red exists for this guard (task 12f-i.2, the same
// category test/conformance/brain_single_clock_read_test.go established
// for design D4 Layer 3): by the time it is written, internal/brain/
// correction.go already holds exactly the shape it checks. What proves it
// fails for the right reason is the temporary-break check performed and
// reverted for this PR (recorded in the commit message), not a red run of
// this test itself.
//
// Its own honest limitation, matching golden_sets_test.go's own
// announced-proxy precedent: matching is by identifier name only, not by
// receiver type, and statement order is decided only for calls that are
// direct children of applyWithPreImage's top-level statement list — a call
// reached only through a nested closure, or both names found inside one
// shared top-level statement (e.g. opposite branches of one if/else), are
// shapes this guard cannot safely resolve by position alone. Rather than
// guess, it fails loudly naming the shape it could not read, per this PR's
// own governing instruction to refuse rather than pass by not understanding
// what it saw.
func TestCorrectionAuditPrecedesEveryUpdate(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	fset := token.NewFileSet()

	scanned := 0
	foundApplyFn := false
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, repoRoot+string(filepath.Separator))
		if strings.HasPrefix(rel, filepath.Join("internal", "store")+string(filepath.Separator)) ||
			strings.HasPrefix(rel, filepath.Join("internal", "ports")+string(filepath.Separator)) {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if fn.Name.Name != "dispatchEdits" {
				checkNoUpdateCall(t, fset, fn)
			}
			if fn.Name.Name == "applyWithPreImage" {
				foundApplyFn = true
				checkAuditBeforeEdit(t, fn)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned zero non-test .go files under internal/ — nothing to check yet")
	}
	if !foundApplyFn {
		t.Fatal("no applyWithPreImage function found anywhere under internal/ — design D5 Layer 1 is not implemented yet")
	}
}

// checkNoUpdateCall fails fn if its body calls an UpdateContent/
// UpdateEventAt/UpdateDueAt-named selector anywhere, including inside a
// nested closure — fn is already known not to be named dispatchEdits.
func checkNoUpdateCall(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl) {
	t.Helper()
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if name, ok := selectorCallName(n); ok && updateMethodNames[name] {
			t.Errorf("%s: %s calls %s outside dispatchEdits — design D5 Layer 2's one door (ADR-0016)",
				fset.Position(n.Pos()), fn.Name.Name, name)
		}
		return true
	})
}

// checkAuditBeforeEdit fails fn (already known to be named
// applyWithPreImage) unless recordPreImage and dispatchEdits are each
// called exactly once, as direct (non-closure) descendants of exactly one
// top-level statement apiece, with recordPreImage's statement strictly
// before dispatchEdits's.
func checkAuditBeforeEdit(t *testing.T, fn *ast.FuncDecl) {
	t.Helper()

	var preIdx, editIdx = -1, -1
	for i, stmt := range fn.Body.List {
		direct, closure := scanForCalls(stmt, "recordPreImage", "dispatchEdits")
		if closure["recordPreImage"] {
			t.Fatalf("applyWithPreImage: recordPreImage is called only inside a closure — statement order cannot be determined safely")
		}
		if closure["dispatchEdits"] {
			t.Fatalf("applyWithPreImage: dispatchEdits is called only inside a closure — statement order cannot be determined safely")
		}
		if direct["recordPreImage"] && direct["dispatchEdits"] {
			t.Fatalf("applyWithPreImage: recordPreImage and dispatchEdits both appear in the same top-level statement (index %d) — cannot determine sequential order without deeper branch analysis", i)
		}
		if direct["recordPreImage"] {
			if preIdx != -1 {
				t.Fatalf("applyWithPreImage: recordPreImage is called more than once (statements %d and %d)", preIdx, i)
			}
			preIdx = i
		}
		if direct["dispatchEdits"] {
			if editIdx != -1 {
				t.Fatalf("applyWithPreImage: dispatchEdits is called more than once (statements %d and %d)", editIdx, i)
			}
			editIdx = i
		}
	}

	if preIdx == -1 {
		t.Fatal("applyWithPreImage: recordPreImage is never called — ADR-0016's pre-image would never be written")
	}
	if editIdx == -1 {
		t.Fatal("applyWithPreImage: dispatchEdits is never called — no edit would ever be applied")
	}
	if editIdx < preIdx {
		t.Fatalf("applyWithPreImage: dispatchEdits (statement %d) runs before recordPreImage (statement %d) — ADR-0016's ordering is violated", editIdx, preIdx)
	}
}

// scanForCalls inspects stmt for calls to any of names, distinguishing a
// direct call (no ast.FuncLit ancestor within stmt) from one reachable only
// through a nested closure. It tracks ancestry with an explicit stack built
// from ast.Inspect's own pre/post-order pair (a non-nil node is a visit, a
// following nil is that node's own post-visit marker — go/ast's documented
// contract for Inspect), because a single forward-only pass cannot tell
// when a closure's subtree has ended.
func scanForCalls(stmt ast.Stmt, names ...string) (direct, closure map[string]bool) {
	direct = make(map[string]bool, len(names))
	closure = make(map[string]bool, len(names))
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}

	var stack []bool // one entry per open ancestor frame; true iff that frame is a FuncLit
	inClosure := func() bool {
		for _, isFuncLit := range stack {
			if isFuncLit {
				return true
			}
		}
		return false
	}

	ast.Inspect(stmt, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		_, isFuncLit := n.(*ast.FuncLit)
		stack = append(stack, isFuncLit)

		if name, ok := selectorCallName(n); ok && want[name] {
			if inClosure() {
				closure[name] = true
			} else {
				direct[name] = true
			}
		}
		return true
	})
	return direct, closure
}

// selectorCallName reports the method name of a call expression of the
// shape x.Name(...), for any x — matching brain_single_clock_read_test.go's
// own isNowCall convention (any selector, not a specific receiver type).
func selectorCallName(n ast.Node) (string, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	return sel.Sel.Name, true
}
