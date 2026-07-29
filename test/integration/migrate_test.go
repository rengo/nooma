//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestMigrateFromScratchSetsUserVersion asserts R3.2/R3.3: opening a
// brand-new vault applies every published migration and leaves
// PRAGMA user_version at the highest published version (design §5.2; two
// migrations, 0001 and 0002, are published as of this change — R3.8).
//
// user_version is a persistent property of the database file header, not a
// per-connection setting (same reasoning TestOpenAppliesPragmas already
// relies on for journal_mode), so it can be read back through a second,
// independent connection to the same file rather than needing a new
// exported accessor on Vault — the migration runner lives entirely inside
// sqlite.Open's own call chain (design D1).
func TestMigrateFromScratchSetsUserVersion(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer v.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	// "file:"+dbPath is a hand-built DSN, not routed through buildDSN's
	// path-style abstraction (internal/store/sqlite/dsn.go) — noted rather
	// than silently left, per the slice-3 review's suggestion 4. This is a
	// real package-boundary constraint, not an oversight: buildDSN is
	// unexported, and test/integration is a separate package by design
	// (design D7's containment layers — Vault's exported surface is meant
	// to stay frozen and reviewer-auditable via store_api.golden, task
	// 3.6). Exporting buildDSN purely so a black-box test could reach it
	// would widen that surface ahead of schedule. This particular read-back
	// only ever needs a plain absolute POSIX path (t.TempDir() never
	// returns a Windows-shaped one on this repo's own CI), so the gap costs
	// nothing here; a test that specifically needed to exercise
	// buildDSN's Windows branch already lives inside package sqlite itself
	// (dsn_test.go), where it can call buildDSN directly.
	raw, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer raw.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	var userVersion int
	if err := raw.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatalf("PRAGMA user_version: %v", err)
	}
	const wantVersion = 2 // 0001_core_tables.sql and 0002_learning_and_search.sql are both published
	if userVersion != wantVersion {
		t.Errorf("PRAGMA user_version = %d, want %d (opening a fresh vault must apply every published migration)", userVersion, wantVersion)
	}
}

// TestMigrateIsIdempotent asserts R3.5: opening a vault that is already at
// the highest published user_version applies zero migrations and does not
// error. A naive re-run of every migration unconditionally (this task's
// starting point, task 3.2) fails on "table units already exists" — this
// test's RED is exactly that failure, and it is what forces the
// state-matrix algorithm (design §5.2/§5.3, D4).
func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	v1, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) [first open] = _, %v, want nil error", dbPath, err)
	}
	if err := v1.Close(); err != nil {
		t.Fatalf("v1.Close() = %v, want nil error", err)
	}

	v2, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) [second open, already at target version] = _, %v, want nil error (must not re-run an applied migration)", dbPath, err)
	}
	defer v2.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	raw, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer raw.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	var userVersion int
	if err := raw.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatalf("PRAGMA user_version: %v", err)
	}
	const wantVersion = 2
	if userVersion != wantVersion {
		t.Errorf("PRAGMA user_version after reopening an already-migrated vault = %d, want %d (unchanged)", userVersion, wantVersion)
	}
}

