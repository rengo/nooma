package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestServe_WiresTheLearningServiceIntoTheAPIAndTheUI: serve.go hands the
// service wireLearning builds to httpapi.Deps.Learning and to uiDeps, or GET
// /learning and /ui/learning would answer 503 in production. Read from the
// source, the serve gate's own approach: runServe needs a whole vault and a
// listener to observe this any other way.
func TestServe_WiresTheLearningServiceIntoTheAPIAndTheUI(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "serve.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var wired string // the variable assigned wireLearning(...)
	toAPI, toUI := "", ""
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if c := asCall(n.Rhs[len(n.Rhs)-1]); len(n.Lhs) == 1 && c != nil && calleeName(c) == "wireLearning" {
				if id, ok := n.Lhs[0].(*ast.Ident); ok {
					wired = id.Name
				}
			}
		case *ast.CompositeLit:
			if sel, ok := n.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Deps" {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "httpapi" {
					for _, el := range n.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Learning" {
								if v, ok := kv.Value.(*ast.Ident); ok {
									toAPI = v.Name
								}
							}
						}
					}
				}
			}
		case *ast.CallExpr:
			if calleeName(n) == "uiDeps" && len(n.Args) >= 2 {
				if v, ok := n.Args[len(n.Args)-2].(*ast.Ident); ok {
					toUI = v.Name
				}
			}
		}
		return true
	})
	if wired == "" {
		t.Fatal("serve.go assigns no variable from wireLearning(...)")
	}
	if toAPI != wired {
		t.Errorf("httpapi.Deps.Learning is %q, want %q, the service wireLearning built", toAPI, wired)
	}
	if toUI != wired {
		t.Errorf("uiDeps' learning argument is %q, want %q, the service wireLearning built", toUI, wired)
	}
}

func asCall(e ast.Expr) *ast.CallExpr {
	c, _ := e.(*ast.CallExpr)
	return c
}
