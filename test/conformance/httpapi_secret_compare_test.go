// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestHTTPAPISecretCompareStructure is the structural gate
// TestUIViewsRequireCookie (internal/httpapi/server_test.go) cannot be: that
// test only ever sees requireCookie's response over HTTP, and a mutation
// that changes only requireCookie's TIMING — never its response — is
// invisible to it. What this test proves instead, on the AST rather than on
// any response, is exactly three things:
//
//  1. Signature: internal/httpapi/cookie.go's presentedSecret — the decode
//     step requireCookie calls to read its cookie — returns a single
//     []byte, with no error result and no http.ResponseWriter parameter.
//     This is what makes an early return on a decode error UNEXPRESSIBLE
//     INSIDE presentedSecret ITSELF, not merely avoided there: a function
//     with no error to return and no response writer to answer with has no
//     decode-error branch of its own left to write one in. It says nothing
//     about presentedSecret's caller — a caller can still re-derive the
//     same fact (e.g. call r.Cookie or base64.RawURLEncoding.DecodeString a
//     second time) and branch on that; check 3 below is what closes that
//     door. (Judgment Day: two independent rounds each defeated an earlier
//     version of this gate that instead walked requireCookie's own
//     ast.FuncDecl body for a `return` inside an if-err-!=-nil branch — one
//     round moved the decode, and the bug inside it, into a same-package
//     helper the walk never followed, a false negative; a later round found
//     the walk also had no way to name WHICH function should carry
//     subtle.ConstantTimeCompare once a same-package helper existed, so a
//     correct extraction that still called subtle.ConstantTimeCompare
//     failed the gate, a false positive. Restructuring presentedSecret so
//     the bug cannot be expressed in its own body — rather than hardening a
//     walk that inspects one function's own statements — is what closes
//     both classes at once; see cookie.go's presentedSecret doc comment.)
//
//  2. Transitive compare: requireCookie (cookie.go) and requireToken
//     (auth.go) must each reach a call to crypto/subtle.ConstantTimeCompare
//     — not necessarily in their own ast.FuncDecl.Body, but somewhere in
//     the closure of same-package, unqualified function calls reachable
//     from them. This is what stays sound under in-package helper
//     extraction in EITHER direction: a helper that still calls
//     subtle.ConstantTimeCompare is not a violation (fixes the false
//     positive above), and a plain byte-equality helper standing in for it
//     anywhere in the closure — extracted or not — is (fixes the false
//     negative the original, single-function-body walk could not see past
//     a helper boundary).
//
//  3. Control flow: inside the per-request http.HandlerFunc literal each of
//     requireCookie and requireToken returns from its outer closure, the
//     ONLY `return` statement permitted is the one whose innermost
//     enclosing `if` reaches subtle.ConstantTimeCompare — directly, or
//     transitively through the same same-package call resolution check 2
//     already performs. This is what actually forbids a caller from
//     re-deriving a decode-error (or any other) fact and branching on it
//     with an early exit: checks 1 and 2 alone missed exactly this —
//     duplicating r.Cookie's presence check, or duplicating the base64
//     decode, in requireCookie's own handler body, ahead of the call to
//     presentedSecret, both compiled, both left presentedSecret's checked
//     signature and the transitive compare untouched, and both produced a
//     provably distinguishable response (an early redirect/401 that never
//     reaches the constant-time compare) that only this control-flow rule
//     catches (Judgment Day round 3: two independently blind reviewers each
//     reproduced a variant of this against round 2's gate).
//
// # Scope, stated plainly
//
// This checks requireCookie and requireToken by name, walking only
// SAME-PACKAGE, UNQUALIFIED function calls (an *ast.Ident naming a
// top-level, non-method function declared somewhere in
// internal/httpapi's own non-test .go files) — not a generic scan of
// "every secret comparison anywhere in this module." It does NOT cover:
//
//   - A call into another package (e.g. a helper moved to an internal
//     subpackage) — invisible to this walk, which only ever resolves an
//     unqualified *ast.Ident against internal/httpapi's own top-level
//     functions.
//   - A method call (`x.Method(...)`) or any other dynamic dispatch — an
//     *ast.SelectorExpr call target is never treated as a same-package
//     function reference here, deliberately: resolving it soundly would
//     need a receiver's static type, which this AST-only walk does not
//     compute (the same boundary go/ast-only tooling in this repository
//     already accepts elsewhere, e.g. i23's applyWithPreImage gate).
//   - A `return` inside a nested function literal declared within the
//     handler literal itself — check 3 walks statements, not expressions,
//     and deliberately does not descend into a nested *ast.FuncLit's own
//     body; requireCookie and requireToken declare no such nested literal
//     today, so this is a stated boundary, not an observed gap.
//   - Real timing. No test in this repository measures actual timing (non-
//     negotiable #5 forbids the network, and a wall-clock timing assertion
//     is inherently flaky). This proves the STRUCTURE timing-safety depends
//     on, the same relationship i01's own "returns or embeds a
//     unit.Status" check has to I01: a static proof of shape, not a dynamic
//     proof of the property the shape exists to guarantee — never a proof
//     that the constant-time compare's elapsed wall-clock time is actually
//     independent of its inputs.
//
// Put together: check 1 makes the decode-error branch unexpressible inside
// presentedSecret; check 3 makes any OTHER early exit from the handler
// unexpressible too, whatever fact it branches on; and
// TestUIViewsRequireCookie (internal/httpapi/server_test.go) is the
// response-level artifact proving the three outcomes this leaves —
// "missing", "wrong" and "malformed" — really do answer byte-identically
// over HTTP. None of the three, alone or together, measures real elapsed
// time.
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
			// A renamed or removed target function must break this gate,
			// not silently pass it — there is nothing left to check the
			// property against.
			t.Fatal("internal/httpapi declares no func presentedSecret — renamed or removed; this gate has nothing to check")
		}
		if ok, reason := decodeHelperSignatureIsSound(fn); !ok {
			t.Errorf("internal/httpapi/cookie.go's presentedSecret: %s", reason)
		}
	})

	t.Run("requireCookie and requireToken each transitively reach subtle.ConstantTimeCompare", func(t *testing.T) {
		for _, name := range []string{"requireCookie", "requireToken"} {
			fn, ok := funcs[name]
			if !ok {
				// Same vacuity guard as above: a renamed or removed
				// requireCookie/requireToken leaves nothing to check.
				t.Fatalf("internal/httpapi declares no top-level func %s — renamed or removed; this gate has nothing to check", name)
			}

			closure := transitiveSamePackageCallClosure(fn, funcs)
			if !anyReachesSubtleConstantTimeCompare(closure) {
				var walked []string
				for reached := range closure {
					walked = append(walked, reached)
				}
				t.Errorf(
					"%s (transitively through %v) never calls subtle.ConstantTimeCompare — a "+
						"secret comparison must run in constant time (ADR-0007, ADR-0028); a plain "+
						"byte-equality helper, anywhere in that call closure, leaks timing "+
						"information about a partial match",
					name, walked,
				)
			}
		}
	})

	t.Run("the per-request handler's only early return is the one guarded by the compare", func(t *testing.T) {
		for _, name := range []string{"requireCookie", "requireToken"} {
			fn := funcs[name]

			lit := handlerFuncLit(fn)
			if lit == nil {
				// Same vacuity guard as the two subtests above: a
				// requireCookie/requireToken that no longer builds its
				// per-request handler as http.HandlerFunc(func(w, r)
				// {...}) has been restructured in a way this check has
				// nothing to walk.
				t.Fatalf(
					"%s: found no http.HandlerFunc(func(http.ResponseWriter, *http.Request) {...}) "+
						"literal — renamed or restructured; this gate has nothing to check", name,
				)
			}

			violations := earlyReturnsNotGuardedByCompare(lit, fset, funcs)
			if len(violations) == 0 {
				continue
			}
			t.Errorf(
				"%s's per-request handler has a `return` not guarded by an `if` that reaches "+
					"subtle.ConstantTimeCompare (%s) — the only return statement that literal may "+
					"contain is the one inside the if that runs the constant-time compare; any other "+
					"early exit lets a caller re-derive some other fact (a missing cookie, an "+
					"undecodable value, a missing header, ...) and answer from it before the compare "+
					"ever runs, which is exactly the timing and code-path oracle ADR-0007/ADR-0028 "+
					"forbid",
				name, strings.Join(violations, ", "),
			)
		}
	})
}

