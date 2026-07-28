//go:build integration

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

// TestOpenPRAGMAsReadBack is a white-box L3 test that proves foreign_keys
// and busy_timeout — the two PRAGMAs design D3 sets that
// TestOpenAppliesPragmas (test/integration/pragma_test.go) cannot observe,
// because they are per-connection and Vault exposes no query surface
// (design D7) — really are applied on a connection Open itself produced,
// by reading them back from inside this package.
//
// Location, recorded rather than silent, same rationale as
// txlock_integration_test.go: this file lives inside package sqlite so it
// can reach Vault's unexported db field directly. test/integration cannot
// do that without widening Vault's frozen exported surface ahead of PR 3's
// store_api.golden. Any connection reached through v.db already carries
// these PRAGMAs — they come from the same DSN driver.Open used to build
// the pool's connector, and the driver applies every "_pragma=" directive
// on every connection it opens (ground truth: driver.go), not only the
// first — so there is no need to target a specific pool connection.
//
// What this test does NOT prove: design §4.4 originally described the
// mitigation for this gap as an FK-violation test "against the migrated
// schema" — but no migrated schema exists until PR 3/4 create tables with
// foreign keys, so that behavioral proof cannot be written in this PR. It
// is scheduled as an explicit PR 4 task, with its own stated RED, in
// tasks.md. What this test proves instead is real observation, not string
// matching: it reads PRAGMA foreign_keys and PRAGMA busy_timeout back from
// a live connection Open produced, rather than asserting on the DSN
// string buildDSN itself generated (TestBuildDSN, dsn_test.go) — the same
// defect class the _txlock bug once exposed.
func TestOpenPRAGMAsReadBack(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vault.db")
	ctx := context.Background()

	v, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer v.db.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	var foreignKeys int
	if err := v.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("PRAGMA foreign_keys = %d, want 1", foreignKeys)
	}

	var busyTimeout int
	if err := v.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("PRAGMA busy_timeout = %d, want 5000", busyTimeout)
	}
}
