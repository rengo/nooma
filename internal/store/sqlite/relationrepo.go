package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
)

// RelationRepo is the SQLite-backed ports.RelationRepo over relations
// (migration 0001:30-40) and relation_thresholds (migration 0002:30-35).
type RelationRepo struct {
	db *sql.DB
}

// NewRelationRepo returns a ports.RelationRepo backed by v's already-migrated
// vault.
func NewRelationRepo(v *Vault) *RelationRepo {
	return &RelationRepo{db: v.db}
}

var _ ports.RelationRepo = (*RelationRepo)(nil)

// Upsert implements ports.RelationRepo.
//
// ON CONFLICT (from_unit_id, to_unit_id, type) — the real UNIQUE constraint
// (migration 0001:39), not the id primary key — is the conflict target, so a
// second Upsert over the same triple resolves against that index rather than
// failing it (I07). Only strength and confidence are SET on conflict: id,
// from_unit_id, to_unit_id, type, created_by and created_at all keep the
// first row's values, because ports.RelationRepo.Upsert's own contract is
// that a later confidence revision must not also rewrite who first noticed
// the relation or when.
func (r *RelationRepo) Upsert(ctx context.Context, rel ports.Relation) error {
	const q = `
INSERT INTO relations (id, from_unit_id, to_unit_id, type, strength, confidence, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (from_unit_id, to_unit_id, type) DO UPDATE SET
  strength   = excluded.strength,
  confidence = excluded.confidence`

	_, err := r.db.ExecContext(ctx, q,
		rel.ID, rel.FromUnitID, rel.ToUnitID, rel.Type, rel.Strength, rel.Confidence,
		rel.CreatedBy, rel.CreatedAt.UTC().Format(unitTimeLayout),
	)
	if err != nil {
		return fmt.Errorf("upserting relation %s->%s (%s): %w", rel.FromUnitID, rel.ToUnitID, rel.Type, err)
	}
	return nil
}

// ByUnit implements ports.RelationRepo.
//
// unitID matches either endpoint — design D8: "ByUnit's consumer is Phase
// C's read-only units route", which shows every relation touching a unit,
// not only the ones it originates. ORDER BY created_at, id is
// ports.RelationRepo.ByUnit's own documented tie-break, matching
// memrepo.Relations's in-memory sort so the two cannot silently disagree on
// read order.
func (r *RelationRepo) ByUnit(ctx context.Context, unitID string) ([]ports.Relation, error) {
	const q = `
SELECT id, from_unit_id, to_unit_id, type, strength, confidence, created_by, created_at
FROM relations
WHERE from_unit_id = ? OR to_unit_id = ?
ORDER BY created_at, id`

	rows, err := r.db.QueryContext(ctx, q, unitID, unitID)
	if err != nil {
		return nil, fmt.Errorf("reading relations for unit %s: %w", unitID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []ports.Relation
	for rows.Next() {
		var (
			rel         ports.Relation
			createdAtTx string
		)
		if err := rows.Scan(&rel.ID, &rel.FromUnitID, &rel.ToUnitID, &rel.Type,
			&rel.Strength, &rel.Confidence, &rel.CreatedBy, &createdAtTx); err != nil {
			return nil, fmt.Errorf("scanning a relation row: %w", err)
		}
		at, err := time.Parse(unitTimeLayout, createdAtTx)
		if err != nil {
			return nil, fmt.Errorf("relation %q: created_at: %w", rel.ID, err)
		}
		rel.CreatedAt = at
		out = append(out, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading relations for unit %s: %w", unitID, err)
	}
	return out, nil
}

// ThresholdsFor implements ports.RelationRepo.
//
// relation_type is UNIQUE (migration 0002:32), so at most one row can match.
// sql.ErrNoRows maps to (nil, nil) — design D8, R5.6: relation_thresholds
// seeds no row for any type, so an absent row is the ordinary "never looked
// up before" case relation.Resolve's fallback exists for, not an error.
func (r *RelationRepo) ThresholdsFor(ctx context.Context, relType string) (*relation.Thresholds, error) {
	const q = `
SELECT min_confidence_to_persist, min_confidence_to_surface
FROM relation_thresholds
WHERE relation_type = ?`

	var t relation.Thresholds
	err := r.db.QueryRowContext(ctx, q, relType).Scan(&t.Persist, &t.Surface)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading thresholds for relation type %s: %w", relType, err)
	}
	return &t, nil
}