// handlerFuncLit finds the *ast.FuncLit passed to http.HandlerFunc(...)
// inside fn's body — the per-request handler requireCookie/requireToken
// each build and return from their outer closure. It returns nil when no
// such call exists (fn restructured in a way this gate no longer
// recognizes), never a wrong literal: the first http.HandlerFunc(...) call
// found is the only one either function builds today.
func handlerFuncLit(fn *ast.FuncDecl) *ast.FuncLit {
	if fn.Body == nil {
		return nil
	}

	var found *ast.FuncLit
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "http" || sel.Sel.Name != "HandlerFunc" {
			return true
		}
		if len(call.Args) != 1 {
			return true
		}
		lit, ok := call.Args[0].(*ast.FuncLit)
		if !ok {
			return true
		}
		found = lit
		return false
	})
	return found
}

// earlyReturnsNotGuardedByCompare returns one formatted "line N" entry per
// *ast.ReturnStmt in lit's body whose innermost enclosing *ast.IfStmt does
// not reach subtle.ConstantTimeCompare (directly, or transitively through
// the same same-package call resolution anyReachesSubtleConstantTimeCompare
// already uses) — including a return with NO enclosing if at all, which is
// never guarded by anything. An empty, nil result means every return in the
// literal is the one permitted return.
func earlyReturnsNotGuardedByCompare(lit *ast.FuncLit, fset *token.FileSet, funcs map[string]*ast.FuncDecl) []string {
	var violations []string
	for ret, enclosingIf := range returnsByEnclosingIf(lit.Body) {
		if enclosingIf != nil && conditionReachesSubtleConstantTimeCompare(enclosingIf.Cond, funcs) {
			continue
		}
		violations = append(violations, fmt.Sprintf("line %d", fset.Position(ret.Pos()).Line))
	}
	sort.Strings(violations)
	return violations
}

