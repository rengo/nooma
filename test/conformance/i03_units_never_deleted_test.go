// Package conformance — see test/conformance/doc.go for the package contract.
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
// ports.UnitRepo (internal/ports) now exists — promoted into the untagged
// L2 suite by the same PR that added it (spec R7.2, design D8), per the
// ordering internal/ports/doc.go used to anchor before this test's
// promotion removed that paragraph. tree_scan_test.go's build tag is
// unaffected here — PR 2a already untagged it (spec R7.1's MUST NOT
// against re-touching it).
//
// Two independent checks (design §8.4/D5):
//
//  1. Reflection over the interface: no method name begins with any of
//     deniedMethodPrefixes. That set is {Delete, Remove, Purge, Drop,
//     Destroy} — strengthened by this PR from {Delete} alone (design D5's
//     own stated gap: a Delete-only check would let Purge, Remove or Drop
//     slip past it). Strengthening a conformance test is allowed;
//     weakening one is what docs/06-harness.md §4 forbids.
//  2. Tree scan: no Go source file under internal/ or cmd/ issues a literal
//     DELETE FROM units statement (migrations are .sql files, embedded via
//     the go:embed directive, and are naturally outside this Go-source
//     scan — design D1).
//
// D10's non-empty-corpus guard applies to both: a zero-method interface or a
// zero-file scan fails loudly instead of passing vacuously.
func TestI03_UnitsAreNeverDeleted(t *testing.T) {
	t.Run("repo declares no removal method", func(t *testing.T) {
		repoType := reflect.TypeOf((*ports.UnitRepo)(nil)).Elem()
		if repoType.Kind() != reflect.Interface {
			t.Fatalf("ports.UnitRepo has kind %s, want interface", repoType.Kind())
		}
		if repoType.NumMethod() == 0 {
			t.Fatal("ports.UnitRepo declares zero methods — D10's guard: nothing to check yet")
		}
		for i := 0; i < repoType.NumMethod(); i++ {
			name := repoType.Method(i).Name
			for _, prefix := range deniedMethodPrefixes {
				if strings.HasPrefix(name, prefix) {
					t.Errorf(
						"ports.UnitRepo declares %s — nothing deletes a unit "+
							"(docs/02-cognitive-core.md §1, CLAUDE.md non-negotiable #6: "+
							"archiving is a state transition, not a removal)",
						name,
					)
				}
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

// deniedMethodPrefixes is I03's strengthened prefix set (design D5): a
// ports.UnitRepo method name beginning with any of these would give
// deletion a verb to name it, defeating I03's structural guarantee. Widened
// by this PR from {Delete} alone — a strengthening, per
// docs/06-harness.md §4.
var deniedMethodPrefixes = []string{"Delete", "Remove", "Purge", "Drop", "Destroy"}

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
