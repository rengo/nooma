package brain

import (
	"sync"

	"github.com/rengo/nooma/internal/core/recall"
)

// Index is the in-memory recall.VectorIndex holder ADR-0012 requires: "[t]he
// index is loaded from SQLite when the vault opens" and "must not be paid
// per request" (0012:63,96). It is constructed once, from
// ports.EmbeddingRepo.LoadIndex's result, at vault open — not by
// CaptureService or RecallService, neither of which reads a store to build
// one.
//
// Add exists so that single load stays correct across the vault's whole
// lifetime: without it, a unit captured after vault open would be invisible
// to this and every later capture's own recall step until the process
// restarted, silently reintroducing the per-request reload ADR-0012 forbids.
// captureRunner calls Add once per successfully embedded unit (design D4's
// pipeline, right after embeds.Put), so the vector this capture just wrote
// to the store is also, immediately, in the vector recall.Search ranges
// over.
type Index struct {
	mu    sync.Mutex
	model string
	ids   []string
	vecs  [][]float32
}

// NewIndex wraps idx as the resident index. idx is normally
// ports.EmbeddingRepo.LoadIndex's own return value, read once at vault open;
// a fresh vault's idx carries no entries yet, only the model it is scoped
// to.
func NewIndex(idx recall.VectorIndex) *Index {
	return &Index{
		model: idx.Model,
		ids:   append([]string(nil), idx.IDs...),
		vecs:  append([][]float32(nil), idx.Vectors...),
	}
}

// Add grows the resident index by one entry. vector is expected
// unit-normalized, matching what every recall.VectorIndex holds elsewhere
// (recall.VectorQuery's own doc comment) — callers normalize before calling
// Add, this method does not normalize on their behalf, the same division of
// labor recall.Search itself assumes of its caller.
func (idx *Index) Add(id string, vector []float32) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.ids = append(idx.ids, id)
	idx.vecs = append(idx.vecs, vector)
}

// Snapshot returns idx's current contents as a recall.VectorIndex —
// recall.Search's own input shape. It copies rather than aliases, so a
// concurrent Add cannot change the slice a search already in flight is
// ranging over.
func (idx *Index) Snapshot() recall.VectorIndex {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return recall.VectorIndex{
		Model:   idx.model,
		IDs:     append([]string(nil), idx.ids...),
		Vectors: append([][]float32(nil), idx.vecs...),
	}
}
