package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/ncruces/go-sqlite3/driver"
)

// Snapshot writes a transactionally consistent copy of the database at
// dbPath to dest with SQLite's own VACUUM INTO (docs/03-data-model.md
// §"Operational properties of the vault", ADR-0049).
//
// The connection is read-only, never Open's: Open migrates, and a newer
// binary would change the live schema without the vault lock. Read-only, it
// cannot write the vault at all, so it runs beside serve without the lock;
// VACUUM INTO is one read transaction over one committed state. dest must not
// exist: VACUUM INTO refuses to overwrite, so no snapshot replaces another.
func Snapshot(ctx context.Context, dbPath, dest string) error {
	db, err := openReadOnly(dbPath)
	if err != nil {
		return fmt.Errorf("snapshotting %s: %w", dbPath, err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("snapshotting %s into %s: %w", dbPath, dest, err)
	}
	return nil
}

// VerifySnapshot opens the database file at path read-only, runs PRAGMA
// integrity_check, and returns its schema version (PRAGMA user_version).
//
// Not Open, which migrates and switches the file to WAL: verifying a backup
// must neither change it nor leave a -wal or -shm beside it.
func VerifySnapshot(ctx context.Context, path string) (int, error) {
	// A read-only open of a missing file fails anyway; checking first gives
	// the error a sentence instead of SQLite's "unable to open".
	if _, err := os.Stat(path); err != nil {
		return 0, fmt.Errorf("verifying snapshot %s: %w", path, err)
	}

	db, err := openReadOnly(path)
	if err != nil {
		return 0, fmt.Errorf("verifying snapshot %s: %w", path, err)
	}
	defer func() { _ = db.Close() }()

	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return 0, fmt.Errorf("verifying snapshot %s: integrity_check: %w", path, err)
	}
	if result != "ok" {
		return 0, fmt.Errorf("verifying snapshot %s: integrity_check reported: %s", path, result)
	}

	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("verifying snapshot %s: reading user_version: %w", path, err)
	}
	return version, nil
}

// openReadOnly opens the database file at path with snapshotDSN, FTS5
// registered as on every connection.
func openReadOnly(path string) (*sql.DB, error) {
	dsn, err := snapshotDSN(path)
	if err != nil {
		return nil, err
	}
	return driver.Open(dsn, initConn)
}

// snapshotDSN is buildDSN's read-only sibling: the same absolute-path rule
// and the same URI path handling, with mode=ro instead of the writer's
// PRAGMAs.
func snapshotDSN(path string) (string, error) {
	style := pathStyleForGOOS()
	if !isAbsoluteVaultPath(path, style) {
		return "", errors.Join(ErrRelativeDBPath, fmt.Errorf("%q", path))
	}
	q := url.Values{}
	q.Set("mode", "ro")
	u := url.URL{Scheme: "file", RawQuery: q.Encode()}
	setURIPath(&u, path, style)
	return u.String(), nil
}
