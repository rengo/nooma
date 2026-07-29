//go:build integration

package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ncruces/go-sqlite3/driver"
)

// TestApplyMigrationRefusesWhenRacedPastTarget is an L3 regression test for
// slice-3 review finding 2: an old binary (a smaller target) and a new
// binary (a larger target) racing to open the same fresh vault must never
// let the old binary silently hand back a *Vault over a schema it has
// never seen.
//
// It reproduces the race deterministically, without two real compiled
// binaries or goroutine timing, by driving the runner directly (this file
// lives inside package sqlite so it can reach migrateMigrations and
// applyMigration, which are unexported — same rationale as
// txlock_integration_test.go and pragma_integration_test.go):
//
//  1. Simulate the NEW binary (target=2) running to completion first:
//     migrateMigrations with a two-migration synthetic set leaves the
//     vault at user_version=2.
//  2. Simulate the OLD binary (target=1), which — per the real Open() call
//     sequence — read current=0 BEFORE any of this happened (so it decided
//     to apply migration 1), and only NOW reaches applyMigration after
//     losing the race for the write lock. Calling applyMigration directly
//     for migration 1 with target=1 reproduces exactly that entry point,
//     bypassing migrateMigrations's own outer current > target fast path
//     on purpose — that fast path only ever sees a snapshot BEFORE the
//     race, never the version the vault raced to while this binary
//     waited, which is the whole point of the bug being reproduced.
//
// Before the fix, applyMigration's only guard was `current >= m.Version`:
// with current=2 and m.Version=1, that evaluates true and returns nil —
// the old binary concludes "already applied, skip" and Open hands back a
// working *Vault over a schema it has never seen, with no signal. This
// test's RED is exactly that: err == nil where a non-nil *VersionError is
// required.
func TestApplyMigrationRefusesWhenRacedPastTarget(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	dsn, err := buildDSN(dbPath, pathStyleForGOOS())
	if err != nil {
		t.Fatalf("buildDSN(%q) = _, %v, want nil error", dbPath, err)
	}
	db, err := driver.Open(dsn, initConn)
	if err != nil {
		t.Fatalf("driver.Open(%q) = _, %v, want nil error", dsn, err)
	}
	defer db.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("db.PingContext() = %v, want nil error", err)
	}

	m1 := migration{Version: 1, Name: "0001_a.sql", SQL: "CREATE TABLE a (id TEXT PRIMARY KEY);"}
	m2 := migration{Version: 2, Name: "0002_b.sql", SQL: "CREATE TABLE b (id TEXT PRIMARY KEY);"}

	// Step 1: the "new binary" (target=2) runs to completion first.
	if err := migrateMigrations(ctx, db, []migration{m1, m2}); err != nil {
		t.Fatalf("migrateMigrations(...) [new binary, target=2] = %v, want nil error", err)
	}
	newBinaryVersion, err := readUserVersion(ctx, db)
	if err != nil {
		t.Fatalf("readUserVersion() after the new binary's run = _, %v, want nil error", err)
	}
	if newBinaryVersion != 2 {
		t.Fatalf("user_version after the new binary's run = %d, want 2 (test setup invariant)", newBinaryVersion)
	}

	// Step 2: the "old binary" (target=1) now enters applyMigration for
	// migration 1, having decided to apply it before any of step 1
	// happened.
	err = applyMigration(ctx, db, m1, 1)
	if err == nil {
		t.Fatal("applyMigration(...) [old binary, target=1, raced past by a newer binary] = nil error, want a non-nil *VersionError")
	}
	var versionErr *VersionError
	if !errors.As(err, &versionErr) {
		t.Fatalf("applyMigration(...) error = %v, want it to wrap a *VersionError", err)
	}
	if versionErr.VaultVersion != 2 {
		t.Errorf("VersionError.VaultVersion = %d, want 2", versionErr.VaultVersion)
	}
	if versionErr.BinaryVersion != 1 {
		t.Errorf("VersionError.BinaryVersion = %d, want 1 (the old binary's own target)", versionErr.BinaryVersion)
	}

	// The refusal must not have touched the vault: still at the new
	// binary's version, no partial re-application of migration 1.
	afterVersion, err := readUserVersion(ctx, db)
	if err != nil {
		t.Fatalf("readUserVersion() after the refused apply = _, %v, want nil error", err)
	}
	if afterVersion != 2 {
		t.Errorf("user_version after the old binary's refused apply = %d, want 2 (unchanged)", afterVersion)
	}
}
