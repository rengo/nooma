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

	// CountLiveWithoutEmbedding reports how many live units (status =
	// 'pool') hold no embedding at all, under any model — design D18b row
	// 2's runtime half of spec R6.3, and the units<->embeddings half of
	// docs/03-data-model.md's own "nooma doctor runs PRAGMA integrity_check
	// + units<->embeddings<->fts consistency" promise (the fts half stays
	// M6's). Archived units are excluded — I02's own read-side filter,
	// applied here to a count rather than a read, because nothing live
	// reads an archived unit's missing vector.
	//
	// This is the method m1b-pipeline/design.md:790-793 deliberately did
	// not ship: "UnembeddedLive or similar would be a port method whose
	// only caller is a test... recorded for whoever ships doctor's
	// consistency check." nooma doctor (spec R6.3) is that caller — one of
	// R7.4's three sanctioned edits to an existing internal/ports file, and
	// the last of them.
	//
	// Zero is the healthy answer. Above zero names a vault whose capture
	// path stored units this repository never received a Put for — the
	// shape a permanently-unembedded Cloud vault takes (m1c-surface's own
	// C9 finding, D18's whole reason for existing).
	CountLiveWithoutEmbedding(ctx context.Context) (int, error)
}
