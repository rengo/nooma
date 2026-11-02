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
// for its own process or builds a child's environment with an empty one by
// hand (sandbox.Child builds it, beside a HOME of the child's own). It also
// returns the packages it found running nooma, so a caller can tell the gate
// saw them.
//
// Running nooma, as read from the syntax tree, is any of: a call to run() in a
// package main (cmd/nooma's dispatch), a call to binaryPath() (the e2e
// harness's built binary), a file that execs `go` and names the nooma command
// package as a string (./cmd/nooma, or its module path, even when the
// arguments travel in a slice), or a call to ResolveVault on the config
// package, under whatever name the file imports it. The gate reads literals:
// a vault key held in a constant is resolved within its own file, and a path
// assembled at run time (filepath.Join) is not seen.
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
			case "worktrees":
				// .claude/worktrees holds live copies of this repository made by
				// review agents; scanning them would fail on whether another
				// agent happens to be running (as i11's scan found on PR 5).
				if filepath.Base(filepath.Dir(p)) == ".claude" {
					return filepath.SkipDir
				}
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
		consts := stringConsts(file)
		config := importName(file, "config")
		goExec, noomaPath := false, false
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				if n.Name.Name == "TestMain" && n.Recv == nil && callsSandboxMain(n.Body) {
					k.sandboxed = true
				}
			case *ast.BasicLit:
				if s, ok := stringLit(n); ok {
					if s == emptyVaultEntry {
						violations = append(violations, rel+": builds an environment entry with an empty NOOMA_VAULT by hand; sandbox.Child builds a child's, beside its own HOME ("+filepath.Base(p)+")")
					}
					if s == "./cmd/nooma" || s == "cmd/nooma" || strings.HasSuffix(s, "/cmd/nooma") {
						noomaPath = true
					}
				}
			case *ast.CallExpr:
				if why := runsNoomaCall(file.Name.Name, config, n); why != "" && !k.runs {
					k.runs, k.why = true, why
				}
				if execsGo(n) {
					goExec = true
				}
				if v := emptiesNoomaVault(n, consts); v != "" {
					violations = append(violations, rel+": "+v+" ("+filepath.Base(p)+")")
				}
			}
			return true
		})
		if goExec && noomaPath && !k.runs {
			k.runs, k.why = true, "build and run nooma"
		}
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

// emptyVaultEntry is the environment entry that empties a child's vault. The
// gate looks for it as a string literal, so it is spelled in two pieces here.
const emptyVaultEntry = "NOOMA_VAULT" + "="

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

// runsNoomaCall names how call runs nooma, or "" if it does not. config is
// the name the file gives the config package ("" if it does not import it).
func runsNoomaCall(pkgName, config string, call *ast.CallExpr) string {
	if id, ok := call.Fun.(*ast.Ident); ok {
		if id.Name == "run" && pkgName == "main" {
			return "call run(), cmd/nooma's dispatch,"
		}
		if id.Name == "binaryPath" {
			return "run the built nooma binary"
		}
	}
	if config != "" && isSelector(call.Fun, config, "ResolveVault") {
		return "call config.ResolveVault"
	}
	return ""
}

// execsGo reports whether call is exec.Command or exec.CommandContext of "go".
func execsGo(call *ast.CallExpr) bool {
	switch {
	case isSelector(call.Fun, "exec", "Command") && len(call.Args) > 0:
		s, ok := stringLit(call.Args[0])
		return ok && s == "go"
	case isSelector(call.Fun, "exec", "CommandContext") && len(call.Args) > 1:
		s, ok := stringLit(call.Args[1])
		return ok && s == "go"
	}
	return false
}

// importName is the name file gives the package whose import path ends in
// "/<last>" (or is <last>): its alias, or <last>. "" when not imported.
func importName(file *ast.File, last string) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || (path != last && !strings.HasSuffix(path, "/"+last)) {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return last
	}
	return ""
}

// stringConsts maps each constant in file bound to a string literal to it.
func stringConsts(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs := sp.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					if s, ok := stringLit(vs.Values[i]); ok {
						out[name.Name] = s
					}
				}
			}
		}
	}
	return out
}

// emptiesNoomaVault names how call leaves NOOMA_VAULT empty in the test's own
// process, or "".
func emptiesNoomaVault(call *ast.CallExpr, consts map[string]string) string {
	str := func(e ast.Expr) (string, bool) {
		if id, ok := e.(*ast.Ident); ok {
			s, found := consts[id.Name]
			return s, found
		}
		return stringLit(e)
	}
	if isMethod(call.Fun, "Setenv") && len(call.Args) == 2 {
		if k, ok := str(call.Args[0]); ok && k == "NOOMA_VAULT" {
			if v, ok := str(call.Args[1]); ok && v == "" {
				return "sets NOOMA_VAULT empty, which sends resolution up the working directory's parents"
			}
		}
	}
	if isSelector(call.Fun, "os", "Unsetenv") && len(call.Args) == 1 {
		if k, ok := str(call.Args[0]); ok && k == "NOOMA_VAULT" {
			return "unsets NOOMA_VAULT, which sends resolution up the working directory's parents"
		}
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
