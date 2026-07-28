package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
)

// Vault is an open SQLite database file with the operational PRAGMAs
// applied and FTS5 registered on every connection (design D2/D3). It reads
// and writes no domain row: the handle below is unexported and this
// package exports no way to run arbitrary SQL against it. See
// docs/06-harness.md §1.
//
// Vault does not yet guarantee the file is migrated to the schema this
// binary carries — the migration runner lands in PR 3 of this same change
// (openspec/changes/complete-harness). Until then, Open only opens the
// connection pool and pings it.
type Vault struct {
	db   *sql.DB
	path string
}

// Open opens dbPath, applies the operational PRAGMAs, and registers FTS5
// on every connection (design D2/D3, spec R2.3). Resolving where dbPath
// lives is the caller's job — this function accepts an absolute path and
// resolves nothing else.
//
// Open does not yet migrate the vault forward: that is PR 3 of this same
// change (openspec/changes/complete-harness) — the next reader extending
// this function with migration logic should not assume it already runs.
func Open(ctx context.Context, dbPath string) (*Vault, error) {
	dsn, err := buildDSN(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open vault %q: %w", dbPath, err)
	}

	db, err := driver.Open(dsn, initConn)
	if err != nil {
		return nil, fmt.Errorf("open vault %q: %w", dbPath, err)
	}

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open vault %q: ping: %w", dbPath, errors.Join(err, db.Close()))
	}

	return &Vault{db: db, path: dbPath}, nil
}

// initConn runs on every connection the pool creates (verified against the
// driver source, design §1/§4.2). A non-nil return fails that connection's
// open, so a connection without FTS5 registered is unrepresentable.
func initConn(c *sqlite3.Conn) error {
	return fts5.Register(c)
}

// Close closes the vault's connection pool.
func (v *Vault) Close() error {
	return v.db.Close()
}

// Path returns the filesystem path this vault was opened from.
func (v *Vault) Path() string {
	return v.path
}

// Stats reports the connection pool counters (design D2's deferred
// reader/writer split trigger: revisit when this shows more open
// connections than expected).
func (v *Vault) Stats() sql.DBStats {
	return v.db.Stats()
}

// Check runs a probe that exercises the same failure mode a missing FTS5
// registration would hit, on a connection taken from the pool (design
// §4.3). temp. keeps it out of the vault entirely and it touches no domain
// row.
func (v *Vault) Check(ctx context.Context) error {
	_, err := v.db.ExecContext(ctx,
		`CREATE VIRTUAL TABLE temp.nooma_fts5_probe USING fts5(c);
DROP TABLE temp.nooma_fts5_probe;`)
	return err
}
