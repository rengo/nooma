package ports

import (
	"context"
	"time"

	"github.com/rengo/nooma/internal/core/recall"
)

// Embedding is one unit's vector under one model — design D8, the
// `embeddings` table of docs/03-data-model.md.
//
// Model is carried on every row rather than assumed globally, because a vault
// holds rows from two models while a reindex is in progress (doc 02 §5,
// ADR-0003's amendment). Without it, the rows an interrupted reindex has
// already rewritten would be indistinguishable from the ones it has not
// reached yet — and every search filters on model precisely so the two are
// never compared.
//
// At is data, like every other timestamp crossing a port: no repository
// method reads a clock, so the instant arrives from the one the pipeline
// already read (design D5).
type Embedding struct {
	UnitID string
	Model  string
	Vector []float32
	At     time.Time
}

// EmbeddingRepo is the repository port over embeddings — design D8.
//
// Two methods, and the asymmetry between them is the design: writes are
// per-unit, reads are whole-index. Vector search scores a query against every
// candidate at once, so there is no useful "one embedding by id" read — and
// not offering one is what keeps a caller from assembling an index by hand
// and getting the model scoping wrong.
//
// LoadIndex returns a recall.VectorIndex rather than a slice for the same
// reason: the value handed back is the one recall.Search consumes, already
// scoped to a single model, so no caller sits between the store and the
// search deciding what "the index for this model" means.
type EmbeddingRepo interface {
	// Put stores e, replacing the unit's embedding under e.Model only. The
	// key is (UnitID, Model), as unit_embeddings' primary key is since
	// migration 0016 (ADR-0058): a unit holds one embedding per model, and a
	// write under one model keeps its embedding under any other. A reindex
	// therefore adds the new model's vectors beside the old ones; it never
	// takes away the index the running model reads, and nothing is deleted.
	Put(ctx context.Context, e Embedding) error

	// LoadIndex returns every embedding recorded under model, as an index
	// scoped to exactly that model — I21's storage precondition. A model
	// with no embeddings is not an error: it returns an empty index carrying
	// the requested model, which is the ordinary cold-start case and which
	// recall.Search compares against.
	LoadIndex(ctx context.Context, model string) (recall.VectorIndex, error)

	// CountLiveWithoutEmbedding reports how many live units (status =
	// 'pool') hold no embedding under model: none at all, or only another
	// model's. Recall reads model's vectors alone (I21), so a unit embedded
	// under another model is as unreachable by meaning as one never
	// embedded; counting "any model" once let a model change read as a
	// healthy vault. It is the units<->embeddings half of
	// docs/03-data-model.md's consistency promise, read by nooma doctor and
	// by serve's boot warning. Archived units are excluded — I02's
	// read-side filter, applied to a count, because nothing live reads an
	// archived unit's missing vector.
	//
	// Zero is the healthy answer.
	CountLiveWithoutEmbedding(ctx context.Context, model string) (int, error)
}
