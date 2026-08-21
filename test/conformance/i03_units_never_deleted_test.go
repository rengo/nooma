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
//  1. Reflection over every ports repository interface — UnitRepo,
//     RelationRepo, SelfModelRepo, ConfigRepo, StateRepo, SignalRepo and
//     EmbeddingRepo (m2c design §4.6, spec R2.7) — no method name begins
//     with any of deniedMethodPrefixes. That set is {Delete, Remove,
//     Purge, Drop, Destroy} — strengthened by an earlier PR from {Delete}
//     alone (design D5's own stated gap: a Delete-only check would let
//     Purge, Remove or Drop slip past it). Strengthening a conformance
//     test is allowed; weakening one is what docs/06-harness.md §4
//     forbids. The sweep itself was widened from ports.UnitRepo alone to
//     the five m2c introduces by m2c PR 3, then to all seven ports
//     repository interfaces in this PR — closing the gap between what
//     RelationRepo's doc comment claims ("every ports repository
//     interface") and what this test actually checked: SignalRepo (I13:
//     a learning signal outlives the deletion of its target) and
//     EmbeddingRepo (embeddings hang off units, which are never deleted)
//     belong to the same invariant and had no removal verb to begin
//     with — widening the sweep makes the doc comment's claim true
//     instead of narrowing the claim to match a partial sweep.
//     TriggerRepo and TimerRepo joined the sweep in the PR that declared
//     them, for that same reason: the list's claim is "every ports
//     repository interface", and a port left out of it would make the
//     claim false the moment it landed. Their own contract suite asserts
//     the identical prefix set over the identical method sets
//     (test/support/repocontract/triggerrepo.go) — redundant on purpose,
//     since that suite runs once per implementation while this one runs
//     over the declaration itself.
//  2. Tree scan: no Go source file under internal/ or cmd/ issues a literal
//     DELETE FROM units statement (migrations are .sql files, embedded via
//     the go:embed directive, and are naturally outside this Go-source
//     scan — design D1).
//
// D10's non-empty-corpus guard applies to both: a zero-method interface or a
// zero-file scan fails loudly instead of passing vacuously.
func TestI03_UnitsAreNeverDeleted(t *testing.T) {
	t.Run("repo declares no removal method", func(t *testing.T) {
		for _, repoType := range sweptPortsRepoTypes {
			if repoType.Kind() != reflect.Interface {
				t.Fatalf("%s has kind %s, want interface", repoType, repoType.Kind())
			}
			if repoType.NumMethod() == 0 {
				t.Fatalf("%s declares zero methods — D10's guard: nothing to check yet", repoType)
			}
			for i := 0; i < repoType.NumMethod(); i++ {
				name := repoType.Method(i).Name
				for _, prefix := range deniedMethodPrefixes {
					if strings.HasPrefix(name, prefix) {
						t.Errorf(
							"%s declares %s — nothing deletes a unit "+
								"(docs/02-cognitive-core.md §1, CLAUDE.md non-negotiable #6: "+
								"archiving is a state transition, not a removal)",
							repoType, name,
						)
					}
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
// ports repository method name beginning with any of these would give
// deletion a verb to name it, defeating I03's structural guarantee. Widened
// by an earlier PR from {Delete} alone — a strengthening, per
// docs/06-harness.md §4.
var deniedMethodPrefixes = []string{"Delete", "Remove", "Purge", "Drop", "Destroy"}

// sweptPortsRepoTypes is every ports repository interface I03's reflection
// check sweeps — m2c design §4.6, spec R2.7: all seven that
// internal/ports declares — UnitRepo, RelationRepo, SelfModelRepo,
// ConfigRepo, StateRepo, SignalRepo and EmbeddingRepo. The first five were
// swept starting with m2c PR 3 (the PR that added the last of the three
// new interfaces introduced there); SignalRepo and EmbeddingRepo were
// added to the sweep in this PR, closing the gap between
// RelationRepo's doc comment's "every ports repository interface" claim
// and what the sweep actually checked. A future ports interface needs a
// line added here — this list is that claim, held to what actually
// exists, not what the package doc comment alone says.
var sweptPortsRepoTypes = []reflect.Type{
	reflect.TypeOf((*ports.UnitRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.RelationRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.SelfModelRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.ConfigRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.StateRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.SignalRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.EmbeddingRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.TriggerRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.TimerRepo)(nil)).Elem(),
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
