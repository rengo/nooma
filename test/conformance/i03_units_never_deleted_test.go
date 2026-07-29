//go:build pendingimpl

// Package conformance — see test/conformance/doc.go for the package contract.
//
// This file is tagged pendingimpl (design.md §8) and is never compiled by
// the untagged build. It is compiled in isolation by `make pending-red`
// (scripts/pending-red.sh), whose job is to confirm this package FAILS to
// compile, and fails for the right reason.
package conformance

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// TestI03_UnitsAreNeverDeleted proves invariant I03 (docs/02-cognitive-core.md
// §1, CLAUDE.md non-negotiable #6): nothing is deleted from units — archiving
// is a state transition, never a removal. No code path outside the
// migrations may emit DELETE FROM units.
//
// Anchor: ports.UnitRepo (internal/ports), spec R6.1/R6.4, design.md §8.4.
// The symbol does not exist yet — see internal/ports/doc.go and
// test/conformance/pending_symbols.txt. In this chain the RED is a compile
// error, `undefined: ports.UnitRepo` — that IS the passing state of
// scripts/pending-red.sh (design §8.1/§8.2, D9), not a defect to fix. This
// test never turns green inside this change.
//
// Promotion: the PR that adds ports.UnitRepo must, in the SAME PR, drop the
// pendingimpl tag from this file, move it into the untagged L2 suite, and
// remove its line from pending_symbols.txt (design §8.3/§8.5, spec R7.3).
//
// Two independent checks (design §8.4):
//
//  1. Reflection over the interface: no method named Delete*. A repository
//     contract that never declares a delete method makes "nothing deletes a
//     unit" a compile-time property of the port, not a discipline someone
//     has to remember at every call site.
//  2. Tree scan: no Go source file under internal/ or cmd/ issues a literal
//     DELETE FROM units statement (migrations are .sql, embedded via
//     go:embed, and are naturally outside this Go-source scan — design D1).
//
// D10's non-empty-corpus guard applies to both: a zero-method interface or a
// zero-file scan fails loudly instead of passing vacuously.
func TestI03_UnitsAreNeverDeleted(t *testing.T) {
	t.Run("repo declares no Delete method", func(t *testing.T) {
		repoType := reflect.TypeOf((*ports.UnitRepo)(nil)).Elem()
		if repoType.Kind() != reflect.Interface {
			t.Fatalf("ports.UnitRepo has kind %s, want interface", repoType.Kind())
		}
		if repoType.NumMethod() == 0 {
			t.Fatal("ports.UnitRepo declares zero methods — D10's guard: nothing to check yet")
		}
		for i := 0; i < repoType.NumMethod(); i++ {
			name := repoType.Method(i).Name
			if strings.HasPrefix(name, "Delete") {
				t.Errorf(
					"ports.UnitRepo declares %s — nothing deletes a unit "+
						"(docs/02-cognitive-core.md §1, CLAUDE.md non-negotiable #6: "+
						"archiving is a state transition, not a removal)",
					name,
				)
			}
		}
	})

	t.Run("tree scan for DELETE FROM units", func(t *testing.T) {
		repoRoot := repoRootFromCaller(t)
		report := func(path string, lineNum int, line string) {
			t.Errorf(
				"%s:%d: %q — no code path outside the migrations may emit "+
					"DELETE FROM units (docs/02-cognitive-core.md §1, CLAUDE.md "+
					"non-negotiable #6)",
				path, lineNum, strings.TrimSpace(line),
			)
		}

		scanned := scanGoTree(t, filepath.Join(repoRoot, "internal"), containsUnitsDeleteStatement, report)
		scanned += scanGoTree(t, filepath.Join(repoRoot, "cmd"), containsUnitsDeleteStatement, report)
		if scanned == 0 {
			t.Fatal("scanned zero .go files under internal/ and cmd/ — D10's guard: nothing to check yet")
		}
	})
}

// containsUnitsDeleteStatement reports whether line contains the exact
// (case-insensitive) statement "DELETE FROM units", rejecting a match whose
// next character would extend the identifier — "DELETE FROM units_fts" is a
// different table's DDL/DML entirely, not a violation of I03.
func containsUnitsDeleteStatement(line string) bool {
	const marker = "DELETE FROM UNITS"

	upper := strings.ToUpper(line)
	idx := strings.Index(upper, marker)
	if idx == -1 {
		return false
	}

	after := idx + len(marker)
	if after < len(upper) {
		c := upper[after]
		isIdentTail := c == '_' || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if isIdentTail {
			return false
		}
	}
	return true
}
