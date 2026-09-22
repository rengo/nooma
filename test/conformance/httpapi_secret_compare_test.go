// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHTTPAPISecretCompareStructure is requireCookie's and requireToken's
// structural gate — needed because TestUIViewsRequireCookie
// (internal/httpapi/server_test.go) only sees a response over HTTP, and a
// mutation that changes only TIMING is invisible to it.
//
// Four Judgment Day rounds live behind this version. Rounds 1-3 each
// blacklisted one bug SHAPE — an unguarded `return`, then a same-package
// call-closure walk requiring only that the guarding `if` REACHES the
// compare — and round 4 broke that rule three ways, all compiling and
// gate-green: a `||`-joined condition that reaches the compare without ever
// running it (Go short-circuits `||`); the handler wrapped by another
// same-package call, moving the guarded `return` one function away from the
// walk; and a return-free conditional busy loop ahead of the compare,
// invisible to a return-only rule.
//
// Enumerating bad shapes does not terminate, so this version whitelists the
// one GOOD shape instead: requireCookie's and requireToken's own handler
// bodies — the closure each returns, and the http.HandlerFunc literal that
// closure returns — must match a literal template declared below, exactly,
// statement by statement. Two checks: (1) presentedSecret (cookie.go)
// returns a single []byte, no error, no http.ResponseWriter, so a
// decode-error return is unexpressible inside it; (2) the template match,
// where the compare `if`'s condition must itself BE (never merely reach) a
// subtle.ConstantTimeCompare(...) != 1 comparison, and the middleware must
// return http.HandlerFunc(<literal>) directly, never a wrapping call.
//
// Scope: checks requireCookie and requireToken BY NAME against a template
// declared here, not a generic scan — not presentedSecret's own body beyond
// its signature, not a move to another package (fails the template,
// correctly), never real timing (no test here measures a wall clock; this
// proves the structure timing-safety depends on, not the timing itself).
//
// Inversion, on purpose: an earlier round required staying sound under
// in-package helper extraction "in either direction" — moving the compare
// or the decode into a helper had to PASS. The template now requires the
// opposite, since that move changes the pinned statement sequence: changing
// either handler's shape, refactor or not, means changing its template in
// the same commit, deliberately.
func TestHTTPAPISecretCompareStructure(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	httpapiDir := filepath.Join(repoRoot, "internal", "httpapi")

	funcs, fset := parsePackageFuncDecls(t, httpapiDir)
	if len(funcs) == 0 {
		t.Fatal("found zero top-level functions under internal/httpapi — D10's guard: nothing to check yet")
	}

	t.Run("presentedSecret's signature makes an early return on decode error unexpressible", func(t *testing.T) {
		fn, ok := funcs["presentedSecret"]
		if !ok {
			t.Fatal("internal/httpapi declares no func presentedSecret — renamed or removed; this gate has nothing to check")
		}
		if ok, reason := decodeHelperSignatureIsSound(fn); !ok {
			t.Errorf("internal/httpapi/cookie.go's presentedSecret: %s", reason)
		}
	})

	for _, tc := range []struct {
		fnName   string
		template string
	}{
		{"requireCookie", requireCookieTemplateSrc},
		{"requireToken", requireTokenTemplateSrc},
	} {
		tc := tc
		t.Run(tc.fnName+" matches its declared template", func(t *testing.T) {
			gotFn, ok := funcs[tc.fnName]
			if !ok {
				t.Fatalf("internal/httpapi declares no top-level func %s — renamed or removed; this gate has nothing to check", tc.fnName)
			}

			wantFn, wantFset := parseTemplateFuncDecl(t, tc.fnName, tc.template)

			violations := compareBlock(wantFset, fset, tc.fnName+" body", wantFn.Body, gotFn.Body)
			if len(violations) == 0 {
				return
			}
			t.Errorf(
				"%s's handler no longer matches its declared template (below) — changing this "+
					"middleware means changing the matching template in "+
					"test/conformance/httpapi_secret_compare_test.go in the same commit, "+
					"deliberately:\n\n%s",
				tc.fnName, strings.Join(violations, "\n\n"),
			)
		})
	}
}

