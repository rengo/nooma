package sqlite

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/ncruces/go-sqlite3/driver"
)

// SnapshotTo writes a transactionally consistent copy of the vault to dest
// with SQLite's own VACUUM INTO (docs/03-data-model.md §"Operational
// properties of the vault", ADR-0049).
//
// It is a read transaction on the source, so it runs while `nooma serve`
// holds the vault: WAL lets a reader and the writer proceed together, and the
// copy sees one committed state, never half of a write. Copying nooma.db with
// the filesystem would not — the latest commits may still live only in
// nooma.db-wal.
//
// dest must not exist: VACUUM INTO refuses to overwrite, and that refusal is
// kept rather than worked around, so a snapshot can never replace another.
//
// Like IntegrityCheck it reads no domain row through this surface: it asks
// SQLite to copy the FILE.
func (v *Vault) SnapshotTo(ctx context.Context, dest string) error {
	if _, err := v.db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("snapshotting %s into %s: %w", v.path, dest, err)
	}
	return nil
}

// VerifySnapshot opens the database file at path read-only, runs PRAGMA
// integrity_check, and returns its schema version (PRAGMA user_version).
//
// It is deliberately not Open: Open migrates and switches the file to WAL,
// and verifying a backup must neither change it nor leave a -wal or -shm
// beside it. VACUUM INTO writes a rollback-journal file, and a read-only open
// of one creates no side file
// (TestSnapshotTo_WritesAVerifiableCopyAtTheSameSchema).
func VerifySnapshot(ctx context.Context, path string) (int, error) {
	// A read-only open of a missing file fails anyway; checking first gives
	// the error a sentence instead of SQLite's "unable to open".
	if _, err := os.Stat(path); err != nil {
		return 0, fmt.Errorf("verifying snapshot %s: %w", path, err)
	}

	dsn, err := snapshotDSN(path)
	if err != nil {
		return 0, fmt.Errorf("verifying snapshot %s: %w", path, err)
	}
	db, err := driver.Open(dsn, initConn)
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
