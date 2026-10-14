// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// configSetterNames are the five ConfigRepo setters m4e2-admin adds (design
// Q5). The port's own shape is closed by TestConfigRepo_ExposesExactlyTheQ5Setters.
var configSetterNames = []string{
	"SetWeightThreshold", "SetHysteresisMargin", "SetGoalStagnationDays",
	"SetMentalLoadThreshold", "SetConsolidationEnabled",
}

// TestConfigSettersAreCalledOnlyInsideTheLoggedDoor is m4e2-admin's G5: no
// admin config write without its config.updated row. Every call to one of the
// five setters in non-test code under internal/** (the store, which
// implements them, excepted) and cmd/** must sit inside a function literal
// passed to a call named write, in internal/brain/admin*.go: the door that
// writes first and then records. The name rule needs no type information
// because AdminService exports only View and Update
// (TestAdminServiceExportsOnlyViewAndUpdate), so no service method can share
// a setter's name. The order inside the door is pinned behaviourally
// (TestAdmin_ApplyFailureLogsNothing).
func TestConfigSettersAreCalledOnlyInsideTheLoggedDoor(t *testing.T) {
	root := repoRootFromCaller(t)
	scanned := 0
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if rel == "internal/store" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			for _, v := range configDoorViolations(t, rel, string(src)) {
				t.Error(v)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", top, err)
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no files")
	}
}

// configDoorViolations returns one message per call to a config setter in src
// (a file at repo-relative path name) that is not inside a function literal
// passed to write in internal/brain/admin*.go.
func configDoorViolations(t *testing.T, name, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	inDoorFile := filepath.ToSlash(filepath.Dir(name)) == "internal/brain" && strings.HasPrefix(filepath.Base(name), "admin")

	var out []string
	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !slices.Contains(configSetterNames, sel.Sel.Name) {
			return true
		}
		if !inDoorFile || !insideWriteClosure(stack) {
			out = append(out, fmt.Sprintf("%s: %s is called outside the logged door "+
				"(a function literal passed to write in internal/brain/admin*.go): "+
				"a config write must record its config.updated row", fset.Position(call.Pos()), sel.Sel.Name))
		}
		return true
	})
	return out
}

// insideWriteClosure reports whether the innermost enclosing function literal
// is an argument of a call whose selector is named write.
func insideWriteClosure(stack []ast.Node) bool {
	for i := len(stack) - 1; i > 0; i-- {
		lit, ok := stack[i].(*ast.FuncLit)
		if !ok {
			continue
		}
		call, ok := stack[i-1].(*ast.CallExpr)
		if !ok || !slices.Contains(call.Args, ast.Expr(lit)) {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "write"
	}
	return false
}

// Self-proof for G5: each shape the gate exists to refuse fires, and the
// door's own closures are silent.
func TestConfigDoorGate_SelfProof(t *testing.T) {
	const door = `package brain
func (s *S) applyHysteresisMargin(ctx C, v float64) error {
	return s.write(ctx, F, 0, v, "", func(at T) error { return s.cfg.SetHysteresisMargin(ctx, v, at) })
}`
	for _, tc := range []struct {
		name, path, src string
		want            int
	}{
		{"the door's closure is silent", "internal/brain/admin_write.go", door, 0},
		{"a direct call in a service fires", "internal/brain/preferences.go",
			"package brain\nfunc (s *S) f() { s.cfg.SetHysteresisMargin(ctx, 1, at) }", 1},
		{"a direct call in cmd/nooma fires", "cmd/nooma/serve.go",
			"package main\nfunc f() { cfg.SetWeightThreshold(ctx, 1, at) }", 1},
		{"a direct call inside the door file fires", "internal/brain/admin.go",
			"package brain\nfunc (s *S) f() { s.cfg.SetGoalStagnationDays(ctx, 1, at) }", 1},
		{"a closure passed to something other than write fires", "internal/brain/admin_write.go",
			"package brain\nfunc (s *S) f() { run(func() { s.cfg.SetMentalLoadThreshold(ctx, 1, at) }) }", 1},
		{"a write closure outside the door files fires", "internal/brain/preferences.go",
			"package brain\nfunc (s *S) f() { s.write(a, func(at T) error { return s.cfg.SetConsolidationEnabled(ctx, true, at) }) }", 1},
		{"a method that is not a setter is silent", "internal/brain/preferences.go",
			"package brain\nfunc (s *S) f() { s.cfg.SetReminderPrefs(ctx, u, at) }", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := configDoorViolations(t, tc.path, tc.src); len(got) != tc.want {
				t.Errorf("violations = %v, want %d", got, tc.want)
			}
		})
	}
}
