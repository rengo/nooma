package schema

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseMarkdownIgnoresNonSQLFenceAndNonCreateStatements asserts design
// §6.4 steps 1 and 4: a fenced block whose info string is not exactly "sql"
// (docs/03-data-model.md's own ```go``` fts5.Register snippet) is never
// scanned, and a statement inside a ```sql``` fence that does not begin
// with CREATE (a sample query added later) is ignored rather than
// mis-parsed — both are documented conventions, not silent data loss,
// because neither shape can ever represent a schema object R4.3 compares.
func TestParseMarkdownIgnoresNonSQLFenceAndNonCreateStatements(t *testing.T) {
	md := []byte("" +
		"# doc\n\n" +
		"```go\n" +
		"CREATE TABLE go_fence_is_not_sql (id TEXT);\n" +
		"```\n\n" +
		"```sql\n" +
		"SELECT * FROM units;\n" +
		"\n" +
		"CREATE TABLE units (\n" +
		"  id TEXT PRIMARY KEY\n" +
		");\n" +
		"```\n",
	)

	got, err := ParseMarkdown(md)
	if err != nil {
		t.Fatalf("ParseMarkdown(...) = _, %v, want nil error", err)
	}

	want := []Object{{Kind: KindTable, Name: "units", Columns: []string{"id"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseMarkdown(...) = %#v, want %#v", got, want)
	}
}

// TestParseMarkdownCommentStrippingIsStringAware asserts design §6.4 step
// 2: a "--" sequence inside a single-quoted string literal is data, not a
// comment start, so it must not truncate the statement or corrupt the
// string-tracking state used by later steps (statement splitting, column
// extraction). A real trailing "--" comment on the same statement must
// still be stripped.
func TestParseMarkdownCommentStrippingIsStringAware(t *testing.T) {
	md := []byte("```sql\n" +
		"CREATE TABLE t (\n" +
		"  id TEXT PRIMARY KEY,\n" +
		"  note TEXT DEFAULT 'contains -- not a comment', -- but this is a real comment\n" +
		"  count INTEGER\n" +
		");\n" +
		"```\n",
	)

	got, err := ParseMarkdown(md)
	if err != nil {
		t.Fatalf("ParseMarkdown(...) = _, %v, want nil error", err)
	}

	want := []Object{{Kind: KindTable, Name: "t", Columns: []string{"count", "id", "note"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseMarkdown(...) = %#v, want %#v", got, want)
	}
}

// TestParseMarkdownTriggerAwareStatementSplitting asserts design §6.4 step
// 3: a ";" inside a CREATE TRIGGER's BEGIN...END body does not end the
// statement — only the ";" following the balancing END does — and parsing
// resumes correctly for whatever statement follows.
func TestParseMarkdownTriggerAwareStatementSplitting(t *testing.T) {
	md := []byte("```sql\n" +
		"CREATE TRIGGER units_fts_ai AFTER INSERT ON units BEGIN\n" +
		"  INSERT INTO units_fts(rowid, content) VALUES (new.rowid, new.content);\n" +
		"END;\n" +
		"CREATE TABLE after_trigger (\n" +
		"  id TEXT PRIMARY KEY\n" +
		");\n" +
		"```\n",
	)

	got, err := ParseMarkdown(md)
	if err != nil {
		t.Fatalf("ParseMarkdown(...) = _, %v, want nil error", err)
	}

	want := []Object{
		{Kind: KindTable, Name: "after_trigger", Columns: []string{"id"}},
		{Kind: KindTrigger, Name: "units_fts_ai"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseMarkdown(...) = %#v, want %#v", got, want)
	}
}

// TestParseMarkdownTableConstraintIsNotAColumn asserts design §6.4 step 6:
// a table-level constraint (relations' own UNIQUE (from_unit_id,
// to_unit_id, type), docs/03-data-model.md) is dropped because its first
// token is a constraint keyword, not treated as a column named "UNIQUE".
func TestParseMarkdownTableConstraintIsNotAColumn(t *testing.T) {
	md := []byte("```sql\n" +
		"CREATE TABLE relations (\n" +
		"  id            TEXT PRIMARY KEY,\n" +
		"  from_unit_id  TEXT NOT NULL,\n" +
		"  to_unit_id    TEXT NOT NULL,\n" +
		"  type          TEXT NOT NULL,\n" +
		"  UNIQUE (from_unit_id, to_unit_id, type)\n" +
		");\n" +
		"```\n",
	)

	got, err := ParseMarkdown(md)
	if err != nil {
		t.Fatalf("ParseMarkdown(...) = _, %v, want nil error", err)
	}

	want := []Object{{
		Kind:    KindTable,
		Name:    "relations",
		Columns: []string{"from_unit_id", "id", "to_unit_id", "type"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseMarkdown(...) = %#v, want %#v", got, want)
	}
}

// TestParseMarkdownFTS5OptionIsNotAColumn asserts design §6.4 step 7: an
// fts5 module option (content='units', content_rowid='rowid') is not a
// column because it contains a depth-0 "=" — only "content" is a real
// column of docs/03-data-model.md's units_fts declaration.
func TestParseMarkdownFTS5OptionIsNotAColumn(t *testing.T) {
	md := []byte("```sql\n" +
		"CREATE VIRTUAL TABLE units_fts USING fts5(\n" +
		"  content, content='units', content_rowid='rowid'\n" +
		");\n" +
		"```\n",
	)

	got, err := ParseMarkdown(md)
	if err != nil {
		t.Fatalf("ParseMarkdown(...) = _, %v, want nil error", err)
	}

	want := []Object{{Kind: KindVirtualTable, Name: "units_fts", Columns: []string{"content"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseMarkdown(...) = %#v, want %#v", got, want)
	}
}

// TestParseMarkdownMultilinePartialIndex asserts design §6.4 step 8 for a
// multi-line CREATE UNIQUE INDEX whose ON clause and WHERE predicate span
// several lines (docs/03-data-model.md's idx_units_unique_active_insight):
// only kind and name are captured, no column parsing is attempted for an
// index.
func TestParseMarkdownMultilinePartialIndex(t *testing.T) {
	md := []byte("```sql\n" +
		"CREATE UNIQUE INDEX idx_units_unique_active_insight\n" +
		"  ON units(type, json_extract(structured_data,'$.domain'),\n" +
		"                 json_extract(structured_data,'$.metricKey'))\n" +
		"  WHERE status = 'pool' AND type = 'insight';\n" +
		"```\n",
	)

	got, err := ParseMarkdown(md)
	if err != nil {
		t.Fatalf("ParseMarkdown(...) = _, %v, want nil error", err)
	}

	want := []Object{{Kind: KindUniqueIndex, Name: "idx_units_unique_active_insight"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseMarkdown(...) = %#v, want %#v", got, want)
	}
}

// TestParseMarkdownErrorsOnUnclassifiableCreateStatement is the trap-avoidance
// test this task's brief demanded explicitly: ParseMarkdown must never
// silently skip a statement it cannot understand. A statement that begins
// with CREATE but does not match any recognized object shape (here,
// "CREATE FOREIGN TABLE", the same unclassifiable shape schema.Classify's
// own table test already names) MUST be a loud error naming the offending
// text, never a quiet omission — anything else would let doc 03 and the
// schema silently disagree while the gate stays green.
func TestParseMarkdownErrorsOnUnclassifiableCreateStatement(t *testing.T) {
	md := []byte("```sql\n" +
		"CREATE FOREIGN TABLE not_a_real_sqlite_object (id TEXT);\n" +
		"```\n",
	)

	_, err := ParseMarkdown(md)
	if err == nil {
		t.Fatal("ParseMarkdown(...) = _, nil, want a non-nil error naming the unclassifiable statement")
	}
	if !strings.Contains(err.Error(), "CREATE FOREIGN TABLE") {
		t.Errorf("ParseMarkdown(...) error = %q, want it to name the unparsed statement", err.Error())
	}
}

// TestParseMarkdownTrailingStatementWithoutSemicolonStillErrors closes a
// narrower version of the same silent-drop risk: a fenced block's final
// statement missing its terminating ";" (a doc-authoring mistake) must
// still be classified rather than silently dropped by the range scanner
// that only emits a statement when it sees a ";".
func TestParseMarkdownTrailingStatementWithoutSemicolonStillErrors(t *testing.T) {
	md := []byte("```sql\n" +
		"CREATE FOREIGN TABLE not_a_real_sqlite_object (id TEXT)\n" +
		"```\n",
	)

	_, err := ParseMarkdown(md)
	if err == nil {
		t.Fatal("ParseMarkdown(...) = _, nil, want a non-nil error for the unterminated, unclassifiable statement")
	}
}
