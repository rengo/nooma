// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// TestUIMuxWiringMatchesDeclaredGuardTable is the structural gate
// TestHTTPAPISecretCompareStructure (httpapi_secret_compare_test.go) does
// not provide: that requireCookie is not merely a function BY THAT NAME
// somewhere in the package, but the function newUIMux actually wires its
// guarded leaves through.
//
// Round 5, judge B: added a sibling `weakerUIGuard(d.Token)` in server.go,
// wired GET /ui/{$} through it instead of requireCookie, and left
// requireCookie itself untouched. Every existing test stayed green,
// including TestUIViewsRequireCookie (internal/httpapi/server_test.go),
// which only ever drove GET /ui — never the sibling leaf. Nothing anywhere
// checked that the routes newUIMux registers use the guard
// httpapi_secret_compare_test.go inspects.
//
// This gate parses internal/httpapi/server.go's newUIMux, resolves what
// each mux.Handle(pattern, handler) call actually wires (following one
// level of local variable assignment, e.g. `guardedUI :=
// requireCookie(d.Token)(d.UI)`), and matches the result against a
// declared table below: each guarded leaf must resolve to exactly
// requireCookie(d.Token)(d.UI); each open leaf must resolve to anything
// else. A pattern newUIMux registers that is not in the table — guarded or
// not — fails too, so "guard everything" is not a passing answer: an open
// leaf wrongly wrapped in requireCookie is caught the same way a guarded
// leaf left unwrapped is.
//
// Scope: checks newUIMux's own wiring, by parsing server.go directly — not
// a generic scan of the package, not real HTTP behaviour (that is
// TestUIViewsRequireCookie's job), not the API side. The API side's
// apiRoutes (design D10) is guarded once, for the whole guardedMux, by a
// single requireToken(d.Token)(guardedMux) wrap around the entire mux in
// Handler — there is no per-route wiring on that side for a route to omit,
// so the per-leaf check this gate performs does not apply there; a route
// added to apiRoutes is guarded by construction, already proven by
// TestGuardedRoutesRequireToken iterating the same slice Handler registers
// from. And not a check that wantUIMuxWiring's own classification is
// correct: this gate proves newUIMux matches the table, never that the
// table itself got a leaf right. A new sensitive /ui leaf added as
// `guarded: false` in the same commit as its own table row passes cleanly —
// the gate cannot tell a correct classification from a self-consistent
// wrong one.
func TestUIMuxWiringMatchesDeclaredGuardTable(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	serverPath := filepath.Join(repoRoot, "internal", "httpapi", "server.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, serverPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", serverPath, err)
	}

	var muxFn *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "newUIMux" {
			muxFn = fn
			break
		}
	}
	if muxFn == nil {
		t.Fatal("internal/httpapi/server.go declares no func newUIMux — renamed or removed; this gate has nothing to check")
	}

	found := collectMuxHandleCalls(t, fset, muxFn, "mux", true)
	if len(found) == 0 {
		t.Fatal("newUIMux registers zero routes — this gate's own guard: nothing to check")
	}

	foundByPattern := make(map[string]uiLeafWiring, len(found))
	for _, f := range found {
		foundByPattern[f.pattern] = f
	}

	for _, want := range wantUIMuxWiring {
		got, ok := foundByPattern[want.pattern]
		if !ok {
			t.Errorf(
				"newUIMux no longer registers %q — this gate's expected-wiring table "+
					"(httpapi_ui_wiring_test.go) needs the same edit, in this commit",
				want.pattern,
			)
			continue
		}
		delete(foundByPattern, want.pattern)

		if got.guarded != want.guarded {
			t.Errorf(
				"newUIMux's %q: expected guarded=%v (%s(%s)(%s)), found guarded=%v — %s",
				want.pattern, want.guarded, wantGuardFuncName, wantGuardTokenExpr, wantGuardTargetExpr,
				got.guarded, got.describe(),
			)
			continue
		}
		if !want.guarded {
			continue
		}
		if got.guardFunc != wantGuardFuncName || got.tokenArg != wantGuardTokenExpr || got.targetArg != wantGuardTargetExpr {
			t.Errorf(
				"newUIMux's %q: expected %s(%s)(%s), found %s — a guard function swapped for "+
					"another one, or wired to the wrong token or target, is exactly what this "+
					"gate exists to catch",
				want.pattern, wantGuardFuncName, wantGuardTokenExpr, wantGuardTargetExpr, got.describe(),
			)
		}
	}

	for pattern, f := range foundByPattern {
		t.Errorf(
			"newUIMux registers %q — not declared in this gate's expected-wiring table "+
				"(httpapi_ui_wiring_test.go) as either a guarded or an open leaf (%s). A leaf "+
				"added to the mux must be added to that table too, in the same commit, saying "+
				"whether it must be guarded",
			pattern, f.describe(),
		)
	}
}

