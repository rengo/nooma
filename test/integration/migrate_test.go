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
// PRAGMA user_version at the highest published version (design §5.2, only
// 0001 published as of this task).
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
	const wantVersion = 1 // only 0001_core_tables.sql is published at this stage
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
	const wantVersion = 1
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
	raw, err := sql.Open("sqlite3",
		"file:"+dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)&_txlock=immediate")
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
	if versionErr.BinaryVersion != 1 {
		t.Errorf("VersionError.BinaryVersion = %d, want 1 (only 0001 is published at this stage)", versionErr.BinaryVersion)
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
	const wantVersion = 1
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