// requireCookieTemplateSrc is the one shape requireCookie's per-request
// handler is permitted to have, declared as ordinary Go source a reader sees
// without running anything: an assignment from presentedSecret, an `if`
// whose condition IS the constant-time compare, and next.ServeHTTP —
// nothing else, in this order. Derived from internal/httpapi/cookie.go.
const requireCookieTemplateSrc = `
package template

func requireCookie(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented := presentedSecret(r, uiCookieName, len(token))

			if subtle.ConstantTimeCompare(presented, []byte(token)) != 1 {
				http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
`

// requireTokenTemplateSrc is requireCookie's sibling shape, not the same
// template: requireToken additionally extracts the Bearer prefix into
// `presented` through an `if` that itself carries NO return — only the
// compare `if` below it may. Derived from internal/httpapi/auth.go.
const requireTokenTemplateSrc = `
package template

func requireToken(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented := ""
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, bearerPrefix) {
				presented = strings.TrimPrefix(h, bearerPrefix)
			}

			if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
`

// parseTemplateFuncDecl parses src and returns the *ast.FuncDecl named
// fnName plus its *token.FileSet. A parse or lookup failure is this test
// file's own bug, so it is t.Fatal.
func parseTemplateFuncDecl(t *testing.T, fnName, src string) (*ast.FuncDecl, *token.FileSet) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fnName+"-template", src, 0)
	if err != nil {
		t.Fatalf("parse %s's own template: %v", fnName, err)
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == fnName {
			return fn, fset
		}
	}
	t.Fatalf("%s's template declares no func %s — this test file's own bug", fnName, fnName)
	return nil, nil
}

// compareBlock compares two statement lists at the same nesting level. A
// statement COUNT mismatch is reported once, for the whole block (both
// printed for a by-eye diff) — the template permits no extra or missing one.
func compareBlock(wantFset, gotFset *token.FileSet, path string, want, got *ast.BlockStmt) []string {
	if len(want.List) != len(got.List) {
		return []string{fmt.Sprintf(
			"%s: template has %d statement(s), found %d\n  template:\n%s\n  found:\n%s",
			path, len(want.List), len(got.List),
			indent(renderNode(wantFset, want)), indent(renderNode(gotFset, got)),
		)}
	}

	var violations []string
	for i := range want.List {
		violations = append(violations, compareStmt(
			wantFset, gotFset, fmt.Sprintf("%s, statement %d", path, i+1), want.List[i], got.List[i],
		)...)
	}
	return violations
}

// compareStmt compares one want/got statement pair. Different Go kinds fail
// immediately by kind name — that alone closes round 4's busy-loop exploit,
// an *ast.ForStmt the template never declares at that statement index.
func compareStmt(wantFset, gotFset *token.FileSet, path string, want, got ast.Stmt) []string {
	if wantKind, gotKind := fmt.Sprintf("%T", want), fmt.Sprintf("%T", got); wantKind != gotKind {
		return []string{fmt.Sprintf(
			"%s: template expects a %s, found a %s\n  template:\n%s\n  found:\n%s",
			path, wantKind, gotKind,
			indent(renderNode(wantFset, want)), indent(renderNode(gotFset, got)),
		)}
	}

	switch w := want.(type) {
	case *ast.IfStmt:
		return compareIf(wantFset, gotFset, path, w, got.(*ast.IfStmt))
	case *ast.ReturnStmt:
		return compareReturn(wantFset, gotFset, path, w, got.(*ast.ReturnStmt))
	default:
		// *ast.AssignStmt, *ast.ExprStmt: a one-liner in both templates, so
		// a text mismatch IS the statement-level mismatch.
		if normalizedText(wantFset, want) != normalizedText(gotFset, got) {
			return []string{fmt.Sprintf(
				"%s:\n  template: %s\n  found:    %s",
				path, normalizedText(wantFset, want), normalizedText(gotFset, got),
			)}
		}
		return nil
	}
}