// uiLeafWiring is one mux.Handle registration newUIMux makes, resolved to
// whether it is wrapped by requireCookie and, if so, with which token and
// target arguments.
type uiLeafWiring struct {
	pattern   string
	guarded   bool
	guardFunc string
	tokenArg  string
	targetArg string
	rawText   string
}

// describe renders w for an error message — the resolved guard call when
// guarded, the raw registered expression otherwise.
func (w uiLeafWiring) describe() string {
	if w.guarded {
		return w.guardFunc + "(" + w.tokenArg + ")(" + w.targetArg + ")"
	}
	return "registered directly as " + w.rawText
}

// wantUIMuxWiring is newUIMux's declared expected wiring: every pattern it
// registers must appear here, and every pattern here must be registered —
// the whitelist this gate matches the AST against. GET/POST /ui/login are
// unguarded (PR 4b, design m4a §3.2): the login screen cannot require the
// cookie it exists to issue. newUIMux registers both only inside an `if
// d.Token != ""` block — uiMuxHandleCalls walks into an *ast.IfStmt's body
// for exactly this reason (below), so a route conditionally registered is
// still found, not silently skipped by a gate written before this PR had
// any conditional to look inside.
var wantUIMuxWiring = []struct {
	pattern string
	guarded bool
}{
	{pattern: "GET /ui/static/app.css", guarded: false},
	{pattern: "GET /ui/static/htmx.min.js", guarded: false},
	{pattern: "GET /ui/static/htmx.LICENSE", guarded: false},
	{pattern: "GET /ui/login", guarded: false},
	{pattern: "POST /ui/login", guarded: false},
	{pattern: "GET /ui", guarded: true},
	{pattern: "GET /ui/{$}", guarded: true},
	{pattern: "GET /ui/units", guarded: true},
	{pattern: "GET /ui/units/{id}", guarded: true},
	{pattern: "GET /ui/capture", guarded: true},
	{pattern: "POST /ui/capture", guarded: true},
	{pattern: "POST /ui/units/{id}/correct", guarded: true},
	{pattern: "GET /ui/beliefs", guarded: true},
	{pattern: "POST /ui/beliefs/{id}/edit", guarded: true},
	{pattern: "POST /ui/beliefs/{id}/retire", guarded: true},
	{pattern: "GET /ui/activity", guarded: true},
}

// wantGuardFuncName, wantGuardTokenExpr and wantGuardTargetExpr are the
// exact guard call every guarded row in wantUIMuxWiring must resolve to:
// requireCookie(d.Token)(d.UI).
const (
	wantGuardFuncName   = "requireCookie"
	wantGuardTokenExpr  = "d.Token"
	wantGuardTargetExpr = "d.UI"
)

// collectMuxHandleCalls walks fn's ENTIRE body via ast.Inspect — every
// statement however nested: an if, a for, a switch, not only the top level
// and one level of if-branch the narrower walk this replaces used to see —
// closing design N7's non-literal-pattern, HandleFunc and loop holes at
// once, rather than adding a special case per hole. It tracks local `name
// := expr` assignments (newUIMux's own `guardedUI := ...`) across the whole
// function, matching its flat style, so a call passing a variable still
// resolves to what that variable was assigned.
//
// Two shapes fail the gate immediately, by t.Fatal, rather than being
// silently skipped the way the narrower walk used to skip them: a pattern
// argument that is not a string literal (round 5's own class — a route this
// gate cannot read statically), and — only when requireHandleOnly is true,
// newUIMux's own case — a call through HandleFunc instead of Handle.
// requireHandleOnly is false for Handler's own outer mux (server.go), which
// legitimately mixes both for its two non-UI leaves.
func collectMuxHandleCalls(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl, receiverName string, requireHandleOnly bool) []uiLeafWiring {
	t.Helper()

	varExpr := map[string]ast.Expr{}
	var leaves []uiLeafWiring

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE && len(s.Lhs) == 1 && len(s.Rhs) == 1 {
				if ident, ok := s.Lhs[0].(*ast.Ident); ok {
					varExpr[ident.Name] = s.Rhs[0]
				}
			}

		case *ast.CallExpr:
			sel, ok := s.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok || recv.Name != receiverName {
				return true
			}
			if requireHandleOnly && sel.Sel.Name != "Handle" {
				t.Fatalf("%s: %s.%s(...) registers a route via HandleFunc, not Handle — the hardened wiring gate requires every %s leaf to go through .Handle so a guard wrap stays inspectable (design N7)", fn.Name.Name, receiverName, sel.Sel.Name, receiverName)
			}
			if len(s.Args) != 2 {
				return true
			}
			lit, ok := s.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Fatalf("%s: %s.%s registers a non-literal pattern (%s) — the hardened wiring gate requires every pattern to be a string literal it can read statically (design N7)", fn.Name.Name, receiverName, sel.Sel.Name, normalizedText(fset, s.Args[0]))
			}
			pattern, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("%s: unquote route pattern %s: %v", fn.Name.Name, lit.Value, err)
			}
			leaves = append(leaves, classifyUILeaf(fset, pattern, resolveLocalVar(s.Args[1], varExpr)))
		}
		return true
	})

	return leaves
}

