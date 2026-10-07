package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// serveGateViolations is the structural check behind
// TestServe_OneFocusKeeperSharedByTodayAndDigest (design m4c §3.8). It reads
// the non-test sources of cmd/nooma, given as name -> source, and reports each
// way the previous focus could stop being ONE object shared by every writer.
//
// Today and the digest only share an incumbent because serve hands them the
// same *brain.FocusKeeper. Nothing in the type system says so: two keepers
// compile, pass every unit test, and leave the digest reading an incumbent
// Today never writes. So the gate reads the wiring:
//
//	(a) exactly one wireFocus call, in serve.go, assigned to an identifier X;
//	(b) wireToday is called with X as its last argument;
//	(c) NewFocusKeeper is called only inside wireFocus, and wireFocus only
//	    from serve.go.
func serveGateViolations(t *testing.T, srcs map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for name, src := range srcs {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files[name] = f
	}

	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	// X is the identifier serve.go assigns wireFocus's result to.
	keeperVar := ""
	focusCallsInServe := 0
	for name, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch calleeName(call) {
				case "wireFocus":
					if name != "serve.go" {
						add("%s: wireFocus called outside serve.go (in %s) — a second keeper", name, fn.Name.Name)
					} else {
						focusCallsInServe++
					}
				case "NewFocusKeeper":
					if fn.Name.Name != "wireFocus" {
						add("%s: NewFocusKeeper called in %s — only wireFocus may build the keeper", name, fn.Name.Name)
					}
				}
				return true
			})
		}
	}

	serve, ok := files["serve.go"]
	if !ok {
		return append(out, "serve.go is missing from the scanned sources")
	}
	if focusCallsInServe != 1 {
		add("serve.go: %d wireFocus calls, want exactly 1", focusCallsInServe)
	}
	ast.Inspect(serve, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || calleeName(call) != "wireFocus" {
			return true
		}
		if id, ok := assign.Lhs[0].(*ast.Ident); ok && len(assign.Lhs) == 1 {
			keeperVar = id.Name
		}
		return true
	})
	if keeperVar == "" {
		add("serve.go: wireFocus's result is not assigned to one identifier")
	}

	ast.Inspect(serve, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || calleeName(call) != "wireToday" {
			return true
		}
		if !lastArgIs(call, keeperVar) {
			add("serve.go: wireToday's last argument is not %q, the one shared keeper", keeperVar)
		}
		return true
	})
	return out
}

// calleeName is the name a call expression invokes: f(...) and pkg.f(...)
// both answer "f".
func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// lastArgIs reports whether call's last argument is the bare identifier name.
func lastArgIs(call *ast.CallExpr, name string) bool {
	if name == "" || len(call.Args) == 0 {
		return false
	}
	id, ok := call.Args[len(call.Args)-1].(*ast.Ident)
	return ok && id.Name == name
}

// goodServeSources is the wiring the gate must accept: the shape of the real
// cmd/nooma, cut down to what the gate reads.
func goodServeSources() map[string]string {
	return map[string]string{
		"serve.go": `package main

func runServe() {
	focusKeeper := wireFocus(db)
	today := wireToday(db, focusKeeper)
	_ = today
}
`,
		"wiring.go": `package main

func wireFocus(db *sqlite.Vault) *brain.FocusKeeper {
	return brain.NewFocusKeeper(units, cfg, rels)
}

func wireToday(db *sqlite.Vault, keeper *brain.FocusKeeper) *brain.TodayService {
	return brain.NewTodayService(clock, units, cfg, state, triggers, questions, log, keeper)
}
`,
	}
}

// TestServe_OneFocusKeeperSharedByTodayAndDigest fails when cmd/nooma stops
// wiring one shared FocusKeeper. It reads the real sources, and then proves
// the checker itself against probes: a gate that never fires on the broken
// shapes is not a gate.
func TestServe_OneFocusKeeperSharedByTodayAndDigest(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read cmd/nooma: %v", err)
	}
	sources := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		sources[e.Name()] = string(src)
	}
	if got := serveGateViolations(t, sources); len(got) != 0 {
		t.Errorf("cmd/nooma does not wire one shared FocusKeeper:\n  %s", strings.Join(got, "\n  "))
	}

	if got := serveGateViolations(t, goodServeSources()); len(got) != 0 {
		t.Fatalf("the gate fires on the correct wiring: %v", got)
	}

	probes := map[string]func(m map[string]string){
		"a second wireFocus call in serve.go": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "today := ", "other := wireFocus(db)\n\t_ = other\n\ttoday := ", 1)
		},
		"wireToday with the keeper built inline": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "wireToday(db, focusKeeper)", "wireToday(db, wireFocus(db))", 1)
		},
		"wireToday passed a different identifier": func(m map[string]string) {
			m["serve.go"] = strings.Replace(m["serve.go"], "wireToday(db, focusKeeper)", "wireToday(db, otherKeeper)", 1)
		},
		"wireFocus called outside serve.go": func(m map[string]string) {
			m["wiring.go"] += "\nfunc wireElsewhere(db *sqlite.Vault) { _ = wireFocus(db) }\n"
		},
		"NewFocusKeeper called outside wireFocus": func(m map[string]string) {
			m["wiring.go"] += "\nfunc wireProactive(db *sqlite.Vault) { _ = brain.NewFocusKeeper(units, cfg, rels) }\n"
		},
	}
	for name, mutate := range probes {
		t.Run("fires on "+name, func(t *testing.T) {
			m := goodServeSources()
			mutate(m)
			if got := serveGateViolations(t, m); len(got) == 0 {
				t.Fatalf("the gate is silent on %q", name)
			}
		})
	}
}
