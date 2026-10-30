package memrepo

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/ports"
)

// Embeddings is an in-memory ports.EmbeddingRepo. The zero value is not
// usable — call NewEmbeddings. Two instances share no state, matching
// memrepo.Units's own isolation rule.
type Embeddings struct {
	mu sync.Mutex
	// order preserves insertion order so LoadIndex is deterministic. A map
	// alone would return a different index ordering on every call, and while
	// recall.Search sorts by score, ties would then resolve differently run
	// to run — flakiness that reads as a ranking bug.
	order []embeddingKey
	rows  map[embeddingKey]ports.Embedding

	// liveUnits is every id EnsureUnit has registered, for
	// CountLiveWithoutEmbedding's benefit. The fake has no notion of unit
	// status (unlike the real store's units.status column), so every
	// ensured unit counts as live here — archived-exclusion is proven only
	// at L3, against a real vault
	// (internal/store/sqlite/embeddingrepo_integration_test.go).
	liveUnits map[string]bool
}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent: a port signature change then fails here, in the package that
// broke, rather than in a conformance test one directory away.
var _ ports.EmbeddingRepo = (*Embeddings)(nil)

// NewEmbeddings returns an empty, ready-to-use in-memory
// ports.EmbeddingRepo. Every call returns an independent instance.
func NewEmbeddings() *Embeddings {
	return &Embeddings{rows: make(map[embeddingKey]ports.Embedding), liveUnits: make(map[string]bool)}
}

// EnsureUnit implements repocontract.EmbeddingHarness. It enforces no
// foreign key — every unit id is already a valid Put target over this fake
// — but it does record id as live, so CountLiveWithoutEmbedding has
// something to count against. The store's own EnsureUnit
// (internal/store/sqlite/embeddingrepo_integration_test.go) inserts a real
// row instead; see repocontract.EmbeddingHarness for why the store needs
// the hook at all.
func (r *Embeddings) EnsureUnit(_ *testing.T, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.liveUnits[id] = true
}

// CountLiveWithoutEmbedding implements ports.EmbeddingRepo.
func (r *Embeddings) CountLiveWithoutEmbedding(_ context.Context, model string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	count := 0
	for id := range r.liveUnits {
		if _, embedded := r.rows[embeddingKey{id, model}]; !embedded {
			count++
		}
	}
	return count, nil
}

// Put implements ports.EmbeddingRepo.
func (r *Embeddings) Put(_ context.Context, e ports.Embedding) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := embeddingKey{e.UnitID, e.Model}
	if _, exists := r.rows[key]; !exists {
		r.order = append(r.order, key)
	}
	// Keyed on (unit, model), as unit_embeddings is since migration 0016: a
	// write under one model replaces only that model's vector for the unit.
	// Copied, never aliased — the caller still holds the slice it passed.
	stored := e
	stored.Vector = slices.Clone(e.Vector)
	r.rows[key] = stored
	return nil
}

// LoadIndex implements ports.EmbeddingRepo.
func (r *Embeddings) LoadIndex(_ context.Context, model string) (recall.VectorIndex, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := recall.VectorIndex{
		Model:   model,
		IDs:     make([]string, 0, len(r.order)),
		Vectors: make([][]float32, 0, len(r.order)),
	}
	for _, key := range r.order {
		e := r.rows[key]
		if key.model != model {
			continue
		}
		idx.IDs = append(idx.IDs, e.UnitID)
		idx.Vectors = append(idx.Vectors, slices.Clone(e.Vector))
	}
	// Model is carried even when empty — recall.Search compares it, and an
	// empty string would mismatch every query.
	return idx, nil
}

// embeddingKey is unit_embeddings' primary key, (unit_id, model).
type embeddingKey struct{ unit, model string }
