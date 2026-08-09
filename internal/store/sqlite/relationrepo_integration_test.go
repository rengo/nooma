//go:build integration

package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// relationFixtureTime is a fixed, whole-second UTC instant: this suite does
// not exercise clock behaviour, so a literal keeps every fixture identical.
var relationFixtureTime = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

// relationHarness adapts the real repo to repocontract.RelationHarness.
//
// EnsureUnit is the whole reason the harness exists: relations.from_unit_id
// and relations.to_unit_id both REFERENCE units(id) (migration 0001:32-33)
// and the vault opens with foreign_keys=on, so an Upsert naming a unit that
// does not exist is a constraint violation here and a no-op in the fake. It
// inserts directly rather than going through UnitRepo — the fixture must not
// depend on another port's behaviour to set up this one's (the same
// reasoning internal/store/sqlite/embeddingrepo_integration_test.go's own
// embeddingHarness.EnsureUnit already applies).
//
// SeedThreshold inserts a relation_thresholds row directly: there is no
// write method for that table on ports.RelationRepo (design D8 names only
// three methods, each with a real caller).
type relationHarness struct {
	*RelationRepo
	v *Vault
}

func (h relationHarness) EnsureUnit(t *testing.T, id string) {
	t.Helper()

	_, err := h.v.db.ExecContext(context.Background(), `
INSERT INTO units (id, type, content, status, weight, weight_decay_rate,
                   last_touched_at, source, created_at, updated_at)
VALUES (?, 'task', 'fixture', 'pool', 1.0, 0.01, ?, 'chat', ?, ?)
ON CONFLICT(id) DO NOTHING`,
		id, relationFixtureTime.Format(unitTimeLayout),
		relationFixtureTime.Format(unitTimeLayout),
		relationFixtureTime.Format(unitTimeLayout))
	if err != nil {
		t.Fatalf("seeding unit %s: %v", id, err)
	}
}