// resolveLocalVar follows e through varExpr while e is an identifier bound
// by a local `name := expr` assignment newUIMux made earlier in its body,
// stopping at the first expression that is not such an identifier — one
// level for newUIMux's own shape (`guardedUI := requireCookie(...)(...)`),
// cycle-guarded in case a future edit ever chains assignments.
func resolveLocalVar(e ast.Expr, varExpr map[string]ast.Expr) ast.Expr {
	seen := map[string]bool{}
	for {
		ident, ok := e.(*ast.Ident)
		if !ok {
			return e
		}
		if seen[ident.Name] {
			return e
		}
		seen[ident.Name] = true
		next, ok := varExpr[ident.Name]
		if !ok {
			return e
		}
		e = next
	}
}

// classifyUILeaf reports whether handler IS the shape
// requireCookie(token)(target) — an *ast.CallExpr whose own Fun is itself
// an *ast.CallExpr naming the guard function — and, when it is, the guard
// function's name plus its token and target arguments. Any other shape
// (d.UI directly, ui.Assets(), weakerUIGuard(d.Token)(d.UI) — a different
// guard function is still a match on shape but not on name, caught by the
// caller comparing guardFunc against wantGuardFuncName) is reported
// unguarded except that the wrapping call itself does get named, so a
// swapped guard function is diagnosed by name rather than only by
// guarded/unguarded.
func classifyUILeaf(fset *token.FileSet, pattern string, handler ast.Expr) uiLeafWiring {
	leaf := uiLeafWiring{pattern: pattern, rawText: normalizedText(fset, handler)}

	outer, ok := handler.(*ast.CallExpr)
	if !ok || len(outer.Args) != 1 {
		return leaf
	}
	inner, ok := outer.Fun.(*ast.CallExpr)
	if !ok || len(inner.Args) != 1 {
		return leaf
	}
	guardIdent, ok := inner.Fun.(*ast.Ident)
	if !ok {
		return leaf
	}

	leaf.guarded = true
	leaf.guardFunc = guardIdent.Name
	leaf.tokenArg = normalizedText(fset, inner.Args[0])
	leaf.targetArg = normalizedText(fset, outer.Args[0])
	return leaf
}

// wantHandlerOuterMuxPatterns is Handler's own outer mux (server.go) — the
// one that wraps newUIMux, never the inner guardedMux apiRoutes wires — and
// design N7's last named hole: a /ui/... leaf registered directly here
// would bypass newUIMux's xo/requireCookie wrap entirely and appear in no
// table at all, because TestUIMuxWiringMatchesDeclaredGuardTable only ever
// parses newUIMux.
var wantHandlerOuterMuxPatterns = map[string]bool{
	"GET /{$}": true,
	"/ui":      true,
	"/ui/":     true,
	"/":        true,
}

// TestUIHandlerOuterMuxRegistersOnlyDeclaredLeaves is TestUIMuxWiringMatchesDeclaredGuardTable's
// sibling gate (design §3.6, N7): it parses Handler itself, not newUIMux,
// and asserts its own local var literally named mux registers exactly
// wantHandlerOuterMuxPatterns — no more, no fewer. A future /ui/... leaf
// added directly to Handler instead of newUIMux lands here as an unlisted
// pattern and fails loudly, instead of quietly reaching a browser ungated.
func TestUIHandlerOuterMuxRegistersOnlyDeclaredLeaves(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	serverPath := filepath.Join(repoRoot, "internal", "httpapi", "server.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, serverPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", serverPath, err)
	}

	var handlerFn *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "Handler" {
			handlerFn = fn
			break
		}
	}
	if handlerFn == nil {
		t.Fatal("internal/httpapi/server.go declares no func Handler — renamed or removed; this gate has nothing to check")
	}

	found := collectMuxHandleCalls(t, fset, handlerFn, "mux", false)
	if len(found) == 0 {
		t.Fatal("Handler's outer mux registers zero routes — this gate's own guard: nothing to check")
	}

	seen := make(map[string]bool, len(found))
	for _, f := range found {
		seen[f.pattern] = true
		if !wantHandlerOuterMuxPatterns[f.pattern] {
			t.Errorf("Handler's outer mux registers %q — not in this gate's declared set %v; a /ui/... leaf registered here bypasses newUIMux's xo/requireCookie wrap entirely (design N7)", f.pattern, wantHandlerOuterMuxPatterns)
		}
	}
	for pattern := range wantHandlerOuterMuxPatterns {
		if !seen[pattern] {
			t.Errorf("Handler's outer mux no longer registers %q — this gate's declared set needs the same edit, in this commit", pattern)
		}
	}
}