// TestVaultNewerThanBinaryRefusesToOpen asserts R3.6: a vault whose
// user_version is higher than the highest version this binary's embedded
// migrations know how to apply (an old binary opening a newer vault)
// refuses to open, returns a *sqlite.VersionError, and leaves the vault
// file unmodified — no migration attempted, no forward skip, no silent
// proceed (design §5.3's downgrade row, "the one where being wrong
// corrupts a real person's vault").
func TestVaultNewerThanBinaryRefusesToOpen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	// Seed a real, migrated vault through sqlite.Open first, so the file is
	// already in the store's own operational state (WAL journal mode in
	// particular — a persistent, idempotent file property per design D3 —
	// applied once here). This matters for the "unmodified" assertion
	// below: opening a file that has never been in WAL mode always rewrites
	// its header the first time journal_mode=wal is requested, which would
	// make ANY reopen look like a modification, refused or not, and prove
	// nothing about this test's actual claim.
	seed, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) [seed] = _, %v, want nil error", dbPath, err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed.Close() = %v, want nil error", err)
	}

	// Bump user_version past anything this binary embeds, through a raw
	// connection using the store's own PRAGMA set (so this step itself
	// changes only the version header field, not the journal mode).
	const futureVersion = 999
	// pragmaDSN (foreign_key_test.go, same package) reconstructs sqlite.Open's
	// exact pragma set — shared rather than duplicated inline (slice-4a
	// four-lens pre-PR review): this file used to hand-build the same
	// byte-identical DSN string here.
	raw, err := sql.Open("sqlite3", pragmaDSN(dbPath))
	if err != nil {
		t.Fatalf("sql.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	if _, err := raw.ExecContext(ctx, "PRAGMA user_version = 999"); err != nil {
		t.Fatalf("seed PRAGMA user_version = %v, want nil error", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("raw.Close() = %v, want nil error", err)
	}

	before, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) [before] = _, %v, want nil error", dbPath, err)
	}

	_, err = sqlite.Open(ctx, dbPath)
	if err == nil {
		t.Fatal("sqlite.Open(...) on a vault newer than this binary = nil error, want a non-nil *sqlite.VersionError")
	}
	var versionErr *sqlite.VersionError
	if !errors.As(err, &versionErr) {
		t.Fatalf("sqlite.Open(...) error = %v, want it to wrap a *sqlite.VersionError", err)
	}
	if versionErr.VaultVersion != futureVersion {
		t.Errorf("VersionError.VaultVersion = %d, want %d", versionErr.VaultVersion, futureVersion)
	}
	if versionErr.BinaryVersion != 2 {
		t.Errorf("VersionError.BinaryVersion = %d, want 2 (0001 and 0002 are both published at this stage)", versionErr.BinaryVersion)
	}

	after, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) [after] = _, %v, want nil error", dbPath, err)
	}
	if string(before) != string(after) {
		t.Error("the vault file changed after a refused open, want it byte-for-byte unmodified")
	}

	// Belt-and-braces (suggestion in the slice-3 review): under WAL,
	// writes can land in the "-wal" sidecar rather than the main file, so
	// a byte-for-byte comparison of dbPath alone cannot rule out a write
	// landing there instead. A refused open must leave no sidecar either.
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); err == nil {
			t.Errorf("%s exists after a refused open, want no WAL sidecar left behind", dbPath+suffix)
		} else if !os.IsNotExist(err) {
			t.Errorf("os.Stat(%q) = %v, want either nil (sidecar exists, itself a failure above) or a not-exist error", dbPath+suffix, err)
		}
	}
}

// TestMigrationsAreEmbeddedNotReadFromDisk asserts R3.1: migration SQL is
// embedded into the binary via go:embed and is never read from the
// filesystem at runtime. Proven by opening a vault whose own directory
// contains zero .sql files (and still contains zero afterward) and
// confirming migrations apply anyway — if parseMigrations ever read from
// disk instead of the embed, this would fail with "no such file or
// directory" or apply nothing.
func TestMigrationsAreEmbeddedNotReadFromDisk(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vault.db")

	entriesBefore, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir(%q) = _, %v, want nil error", dir, err)
	}
	for _, e := range entriesBefore {
		if strings.HasSuffix(e.Name(), ".sql") {
			t.Fatalf("test setup invariant violated: %q already contains a .sql file (%s)", dir, e.Name())
		}
	}

	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer v.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	raw, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer raw.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	var userVersion int
	if err := raw.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatalf("PRAGMA user_version: %v", err)
	}
	const wantVersion = 2
	if userVersion != wantVersion {
		t.Errorf("PRAGMA user_version = %d, want %d (migrations must apply even with no .sql file anywhere on disk)", userVersion, wantVersion)
	}

	entriesAfter, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir(%q) [after] = _, %v, want nil error", dir, err)
	}
	for _, e := range entriesAfter {
		if strings.HasSuffix(e.Name(), ".sql") {
			t.Errorf("%q contains a .sql file (%s) after Open — nothing should ever write one there", dir, e.Name())
		}
	}
}