// SetLastTouchedAt implements repocontract.RelationHarness by updating the
// real units.last_touched_at column directly — a real implementation, not a
// placeholder: setting a column on an already-migrated table needs no new
// port method and no PR 5 dependency, unlike Evidence's own join logic.
func (h relationHarness) SetLastTouchedAt(t *testing.T, id string, at time.Time) {
	t.Helper()

	res, err := h.v.db.ExecContext(context.Background(),
		`UPDATE units SET last_touched_at = ? WHERE id = ?`, at.Format(unitTimeLayout), id)
	if err != nil {
		t.Fatalf("setting last_touched_at for unit %s: %v", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("rows affected setting last_touched_at for unit %s: %v", id, err)
	}
	if n == 0 {
		t.Fatalf("SetLastTouchedAt(%s): no row updated — call EnsureUnit(%s) first", id, id)
	}
}

func (h relationHarness) SeedThreshold(t *testing.T, relType string, persist, surface float64) {
	t.Helper()

	_, err := h.v.db.ExecContext(context.Background(), `
INSERT INTO relation_thresholds (id, relation_type, min_confidence_to_persist, min_confidence_to_surface)
VALUES (?, ?, ?, ?)`,
		"threshold-"+relType, relType, persist, surface)
	if err != nil {
		t.Fatalf("seeding relation_thresholds for %s: %v", relType, err)
	}
}

// TestRelationRepo_Contract runs the same repocontract.RunRelationRepo suite
// the in-memory fake answers at L2, now against a real temporary SQLite
// vault at L3 — design D6's "answered twice" standing rule.
func TestRelationRepo_Contract(t *testing.T) {
	repocontract.RunRelationRepo(t, func(t *testing.T) repocontract.RelationHarness {
		v := openTestVault(t)
		return relationHarness{RelationRepo: NewRelationRepo(v), v: v}
	})
}

// TestRelationRepo_UpsertSatisfiesRealUniqueConstraint is task 11b.3's own
// claim, below the contract: the SQL upsert
// (ON CONFLICT (from_unit_id, to_unit_id, type) DO UPDATE SET strength =
// excluded.strength, confidence = excluded.confidence) runs against the
// real UNIQUE constraint (confirmed present, migration 0001:39), not only
// the fake's map key — I07 proven against real SQLite.
//
// This asserts the ON CONFLICT target directly: a second Upsert with a
// *different* id must still resolve against the (from, to, type) unique
// index rather than the id primary key, which is exactly the row the fake
// cannot get wrong (a Go map keyed on the triple can never "accidentally"
// conflict on a different column) but SQLite's ON CONFLICT clause names
// explicitly and could name wrong.
func TestRelationRepo_UpsertSatisfiesRealUniqueConstraint(t *testing.T) {
	v := openTestVault(t)
	h := relationHarness{RelationRepo: NewRelationRepo(v), v: v}
	h.EnsureUnit(t, "unit-from")
	h.EnsureUnit(t, "unit-to")
	ctx := context.Background()

	first := fixtureStoreRelation("rel-1", "unit-from", "unit-to", "reference", 0.4, 0.5)
	if err := h.Upsert(ctx, first); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}

	second := first
	second.ID = "rel-2"
	second.Strength = 0.9
	second.Confidence = 0.95
	if err := h.Upsert(ctx, second); err != nil {
		t.Fatalf("second Upsert over the same triple: %v — must resolve via ON CONFLICT "+
			"(from_unit_id, to_unit_id, type), not fail the UNIQUE constraint", err)
	}

	var count int
	if err := v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM relations`).Scan(&count); err != nil {
		t.Fatalf("counting relations: %v", err)
	}
	if count != 1 {
		t.Fatalf("relations holds %d rows after two Upserts over one triple, want 1 — the real "+
			"UNIQUE (from_unit_id, to_unit_id, type) constraint (migration 0001:39) was not "+
			"honored as an upsert target", count)
	}

	var (
		id, createdBy        string
		strength, confidence float64
	)
	row := v.db.QueryRowContext(ctx,
		`SELECT id, strength, confidence, created_by FROM relations WHERE from_unit_id = ? AND to_unit_id = ? AND type = ?`,
		"unit-from", "unit-to", "reference")
	if err := row.Scan(&id, &strength, &confidence, &createdBy); err != nil {
		t.Fatalf("reading the stored row: %v", err)
	}
	if id != first.ID {
		t.Errorf("id = %q, want %q — the second Upsert's id must not have replaced the first "+
			"row's identity (the ON CONFLICT clause must not SET id)", id, first.ID)
	}
	if createdBy != first.CreatedBy {
		t.Errorf("created_by = %q, want %q — the ON CONFLICT clause must not SET created_by",
			createdBy, first.CreatedBy)
	}
	if strength != second.Strength || confidence != second.Confidence {
		t.Errorf("strength/confidence = %v/%v, want the second Upsert's %v/%v",
			strength, confidence, second.Strength, second.Confidence)
	}
}

// TestRelationRepo_UpsertRejectsUnknownUnit proves the foreign key the
// contract's EnsureUnit hook exists to route around is real: an Upsert
// naming a unit that was never inserted must fail against the real vault
// (foreign_keys=on), the exact defect C11 found invisible until a second
// implementation existed.
func TestRelationRepo_UpsertRejectsUnknownUnit(t *testing.T) {
	v := openTestVault(t)
	h := relationHarness{RelationRepo: NewRelationRepo(v), v: v}
	ctx := context.Background()

	err := h.Upsert(ctx, fixtureStoreRelation("rel-1", "unit-never-created", "unit-also-never-created", "reference", 0.4, 0.5))
	if err == nil {
		t.Fatal("Upsert for units that do not exist returned nil error — " +
			"relations.from_unit_id/to_unit_id REFERENCES units(id) with foreign_keys=on")
	}
}

func fixtureStoreRelation(id, from, to, relType string, strength, confidence float64) ports.Relation {
	return ports.Relation{
		ID: id, FromUnitID: from, ToUnitID: to, Type: relType,
		Strength: strength, Confidence: confidence,
		CreatedBy: "system", CreatedAt: relationFixtureTime,
	}
}