// compareIf compares Init/Else as text/bool, Body recursively, and Cond one
// of two ways: when want's own Cond IS the constant-time-compare shape
// (isConstantTimeCompareCond), got's Cond must be that exact shape too —
// closing round 4's short-circuit exploit; any other `if` (requireToken's
// Bearer-prefix extraction) compares Cond as plain text.
func compareIf(wantFset, gotFset *token.FileSet, path string, want, got *ast.IfStmt) []string {
	var violations []string

	if normalizedText(wantFset, want.Init) != normalizedText(gotFset, got.Init) {
		violations = append(violations, fmt.Sprintf(
			"%s condition's init:\n  template: %s\n  found:    %s",
			path, normalizedText(wantFset, want.Init), normalizedText(gotFset, got.Init),
		))
	}

	if isCompare, _ := isConstantTimeCompareCond(want.Cond); isCompare {
		violations = append(violations, compareCompareCond(wantFset, gotFset, path, want.Cond, got.Cond)...)
	} else if normalizedText(wantFset, want.Cond) != normalizedText(gotFset, got.Cond) {
		violations = append(violations, fmt.Sprintf(
			"%s condition:\n  template: %s\n  found:    %s",
			path, normalizedText(wantFset, want.Cond), normalizedText(gotFset, got.Cond),
		))
	}

	if (want.Else == nil) != (got.Else == nil) {
		violations = append(violations, fmt.Sprintf(
			"%s: template %s an else branch, found %s one",
			path, presence(want.Else != nil), presence(got.Else != nil),
		))
	}

	return append(violations, compareBlock(wantFset, gotFset, path+"'s if-body", want.Body, got.Body)...)
}

// compareCompareCond enforces the rule this gate exists to state precisely:
// the compare `if`'s condition must BE `subtle.ConstantTimeCompare(...) !=
// 1`, structurally; a shape mismatch fails here by name, a call-argument
// mismatch fails the element-wise comparison below it instead.
func compareCompareCond(wantFset, gotFset *token.FileSet, path string, want, got ast.Expr) []string {
	gotIsCompare, reason := isConstantTimeCompareCond(got)
	if !gotIsCompare {
		return []string{fmt.Sprintf(
			"%s condition must BE `subtle.ConstantTimeCompare(...) != 1` — a *ast.BinaryExpr "+
				"comparing the compare call against 1 directly, never merely containing or "+
				"reaching it (a `&&`/`||` joined condition fails this check) — found `%s`: %s",
			path, normalizedText(gotFset, got), reason,
		)}
	}

	wantArgs := want.(*ast.BinaryExpr).X.(*ast.CallExpr).Args
	gotArgs := got.(*ast.BinaryExpr).X.(*ast.CallExpr).Args
	if len(wantArgs) != len(gotArgs) {
		return []string{fmt.Sprintf(
			"%s condition's subtle.ConstantTimeCompare call: template passes %d argument(s), found %d",
			path, len(wantArgs), len(gotArgs),
		)}
	}

	var violations []string
	for i := range wantArgs {
		if normalizedText(wantFset, wantArgs[i]) != normalizedText(gotFset, gotArgs[i]) {
			violations = append(violations, fmt.Sprintf(
				"%s condition's subtle.ConstantTimeCompare argument %d:\n  template: %s\n  found:    %s",
				path, i+1, normalizedText(wantFset, wantArgs[i]), normalizedText(gotFset, gotArgs[i]),
			))
		}
	}
	return violations
}

// isConstantTimeCompareCond reports whether cond is exactly
// `subtle.ConstantTimeCompare(...) != 1`. reason names which part failed,
// non-empty only when ok is false.
func isConstantTimeCompareCond(cond ast.Expr) (ok bool, reason string) {
	bin, isBinary := cond.(*ast.BinaryExpr)
	if !isBinary {
		return false, fmt.Sprintf("condition is a %T, not a binary comparison", cond)
	}
	if bin.Op != token.NEQ {
		return false, fmt.Sprintf("comparison operator is %q, want !=", bin.Op)
	}
	call, isCall := bin.X.(*ast.CallExpr)
	if !isCall {
		return false, "left side of != is not a function call"
	}
	sel, isSelector := call.Fun.(*ast.SelectorExpr)
	if !isSelector {
		return false, "left side of != does not call subtle.ConstantTimeCompare"
	}
	pkg, isIdent := sel.X.(*ast.Ident)
	if !isIdent || pkg.Name != "subtle" || sel.Sel.Name != "ConstantTimeCompare" {
		return false, "left side of != does not call subtle.ConstantTimeCompare"
	}
	lit, isLit := bin.Y.(*ast.BasicLit)
	if !isLit || lit.Value != "1" {
		return false, "right side of != is not the literal 1"
	}
	return true, ""
}

