//go:build integration

package integration

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestConcurrentOpenOnFreshVault asserts the slice-3 review's finding 1: N
// goroutines calling sqlite.Open on a path that does not exist yet must all
// succeed, and the migration must land exactly once (design D4's state
// matrix, spec R3.2/R3.3/R3.7).
//
// Before the opener's fix, this failed 60-80% of the time with
// "ping: sqlite3: invalid _pragma: sqlite3: database is locked". Root cause
// (isolated by controlled experiment, not assumed): converting a brand-new
// database file to WAL mode for the first time (buildDSN's
// journal_mode(wal) PRAGMA, design D3) requires SQLite to hold the file
// exclusively for that one-time conversion, and this specific exclusivity
// check is NOT retried through busy_timeout — it fails SQLITE_BUSY almost
// instantly, long before any busy_timeout budget elapses, the moment two
// processes' first connections race to convert the same fresh file. This is
// a defect in the connection opener (slice 2), not in the migration runner
// (design D4's algorithm is sound against an already-WAL-mode file racing
// at user_version=0 — that case passes 8/8 in a control run); slice 3 only
// surfaced it by being the first thing that makes two Open() calls
// genuinely race on the same brand-new file.
func TestConcurrentOpenOnFreshVault(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	const goroutines = 8

	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	vaults := make([]*sqlite.Vault, goroutines)
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			v, err := sqlite.Open(ctx, dbPath)
			errs[i] = err
			vaults[i] = v
		}(i)
	}
	wg.Wait()

	for _, v := range vaults {
		if v != nil {
			defer v.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran
		}
	}

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: sqlite.Open(%q) = _, %v, want nil error", i, dbPath, err)
		}
	}

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
		t.Errorf("PRAGMA user_version after %d concurrent first opens = %d, want %d (migration applied exactly once, not skipped and not reapplied)", goroutines, userVersion, wantVersion)
	}
}
