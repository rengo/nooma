// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/unit"
)

// TestI01_FocusIsNeverAPersistedStatus proves invariant I01
// (docs/02-cognitive-core.md §3): "being in focus" is a computed view over
// the pool — sort by effective priority, take the top N — never a stored
// status. status='focus' must never exist as a member of the unit status
// vocabulary.
//
// unit.Status and unit.AllStatuses (internal/core/unit) now exist —
// promoted into the untagged L2 suite by the same PR that added them
// (spec R7.1, design D8), per the ordering internal/core/unit/doc.go used
// to anchor before this test's promotion removed that paragraph.
//
// Three independent checks, mirroring docs/06-harness.md §4's own framing
// ("a test that fails if the literal 'focus' appears as a status value in
// the tree"):
//
//  1. Vocabulary: no member of unit.AllStatuses() is "focus".
//
//  2. Tree scan: no Go source file under internal/ or cmd/ assigns the
//     literal "focus" to something named Status. This is a coarse,
//     line-based heuristic (like i13_learning_signal_test.go's migration
//     scan), not a type-checked one — deliberately, so it needs no import
//     of go/ast to reason about a status literal it does not yet know the
//     shape of. Go source only: migrations are .sql files, embedded via
//     the go:embed directive, and are naturally outside this scan (design D1).
//
//  3. Structural, added PR 4b once a package literally named focus exists
//     with a real corpus for the first time (spec R4.2, R4.6; design D9 —
//     "I01 made a property of the API"): no exported function in
//     internal/core/focus returns or embeds a unit.Status;
//     focus.Selection.Members is []string, unit ids, never units
//     themselves; and internal/core/focus declares no package-level var.
//     Not a missing-symbol red step — by this point in the chain the
//     structural guarantees already hold (4a and 4b.2 ship the correct
//     shapes) — this is the permanent proof, not a step toward one.
//
//     The "returns or embeds" half of that name is literal, not aspirational
//     (Judgment Day round 1, PR 4b): the check follows an exported
//     function's declared result type into a LOCALLY declared struct
//     type's own field types, recursively, so
//     `func F() struct{ S unit.Status }` and a named
//     `type wrapper struct{ S unit.Status }` returned as `func F() wrapper`
//     are both caught, not only a function that names unit.Status directly.
//     Still unchecked, and left that way rather than silently: a named type
//     declared OUTSIDE internal/core/focus (this check parses only that
//     one directory's non-test files), and a unit.Status wrapped inside a
//     slice, map, array, or pointer-to-pointer element rather than a bare
//     struct field. Closing either gap needs a type-checked pass
//     (go/types, loading the full module graph) rather than the plain
//     go/ast parse every other check in this package already uses; this
//     structural check stays go/ast-only like its siblings, and the two
//     gaps are recorded here rather than assumed closed.
//
// All three checks apply design D10's non-empty-corpus guard: assert a
// non-empty corpus was found before asserting anything about its content,
// so a moved or renamed directory cannot turn this test vacuously green.
func TestI01_FocusIsNeverAPersistedStatus(t *testing.T) {
	t.Run("Status is a string-kind vocabulary type", func(t *testing.T) {
		// Referenced directly (not only through AllStatuses' return type) so
		// this file's RED named both anchored symbols, unit.Status and
		// unit.AllStatuses, independently of one another, back when this
		// test was itself pendingimpl-tagged (Phase A; both symbols have
		// existed since, and the pending-red gate that watched this RED is
		// now retired — m1b-pipeline PR 8a).
		var zero unit.Status
		if reflect.TypeOf(zero).Kind() != reflect.String {
			t.Errorf("unit.Status has kind %s, want a string-kind vocabulary type", reflect.TypeOf(zero).Kind())
		}
	})

	t.Run("vocabulary", func(t *testing.T) {
		statuses := unit.AllStatuses()
		if len(statuses) == 0 {
			t.Fatal("unit.AllStatuses() returned zero statuses — D10's guard: nothing to check yet")
		}
		for _, s := range statuses {
			if string(s) == "focus" {
				t.Errorf(
					"unit.AllStatuses() includes %q — focus is a computed view "+
						"(docs/02-cognitive-core.md §3: sort the pool by priority, take "+
						"the top N), never a persisted unit.Status",
					s,
				)
			}
		}
	})

	t.Run("tree scan", func(t *testing.T) {
		repoRoot := repoRootFromCaller(t)
		report := func(path string, lineNum int, line string) {
			t.Errorf(
				"%s:%d: %q — status='focus' must never be a persisted value "+
					"(docs/02-cognitive-core.md §3: focus is a computed view, not a stored status)",
				path, lineNum, strings.TrimSpace(line),
			)
		}

		scanned := scanGoTree(t, filepath.Join(repoRoot, "internal"), isFocusStatusLiteral, report)
		scanned += scanGoTree(t, filepath.Join(repoRoot, "cmd"), isFocusStatusLiteral, report)
		if scanned == 0 {
			t.Fatal("scanned zero .go files under internal/ and cmd/ — D10's guard: nothing to check yet")
		}
	})

	t.Run("focus package structural guarantees", func(t *testing.T) {
		repoRoot := repoRootFromCaller(t)
		focusDir := filepath.Join(repoRoot, "internal", "core", "focus")

		t.Run("no exported function returns or embeds a unit.Status", func(t *testing.T) {
			names, resultTypes := exportedFuncResultTypes(t, focusDir)
			if len(names) == 0 {
				t.Fatal("internal/core/focus has zero exported functions — nothing to check (D10's non-empty-corpus guard)")
			}

			localFields := localStructFieldTypes(t, focusDir)
			const banned = "unit.Status"
			for i, name := range names {
				for _, resultType := range resultTypes[i] {
					if resultTypeEmbeds(resultType, banned, localFields, map[string]bool{}) {
						t.Errorf(
							"%s returns %q, which contains or embeds %q — focus is a computed view "+
								"(docs/02-cognitive-core.md §3), never a persisted unit.Status (I01, R4.2)",
							name, resultType, banned,
						)
					}
				}
			}
		})

		t.Run("Selection.Members is []string", func(t *testing.T) {
			var zero focus.Selection
			got := reflect.TypeOf(zero.Members)
			want := reflect.TypeOf([]string(nil))
			if got != want {
				t.Errorf(
					"focus.Selection.Members has type %v, want %v — Members is unit ids, "+
						"never units themselves, since a []unit.Unit would be a persistable "+
						"shape and would put I01 one careless repository call away (R4.1, R4.2)",
					got, want,
				)
			}
		})

		t.Run("no package-level var", func(t *testing.T) {
			names := focusPackageLevelVarNames(t, focusDir)
			if len(names) != 0 {
				t.Errorf(
					"internal/core/focus declares package-level var(s) %v — R4.2 and R4.6 "+
						"forbid package-level mutable state: the previous focus is a parameter, "+
						"never held in a var, sync.Map, or init-time state",
					names,
				)
			}
		})
	})
}