// compareReturn recurses into a nested http.HandlerFunc literal for two
// shapes: a bare FuncLit return (the outer closure) and an
// http.HandlerFunc-wrapped FuncLit return (the inner handler) — the latter
// must be wrapped DIRECTLY, closing round 4's wrapping exploit. Any other
// return (`return next`, ...) compares as plain text.
func compareReturn(wantFset, gotFset *token.FileSet, path string, want, got *ast.ReturnStmt) []string {
	if wantLit := bareFuncLitReturned(want); wantLit != nil {
		gotLit := bareFuncLitReturned(got)
		if gotLit == nil {
			return []string{fmt.Sprintf(
				"%s: template returns a function literal directly, found `%s` instead",
				path, normalizedText(gotFset, got),
			)}
		}
		return compareFuncLit(wantFset, gotFset, path, wantLit, gotLit)
	}

	if wantLit := handlerFuncWrappedLiteral(want); wantLit != nil {
		gotLit := handlerFuncWrappedLiteral(got)
		if gotLit == nil {
			return []string{fmt.Sprintf(
				"%s: template returns http.HandlerFunc(<literal>) directly — the middleware must "+
					"return the handler literal itself, not a call wrapping it — found `%s` instead",
				path, normalizedText(gotFset, got),
			)}
		}
		return compareFuncLit(wantFset, gotFset, path, wantLit, gotLit)
	}

	if normalizedText(wantFset, want) != normalizedText(gotFset, got) {
		return []string{fmt.Sprintf(
			"%s:\n  template: %s\n  found:    %s",
			path, normalizedText(wantFset, want), normalizedText(gotFset, got),
		)}
	}
	return nil
}

// compareFuncLit compares two *ast.FuncLit found by compareReturn: Type
// (param/result list) as whole text, then Body recursively via compareBlock.
func compareFuncLit(wantFset, gotFset *token.FileSet, path string, want, got *ast.FuncLit) []string {
	var violations []string
	if normalizedText(wantFset, want.Type) != normalizedText(gotFset, got.Type) {
		violations = append(violations, fmt.Sprintf(
			"%s's function literal signature:\n  template: %s\n  found:    %s",
			path, normalizedText(wantFset, want.Type), normalizedText(gotFset, got.Type),
		))
	}
	return append(violations, compareBlock(wantFset, gotFset, path+"'s function literal body", want.Body, got.Body)...)
}

// bareFuncLitReturned reports ret's *ast.FuncLit when ret returns exactly
// one result and that result IS a function literal directly, nil otherwise.
func bareFuncLitReturned(ret *ast.ReturnStmt) *ast.FuncLit {
	if len(ret.Results) != 1 {
		return nil
	}
	lit, _ := ret.Results[0].(*ast.FuncLit)
	return lit
}

