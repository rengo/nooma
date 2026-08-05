// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
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
// # The "returns or embeds" check's own history
//
// This check has been rewritten three times across three Judgment Day
// rounds, and the rewrite history matters more here than in most
// conformance tests, because each round's fix turned out to still be a
// go/ast-TEXT check reasoning about a substring, and each time that method
// found a new blind spot rather than closing the class of them:
//
//   - Round 1 found a LOCAL named struct wrapping unit.Status escaping a
//     check that looked only at the bare identifier of a function's
//     declared result type. Fixed by following a local struct's own field
//     types, recursively.
//   - Round 2 found a type ALIAS (`type X = Y`) escaping — an alias IS its
//     target type (identical, not merely shaped like it), which the
//     round-1 fix never resolved. Fixed by adding a second, alias-specific
//     resolution table alongside the struct-field one.
//   - Round 3, independently by both judges, found that the whole APPROACH
//     was the defect: the round-1/round-2 machinery indexed only
//     `*ast.StructType` declarations and `Assign`-valid aliases, so every
//     defined type whose underlying kind is a slice, map, pointer, channel,
//     func, or interface was invisible to it — an entire CATEGORY, not one
//     more shape to add to a list. `type X []unit.Status`,
//     `type X map[string]unit.Status`, `type X *unit.Status`,
//     `type X chan unit.Status`, `type X func() unit.Status`, and
//     `type X interface{ Get() unit.Status }` all passed this check green,
//     with zero conversion needed to extract a real unit.Status from any of
//     them. A generics bypass (a generic struct instantiated with
//     unit.Status) was also found as a theoretical gap the same round.
//
// Round 3's own ruling, not merely another patch: Go's type-expression
// space cannot be covered by a hand-maintained substring/name list — each
// round found a new shape because the METHOD (rendered-text matching
// against an enumerated set of "kinds I thought to index") cannot be
// complete by construction. This version replaces that method entirely
// with a real type-checked pass over go/types' resolved type graph
// (typeCanYieldWithoutConversion, below): named types, aliases, struct
// fields (named and embedded), slice/array/map/pointer/channel element
// types, func signature results, interface method results, and generic
// type arguments all fall out of a handful of type-switch cases over
// go/types' own type kinds, rather than out of an enumerated list of
// syntax shapes someone had to think of first. There is no longer a
// separate alias-resolution table: go/types already resolves `type X = Y`
// to the identical object Y itself is, so an alias needs no special case
// at all — the round-2 fix's whole existence was itself a symptom of
// working from rendered AST text instead of resolved types.
//
// A defined type WITHOUT `=` (`type StatusDefined unit.Status`) is still,
// correctly, NOT flagged: Go's own type-identity rule makes it a
// genuinely distinct type from unit.Status (converting between them needs
// an explicit conversion, and go/types' own types.Identical treats the two
// as unequal), so a function returning it is not returning "a unit.Status"
// in the sense this invariant forbids. This exclusion is unchanged from
// round 2 — only its scope was ever wrong, never its own reasoning.
//
// Verification: TestI01TypecheckFixture_KnownShapes below is this check's
// permanent regression fixture (testdata/i01_typecheck_probe/fixture.go),
// pinning the shapes that mattered most so a future refactor of
// typeCanYieldWithoutConversion cannot silently reopen any of the three
// rounds' findings. The FULL probe matrix this round actually ran — every
// shape added to a throwaway copy of internal/core/focus (and, for the
// outside-package case, internal/core/weight) one at a time, watched, then
// reverted — is recorded in this change's own apply-progress record
// (`sdd/m2a-weight-focus/apply-progress`) and openspec/changes/
// m2a-weight-focus/tasks.md, not restated here: a go/ast-text doc comment
// that promised more than the code enforced is exactly what round 2 (C-series
// citations across this same change) kept finding, and a type-checked pass
// is trusted here on the strength of what actually ran, not on a prose
// restatement of it.
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
			modulePath := moduleImportPath(t, repoRoot)
			fset := token.NewFileSet()
			imp := newModuleImporter(fset, repoRoot, modulePath)

			unitPkg, err := imp.Import(modulePath + "/internal/core/unit")
			if err != nil {
				t.Fatalf("type-check internal/core/unit: %v", err)
			}
			statusObj := unitPkg.Scope().Lookup("Status")
			if statusObj == nil {
				t.Fatal("internal/core/unit has no exported Status type — nothing to check against")
			}
			banned := statusObj.Type()

			focusPkg, err := imp.Import(modulePath + "/internal/core/focus")
			if err != nil {
				t.Fatalf("type-check internal/core/focus: %v", err)
			}

			scope := focusPkg.Scope()
			var checked int
			for _, name := range scope.Names() {
				if !ast.IsExported(name) {
					continue
				}
				fn, ok := scope.Lookup(name).(*types.Func)
				if !ok {
					// Package scope only ever holds top-level declarations;
					// methods live on their receiver's method set, never
					// here, so this loop already only ever sees top-level
					// functions — same scope as the previous exported-func
					// check.
					continue
				}
				checked++

				sig, ok := fn.Type().(*types.Signature)
				if !ok {
					t.Fatalf("%s's type is %T, want *types.Signature", name, fn.Type())
				}
				results := sig.Results()
				for i := 0; i < results.Len(); i++ {
					resultType := results.At(i).Type()
					if typeCanYieldWithoutConversion(resultType, banned, map[types.Type]bool{}) {
						t.Errorf(
							"%s's result %d (%s) can yield a %s without an explicit "+
								"conversion — focus is a computed view "+
								"(docs/02-cognitive-core.md §3), never a persisted "+
								"unit.Status (I01, R4.2)",
							name, i, resultType, banned,
						)
					}
				}
			}
			if checked == 0 {
				t.Fatal("internal/core/focus has zero exported functions — nothing to check (D10's non-empty-corpus guard)")
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

// TestI01TypecheckFixture_KnownShapes is the permanent regression fixture
// this test file's own doc comment promises: it runs the exact same
// machinery ("no exported function returns or embeds a unit.Status" runs
// above) against testdata/i01_typecheck_probe/fixture.go, a synthetic
// package holding the shapes that mattered most across all three Judgment
// Day rounds on this check, and pins which of its exported functions must
// be flagged and which must not.
//
// This exists so a future refactor of typeCanYieldWithoutConversion
// regresses HERE, in a test that runs every time `make check`/`make
// check-all` runs, rather than only being caught by re-running a
// probe-and-revert matrix by hand against internal/core/focus — the
// discipline this exact check's own history (three rounds finding the same
// root cause in three different shapes) shows is not sufficient on its
// own. The fixture package lives under testdata/ specifically so it is
// never part of the real build (Go's own testdata convention), yet is
// still real, parseable, type-checked Go source this test can point
// go/types at directly.
//
// mutuallyA/mutuallyB and selfReferential (in the fixture) are what proves
// termination, not merely correctness: if typeCanYieldWithoutConversion's
// cycle guard regressed, this test would hang or stack-overflow rather
// than merely report a wrong boolean — Go's own test timeout is the
// backstop.
func TestI01TypecheckFixture_KnownShapes(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	modulePath := moduleImportPath(t, repoRoot)
	fset := token.NewFileSet()
	imp := newModuleImporter(fset, repoRoot, modulePath)

	unitPkg, err := imp.Import(modulePath + "/internal/core/unit")
	if err != nil {
		t.Fatalf("type-check internal/core/unit: %v", err)
	}
	statusObj := unitPkg.Scope().Lookup("Status")
	if statusObj == nil {
		t.Fatal("internal/core/unit has no exported Status type — nothing to check against")
	}
	banned := statusObj.Type()

	fixturePkg, err := imp.Import(modulePath + "/testdata/i01_typecheck_probe")
	if err != nil {
		t.Fatalf("type-check testdata/i01_typecheck_probe: %v", err)
	}

	// want pins, per exported fixture function, whether
	// typeCanYieldWithoutConversion must report true (a real bypass) or
	// false (correctly excluded) for its result type(s). See
	// testdata/i01_typecheck_probe/fixture.go's own doc comment for what
	// each shape proves.
	want := map[string]bool{
		"ReturnsDirect":                      true,
		"ReturnsCaughtSlice":                 true,
		"ReturnsCaughtMap":                   true,
		"ReturnsCaughtPointer":               true,
		"ReturnsCaughtChan":                  true,
		"ReturnsCaughtFunc":                  true,
		"ReturnsCaughtInterface":             true,
		"ReturnsCaughtStructField":           true,
		"ReturnsNotFlagged":                  false,
		"ReturnsAliasOfWrapper":              true,
		"ReturnsSelfReferentialNoStatus":     false,
		"ReturnsMutuallyRecursiveWithStatus": true,
		"ReturnsGenericDirect":               true,
		"ReturnsGenericViaAlias":             true,
		"ReturnsSecondOfTwo":                 true,
	}

	scope := fixturePkg.Scope()
	var checked int
	for name, wantFlagged := range want {
		obj := scope.Lookup(name)
		fn, ok := obj.(*types.Func)
		if !ok {
			t.Fatalf(
				"testdata/i01_typecheck_probe has no exported func %s — fixture and this "+
					"test's own table have drifted apart",
				name,
			)
		}
		checked++

		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			t.Fatalf("%s's type is %T, want *types.Signature", name, fn.Type())
		}

		var gotFlagged bool
		results := sig.Results()
		for i := 0; i < results.Len(); i++ {
			if typeCanYieldWithoutConversion(results.At(i).Type(), banned, map[types.Type]bool{}) {
				gotFlagged = true
				break
			}
		}

		if gotFlagged != wantFlagged {
			t.Errorf(
				"%s: typeCanYieldWithoutConversion(...) = %v, want %v",
				name, gotFlagged, wantFlagged,
			)
		}
	}
	if checked != len(want) {
		t.Fatalf(
			"checked %d of %d fixture functions this test's table names — fixture and table "+
				"have drifted apart",
			checked, len(want),
		)
	}
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

// moduleImportPath returns the module path declared in repoRoot/go.mod
// (e.g. "github.com/rengo/nooma"), read directly rather than hardcoded, so
// a future module rename cannot make this check silently resolve the wrong
// packages.
func moduleImportPath(t *testing.T, repoRoot string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("go.mod has no module directive")
	return ""
}

// moduleImporter is a types.Importer scoped to exactly this module: any
// import path under modulePath resolves to a source directory inside
// repoRoot and is parsed and type-checked directly, recursively through
// this same importer, so a chain of same-module imports resolves the same
// way it would under `go build`. Every other import path — for
// internal/core/focus and internal/core/unit, the standard library only
// (math, sort, time) — is handed to go/importer's default compiler-native
// importer, which reads precompiled export data from the local Go
// installation and needs no network access.
//
// This is the choice this check's own owner ruling asked to be reported
// explicitly: golang.org/x/tools/go/packages is the more ergonomic,
// module-aware way to load and type-check a package, but it is a new
// go.mod dependency this module does not otherwise need. Plain go/types
// with a hand-rolled importer needed roughly 40 lines because
// internal/core/focus's own import graph is small and fully known ahead of
// time — exactly two same-module packages (unit, weight) plus three
// stdlib ones — not because this technique generalizes to an arbitrary
// package graph. A check that needed to resolve an arbitrary, unbounded
// set of module dependencies would not get away with this; this one does,
// because the package under test is internal/core/focus specifically.
type moduleImporter struct {
	fset       *token.FileSet
	repoRoot   string
	modulePath string
	fallback   types.Importer
	cache      map[string]*types.Package
}

// newModuleImporter returns a moduleImporter rooted at repoRoot for
// modulePath, with fset as the shared token.FileSet every parsed file in
// this module is registered against (a single FileSet is required so
// position information stays comparable across packages during one
// type-check run).
func newModuleImporter(fset *token.FileSet, repoRoot, modulePath string) *moduleImporter {
	return &moduleImporter{
		fset:       fset,
		repoRoot:   repoRoot,
		modulePath: modulePath,
		fallback:   importer.Default(),
		cache:      make(map[string]*types.Package),
	}
}

// Import implements types.Importer.
func (imp *moduleImporter) Import(path string) (*types.Package, error) {
	return imp.ImportFrom(path, "", 0)
}

// ImportFrom implements types.ImporterFrom. srcDir and mode are unused:
// every import this test ever issues is absolute (module-path-qualified),
// never relative, so srcDir carries no information this importer needs.
func (imp *moduleImporter) ImportFrom(path, _ string, _ types.ImportMode) (*types.Package, error) {
	if pkg, ok := imp.cache[path]; ok {
		return pkg, nil
	}

	rel, ok := strings.CutPrefix(path, imp.modulePath+"/")
	if !ok {
		pkg, err := imp.fallback.Import(path)
		if err != nil {
			return nil, fmt.Errorf("import %s via the default (stdlib) importer: %w", path, err)
		}
		imp.cache[path] = pkg
		return pkg, nil
	}

	dirParts := append([]string{imp.repoRoot}, strings.Split(rel, "/")...)
	dir := filepath.Join(dirParts...)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s (import path %s): %w", dir, path, err)
	}

	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(imp.fset, filepath.Join(dir, e.Name()), nil, 0)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), parseErr)
		}
		files = append(files, file)
	}

	conf := types.Config{Importer: imp}
	pkg, err := conf.Check(path, imp.fset, files, nil)
	if err != nil {
		return nil, fmt.Errorf("type-check %s: %w", path, err)
	}
	imp.cache[path] = pkg
	return pkg, nil
}

