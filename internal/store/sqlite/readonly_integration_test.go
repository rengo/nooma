//go:build integration

package sqlite

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

// TestOpenReadOnly_NeverMigratesAndCannotWrite: a read-only vault reports
// the schema version the file holds, behind this binary or not, migrates
// nothing, and refuses a write at the SQLite level (I55).
func TestOpenReadOnly_NeverMigratesAndCannotWrite(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	target, err := SchemaTarget()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.db.ExecContext(ctx, "PRAGMA user_version = 3"); err != nil {
		t.Fatal(err)
	}
	path := v.Path()
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	ro, err := OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	got, err := ro.SchemaVersion(ctx)
	if err != nil || got != 3 {
		t.Fatalf("SchemaVersion = %d, %v; want 3, the file's own (this binary is at %d)", got, err, target)
	}
	if _, err := ro.db.ExecContext(ctx, "PRAGMA user_version = 99"); err == nil {
		t.Error("a write through the read-only vault succeeded")
	}
	if err := ro.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := VerifySnapshot(ctx, path)
	if err != nil || again != 3 {
		t.Errorf("after the read-only open the file is at %d, %v; want 3, unmigrated", again, err)
	}
}

// TestOpenReadOnly_ReportsAVaultAheadOfTheBinaryWithoutRefusing: Open refuses
// a vault newer than this binary; the read-only open still opens it, so
// doctor can say so.
func TestOpenReadOnly_ReportsAVaultAheadOfTheBinaryWithoutRefusing(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	target, err := SchemaTarget()
	if err != nil {
		t.Fatal(err)
	}
	// PRAGMA takes no bound parameter.
	if _, err := v.db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", target+1)); err != nil {
		t.Fatal(err)
	}
	path := v.Path()
	_ = v.Close()

	ro, err := OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatalf("OpenReadOnly of a vault ahead of the binary: %v", err)
	}
	defer func() { _ = ro.Close() }()
	if got, _ := ro.SchemaVersion(ctx); got != target+1 {
		t.Errorf("SchemaVersion = %d, want %d", got, target+1)
	}
}

// TestSchemaTarget_IsWhatOpenMigratesTo: the binary's own schema version is
// the one a fresh Open leaves.
func TestSchemaTarget_IsWhatOpenMigratesTo(t *testing.T) {
	v := openTestVault(t)
	got, err := v.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	target, err := SchemaTarget()
	if err != nil || target != got || target < 15 {
		t.Errorf("SchemaTarget = %d, %v; want %d, the version Open migrates to", target, err, got)
	}
}

