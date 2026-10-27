//go:build integration

package sqlite

import (
	"database/sql"
	"encoding/json"
	"testing"
)

const backfillMigration = "0013_relation_signal_types.sql"

// seedGraphReject writes what a graph delete left before migration 0013: the
// relation.rejected row carrying the relation as it was, and the untyped
// relation_reject signal emitted before the delete (I10).
func seedGraphReject(exec func(string, ...any), decisionID, signalID, relationID, relType, createdBy string, confidence float64) {
	exec(`INSERT INTO decision_log (id, action, rationale, context, occurred_at)
	      VALUES (?, 'relation.rejected', 'r', json_object('relation_id', ?, 'type', ?,
	        'previous', json_object('type', ?, 'strength', 0.4, 'confidence', ?, 'created_by', ?),
	        'next', json_object('type', NULL)), '2026-10-25T09:00:00Z')`,
		decisionID, relationID, relType, relType, confidence, createdBy)
	seedUntypedSignal(exec, signalID, "relation_reject", relationID)
}

func seedUntypedSignal(exec func(string, ...any), id, typ, relationID string) {
	exec(`INSERT INTO learning_signals (id, signal_type, valence, target_kind, target_id, context, occurred_at)
	      VALUES (?, ?, 'negative', 'relation', ?, '{}', '2026-10-25T09:00:00Z')`, id, typ, relationID)
}

type signalRow struct {
	relationType sql.NullString
	context      map[string]any
}

func signalByID(t *testing.T, db *sql.DB, id string) signalRow {
	t.Helper()
	var r signalRow
	var raw string
	if err := db.QueryRow(`SELECT relation_type, context FROM learning_signals WHERE id = ?`, id).Scan(&r.relationType, &raw); err != nil {
		t.Fatalf("read signal %s: %v", id, err)
	}
	if err := json.Unmarshal([]byte(raw), &r.context); err != nil {
		t.Fatalf("signal %s context %q: %v", id, raw, err)
	}
	return r
}

func countSignals(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM learning_signals`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestBackfillTypesAGraphDeleteFromItsDecisionRow: the client's answer of
// 2026-10-27 — learn from past graph deletions. The relation is gone, and the
// relation.rejected row is the one place that still names its type, maker and
// confidence; the existing signal gains them and no signal is added.
func TestBackfillTypesAGraphDeleteFromItsDecisionRow(t *testing.T) {
	db := backfilledBefore(t, backfillMigration, func(exec func(string, ...any)) {
		seedGraphReject(exec, "d-1", "s-1", "rel-1", "same_date", "consolidation", 0.82)
		seedGraphReject(exec, "d-2", "s-2", "rel-2", "same_topic", "system", 0.41)
	})
	for id, want := range map[string]struct {
		typ, by string
		conf    float64
		from    string
	}{"s-1": {"same_date", "consolidation", 0.82, "d-1"}, "s-2": {"same_topic", "system", 0.41, "d-2"}} {
		got := signalByID(t, db, id)
		if !got.relationType.Valid || got.relationType.String != want.typ {
			t.Errorf("%s: relation_type = %+v, want %s", id, got.relationType, want.typ)
		}
		if got.context["created_by"] != want.by || got.context["confidence"] != want.conf || got.context["backfilled_from"] != want.from {
			t.Errorf("%s: context = %v, want created_by %s, confidence %v and the row it came from, %s", id, got.context, want.by, want.conf, want.from)
		}
	}
	if n := countSignals(t, db); n != 2 {
		t.Errorf("%d signals after the backfill, want the same 2: it types, it never adds", n)
	}
}

// TestBackfillLeavesAChatRejectUntyped: a reject answered in chat left a
// capture.relation_checkin.resolved row naming no type, and its relation is
// gone. Nothing can recover the type, so the signal stays untyped and the
// learner does not count it (doc 02 §9).
func TestBackfillLeavesAChatRejectUntyped(t *testing.T) {
	db := backfilledBefore(t, backfillMigration, func(exec func(string, ...any)) {
		exec(`INSERT INTO decision_log (id, action, rationale, context, occurred_at)
		      VALUES ('d-chat', 'capture.relation_checkin.resolved', 'r',
		        json_object('question_id', 'q', 'relation_id', 'rel-chat', 'resolution', 'rejected'), '2026-10-25T09:00:00Z')`)
		seedUntypedSignal(exec, "s-chat", "relation_reject", "rel-chat")
	})
	got := signalByID(t, db, "s-chat")
	if got.relationType.Valid || len(got.context) != 0 {
		t.Errorf("a chat reject was given %+v %v: nothing records its type", got.relationType, got.context)
	}
}

// TestBackfillTypesAConfirmFromItsLiveRelation: a confirmed relation still
// exists, so its row names the type and maker; the confidence the user judged
// is not recoverable (the confirm raised it) and is left out, not guessed.
func TestBackfillTypesAConfirmFromItsLiveRelation(t *testing.T) {
	db := backfilledBefore(t, backfillMigration, func(exec func(string, ...any)) {
		seedLegacyUnit(exec, "a")
		seedLegacyUnit(exec, "b")
		exec(`INSERT INTO relations (id, from_unit_id, to_unit_id, type, strength, confidence, created_by, created_at)
		      VALUES ('rel-ok', 'a', 'b', 'same_topic', 0.5, 0.5, 'consolidation', '2026-10-20T09:00:00Z')`)
		exec(`INSERT INTO learning_signals (id, signal_type, valence, target_kind, target_id, context, occurred_at)
		      VALUES ('s-ok', 'relation_confirm', 'positive', 'relation', 'rel-ok', '{}', '2026-10-25T09:00:00Z')`)
		seedUntypedSignal(exec, "s-gone", "relation_confirm", "rel-never")
	})
	got := signalByID(t, db, "s-ok")
	if got.relationType.String != "same_topic" || got.context["created_by"] != "consolidation" || got.context["backfilled_from"] != "relations" {
		t.Errorf("confirm signal = %+v %v, want same_topic by consolidation, backfilled from relations", got.relationType, got.context)
	}
	if _, has := got.context["confidence"]; has {
		t.Errorf("confirm signal context %v carries a confidence nothing recorded", got.context)
	}
	if gone := signalByID(t, db, "s-gone"); gone.relationType.Valid {
		t.Errorf("a confirm whose relation is gone was typed %+v", gone.relationType)
	}
}

// TestBackfillKeepsATypedSignal: a signal that already carries its type is
// left as it is, so the backfill touches only what predates it.
func TestBackfillKeepsATypedSignal(t *testing.T) {
	db := backfilledBefore(t, backfillMigration, func(exec func(string, ...any)) {
		seedGraphReject(exec, "d-1", "s-1", "rel-1", "same_date", "consolidation", 0.82)
		exec(`UPDATE learning_signals SET relation_type = 'kept', context = '{"created_by":"system"}' WHERE id = 's-1'`)
	})
	got := signalByID(t, db, "s-1")
	if got.relationType.String != "kept" || got.context["created_by"] != "system" {
		t.Errorf("a typed signal was rewritten to %+v %v", got.relationType, got.context)
	}
}
