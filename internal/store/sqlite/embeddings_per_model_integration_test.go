//go:build integration

package sqlite

import (
	"bytes"
	"context"
	"testing"

	"github.com/rengo/nooma/internal/ports"
)

func fixtureEmbeddingAt(unit, model string, vec []float32) ports.Embedding {
	return ports.Embedding{UnitID: unit, Model: model, Vector: vec, At: embeddingFixtureTime}
}

// TestMigration0016KeysEmbeddingsByUnitAndModel: a vault that already holds
// vectors when 0016 runs keeps every one, byte for byte, with its model, dim
// and instant; the key becomes (unit_id, model), so a second model's vector
// for the same unit is a second row; the model index and the cascade from
// units remain. Nothing is deleted (ADR-0058).
func TestMigration0016KeysEmbeddingsByUnitAndModel(t *testing.T) {
	blobA, blobB := encodeVector([]float32{1, 0}), encodeVector([]float32{0, 1, 0})
	db := backfilledBefore(t, "0016_embeddings_per_model.sql", func(exec func(string, ...any)) {
		seedLegacyUnit(exec, "u-1")
		seedLegacyUnit(exec, "u-2")
		exec(`INSERT INTO unit_embeddings (unit_id, model, dim, embedding, created_at) VALUES ('u-1', 'model-a', 2, ?, '2026-10-01T00:00:00Z')`, blobA)
		exec(`INSERT INTO unit_embeddings (unit_id, model, dim, embedding, created_at) VALUES ('u-2', 'model-b', 3, ?, '2026-10-02T00:00:00Z')`, blobB)
	})
	ctx := context.Background()

	rows, err := db.QueryContext(ctx, `SELECT unit_id, model, dim, embedding, created_at FROM unit_embeddings ORDER BY unit_id`)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		unit, model string
		dim         int
		blob        []byte
		at          string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.unit, &r.model, &r.dim, &r.blob, &r.at); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	_ = rows.Close()
	want := []row{{"u-1", "model-a", 2, blobA, "2026-10-01T00:00:00Z"}, {"u-2", "model-b", 3, blobB, "2026-10-02T00:00:00Z"}}
	if len(got) != len(want) {
		t.Fatalf("rows after 0016 = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].unit != want[i].unit || got[i].model != want[i].model || got[i].dim != want[i].dim ||
			!bytes.Equal(got[i].blob, want[i].blob) || got[i].at != want[i].at {
			t.Errorf("row %d after 0016 = %+v, want %+v", i, got[i], want[i])
		}
	}

	var pk []string
	pkRows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('unit_embeddings') WHERE pk > 0 ORDER BY pk`)
	if err != nil {
		t.Fatal(err)
	}
	for pkRows.Next() {
		var n string
		_ = pkRows.Scan(&n)
		pk = append(pk, n)
	}
	_ = pkRows.Close()
	if len(pk) != 2 || pk[0] != "unit_id" || pk[1] != "model" {
		t.Errorf("unit_embeddings' primary key = %v, want (unit_id, model)", pk)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO unit_embeddings (unit_id, model, dim, embedding, created_at) VALUES ('u-1', 'model-b', 3, ?, '2026-10-03T00:00:00Z')`, blobB); err != nil {
		t.Errorf("a second model's vector for u-1 was refused: %v", err)
	}
	var idx int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_unit_embeddings_model'`).Scan(&idx); err != nil || idx != 1 {
		t.Errorf("idx_unit_embeddings_model after 0016: %d, %v", idx, err)
	}
	var fk string
	if err := db.QueryRowContext(ctx, `SELECT "table" || ':' || on_delete FROM pragma_foreign_key_list('unit_embeddings')`).Scan(&fk); err != nil || fk != "units:CASCADE" {
		t.Errorf("unit_embeddings' foreign key = %q, %v; want units, ON DELETE CASCADE", fk, err)
	}
	var violations int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check('unit_embeddings')`).Scan(&violations); err != nil || violations != 0 {
		t.Errorf("foreign key violations after 0016: %d, %v", violations, err)
	}
}

// TestEmbeddingRepo_KeepsEachModelsVector (I21): over the real store, a unit
// holds one vector per model; writing it under one model keeps its vector
// under another, and each model's index and count read only their own.
func TestEmbeddingRepo_KeepsEachModelsVector(t *testing.T) {
	v := openTestVault(t)
	h := embeddingHarness{EmbeddingRepo: NewEmbeddingRepo(v), v: v}
	ctx := context.Background()
	h.EnsureUnit(t, "u-1")
	h.EnsureUnit(t, "u-2")
	for _, e := range []struct {
		unit, model string
		vec         []float32
	}{{"u-1", "model-a", []float32{1, 0}}, {"u-1", "model-b", []float32{0, 1, 0}}, {"u-2", "model-a", []float32{0, 1}}} {
		if err := h.Put(ctx, fixtureEmbeddingAt(e.unit, e.model, e.vec)); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := h.LoadIndex(ctx, "model-a")
	b, _ := h.LoadIndex(ctx, "model-b")
	if len(a.IDs) != 2 || len(b.IDs) != 1 || b.IDs[0] != "u-1" || len(b.Vectors[0]) != 3 {
		t.Errorf("model-a index %v, model-b index %v; want u-1 in both, each with its own vector", a.IDs, b.IDs)
	}
	if missing, _ := h.CountLiveWithoutEmbedding(ctx, "model-b"); missing != 1 {
		t.Errorf("live units without a model-b vector = %d, want 1 (u-2)", missing)
	}
	rows, _ := NewEmbeddingRepo(v).ModelRows(ctx)
	if len(rows) != 2 || rows[0].Units != 2 || rows[1].Units != 1 {
		t.Errorf("ModelRows = %+v, want model-a 2, model-b 1", rows)
	}
}
