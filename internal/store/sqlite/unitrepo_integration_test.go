//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// unitFixtureTime is a fixed, whole-second UTC instant — this suite's own
// units.Create/UpdateContent calls do not exercise clock behavior, so a
// literal constant keeps every fixture in this file identical.
var unitFixtureTime = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

// openTestVault opens a fresh, migrated vault under t.TempDir() — migrations
// 0001/0002 only, this PR adds none (spec R4.4).
func openTestVault(t *testing.T) *Vault {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	v, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open(%q) = _, %v, want nil error", dbPath, err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v
}

// TestUnitRepo_Contract runs the same repocontract.RunUnitRepo suite PR 3
// ran against the in-memory fake at L2, now against a real temporary
// SQLite vault at L3 — design D6's "answered twice" standing rule.
func TestUnitRepo_Contract(t *testing.T) {
	repocontract.RunUnitRepo(t, func(t *testing.T) ports.UnitRepo {
		return NewUnitRepo(openTestVault(t))
	})
}

// TestUnitRepo_LiveByIDsFiltersPositively seeds one unit per status via a
// raw INSERT INTO units — bypassing UnitRepo.Create deliberately, so the
// fixture cannot lean on the repo already excluding non-pool rows — and
// asserts LiveByIDs returns exactly the pool unit. This distinguishes the
// correct positive filter (status = 'pool') from the specific wrong
// negative form spec R4.2's own MUST NOT names (status NOT IN
// ('superseded', 'incomplete')), which would wrongly include the archived
// row on this exact fixture.
func TestUnitRepo_LiveByIDsFiltersPositively(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()

	seedRawUnit(t, v.db, "raw-pool", string(unit.StatusPool))
	seedRawUnit(t, v.db, "raw-archived", string(unit.StatusArchived))
	seedRawUnit(t, v.db, "raw-superseded", string(unit.StatusSuperseded))
	seedRawUnit(t, v.db, "raw-incomplete", string(unit.StatusIncomplete))

	repo := NewUnitRepo(v)
	got, err := repo.LiveByIDs(ctx, []string{"raw-pool", "raw-archived", "raw-superseded", "raw-incomplete"})
	if err != nil {
		t.Fatalf("LiveByIDs: %v", err)
	}
	if len(got) != 1 || got[0].ID != "raw-pool" {
		t.Fatalf("LiveByIDs = %+v, want exactly the pool unit (raw-pool) — a negative exclusion filter would wrongly admit raw-archived too", got)
	}
}

// TestUnitRepo_UpdateContentDoesNotChangeRowCount is I03's behavioral half
// (R4.3): UpdateContent must never grow or shrink the units table, since no
// method on this implementation may delete a row.
func TestUnitRepo_UpdateContentDoesNotChangeRowCount(t *testing.T) {
	v := openTestVault(t)
	ctx := context.Background()
	repo := NewUnitRepo(v)

	before := countUnits(t, v.db)

	u := unit.Unit{
		ID:              "row-count-1",
		Type:            unit.TypeTask,
		Content:         "original",
		Status:          unit.StatusPool,
		Weight:          1.0,
		WeightDecayRate: 0.1,
		LastTouchedAt:   unitFixtureTime,
		Source:          "test",
		CreatedAt:       unitFixtureTime,
		UpdatedAt:       unitFixtureTime,
	}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	afterCreate := countUnits(t, v.db)
	if afterCreate != before+1 {
		t.Fatalf("row count after Create = %d, want %d", afterCreate, before+1)
	}

	if err := repo.UpdateContent(ctx, u.ID, "revised", unitFixtureTime); err != nil {
		t.Fatalf("UpdateContent: %v", err)
	}

	afterUpdate := countUnits(t, v.db)
	if afterUpdate != afterCreate {
		t.Errorf("row count after UpdateContent = %d, want unchanged at %d", afterUpdate, afterCreate)
	}
}

// countUnits reads SELECT COUNT(*) FROM units directly, bypassing UnitRepo
// entirely — this test checks the table's own row count, not what the
// repo's read methods report.
func countUnits(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM units").Scan(&n); err != nil {
		t.Fatalf("SELECT COUNT(*) FROM units: %v", err)
	}
	return n
}

// seedRawUnit inserts a minimal units row directly via SQL — not through
// UnitRepo.Create — so TestUnitRepo_LiveByIDsFiltersPositively's fixture
// cannot accidentally depend on the repo's own write path already
// excluding non-pool rows.
func seedRawUnit(t *testing.T, db *sql.DB, id, status string) {
	t.Helper()
	const at = "2026-07-31T12:00:00Z"
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO units (id, type, content, status, weight, weight_decay_rate, last_touched_at, source, created_at, updated_at)
		 VALUES (?, 'task', 'raw fixture', ?, 1.0, 0.1, ?, 'test', ?, ?)`,
		id, status, at, at, at,
	)
	if err != nil {
		t.Fatalf("seedRawUnit(%q, %q): %v", id, status, err)
	}
}
