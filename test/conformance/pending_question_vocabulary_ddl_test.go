// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// TestPendingQuestionVocabulariesMatchMigration0004Comments pins the two
// vocabularies internal/ports declares for pending_questions to migration
// 0004's own column comments — TestTriggerTimerVocabulariesMatchMigration0001Comments'
// mechanism, applied to the table m3e added.
//
// These constants live in internal/ports, outside
// calibration_doc_test.go's reach (§13 covers internal/core symbols only),
// and migration 0004 carries NO CHECK constraint on either column — no
// table in this schema does. So the SQL comment is their only other source
// of truth, and this test is what makes "only" mean "one" rather than "two
// that may drift".
//
// Order is part of the assertion, and the length is checked independently
// so a silently dropped member fails even when the survivors are still in
// order.
func TestPendingQuestionVocabulariesMatchMigration0004Comments(t *testing.T) {
	sqlText := migrationSQLText(t)

	t.Run("pending_questions.kind", func(t *testing.T) {
		want := columnCommentVocabulary(t, sqlText, "kind         TEXT NOT NULL,   -- ")

		got := make([]string, 0, len(ports.AllQuestionKinds()))
		for _, k := range ports.AllQuestionKinds() {
			got = append(got, string(k))
		}
		assertVocabularyMatches(t, "ports.AllQuestionKinds()", got, want)
	})

	t.Run("pending_questions.resolution", func(t *testing.T) {
		want := columnCommentVocabulary(t, sqlText, "resolution   TEXT             -- ")

		got := make([]string, 0, len(ports.AllQuestionResolutions()))
		for _, r := range ports.AllQuestionResolutions() {
			got = append(got, string(r))
		}
		assertVocabularyMatches(t, "ports.AllQuestionResolutions()", got, want)
	})
}

// TestPendingQuestionVocabulariesAreFreshSlices proves each AllX returns a
// fresh slice rather than a shared backing array — ports.AllDecisionActions'
// own reasoning: a completeness check run from outside the package must not
// be defeatable by an importer that scribbled on an earlier call's result.
func TestPendingQuestionVocabulariesAreFreshSlices(t *testing.T) {
	t.Run("kind", func(t *testing.T) {
		first := ports.AllQuestionKinds()
		if len(first) == 0 {
			t.Fatal("ports.AllQuestionKinds() returned zero members — nothing to check yet")
		}
		first[0] = "scribbled"
		if second := ports.AllQuestionKinds(); second[0] == "scribbled" {
			t.Fatal("ports.AllQuestionKinds() shares its backing array across calls")
		}
	})

	t.Run("resolution", func(t *testing.T) {
		first := ports.AllQuestionResolutions()
		if len(first) == 0 {
			t.Fatal("ports.AllQuestionResolutions() returned zero members — nothing to check yet")
		}
		first[0] = "scribbled"
		if second := ports.AllQuestionResolutions(); second[0] == "scribbled" {
			t.Fatal("ports.AllQuestionResolutions() shares its backing array across calls")
		}
	})
}

// columnCommentVocabulary is columnCommentMembers for a comment that also
// carries prose after its vocabulary.
//
// Migration 0001's comments are bare pipe-separated lists, so
// columnCommentMembers splits them and stops. 0004's are not:
//
//	kind         TEXT NOT NULL,   -- relation (the only member m3e writes)
//	resolution   TEXT             -- confirmed|rejected|expired; NULL while open
//
// A published migration is never modified (CLAUDE.md), so reformatting
// 0004 into 0001's shape is not available and this reads what is actually
// there: the vocabulary runs to the first ";" or " (", and whatever follows
// is a note to a human. Two delimiters and no more — a comment that needs a
// third has drifted far enough from a vocabulary that it should fail here
// rather than be parsed into agreement.
func columnCommentVocabulary(t *testing.T, sqlText, marker string) []string {
	t.Helper()

	idx := strings.Index(sqlText, marker)
	if idx == -1 {
		t.Fatalf("column comment %q not found in the embedded migrations — nothing to check yet", marker)
	}
	rest := sqlText[idx+len(marker):]
	end := strings.IndexByte(rest, '\n')
	if end == -1 {
		t.Fatalf("column comment %q has no terminating newline", marker)
	}

	comment := rest[:end]
	if cut := strings.IndexByte(comment, ';'); cut != -1 {
		comment = comment[:cut]
	}
	if cut := strings.Index(comment, " ("); cut != -1 {
		comment = comment[:cut]
	}

	members := strings.Split(strings.TrimSpace(comment), "|")
	for i := range members {
		members[i] = strings.TrimSpace(members[i])
	}
	return members
}