// TestMigrateAppliesOnlyPendingMigrations asserts R3.4: a vault already at an
// intermediate version applies only the migrations after that version, in
// order, and never re-runs one already applied.
//
// Every existing test in this file starts a vault at user_version = 0 (a
// brand-new file, TestMigrateFromScratchSetsUserVersion) or already at the
// target (TestMigrateIsIdempotent) — the `0 < current < target` case in
// migrateMigrations' skip loop (`if m.Version <= current { continue }`,
// internal/store/sqlite/migrate.go) was never exercised by any test until
// this one, and it only became reachable at all once 0002 published a
// second migration in this same change (four-lens pre-PR review, slice 4a,
// BLOCKER finding 1). A regression here — `<` instead of `<=`, or comparing
// against the wrong variable — would either re-run 0001 loudly (a
// "table already exists" failure) or silently skip 0002 while Open still
// reports success, leaving a vault the binary believes is fully migrated but
// is not.
//
// The pre-seed below applies ONLY 0001_core_tables.sql directly, by reading
// it from its real file on disk (test/integration is a separate package
// from internal/store/sqlite and migrationFS is unexported — design D7's
// scope boundary — so this reads the same published file sqlite.Open itself
// embeds, not a private copy of its content).
//
// Two mutations were actually tried against the skip condition, and they
// behave differently — worth recording both, not just the one that produced
// the expected red:
//
//   - "m.Version <= current" -> "m.Version < current" does NOT turn this
//     test red. applyMigration re-reads user_version INSIDE its own
//     transaction and independently checks "current >= m.Version" before
//     doing any work (design D4's race guard, added by the PR-3 review's
//     finding 2 fix) — that second, stricter check silently absorbs this
//     particular bug: the transaction opens, observes 0001 is already
//     applied, and rolls back doing nothing. Genuine defense in depth, not a
//     gap this test needs to plug.
//
//   - "m.Version <= current" -> "m.Version <= target" DOES turn this test
//     red — this is the "compares against the wrong version" danger the
//     BLOCKER finding named, and nothing else in migrateMigrations/
//     applyMigration catches it, because the outer loop then never calls
//     applyMigration for 0002 at all. RED, watched (verbatim):
//
//     migrate_test.go:326: PRAGMA user_version after reopening a vault seeded at version 1 = 1, want 2 (only 0002 should have applied)
//     migrate_test.go:335: sqlite_master lookup for learning_signals (a 0002-only table) = sql: no rows in result set, want it to be found (0002 must have run)
//
//     — exactly R3.4's "silently skip 0002 while reporting success" failure
//     mode, the more dangerous of the two the finding described. Reverting
//     to "m.Version <= current" makes this test pass again.
func TestMigrateAppliesOnlyPendingMigrations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	migration0001, err := os.ReadFile("../../internal/store/sqlite/migrations/0001_core_tables.sql")
	if err != nil {
		t.Fatalf("os.ReadFile(0001_core_tables.sql) = _, %v, want nil error", err)
	}

	seed, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%q) [seed] = _, %v, want nil error", dbPath, err)
	}
	// ExecContext must be called with zero arguments: with arguments the
	// driver falls back to Prepare, which rejects a trailing statement, and
	// 0001 is many statements (same constraint applyMigration itself
	// observes in migrate.go).
	if _, err := seed.ExecContext(ctx, string(migration0001)); err != nil {
		t.Fatalf("apply 0001_core_tables.sql directly [seed] = %v, want nil error", err)
	}
	if _, err := seed.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
		t.Fatalf("seed PRAGMA user_version = 1 = %v, want nil error", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed.Close() = %v, want nil error", err)
	}

	// Reopen through the store — this is the incremental path under test.
	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) [reopen at intermediate version] = _, %v, want nil error", dbPath, err)
	}
	defer v.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	raw, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer raw.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	var userVersion int
	if err := raw.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatalf("PRAGMA user_version: %v", err)
	}
	const wantVersion = 2
	if userVersion != wantVersion {
		t.Errorf("PRAGMA user_version after reopening a vault seeded at version 1 = %d, want %d (only 0002 should have applied)", userVersion, wantVersion)
	}

	// Prove 0002 actually ran — not just that the version counter moved —
	// by checking one of the tables only 0002 creates exists.
	var name string
	err = raw.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name='learning_signals'").Scan(&name)
	if err != nil {
		t.Errorf("sqlite_master lookup for learning_signals (a 0002-only table) = %v, want it to be found (0002 must have run)", err)
	}
}
