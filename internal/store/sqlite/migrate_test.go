package sqlite

import (
	"strings"
	"testing"
	"testing/fstest"
)

// TestParseMigrationsRealEmbeddedSet is D10's non-empty-corpus guard applied
// to the real embedded migration set: it asserts parseMigrations finds
// exactly versions 1..3 (0001_core_tables.sql, 0002_learning_and_search.sql,
// 0003_current_state_source.sql — R3.8, design §5.1's own stated
// expectation) with non-empty SQL, before any other test in this file
// trusts synthetic inputs to mean anything.
func TestParseMigrationsRealEmbeddedSet(t *testing.T) {
	migrations, err := parseMigrations(migrationFS)
	if err != nil {
		t.Fatalf("parseMigrations(migrationFS) = _, %v, want nil error", err)
	}
	if len(migrations) != 3 {
		t.Fatalf("parseMigrations(migrationFS) returned %d migrations, want exactly 3 (R3.8: 0001_core_tables.sql, 0002_learning_and_search.sql, 0003_current_state_source.sql)", len(migrations))
	}
	for _, m := range migrations {
		if strings.TrimSpace(m.SQL) == "" {
			t.Errorf("migration %q has empty SQL", m.Name)
		}
	}
	if migrations[0].Version != 1 {
		t.Errorf("migrations[0].Version = %d, want 1", migrations[0].Version)
	}
	if migrations[1].Version != 2 {
		t.Errorf("migrations[1].Version = %d, want 2", migrations[1].Version)
	}
	if migrations[2].Version != 3 {
		t.Errorf("migrations[2].Version = %d, want 3", migrations[2].Version)
	}
}

// TestParseMigrationsNamingAndOrdering is a table-driven test over synthetic
// migration sets (design §5.1): a gap, a duplicate version, a version < 1,
// and a name that does not match NNNN_snake_case.sql are all rejected;
// a well-formed, non-contiguous-in-directory-listing-order set still comes
// back sorted ascending by version.
func TestParseMigrationsNamingAndOrdering(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		wantErr bool
	}{
		{
			name: "well-formed contiguous set",
			files: map[string]string{
				"migrations/0002_second.sql": "CREATE TABLE b (id TEXT);",
				"migrations/0001_first.sql":  "CREATE TABLE a (id TEXT);",
			},
			wantErr: false,
		},
		{
			name: "gap between versions",
			files: map[string]string{
				"migrations/0001_first.sql": "CREATE TABLE a (id TEXT);",
				"migrations/0003_third.sql": "CREATE TABLE c (id TEXT);",
			},
			wantErr: true,
		},
		{
			name: "duplicate version",
			files: map[string]string{
				"migrations/0001_first.sql": "CREATE TABLE a (id TEXT);",
				"migrations/0001_again.sql": "CREATE TABLE a2 (id TEXT);",
			},
			wantErr: true,
		},
		{
			name: "version below 1",
			files: map[string]string{
				"migrations/0000_zero.sql": "CREATE TABLE z (id TEXT);",
			},
			wantErr: true,
		},
		{
			name: "name does not match the pattern",
			files: map[string]string{
				"migrations/first.sql": "CREATE TABLE a (id TEXT);",
			},
			wantErr: true,
		},
		{
			name: "uppercase in the description segment is rejected",
			files: map[string]string{
				"migrations/0001_First.sql": "CREATE TABLE a (id TEXT);",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for path, content := range tt.files {
				fsys[path] = &fstest.MapFile{Data: []byte(content)}
			}

			migrations, err := parseMigrations(fsys)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseMigrations(...) = %v, nil, want a non-nil error", migrations)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMigrations(...) = _, %v, want nil error", err)
			}
			for i := 1; i < len(migrations); i++ {
				if migrations[i-1].Version >= migrations[i].Version {
					t.Errorf("migrations not sorted ascending by version: %d before %d", migrations[i-1].Version, migrations[i].Version)
				}
			}
		})
	}
}

