// Package schema holds the stdlib-only structural projection of the
// database schema (design D1/§6.4, openspec/changes/complete-harness):
// Kind and Object describe one object's shape, Marshal/ParseGolden
// serialize and read back testdata/schema/structure.golden. ParseMarkdown
// and Diff (markdown.go) are the docs/03-data-model.md side of the
// comparison, consumed by test/conformance's TestHarness_SchemaMatchesDoc03
// (R4.3).
//
// This package deliberately imports nothing beyond the standard library
// (design §3): FromSQLite, which needs database/sql, stays in the L3 test
// file that generates the golden, not here.
package schema

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Kind names the category of a schema object, read from the normalized DDL
// prefix rather than sqlite_master.type alone (design §6.2): a virtual
// table and a unique index need their own Kind so the golden's sort and a
// future doc-03 comparison can tell them apart from an ordinary table or
// index.
type Kind string

const (
	KindTable        Kind = "table"
	KindVirtualTable Kind = "virtual_table"
	KindIndex        Kind = "index"
	KindUniqueIndex  Kind = "unique_index"
	KindTrigger      Kind = "trigger"
	KindView         Kind = "view"
)

// kindRank orders Kind values for the structural golden's sort (design
// §6.2): table < virtual_table < index < unique_index < trigger < view.
var kindRank = map[Kind]int{
	KindTable:        0,
	KindVirtualTable: 1,
	KindIndex:        2,
	KindUniqueIndex:  3,
	KindTrigger:      4,
	KindView:         5,
}

// Rank returns k's position in the golden's total order — table <
// virtual_table < index < unique_index < trigger < view (design §6.2). An
// unrecognized Kind sorts last.
//
// Exported so every consumer of this ordering — Sort/Marshal here, and the
// L3 golden-generation test in test/integration/schema_golden_test.go —
// reads it from the SAME map instead of each maintaining its own copy
// (slice-3 review finding 7: the two copies agreed today, but nothing kept
// them agreeing once a future migration added a Kind only one of them
// knew about — a silently different sort order between structure.golden
// and ddl.golden is exactly the un-reviewable golden diff the gate exists
// to prevent).
func Rank(k Kind) int {
	if r, ok := kindRank[k]; ok {
		return r
	}
	return len(kindRank)
}

// createPrefixPattern recognizes the CREATE statement shapes sqlite_master
// can hold, case- and whitespace-insensitive, with an optional TEMP /
// TEMPORARY qualifier (design §6.2). It anchors on the CREATE ... <kind>
// prefix only and does not need to know what follows: "CREATE TABLE IF NOT
// EXISTS foo (...)" matches on "CREATE TABLE" exactly the same way
// "CREATE TABLE foo (...)" does, because \b already ends the match at the
// word boundary after TABLE regardless of what comes next.
var createPrefixPattern = regexp.MustCompile(
	`(?is)^CREATE\s+(?:TEMP\s+|TEMPORARY\s+)?(VIRTUAL\s+TABLE|UNIQUE\s+INDEX|TABLE|INDEX|TRIGGER|VIEW)\b`,
)

// Classify reads a schema object's Kind off the normalized DDL prefix
// (design §6.2), not off sqlite_master.type alone — that column cannot
// distinguish a virtual table from an ordinary one, or a unique index from
// a plain one. ok is false when createSQL's prefix does not match any
// recognized CREATE shape (the caller is expected to treat that as an
// unclassified-object error — moved here, with its own direct table tests,
// per slice-3 review finding 3: this regex previously had zero direct
// tests and was only exercised end-to-end through the single fixture
// 0001_core_tables.sql happens to produce).
func Classify(createSQL string) (Kind, bool) {
	m := createPrefixPattern.FindStringSubmatch(createSQL)
	if m == nil {
		return "", false
	}
	switch strings.ToUpper(strings.Join(strings.Fields(m[1]), " ")) {
	case "TABLE":
		return KindTable, true
	case "VIRTUAL TABLE":
		return KindVirtualTable, true
	case "INDEX":
		return KindIndex, true
	case "UNIQUE INDEX":
		return KindUniqueIndex, true
	case "TRIGGER":
		return KindTrigger, true
	case "VIEW":
		return KindView, true
	default:
		return "", false
	}
}