// typeCanYieldWithoutConversion reports whether a value of type t can
// produce a value identical to banned (go/types' own notion of sameness —
// the same declared type, not merely a structurally matching one) without
// any explicit conversion syntax standing between the two: following
// struct fields (named or embedded, since go/types represents an embedded
// field as an ordinary Struct field with its type name as its field name),
// slice/array/map/pointer/channel element (and map key) types, a func or
// method signature's result types, and — since Go 1.18 — a generic type's
// own instantiated type arguments.
//
// Resolving through a type ALIAS (`type X = Y`) needed a dedicated,
// hand-written resolution table in the go/ast-text version of this check
// (Judgment Day round 2's own fix). It needs none here: go/types already
// resolves X to the identical object Y itself is — per the language
// spec, an alias declaration does not introduce a new type, it binds an
// identifier to an existing one — so a reference to X arrives at this
// function already carrying Y's own *types.Named (or other) value.
// Whatever this function does with a direct reference to Y, it does
// automatically for X, with no special case.
//
// A defined type WITHOUT `=` (`type StatusDefined unit.Status`) correctly
// falls through unflagged: types.Identical treats two *types.Named values
// as the same only when they are literally the same declaration, so
// StatusDefined and unit.Status compare unequal even though their
// underlying types are identical — Go itself requires an explicit
// conversion between them, which is exactly the boundary this check exists
// to respect. This exclusion, and its reasoning, are unchanged from the
// go/ast-text version; only the version before this one had it correctly
// scoped to nothing else.
//
// Interface method results ARE followed (an interface's method set is its
// whole shape, so a method returning unit.Status is exactly the same kind
// of exposure as a struct field of that type), but a NAMED STRUCT type's
// own methods are deliberately NOT — I01 is about what a value structurally
// carries or embeds, not what calling an arbitrary method on it might
// return; only interfaces make the method set part of the type itself.
//
// visited is a cycle guard, not merely an optimisation: internal/core's
// own type graph is finite but not acyclic in general (a struct with a
// pointer to itself, or two structs pointing at each other), and a plain
// recursive walk without one would not terminate. Marking a type visited
// BEFORE recursing into it, rather than only after fully exploring it, is
// what makes this a sound graph-reachability walk: this is the same
// "discover once, still enumerate every one of that node's own edges on
// that one discovery" shape any DFS-based reachability algorithm needs — a
// back-edge into a type still being explored short-circuits only THAT one
// redundant re-entry, never the sibling fields or elements the original
// call is still in the middle of checking, so a real unit.Status reachable
// by any path is still found. TestI01TypecheckFixture_KnownShapes' mutually
// recursive and self-referential fixtures are this argument's proof, not
// just its illustration: if it were unsound, one of those two would either
// hang or report the wrong boolean.
func typeCanYieldWithoutConversion(t, banned types.Type, visited map[types.Type]bool) bool {
	if t == nil {
		return false
	}
	// Go 1.22+ can materialize a `type X = Y` alias as its own *types.Alias
	// node instead of resolving X directly to Y's own type object (the
	// historical behavior types.Identical still accounts for on its own,
	// which is why the check below already catches an alias of banned
	// directly). Unalias here too, before the Named/struct/etc. dispatch
	// below, so an alias of anything ELSE that embeds banned (a wrapper
	// struct, a container, a type declared in another package) is resolved
	// the same way a direct reference to that same underlying type would
	// be — found empirically: without this call, `type A = wrapper; func
	// F() A` passed this check green even though `func F() wrapper`
	// (wrapper's own declaration) did not, because t stayed a *types.Alias
	// no case below ever unwrapped.
	t = types.Unalias(t)
	if types.Identical(t, banned) {
		return true
	}
	if visited[t] {
		return false
	}
	visited[t] = true

	if named, ok := t.(*types.Named); ok {
		if targs := named.TypeArgs(); targs != nil {
			for i := 0; i < targs.Len(); i++ {
				if typeCanYieldWithoutConversion(targs.At(i), banned, visited) {
					return true
				}
			}
		}
		return typeCanYieldWithoutConversion(named.Underlying(), banned, visited)
	}

	switch u := t.(type) {
	case *types.Pointer:
		return typeCanYieldWithoutConversion(u.Elem(), banned, visited)
	case *types.Slice:
		return typeCanYieldWithoutConversion(u.Elem(), banned, visited)
	case *types.Array:
		return typeCanYieldWithoutConversion(u.Elem(), banned, visited)
	case *types.Map:
		return typeCanYieldWithoutConversion(u.Key(), banned, visited) ||
			typeCanYieldWithoutConversion(u.Elem(), banned, visited)
	case *types.Chan:
		return typeCanYieldWithoutConversion(u.Elem(), banned, visited)
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if typeCanYieldWithoutConversion(u.Field(i).Type(), banned, visited) {
				return true
			}
		}
	case *types.Signature:
		results := u.Results()
		for i := 0; i < results.Len(); i++ {
			if typeCanYieldWithoutConversion(results.At(i).Type(), banned, visited) {
				return true
			}
		}
	case *types.Interface:
		for i := 0; i < u.NumMethods(); i++ {
			if typeCanYieldWithoutConversion(u.Method(i).Type(), banned, visited) {
				return true
			}
		}
	}
	return false
}
