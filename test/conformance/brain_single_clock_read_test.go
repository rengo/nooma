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

// TestBrainReadsTheClockAtMostOnceAndNeverAgainstAnInstantItAlreadyHas is
// design D4 Layer 3: the AST scan that catches the bug class proposal R9
// actually names — not a stray time.Now() (Layer 2's job,
// brain_no_direct_clock_read_test.go), but a SECOND, independent clock
// read mid operation, so one decision ends up seeing two different
// instants.
//
// It fails a non-test file under internal/brain/** on either:
//
//  1. more than one `Now()` call expression in that single file — any
//     selector call named Now, not only time.Now(): CaptureService.Capture
//     reads s.clock.Now() precisely because the real clock lives behind
//     ports.Clock, and a second s.clock.Now() (or a second field of type
//     ports.Clock added later) is exactly the shape R9 warns about — a
//     scan that only matched the literal text "time.Now(" would miss it
//     entirely.
//  2. a `Now()` call anywhere inside a FuncDecl whose own signature
//     already takes a time.Time parameter — a function that was HANDED
//     the instant and reaches for a fresh one anyway. This is the
//     dominant real-world shape of R9: captureRunner.at and everything it
//     calls take `now time.Time`, so any of them still calling `.Now()`
//     is a function ignoring the instant its own caller already gave it.
//
// Its own honest limitation, announced here as design D4 Layer 3 itself
// requires (the precedent is golden_sets_test.go:164-176, which announces
// its own literal-substring proxy): this guard does NOT catch two Now()
// reads living in two different files that together implement one logical
// operation — e.g. a helper in a second file called from inside
// CaptureService.Capture that itself reads the clock. Nothing short of a
// call-graph analysis does, and that is out of scope here. What this
// catches is the dominant form — one file, one function — which is what
// converts proposal R9 from "a review property, stated so it is known to
// be one" into a gate: docs/06-harness.md §6's own precedence rule, "if a
// rule can be an automated gate, it is a gate".
//
// This guard has no pre-implementation red: by the time it is written,
// internal/brain holds exactly one Now() call (CaptureService.Capture's
// s.clock.Now()), in a function that takes no time.Time parameter. Its
// temporary-break check, run and recorded in this PR, is what proves it
// fails for the right reason (docs/06-harness.md §4).
//
// Non-test .go files only, matching brain_no_direct_clock_read_test.go:
// an L2 test comparing a persisted timestamp against a real instant is
// exactly the kind of code a _test.go file needs to construct freely.
func TestBrainReadsTheClockAtMostOnceAndNeverAgainstAnInstantItAlreadyHas(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	fset := token.NewFileSet()

	scanned := 0
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal", "brain"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		nowCalls := 0
		ast.Inspect(file, func(n ast.Node) bool {
			if isNowCall(n) {
				nowCalls++
			}
			return true
		})
		if nowCalls > 1 {
			t.Errorf("%s: %d Now() call expressions in one file, want at most 1 — "+
				"a second clock read lets one operation see two different instants "+
				"(design D4 Layer 3, proposal R9)", path, nowCalls)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !takesTimeTimeParam(fn) {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && isNowCall(call) {
					t.Errorf("%s: %s calls Now() despite already taking a time.Time "+
						"parameter — it was handed the instant its caller read and must "+
						"use that one, not a fresh read (design D4 Layer 3, proposal R9)",
						fset.Position(call.Pos()), fn.Name.Name)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/brain: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned zero non-test .go files under internal/brain/ — nothing to check yet")
	}
}

// isNowCall reports whether n is a call expression of the shape x.Now(),
// for any x — not only time.Now(). ports.Clock.Now() is the legitimate
// call this whole package exists to bound to exactly one, so the scan must
// see it to count it.
func isNowCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Now"
}

// takesTimeTimeParam reports whether fn's signature declares a parameter
// of type time.Time — the shape that turns a Now() call inside fn's body
// from "reading the clock" into "ignoring the instant already received".
func takesTimeTimeParam(fn *ast.FuncDecl) bool {
	if fn.Body == nil || fn.Type.Params == nil {
		return false
	}
	for _, field := range fn.Type.Params.List {
		sel, ok := field.Type.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok && pkg.Name == "time" && sel.Sel.Name == "Time" {
			return true
		}
	}
	return false
}
