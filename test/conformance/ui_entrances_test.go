// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ui"
)

// TestUIEntrances_DepsExposeOnlyDeclaredMethods is design §3.7 row 3, part
// (a): ui.Deps' own interface fields — Today, Units, Search, Capture and Beliefs
// once PR 5 adds it — may together expose only the method names this gate
// whitelists. Capture is whitelisted from this PR even though nothing
// wires it yet (design §6.1: "harmless while unused") so PR 5 does not
// have to touch this gate again just to widen it.
//
// This is reflection over the TYPE ui.Deps declares, not the tree: it
// proves what a *brain.RecallService (or any future implementation) is
// ALLOWED to be asked for through this package's own seam, regardless of
// which concrete type satisfies the interface at any call site.
func TestUIEntrances_DepsExposeOnlyDeclaredMethods(t *testing.T) {
	t.Parallel()

	allowedMethods := map[string]bool{"Today": true, "Browse": true, "Detail": true, "ForText": true, "Capture": true, "ByFacet": true, "Edit": true, "Retire": true, "Page": true}

	typ := reflect.TypeOf(ui.Deps{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type.Kind() != reflect.Interface {
			continue
		}
		for m := 0; m < field.Type.NumMethod(); m++ {
			name := field.Type.Method(m).Name
			if !allowedMethods[name] {
				t.Errorf("ui.Deps.%s exposes method %q — not in this gate's declared whitelist %v; a wider interface here is a wider door into brain than I22 declares (design §3.7)", field.Name, name, allowedMethods)
			}
		}
	}
}

// TestUIEntrances_NoDirectBrainServiceReferences is design §3.7 row 3, part
// (b): every non-test file in internal/ui — the compiled package, not its
// own tests — names no `brain.<X>Service` identifier, calls no
// `brain.New...` constructor, and asserts no value to any `brain.<X>`
// type. ui.go's own TodayReader interface already returns brain.Today, an
// OUTPUT type this rule leaves alone; what it forbids is internal/ui
// reaching PAST its own declared interfaces to construct or unwrap a
// brain service directly, which would make Deps' own narrow interfaces
// decorative rather than the only door in.
func TestUIEntrances_NoDirectBrainServiceReferences(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootFromCaller(t)
	uiDir := filepath.Join(repoRoot, "internal", "ui")

	entries, err := os.ReadDir(uiDir)
	if err != nil {
		t.Fatalf("read %s: %v", uiDir, err)
	}

	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++

		path := filepath.Join(uiDir, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch expr := n.(type) {
			case *ast.SelectorExpr:
				if ident, ok := expr.X.(*ast.Ident); ok && ident.Name == "brain" && strings.HasSuffix(expr.Sel.Name, "Service") {
					t.Errorf("%s: references brain.%s — internal/ui may reach a brain service only through its own declared interfaces (Deps' fields), never the service type itself (I22, design §3.7)", path, expr.Sel.Name)
				}
			case *ast.CallExpr:
				if sel, ok := expr.Fun.(*ast.SelectorExpr); ok {
					if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "brain" && strings.HasPrefix(sel.Sel.Name, "New") {
						t.Errorf("%s: calls brain.%s — internal/ui constructs no brain service; every service it uses is wired in cmd/nooma and handed in through Deps", path, sel.Sel.Name)
					}
				}
			case *ast.TypeAssertExpr:
				if sel, ok := expr.Type.(*ast.SelectorExpr); ok {
					if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "brain" {
						t.Errorf("%s: asserts a value to brain.%s — a type assertion to a concrete brain type would let this package reach past the interface it was handed (I22, design §3.7)", path, sel.Sel.Name)
					}
				}
			}
			return true
		})
	}

	if checked == 0 {
		t.Fatal("no non-test .go file found under internal/ui — this gate's own guard: nothing to check")
	}
}

// TestUIEntrances_ReferentIDOnlyFromPath is design §3.7 row 3, part (c),
// spec R5's own structural half: brain.CaptureInput.ReferentID — the field
// that makes a correction explicit rather than falling into chat's own
// hybrid-recall/ambiguity-gate path (doc 02 §5 step 4) — is set from
// exactly one place in the compiled package: a composite literal whose
// `ReferentID:` value is the call expression r.PathValue("id") literally,
// the correction route's own path parameter. Two other shapes are checked
// for and fail the gate outright: a second composite literal also setting
// `ReferentID:` (to anything, including the same expression again — a
// capture-route literal that ALSO sets ReferentID from a submitted
// unit_id would land here, not only a submitted-unit_id shape
// specifically), and any assignment statement — including a
// compound-assignment form (+=, etc.) — whose left-hand side is a
// `.ReferentID` selector, however it is reached (`in.ReferentID = ...`,
// `input.ReferentID = ...`).
//
// This gate is deliberately RED before this PR's own correction handler
// lands: a fresh checkout has ZERO ReferentID references anywhere in
// internal/ui, and wantReferentIDLiterals == 0 fails loudly by the same
// vacuity-guard shape every other gate in this file uses, rather than
// silently reporting "0 violations found" as if the property were already
// satisfied — the i22/wiring gates' own precedent (design §3.7, PR 3/4).
func TestUIEntrances_ReferentIDOnlyFromPath(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootFromCaller(t)
	uiDir := filepath.Join(repoRoot, "internal", "ui")

	entries, err := os.ReadDir(uiDir)
	if err != nil {
		t.Fatalf("read %s: %v", uiDir, err)
	}

	var literalCount int
	var assignCount int
	checked := 0

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++

		path := filepath.Join(uiDir, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch expr := n.(type) {
			case *ast.CompositeLit:
				for _, elt := range expr.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "ReferentID" {
						continue
					}
					literalCount++
					if !isPathValueIDCall(kv.Value) {
						t.Errorf("%s: ReferentID: %s — the only value this gate allows is r.PathValue(\"id\") (spec R5, design §3.7)", path, normalizedText(fset, kv.Value))
					}
				}
			case *ast.AssignStmt:
				for _, lhs := range expr.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "ReferentID" {
						continue
					}
					assignCount++
					t.Errorf("%s: assigns %s — ReferentID may only be set inside a brain.CaptureInput composite literal, never by assignment after construction (spec R5, design §3.7)", path, normalizedText(fset, lhs))
				}
			}
			return true
		})
	}

	if checked == 0 {
		t.Fatal("no non-test .go file found under internal/ui — this gate's own guard: nothing to check")
	}
	if literalCount == 0 {
		t.Fatal("no ReferentID: composite literal found anywhere in internal/ui — this gate's own vacuity guard: nothing to check yet (expected before the correction route lands)")
	}
	if literalCount != 1 {
		t.Errorf("found %d ReferentID: composite literals, want exactly 1 — the correction route's own", literalCount)
	}
	if assignCount != 0 {
		t.Errorf("found %d ReferentID assignment(s) outside a composite literal, want 0", assignCount)
	}
}

// isPathValueIDCall reports whether e is exactly the call expression
// r.PathValue("id") — the correction route's own request-scoped receiver
// name, matching every handler in this package (serveUnit, serveCorrect).
func isPathValueIDCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "PathValue" {
		return false
	}
	if recv, ok := sel.X.(*ast.Ident); !ok || recv.Name != "r" {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(lit.Value)
	return err == nil && value == "id"
}
