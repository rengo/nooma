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
//     The "returns or embeds" half of that name is literal, not
//     aspirational. The check follows an exported function's declared
//     result type into a LOCALLY declared struct type's own field types,
//     recursively (Judgment Day round 1, PR 4b), and into a LOCALLY
//     declared type ALIAS's target, recursively, resolving a chain of
//     aliases (Judgment Day round 2, PR 4b — a `type X = Y` alias is, per
//     the language spec, the identical type as Y, reflect.TypeOf equal,
//     not merely shaped like it, so it must be caught the same as a
//     function naming Y directly).
//
//     This doc comment's gap list is re-derived from scratch here, by
//     probing every bypass shape tried against the check rather than by
//     reasoning about the code — round 1's own gap list was itself
//     incomplete (it missed the alias case entirely) precisely because it
//     was reasoned rather than probed. The full probe matrix, run against
//     a scratch copy of this check with each shape added to
//     internal/core/focus one at a time and reverted after:
//
//     Caught (all confirmed by watching the subtest fail for the right
//     reason, then reverting):
//     - `func F() unit.Status` and `func F() struct{ S unit.Status }`
//     (direct, pre-existing).
//     - A named local struct wrapping unit.Status, `type wrapper
//     struct{ S unit.Status }` returned as `func F() wrapper` (round 1).
//     - A direct alias of unit.Status, `type StatusAlias = unit.Status`,
//     returned as `func F() StatusAlias` (round 2, judge B's exact form).
//     - An alias of a local wrapper struct, `type Alias = wrapper` (round 2,
//     judge A's exact form).
//     - An alias of an alias, `type A = unit.Status; type B = A`.
//     - A same-package struct declared in a DIFFERENT file of the directory
//     (localNamedTypes parses every non-test file in dir, not only the
//     one being checked).
//     - Two-level local-struct nesting (an outer local struct whose field is
//     an inner local struct that embeds unit.Status) and an anonymous
//     embedded field (`struct{ unit.Status }`) — both already exercised by
//     the same probe, since the recursive field-following and the plain
//     substring check apply identically to a named and an anonymous
//     field's rendered text.
//     - unit.Status wrapped in a slice, map, or a single pointer,
//     RETURNED DIRECTLY (`[]unit.Status`, `map[string]unit.Status`,
//     `*unit.Status`) or as a LOCAL STRUCT FIELD of one of those shapes
//     (`struct{ S []unit.Status }`) — caught in every case, because
//     go/printer renders the substring "unit.Status" literally inside the
//     wrapping syntax, and the check's own leading strings.Contains test
//     matches a substring anywhere in the rendered text, not only a bare
//     identifier. This was wrongly listed as an open gap before this
//     round; it never was one.
//
//     Confirmed NOT caught (probed, not merely reasoned about — both
//     verified to pass green with the bypass present, which is the
//     defect, so both remain recorded as gaps rather than assumed closed):
//     - A named type (struct, or an alias resolving to one) declared
//     OUTSIDE internal/core/focus and referenced only through a LOCAL
//     alias to it (e.g. `type Alias = weight.SomeWrapper` where
//     SomeWrapper, declared in another package, embeds unit.Status).
//     localNamedTypes parses only the one directory's non-test files, so
//     a type declared elsewhere is invisible to it, alias or not.
//     - A second level of pointer indirection wrapping a LOCAL named type,
//     `**wrapper`, where wrapper does not itself contain "unit.Status" in
//     its own rendered name (unlike `**unit.Status`, which the substring
//     check still catches directly). Only one leading "*" is stripped
//     before the name lookup, so `**wrapper` looks up the key `*wrapper`,
//     which is never in either map.
//
//     Deliberately never indexed, and correctly so — not a gap in the
//     sense of the two above: a defined type WITHOUT `=`,
//     `type StatusDefined unit.Status`. Probed and confirmed to pass
//     green, but this is not a bypass of the same guarantee: Go treats
//     StatusDefined as a genuinely distinct type from unit.Status
//     (reflect.TypeOf differs, and converting between them needs an
//     explicit conversion), so a function returning it is not returning
//     "a unit.Status" in the sense I01 forbids the way an alias — which IS
//     unit.Status, not merely convertible to it — would be.
//
//     Closing either of the two remaining confirmed gaps needs a
//     type-checked pass (go/types, loading the full module graph) rather
//     than the plain go/ast parse every other check in this package
//     already uses; this structural check stays go/ast-only like its
//     siblings, and both gaps are recorded here rather than assumed
//     closed.
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

			localFields, aliases := localNamedTypes(t, focusDir)
			const banned = "unit.Status"
			for i, name := range names {
				for _, resultType := range resultTypes[i] {
					if resultTypeEmbeds(resultType, banned, localFields, aliases, map[string]bool{}) {
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

// localNamedTypes parses every non-test .go file in dir and returns two
// maps describing every type declared locally, so a check on an exported
// function's declared result type can follow a LOCAL named type into what
// it actually is or embeds, rather than stopping at the bare identifier
// the way exportedFuncResultTypes' printer.Fprint alone would:
//
//   - structFields, keyed by the name of each locally declared struct
//     type, holds the rendered text of every one of that struct's field
//     types (named or embedded) — Judgment Day round 1, PR 4b: a function
//     returning `wrapper` where `type wrapper struct{ S unit.Status }` is
//     declared in the same directory must not pass silently just because
//     "wrapper" itself does not contain the banned substring.
//   - aliases, keyed by the name of each locally declared TYPE ALIAS
//     (`type X = Y`, go/ast's TypeSpec.Assign set), holds the rendered
//     text of Y — Judgment Day round 2, PR 4b: `type StatusAlias =
//     unit.Status` is, per the language spec, the identical type as
//     unit.Status (reflect.TypeOf equal), not merely shaped like it, so a
//     function returning StatusAlias must be caught exactly like one
//     returning unit.Status directly, and an alias of a local wrapper
//     struct, or an alias of an alias, must resolve all the way through.
//
// A defined type WITHOUT `=` (`type StatusDefined unit.Status`) is
// deliberately NOT indexed in either map: Go's type system treats it as a
// genuinely distinct type from unit.Status — assigning one to the other
// needs an explicit conversion, and reflect.TypeOf of the two differ — so
// it does not defeat this check in the same "same type" sense an alias
// does. Probed directly (not merely reasoned about) alongside every other
// shape this function exists to catch; see this test file's own doc
// comment for the full probe matrix.
func localNamedTypes(t *testing.T, dir string) (structFields map[string][]string, aliases map[string]string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	structFields = make(map[string][]string)
	aliases = make(map[string]string)
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

				if ts.Assign.IsValid() {
					// `type X = Y`: X IS Y, so record what it aliases
					// directly. resultTypeEmbeds resolves through this,
					// including a chain of aliases, the same way it
					// already resolves through a struct's fields.
					var sb strings.Builder
					if err := printer.Fprint(&sb, fset, ts.Type); err != nil {
						t.Fatalf("render alias target of %s: %v", ts.Name.Name, err)
					}
					aliases[ts.Name.Name] = sb.String()
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
				structFields[ts.Name.Name] = fieldTypes
			}
		}
	}
	return structFields, aliases
}

// resultTypeEmbeds reports whether resultType — the rendered text of an
// exported function's declared return type — contains banned, either
// directly, or recursively through a LOCALLY declared struct type's own
// field types (localFields), or recursively through a LOCALLY declared
// type alias's target (aliases) — both from localNamedTypes. A leading
// "*" is stripped before a name lookup, so a pointer to a local wrapper
// type or a local alias resolves the same as the type itself. visited
// guards against a self-referential or mutually recursive local type
// turning this into an infinite loop, and is shared across both maps
// since a struct name and an alias name are drawn from the same
// declaration space and cannot collide.
//
// Known gaps (probed, not merely reasoned about — see
// TestI01_FocusIsNeverAPersistedStatus's own doc comment for the full
// matrix and what closing each one would need):
//
//   - A named type (struct or alias) declared OUTSIDE the directory
//     localNamedTypes was built from. This check parses only
//     internal/core/focus's own non-test files.
//   - A second level of pointer indirection wrapping a LOCAL named type
//     that does not itself contain "unit.Status" in its rendered text
//     (e.g. **wrapper): only one leading "*" is stripped before the name
//     lookup. A slice, map, or array element, or a single level of
//     pointer indirection, wrapping unit.Status (or a local alias of it)
//     directly is NOT a gap — printer.Fprint renders "[]unit.Status",
//     "map[string]unit.Status", "*unit.Status" and similar with the
//     substring "unit.Status" still literally present in the text, so the
//     leading strings.Contains check above already catches those without
//     needing to resolve any named type at all.
//
// Closing either remaining gap needs a type-checked pass (go/types,
// loading the full module graph) rather than the plain go/ast parse every
// other check in this package already uses; this structural check stays
// go/ast-only like its siblings, and both gaps are recorded here rather
// than assumed closed.
func resultTypeEmbeds(resultType, banned string, localFields map[string][]string, aliases map[string]string, visited map[string]bool) bool {
	if strings.Contains(resultType, banned) {
		return true
	}

	name := strings.TrimPrefix(resultType, "*")
	if visited[name] {
		return false
	}
	visited[name] = true

	if target, ok := aliases[name]; ok {
		if resultTypeEmbeds(target, banned, localFields, aliases, visited) {
			return true
		}
	}

	for _, fieldType := range localFields[name] {
		if resultTypeEmbeds(fieldType, banned, localFields, aliases, visited) {
			return true
		}
	}
	return false
}
