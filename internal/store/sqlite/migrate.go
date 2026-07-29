package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

// migrationFS embeds every published migration file. No migration is ever
// read from the filesystem at runtime (spec R3.1) — this is the only source
// parseMigrations reads from in production.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// migration is one parsed, validated migration file.
type migration struct {
	Version int
	Name    string
	SQL     string
}

// migrationNamePattern matches NNNN_snake_case.sql: a zero-padded four-digit
// version, an underscore, a lowercase-snake-case description, and the .sql
// extension (design §5.1).
var migrationNamePattern = regexp.MustCompile(`^([0-9]{4})_[a-z0-9]+(?:_[a-z0-9]+)*\.sql$`)

// transactionControlPattern matches COMMIT or ROLLBACK used anywhere, and
// BEGIN used as a transaction verb (a bare "BEGIN;" or "BEGIN" followed by
// TRANSACTION/DEFERRED/IMMEDIATE/EXCLUSIVE) — the runner owns the
// transaction (design §5.1). It deliberately does NOT match the BEGIN that
// opens a CREATE TRIGGER body, which is followed by an ordinary statement,
// not by ";" or one of those four words.
var transactionControlPattern = regexp.MustCompile(
	`(?is)\b(COMMIT|ROLLBACK)\b|\bBEGIN\s*(;|TRANSACTION\b|DEFERRED\b|IMMEDIATE\b|EXCLUSIVE\b)`,
)

// userVersionPattern matches PRAGMA user_version — the runner owns the
// version (design §5.1).
var userVersionPattern = regexp.MustCompile(`(?is)PRAGMA\s+user_version\b`)

// validateMigrationSQL rejects the constructs design §5.1 reserves for the
// runner itself: COMMIT, ROLLBACK, BEGIN used as a transaction verb (the
// BEGIN...END pair inside a CREATE TRIGGER body is legal and must pass),
// and PRAGMA user_version.
func validateMigrationSQL(name, sql string) error {
	if transactionControlPattern.MatchString(sql) {
		return fmt.Errorf("migration %q must not manage its own transaction (COMMIT/ROLLBACK/BEGIN <mode> is reserved for the runner)", name)
	}
	if userVersionPattern.MatchString(sql) {
		return fmt.Errorf("migration %q must not set PRAGMA user_version (reserved for the runner)", name)
	}
	return nil
}

// parseMigrations reads every migration file in fsys's "migrations"
// directory, validates its name and contents, and returns them sorted
// ascending by version. It rejects a name that does not match
// NNNN_snake_case.sql, a version below 1, a duplicate version, and a gap in
// the version sequence (design §5.1).
func parseMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	seen := make(map[int]string, len(entries))
	for _, entry := range entries {
		name := entry.Name()

		m := migrationNamePattern.FindStringSubmatch(name)
		if m == nil {
			return nil, fmt.Errorf("migration %q does not match NNNN_snake_case.sql", name)
		}

		version, err := strconv.Atoi(m[1])
		if err != nil || version < 1 {
			return nil, fmt.Errorf("migration %q has an invalid version", name)
		}

		if prior, ok := seen[version]; ok {
			return nil, fmt.Errorf("migration version %d is declared twice: %q and %q", version, prior, name)
		}
		seen[version] = name

		content, err := fs.ReadFile(fsys, "migrations/"+name)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}

		if err := validateMigrationSQL(name, string(content)); err != nil {
			return nil, err
		}

		migrations = append(migrations, migration{Version: version, Name: name, SQL: string(content)})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })

	for i, m := range migrations {
		wantVersion := i + 1
		if m.Version != wantVersion {
			return nil, fmt.Errorf("migration versions have a gap: expected %d, found %d (%q)", wantVersion, m.Version, m.Name)
		}
	}

	return migrations, nil
}

// readUserVersion reads PRAGMA user_version from q, which may be either a
// *sql.DB (outside any transaction) or a *sql.Tx (re-read inside one, D4's
// race guard).
func readUserVersion(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}) (int, error) {
	var version int
	if err := q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	return version, nil
}

// migrate applies every pending embedded migration to db, following design
// §5.2's state matrix (D4). It is a thin wrapper over migrateMigrations
// (below) that supplies the real embedded migration set — split out so a
// test can drive the algorithm with a synthetic, truncated set instead
// (slice-3 review finding 2).
func migrate(ctx context.Context, db *sql.DB) error {
	migrations, err := parseMigrations(migrationFS)
	if err != nil {
		return fmt.Errorf("parse embedded migrations: %w", err)
	}
	return migrateMigrations(ctx, db, migrations)
}