// IsShadowTable reports whether name matches the shape of an FTS5 shadow
// table of one of the virtual tables in virtualTables: "<vt>_data",
// "<vt>_idx", "<vt>_docsize", "<vt>_config" and similar "<vt>_<suffix>"
// forms (design §6.2).
//
// This is a bare name-prefix check. It does NOT look at the candidate
// object's Kind, so it also returns true for a table, trigger, index, or
// view that merely happens to be named "<vt>_<something>" — for example a
// virtual table units_fts and a same-migration trigger named
// units_fts_ai both collide with this prefix rule, even though only the
// former is an FTS5 shadow object. Every caller MUST gate on
// Kind == KindTable before trusting a true result here (dumpSchema in
// test/integration/schema_golden_test.go does this); this function does
// not and cannot do that gating itself, because it is never given a Kind
// to gate on. Discovered when 0002_learning_and_search.sql's FTS5 sync
// triggers (units_fts_ai/_ad/_au) were silently dropped from both schema
// goldens by a call site that had not yet added that guard (four-lens
// pre-PR review, slice 4a).
//
// Residual gap, left as-is rather than fixed here because the fix is not
// obviously cheap or correct: this prefix check does not verify the
// suffix is one of FTS5's own known shadow suffixes ("_data", "_idx",
// "_docsize", "_config"). A future ordinary table named
// "units_fts_history" would still be silently dropped from both goldens,
// Kind guard or not, because that guard only rules out non-table objects —
// it does not validate the suffix.
func IsShadowTable(name string, virtualTables map[string]bool) bool {
	for vt := range virtualTables {
		if strings.HasPrefix(name, vt+"_") {
			return true
		}
	}
	return false
}

// NormalizeDDL implements ddl.golden's per-statement normalization (design
// §6.3): trim trailing whitespace per line, trim the whole statement, and
// end it with a single ";".
func NormalizeDDL(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + ";"
}

// Object is one schema object's structural projection: its kind, name,
// and, for a table or virtual table, its column name set. Types, NOT
// NULL, defaults, FK clauses, trigger bodies and index predicates are
// deliberately not part of this projection — ddl.golden carries those
// instead (design §6.2/§6.3).
type Object struct {
	Kind    Kind
	Name    string
	Columns []string
}

// Sort orders objs by (kind rank, name) and each object's Columns by name
// (design §6.2), in place.
func Sort(objs []Object) {
	sort.Slice(objs, func(i, j int) bool {
		if objs[i].Kind != objs[j].Kind {
			return kindRank[objs[i].Kind] < kindRank[objs[j].Kind]
		}
		return objs[i].Name < objs[j].Name
	})
	for i := range objs {
		sort.Strings(objs[i].Columns)
	}
}

// Marshal renders objs as testdata/schema/structure.golden's format
// (design §6.2): a two-space-indent, one-object-or-column-per-line text
// file with schemaVersion (the highest published migration version, R3.8)
// recorded as a header line. objs is not mutated; Marshal sorts a copy.
func Marshal(objs []Object, schemaVersion int) []byte {
	sorted := make([]Object, len(objs))
	copy(sorted, objs)
	Sort(sorted)

	var b strings.Builder
	b.WriteString("# nooma schema structure golden — generated by `make schema-golden`, do not edit.\n")
	b.WriteString("# Compared against docs/03-data-model.md by TestHarness_SchemaMatchesDoc03 (L2).\n")
	fmt.Fprintf(&b, "schema_version %d\n\n", schemaVersion)

	for _, obj := range sorted {
		fmt.Fprintf(&b, "%s %s\n", obj.Kind, obj.Name)
		for _, col := range obj.Columns {
			fmt.Fprintf(&b, "  column %s\n", col)
		}
	}

	return []byte(b.String())
}

// ParseGolden parses testdata/schema/structure.golden's format back into
// Objects, sorted the same way Marshal would sort them. Comment lines
// (#...) and the schema_version header are recognized and skipped — they
// are not schema content.
func ParseGolden(b []byte) ([]Object, error) {
	var objs []Object
	var current *Object

	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "schema_version "):
			continue
		case strings.HasPrefix(line, "  column "):
			if current == nil {
				return nil, fmt.Errorf("column line %q precedes any object", line)
			}
			current.Columns = append(current.Columns, strings.TrimPrefix(line, "  column "))
		default:
			if current != nil {
				objs = append(objs, *current)
			}
			kind, name, ok := strings.Cut(trimmed, " ")
			if !ok {
				return nil, fmt.Errorf("malformed object line: %q", line)
			}
			current = &Object{Kind: Kind(kind), Name: name}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan golden: %w", err)
	}
	if current != nil {
		objs = append(objs, *current)
	}

	Sort(objs)
	return objs, nil
}
