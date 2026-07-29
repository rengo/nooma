//go:build integration

package integration

import (
	"context"
	"database/sql"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"

	"github.com/rengo/nooma/internal/store/sqlite"
	"github.com/rengo/nooma/test/support/schema"
)

// update regenerates both schema goldens instead of comparing against
// them. `make schema-golden` runs this test with -update (design §9.2).
var update = flag.Bool("update", false, "regenerate testdata/schema goldens instead of comparing against them")

const (
	structureGoldenPath = "../../testdata/schema/structure.golden"
	ddlGoldenPath       = "../../testdata/schema/ddl.golden"
)

// sqliteMasterRow is one row of a filtered, kind-classified sqlite_master
// dump — the shared input both goldens are projected from.
type sqliteMasterRow struct {
	Kind schema.Kind
	Name string
	SQL  string
}

// dumpSchema applies design §6.2/§6.3's shared exclusion rules to a raw
// sqlite_master dump: drop rows with no sql (auto-created objects like
// sqlite_autoindex_*), drop sqlite_-prefixed bookkeeping, and drop FTS5
// shadow tables.
//
// This function stays here, in the L3 test file, rather than moving to
// test/support/schema alongside schema.Classify/IsShadowTable: it needs a
// live *sql.DB connection, and test/support/schema is deliberately
// stdlib-only (design D1/§6.4) so the L2 gate that will eventually import
// it links no SQLite driver. The pure classification/normalization logic
// it calls (schema.Classify, schema.IsShadowTable) DOES live there, with
// its own direct table tests — moved out of this file per slice-3 review
// finding 3, which found that logic exercised only end-to-end through this
// one whole-pipeline comparison.
func dumpSchema(ctx context.Context, db *sql.DB) ([]sqliteMasterRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // read-only cursor, nothing to flush

	var all []sqliteMasterRow
	virtualTables := map[string]bool{}
	for rows.Next() {
		var name, createSQL string
		if err := rows.Scan(&name, &createSQL); err != nil {
			return nil, err
		}
		if strings.HasPrefix(name, "sqlite_") {
			continue
		}
		kind, ok := schema.Classify(createSQL)
		if !ok {
			return nil, &unclassifiedObjectError{name: name, sql: createSQL}
		}
		if kind == schema.KindVirtualTable {
			virtualTables[name] = true
		}
		all = append(all, sqliteMasterRow{Kind: kind, Name: name, SQL: createSQL})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	filtered := all[:0]
	for _, r := range all {
		// The shadow-table exclusion is restricted to Kind == table,
		// discovered while wiring 0002_learning_and_search.sql (design §6.2
		// never said this explicitly, because every worked example in that
		// section — units_fts_data, units_fts_idx, units_fts_docsize,
		// units_fts_config — happens to also be a table). schema.IsShadowTable
		// itself is a pure name-prefix check ("<vt>_<suffix>"); nothing in
		// its contract distinguishes kind. 0002 names its FTS5 sync triggers
		// units_fts_ai/units_fts_ad/units_fts_au (design §6.5's own
		// illustrative failure output lists them as real, comparable
		// objects), which collide with that exact prefix — without this
		// Kind guard, the real triggers R9.1 requires would be silently
		// dropped from both goldens, and the doc-03 gate (a later task)
		// would never see them to compare against docs/03-data-model.md.
		if r.Kind == schema.KindTable && schema.IsShadowTable(r.Name, virtualTables) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered, nil
}

type unclassifiedObjectError struct {
	name, sql string
}

func (e *unclassifiedObjectError) Error() string {
	return "sqlite_master object " + e.name + " has no recognized CREATE prefix: " + e.sql
}

// tableColumns reads PRAGMA table_info(name) — declared columns only, not
// table_xinfo, which would add FTS5's hidden columns (design §6.2). name
// always comes from sqlite_master.name, never caller input, so building the
// PRAGMA statement by concatenation carries no injection surface.
func tableColumns(ctx context.Context, db *sql.DB, name string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+name+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // read-only cursor, nothing to flush

	var cols []string
	for rows.Next() {
		var cid, notNull, pk int
		var colName, colType string
		var dflt any
		if err := rows.Scan(&cid, &colName, &colType, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, colName)
	}
	return cols, rows.Err()
}

// sortRows orders rows the same way schema.Sort orders Objects — by
// (kind rank, name) — reading the rank from schema.Rank, the single
// exported source of truth for the ordering (slice-3 review finding 7:
// this file used to keep its own independent copy of the rank table,
// which agreed with test/support/schema's today but had nothing keeping
// it that way once a future migration added a Kind only one of them knew
// about).
func sortRows(rows []sqliteMasterRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return schema.Rank(rows[i].Kind) < schema.Rank(rows[j].Kind)
		}
		return rows[i].Name < rows[j].Name
	})
}

