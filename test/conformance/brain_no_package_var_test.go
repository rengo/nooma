// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packageVarViolations returns one message per package-level var spec in src
// that is not one of the two allowed shapes. It is default-deny and purely
// syntactic: it never asks what a variable's type is, because that is the
// question a syntactic gate cannot answer and must not guess at.
//
// Allowed:
//  1. a blank assertion: every name is `_` (`var _ T = ...`);
//  2. an error sentinel: every value is a call to errors.New or fmt.Errorf.
//
// Both shapes hold no state a service could share, which is what keeps the
// gate from firing on correct code.
func packageVarViolations(t *testing.T, name, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if allBlank(vs.Names) || allErrorSentinels(vs.Values) {
				continue
			}
			out = append(out, fset.Position(vs.Pos()).String()+": package-level var "+vs.Names[0].Name)
		}
	}
	return out
}

func allBlank(names []*ast.Ident) bool {
	for _, n := range names {
		if n.Name != "_" {
			return false
		}
	}
	return true
}

func allErrorSentinels(values []ast.Expr) bool {
	if len(values) == 0 {
		return false
	}
	for _, v := range values {
		call, ok := v.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return false
		}
		isNew := pkg.Name == "errors" && sel.Sel.Name == "New"
		isErrorf := pkg.Name == "fmt" && sel.Sel.Name == "Errorf"
		if !isNew && !isErrorf {
			return false
		}
	}
	return true
}

// TestBrain_DeclaresNoPackageLevelVar is the structural gate behind I01's
// "a fresh service has no incumbent" (design m4c §3.8): a package-level
// variable in internal/brain would be shared by every service in the process,
// every test included, so a freshly constructed FocusKeeper could no longer
// be trusted to be fresh. The incumbent lives in a keeper that cmd/nooma
// builds once; this makes the other place it could live unexpressible.
//
// Non-test .go files only: a test may declare whatever fixture it likes.
func TestBrain_DeclaresNoPackageLevelVar(t *testing.T) {
	repoRoot := repoRootFromCaller(t)

	scanned := 0
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal", "brain"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, v := range packageVarViolations(t, path, string(src)) {
			t.Errorf("%s — internal/brain holds state in a keeper cmd/nooma builds once, never in a package variable (I01)", v)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/brain: %v", err)
	}
	if scanned == 0 {
		t.Fatal("scanned zero non-test .go files under internal/brain/")
	}
}

// TestBrain_NoPackageLevelVarGateProvesItself runs the gate's own checker
// over probe sources: it must fire on every shape that can hold shared
// state, whatever its type, and stay silent on the two allowed shapes, so the
// gate is proved neither blind nor over-eager.
func TestBrain_NoPackageLevelVarGateProvesItself(t *testing.T) {
	// Parsed only, never type-checked, so the probes need no real imports.
	const header = "package brain\n\n"

	fires := map[string]string{
		"keeper value":    "var defaultKeeper = NewFocusKeeper(nil, nil, nil)\n",
		"atomic pointer":  "var held atomic.Pointer[int]\n",
		"map":             "var cache = map[string]int{}\n",
		"int":             "var n = 0\n",
		"mutex":           "var mu sync.Mutex\n",
		"pointer":         "var x *int\n",
		"grouped mixed":   "var (\n\tErrX = errors.New(\"x\")\n\tcount = 0\n)\n",
		"sentinel + call": "var ErrY, z = errors.New(\"y\"), len(\"z\")\n",
	}
	for name, body := range fires {
		t.Run("fires on "+name, func(t *testing.T) {
			if got := packageVarViolations(t, name+".go", header+body); len(got) == 0 {
				t.Fatalf("the gate is silent on %q — a package variable would slip through", strings.TrimSpace(body))
			}
		})
	}

	silent := map[string]string{
		"errors.New sentinel": "var ErrX = errors.New(\"x\")\n",
		"fmt.Errorf sentinel": "var ErrY = fmt.Errorf(\"y\")\n",
		"blank assertion":     "var _ fmt.Stringer = (*strings.Builder)(nil)\n",
		"grouped allowed":     "var (\n\tErrA = errors.New(\"a\")\n\t_ sync.Locker = (*sync.Mutex)(nil)\n\tErrB = fmt.Errorf(\"b\")\n)\n",
	}
	for name, body := range silent {
		t.Run("silent on "+name, func(t *testing.T) {
			if got := packageVarViolations(t, name+".go", header+body); len(got) != 0 {
				t.Fatalf("the gate fires on correct code %q: %v", strings.TrimSpace(body), got)
			}
		})
	}
}
