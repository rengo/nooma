//go:build integration

package integration

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"

	"github.com/rengo/nooma/internal/store/sqlite"
)

// openMigratedRawConn migrates a fresh vault through sqlite.Open (so
// 0001/0002 run for real, the same way any real vault gets its schema), then
// closes it and returns a raw, FTS5-registered connection to the same file.
// Vault exposes no query surface (design D7), so a test that needs to insert
// rows and read units_fts back cannot do it through Vault itself — this is
// the same "raw connection, FTS5 registered the same way sqlite.Open does"
// pattern schema_golden_test.go already uses (design §7.2's recorded
// exception for test/integration), reused here because these tests need the
// same real, migrated schema, not a synthetic one.
func openMigratedRawConn(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "vault.db")

	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("v.Close() = %v, want nil error", err)
	}

	raw, err := driver.Open("file:"+dbPath, func(c *sqlite3.Conn) error {
		return fts5.Register(c)
	})
	if err != nil {
		t.Fatalf("driver.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	t.Cleanup(func() {
		_ = raw.Close() // best-effort cleanup, the assertions above already ran
	})
	return raw
}

// insertUnit inserts a minimal, valid units row (every NOT NULL column
// without a default supplied explicitly — type, content, last_touched_at,
// created_at, updated_at; status/source/weight/weight_decay_rate keep their
// schema defaults) and returns its rowid, which is what units_fts is keyed
// on (content_rowid='rowid').
func insertUnit(t *testing.T, ctx context.Context, db *sql.DB, id, content string) int64 {
	t.Helper()

	res, err := db.ExecContext(ctx,
		`INSERT INTO units (id, type, content, last_touched_at, created_at, updated_at)
		 VALUES (?, 'knowledge', ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		id, content)
	if err != nil {
		t.Fatalf("insert units row %q: %v, want nil error", id, err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() for units row %q = _, %v, want nil error", id, err)
	}
	return rowid
}

// ftsMatchCount returns how many rows units_fts reports for a MATCH query —
// the runtime proof that the sync triggers (0002_learning_and_search.sql)
// actually keep units_fts current with units.content, which — before this
// file — nothing in the test suite exercised: the schema golden only proves
// the triggers' DDL text was written (structural), and the migration-file
// statement-splitter tests only prove the file parses, neither ever runs
// the triggers against a real INSERT/UPDATE/DELETE (four-lens pre-PR review,
// slice 4a, CRITICAL finding 2).
func ftsMatchCount(t *testing.T, ctx context.Context, db *sql.DB, query string) int {
	t.Helper()

	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM units_fts WHERE units_fts MATCH ?", query).Scan(&n); err != nil {
		t.Fatalf("SELECT count(*) FROM units_fts WHERE units_fts MATCH %q: %v, want nil error", query, err)
	}
	return n
}

// TestFTS5IndexTracksInsertedContent proves units_fts_ai actually runs: a
// freshly inserted units row becomes findable by its content through
// units_fts, not merely declared in the schema.
//
// RED, watched (verbatim, after commenting out the CREATE TRIGGER
// units_fts_ai statement in 0002_learning_and_search.sql and reverting the
// schema goldens to match — see this finding's write-up for the full
// before/after):
//
//	fts5_search_test.go: units_fts MATCH "aardvark" count = 0, want 1 (the inserted unit must be findable)
//
// Restoring the trigger makes this test pass again.
func TestFTS5IndexTracksInsertedContent(t *testing.T) {
	ctx := context.Background()
	db := openMigratedRawConn(t, ctx)

	insertUnit(t, ctx, db, "u-insert", "an aardvark wandered into the office")

	if got, want := ftsMatchCount(t, ctx, db, "aardvark"), 1; got != want {
		t.Errorf(`units_fts MATCH "aardvark" count = %d, want %d (the inserted unit must be findable)`, got, want)
	}
}

// TestFTS5IndexTracksUpdatedContent proves units_fts_au actually runs, and
// that it runs as a delete-then-insert pair (external-content FTS5 tables
// have no UPDATE path at all — 0002_learning_and_search.sql's own trigger
// comment explains why there are three triggers, not one): after an UPDATE
// changes units.content, the new content is findable and the old content is
// not.
//
// RED, watched (verbatim, after commenting out the CREATE TRIGGER
// units_fts_au statement in 0002_learning_and_search.sql and reverting the
// schema goldens to match):
//
//	fts5_search_test.go: units_fts MATCH "giraffe" (new content) count = 0, want 1 (an UPDATE must reindex the new content)
//	fts5_search_test.go: units_fts MATCH "aardvark" (stale old content) count = 1, want 0 (an UPDATE must remove the old content from the index)
//
// Restoring the trigger makes this test pass again.
func TestFTS5IndexTracksUpdatedContent(t *testing.T) {
	ctx := context.Background()
	db := openMigratedRawConn(t, ctx)

	insertUnit(t, ctx, db, "u-update", "an aardvark wandered into the office")

	if _, err := db.ExecContext(ctx,
		"UPDATE units SET content = ?, updated_at = '2026-01-02T00:00:00Z' WHERE id = 'u-update'",
		"a giraffe wandered into the office"); err != nil {
		t.Fatalf("UPDATE units.content: %v, want nil error", err)
	}

	if got, want := ftsMatchCount(t, ctx, db, "giraffe"), 1; got != want {
		t.Errorf(`units_fts MATCH "giraffe" (new content) count = %d, want %d (an UPDATE must reindex the new content)`, got, want)
	}
	if got, want := ftsMatchCount(t, ctx, db, "aardvark"), 0; got != want {
		t.Errorf(`units_fts MATCH "aardvark" (stale old content) count = %d, want %d (an UPDATE must remove the old content from the index)`, got, want)
	}
}

// TestFTS5IndexStaysConsistentAcrossArchival proves the specific semantic
// the four-lens review flagged as the important one: archiving a unit is an
// UPDATE of units.status to 'archived' — NEVER a DELETE (CLAUDE.md
// non-negotiable #6) — and units_fts_au fires on every UPDATE regardless of
// which column changed, so the archived unit's content stays exactly as
// findable afterward as before, at the same rowid, with no duplicate entry.
//
// This is deliberate, not an oversight: docs/02-cognitive-core.md's archived
// units are "cold, weight ~= 0", NOT excluded from read surfaces — only
// superseded/incomplete units are, and that exclusion is applied positively
// (status = 'pool') at query time, never by removing rows from the FTS
// index. Excluding archived rows from units_fts at index time would be
// wrong, not just unnecessary: units_fts is content='units',
// content_rowid='rowid' and must mirror units row-for-row, or FTS5 returns
// rowids that resolve to the wrong unit or to nothing.
func TestFTS5IndexStaysConsistentAcrossArchival(t *testing.T) {
	ctx := context.Background()
	db := openMigratedRawConn(t, ctx)

	rowid := insertUnit(t, ctx, db, "u-archive", "an aardvark wandered into the office")

	if _, err := db.ExecContext(ctx,
		"UPDATE units SET status = 'archived', updated_at = '2026-01-02T00:00:00Z' WHERE id = 'u-archive'"); err != nil {
		t.Fatalf("UPDATE units.status = 'archived': %v, want nil error", err)
	}

	if got, want := ftsMatchCount(t, ctx, db, "aardvark"), 1; got != want {
		t.Errorf(`units_fts MATCH "aardvark" after archival count = %d, want %d (an archived unit is NOT deleted, so it must stay findable — doc 02's "cold, weight ~= 0", not excluded from read surfaces)`, got, want)
	}

	var gotRowid int64
	if err := db.QueryRowContext(ctx, "SELECT rowid FROM units_fts WHERE units_fts MATCH 'aardvark'").Scan(&gotRowid); err != nil {
		t.Fatalf("SELECT rowid FROM units_fts WHERE units_fts MATCH 'aardvark': %v, want nil error", err)
	}
	if gotRowid != rowid {
		t.Errorf("units_fts rowid after archival = %d, want %d (the same rowid units.rowid always had — the archival UPDATE must not have produced a different or duplicate entry)", gotRowid, rowid)
	}

	// Confirm the row still exists in units itself, status='archived', not
	// deleted — the premise this whole test depends on (CLAUDE.md
	// non-negotiable #6).
	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM units WHERE id = 'u-archive'").Scan(&status); err != nil {
		t.Fatalf("SELECT status FROM units WHERE id = 'u-archive': %v, want nil error (archiving must never delete the row)", err)
	}
	if status != "archived" {
		t.Errorf("units.status for u-archive = %q, want %q", status, "archived")
	}
}

