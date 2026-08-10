package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rengo/nooma/internal/core/consolidation"
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

// Evidence implements ports.RelationRepo — the join design §4.2 fixed:
// every relation, joined to both endpoints' own last_touched_at, in one
// query — never relations, then units, then a zip in brain (design §4.1's
// own reason against that shape, spec R3.5).
//
// from_unit_id, to_unit_id, type and confidence ride along in the same
// SELECT (m2c PR8's own widening, consolidation.RelationEvidence's own doc
// comment): brain's persist step needs them to target
// ports.RelationRepo.Upsert's (from, to, type) conflict key correctly, and
// they cost nothing extra here — the join already has every column.
func (r *RelationRepo) Evidence(ctx context.Context) ([]consolidation.RelationEvidence, error) {
	const q = `
SELECT r.id, r.from_unit_id, r.to_unit_id, r.type, r.strength, r.confidence, uf.last_touched_at, ut.last_touched_at
FROM relations r
JOIN units uf ON uf.id = r.from_unit_id
JOIN units ut ON ut.id = r.to_unit_id
ORDER BY r.id`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("reading relation evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []consolidation.RelationEvidence
	for rows.Next() {
		var e consolidation.RelationEvidence
		var fromText, toText string
		if err := rows.Scan(&e.RelationID, &e.FromUnitID, &e.ToUnitID, &e.Type, &e.Strength, &e.Confidence, &fromText, &toText); err != nil {
			return nil, fmt.Errorf("scanning a relation evidence row: %w", err)
		}
		fromAt, err := time.Parse(unitTimeLayout, fromText)
		if err != nil {
			return nil, fmt.Errorf("relation %q: from_unit_id last_touched_at: %w", e.RelationID, err)
		}
		toAt, err := time.Parse(unitTimeLayout, toText)
		if err != nil {
			return nil, fmt.Errorf("relation %q: to_unit_id last_touched_at: %w", e.RelationID, err)
		}
		e.FromLastTouchedAt = fromAt
		e.ToLastTouchedAt = toAt
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading relation evidence: %w", err)
	}
	return out, nil
}

// ExistingPairs implements ports.RelationRepo — bounded by the candidate
// set pairs names (design §4.2, spec R3.6, ConnectSourceLimit x
// ConnectCandidateK = 100): one query over relations touching any candidate
// unit id, canonicalized in Go and matched against pairs, never a
// full-table scan over every relation in the vault. A pair with no stored
// relation is left absent from the returned map, never a false-valued
// entry.
func (r *RelationRepo) ExistingPairs(ctx context.Context, pairs []consolidation.Pair) (map[consolidation.Pair]bool, error) {
	out := make(map[consolidation.Pair]bool, len(pairs))
	if len(pairs) == 0 {
		return out, nil
	}

	want := make(map[consolidation.Pair]bool, len(pairs))
	idSet := make(map[string]struct{}, len(pairs)*2)
	for _, p := range pairs {
		want[p] = true
		idSet[p.From] = struct{}{}
		idSet[p.To] = struct{}{}
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)*2)
	for _, id := range ids {
		args = append(args, id)
	}
	for _, id := range ids {
		args = append(args, id)
	}

	q := `SELECT from_unit_id, to_unit_id FROM relations WHERE from_unit_id IN (` +
		placeholders + `) OR to_unit_id IN (` + placeholders + `)`

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("reading existing pairs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			return nil, fmt.Errorf("scanning an existing-pairs row: %w", err)
		}
		pair := consolidation.CanonicalPair(from, to)
		if want[pair] {
			out[pair] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading existing pairs: %w", err)
	}
	return out, nil
}
