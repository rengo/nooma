package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/internal/ports"
)

// EmbeddingRepo is the SQLite-backed ports.EmbeddingRepo over
// unit_embeddings (migration 0002).
type EmbeddingRepo struct {
	db *sql.DB
}

// NewEmbeddingRepo returns a ports.EmbeddingRepo backed by v's
// already-migrated vault.
func NewEmbeddingRepo(v *Vault) *EmbeddingRepo {
	return &EmbeddingRepo{db: v.db}
}

var _ ports.EmbeddingRepo = (*EmbeddingRepo)(nil)

// Put implements ports.EmbeddingRepo.
//
// ON CONFLICT(unit_id) — the primary key — so a re-embed replaces the unit's
// row rather than adding a second one. That is what makes ADR-0003's
// amendment work: reindex is "an ordinary UPDATE loop", resumable and
// incremental, complete when no rows of the old model remain. model is
// updated alongside the vector, so a half-finished reindex is readable from
// the data rather than from a separate progress record.
//
// The vector is L2-normalized immediately before encoding — the column
// comment in migration 0002 promises "L2-normalized on write", and
// recall.Search's cosine scoring assumes it. Normalizing here rather than at
// the call site means no caller can forget: the promise belongs to whoever
// writes the bytes.
//
// store → core is the import direction Phase A's unitrepo.go already
// establishes: the adapter may use the core's pure functions, never the
// reverse (design D6).
func (r *EmbeddingRepo) Put(ctx context.Context, e ports.Embedding) error {
	normalized, err := recall.Normalize(e.Vector)
	if err != nil {
		return fmt.Errorf("normalizing the embedding for unit %s: %w", e.UnitID, err)
	}

	const q = `
INSERT INTO unit_embeddings (unit_id, model, dim, embedding, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(unit_id) DO UPDATE SET
  model      = excluded.model,
  dim        = excluded.dim,
  embedding  = excluded.embedding,
  created_at = excluded.created_at`

	_, err = r.db.ExecContext(ctx, q,
		e.UnitID, e.Model, len(normalized), encodeVector(normalized),
		e.At.UTC().Format(unitTimeLayout),
	)
	if err != nil {
		return fmt.Errorf("storing the embedding for unit %s: %w", e.UnitID, err)
	}
	return nil
}

// LoadIndex implements ports.EmbeddingRepo.
//
// One query at vault-open time, not one per recall call — spec R3.2's MUST
// NOT. The index is loaded whole and searched in memory, so the query path
// touches no SQL at all.
//
// Ordering by unit_id makes the index deterministic. recall.Search sorts by
// score, but ties would otherwise resolve by whatever order SQLite happened
// to return, which reads as a ranking bug rather than as the coin-flip it is.
func (r *EmbeddingRepo) LoadIndex(ctx context.Context, model string) (recall.VectorIndex, error) {
	const q = `
SELECT unit_id, dim, embedding
FROM unit_embeddings
WHERE model = ?
ORDER BY unit_id`

	// Model is carried even when no row matches: recall.Search compares this
	// field against the query's, and an empty string would mismatch every
	// query rather than return no results.
	idx := recall.VectorIndex{Model: model}

	rows, err := r.db.QueryContext(ctx, q, model)
	if err != nil {
		return recall.VectorIndex{}, fmt.Errorf("loading the embedding index for %s: %w", model, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			unitID string
			dim    int
			blob   []byte
		)
		if err := rows.Scan(&unitID, &dim, &blob); err != nil {
			return recall.VectorIndex{}, fmt.Errorf("scanning an embedding row: %w", err)
		}

		vector, err := decodeVector(blob, dim)
		if err != nil {
			return recall.VectorIndex{}, fmt.Errorf("decoding unit %s's embedding: %w", unitID, err)
		}
		idx.IDs = append(idx.IDs, unitID)
		idx.Vectors = append(idx.Vectors, vector)
	}
	if err := rows.Err(); err != nil {
		return recall.VectorIndex{}, fmt.Errorf("iterating embedding rows for %s: %w", model, err)
	}
	return idx, nil
}

// CountLiveWithoutEmbedding implements ports.EmbeddingRepo.
//
// A LEFT JOIN from units to unit_embeddings, filtered to status = 'pool' —
// design D18b row 2's own SQL shape, verbatim. unit_id IS NULL is what a
// missing row on the right side of the join looks like; it is what makes
// this one query rather than a count-then-count-then-subtract over two
// round trips.
func (r *EmbeddingRepo) CountLiveWithoutEmbedding(ctx context.Context) (int, error) {
	const q = `
SELECT COUNT(*)
FROM units u
LEFT JOIN unit_embeddings ue ON ue.unit_id = u.id
WHERE u.status = 'pool' AND ue.unit_id IS NULL`

	var count int
	if err := r.db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting live units without an embedding: %w", err)
	}
	return count, nil
}

// encodeVector renders v as dim × float32, little-endian — migration 0002's
// stated BLOB encoding.
func encodeVector(v []float32) []byte {
	blob := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(f))
	}
	return blob
}

// decodeVector reverses encodeVector, checking the blob against the row's own
// dim column.
//
// dim is redundant with the blob length, and migration 0002's comment says
// that redundancy is "worth the redundancy". This is where it earns that: a
// blob truncated by a partial write decodes into a shorter vector that
// recall.Search would happily score against a full-length query, producing a
// number rather than an error. Checking the two against each other turns a
// silent wrong answer into a loud one.
func decodeVector(blob []byte, dim int) ([]float32, error) {
	if len(blob) != dim*4 {
		return nil, fmt.Errorf("blob is %d bytes, but dim says %d float32s (%d bytes)",
			len(blob), dim, dim*4)
	}
	v := make([]float32, dim)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return v, nil
}
