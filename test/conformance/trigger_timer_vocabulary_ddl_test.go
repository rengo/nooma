// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// TestTriggerTimerVocabulariesMatchMigration0001Comments pins the three
// vocabularies internal/ports declares for triggers and timers to
// migration 0001's own column comments — the mechanism
// TestRelationCreatedByDDLMatchesAllCreatedBy already uses against 0001:37
// (relation_createdby_ddl_test.go) and TestStateSourceLiteralsMatchMigration0003Comment
// against 0003.
//
// These constants live in internal/ports, deliberately outside
// test/conformance/calibration_doc_test.go's reach (§13 covers
// internal/core symbols only) — ports.StateSourceConsolidation's own
// situation, design §3.2. So the SQL comment is their only other source of
// truth, and this test is what makes "only" mean "one", not "two that may
// drift".
//
// Order is part of the assertion: each AllX() must answer in the comment's
// own left-to-right order, and the length is checked independently so a
// silently dropped member fails even when the survivors are still in order.
func TestTriggerTimerVocabulariesMatchMigration0001Comments(t *testing.T) {
	sqlText := migrationSQLText(t)

	t.Run("triggers.status", func(t *testing.T) {
		want := columnCommentMembers(t, sqlText, "status            TEXT NOT NULL DEFAULT 'armed', -- ")

		got := make([]string, 0, len(ports.AllTriggerStatuses()))
		for _, s := range ports.AllTriggerStatuses() {
			got = append(got, string(s))
		}
		assertVocabularyMatches(t, "ports.AllTriggerStatuses()", got, want)
	})

	t.Run("triggers.kind", func(t *testing.T) {
		want := columnCommentMembers(t, sqlText, "kind              TEXT NOT NULL,               -- ")

		got := make([]string, 0, len(ports.AllTriggerKinds()))
		for _, k := range ports.AllTriggerKinds() {
			got = append(got, string(k))
		}
		assertVocabularyMatches(t, "ports.AllTriggerKinds()", got, want)
	})

	t.Run("timers.status", func(t *testing.T) {
		want := columnCommentMembers(t, sqlText, "status        TEXT NOT NULL DEFAULT 'pending', -- ")

		got := make([]string, 0, len(ports.AllTimerStatuses()))
		for _, s := range ports.AllTimerStatuses() {
			got = append(got, string(s))
		}
		assertVocabularyMatches(t, "ports.AllTimerStatuses()", got, want)
	})
}

// TestTriggerTimerVocabulariesAreFreshSlices proves each AllX is a
// function returning a fresh slice, never a shared backing array a caller
// can mutate — ports.AllDecisionActions's own reasoning
// (decisionlog.go:98-106): a completeness check run from outside this
// package must not be defeatable by an importer that scribbled on an
// earlier call's result.
func TestTriggerTimerVocabulariesAreFreshSlices(t *testing.T) {
	t.Run("triggers.status", func(t *testing.T) {
		first := ports.AllTriggerStatuses()
		if len(first) == 0 {
			t.Fatal("ports.AllTriggerStatuses() returned zero members — nothing to check yet")
		}
		first[0] = "scribbled"
		if second := ports.AllTriggerStatuses(); second[0] == "scribbled" {
			t.Fatal("ports.AllTriggerStatuses() shares its backing array across calls")
		}
	})

	t.Run("triggers.kind", func(t *testing.T) {
		first := ports.AllTriggerKinds()
		if len(first) == 0 {
			t.Fatal("ports.AllTriggerKinds() returned zero members — nothing to check yet")
		}
		first[0] = "scribbled"
		if second := ports.AllTriggerKinds(); second[0] == "scribbled" {
			t.Fatal("ports.AllTriggerKinds() shares its backing array across calls")
		}
	})

	t.Run("timers.status", func(t *testing.T) {
		first := ports.AllTimerStatuses()
		if len(first) == 0 {
			t.Fatal("ports.AllTimerStatuses() returned zero members — nothing to check yet")
		}
		first[0] = "scribbled"
		if second := ports.AllTimerStatuses(); second[0] == "scribbled" {
			t.Fatal("ports.AllTimerStatuses() shares its backing array across calls")
		}
	})
}

// columnCommentMembers returns the pipe-separated vocabulary written in the
// column comment that follows marker in sqlText. marker carries the whole
// column definition up to and including its "-- ", so it is unique in the
// migration corpus and a reformatted DDL line fails loudly here rather than
// silently matching a different column.
func columnCommentMembers(t *testing.T, sqlText, marker string) []string {
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

	return strings.Split(strings.TrimSpace(rest[:end]), "|")
}

// assertVocabularyMatches fails unless got names exactly want, in order.
func assertVocabularyMatches(t *testing.T, name string, got, want []string) {
	t.Helper()

	if len(got) == 0 {
		t.Fatalf("%s returned zero members — nothing to check yet", name)
	}
	if len(got) != len(want) {
		t.Fatalf("migration 0001's column comment lists %d members %v, %s lists %d %v",
			len(want), want, name, len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: migration comment says %q, %s says %q", i, want[i], name, got[i])
		}
	}
}