// returnsByEnclosingIf maps every *ast.ReturnStmt directly inside body to
// the innermost *ast.IfStmt that lexically contains it — nil when a return
// sits in body (or in a for/switch/select arm) with no enclosing if at all.
// It walks statements, not expressions, and deliberately does not descend
// into a nested *ast.FuncLit's own body: a closure's returns belong to that
// closure, not to body's (see this file's own "Scope, stated plainly" doc
// comment).
func returnsByEnclosingIf(body *ast.BlockStmt) map[*ast.ReturnStmt]*ast.IfStmt {
	result := map[*ast.ReturnStmt]*ast.IfStmt{}

	var walkList func(list []ast.Stmt, enclosing *ast.IfStmt)
	var walk func(stmt ast.Stmt, enclosing *ast.IfStmt)

	walkList = func(list []ast.Stmt, enclosing *ast.IfStmt) {
		for _, s := range list {
			walk(s, enclosing)
		}
	}

	walk = func(stmt ast.Stmt, enclosing *ast.IfStmt) {
		switch s := stmt.(type) {
		case *ast.BlockStmt:
			walkList(s.List, enclosing)
		case *ast.IfStmt:
			walk(s.Body, s)
			if s.Else != nil {
				walk(s.Else, s)
			}
		case *ast.ForStmt:
			if s.Body != nil {
				walk(s.Body, enclosing)
			}
		case *ast.RangeStmt:
			if s.Body != nil {
				walk(s.Body, enclosing)
			}
		case *ast.SwitchStmt:
			for _, c := range s.Body.List {
				walk(c, enclosing)
			}
		case *ast.TypeSwitchStmt:
			for _, c := range s.Body.List {
				walk(c, enclosing)
			}
		case *ast.SelectStmt:
			for _, c := range s.Body.List {
				walk(c, enclosing)
			}
		case *ast.CaseClause:
			walkList(s.Body, enclosing)
		case *ast.CommClause:
			walkList(s.Body, enclosing)
		case *ast.LabeledStmt:
			walk(s.Stmt, enclosing)
		case *ast.ReturnStmt:
			result[s] = enclosing
		}
	}

	walkList(body.List, nil)
	return result
}

// conditionReachesSubtleConstantTimeCompare reports whether cond contains a
// call expression that reaches subtle.ConstantTimeCompare — either directly
// (subtle.ConstantTimeCompare(...) written inline in cond) or transitively,
// through the identical same-package unqualified-call resolution
// anyReachesSubtleConstantTimeCompare/transitiveSamePackageCallClosure
// already use elsewhere in this file: an unqualified call in cond to an
// in-package function whose own transitive closure reaches
// subtle.ConstantTimeCompare counts too, so moving the compare into a
// helper (the shape check 2's own doc comment already treats as sound) does
// not also break this rule. Consistent with this file's stated scope, a
// method call or a cross-package call in cond is never resolved here
// either.
func conditionReachesSubtleConstantTimeCompare(cond ast.Expr, funcs map[string]*ast.FuncDecl) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := fun.X.(*ast.Ident); ok && pkg.Name == "subtle" && fun.Sel.Name == "ConstantTimeCompare" {
				found = true
			}
		case *ast.Ident:
			if callee, ok := funcs[fun.Name]; ok {
				if anyReachesSubtleConstantTimeCompare(transitiveSamePackageCallClosure(callee, funcs)) {
					found = true
				}
			}
		}
		return true
	})
	return found
}