// TestEmbeddingRepo_CountsLiveUnitsLackingTheConfiguredModel: a live unit
// whose only vector is another model's lacks the configured one, as one with
// no vector does (I21); an archived unit is not counted, and the models held
// by live units are reported with their counts.
func TestEmbeddingRepo_CountsLiveUnitsLackingTheConfiguredModel(t *testing.T) {
	v := openTestVault(t)
	h := embeddingHarness{EmbeddingRepo: NewEmbeddingRepo(v), v: v}
	ctx := context.Background()
	for _, id := range []string{"u-a", "u-b", "u-bare", "u-archived"} {
		h.EnsureUnit(t, id)
	}
	for id, model := range map[string]string{"u-a": "model-a", "u-b": "model-b", "u-archived": "model-b"} {
		if err := h.Put(ctx, ports.Embedding{UnitID: id, Model: model, Vector: []float32{1, 0, 0}, At: embeddingFixtureTime}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := v.db.ExecContext(ctx, `UPDATE units SET status = 'archived' WHERE id = 'u-archived'`); err != nil {
		t.Fatal(err)
	}

	missing, err := h.CountLiveWithoutEmbedding(ctx, "model-a")
	if err != nil || missing != 2 {
		t.Errorf("CountLiveWithoutEmbedding(model-a) = %d, %v; want 2 (u-b under model-b, u-bare)", missing, err)
	}
	if missing, _ := h.CountLiveWithoutEmbedding(ctx, "model-b"); missing != 2 {
		t.Errorf("CountLiveWithoutEmbedding(model-b) = %d, want 2 (u-a, u-bare)", missing)
	}
	models, err := NewEmbeddingRepo(v).LiveModels(ctx)
	want := []ModelCount{{Model: "model-a", Units: 1}, {Model: "model-b", Units: 1}}
	if err != nil || !reflect.DeepEqual(models, want) {
		t.Errorf("LiveModels = %+v, %v; want %+v (the archived unit's vector not counted)", models, err, want)
	}
	rows, err := NewEmbeddingRepo(v).ModelRows(ctx)
	wantRows := []ModelCount{{Model: "model-a", Units: 1}, {Model: "model-b", Units: 2}}
	if err != nil || !reflect.DeepEqual(rows, wantRows) {
		t.Errorf("ModelRows = %+v, %v; want %+v (every row, the archived unit's included)", rows, err, wantRows)
	}
}

// TestSearchIndexGaps_FindsUnitsMissingFromTheWordIndexAndRowsForNoUnit: the
// word index mirrors units row for row (doc 03); a unit it lost and an entry
// for no unit are each counted, and a coherent index reports none.
func TestSearchIndexGaps_FindsUnitsMissingFromTheWordIndexAndRowsForNoUnit(t *testing.T) {
	v := openTestVault(t)
	h := embeddingHarness{EmbeddingRepo: NewEmbeddingRepo(v), v: v}
	ctx := context.Background()
	for _, id := range []string{"u-1", "u-2", "u-3"} {
		h.EnsureUnit(t, id)
	}
	gaps, err := v.SearchIndexGaps(ctx)
	if err != nil || gaps != (SearchIndexGaps{}) {
		t.Fatalf("SearchIndexGaps on a coherent index = %+v, %v; want none", gaps, err)
	}

	if _, err := v.db.ExecContext(ctx,
		`INSERT INTO units_fts(units_fts, rowid, content) SELECT 'delete', rowid, content FROM units WHERE id = 'u-3'`); err != nil {
		t.Fatal(err)
	}
	if _, err := v.db.ExecContext(ctx, `INSERT INTO units_fts(rowid, content) VALUES (9999, 'nobody')`); err != nil {
		t.Fatal(err)
	}
	gaps, err = v.SearchIndexGaps(ctx)
	if err != nil || gaps != (SearchIndexGaps{Missing: 1, Orphaned: 1}) {
		t.Errorf("SearchIndexGaps = %+v, %v; want one unit missing and one entry for no unit", gaps, err)
	}

	// Read-only, the same answer.
	path := v.Path()
	_ = v.Close()
	ro, err := OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	if gaps, err := ro.SearchIndexGaps(ctx); err != nil || gaps != (SearchIndexGaps{Missing: 1, Orphaned: 1}) {
		t.Errorf("read-only SearchIndexGaps = %+v, %v; want the same", gaps, err)
	}
}

// TestRebuildSearchIndex_ClosesEveryGap: after a vacuum the word index may
// point at the wrong rows (doc 03), so import rebuilds it; the rebuild leaves
// it mirroring units exactly.
func TestRebuildSearchIndex_ClosesEveryGap(t *testing.T) {
	v := openTestVault(t)
	h := embeddingHarness{EmbeddingRepo: NewEmbeddingRepo(v), v: v}
	ctx := context.Background()
	for _, id := range []string{"u-1", "u-2"} {
		h.EnsureUnit(t, id)
	}
	if _, err := v.db.ExecContext(ctx,
		`INSERT INTO units_fts(units_fts, rowid, content) SELECT 'delete', rowid, content FROM units WHERE id = 'u-2'`); err != nil {
		t.Fatal(err)
	}
	if _, err := v.db.ExecContext(ctx, `INSERT INTO units_fts(rowid, content) VALUES (9999, 'nobody')`); err != nil {
		t.Fatal(err)
	}
	if err := v.RebuildSearchIndex(ctx); err != nil {
		t.Fatalf("RebuildSearchIndex: %v", err)
	}
	if gaps, err := v.SearchIndexGaps(ctx); err != nil || gaps != (SearchIndexGaps{}) {
		t.Errorf("SearchIndexGaps after the rebuild = %+v, %v; want none", gaps, err)
	}
}
