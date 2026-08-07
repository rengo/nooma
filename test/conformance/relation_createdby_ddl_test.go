// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/core/relation"
)

// TestRelationCreatedByDDLMatchesAllCreatedBy proves R4.7's L2 half:
// relation.AllCreatedBy() has one source of truth in Go and one in SQL, and
// this test pins them together — unit_status_ddl_test.go's own mechanism
// (TestUnitStatusDDLMatchesAllStatuses), reading migration 0001's
// relations.created_by column comment
// ("system|consolidation|user", 0001_core_tables.sql:37) via
// migrationSQLText (i13_learning_signal_test.go:24) rather than a second
// hardcoded copy of the vocabulary.
//
// Not a missing-symbol TDD red step: relation.AllCreatedBy already exists
// (this PR's own tasks 4.1-4.2) and already matches — this test is the
// permanent guard against a future drift between the two.
func TestRelationCreatedByDDLMatchesAllCreatedBy(t *testing.T) {
	sqlText := migrationSQLText(t)

	const marker = "created_by    TEXT NOT NULL DEFAULT 'system',  -- "
	idx := strings.Index(sqlText, marker)
	if idx == -1 {
		t.Fatal("relations.created_by column comment not found in the embedded migrations — nothing to check yet")
	}
	rest := sqlText[idx+len(marker):]
	end := strings.IndexByte(rest, '\n')
	if end == -1 {
		t.Fatal("relations.created_by column comment has no terminating newline")
	}
	comment := strings.TrimSpace(rest[:end])

	docMembers := strings.Split(comment, "|")

	allCreatedBy := relation.AllCreatedBy()
	if len(allCreatedBy) == 0 {
		t.Fatal("relation.AllCreatedBy() returned zero members — nothing to check yet")
	}

	if len(docMembers) != len(allCreatedBy) {
		t.Fatalf("migration 0001's relations.created_by comment lists %d members %v, relation.AllCreatedBy() lists %d %v",
			len(docMembers), docMembers, len(allCreatedBy), allCreatedBy)
	}

	for i, want := range docMembers {
		got := string(allCreatedBy[i])
		if got != want {
			t.Errorf("position %d: migration comment says %q, relation.AllCreatedBy() says %q", i, want, got)
		}
	}
}
