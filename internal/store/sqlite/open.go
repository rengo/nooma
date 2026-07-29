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

// Vault is an open SQLite database file, migrated to the schema this
// binary carries, with the operational PRAGMAs applied and FTS5 registered
// on every connection (design D1/D2/D3). It reads and writes no domain
// row: the handle below is unexported and this package exports no way to
// run arbitrary SQL against it. See docs/06-harness.md §1.
type Vault struct {
	db   *sql.DB
	path string
}

// Open opens dbPath, applies the operational PRAGMAs, registers FTS5 on
// every connection, and migrates the vault forward to the schema this
// binary carries (design D2/D3/D4, spec R2.3, R3.3-R3.6). Resolving where
// dbPath lives is the caller's job — this function accepts an absolute
// path and resolves nothing else.
//
// If the vault is newer than this binary knows how to migrate, Open
// returns a *VersionError having modified nothing.
func Open(ctx context.Context, dbPath string) (*Vault, error) {
	dsn, err := buildDSN(dbPath, pathStyleForGOOS())
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

	if err := migrate(ctx, db); err != nil {
		return nil, fmt.Errorf("open vault %q: migrate: %w", dbPath, errors.Join(err, db.Close()))
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
