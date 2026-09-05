package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// PendingQuestionRepo is the SQLite-backed ports.PendingQuestionRepo over
// pending_questions (migration 0004) — design D6's "answered twice"
// standing rule: the same repocontract.RunPendingQuestionRepo suite that
// runs against test/support/memrepo's fake at L2 runs against this type at
// L3, over a real migrated vault.
type PendingQuestionRepo struct {
	db *sql.DB
}

// NewPendingQuestionRepo returns a ports.PendingQuestionRepo backed by v's
// already-migrated vault.
func NewPendingQuestionRepo(v *Vault) *PendingQuestionRepo {
	return &PendingQuestionRepo{db: v.db}
}

var _ ports.PendingQuestionRepo = (*PendingQuestionRepo)(nil)

// Create implements ports.PendingQuestionRepo. INSERT ... SELECT ... WHERE
// EXISTS is the referential check ADR-0027 chose over a foreign key: it
// costs nothing at read time and, unlike a constraint, places no obligation
// on a later DELETE FROM relations (the FK was refused precisely so a
// rejection's delete cannot cascade into the question row that records
// it).
func (r *PendingQuestionRepo) Create(ctx context.Context, q ports.PendingQuestion) error {
	const query = `
INSERT INTO pending_questions (id, kind, relation_id, created_at)
SELECT ?, ?, ?, ?
WHERE EXISTS (SELECT 1 FROM relations WHERE id = ?)`

	res, err := r.db.ExecContext(ctx, query,
		q.ID, string(q.Kind), q.RelationID, formatUnitTime(q.CreatedAt), q.RelationID)
	if err != nil {
		return fmt.Errorf("inserting pending question %q: %w", q.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("pending question %q: rows affected: %w", q.ID, err)
	}
	if n == 0 {
		return ports.ErrRelationNotFound
	}
	return nil
}

// Unasked implements ports.PendingQuestionRepo — the digest's own queue:
// oldest created_at first, then id, matching idx_pending_questions_unasked.
func (r *PendingQuestionRepo) Unasked(ctx context.Context) ([]ports.RelationQuestion, error) {
	return r.relationQuestions(ctx,
		`pq.asked_at IS NULL AND pq.resolved_at IS NULL`, `pq.created_at, pq.id`)
}

// Open implements ports.PendingQuestionRepo — the disambiguation pool and
// the expiry sweep: most recently asked first, then id, mirroring
// ports.TriggerRepo.Delivered's own order.
func (r *PendingQuestionRepo) Open(ctx context.Context) ([]ports.RelationQuestion, error) {
	return r.relationQuestions(ctx,
		`pq.asked_at IS NOT NULL AND pq.resolved_at IS NULL`, `pq.asked_at DESC, pq.id DESC`)
}

// relationQuestions is the shared read behind Unasked and Open: one query
// joining pending_questions to relations and both endpoints' units, so a
// question whose relation was deleted is skipped rather than surfaced or
// failed on (R8's mitigation) — never relations, then units, then a zip in
// Go (RelationRepo.Evidence's own stated reason, design §3.2).
func (r *PendingQuestionRepo) relationQuestions(ctx context.Context, where, order string) ([]ports.RelationQuestion, error) {
	query := `
SELECT pq.id, pq.relation_id, r.type, r.from_unit_id, r.to_unit_id, uf.content, ut.content, pq.created_at, pq.asked_at
FROM pending_questions pq
JOIN relations r ON r.id = pq.relation_id
JOIN units uf ON uf.id = r.from_unit_id
JOIN units ut ON ut.id = r.to_unit_id
WHERE ` + where + `
ORDER BY ` + order

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("reading pending questions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ports.RelationQuestion, 0)
	for rows.Next() {
		var (
			rq          ports.RelationQuestion
			createdAtTx string
			askedAtTx   sql.NullString
		)
		if err := rows.Scan(&rq.ID, &rq.RelationID, &rq.RelationType, &rq.FromUnitID, &rq.ToUnitID,
			&rq.FromContent, &rq.ToContent, &createdAtTx, &askedAtTx); err != nil {
			return nil, fmt.Errorf("scanning a pending question row: %w", err)
		}
		at, err := time.Parse(unitTimeLayout, createdAtTx)
		if err != nil {
			return nil, fmt.Errorf("pending question %q: created_at: %w", rq.ID, err)
		}
		rq.CreatedAt = at
		if askedAtTx.Valid {
			asked, err := time.Parse(unitTimeLayout, askedAtTx.String)
			if err != nil {
				return nil, fmt.Errorf("pending question %q: asked_at: %w", rq.ID, err)
			}
			rq.AskedAt = &asked
		}
		out = append(out, rq)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading pending questions: %w", err)
	}
	return out, nil
}

// MarkAsked implements ports.PendingQuestionRepo. One UPDATE under an
// asked_at IS NULL precondition — a question is asked exactly once.
func (r *PendingQuestionRepo) MarkAsked(ctx context.Context, id string, at time.Time) error {
	return r.transition(ctx, id,
		`UPDATE pending_questions SET asked_at = ? WHERE id = ? AND asked_at IS NULL`,
		formatUnitTime(at), id)
}

// Confirm implements ports.PendingQuestionRepo.
func (r *PendingQuestionRepo) Confirm(ctx context.Context, id string, at time.Time) error {
	return r.resolve(ctx, id, at, ports.QuestionConfirmed)
}

// Reject implements ports.PendingQuestionRepo.
func (r *PendingQuestionRepo) Reject(ctx context.Context, id string, at time.Time) error {
	return r.resolve(ctx, id, at, ports.QuestionRejected)
}

// Expire implements ports.PendingQuestionRepo.
func (r *PendingQuestionRepo) Expire(ctx context.Context, id string, at time.Time) error {
	return r.resolve(ctx, id, at, ports.QuestionExpired)
}

// resolve is Confirm/Reject/Expire's shared statement: resolved_at and
// resolution are set together, in ONE statement, under a resolved_at IS
// NULL precondition — no method above takes a resolution parameter, so the
// literal lives in one of exactly three call sites inside this file and
// never crosses the port.
func (r *PendingQuestionRepo) resolve(ctx context.Context, id string, at time.Time, resolution ports.QuestionResolution) error {
	return r.transition(ctx, id,
		`UPDATE pending_questions SET resolved_at = ?, resolution = ? WHERE id = ? AND resolved_at IS NULL`,
		formatUnitTime(at), string(resolution), id)
}

// transition distinguishes "no such question" from "the precondition did
// not hold" — the two-statement shape ports.TriggerRepo's own
// implementation uses (triggerrepo.go's guardedUpdate), for the identical
// reason: the UPDATE's own WHERE is what decides a race, and the SELECT
// exists only to make the not-found case honest.
func (r *PendingQuestionRepo) transition(ctx context.Context, id, query string, args ...any) error {
	var exists int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM pending_questions WHERE id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrQuestionNotFound
	}
	if err != nil {
		return fmt.Errorf("reading pending question %q: %w", id, err)
	}

	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating pending question %q: %w", id, err)
	}
	return requireRowAffected(res, ports.ErrQuestionStatusConflict)
}
