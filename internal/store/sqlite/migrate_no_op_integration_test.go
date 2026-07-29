//go:build integration

package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
)

// countingAuthorizer returns an authorizer callback that increments count
// every time SQLite's core reports an AUTH_TRANSACTION action whose
// operation starts with "BEGIN". The authorizer fires at PREPARE time for
// EVERY statement regardless of which Go-level API issued it (Exec,
// Prepare, or the driver's own db.BeginTx, which all funnel into
// sqlite3_exec/sqlite3_prepare at the C level) — unlike TRACE_STMT, which
// this driver only threads through statements tracked via Conn.Prepare, so
// it never fires for the sqlite3_exec path BeginTx and migrate's own
// ExecContext both use.
func countingAuthorizer(count *atomic.Int32) func(sqlite3.AuthorizerActionCode, string, string, string, string) sqlite3.AuthorizerReturnCode {
	return func(action sqlite3.AuthorizerActionCode, operation, _, _, _ string) sqlite3.AuthorizerReturnCode {
		if action == sqlite3.AUTH_TRANSACTION && strings.HasPrefix(strings.ToUpper(operation), "BEGIN") {
			count.Add(1)
		}
		return sqlite3.AUTH_OK
	}
}

// TestMigrateAtTargetOpensNoTransaction strengthens R3.5 (slice-3 review
// finding 5): TestMigrateIsIdempotent (test/integration/migrate_test.go)
// only ever asserted an OUTCOME — the second Open returns nil and
// user_version is unchanged. A regression that removed migrateMigrations's
// top-level `current == target` fast path but kept applyMigration's
// per-migration guard (`current >= m.Version` skips) would still pass that
// test, while every reopen of an already-migrated vault would needlessly
// acquire a BEGIN IMMEDIATE write lock it does not need. This test proves
// the MECHANISM instead: it counts every AUTH_TRANSACTION "BEGIN" SQLite's
// own authorizer callback reports on the connection, so it can distinguish
// "no transaction was opened" from "a transaction was opened and no-op'd".
//
// This file lives inside package sqlite (not test/integration/) so it can
// call the unexported migrate directly on a connection it built and
// instrumented itself — same rationale as txlock_integration_test.go and
// pragma_integration_test.go: Vault's exported surface (design D7) has no
// way to attach an authorizer to a pooled connection from outside the
// package.
func TestMigrateAtTargetOpensNoTransaction(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	var beginCount atomic.Int32
	authInit := func(c *sqlite3.Conn) error {
		if err := initConn(c); err != nil {
			return err
		}
		return c.SetAuthorizer(countingAuthorizer(&beginCount))
	}

	dsn, err := buildDSN(dbPath, pathStyleForGOOS())
	if err != nil {
		t.Fatalf("buildDSN(%q) = _, %v, want nil error", dbPath, err)
	}
	db, err := driver.Open(dsn, authInit)
	if err != nil {
		t.Fatalf("driver.Open(%q) = _, %v, want nil error", dsn, err)
	}
	defer db.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("db.PingContext() = %v, want nil error", err)
	}

	// First migrate: the vault is empty, so at least one transaction MUST
	// be opened to apply 0001_core_tables.sql. This is the control: if the
	// trace hook never observes a BEGIN here, it proves nothing below.
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate() [from scratch] = %v, want nil error", err)
	}
	if beginCount.Load() == 0 {
		t.Fatal("migrate() [from scratch] opened zero BEGIN-shaped statements, want at least one — the trace hook is not observing transactions, so the assertion below would prove nothing")
	}

	// Second migrate: the vault is already at target. R3.5's fast path
	// must return without opening ANY transaction at all.
	beginCount.Store(0)
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate() [already at target] = %v, want nil error", err)
	}
	if got := beginCount.Load(); got != 0 {
		t.Errorf("migrate() [already at target] opened %d BEGIN-shaped statement(s), want 0 — R3.5's current == target fast path must return before opening any transaction, not merely no-op inside one", got)
	}
}