// TestSchemaGolden asserts R4.1/R4.2/R4.4/R4.5: the schema golden is a
// structural + DDL dump of every object SQLite reports after applying
// every published migration to a fresh, empty vault (design §6.1), and
// that dump matches the two committed golden files. `make schema-golden`
// is this same test run with -update, which (re)writes them instead of
// comparing (design §9.2).
func TestSchemaGolden(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "vault.db")

	v, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	if err := v.Close(); err != nil {
		t.Fatalf("v.Close() = %v, want nil error", err)
	}

	// A raw connection is allowed here (design §7.2's recorded exception
	// for test/integration): it registers FTS5 itself, the same way
	// sqlite.Open does, so a schema containing a virtual table cannot
	// surprise the dump (design §6.1 step 2).
	raw, err := driver.Open("file:"+dbPath, func(c *sqlite3.Conn) error {
		return fts5.Register(c)
	})
	if err != nil {
		t.Fatalf("driver.Open(%q) = _, %v, want nil error", dbPath, err)
	}
	defer raw.Close() //nolint:errcheck // best-effort cleanup, the assertions below already ran

	var schemaVersion int
	if err := raw.QueryRowContext(ctx, "PRAGMA user_version").Scan(&schemaVersion); err != nil {
		t.Fatalf("PRAGMA user_version: %v", err)
	}

	rowsData, err := dumpSchema(ctx, raw)
	if err != nil {
		t.Fatalf("dumpSchema() = _, %v, want nil error", err)
	}
	if len(rowsData) == 0 {
		t.Fatal("dumpSchema() returned zero objects, want at least the core tables (D10's non-empty guard)")
	}
	sortRows(rowsData)

	objs := make([]schema.Object, 0, len(rowsData))
	ddlParts := make([]string, 0, len(rowsData))
	for _, r := range rowsData {
		obj := schema.Object{Kind: r.Kind, Name: r.Name}
		if r.Kind == schema.KindTable || r.Kind == schema.KindVirtualTable {
			cols, err := tableColumns(ctx, raw, r.Name)
			if err != nil {
				t.Fatalf("tableColumns(%q) = _, %v, want nil error", r.Name, err)
			}
			obj.Columns = cols
		}
		objs = append(objs, obj)
		ddlParts = append(ddlParts, schema.NormalizeDDL(r.SQL))
	}

	structureGolden := schema.Marshal(objs, schemaVersion)
	ddlGolden := []byte(strings.Join(ddlParts, "\n\n") + "\n")

	if *update {
		if err := os.WriteFile(structureGoldenPath, structureGolden, 0o644); err != nil {
			t.Fatalf("write %s: %v", structureGoldenPath, err)
		}
		if err := os.WriteFile(ddlGoldenPath, ddlGolden, 0o644); err != nil {
			t.Fatalf("write %s: %v", ddlGoldenPath, err)
		}
		return
	}

	wantStructure, err := os.ReadFile(structureGoldenPath)
	if err != nil {
		t.Fatalf("read %s: %v (run `make schema-golden` to generate it)", structureGoldenPath, err)
	}
	if string(structureGolden) != string(wantStructure) {
		t.Errorf("structure.golden mismatch — freshly generated:\n%s\nwant (committed):\n%s\nrun `make schema-golden` to regenerate", structureGolden, wantStructure)
	}

	wantDDL, err := os.ReadFile(ddlGoldenPath)
	if err != nil {
		t.Fatalf("read %s: %v (run `make schema-golden` to generate it)", ddlGoldenPath, err)
	}
	if string(ddlGolden) != string(wantDDL) {
		t.Errorf("ddl.golden mismatch — freshly generated:\n%s\nwant (committed):\n%s\nrun `make schema-golden` to regenerate", ddlGolden, wantDDL)
	}
}
