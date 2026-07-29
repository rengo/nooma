package schema

import (
	"reflect"
	"testing"
)

// TestDiffNoDisagreement asserts Diff returns nil (or empty) when doc and
// golden declare exactly the same objects and column sets — the steady
// state R4.3 is meant to hold once slice 4a closed the R9.1/R9.2 gap.
func TestDiffNoDisagreement(t *testing.T) {
	doc := []Object{
		{Kind: KindTable, Name: "units", Columns: []string{"content", "id"}},
		{Kind: KindTrigger, Name: "units_fts_ai"},
	}
	golden := []Object{
		{Kind: KindTrigger, Name: "units_fts_ai"},
		{Kind: KindTable, Name: "units", Columns: []string{"id", "content"}},
	}

	got := Diff(doc, golden)
	if len(got) != 0 {
		t.Errorf("Diff(...) = %#v, want no differences", got)
	}
}

// TestDiffMissingFromSchema asserts design §6.5's first assertion: an
// object doc 03 declares but the schema golden does not is reported as
// MissingFromSchema.
func TestDiffMissingFromSchema(t *testing.T) {
	doc := []Object{
		{Kind: KindTrigger, Name: "units_fts_del"},
	}
	var golden []Object

	got := Diff(doc, golden)
	want := []Difference{{DiffKind: DiffMissingFromSchema, Kind: KindTrigger, Name: "units_fts_del"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Diff(...) = %#v, want %#v", got, want)
	}
}

// TestDiffUndeclaredInDoc asserts design §6.5's second assertion — the one
// that forces the FTS trigger DDL into doc 03 (proposal §4.2, R9.2): an
// object the schema golden has but doc 03 never declares is reported as
// UndeclaredInDoc.
func TestDiffUndeclaredInDoc(t *testing.T) {
	var doc []Object
	golden := []Object{
		{Kind: KindTrigger, Name: "units_fts_ad"},
	}

	got := Diff(doc, golden)
	want := []Difference{{DiffKind: DiffUndeclaredInDoc, Kind: KindTrigger, Name: "units_fts_ad"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Diff(...) = %#v, want %#v", got, want)
	}
}

// TestDiffColumnMismatch asserts design §6.5's third assertion: for a table
// or virtual_table present on both sides, a differing column set is
// reported with both directions of the set difference, matching §6.5's
// illustrative failure output ("only in doc 03" / "only in schema").
func TestDiffColumnMismatch(t *testing.T) {
	doc := []Object{
		{Kind: KindTable, Name: "units", Columns: []string{"content", "id"}},
	}
	golden := []Object{
		{Kind: KindTable, Name: "units", Columns: []string{"content", "id", "archived_at"}},
	}

	got := Diff(doc, golden)
	want := []Difference{{
		DiffKind:     DiffColumnMismatch,
		Kind:         KindTable,
		Name:         "units",
		OnlyInDoc:    nil,
		OnlyInSchema: []string{"archived_at"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Diff(...) = %#v, want %#v", got, want)
	}
}

// TestDiffDoesNotCompareColumnsForIndexOrTrigger asserts design §6.5: the
// column-set comparison applies only to table and virtual_table objects —
// an index or trigger present on both sides with no column data at all
// must never spuriously appear as a ColumnMismatch.
func TestDiffDoesNotCompareColumnsForIndexOrTrigger(t *testing.T) {
	doc := []Object{
		{Kind: KindIndex, Name: "idx_units_status_touched"},
		{Kind: KindTrigger, Name: "units_fts_ai"},
	}
	golden := []Object{
		{Kind: KindIndex, Name: "idx_units_status_touched"},
		{Kind: KindTrigger, Name: "units_fts_ai"},
	}

	got := Diff(doc, golden)
	if len(got) != 0 {
		t.Errorf("Diff(...) = %#v, want no differences (index/trigger carry no columns to compare)", got)
	}
}

// TestDiffOrdersDeterministically asserts Diff's output order does not
// depend on Go's randomized map iteration: MissingFromSchema before
// UndeclaredInDoc before ColumnMismatch, each group sorted by (kind rank,
// name) — a flaky-looking diff report would be its own bug independent of
// whatever real disagreement it is supposed to name.
func TestDiffOrdersDeterministically(t *testing.T) {
	doc := []Object{
		{Kind: KindTable, Name: "z_only_in_doc"},
		{Kind: KindTable, Name: "units", Columns: []string{"content"}},
	}
	golden := []Object{
		{Kind: KindTable, Name: "a_only_in_schema"},
		{Kind: KindTable, Name: "units", Columns: []string{"content", "archived_at"}},
	}

	for i := 0; i < 20; i++ {
		got := Diff(doc, golden)
		want := []Difference{
			{DiffKind: DiffMissingFromSchema, Kind: KindTable, Name: "z_only_in_doc"},
			{DiffKind: DiffUndeclaredInDoc, Kind: KindTable, Name: "a_only_in_schema"},
			{DiffKind: DiffColumnMismatch, Kind: KindTable, Name: "units", OnlyInSchema: []string{"archived_at"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Diff(...) run %d = %#v, want %#v", i, got, want)
		}
	}
}
