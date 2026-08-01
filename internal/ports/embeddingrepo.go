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
	// Put stores e, replacing any embedding already held for the unit. The
	// key is UnitID alone, because unit_embeddings.unit_id is the primary
	// key (migration 0002): one unit holds exactly one embedding, under
	// exactly one model.
	//
	// A model change is an overwrite, not a second row. ADR-0003's amendment
	// makes reindex "an ordinary UPDATE loop", resumable and incremental,
	// complete when no rows of the old model remain. That is what "a vault
	// can hold two models at once" means — different *units* at different
	// points in one reindex, never one unit twice.
	Put(ctx context.Context, e Embedding) error

	// LoadIndex returns every embedding recorded under model, as an index
	// scoped to exactly that model — I21's storage precondition. A model
	// with no embeddings is not an error: it returns an empty index carrying
	// the requested model, which is the ordinary cold-start case and which
	// recall.Search compares against.
	LoadIndex(ctx context.Context, model string) (recall.VectorIndex, error)
}
