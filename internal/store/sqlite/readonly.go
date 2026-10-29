package sqlite

import (
	"context"
	"fmt"
	"os"
)

// OpenReadOnly opens the database file at dbPath read-only (mode=ro): it
// never migrates, whatever version the file is at, and SQLite itself refuses
// every write through it. It is how nooma doctor looks at a vault without
// changing it (I55), beside a running serve and without the vault lock.
//
// The Vault it returns is the same type Open returns, so every read the store
// offers works over it, but unlike Open's it may be behind this binary's
// schema or ahead of it: SchemaVersion says which, against SchemaTarget. A
// method that writes fails at SQLite ("attempt to write a readonly database").
func OpenReadOnly(ctx context.Context, dbPath string) (*Vault, error) {
	// A read-only open of a missing file fails anyway; checking first gives
	// the error a sentence instead of SQLite's "unable to open".
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("open vault %q read-only: %w", dbPath, err)
	}
	db, err := openReadOnly(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open vault %q read-only: %w", dbPath, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open vault %q read-only: %w", dbPath, err)
	}
	return &Vault{db: db, path: dbPath}, nil
}

// SchemaTarget is the schema version this binary migrates a vault to: the
// highest embedded migration.
func SchemaTarget() (int, error) {
	migrations, err := parseMigrations(migrationFS)
	if err != nil {
		return 0, fmt.Errorf("parse embedded migrations: %w", err)
	}
	target := 0
	for _, m := range migrations {
		target = max(target, m.Version)
	}
	return target, nil
}

// SearchIndexGaps is where the word index (units_fts) and units disagree.
// The index must mirror units row for row, every status included
// (docs/03-data-model.md §"Search"): Missing counts units it holds no entry
// for, which a word search cannot find, and Orphaned counts entries naming no
// unit, which can resolve to the wrong one.
type SearchIndexGaps struct {
	Missing  int
	Orphaned int
}

// SearchIndexGaps compares units' rowids with the word index's own per-row
// table, units_fts_docsize, in both directions. A count over units_fts itself
// would prove nothing: an external-content FTS5 table reads its rows from
// units. FTS5's own 'integrity-check' is a write command and refuses a
// read-only vault, so this is what doctor can ask without changing anything.
// It does not see an entry indexed with a unit's old text.
func (v *Vault) SearchIndexGaps(ctx context.Context) (SearchIndexGaps, error) {
	var g SearchIndexGaps
	if err := v.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM units u
LEFT JOIN units_fts_docsize d ON d.id = u.rowid
WHERE d.id IS NULL`).Scan(&g.Missing); err != nil {
		return SearchIndexGaps{}, fmt.Errorf("counting units missing from the word index of %s: %w", v.path, err)
	}
	if err := v.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM units_fts_docsize d
LEFT JOIN units u ON u.rowid = d.id
WHERE u.rowid IS NULL`).Scan(&g.Orphaned); err != nil {
		return SearchIndexGaps{}, fmt.Errorf("counting word index entries for no unit in %s: %w", v.path, err)
	}
	return g, nil
}
