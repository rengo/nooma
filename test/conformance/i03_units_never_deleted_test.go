// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"os"
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
//
//     **ports.RelationRepo left this list on 2026-08-24, by owner ruling,
//     and the reason is a collision this sweep had been hiding.** I10
//     (docs/06-harness.md:250, docs/02-cognitive-core.md:331) requires
//     that rejecting a relation DELETES it, emitting relation_reject
//     first. m2c widened this sweep from ports.UnitRepo alone to every
//     repository interface, and the conflict went unnoticed for two
//     milestones because nothing had ever needed to delete a relation.
//     When m3d did, the sweep forbade the method I10 demands.
//
//     The alternative — naming the method something the prefix set does
//     not match — was rejected outright: it is exactly the "correct guard
//     entered from underneath" this repository keeps closing, and a check
//     that can be satisfied by a synonym is a check nobody should trust.
//
//     What survives is the claim I03 actually makes, which its own name
//     says: nothing is deleted from UNITS. Relations are deletable by
//     design and by an invariant of their own.
//     ports.Channel joined the sweep in the PR that declared it, and it is
//     the first member that is not a repository — so the list's own claim,
//     "every ports repository interface", either widens or the port stays
//     out. It widens. I03's subject is that nothing is deleted, and a
//     channel offering to delete a conversation would be the same failure
//     in a different table; keeping it out to protect the wording of a
//     variable name would be the wrong half to preserve.
//
//  2. Tree scan: no Go source file under internal/ or cmd/ issues a literal
//     DELETE FROM units statement (migrations are .sql files, embedded via
//     the go:embed directive, and are naturally outside this Go-source
//     scan — design D1).
//
// The tree scan also covers self_beliefs (m4e): a belief is retired, never
// removed, and the scan is the structural half of that — the reflection
// check above already forbids a removal verb on SelfModelRepo. The rule is
// the same identifier-tail rule, parameterised by table name.
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

	for _, table := range i03DeleteScannedTables {
		t.Run("tree scan for DELETE FROM "+table, func(t *testing.T) {
			repoRoot := repoRootFromCaller(t)
			report := func(path string, lineNum int, line string) {
				t.Errorf(
					"%s:%d: %q — no code path outside the migrations may emit "+
						"DELETE FROM %s (docs/02-cognitive-core.md §1, CLAUDE.md "+
						"non-negotiable #6)",
					path, lineNum, strings.TrimSpace(line), table,
				)
			}
			match := func(line string) bool { return containsDeleteStatementFrom(line, table) }

			scanned := scanGoTree(t, filepath.Join(repoRoot, "internal"), match, report)
			scanned += scanGoTree(t, filepath.Join(repoRoot, "cmd"), match, report)
			if scanned == 0 {
				t.Fatal("scanned zero .go files under internal/ and cmd/ — D10's guard: nothing to check yet")
			}
		})
	}
}

// i03DeleteScannedTables is every table whose removal the tree scan
// forbids: units (I03 proper) and self_beliefs (m4e: a belief is retired,
// never removed).
var i03DeleteScannedTables = []string{"units", "self_beliefs"}

// TestI03_ScansUnitsAndSelfBeliefs keeps the scan from quietly shrinking:
// dropping a table from i03DeleteScannedTables would leave every other I03
// test green, because they pass on a tree with no matching statement.
func TestI03_ScansUnitsAndSelfBeliefs(t *testing.T) {
	for _, want := range []string{"units", "self_beliefs"} {
		found := false
		for _, got := range i03DeleteScannedTables {
			found = found || got == want
		}
		if !found {
			t.Errorf("i03DeleteScannedTables = %v, missing %q — the tree scan no longer covers it", i03DeleteScannedTables, want)
		}
	}
}

// TestI03_DeleteMarkerMatchesTheTableNameExactly pins the scan's matcher
// (the probe rows of m4e gate G2): a DELETE FROM <table> statement matches,
// case-insensitively and at the end of a line; a longer identifier that
// merely starts with the table name (self_beliefs_x) and a different table
// do not.
func TestI03_DeleteMarkerMatchesTheTableNameExactly(t *testing.T) {
	cases := []struct {
		line  string
		table string
		want  bool
	}{
		{`const q = "DELETE FROM self_beliefs WHERE id = ?"`, "self_beliefs", true},
		{`const q = "delete from SELF_BELIEFS"`, "self_beliefs", true},
		{`const q = "DELETE FROM self_beliefs;"`, "self_beliefs", true},
		{"DELETE FROM self_beliefs", "self_beliefs", true},
		{`const q = "DELETE FROM self_beliefs_x"`, "self_beliefs", false},
		{`const q = "DELETE FROM self_beliefs2"`, "self_beliefs", false},
		{`const q = "DELETE FROM units WHERE id = ?"`, "self_beliefs", false},
		{`const q = "DELETE FROM self_beliefs"`, "units", false},
		{`const q = "DELETE FROM units WHERE id = ?"`, "units", true},
		{`const q = "DELETE FROM units_fts"`, "units", false},
	}
	for _, tc := range cases {
		if got := containsDeleteStatementFrom(tc.line, tc.table); got != tc.want {
			t.Errorf("containsDeleteStatementFrom(%q, %q) = %v, want %v", tc.line, tc.table, got, tc.want)
		}
	}
}

// TestI03_TreeScanFiresOnAProbeFile proves the scan itself, not only its
// matcher: over a tree holding one file that emits DELETE FROM self_beliefs
// and one that only names self_beliefs_x, it reports exactly the first.
func TestI03_TreeScanFiresOnAProbeFile(t *testing.T) {
	dir := t.TempDir()
	probes := map[string]string{
		"bad.go":    "package probe\n\nconst q = \"DELETE FROM self_beliefs WHERE id = ?\"\n",
		"silent.go": "package probe\n\nconst q = \"DELETE FROM self_beliefs_x\"\n",
	}
	for name, body := range probes {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write probe %s: %v", name, err)
		}
	}

	var reported []string
	scanned := scanGoTree(t, dir,
		func(line string) bool { return containsDeleteStatementFrom(line, "self_beliefs") },
		func(path string, _ int, _ string) { reported = append(reported, filepath.Base(path)) },
	)
	if scanned != 2 {
		t.Fatalf("scanned %d probe files, want 2", scanned)
	}
	if len(reported) != 1 || reported[0] != "bad.go" {
		t.Errorf("scan reported %v, want exactly [bad.go]", reported)
	}
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
	reflect.TypeOf((*ports.SelfModelRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.ConfigRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.StateRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.SignalRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.EmbeddingRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.TriggerRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.TimerRepo)(nil)).Elem(),
	reflect.TypeOf((*ports.Channel)(nil)).Elem(),
	// PendingQuestionRepo joins the sweep with no carve-out (m3e,
	// ADR-0027) — RelationRepo remains the sweep's one carve-out
	// (RelationRepo.Delete, 2026-08-24 owner ruling), and a pending
	// question is a state machine, never a removal.
	reflect.TypeOf((*ports.PendingQuestionRepo)(nil)).Elem(),
}

// containsDeleteStatementFrom reports whether line contains the exact
// (case-insensitive) statement "DELETE FROM <table>", rejecting a match
// whose next character would extend the identifier — "DELETE FROM units_fts"
// is a different table's DDL/DML entirely, not a violation of I03, and
// neither is "DELETE FROM self_beliefs_x".
func containsDeleteStatementFrom(line, table string) bool {
	marker := "DELETE FROM " + strings.ToUpper(table)

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