// parsePackageFuncDecls parses every non-test .go file directly inside dir
// and returns its top-level, non-method function declarations keyed by
// name, plus the *token.FileSet they were parsed with — callers that need a
// line number for a diagnostic (earlyReturnsNotGuardedByCompare) resolve it
// through this same fset, never a fresh one, since a *ast.Node's Pos() is
// only meaningful against the fset that produced it. Only top-level
// functions are indexed (fn.Recv == nil) — a method's receiver would need a
// static type resolution this AST-only walk does not perform, the same
// boundary this file's own scope note above states.
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

// decodeHelperSignatureIsSound reports whether fn's signature is exactly
// "returns a single []byte, no error, no http.ResponseWriter parameter" —
// the shape that leaves no decode-error branch to write an early return in.
// The reason string, non-empty only when ok is false, names which part of
// the signature failed so a contributor who adds an error return (the exact
// shape that reopened this bug twice before, see this file's own doc
// comment) sees why immediately rather than re-deriving it.
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
// parsed by go/ast as a plain *ast.Ident (error is a predeclared type, not a
// keyword, so it carries no dedicated node kind of its own).
func exprIsErrorType(e ast.Expr) bool {
	ident, ok := e.(*ast.Ident)
	return ok && ident.Name == "error"
}

// exprIsHTTPResponseWriterType reports whether e names http.ResponseWriter,
// directly (`http.ResponseWriter`) or through a pointer
// (`*http.ResponseWriter`) — ResponseWriter is itself an interface, so a
// pointer to one is not idiomatic Go, but this check does not rely on that
// convention holding.
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
// effort (a type shape this function does not special-case renders as its
// Go syntax node type name instead of source text), which is acceptable
// here since every shape this gate's own test cases exercise (identifiers,
// selectors, slices, pointers) is covered.
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

// transitiveSamePackageCallClosure returns every function reachable from
// start by following same-package, unqualified function calls (an
// *ast.CallExpr whose Fun is a plain *ast.Ident naming a function present in
// funcs), to fixpoint, keyed by name and including start itself. visited
// (implicit in the returned map) doubles as the cycle guard: a function is
// added to the result, and so skipped on rediscovery, before its own body is
// walked for further callees — the same discover-before-explore shape any
// sound DFS/BFS reachability walk over a possibly cyclic graph needs.
func transitiveSamePackageCallClosure(start *ast.FuncDecl, funcs map[string]*ast.FuncDecl) map[string]*ast.FuncDecl {
	closure := map[string]*ast.FuncDecl{start.Name.Name: start}
	queue := []*ast.FuncDecl{start}
	for len(queue) > 0 {
		fn := queue[0]
		queue = queue[1:]
		for _, calleeName := range sameUnqualifiedCallNames(fn) {
			if _, already := closure[calleeName]; already {
				continue
			}
			callee, ok := funcs[calleeName]
			if !ok {
				// Not a same-package top-level function this walk can
				// follow (stdlib, cross-package, a method, or a variable
				// holding a func value) — outside this gate's stated
				// scope, not an error.
				continue
			}
			closure[calleeName] = callee
			queue = append(queue, callee)
		}
	}
	return closure
}

// sameUnqualifiedCallNames returns the name of every function fn's body
// calls through a bare identifier (`f(...)`, never `pkg.f(...)` or
// `x.Method(...)`), in the order ast.Inspect visits them; duplicates are
// possible and harmless, since the caller de-duplicates via its own
// closure map.
func sameUnqualifiedCallNames(fn *ast.FuncDecl) []string {
	if fn.Body == nil {
		return nil
	}
	var names []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok {
			names = append(names, ident.Name)
		}
		return true
	})
	return names
}

// anyReachesSubtleConstantTimeCompare reports whether any function in
// closure contains a call expression of the exact shape
// subtle.ConstantTimeCompare(...).
func anyReachesSubtleConstantTimeCompare(closure map[string]*ast.FuncDecl) bool {
	for _, fn := range closure {
		if fn.Body == nil {
			continue
		}
		found := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && pkg.Name == "subtle" && sel.Sel.Name == "ConstantTimeCompare" {
				found = true
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}