// migrateMigrations runs design §5.2's state matrix against an
// already-parsed, already-validated migration set:
//
//   - current > target: the vault is newer than this binary knows how to
//     migrate. Returns *VersionError having opened no transaction and
//     modified nothing (spec R3.6).
//   - current == target: no transaction is opened at all (spec R3.5).
//   - current < target: one BEGIN IMMEDIATE transaction per pending
//     migration, re-reading user_version INSIDE the transaction before
//     applying (design D4) — this is what makes two racing processes safe
//     without the single-writer lockfile, which does not exist yet (that
//     is M0's job).
//
// Split out from migrate so a test can drive it directly with a synthetic,
// truncated migration set — simulating an older binary (a smaller target)
// racing a newer one (a larger target) against the same vault, without
// needing two real compiled binaries (design §5.3's "ahead of the binary"
// row; spec R3.6; slice-3 review finding 2).
func migrateMigrations(ctx context.Context, db *sql.DB, migrations []migration) error {
	target := 0
	for _, m := range migrations {
		if m.Version > target {
			target = m.Version
		}
	}

	current, err := readUserVersion(ctx, db)
	if err != nil {
		return err
	}

	if current > target {
		return &VersionError{VaultVersion: current, BinaryVersion: target}
	}
	if current == target {
		return nil
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if err := applyMigration(ctx, db, m, target); err != nil {
			return err
		}
	}

	return nil
}

// applyMigration runs one migration inside its own BEGIN IMMEDIATE
// transaction, re-reading user_version inside it before applying (design
// D4's race guard): two processes racing to migrate the same vault before
// the single-writer lockfile exists both read current == target-1 outside
// any transaction, but only the winner of BEGIN IMMEDIATE's write lock sees
// its own commit — the loser re-reads user_version after acquiring the
// lock, observes the winner's work, and skips.
//
// target is this binary's OWN highest embedded migration version — NOT
// necessarily the vault's final destination. It is re-checked here, inside
// the transaction, because the outer current > target fast path in
// migrateMigrations only catches a vault that was ALREADY ahead at the
// moment it read current; it cannot catch a vault that races ahead WHILE
// this binary is waiting for the write lock (slice-3 review finding 2,
// confirmed 3/3 by the reviewer: an old binary with target=1 and a new
// binary with target=2 open the same fresh vault; the old one reads
// current=0 and decides to apply migration 1; the new one wins the lock
// first and applies BOTH 1 and 2, leaving user_version=2; the old one then
// enters this function, re-reads current=2 inside its own transaction, and
// — with only the old `current >= m.Version` guard — evaluated 2 >= 1,
// concluded "already applied, skip", and returned nil, handing back a
// working *Vault over a schema this binary has never seen). The fix is the
// check immediately below: current > target is evaluated FIRST, before the
// "already applied this one" guard, so racing past this binary's own
// target is always caught, no matter which migration was in flight when it
// happened.
func applyMigration(ctx context.Context, db *sql.DB, m migration, target int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("apply migration %q: begin: %w", m.Name, err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit; the error path below already reports the real failure

	current, err := readUserVersion(ctx, tx)
	if err != nil {
		return fmt.Errorf("apply migration %q: %w", m.Name, err)
	}
	if current > target {
		// Another process — necessarily a newer binary with a longer
		// migration set — won the write lock while this one waited, and
		// carried the vault past this binary's own target. Forward-only
		// means refusing here, not silently skipping (spec R3.6).
		return &VersionError{VaultVersion: current, BinaryVersion: target}
	}
	if current >= m.Version {
		// Another process already applied this migration — and nothing
		// past this binary's own target — while this one waited for the
		// write lock. Nothing to do.
		return nil
	}
	if current != m.Version-1 {
		// Unreachable by design (parseMigrations guarantees a contiguous,
		// gap-free version sequence, and this loop applies migrations in
		// ascending order), checked anyway because the alternative is a
		// vault silently missing a migration.
		return fmt.Errorf("apply migration %q: expected vault at version %d, found %d", m.Name, m.Version-1, current)
	}

	// ExecContext must be called with zero arguments: with arguments the
	// driver falls back to Prepare, which rejects a trailing statement, and
	// a migration file is many statements (design §5.2, ground truth
	// verified in driver.go).
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("apply migration %q: %w", m.Name, err)
	}
	// PRAGMA user_version cannot be parameterized; m.Version is parsed from
	// an embedded file name, not caller input, so there is no injection
	// surface (design §5.2).
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.Version)); err != nil {
		return fmt.Errorf("apply migration %q: set user_version: %w", m.Name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("apply migration %q: commit: %w", m.Name, err)
	}
	return nil
}