// handlerFuncWrappedLiteral reports ret's *ast.FuncLit when ret returns
// exactly `http.HandlerFunc(<func literal>)` — nil otherwise, including when
// http.HandlerFunc wraps anything but a literal, or the outer call is
// anything but http.HandlerFunc itself (a same-package wrapper one call
// away, round 4's exploit 2).
func handlerFuncWrappedLiteral(ret *ast.ReturnStmt) *ast.FuncLit {
	if len(ret.Results) != 1 {
		return nil
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "HandlerFunc" {
		return nil
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "http" {
		return nil
	}
	lit, _ := call.Args[0].(*ast.FuncLit)
	return lit
}

// presence renders "has"/"has no" for the else-branch mismatch message.
func presence(has bool) string {
	if has {
		return "has"
	}
	return "has no"
}

// renderNode prints n back to Go source against fset — nil (an absent Init
// or Else) prints as "<nothing>", since go/printer cannot print a nil node.
func renderNode(fset *token.FileSet, n ast.Node) string {
	if n == nil {
		return "<nothing>"
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, n); err != nil {
		return fmt.Sprintf("<unprintable: %v>", err)
	}
	return buf.String()
}

// normalizedText renders n and collapses whitespace runs to a single
// space — the template's own indentation and the real file's gofmt
// indentation differ only in that, and every token still must match.
func normalizedText(fset *token.FileSet, n ast.Node) string {
	return strings.Join(strings.Fields(renderNode(fset, n)), " ")
}

// indent prefixes every line of s with two spaces, for a statement-count
// mismatch diagnostic that prints a whole block.
func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}

// parsePackageFuncDecls parses every non-test .go file directly inside dir
// and returns its top-level, non-method function declarations keyed by
// name, plus the *token.FileSet they were parsed with.
func parsePackageFuncDecls(t *testing.T, dir string) (map[string]*ast.FuncDecl, *token.FileSet) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	funcs := map[string]*ast.FuncDecl{}
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
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			funcs[fn.Name.Name] = fn
		}
	}
	return funcs, fset
}

// decodeHelperSignatureIsSound reports whether fn returns a single []byte,
// no error, no http.ResponseWriter parameter — the shape that leaves no
// decode-error branch to write an early return in. reason names the part
// that failed, non-empty only when ok is false.
func decodeHelperSignatureIsSound(fn *ast.FuncDecl) (ok bool, reason string) {
	var resultTypes []ast.Expr
	if fn.Type.Results != nil {
		for _, field := range fn.Type.Results.List {
			n := len(field.Names)
			if n == 0 {
				n = 1
			}
			for i := 0; i < n; i++ {
				resultTypes = append(resultTypes, field.Type)
			}
		}
	}

	if len(resultTypes) == 2 && exprIsErrorType(resultTypes[1]) {
		return false, fmt.Sprintf(
			"returns (%s, error) — it must return a single []byte with no error result, so a "+
				"caller has no decode-error branch to answer from",
			exprString(resultTypes[0]),
		)
	}
	if len(resultTypes) != 1 {
		return false, fmt.Sprintf("returns %d values, want exactly 1 ([]byte)", len(resultTypes))
	}
	if !exprIsByteSliceType(resultTypes[0]) {
		return false, fmt.Sprintf("result type is %s, want []byte", exprString(resultTypes[0]))
	}

	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			if exprIsHTTPResponseWriterType(field.Type) {
				return false, "takes an http.ResponseWriter parameter — it must not, so it has " +
					"no response to write an early decode-error answer to"
			}
		}
	}
	return true, ""
}

// exprIsByteSliceType reports whether e is exactly the type []byte — an
// *ast.ArrayType with no Len (a slice, not an array) whose element is the
// identifier "byte".
func exprIsByteSliceType(e ast.Expr) bool {
	arr, ok := e.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	ident, ok := arr.Elt.(*ast.Ident)
	return ok && ident.Name == "byte"
}

// exprIsErrorType reports whether e is the predeclared identifier error,
// parsed by go/ast as a plain *ast.Ident (error is predeclared, not a
// keyword, so it carries no dedicated node kind of its own).
func exprIsErrorType(e ast.Expr) bool {
	ident, ok := e.(*ast.Ident)
	return ok && ident.Name == "error"
}

// exprIsHTTPResponseWriterType reports whether e names http.ResponseWriter,
// directly or through a pointer.
func exprIsHTTPResponseWriterType(e ast.Expr) bool {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "http" && sel.Sel.Name == "ResponseWriter"
}

// exprString renders e back to source text for an error message — best-
// effort (an uncovered shape renders as its Go syntax node type name
// instead), acceptable since every shape this gate exercises is covered.
func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			return pkg.Name + "." + t.Sel.Name
		}
	case *ast.ArrayType:
		return "[]" + exprString(t.Elt)
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	}
	return fmt.Sprintf("%T", e)
}