// TestValidateMigrationSQLOwnershipRules is a table-driven test over
// synthetic SQL bodies (design §5.1): the runner owns the transaction and
// the version, so a migration file that tries to COMMIT, ROLLBACK, start
// its own transaction, or write PRAGMA user_version is rejected — except
// the BEGIN ... END pair inside a CREATE TRIGGER body, which is ordinary
// trigger syntax and must pass.
func TestValidateMigrationSQLOwnershipRules(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{
			name:    "plain DDL",
			sql:     "CREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: false,
		},
		{
			name:    "trigger BEGIN...END body is legal",
			sql:     "CREATE TRIGGER units_fts_ai AFTER INSERT ON units BEGIN\n  INSERT INTO units_fts(rowid, content) VALUES (new.rowid, new.content);\nEND;",
			wantErr: false,
		},
		{
			name:    "bare COMMIT",
			sql:     "CREATE TABLE units (id TEXT PRIMARY KEY);\nCOMMIT;",
			wantErr: true,
		},
		{
			name:    "bare ROLLBACK",
			sql:     "CREATE TABLE units (id TEXT PRIMARY KEY);\nROLLBACK;",
			wantErr: true,
		},
		{
			name:    "BEGIN as a bare transaction verb",
			sql:     "BEGIN;\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: true,
		},
		{
			name:    "BEGIN TRANSACTION",
			sql:     "BEGIN TRANSACTION;\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: true,
		},
		{
			name:    "BEGIN IMMEDIATE",
			sql:     "BEGIN IMMEDIATE;\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: true,
		},
		{
			name:    "BEGIN DEFERRED",
			sql:     "BEGIN DEFERRED;\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: true,
		},
		{
			name:    "BEGIN EXCLUSIVE",
			sql:     "BEGIN EXCLUSIVE;\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: true,
		},
		{
			name:    "PRAGMA user_version",
			sql:     "PRAGMA user_version = 3;",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMigrationSQL("0001_test.sql", tt.sql)
			if tt.wantErr && err == nil {
				t.Fatal("validateMigrationSQL(...) = nil, want a non-nil error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateMigrationSQL(...) = %v, want nil error", err)
			}
		})
	}
}

// TestValidateMigrationSQLIgnoresComments is slice-3 review finding 4:
// validateMigrationSQL scanned raw SQL text with no comment stripping, so a
// migration whose PROSE happened to mention a reserved word failed
// validation for a reason that has nothing to do with what the migration
// actually does. This blocks slice 4's 0002_learning_and_search, whose FTS5
// synchronization triggers need comments that discuss transactions in
// plain English.
//
// Measured on the code before this fix, verbatim:
//
//	comment containing COMMIT        -> err=migration "0002_x.sql" must not manage its own transaction
//	comment containing BEGIN         -> err=migration "0002_x.sql" must not manage its own transaction
//	comment containing user_version  -> err=migration "0002_x.sql" must not set PRAGMA user_version
//	clean control                    -> err=<nil>
func TestValidateMigrationSQLIgnoresComments(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{
			name:    "line comment mentioning COMMIT is not a violation",
			sql:     "-- This trigger must COMMIT cleanly once SQLite finishes it.\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: false,
		},
		{
			name:    "line comment mentioning BEGIN is not a violation",
			sql:     "-- BEGIN a mental model: this table starts the learning pipeline.\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: false,
		},
		{
			name:    "line comment mentioning PRAGMA user_version is not a violation",
			sql:     "-- The runner bumps PRAGMA user_version after this file applies.\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: false,
		},
		{
			name:    "block comment mentioning COMMIT is not a violation",
			sql:     "/* Notes: this migration's DDL is committed as one unit by the runner. */\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: false,
		},
		{
			name:    "block comment spanning multiple lines mentioning BEGIN/ROLLBACK",
			sql:     "/*\n * BEGIN reading here: a failed statement causes an implicit ROLLBACK.\n */\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: false,
		},
		{
			name:    "a real transaction-control statement is still rejected even inside a migration with an unrelated comment",
			sql:     "-- plain DDL below\nCREATE TABLE units (id TEXT PRIMARY KEY);\nCOMMIT;",
			wantErr: true,
		},
		{
			name:    "a real PRAGMA user_version statement is still rejected",
			sql:     "-- version bookkeeping is the runner's job\nPRAGMA user_version = 3;",
			wantErr: true,
		},
		{
			name:    "a real BEGIN transaction verb is still rejected even with an unrelated line comment above it",
			sql:     "-- start of file\nBEGIN;\nCREATE TABLE units (id TEXT PRIMARY KEY);",
			wantErr: true,
		},
		{
			name:    "the trigger BEGIN...END carve-out still passes with a comment mentioning COMMIT nearby",
			sql:     "-- fires after insert, must not need its own COMMIT\nCREATE TRIGGER units_fts_ai AFTER INSERT ON units BEGIN\n  INSERT INTO units_fts(rowid, content) VALUES (new.rowid, new.content);\nEND;",
			wantErr: false,
		},
		{
			name:    "a string literal containing '--' is data, not a comment, and must not hide a real violation",
			sql:     "CREATE TABLE units (id TEXT PRIMARY KEY, note TEXT DEFAULT '-- not a comment');\nCOMMIT;",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMigrationSQL("0002_x.sql", tt.sql)
			if tt.wantErr && err == nil {
				t.Fatal("validateMigrationSQL(...) = nil, want a non-nil error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateMigrationSQL(...) = %v, want nil error", err)
			}
		})
	}
}
