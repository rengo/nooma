// Package sqlitetest is test support for the SQLite store: fixtures that need a
// row no port can write, kept next to the schema so the column list stays inside
// internal/store (the sqlite-containment depguard rule exempts this path).
//
// It is imported by tests only. No production code may import it: ports.StateRepo
// declares no energy writer on purpose (the load watcher is the only writer of
// energy outside a user's own report), and this package must not become one.
package sqlitetest

import (
	"context"
	"database/sql"
	"net/url"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver" // registers the "sqlite3" driver this package opens

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/store/sqlite"
)

// SeedEnergy appends one user-sourced current_state row carrying energy,
// recorded at the given instant, to the vault v. It opens its own short-lived
// handle on v's file with the operational pragmas the vault applies that
// matter to a second writer (busy_timeout, foreign_keys); WAL is a property of
// the file and is already in force once the vault is open.
func SeedEnergy(t testing.TB, v *sqlite.Vault, id string, at time.Time, energy float64) {
	t.Helper()
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(on)")
	q.Set("_txlock", "immediate")
	dsn := "file:" + v.Path() + "?" + q.Encode()

	raw, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("sqlitetest.SeedEnergy: open: %v", err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.ExecContext(context.Background(),
		`INSERT INTO current_state (id, energy, mood, active, recorded_at, source) VALUES (?, ?, '', 0, ?, ?)`,
		id, energy, at.UTC().Format(time.RFC3339), ports.StateSourceUser); err != nil {
		t.Fatalf("sqlitetest.SeedEnergy: insert: %v", err)
	}
}
