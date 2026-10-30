package harness

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// sandboxViolations scans every _test.go file under root and reports, as
// "<dir relative to root>: <what>", each package whose tests run nooma
// without starting in sandbox.Main, and each test that empties NOOMA_VAULT
// for its own process or for a child that does not get its own HOME. It also
// returns the packages it found running nooma, so a caller can tell the gate
// saw them.
//
// Running nooma, as read from the syntax tree, is any of: a call to run() in a
// package main (cmd/nooma's dispatch), a call to binaryPath() (the e2e
// harness's built binary), an exec of a `go build` of ./cmd/nooma, or a call
// to config.ResolveVault.
func sandboxViolations(root string) (violations []string, runsNooma map[string]bool, err error) {
	type pkg struct {
		runs, sandboxed bool
		why             string
	}
	pkgs := map[string]*pkg{}
	runsNooma = map[string]bool{}

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), p, src, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if pkgs[rel] == nil {
			pkgs[rel] = &pkg{}
		}
		k := pkgs[rel]
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				if n.Name.Name == "TestMain" && n.Recv == nil && callsSandboxMain(n.Body) {
					k.sandboxed = true
				}
			case *ast.CallExpr:
				if why := runsNoomaCall(file.Name.Name, n); why != "" && !k.runs {
					k.runs, k.why = true, why
				}
				if v := emptiesNoomaVault(n); v != "" {
					violations = append(violations, rel+": "+v+" ("+filepath.Base(p)+")")
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	for rel, k := range pkgs {
		if !k.runs {
			continue
		}
		runsNooma[rel] = true
		if !k.sandboxed {
			violations = append(violations, rel+": its tests "+k.why+" but no TestMain calls sandbox.Main, so a command given no vault could resolve a real one (docs/06-harness.md §3)")
		}
	}
	sort.Strings(violations)
	return violations, runsNooma, nil
}

// callsSandboxMain reports whether body calls sandbox.Main.
func callsSandboxMain(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && isSelector(c.Fun, "sandbox", "Main") {
			found = true
		}
		return !found
	})
	return found
}

// runsNoomaCall names how call runs nooma, or "" if it does not.
func runsNoomaCall(pkgName string, call *ast.CallExpr) string {
	if id, ok := call.Fun.(*ast.Ident); ok {
		if id.Name == "run" && pkgName == "main" {
			return "call run(), cmd/nooma's dispatch,"
		}
		if id.Name == "binaryPath" {
			return "run the built nooma binary"
		}
	}
	if isSelector(call.Fun, "config", "ResolveVault") {
		return "call config.ResolveVault"
	}
	if isSelector(call.Fun, "exec", "Command") || isSelector(call.Fun, "exec", "CommandContext") {
		for _, a := range call.Args {
			if s, ok := stringLit(a); ok && s == "./cmd/nooma" {
				return "build and run nooma"
			}
		}
	}
	return ""
}

// emptiesNoomaVault names how call leaves NOOMA_VAULT empty, or "".
func emptiesNoomaVault(call *ast.CallExpr) string {
	if (isMethod(call.Fun, "Setenv") || isSelector(call.Fun, "os", "Setenv")) && len(call.Args) == 2 {
		if k, ok := stringLit(call.Args[0]); ok && k == "NOOMA_VAULT" {
			if v, ok := stringLit(call.Args[1]); ok && v == "" {
				return "sets NOOMA_VAULT empty, which sends resolution up the working directory's parents"
			}
		}
	}
	if isSelector(call.Fun, "os", "Unsetenv") && len(call.Args) == 1 {
		if k, ok := stringLit(call.Args[0]); ok && k == "NOOMA_VAULT" {
			return "unsets NOOMA_VAULT, which sends resolution up the working directory's parents"
		}
	}
	// An environment list for a child: "NOOMA_VAULT=" is allowed only beside
	// the child's own HOME in the same list.
	emptyVault, home := false, false
	for _, a := range call.Args {
		if s, ok := stringLit(a); ok && s == "NOOMA_VAULT=" {
			emptyVault = true
		}
		if s, ok := leftmostLit(a); ok && strings.HasPrefix(s, "HOME=") {
			home = true
		}
	}
	if emptyVault && !home {
		return "gives a child an empty NOOMA_VAULT without its own HOME"
	}
	return ""
}

func isSelector(e ast.Expr, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == pkg
}

// isMethod reports a call of name on any receiver: t.Setenv, b.Setenv.
func isMethod(e ast.Expr, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// leftmostLit is the string literal an expression like "HOME="+home starts with.
func leftmostLit(e ast.Expr) (string, bool) {
	for {
		switch x := e.(type) {
		case *ast.BinaryExpr:
			e = x.X
		default:
			return stringLit(e)
		}
	}
}
