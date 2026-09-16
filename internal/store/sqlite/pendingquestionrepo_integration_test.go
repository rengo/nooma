//go:build integration

package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// pendingQuestionFixtureTime is a fixed, whole-second UTC instant — this
// suite does not exercise clock behaviour, so a literal keeps every
// fixture identical.
var pendingQuestionFixtureTime = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

// pendingQuestionHarness adapts the real repo to
// repocontract.PendingQuestionHarness, inserting real units and a real
// relations row directly — the fixture must not depend on
// ports.RelationRepo's own behaviour to set up this port's (the same
// reasoning relationHarness.EnsureUnit already applies).
type pendingQuestionHarness struct {
	*PendingQuestionRepo
	v *Vault
}

func (h pendingQuestionHarness) EnsureRelation(t *testing.T, relationID, relType, fromUnitID, toUnitID, fromContent, toContent string) {
	t.Helper()
	ctx := context.Background()

	for _, u := range []struct{ id, content string }{{fromUnitID, fromContent}, {toUnitID, toContent}} {
		_, err := h.v.db.ExecContext(ctx, `
INSERT INTO units (id, type, content, status, weight, weight_decay_rate, last_touched_at, source, created_at, updated_at)
VALUES (?, 'task', ?, 'pool', 1.0, 0.01, ?, 'chat', ?, ?)
ON CONFLICT(id) DO NOTHING`,
			u.id, u.content,
			pendingQuestionFixtureTime.Format(unitTimeLayout),
			pendingQuestionFixtureTime.Format(unitTimeLayout),
			pendingQuestionFixtureTime.Format(unitTimeLayout))
		if err != nil {
			t.Fatalf("seeding unit %s: %v", u.id, err)
		}
	}

	_, err := h.v.db.ExecContext(ctx, `
INSERT INTO relations (id, from_unit_id, to_unit_id, type, strength, confidence, created_by, created_at)
VALUES (?, ?, ?, ?, 0.5, 0.4, 'consolidation', ?)
ON CONFLICT(id) DO NOTHING`,
		relationID, fromUnitID, toUnitID, relType, pendingQuestionFixtureTime.Format(unitTimeLayout))
	if err != nil {
		t.Fatalf("seeding relation %s: %v", relationID, err)
	}
}

func (h pendingQuestionHarness) ForgetRelation(t *testing.T, relationID string) {
	t.Helper()
	if _, err := h.v.db.ExecContext(context.Background(), `DELETE FROM relations WHERE id = ?`, relationID); err != nil {
		t.Fatalf("deleting relation %s: %v", relationID, err)
	}
}

// TestPendingQuestionRepo_Contract runs the same repocontract.RunPendingQuestionRepo
// suite the in-memory fake answers at L2, now against a real temporary
// SQLite vault at L3 — design D6's "answered twice" standing rule.
func TestPendingQuestionRepo_Contract(t *testing.T) {
	repocontract.RunPendingQuestionRepo(t, func(t *testing.T) repocontract.PendingQuestionHarness {
		v := openTestVault(t)
		return pendingQuestionHarness{PendingQuestionRepo: NewPendingQuestionRepo(v), v: v}
	})
}

// TestPendingQuestionRepo_CreateRejectsUnknownRelationAndInsertsNothing is
// N1's own dedicated L3 case: Create against an unknown relation_id returns
// ErrRelationNotFound (proven above, over the fake and the real store
// identically) AND the raw pending_questions table stays empty — asserted
// here directly against SQL, which is the only place N1 (rel.ID is not
// necessarily the stored relation's id, on Upsert's own conflict contract)
// can actually fail.
func TestPendingQuestionRepo_CreateRejectsUnknownRelationAndInsertsNothing(t *testing.T) {
	v := openTestVault(t)
	repo := NewPendingQuestionRepo(v)
	ctx := context.Background()

	err := repo.Create(ctx, ports.PendingQuestion{
		ID: "q-orphan", Kind: ports.QuestionKindRelation, RelationID: "no-such-relation",
		CreatedAt: pendingQuestionFixtureTime,
	})
	if err == nil {
		t.Fatal("Create(unknown relation_id) returned nil error, want ErrRelationNotFound")
	}

	var count int
	if err := v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pending_questions`).Scan(&count); err != nil {
		t.Fatalf("counting pending_questions: %v", err)
	}
	if count != 0 {
		t.Fatalf("pending_questions holds %d row(s) after a refused Create, want 0 — the WHERE EXISTS guard did not stop the insert", count)
	}
}

// TestPendingQuestionRepo_QuestionSurvivesItsRelationBeingDeleted is the
// test that fails if a future migration adds a CASCADE — ADR-0027's own
// stated reason for refusing every foreign key behaviour on relation_id.
// It deletes the relation with a raw DELETE (RejectRelation's own shape,
// I10), never through PendingQuestionRepo, and confirms the question row
// survives, resolved or not, with relation_id still readable — the honest
// historical record of a relation that was rejected.
func TestPendingQuestionRepo_QuestionSurvivesItsRelationBeingDeleted(t *testing.T) {
	v := openTestVault(t)
	h := pendingQuestionHarness{PendingQuestionRepo: NewPendingQuestionRepo(v), v: v}
	ctx := context.Background()

	h.EnsureRelation(t, "rel-doomed", "same_topic", "unit-a", "unit-b", "a", "b")
	if err := h.Create(ctx, ports.PendingQuestion{
		ID: "q-1", Kind: ports.QuestionKindRelation, RelationID: "rel-doomed", CreatedAt: pendingQuestionFixtureTime,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := v.db.ExecContext(ctx, `DELETE FROM relations WHERE id = ?`, "rel-doomed"); err != nil {
		t.Fatalf("deleting relation rel-doomed: %v", err)
	}

	var (
		id, relationID string
		resolvedAt     any
	)
	row := v.db.QueryRowContext(ctx, `SELECT id, relation_id, resolved_at FROM pending_questions WHERE id = ?`, "q-1")
	if err := row.Scan(&id, &relationID, &resolvedAt); err != nil {
		t.Fatalf("the pending_questions row did not survive its relation being deleted: %v — a foreign key would have cascaded it away, which is exactly what ADR-0027 refuses", err)
	}
	if relationID != "rel-doomed" {
		t.Errorf("relation_id = %q, want %q — still readable after the relation itself is gone", relationID, "rel-doomed")
	}
}

// TestPendingQuestionRepo_UnaskedAndOpenUsePartialIndexes is design §3.1's
// own stated purpose for the two partial indexes: confirm the query planner
// actually uses them rather than a full table scan.
func TestPendingQuestionRepo_UnaskedAndOpenUsePartialIndexes(t *testing.T) {
	for _, tc := range []struct {
		query string
		index string
	}{
		{`SELECT id FROM pending_questions WHERE asked_at IS NULL AND resolved_at IS NULL ORDER BY created_at, id`, "idx_pending_questions_unasked"},
		{`SELECT id FROM pending_questions WHERE asked_at IS NOT NULL AND resolved_at IS NULL ORDER BY asked_at DESC, id DESC`, "idx_pending_questions_open"},
	} {
		v := openTestVault(t)
		rows, err := v.db.QueryContext(context.Background(), `EXPLAIN QUERY PLAN `+tc.query)
		if err != nil {
			t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
		}
		var plan string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				_ = rows.Close()
				t.Fatalf("scanning query plan row: %v", err)
			}
			plan += detail + "\n"
		}
		_ = rows.Close()
		if !strings.Contains(plan, tc.index) {
			t.Errorf("query plan for %q does not mention %s:\n%s", tc.query, tc.index, plan)
		}
	}
}
