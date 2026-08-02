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

// TestBrainNeverReadsTheClockDirectly is internal/brain's half of the
// clock discipline docs/06-harness.md §2 states: the current instant is
// data that arrives through ports.Clock, never something a layer helps
// itself to — mirroring TestStoreNeverReadsTheClockDirectly
// (store_no_direct_clock_read_test.go) exactly, over internal/brain
// instead of internal/store.
//
// Why a test and not a linter. `forbidigo` is scoped to internal/core/**
// alone (.golangci.yml:122-124), deliberately: a time.Now() in
// internal/brain is legal and correct, since brain is where the instant
// enters (design D4). But design D4 Layer 1 says the entry is exactly
// one call, inside CaptureService.Capture — every other file in this
// package receives now as a plain parameter, and this guard is what makes
// that a checked fact rather than a convention nobody enforces.
//
// It matches the AST, not the text, for the same reason
// store_no_direct_clock_read_test.go does: a line-based scan cannot tell
// a call from a sentence about one, and this file's own doc comments
// mention "time.Now()" more than once.
//
// This guard has no pre-implementation red: by the time it is written,
// internal/brain's only time.Now() call already lives inside
// CaptureService.Capture, which this guard does not flag (see below).
// What proves it fails for the right reason is its temporary-break check,
// run and recorded in this PR, per docs/06-harness.md §4's discipline for
// a guard with no natural red.
//
// This guard's own honest limitation: it forbids time.Now( anywhere in a
// non-test file under internal/brain/**, including inside
// CaptureService.Capture itself. That is deliberate, not an oversight —
// design D4 Layer 1 puts the one legitimate read behind a ports.Clock
// field (s.clock.Now()), never a bare time.Now() call, so this guard
// catching the literal call is still catching a real violation of the
// design, not a false positive. A future package-level Clock adapter
// wiring (cmd/nooma/) is deliberately out of this scan's tree.
//
// Non-test .go files only. A _test.go file under internal/brain/ may
// construct a real time.Time freely — an L2 test comparing a persisted
// timestamp against a fixed instant needs one to compare against, and
// this guard's own test files (this one included) are themselves inside
// test/conformance/, outside internal/brain/ entirely.
func TestBrainNeverReadsTheClockDirectly(t *testing.T) {
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
				"%s: %s.Now() — internal/brain must not read the clock directly: "+
					"the instant enters through ports.Clock, read exactly once by "+
					"CaptureService.Capture (s.clock.Now()), and is passed down as a "+
					"plain parameter from there (docs/06-harness.md §2, design D4 Layer 1)",
				fset.Position(call.Pos()), pkg.Name,
			)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/brain: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned zero non-test .go files under internal/brain/ — nothing to check yet")
	}
}
