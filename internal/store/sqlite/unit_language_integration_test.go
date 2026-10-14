//go:build integration

package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ncruces/go-sqlite3/driver"
)

// backfilled opens a vault at the version before the language column, lets seed
// write what a vault of that age holds, migrates it to the end and returns the
// result: the only way to prove the backfill, which runs once.
func backfilled(t *testing.T, seed func(exec func(query string, args ...any))) *sql.DB {
	t.Helper()
	ctx := context.Background()
	dsn, err := buildDSN(filepath.Join(t.TempDir(), "vault.db"), pathStyleForGOOS())
	if err != nil {
		t.Fatal(err)
	}
	db, err := driver.Open(dsn, initConn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	all, err := parseMigrations(migrationFS)
	if err != nil {
		t.Fatal(err)
	}
	before := all[:len(all)-1]
	if last := all[len(all)-1]; last.Name != "0008_unit_language.sql" {
		t.Fatalf("the newest migration is %s; this test is about 0008_unit_language.sql", last.Name)
	}
	if err := migrateMigrations(ctx, db, before); err != nil {
		t.Fatal(err)
	}
	seed(func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	})
	if err := migrateMigrations(ctx, db, all); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedLegacyUnit(exec func(string, ...any), id string) {
	exec(`INSERT INTO units (id, type, content, status, weight, weight_decay_rate, last_touched_at, source, created_at, updated_at)
	      VALUES (?, 'task', 'x', 'pool', 1.0, 0.1, '2026-10-01T00:00:00Z', 'test', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`, id)
}

func seedClassify(exec func(string, ...any), rowid, unitID, language string) {
	exec(`INSERT INTO decision_log (id, action, rationale, context, occurred_at)
	      VALUES (?, 'capture.classify', 'r', json_object('unit_id', ?, 'language', ?), '2026-10-01T00:00:00Z')`, rowid, unitID, language)
}

func languageOf(t *testing.T, db *sql.DB, id string) sql.NullString {
	t.Helper()
	var got sql.NullString
	if err := db.QueryRow(`SELECT language FROM units WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return got
}

// TestBackfillTakesTheEarliestClassifyByRowid: a correction re-classifies, so a
// unit has several capture.classify rows and the capture's own is the earliest by
// rowid. The ids are chosen so that id order is the reverse of row order, and a
// row of another action carrying a language is ignored.
func TestBackfillTakesTheEarliestClassifyByRowid(t *testing.T) {
	db := backfilled(t, func(exec func(string, ...any)) {
		seedLegacyUnit(exec, "u")
		seedLegacyUnit(exec, "other")
		exec(`INSERT INTO decision_log (id, action, rationale, context, occurred_at)
		      VALUES ('z-not-classify', 'capture.correct', 'r', json_object('unit_id', 'u', 'language', 'en'), '2026-10-01T00:00:00Z')`)
		seedClassify(exec, "z-first", "u", "es")
		seedClassify(exec, "a-second", "u", "en")
		seedClassify(exec, "m-other", "other", "en")
	})
	if got := languageOf(t, db, "u"); got.String != "es" || !got.Valid {
		t.Errorf("u.language = %+v, want es: the earliest capture.classify row, not the latest or the smallest id", got)
	}
	if got := languageOf(t, db, "other"); got.String != "en" || !got.Valid {
		t.Errorf("other.language = %+v, want en: each unit takes its own row", got)
	}
}

// TestBackfillEmptyLanguageIsNull: a classification that named no language wrote
// "" (capture.go), which is not a language: it becomes NULL, and a unit with no
// capture.classify row stays NULL. The earliest row decides even when empty.
func TestBackfillEmptyLanguageIsNull(t *testing.T) {
	db := backfilled(t, func(exec func(string, ...any)) {
		seedLegacyUnit(exec, "empty")
		seedLegacyUnit(exec, "unlogged")
		seedClassify(exec, "r1", "empty", "")
		seedClassify(exec, "r2", "empty", "es")
	})
	for _, id := range []string{"empty", "unlogged"} {
		if got := languageOf(t, db, id); got.Valid {
			t.Errorf("%s.language = %q, want NULL", id, got.String)
		}
	}
}
