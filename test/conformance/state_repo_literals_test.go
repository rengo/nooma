// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// TestStateSourceLiteralsMatchMigration0003Comment proves spec R2.5's
// sibling for StateRepo's own literals (design §4.4): ports.StateSourceUser
// and ports.StateSourceConsolidation sit outside
// test/conformance/calibration_doc_test.go's reach (§13 covers
// internal/core symbols only), so they are pinned instead to migration
// 0003's own column comment vocabulary, read off disk via the existing
// migrationSQLText helper — the shape
// TestRelationCreatedByDDLMatchesAllCreatedBy (relation_createdby_ddl_test.go)
// already uses against 0001:37.
//
// Not a missing-symbol TDD red step (m2a C9): ports.StateSourceUser and
// ports.StateSourceConsolidation already exist (PR 3) and migration 0003
// already landed (this PR's own task 4.1) by the time this test runs — a
// genuine value-pin test proving the two Go constants match the SQL
// comment's vocabulary exactly and in order, not a red-for-a-missing-symbol
// step.
func TestStateSourceLiteralsMatchMigration0003Comment(t *testing.T) {
	sqlText := migrationSQLText(t)

	const marker = "ADD COLUMN source TEXT NOT NULL DEFAULT 'user';  -- "
	idx := strings.Index(sqlText, marker)
	if idx == -1 {
		t.Fatal("current_state.source column comment not found in the embedded migrations — nothing to check yet")
	}
	rest := sqlText[idx+len(marker):]
	end := strings.IndexByte(rest, '\n')
	if end == -1 {
		t.Fatal("current_state.source column comment has no terminating newline")
	}
	comment := strings.TrimSpace(rest[:end])

	docMembers := strings.Split(comment, "|")

	// Deliberately the wrong order (discriminating-fixture mutation, PR 3's
	// own precedent) — migration 0003's comment lists "user|consolidation",
	// so this must fail at position 0 for the right reason before task
	// 4.7/4.8's GREEN commit swaps it back.
	wantOrder := []string{ports.StateSourceConsolidation, ports.StateSourceUser}
	if len(docMembers) != len(wantOrder) {
		t.Fatalf("migration 0003's current_state.source comment lists %d members %v, want %d %v",
			len(docMembers), docMembers, len(wantOrder), wantOrder)
	}

	for i, want := range docMembers {
		got := wantOrder[i]
		if got != want {
			t.Errorf("position %d: migration comment says %q, ports constant says %q", i, want, got)
		}
	}
}

// TestMoodLoadedMatchesDoc02Prose proves ports.MoodLoaded's own pin (design
// §4.4). mood is free text with no SQL-enforced vocabulary (design §3.2 —
// "it does not close mood's vocabulary"; migration 0001's mood column
// carries no comment, unlike source or created_by), so MoodLoaded cannot be
// pinned to a migration DDL comment the way StateSourceUser and
// StateSourceConsolidation are above. It is pinned instead to doc 02 §7's
// own prose — the same doc-vs-code pin shape
// TestI11_PhaseOrderMatchesDoc02ArrowLine (i11_consolidation_phase_order_test.go)
// already uses for a value doc 02 states rather than a SQL schema enforces.
func TestMoodLoadedMatchesDoc02Prose(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	docPath := filepath.Join(repoRoot, "docs", "02-cognitive-core.md")

	content, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}

	const marker = "mood = 'loaded'"
	if !strings.Contains(string(content), marker) {
		t.Fatalf("docs/02-cognitive-core.md does not contain %q — nothing to check", marker)
	}

	if ports.MoodLoaded != "loaded" {
		t.Errorf("ports.MoodLoaded = %q, want %q (docs/02-cognitive-core.md §7's own prose)", ports.MoodLoaded, "loaded")
	}
}
