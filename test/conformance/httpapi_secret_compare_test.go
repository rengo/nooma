// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestHTTPAPISecretCompareStaysConstantTimeAndDecodeErrorFallsThrough is the
// structural gate TestUIViewsRequireCookie (internal/httpapi/server_test.go)
// cannot be: that test only ever sees requireCookie's response over HTTP,
// and both mutations judgment-day found on requireCookie produce a
// byte-identical 303 — only their TIMING differs, and no response-level
// test measures timing. What this test proves instead, on the AST rather
// than on any response:
//
//  1. requireToken (internal/httpapi/auth.go) and requireCookie
//     (internal/httpapi/cookie.go) each still call
//     crypto/subtle.ConstantTimeCompare — a plain `bytes.Equal` or other
//     byte-equality helper standing in for it leaks timing information
//     about a partial match (ADR-0007, ADR-0028) with no other observable
//     difference.
//  2. requireCookie's base64 decode-error branch contains no `return`
//     statement — it falls through to the comparison, exactly as design
//     m4a §3.3 requires, so "missing", "wrong" and "malformed" stay
//     timing-indistinguishable, not merely response-indistinguishable.
//
// Scope, stated plainly rather than implied: this checks requireCookie and
// requireToken by name — it does not generalize to "every secret
// comparison anywhere in this module" the way a generic scan would, and it
// does not measure actual timing (no test in this repository does — non-
// negotiable #5 forbids the network and real wall-clock timing assertions
// are inherently flaky). It proves the STRUCTURE that timing-safety
// depends on, the same relationship i01's own "returns or embeds a
// unit.Status" check has to I01: a static proof of shape, not a dynamic
// proof of the property the shape exists to guarantee.
func TestHTTPAPISecretCompareStaysConstantTimeAndDecodeErrorFallsThrough(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	fset := token.NewFileSet()

	t.Run("requireToken and requireCookie both compare via subtle.ConstantTimeCompare", func(t *testing.T) {
		for _, tc := range []struct {
			relPath string
			fn      string
		}{
			{"internal/httpapi/auth.go", "requireToken"},
			{"internal/httpapi/cookie.go", "requireCookie"},
		} {
			path := filepath.Join(repoRoot, tc.relPath)
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			fn := findFuncDecl(file, tc.fn)
			if fn == nil {
				// A renamed or removed target function must break this
				// gate, not silently pass it — there is nothing left to
				// check the property against.
				t.Fatalf("%s declares no func %s — renamed or removed; this gate has nothing to check", tc.relPath, tc.fn)
			}
			if !callsSubtleConstantTimeCompare(fn) {
				t.Errorf(
					"%s's %s no longer calls subtle.ConstantTimeCompare — a secret comparison "+
						"must run in constant time (ADR-0007, ADR-0028); a plain byte-equality "+
						"helper leaks timing information about a partial match",
					tc.relPath, tc.fn,
				)
			}
		}
	})

	t.Run("requireCookie's decode-error branch falls through to the comparison, never returns early", func(t *testing.T) {
		path := filepath.Join(repoRoot, "internal/httpapi/cookie.go")
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		fn := findFuncDecl(file, "requireCookie")
		if fn == nil {
			t.Fatal("internal/httpapi/cookie.go declares no func requireCookie — renamed or removed; this gate has nothing to check")
		}
		if ret := returnInsideDecodeErrorBranch(fn); ret != nil {
			t.Errorf(
				"%s: requireCookie returns early on a base64 decode error instead of falling "+
					"through to the comparison — a decode error must stay indistinguishable from "+
					"a wrong cookie in both response and timing (design m4a §3.3, ADR-0028)",
				fset.Position(ret.Pos()),
			)
		}
	})
}

// findFuncDecl returns file's top-level function declaration named name, or
// nil if file declares no such function.
func findFuncDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

// callsSubtleConstantTimeCompare reports whether fn's body contains a call
// expression of the exact shape subtle.ConstantTimeCompare(...).
func callsSubtleConstantTimeCompare(fn *ast.FuncDecl) bool {
	if fn.Body == nil {
		return false
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
	return found
}

// returnInsideDecodeErrorBranch returns the first ast.ReturnStmt found
// inside the error-handling branch of an if-statement that guards a
// *Decode()-family call's error result (e.g.
// base64.RawURLEncoding.DecodeString), or nil if no such return exists.
//
// It works in two passes over fn's body so the decode call and the error
// check do not need to sit in the same statement: pass 1 collects the name
// of every variable assigned from the second return value of a call whose
// selector ends in "DecodeString" — whichever statement form binds it,
// whether that is a plain `decoded, decErr := ...` or the same assignment
// living in an if-statement's own Init clause, since ast.Inspect walks both
// the same way. Pass 2 then finds every if-statement whose condition
// compares one of those names against `nil`, works out which branch (Body
// for `!= nil`, Else for `== nil`) is the error-handling one, and reports
// the first return statement found anywhere in that branch's subtree.
func returnInsideDecodeErrorBranch(fn *ast.FuncDecl) *ast.ReturnStmt {
	if fn.Body == nil {
		return nil
	}

	decodeErrVars := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) < 2 || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !strings.HasSuffix(sel.Sel.Name, "DecodeString") {
			return true
		}
		if errIdent, ok := assign.Lhs[len(assign.Lhs)-1].(*ast.Ident); ok {
			decodeErrVars[errIdent.Name] = true
		}
		return true
	})
	if len(decodeErrVars) == 0 {
		return nil
	}

	var found *ast.ReturnStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		bin, ok := ifStmt.Cond.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		ident, isNilLit := binaryExprAgainstNil(bin)
		if ident == nil || !isNilLit || !decodeErrVars[ident.Name] {
			return true
		}

		var errorBranch ast.Stmt
		switch bin.Op {
		case token.NEQ: // decErr != nil: the error path is the if's own Body.
			errorBranch = ifStmt.Body
		case token.EQL: // decErr == nil: the error path, if any, is the Else.
			errorBranch = ifStmt.Else
		}
		if errorBranch == nil {
			return true
		}
		ast.Inspect(errorBranch, func(m ast.Node) bool {
			if ret, ok := m.(*ast.ReturnStmt); ok && found == nil {
				found = ret
			}
			return true
		})
		return true
	})
	return found
}

// binaryExprAgainstNil reports whether bin is of the shape `ident == nil`
// or `ident != nil` (in either operand order), returning the identifier
// operand when it is.
func binaryExprAgainstNil(bin *ast.BinaryExpr) (*ast.Ident, bool) {
	if bin.Op != token.EQL && bin.Op != token.NEQ {
		return nil, false
	}
	identOperand, nilOperand := bin.X, bin.Y
	ident, identOK := identOperand.(*ast.Ident)
	nilIdent, nilOK := nilOperand.(*ast.Ident)
	if identOK && nilOK && nilIdent.Name == "nil" {
		return ident, true
	}
	// Try the reversed operand order (`nil == ident`).
	ident, identOK = nilOperand.(*ast.Ident)
	nilIdent, nilOK = identOperand.(*ast.Ident)
	if identOK && nilOK && nilIdent.Name == "nil" {
		return ident, true
	}
	return nil, false
}