// TestFTS5IndexRemovesDeletedContent proves units_fts_ad actually runs.
// No application path deletes a units row today (`rg "DELETE FROM units"`
// finds nothing under internal/ or test/), so this exercises the trigger
// with an out-of-band DELETE the same way a future maintenance path or a
// manual repair might, and proves the DDL is not merely declared but wired
// correctly.
//
// RED, watched (verbatim, after commenting out the CREATE TRIGGER
// units_fts_ad statement in 0002_learning_and_search.sql and reverting the
// schema goldens to match):
//
//	fts5_search_test.go: units_fts MATCH "aardvark" after DELETE count = 1, want 0 (a DELETE must remove the row from the index)
//
// Restoring the trigger makes this test pass again.
func TestFTS5IndexRemovesDeletedContent(t *testing.T) {
	ctx := context.Background()
	db := openMigratedRawConn(t, ctx)

	insertUnit(t, ctx, db, "u-delete", "an aardvark wandered into the office")

	if got, want := ftsMatchCount(t, ctx, db, "aardvark"), 1; got != want {
		t.Fatalf(`units_fts MATCH "aardvark" before DELETE count = %d, want %d (test setup invariant)`, got, want)
	}

	if _, err := db.ExecContext(ctx, "DELETE FROM units WHERE id = 'u-delete'"); err != nil {
		t.Fatalf("DELETE FROM units WHERE id = 'u-delete': %v, want nil error", err)
	}

	if got, want := ftsMatchCount(t, ctx, db, "aardvark"), 0; got != want {
		t.Errorf(`units_fts MATCH "aardvark" after DELETE count = %d, want %d (a DELETE must remove the row from the index)`, got, want)
	}
}
