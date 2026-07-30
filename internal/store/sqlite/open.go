package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

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

	db, err := openFirstConnection(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open vault %q: %w", dbPath, err)
	}

	if err := migrate(ctx, db); err != nil {
		return nil, fmt.Errorf("open vault %q: migrate: %w", dbPath, errors.Join(err, db.Close()))
	}

	return &Vault{db: db, path: dbPath}, nil
}

// firstConnectionBusyRetries bounds how many times openFirstConnection
// retries a first connection whose only failure was a transient
// SQLITE_BUSY (slice-3 review finding 1).
//
// Why this exists, measured rather than assumed: converting a brand-new
// database file to WAL mode for the first time (buildDSN's
// journal_mode(wal) PRAGMA, design D3) requires SQLite to hold the file
// exclusively for that one-time conversion. Unlike an ordinary page-lock
// wait, this exclusivity check is NOT retried through the connection's own
// busy_timeout — there is no open connection yet for a busy handler to
// apply to — so it fails SQLITE_BUSY almost instantly, far below any
// busy_timeout budget, whenever two processes' first connections race to
// convert the same fresh file (measured: 8 concurrent sqlite.Open calls on
// a vault that does not exist yet fail this way 60-80% of the time before
// this fix). The fix retries the connection's ENTIRE opening sequence
// (never just the failed PRAGMA — a connection that failed sqlite3_open's
// PRAGMA batch is closed and unusable) a bounded number of times with a
// short backoff, and ONLY for this specific transient condition: a
// genuinely corrupt vault file fails with a different SQLite error code
// entirely (CORRUPT/NOTADB, never BUSY) and is never retried — it still
// fails immediately and loudly (TestOpenCorruptVaultNamesThePath).
//
// 20 attempts * up to ~50ms backoff comfortably stays under busy_timeout's
// own 5s (design D3): if the condition somehow never clears in that
// window, something is genuinely wrong and the loop must give up rather
// than hide it.
const firstConnectionBusyRetries = 20

// firstConnectionBusyBackoff returns the delay before retry number attempt
// (0-indexed): a short, linearly increasing backoff capped at 50ms, so the
// total budget across all firstConnectionBusyRetries attempts stays well
// under busy_timeout's 5s.
func firstConnectionBusyBackoff(attempt int) time.Duration {
	const step = 5 * time.Millisecond
	const maxBackoff = 50 * time.Millisecond
	d := time.Duration(attempt+1) * step
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// openFirstConnection opens dsn and pings it, retrying a bounded number of
// times when the failure is transient SQLITE_BUSY (see
// firstConnectionBusyRetries's doc comment for why). Each attempt is a
// fresh *sql.DB: a connection whose opening sequence failed is closed by
// the driver and cannot be reused.
func openFirstConnection(ctx context.Context, dsn string) (*sql.DB, error) {
	var lastErr error
	for attempt := 0; attempt < firstConnectionBusyRetries; attempt++ {
		db, err := driver.Open(dsn, initConn)
		if err != nil {
			return nil, err
		}

		pingErr := db.PingContext(ctx)
		if pingErr == nil {
			return db, nil
		}
		closeErr := db.Close()
		lastErr = errors.Join(pingErr, closeErr)

		if !isTransientBusy(pingErr) {
			return nil, fmt.Errorf("ping: %w", lastErr)
		}

		if attempt == firstConnectionBusyRetries-1 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("ping: %w", errors.Join(ctx.Err(), lastErr))
		case <-time.After(firstConnectionBusyBackoff(attempt)):
		}
	}

	// This is the opacity this project already flagged once on slice 2
	// ("fails late and far from the cause") recurring through a different
	// door: the wrapped error chain still says "invalid _pragma" three
	// layers down, but the sentence a human reads first must say what
	// actually happened.
	return nil, fmt.Errorf(
		"ping: gave up after %d attempts waiting for exclusive access to convert a brand-new vault file to WAL mode (another process was racing the same first-time conversion): %w",
		firstConnectionBusyRetries, lastErr,
	)
}

// sqliteErrorCoder is implemented by both *sqlite3.Error (returned when the
// driver has extra context — a message, a syntax-error offset, a wrapped
// OS error) and sqlite3.ExtendedErrorCode (returned instead when it does
// not, which is exactly what a bare SQLITE_BUSY on a fresh connection's
// PRAGMA batch produces — verified against errorFor's own source: it falls
// back to a bare error-code value whenever the driver has nothing else to
// add, and matching against *sqlite3.Error alone silently misses this
// case). Declared as an interface rather than checked type-by-type so this
// keeps working if a code path starts wrapping a *sqlite3.Error instead of
// a bare code, or vice versa.
type sqliteErrorCoder interface {
	Code() sqlite3.ErrorCode
}

// isTransientBusy reports whether err is SQLite's transient "database is
// locked" condition (SQLITE_BUSY) — as opposed to a genuinely corrupt vault
// file, which fails with an entirely different SQLite error code (CORRUPT
// or NOTADB) and must never be retried, only surfaced immediately.
func isTransientBusy(err error) bool {
	var coder sqliteErrorCoder
	if !errors.As(err, &coder) {
		return false
	}
	return coder.Code() == sqlite3.BUSY
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

// SchemaVersion reports the vault's PRAGMA user_version — the migration level
// the file is at.
//
// It is a pragma, not a domain row, so it stays inside the scope boundary
// testdata/schema/store_api.golden guards: this widens the store's surface with a
// way to ask how the FILE is versioned, not with a way to read what is in it.
func (v *Vault) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := v.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("reading user_version from %s: %w", v.path, err)
	}
	return version, nil
}

// IntegrityCheck runs SQLite's own PRAGMA integrity_check against the vault.
//
// Named explicitly rather than folded into Check, which already exists here and
// means something unrelated — an FTS5-registration probe. Two methods called
// Check and IntegrityCheck are clearer than one Check that grew a second job.
//
// Like SchemaVersion this stays inside the scope boundary: it asks SQLite whether
// the FILE is coherent, and reads no domain row.
func (v *Vault) IntegrityCheck(ctx context.Context) error {
	var result string
	if err := v.db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("running integrity_check on %s: %w", v.path, err)
	}
	if result != "ok" {
		return fmt.Errorf("integrity_check on %s reported: %s", v.path, result)
	}
	return nil
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
