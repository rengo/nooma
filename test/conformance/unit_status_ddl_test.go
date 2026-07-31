package conformance

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestUnitStatusDDLMatchesAllStatuses proves design D1's third bullet: the
// unit.Status vocabulary has one source of truth in Go and one in SQL, and
// this test pins them together. It reads migration 0001's units.status
// column comment (the same "pool|archived|superseded|incomplete" domain
// documented in docs/03-data-model.md) via migrationSQLText
// (i13_learning_signal_test.go:24-57) and asserts it names exactly
// unit.AllStatuses()'s members, in the same order the DDL comment lists
// them — a status added to one side without the other must fail loudly.
func TestUnitStatusDDLMatchesAllStatuses(t *testing.T) {
	sqlText := migrationSQLText(t)

	const marker = "status             TEXT NOT NULL DEFAULT 'pool',  -- "
	idx := strings.Index(sqlText, marker)
	if idx == -1 {
		t.Fatal("units.status column comment not found in the embedded migrations — nothing to check yet")
	}
	rest := sqlText[idx+len(marker):]
	end := strings.IndexByte(rest, '\n')
	if end == -1 {
		t.Fatal("units.status column comment has no terminating newline")
	}
	comment := strings.TrimSpace(rest[:end])

	docMembers := strings.Split(comment, "|")

	allStatuses := unit.AllStatuses()
	if len(allStatuses) == 0 {
		t.Fatal("unit.AllStatuses() returned zero statuses — nothing to check yet")
	}

	if len(docMembers) != len(allStatuses) {
		t.Fatalf("migration 0001's units.status comment lists %d members %v, unit.AllStatuses() lists %d %v",
			len(docMembers), docMembers, len(allStatuses), allStatuses)
	}

	for i, want := range docMembers {
		got := string(allStatuses[i])
		if got != want {
			t.Errorf("position %d: migration comment says %q, unit.AllStatuses() says %q", i, want, got)
		}
	}
}
