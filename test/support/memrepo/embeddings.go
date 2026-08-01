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
	order  []string
	byUnit map[string]ports.Embedding
}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent: a port signature change then fails here, in the package that
// broke, rather than in a conformance test one directory away.
var _ ports.EmbeddingRepo = (*Embeddings)(nil)

// NewEmbeddings returns an empty, ready-to-use in-memory
// ports.EmbeddingRepo. Every call returns an independent instance.
func NewEmbeddings() *Embeddings {
	return &Embeddings{byUnit: make(map[string]ports.Embedding)}
}

// EnsureUnit implements repocontract.EmbeddingHarness. It does nothing: the
// fake enforces no foreign key, so every unit id is already a valid target.
// The method exists so one suite can drive both implementations — see
// repocontract.EmbeddingHarness for why the store needs it.
func (r *Embeddings) EnsureUnit(_ *testing.T, _ string) {}

// Put implements ports.EmbeddingRepo.
func (r *Embeddings) Put(_ context.Context, e ports.Embedding) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byUnit[e.UnitID]; !exists {
		r.order = append(r.order, e.UnitID)
	}
	// Keyed on UnitID alone: unit_embeddings.unit_id is the primary key, so
	// a re-embed under a new model overwrites rather than adding a row.
	// Copied, never aliased — the caller still holds the slice it passed.
	stored := e
	stored.Vector = slices.Clone(e.Vector)
	r.byUnit[e.UnitID] = stored
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
	for _, id := range r.order {
		e, ok := r.byUnit[id]
		if !ok || e.Model != model {
			continue
		}
		idx.IDs = append(idx.IDs, e.UnitID)
		idx.Vectors = append(idx.Vectors, slices.Clone(e.Vector))
	}
	// Model is carried even when empty — recall.Search compares it, and an
	// empty string would mismatch every query.
	return idx, nil
}
