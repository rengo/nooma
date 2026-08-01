package memrepo

import (
	"context"
	"slices"
	"sync"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/ports"
)

// embeddingKey is the upsert key: one unit holds one vector per model, so a
// re-embed under a new model must not evict the old one (ports.EmbeddingRepo
// states why).
type embeddingKey struct {
	unitID string
	model  string
}

// Embeddings is an in-memory ports.EmbeddingRepo. The zero value is not
// usable — call NewEmbeddings. Two instances share no state, matching
// memrepo.Units's own isolation rule.
type Embeddings struct {
	mu sync.Mutex
	// byModel keeps insertion order per model, so LoadIndex is
	// deterministic. A map alone would return a different index ordering on
	// every call, and while recall.Search sorts by score, ties would then
	// resolve differently run to run — flakiness that reads as a ranking bug.
	byModel map[string][]embeddingKey
	vectors map[embeddingKey][]float32
}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent: a port signature change then fails here, in the package that
// broke, rather than in a conformance test one directory away.
var _ ports.EmbeddingRepo = (*Embeddings)(nil)

// NewEmbeddings returns an empty, ready-to-use in-memory
// ports.EmbeddingRepo. Every call returns an independent instance.
func NewEmbeddings() *Embeddings {
	return &Embeddings{
		byModel: make(map[string][]embeddingKey),
		vectors: make(map[embeddingKey][]float32),
	}
}

// Put implements ports.EmbeddingRepo.
func (r *Embeddings) Put(_ context.Context, e ports.Embedding) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := embeddingKey{unitID: e.UnitID, model: e.Model}
	if _, exists := r.vectors[key]; !exists {
		r.byModel[e.Model] = append(r.byModel[e.Model], key)
	}
	// Copied, never aliased: the caller still holds the slice it passed, and
	// mutating it must not reach in here.
	r.vectors[key] = slices.Clone(e.Vector)
	return nil
}

// LoadIndex implements ports.EmbeddingRepo.
func (r *Embeddings) LoadIndex(_ context.Context, model string) (recall.VectorIndex, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	keys := r.byModel[model]
	idx := recall.VectorIndex{
		Model:   model,
		IDs:     make([]string, 0, len(keys)),
		Vectors: make([][]float32, 0, len(keys)),
	}
	for _, key := range keys {
		idx.IDs = append(idx.IDs, key.unitID)
		idx.Vectors = append(idx.Vectors, slices.Clone(r.vectors[key]))
	}
	// Model is carried even when empty — recall.Search compares it, and an
	// empty string would mismatch every query.
	return idx, nil
}
