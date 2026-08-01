//go:build integration

package sqlite

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/repocontract"
)

// embeddingFixtureTime is a fixed, whole-second UTC instant: this suite does
// not exercise clock behaviour, so a literal keeps every fixture identical.
var embeddingFixtureTime = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

// embeddingHarness adapts the real repo to repocontract.EmbeddingHarness.
//
// EnsureUnit is the whole reason the harness exists: unit_embeddings.unit_id
// REFERENCES units(id) and the vault opens with foreign_keys=on, so a Put for
// a unit that does not exist is a constraint violation here and a no-op in
// the fake. It inserts directly rather than going through UnitRepo — the
// fixture must not depend on another port's behaviour to set up this one's.
type embeddingHarness struct {
	*EmbeddingRepo
	v *Vault
}

func (h embeddingHarness) EnsureUnit(t *testing.T, id string) {
	t.Helper()

	_, err := h.v.db.ExecContext(context.Background(), `
INSERT INTO units (id, type, content, status, weight, weight_decay_rate,
                   last_touched_at, source, created_at, updated_at)
VALUES (?, 'task', 'fixture', 'pool', 1.0, 0.01, ?, 'chat', ?, ?)
ON CONFLICT(id) DO NOTHING`,
		id, embeddingFixtureTime.Format(unitTimeLayout),
		embeddingFixtureTime.Format(unitTimeLayout),
		embeddingFixtureTime.Format(unitTimeLayout))
	if err != nil {
		t.Fatalf("seeding unit %s: %v", id, err)
	}
}

// TestEmbeddingRepo_Contract runs the same repocontract.RunEmbeddingRepo
// suite the in-memory fake answers at L2, now against a real temporary SQLite
// vault at L3 — design D6's "answered twice" standing rule.
//
// This is the run that matters. The fake answered this suite alone in PR 9a
// and two of its cases were wrong in ways only a real schema could reveal:
// one required a second row per unit that unit_id's PRIMARY KEY forbids, and
// every case Put an embedding for a unit that did not exist, which
// foreign_keys=on rejects. A contract answered once is a fake's opinion.
func TestEmbeddingRepo_Contract(t *testing.T) {
	repocontract.RunEmbeddingRepo(t, func(t *testing.T) repocontract.EmbeddingHarness {
		v := openTestVault(t)
		return embeddingHarness{EmbeddingRepo: NewEmbeddingRepo(v), v: v}
	})
}

