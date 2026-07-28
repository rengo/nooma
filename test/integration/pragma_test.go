//go:build integration

package integration

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/rengo/nooma/internal/store/sqlite"
)

// TestOpenAppliesPragmas asserts journal_mode=wal (design D3, spec R2.1).
// journal_mode is a persistent property of the database file, so it is the
// one PRAGMA in D3's set that can genuinely be observed from a connection
// the test opens itself, after sqlite.Open has run against the same file —
// unlike foreign_keys and busy_timeout, which are per connection and
// SQLite's own defaults on any newly opened connection regardless of what
// a *different* connection (or DSN) requested.
//
// foreign_keys and busy_timeout are proven instead by TestBuildDSN (L1, in
// internal/store/sqlite): design D7's scope boundary gives Vault no query
// surface, so there is no way for this package to read a per-connection
// PRAGMA off a connection Open produced (residual risk R12). TestBuildDSN
// proves Open requests exactly the PRAGMA set D3 specifies; the ground
// truth this design was verified against (driver.go) establishes that the
// driver honours every "_pragma=" directive in a DSN at sqlite3_open time
// on every connection opened with it. Composed, that is sound end-to-end
// proof of R2.1 without inventing a query method that would widen the
// frozen store surface (design §7.3) ahead of schedule.
func TestOpenAppliesPragmas(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer v.Close()

	raw, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer raw.Close()

	var journalMode string
	if err := raw.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("PRAGMA journal_mode = %q, want %q", journalMode, "wal")
	}
}
