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
// "<dir relative to root>: <what>", each package that needs a sandbox and has
// none, and each test that empties NOOMA_VAULT for its own process or builds a
// child's environment with an empty one by hand (sandbox.Child builds it,
// beside a HOME of the child's own). It also returns the packages that need a
// sandbox, so a caller can tell the gate saw them.
//
// The rule does not try to recognise a command that runs nooma, which is how
// each earlier version of this gate missed one more spelling. It is
// default-deny: a package needs a TestMain calling sandbox.Main (found by the
// sandbox package's import path, whatever name the file gives it) when any of
// its test files imports os/exec, imports internal/config, imports the
// sandbox package (for Child), imports a non-test package under test/support
// that imports os/exec (directly, or through other test/support packages), or is a package main under cmd/. A
// package that execs something harmless pays for a throwaway HOME and nothing
// else.
//
// What it does not see, all named in docs/06-harness.md §3: a helper outside
// test/support that runs a command; os.StartProcess, which needs only os; a
// package config calling ResolveVault directly, which imports nothing; os under
// an import alias (Unsetenv, Clearenv); a key or value held in a variable, a
// map-built environment, or a constant declared in another file; deliberate
// evasion (a split literal such as "NOOMA_VAULT"+"=", a child given
// []string{"PATH=/bin"}, which has neither vault nor HOME, or an environment
// filtered by prefix); and build tags: a TestMain behind a tag counts for the
// whole directory. It also over-reports, loudly: a TestMain that reaches
// sandbox.Main through a local helper, a dot import of the sandbox package,
// "NOOMA_VAULT=" as a whole element of a list that only compares, and a key
// constant shadowed in another function (stringConsts ignores scope).
func sandboxViolations(root string) (violations []string, needsSandbox map[string]bool, err error) {
	type pkg struct {
		why       string
		sandboxed bool
		imports   []string
	}
	pkgs := map[string]*pkg{}
	// Non-test packages under test/support: the paths each imports.
	helpers := map[string]*pkg{}
	needsSandbox = map[string]bool{}

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
		isTest := strings.HasSuffix(p, "_test.go")
		if !isTest && !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !isTest && !strings.HasPrefix(rel+"/", "test/support/") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		mode := parser.Mode(0)
		if !isTest {
			mode = parser.ImportsOnly
		}
		file, err := parser.ParseFile(token.NewFileSet(), p, src, mode)
		if err != nil {
			return err
		}
		if !isTest {
			if helpers[rel] == nil {
				helpers[rel] = &pkg{}
			}
			for _, imp := range file.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				helpers[rel].imports = append(helpers[rel].imports, path)
			}
			return nil
		}
		for _, imp := range file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if pkgs[rel] == nil {
				pkgs[rel] = &pkg{}
			}
			pkgs[rel].imports = append(pkgs[rel].imports, path)
		}
		if pkgs[rel] == nil {
			pkgs[rel] = &pkg{}
		}
		k := pkgs[rel]
		need := func(why string) {
			if k.why == "" {
				k.why = why
			}
		}
		sb := importName(file, "test/support/sandbox")
		switch {
		case importName(file, "os/exec") != "":
			need("import os/exec")
		case importName(file, "internal/config") != "":
			need("import internal/config")
		case sb != "":
			need("import the sandbox package")
		case file.Name.Name == "main" && (rel == "cmd" || strings.HasPrefix(rel, "cmd/")):
			need("are a package main under cmd/")
		}
		consts := stringConsts(file)
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				if n.Name.Name == "TestMain" && n.Recv == nil && callsSandboxMain(n.Body, file.Name.Name, sb) {
					k.sandboxed = true
				}
			case *ast.CompositeLit:
				for _, e := range n.Elts {
					if emptyVaultElement(e) {
						violations = append(violations, rel+": "+emptyVaultMsg+" ("+filepath.Base(p)+")")
					}
				}
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "append" {
					for _, a := range n.Args {
						if emptyVaultElement(a) {
							violations = append(violations, rel+": "+emptyVaultMsg+" ("+filepath.Base(p)+")")
						}
					}
				}
				if v := emptiesNoomaVault(n, consts); v != "" {
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
	imports := map[string][]string{}
	for rel, h := range helpers {
		imports[rel] = h.imports
	}
	execs := execingHelpers(imports)
	for _, k := range pkgs {
		for _, path := range k.imports {
			if h := helperFor(path, execs); h != "" {
				if k.why == "" {
					k.why = "import " + h + ", a test/support package that runs commands"
				}
			}
		}
	}
	for rel, k := range pkgs {
		if k.why == "" {
			continue
		}
		needsSandbox[rel] = true
		if !k.sandboxed {
			violations = append(violations, rel+": its tests "+k.why+" but no TestMain calls sandbox.Main, so a command given no vault could resolve a real one (docs/06-harness.md §3)")
		}
	}
	sort.Strings(violations)
	return violations, needsSandbox, nil
}

// execingHelpers returns the test/support packages (dirs relative to the
// root) that import os/exec, or import another that does. That includes
// test/support/sandbox itself, whose importers need a sandbox by their own rule.
func execingHelpers(imports map[string][]string) map[string]bool {
	out := map[string]bool{}
	for dir, imps := range imports {
		for _, path := range imps {
			if path == "os/exec" {
				out[dir] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for dir, imps := range imports {
			if out[dir] {
				continue
			}
			for _, path := range imps {
				if helperFor(path, out) != "" {
					out[dir] = true
					changed = true
					break
				}
			}
		}
	}
	return out
}

// helperFor names the execing helper dir that import path path resolves to
// ("<module>/test/support/x" is dir "test/support/x"), or "".
func helperFor(path string, execs map[string]bool) string {
	for dir := range execs {
		if path == dir || strings.HasSuffix(path, "/"+dir) {
			return dir
		}
	}
	return ""
}

const emptyVaultMsg = "builds an environment entry with an empty NOOMA_VAULT by hand; sandbox.Child builds a child's, beside its own HOME"

// emptyVaultElement reports whether e is the literal environment entry that
// empties a child's vault, standing whole as a list element or an append
// argument. Part of a concatenation ("NOOMA_VAULT="+v) or an argument to any
// other call (strings.HasPrefix) it is not an empty entry, and is not reported.
func emptyVaultElement(e ast.Expr) bool {
	s, ok := stringLit(e)
	return ok && s == "NOOMA_VAULT="
}

// callsSandboxMain reports whether body calls Main on the sandbox package,
// named sb in this file; inside the sandbox package itself, a bare Main.
func callsSandboxMain(body *ast.BlockStmt, pkgName, sb string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return !found
		}
		if sb != "" && isSelector(c.Fun, sb, "Main") {
			found = true
		}
		if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "Main" && pkgName == "sandbox" {
			found = true
		}
		return !found
	})
	return found
}

// importName is the name file gives the package whose import path is suffix
// or ends in "/<suffix>": its alias, or the path's last element. "" when not
// imported.
func importName(file *ast.File, suffix string) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || (path != suffix && !strings.HasSuffix(path, "/"+suffix)) {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return path[strings.LastIndex(path, "/")+1:]
	}
	return ""
}

// stringConsts maps each constant bound to a string literal to it, wherever
// it is declared (a function's own constants included).
func stringConsts(file *ast.File) map[string]string {
	out := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
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
		return true
	})
	return out
}

// emptiesNoomaVault names how call leaves NOOMA_VAULT empty or unset in the
// test's own process, or "".
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
	if isSelector(call.Fun, "os", "Clearenv") {
		return "clears the environment, NOOMA_VAULT with it, which sends resolution up the working directory's parents"
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
