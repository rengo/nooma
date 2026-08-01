package repocontract

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/ports"
)

// EmbeddingHarness is what an EmbeddingRepo implementation must offer the
// contract so the suite can put embeddings in front of it.
//
// EnsureUnit exists because unit_embeddings.unit_id REFERENCES units(id) and
// the vault opens with foreign_keys=on: over the real store, a Put for a unit
// that does not exist is a constraint violation, while the in-memory fake has
// no such notion. Without this hook the suite would pass at L2 and be
// impossible to run at L3 — which is not a contract, it is a fake's opinion.
type EmbeddingHarness interface {
	ports.EmbeddingRepo

	// EnsureUnit makes id a valid embedding target. The store inserts the
	// row; the fake does nothing.
	EnsureUnit(t *testing.T, id string)
}

// RunEmbeddingRepo runs the ports.EmbeddingRepo contract against a fresh
// implementation built by newRepo for every subtest. newRepo must return one
// holding no embedding.
//
// Design D6's "answered twice" rule applies here as it does to UnitRepo: the
// in-memory fake answers this suite at L2 and internal/store/sqlite's
// implementation answers the identical suite at L3, so the two cannot drift
// while one lags behind the other.
func RunEmbeddingRepo(t *testing.T, newRepo func(t *testing.T) EmbeddingHarness) {
	t.Helper()

	t.Run("Put then LoadIndex returns the embedding", func(t *testing.T) {
		repo := newRepo(t)
		repo.EnsureUnit(t, "unit-1")
		ctx := context.Background()

		e := fixtureEmbedding("unit-1", "model-a", []float32{1, 0, 0})
		if err := repo.Put(ctx, e); err != nil {
			t.Fatalf("Put: %v", err)
		}

		idx, err := repo.LoadIndex(ctx, "model-a")
		if err != nil {
			t.Fatalf("LoadIndex: %v", err)
		}
		if idx.Model != "model-a" {
			t.Errorf("index Model = %q, want %q", idx.Model, "model-a")
		}
		if !reflect.DeepEqual(idx.IDs, []string{"unit-1"}) {
			t.Errorf("index IDs = %v, want [unit-1]", idx.IDs)
		}
		if !reflect.DeepEqual(idx.Vectors, [][]float32{{1, 0, 0}}) {
			t.Errorf("index Vectors = %v, want [[1 0 0]]", idx.Vectors)
		}
	})

	// unit_id is the primary key (design D8), so a second Put for the same
	// unit replaces rather than duplicates. Without this, a re-embedding
	// would leave the old vector in the index alongside the new one and
	// recall would score the same unit twice.
	t.Run("Put upserts on unit_id", func(t *testing.T) {
		repo := newRepo(t)
		repo.EnsureUnit(t, "unit-1")
		ctx := context.Background()

		if err := repo.Put(ctx, fixtureEmbedding("unit-1", "model-a", []float32{1, 0, 0})); err != nil {
			t.Fatalf("first Put: %v", err)
		}
		if err := repo.Put(ctx, fixtureEmbedding("unit-1", "model-a", []float32{0, 1, 0})); err != nil {
			t.Fatalf("second Put: %v", err)
		}

		idx, err := repo.LoadIndex(ctx, "model-a")
		if err != nil {
			t.Fatalf("LoadIndex: %v", err)
		}
		if len(idx.IDs) != 1 {
			t.Fatalf("index holds %d entries after two Puts for one unit, want 1: %v",
				len(idx.IDs), idx.IDs)
		}
		if !reflect.DeepEqual(idx.Vectors[0], []float32{0, 1, 0}) {
			t.Errorf("index kept the first vector %v; the second Put must replace it",
				idx.Vectors[0])
		}
	})

	// I21's storage precondition. A vault holds two models at once while a
	// reindex is in progress, and vectors from two models are never compared
	// — ADR-0003: "the distance between them is noise shaped like a number".
	// The filtering has to happen here, because recall.Search only refuses a
	// mismatch it can see: an index that silently mixed models would present
	// one Model field and score rows belonging to another.
	t.Run("LoadIndex scopes to exactly the requested model", func(t *testing.T) {
		repo := newRepo(t)
		repo.EnsureUnit(t, "unit-1")
		repo.EnsureUnit(t, "unit-2")
		repo.EnsureUnit(t, "unit-3")
		ctx := context.Background()

		for _, e := range []ports.Embedding{
			fixtureEmbedding("unit-1", "model-a", []float32{1, 0, 0}),
			fixtureEmbedding("unit-2", "model-b", []float32{0, 1, 0}),
			fixtureEmbedding("unit-3", "model-a", []float32{0, 0, 1}),
		} {
			if err := repo.Put(ctx, e); err != nil {
				t.Fatalf("Put %s/%s: %v", e.UnitID, e.Model, err)
			}
		}

		idx, err := repo.LoadIndex(ctx, "model-a")
		if err != nil {
			t.Fatalf("LoadIndex: %v", err)
		}
		if len(idx.IDs) != 2 {
			t.Fatalf("index holds %v, want exactly the two model-a units", idx.IDs)
		}
		for _, id := range idx.IDs {
			if id == "unit-2" {
				t.Error("index holds unit-2, whose embedding belongs to model-b — comparing " +
					"two models' vectors is noise shaped like a number (I21, ADR-0003)")
			}
		}
	})

	// A model change OVERWRITES the unit's row; it does not add a second
	// one. unit_embeddings.unit_id is the primary key (migration 0002), and
	// ADR-0003's amendment makes reindex an ordinary UPDATE loop, complete
	// when no rows of the old model remain.
	//
	// This case exists because the opposite is an easy and plausible thing
	// to build — a fake keyed on (unit_id, model) passes every other case
	// here while being unimplementable over the real schema. That is the
	// divergence design D6's "answered twice" rule exists to catch, and it
	// caught exactly this one.
	t.Run("a re-embed under a new model replaces the row", func(t *testing.T) {
		repo := newRepo(t)
		repo.EnsureUnit(t, "unit-1")
		ctx := context.Background()

		if err := repo.Put(ctx, fixtureEmbedding("unit-1", "model-old", []float32{1, 0, 0})); err != nil {
			t.Fatalf("Put model-old: %v", err)
		}
		if err := repo.Put(ctx, fixtureEmbedding("unit-1", "model-new", []float32{0, 1, 0})); err != nil {
			t.Fatalf("Put model-new: %v", err)
		}

		old, err := repo.LoadIndex(ctx, "model-old")
		if err != nil {
			t.Fatalf("LoadIndex(model-old): %v", err)
		}
		if len(old.IDs) != 0 {
			t.Errorf("LoadIndex(model-old) still holds %v — a re-embed must move the unit, "+
				"not leave a stale row a search could still return", old.IDs)
		}

		fresh, err := repo.LoadIndex(ctx, "model-new")
		if err != nil {
			t.Fatalf("LoadIndex(model-new): %v", err)
		}
		if len(fresh.IDs) != 1 || fresh.IDs[0] != "unit-1" {
			t.Errorf("LoadIndex(model-new) = %v, want [unit-1]", fresh.IDs)
		}
	})

	// An absent model is not an error: a vault that has never embedded
	// anything under a model is the ordinary cold-start case, and returning
	// an error would make every caller special-case it.
	t.Run("LoadIndex on an unknown model returns an empty index", func(t *testing.T) {
		repo := newRepo(t)

		idx, err := repo.LoadIndex(context.Background(), "model-never-used")
		if err != nil {
			t.Fatalf("LoadIndex on an unknown model = %v, want nil — an empty vault is not "+
				"an error", err)
		}
		if len(idx.IDs) != 0 || len(idx.Vectors) != 0 {
			t.Errorf("index is not empty: %v / %v", idx.IDs, idx.Vectors)
		}
		if idx.Model != "model-never-used" {
			t.Errorf("index Model = %q, want the requested model even when empty — recall.Search "+
				"compares this field, and an empty string would mismatch every query",
				idx.Model)
		}
	})

	// The index must be usable by recall.Search without further shaping:
	// that is the whole reason LoadIndex returns a recall.VectorIndex rather
	// than a slice the caller assembles.
	t.Run("the returned index is directly searchable", func(t *testing.T) {
		repo := newRepo(t)
		repo.EnsureUnit(t, "unit-far")
		repo.EnsureUnit(t, "unit-near")
		ctx := context.Background()

		for _, e := range []ports.Embedding{
			fixtureEmbedding("unit-far", "model-a", []float32{0, 1, 0}),
			fixtureEmbedding("unit-near", "model-a", []float32{1, 0, 0}),
		} {
			if err := repo.Put(ctx, e); err != nil {
				t.Fatalf("Put: %v", err)
			}
		}

		idx, err := repo.LoadIndex(ctx, "model-a")
		if err != nil {
			t.Fatalf("LoadIndex: %v", err)
		}

		scored, err := recall.Search(idx, recall.VectorQuery{
			Model: "model-a", Vector: []float32{1, 0, 0}, K: 1,
		})
		if err != nil {
			t.Fatalf("recall.Search over the loaded index: %v", err)
		}
		if len(scored) != 1 || scored[0].ID != "unit-near" {
			t.Errorf("Search returned %v, want unit-near first", scored)
		}
	})

	// Mutating what Put was handed, or what LoadIndex returned, must not
	// reach into the repository. The precedent is memrepo.Units's own
	// deep-copying contract; a []float32 shared by reference is the easiest
	// version of this bug to write and the hardest to see.
	t.Run("stored vectors are copied, not aliased", func(t *testing.T) {
		repo := newRepo(t)
		repo.EnsureUnit(t, "unit-1")
		ctx := context.Background()

		vector := []float32{1, 0, 0}
		if err := repo.Put(ctx, fixtureEmbedding("unit-1", "model-a", vector)); err != nil {
			t.Fatalf("Put: %v", err)
		}
		vector[0] = 99 // the caller still holds the slice it passed in

		idx, err := repo.LoadIndex(ctx, "model-a")
		if err != nil {
			t.Fatalf("LoadIndex: %v", err)
		}
		if idx.Vectors[0][0] == 99 {
			t.Fatal("mutating the caller's slice changed the stored vector — Put aliased it")
		}

		idx.Vectors[0][0] = 42 // and now the reader mutates what it was given
		reloaded, err := repo.LoadIndex(ctx, "model-a")
		if err != nil {
			t.Fatalf("LoadIndex again: %v", err)
		}
		if reloaded.Vectors[0][0] == 42 {
			t.Fatal("mutating the returned index changed the stored vector — LoadIndex aliased it")
		}
	})
}

// fixtureEmbedding builds an embedding with a fixed instant: At is data, not
// something the repository reads from a clock (design D5's rule, applied to
// this port too).
func fixtureEmbedding(unitID, model string, vector []float32) ports.Embedding {
	return ports.Embedding{
		UnitID: unitID,
		Model:  model,
		Vector: vector,
		At:     time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	}
}