// focusPackageLevelVarNames returns the name of every package-level `var`
// declared in dir's non-test .go files (R4.2, R4.6): internal/core/focus is
// pure and stateless, so it must declare none — golangci-lint alone would
// not catch this, since the stdlib's own sync package is unrestricted, so
// this structural scan is what makes it a property rather than a review
// habit (spec R4.6's own "Verified by").
func focusPackageLevelVarNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	var names []string
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
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				names = append(names, identNames(vs.Names)...)
			}
		}
	}
	return names
}

// identNames returns the Name of every *ast.Ident in idents, in order.
func identNames(idents []*ast.Ident) []string {
	names := make([]string, len(idents))
	for i, ident := range idents {
		names[i] = ident.Name
	}
	return names
}

// isFocusStatusLiteral reports whether line carries both the literal
// "focus" and the substring "Status" — a coarse proxy for "a Status field
// or constant is being set to focus", per docs/06-harness.md §4's own
// framing.
func isFocusStatusLiteral(line string) bool {
	return strings.Contains(line, `"focus"`) && strings.Contains(line, "Status")
}

// localStructFieldTypes parses every non-test .go file in dir and returns,
// keyed by the name of each locally declared struct type, the rendered
// text of every one of that struct's field types (named or embedded) — so
// a check on an exported function's declared result type can follow a
// LOCAL named type into what it actually embeds, rather than stopping at
// the bare identifier the way exportedFuncResultTypes' printer.Fprint
// alone would (Judgment Day round 1, PR 4b: a function returning
// `wrapper` where `type wrapper struct{ S unit.Status }` is declared in
// the same directory must not pass silently just because "wrapper" itself
// does not contain the banned substring).
func localStructFieldTypes(t *testing.T, dir string) map[string][]string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	fields := make(map[string][]string)
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
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}

				var fieldTypes []string
				for _, field := range st.Fields.List {
					var sb strings.Builder
					if err := printer.Fprint(&sb, fset, field.Type); err != nil {
						t.Fatalf("render field type of %s: %v", ts.Name.Name, err)
					}
					fieldTypes = append(fieldTypes, sb.String())
				}
				fields[ts.Name.Name] = fieldTypes
			}
		}
	}
	return fields
}

// resultTypeEmbeds reports whether resultType — the rendered text of an
// exported function's declared return type — contains banned, either
// directly or, recursively, through a LOCALLY declared struct type's own
// field types (localFields, from localStructFieldTypes). A leading "*" is
// stripped before a name lookup, so a pointer to a local wrapper type
// resolves the same as the type itself. visited guards against a
// self-referential or mutually recursive local type turning this into an
// infinite loop.
//
// Known gap (recorded in TestI01_FocusIsNeverAPersistedStatus's own doc
// comment, not silently assumed away): this only follows named types
// declared in the same directory localFields was built from, and only
// through a direct struct field — not through a slice, map, array, or
// second level of pointer indirection wrapping the named type, and not
// through a type declared in another package. Closing those needs a
// type-checked pass (go/types), not a plain go/ast parse.
func resultTypeEmbeds(resultType, banned string, localFields map[string][]string, visited map[string]bool) bool {
	if strings.Contains(resultType, banned) {
		return true
	}

	name := strings.TrimPrefix(resultType, "*")
	if visited[name] {
		return false
	}
	visited[name] = true

	for _, fieldType := range localFields[name] {
		if resultTypeEmbeds(fieldType, banned, localFields, visited) {
			return true
		}
	}
	return false
}
