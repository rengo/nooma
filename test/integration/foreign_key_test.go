//go:build integration

package integration

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"

	"github.com/rengo/nooma/internal/store/sqlite"
)

// violatingUnitEmbeddingInsert violates unit_embeddings.unit_id's foreign
// key to units(id) (0002_learning_and_search.sql): "does-not-exist" is never
// inserted into units by either test in this file.
const violatingUnitEmbeddingInsert = `INSERT INTO unit_embeddings (unit_id, model, dim, embedding, created_at) VALUES ('does-not-exist', 'test-model', 1, x'00000000', '2026-01-01T00:00:00Z')`

// pragmaDSN reconstructs the exact pragma set sqlite.Open's buildDSN
// produces (design D3: busy_timeout, journal_mode=wal, foreign_keys=on,
// _txlock=immediate). This is a hand-built DSN, not routed through the
// unexported buildDSN, for the same package-boundary reason already
// recorded in migrate_test.go's TestMigrateFromScratchSetsUserVersion:
// Vault exposes no query surface (design D7), so a test needing to observe
// a per-connection PRAGMA's effect from test/integration must open its own
// connection with the same pragmas, not reach into Vault's unexported *sql.DB.
//
// Shared by every file in this package that needs the same reconstructed
// DSN (this file and migrate_test.go's TestVaultNewerThanBinaryRefusesToOpen)
// so the pragma string has exactly one definition. It used to be duplicated
// byte-for-byte in both files — same package, no boundary reason for two
// copies — which meant a future change to sqlite.Open's pragma set could
// update one copy and leave the other silently reconstructing something
// `Open` no longer does (four-lens pre-PR review, slice 4a).
func pragmaDSN(dbPath string) string {
	return "file:" + dbPath + "?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)&_txlock=immediate"
}

// TestForeignKeysExplicitlyDisabledAcceptsViolation is the control for
// TestOpenForeignKeysRejectViolation below, and the reason that test is
// genuinely falsifiable rather than a test that can only pass.
//
// Corrected during this task, against evidence, from what tasks.md and
// spec.md R2.1 originally assumed: "foreign_keys defaults to off per SQLite
// connection" is FALSE for this driver. A probe against a completely fresh,
// never-migrated file, opened with a bare "file:" DSN carrying no "_pragma"
// at all, reads PRAGMA foreign_keys back as 1, not 0. Root cause, verified
// in the vendored source, not assumed:
// github.com/ncruces/go-sqlite3-wasm/v3@v3.2.35303/build/sqlite_opt.h:23
// defines SQLITE_DEFAULT_FOREIGN_KEYS 1 — this is a compile-time default
// baked into the WASM SQLite build ADR-0001 chose, so it applies to EVERY
// connection this driver opens, whether or not sqlite.Open (or anything
// else) ever requests foreign_keys=ON. ADR-0001 and docs/03-data-model.md
// are not wrong to require foreign_keys=ON — sqlite.Open still requests it
// explicitly, and should keep doing so rather than depend on a compile
// default nothing enforces at the Go level — but the specific *mechanism*
// R2.1/design §4.4/tasks.md 4.3 described ("an opener that forgot to
// request it would let this through silently") cannot be reproduced with
// this driver: there is no "forgot" state to fall into.
//
// So this control does not omit the pragma — it EXPLICITLY overrides the
// compiled-in default with "_pragma=foreign_keys(off)" (confirmed by a
// probe to actually read back 0, unlike the bare DSN), and proves
// violatingUnitEmbeddingInsert is not rejected for some unrelated reason —
// schema mistake, type mismatch, missing table — by observing it succeed
// there. If this control ever failed, TestOpenForeignKeysRejectViolation
// below would be proving nothing about foreign_keys=ON specifically.
func TestForeignKeysExplicitlyDisabledAcceptsViolation(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	seed, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) [seed] = _, %v, want nil error", dbPath, err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed.Close() = %v, want nil error", err)
	}

	dsn := "file:" + dbPath + "?_pragma=foreign_keys(off)"
	raw, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("sql.Open(%q) = _, %v, want nil error", dsn, err)
	}
	defer raw.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	var fk int
	if err := raw.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 0 {
		t.Fatalf("PRAGMA foreign_keys = %d with an explicit _pragma=foreign_keys(off), want 0 — this control's premise is false, so it proves nothing", fk)
	}

	if _, err := raw.ExecContext(ctx, violatingUnitEmbeddingInsert); err != nil {
		t.Fatalf(
			"INSERT violating unit_embeddings.unit_id's FK, on a connection with foreign_keys explicitly OFF, = %v, want nil error "+
				"(control: this insert must succeed here, or TestOpenForeignKeysRejectViolation proves nothing about foreign_keys=ON specifically)",
			err,
		)
	}
}

// TestOpenForeignKeysRejectViolation asserts R2.1's behavioral half (design
// §4.4's original intent): a vault whose connections carry the same
// pragma set sqlite.Open applies rejects an insert violating one of
// 0002_learning_and_search.sql's foreign keys, with SQLite's own
// "FOREIGN KEY constraint failed" error. Deferred from PR 2 to this task
// because no migrated schema — and so no real foreign key to violate —
// existed until 0002 landed; PR 2 closed the PRAGMA-presence gap instead
// with the white-box TestOpenPRAGMAsReadBack (task 2.10).
//
// RED, watched (recorded, not committed): temporarily dropping "REFERENCES
// units(id) ON DELETE CASCADE" from unit_embeddings.unit_id in
// 0002_learning_and_search.sql made this test fail exactly as expected —
// "INSERT violating unit_embeddings.unit_id's FK = nil error, want a FOREIGN
// KEY constraint violation" — proving this test really depends on the
// migration declaring the FK, not on the compiled-in default alone (see
// TestForeignKeysExplicitlyDisabledAcceptsViolation's comment above for why
// "foreign_keys defaults to off" is not this driver's actual behavior, and
// how that changed this test's control). Reverted before committing.
func TestOpenForeignKeysRejectViolation(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	seed, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) [seed] = _, %v, want nil error", dbPath, err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed.Close() = %v, want nil error", err)
	}

	raw, err := sql.Open("sqlite3", pragmaDSN(dbPath))
	if err != nil {
		t.Fatalf("sql.Open(%q) = _, %v, want nil error", pragmaDSN(dbPath), err)
	}
	defer raw.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	var fk int
	if err := raw.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1 (sqlite.Open's own pragma set, reconstructed) — test setup invariant", fk)
	}

	_, err = raw.ExecContext(ctx, violatingUnitEmbeddingInsert)
	if err == nil {
		t.Fatal("INSERT violating unit_embeddings.unit_id's FK = nil error, want a FOREIGN KEY constraint violation")
	}
	if !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Errorf("INSERT violating unit_embeddings.unit_id's FK error = %v, want it to mention %q", err, "FOREIGN KEY constraint failed")
	}
}
