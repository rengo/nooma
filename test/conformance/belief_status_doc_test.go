package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/core/selfmodel"
)

// TestBeliefStatusDocMatchesAllStatuses is m4e design gate G3: the
// self_beliefs.status vocabulary has one truth in Go (selfmodel.AllStatuses)
// and one in docs/03-data-model.md, and this test pins them together — a
// status added on one side without the other fails loudly.
//
// It reads doc 03, not migration 0001, on purpose: migration 0001 carries no
// vocabulary comment on self_beliefs.status, and a published migration is
// never modified. Doc 03's DDL comparator strips comments, so the comment
// this test reads cannot disturb the schema golden.
func TestBeliefStatusDocMatchesAllStatuses(t *testing.T) {
	text, err := os.ReadFile(filepath.Join(repoRootFromCaller(t), "docs", "03-data-model.md"))
	if err != nil {
		t.Fatalf("read docs/03-data-model.md: %v", err)
	}

	comment := columnComment(t, string(text), "CREATE TABLE self_beliefs (", "status")
	docMembers := strings.Split(comment, "|")

	allStatuses := selfmodel.AllStatuses()
	if len(allStatuses) == 0 {
		t.Fatal("selfmodel.AllStatuses() returned zero statuses — nothing to check yet")
	}
	if len(docMembers) != len(allStatuses) {
		t.Fatalf("doc 03's self_beliefs.status comment lists %d members %v, selfmodel.AllStatuses() lists %d %v",
			len(docMembers), docMembers, len(allStatuses), allStatuses)
	}
	for i, want := range docMembers {
		if got := string(allStatuses[i]); got != want {
			t.Errorf("position %d: doc 03 says %q, selfmodel.AllStatuses() says %q", i, want, got)
		}
	}
}

// columnComment returns the trailing "-- ..." comment of the named column
// inside the CREATE TABLE statement that starts with tableOpening, failing
// the test when the table, the column or its comment is absent.
func columnComment(t *testing.T, doc, tableOpening, column string) string {
	t.Helper()

	start := strings.Index(doc, tableOpening)
	if start == -1 {
		t.Fatalf("%q not found in the document — nothing to check yet", tableOpening)
	}
	block := doc[start:]
	if end := strings.Index(block, "\n);"); end != -1 {
		block = block[:end]
	}

	for _, line := range strings.Split(block, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != column {
			continue
		}
		_, comment, ok := strings.Cut(line, "-- ")
		if !ok {
			t.Fatalf("column %q has no trailing comment in %q: %q", column, tableOpening, line)
		}
		return strings.TrimSpace(comment)
	}
	t.Fatalf("column %q not found in %q", column, tableOpening)
	return ""
}
