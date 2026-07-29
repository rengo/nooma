//go:build integration

package integration

import (
	"context"
	"database/sql"
	"flag"
	"os"
	"path/filepath"
	"regexp"
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

var createPrefixPattern = regexp.MustCompile(`(?is)^CREATE\s+(?:TEMP\s+|TEMPORARY\s+)?(VIRTUAL\s+TABLE|UNIQUE\s+INDEX|TABLE|INDEX|TRIGGER|VIEW)\b`)

// classify reads the Kind off the normalized DDL prefix (design §6.2), not
// off sqlite_master.type alone — that column cannot distinguish a virtual
// table from an ordinary one, or a unique index from a plain one.
func classify(createSQL string) (schema.Kind, bool) {
	m := createPrefixPattern.FindStringSubmatch(createSQL)
	if m == nil {
		return "", false
	}
	switch strings.ToUpper(strings.Join(strings.Fields(m[1]), " ")) {
	case "TABLE":
		return schema.KindTable, true
	case "VIRTUAL TABLE":
		return schema.KindVirtualTable, true
	case "INDEX":
		return schema.KindIndex, true
	case "UNIQUE INDEX":
		return schema.KindUniqueIndex, true
	case "TRIGGER":
		return schema.KindTrigger, true
	case "VIEW":
		return schema.KindView, true
	default:
		return "", false
	}
}

// isShadowTable reports whether name is an FTS5 shadow table of one of the
// virtual tables in vts: "<vt>_data", "<vt>_idx", "<vt>_docsize",
// "<vt>_config" and similar "<vt>_<suffix>" forms (design §6.2).
func isShadowTable(name string, vts map[string]bool) bool {
	for vt := range vts {
		if strings.HasPrefix(name, vt+"_") {
			return true
		}
	}
	return false
}

// dumpSchema applies design §6.2/§6.3's shared exclusion rules to a raw
// sqlite_master dump: drop rows with no sql (auto-created objects like
// sqlite_autoindex_*), drop sqlite_-prefixed bookkeeping, and drop FTS5
// shadow tables.
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
		kind, ok := classify(createSQL)
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
		if isShadowTable(r.Name, virtualTables) {
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

// normalizeDDL implements ddl.golden's per-statement normalization (design
// §6.3): trim trailing whitespace per line, trim the whole statement, and
// end it with a single ";".
func normalizeDDL(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + ";"
}

var kindRankOrder = []schema.Kind{
	schema.KindTable, schema.KindVirtualTable, schema.KindIndex,
	schema.KindUniqueIndex, schema.KindTrigger, schema.KindView,
}

func kindRank(k schema.Kind) int {
	for i, want := range kindRankOrder {
		if k == want {
			return i
		}
	}
	return len(kindRankOrder)
}

func sortRows(rows []sqliteMasterRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return kindRank(rows[i].Kind) < kindRank(rows[j].Kind)
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
		ddlParts = append(ddlParts, normalizeDDL(r.SQL))
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
