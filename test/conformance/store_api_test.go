package conformance

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// update regenerates testdata/schema/store_api.golden from the real,
// current exported surface of internal/store/**. Run via `make
// store-api-golden`, never by hand-editing the golden file.
var update = flag.Bool("update", false, "update the store API golden file")

const storeAPIGoldenPath = "testdata/schema/store_api.golden"

const storeAPIGoldenHeader = "" +
	"# nooma store API golden — regenerate with `make store-api-golden`.\n" +
	"# Adding a line here is a deliberate widening of the store surface. Read it as such.\n"

// TestHarness_StoreAPIUnchanged walks internal/store/** with go/parser
// (skipping _test.go files), collects every exported top-level
// declaration's rendered signature, sorts the result, and compares it
// against testdata/schema/store_api.golden (design §7.3, requirements
// R12.1/R12.2).
//
// This is the third, mechanized layer of the scope boundary proposal §3.1
// states in prose: "the store surface can open a vault and migrate it, and
// cannot read or write a single domain row." The type layer (an
// unexported *sql.DB field) and the import layer (the sqlite-containment
// depguard rule) are both defeatable in isolation; this golden is what
// turns a widening of the surface into a reviewable diff instead of a
// silent addition — the day someone exposes a method that reads a domain
// row, this test fails and names it.
func TestHarness_StoreAPIUnchanged(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	storeDir := filepath.Join(repoRoot, "internal", "store")

	lines := collectExportedSurface(t, repoRoot, storeDir)
	if len(lines) == 0 {
		t.Fatalf("collected zero exported declarations under internal/store/** — the walk is broken, not the surface (design D10's non-empty-corpus guard)")
	}

	got := storeAPIGoldenHeader + strings.Join(lines, "\n") + "\n"
	goldenPath := filepath.Join(repoRoot, storeAPIGoldenPath)

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `make store-api-golden` to generate it)", storeAPIGoldenPath, err)
	}

	if got != string(want) {
		t.Errorf(
			"internal/store/** exported surface does not match %s.\n"+
				"Widening the store surface is a conscious act, not an oversight (design §7.3):\n"+
				"review the diff below, then run `make store-api-golden` to accept it.\n"+
				"--- got (current surface) ---\n%s"+
				"--- want (committed golden) ---\n%s",
			storeAPIGoldenPath, got, string(want),
		)
	}
}

// repoRootFromCaller returns the repository root, derived from this test
// file's own path (two directories up from test/conformance/) rather than
// the working directory `go test` happens to use.
func repoRootFromCaller(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to report this test file's path")
	}
	// thisFile is .../test/conformance/store_api_test.go
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// collectExportedSurface walks storeDir, parses every non-test .go file,
// and renders one "<package-relative-path>: <signature>" line per exported
// top-level declaration.
func collectExportedSurface(t *testing.T, repoRoot, storeDir string) []string {
	t.Helper()

	fset := token.NewFileSet()
	var lines []string

	err := filepath.WalkDir(storeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		rel, err := filepath.Rel(repoRoot, filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("relativize %s: %w", path, err)
		}
		rel = filepath.ToSlash(rel)

		for _, decl := range file.Decls {
			for _, sig := range renderExportedDecl(t, fset, decl) {
				lines = append(lines, rel+": "+sig)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", storeDir, err)
	}

	sort.Strings(lines)
	return lines
}

// renderExportedDecl renders zero or more golden lines for one top-level
// declaration: a rendered "func ..." signature for an exported function or
// method, "type Name" for an exported type, and "var Name" / "const Name" for
// an exported variable or constant. Unexported declarations and imports
// contribute nothing.
//
// An earlier version dropped every GenDecl whose token was not TYPE, with a
// comment claiming const and var "contribute nothing — the store surface is its
// exported API, not its internals". That is backwards for a sentinel error.
// internal/store/sqlite has exported ErrRelativeDBPath since the DSN work and
// callers are expected to match on it with errors.Is; it is as much of the public
// surface as any method, and it had never appeared in the golden. The gate whose
// stated job is to make a widening of the store surface reviewable could only see
// a widening that happened to be a func or a type.
func renderExportedDecl(t *testing.T, fset *token.FileSet, decl ast.Decl) []string {
	t.Helper()

	switch d := decl.(type) {
	case *ast.FuncDecl:
		if !d.Name.IsExported() {
			return nil
		}
		return []string{renderFuncSignature(t, fset, d)}
	case *ast.GenDecl:
		var out []string
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if s.Name.IsExported() {
					out = append(out, "type "+s.Name.Name)
				}
			case *ast.ValueSpec:
				// A single declaration may bind several names
				// (`var A, B = ...`), so every name is rendered on its own line.
				for _, name := range s.Names {
					if name.IsExported() {
						out = append(out, d.Tok.String()+" "+name.Name)
					}
				}
			}
		}
		return out
	default:
		return nil
	}
}

// renderFuncSignature renders a function or method declaration's
// signature only — no body, and for a method, the receiver's type without
// its variable name (e.g. "func (*Vault) Check(ctx context.Context)
// error"), matching design §7.3's golden format.
func renderFuncSignature(t *testing.T, fset *token.FileSet, d *ast.FuncDecl) string {
	t.Helper()

	clone := &ast.FuncDecl{
		Name: d.Name,
		Type: d.Type,
		Recv: stripFieldNames(d.Recv),
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, clone); err != nil {
		t.Fatalf("render signature of %s: %v", d.Name.Name, err)
	}
	return buf.String()
}

// stripFieldNames returns a copy of fl with every field's Names cleared,
// keeping only its Type — used to drop a receiver's variable name (e.g.
// "v *Vault" becomes "*Vault") so a rename of the receiver variable never
// touches the golden.
func stripFieldNames(fl *ast.FieldList) *ast.FieldList {
	if fl == nil {
		return nil
	}
	out := &ast.FieldList{}
	for _, f := range fl.List {
		out.List = append(out.List, &ast.Field{Type: f.Type})
	}
	return out
}

// TestHarness_StoreAPIIncludesExportedSentinels pins the half of the exported
// surface the golden was blind to.
//
// renderExportedDecl used to drop every GenDecl whose token was not TYPE, with a
// comment asserting that const and var "contribute nothing — the store surface is
// its exported API, not its internals". That is exactly backwards for a sentinel
// error: internal/store/sqlite has exported ErrRelativeDBPath since the DSN work,
// callers are expected to compare against it with errors.Is, and it had never
// appeared in the golden. A widening of the store's surface was reviewable only
// if it happened to be a func or a type.
//
// This test names one symbol deliberately rather than asserting "some var
// exists": the golden's whole purpose is that a specific surface is what review
// sees, so the assertion is specific too.
func TestHarness_StoreAPIIncludesExportedSentinels(t *testing.T) {
	repoRoot := repoRootFromCaller(t)

	golden, err := os.ReadFile(filepath.Join(repoRoot, storeAPIGoldenPath))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	const sentinel = "var ErrRelativeDBPath"
	if !strings.Contains(string(golden), sentinel) {
		t.Errorf(
			"%s does not contain %q.\n"+
				"internal/store/sqlite exports that sentinel and callers match on it with\n"+
				"errors.Is, so it is part of the store's public surface. A golden that cannot\n"+
				"see exported var and const declarations makes half a widening invisible to\n"+
				"review — including the ErrVaultInUse the lock will add next.",
			storeAPIGoldenPath, sentinel)
	}
}