// TestEmbeddingRepo_PutNormalizesAndRoundTrips is task 9b.1's own claim,
// below the contract: what actually lands in the column.
//
// The vector goes in deliberately un-normalized. Migration 0002's column
// comment promises "L2-normalized on write" and recall.Search's cosine
// scoring assumes it — a repo that stored raw magnitudes would return
// plausible-looking scores that are wrong by a constant factor per unit,
// which no ranking assertion would obviously catch.
func TestEmbeddingRepo_PutNormalizesAndRoundTrips(t *testing.T) {
	v := openTestVault(t)
	h := embeddingHarness{EmbeddingRepo: NewEmbeddingRepo(v), v: v}
	h.EnsureUnit(t, "unit-1")
	ctx := context.Background()

	// Magnitude 5, direction (3,4)/5 — chosen so the normalized values are
	// exact in binary floating point and the test cannot be flaky.
	raw := []float32{3, 4, 0}
	if err := h.Put(ctx, ports.Embedding{
		UnitID: "unit-1", Model: "model-a", Vector: raw, At: embeddingFixtureTime,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var (
		dim  int
		blob []byte
	)
	row := v.db.QueryRowContext(ctx,
		`SELECT dim, embedding FROM unit_embeddings WHERE unit_id = ?`, "unit-1")
	if err := row.Scan(&dim, &blob); err != nil {
		t.Fatalf("reading the stored row: %v", err)
	}

	if dim != len(raw) {
		t.Errorf("dim = %d, want %d — the column is redundant with the blob length on "+
			"purpose, and the redundancy is only worth anything if it agrees", dim, len(raw))
	}
	if len(blob) != dim*4 {
		t.Fatalf("blob is %d bytes, want dim*4 = %d", len(blob), dim*4)
	}

	// Decode the bytes here rather than through LoadIndex: this test is about
	// the encoding, and reading it back with the same helper that wrote it
	// would prove only that the codec is self-consistent.
	got := make([]float32, dim)
	for i := range got {
		got[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}

	want := []float32{0.6, 0.8, 0}
	for i := range want {
		if math.Abs(float64(got[i]-want[i])) > 1e-6 {
			t.Errorf("stored vector = %v, want %v — Put must L2-normalize before encoding "+
				"(migration 0002's column comment)", got, want)
			break
		}
	}

	var norm float64
	for _, x := range got {
		norm += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-6 {
		t.Errorf("stored vector has L2 norm %v, want 1", math.Sqrt(norm))
	}
}

// TestEmbeddingRepo_LoadIndexScopesToOneModel is task 9b.2 — I21's storage
// half (R3.4), seeded by raw INSERT so the fixture cannot lean on Put's own
// scoping being correct.
func TestEmbeddingRepo_LoadIndexScopesToOneModel(t *testing.T) {
	v := openTestVault(t)
	h := embeddingHarness{EmbeddingRepo: NewEmbeddingRepo(v), v: v}
	ctx := context.Background()

	for _, seed := range []struct {
		id     string
		model  string
		vector []float32
	}{
		{"unit-a1", "model-a", []float32{1, 0, 0}},
		{"unit-b1", "model-b", []float32{0, 1, 0}},
		{"unit-a2", "model-a", []float32{0, 0, 1}},
	} {
		h.EnsureUnit(t, seed.id)
		blob := make([]byte, len(seed.vector)*4)
		for i, f := range seed.vector {
			binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(f))
		}
		if _, err := v.db.ExecContext(ctx, `
INSERT INTO unit_embeddings (unit_id, model, dim, embedding, created_at)
VALUES (?, ?, ?, ?, ?)`,
			seed.id, seed.model, len(seed.vector), blob,
			embeddingFixtureTime.Format(unitTimeLayout)); err != nil {
			t.Fatalf("seeding %s: %v", seed.id, err)
		}
	}

	idx, err := h.LoadIndex(ctx, "model-a")
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if idx.Model != "model-a" {
		t.Errorf("index Model = %q, want model-a", idx.Model)
	}
	if len(idx.IDs) != 2 {
		t.Fatalf("index holds %v, want exactly the two model-a units", idx.IDs)
	}
	for _, id := range idx.IDs {
		if id == "unit-b1" {
			t.Error("index holds unit-b1, whose embedding is model-b — comparing two models' " +
				"vectors is noise shaped like a number (I21, ADR-0003)")
		}
	}
	// ORDER BY unit_id: without it, ties in recall.Search would resolve by
	// whatever order SQLite happened to return.
	if idx.IDs[0] != "unit-a1" || idx.IDs[1] != "unit-a2" {
		t.Errorf("index order = %v, want deterministic [unit-a1 unit-a2]", idx.IDs)
	}
	if idx.Vectors[0][0] != 1 || idx.Vectors[1][2] != 1 {
		t.Errorf("vectors did not round-trip: %v", idx.Vectors)
	}
}

// TestEmbeddingRepo_DimMismatchIsAnError proves the redundancy migration 0002
// deliberately kept actually earns its keep. A blob truncated by a partial
// write decodes into a shorter vector that recall.Search would score against
// a full-length query, producing a number rather than an error — a silent
// wrong answer. dim disagreeing with the blob length turns it into a loud one.
func TestEmbeddingRepo_DimMismatchIsAnError(t *testing.T) {
	v := openTestVault(t)
	h := embeddingHarness{EmbeddingRepo: NewEmbeddingRepo(v), v: v}
	h.EnsureUnit(t, "unit-1")
	ctx := context.Background()

	// dim claims 3 float32s; the blob holds two.
	if _, err := v.db.ExecContext(ctx, `
INSERT INTO unit_embeddings (unit_id, model, dim, embedding, created_at)
VALUES (?, ?, ?, ?, ?)`,
		"unit-1", "model-a", 3, make([]byte, 8),
		embeddingFixtureTime.Format(unitTimeLayout)); err != nil {
		t.Fatalf("seeding the truncated row: %v", err)
	}

	if _, err := h.LoadIndex(ctx, "model-a"); err == nil {
		t.Fatal("LoadIndex returned no error for a blob whose length disagrees with dim — " +
			"the shorter vector would have been scored against full-length queries")
	}
}
