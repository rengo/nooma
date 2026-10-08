package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
)

// SelfModelRepo is the SQLite-backed ports.SelfModelRepo over self_beliefs
// (migration 0001:72-85) — design §4.3, spec R2.1-R2.3.
type SelfModelRepo struct {
	db *sql.DB
}

// NewSelfModelRepo returns a ports.SelfModelRepo backed by v's
// already-migrated vault.
func NewSelfModelRepo(v *Vault) *SelfModelRepo {
	return &SelfModelRepo{db: v.db}
}

var _ ports.SelfModelRepo = (*SelfModelRepo)(nil)

// selfBeliefSelectColumns is shared by ActiveBeliefs and ReinforceByID's own
// verification query — one column list, one place.
const selfBeliefSelectColumns = `SELECT id, facet, topic_key, content, confidence, origin,
	source_unit_id, status, last_reinforced_at, created_at, updated_at`

// ActiveBeliefs implements ports.SelfModelRepo. Filters status = 'active'
// only, every facet included — no status parameter on the call (spec R2.3,
// design §4.3): derive's two dedup defenses and pattern_eval's
// EvaluateStagnation share this one read; the FacetGoal filter
// EvaluateStagnation applies is core's own job, not this port's.
func (r *SelfModelRepo) ActiveBeliefs(ctx context.Context) ([]ports.Belief, error) {
	return r.readBeliefs(ctx, "active", selfBeliefSelectColumns+` FROM self_beliefs WHERE status = 'active'`)
}

// RetiredBeliefs implements ports.SelfModelRepo. The status is a literal in
// the statement, never a parameter — ActiveBeliefs's own rule.
func (r *SelfModelRepo) RetiredBeliefs(ctx context.Context) ([]ports.Belief, error) {
	return r.readBeliefs(ctx, "retired", selfBeliefSelectColumns+` FROM self_beliefs WHERE status = 'retired'`)
}

// readBeliefs runs a belief-list query and scans every row; what names the
// read in an error.
func (r *SelfModelRepo) readBeliefs(ctx context.Context, what, query string) ([]ports.Belief, error) {
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("reading %s beliefs: %w", what, err)
	}
	defer func() { _ = rows.Close() }()

	out := []ports.Belief{}
	for rows.Next() {
		b, err := scanBelief(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning a %s belief row: %w", what, err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading %s beliefs: %w", what, err)
	}
	return out, nil
}

// BeliefByID implements ports.SelfModelRepo. Any status.
func (r *SelfModelRepo) BeliefByID(ctx context.Context, id string) (ports.Belief, error) {
	b, err := scanBelief(r.db.QueryRowContext(ctx, selfBeliefSelectColumns+` FROM self_beliefs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ports.Belief{}, ports.ErrBeliefNotFound
	}
	if err != nil {
		return ports.Belief{}, fmt.Errorf("reading belief %q: %w", id, err)
	}
	return b, nil
}

// SetStatus implements ports.SelfModelRepo. from is an optimistic-concurrency
// precondition, not a validation (UnitRepo.SetStatus's own shape): it is the
// UPDATE's own WHERE clause, so a writer landing between a caller's read and
// this call is refused by the statement itself.
func (r *SelfModelRepo) SetStatus(ctx context.Context, id string, from, to selfmodel.Status, at time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE self_beliefs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		string(to), formatUnitTime(at), id, string(from),
	)
	if err != nil {
		return fmt.Errorf("update belief %q status: %w", id, err)
	}
	return r.explainZeroRows(ctx, res, id)
}

// EditContent implements ports.SelfModelRepo. The origin is a literal in the
// statement and the signature has no origin parameter, so an edit that does
// not mark the row user_stated is not expressible. Only an ACTIVE belief
// whose content is still from is written (compare-and-swap).
func (r *SelfModelRepo) EditContent(ctx context.Context, id, from, to string, at time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE self_beliefs SET content = ?, origin = 'user_stated', updated_at = ?
		 WHERE id = ? AND status = 'active' AND content = ?`,
		to, formatUnitTime(at), id, from,
	)
	if err != nil {
		return fmt.Errorf("edit belief %q content: %w", id, err)
	}
	return r.explainZeroRows(ctx, res, id)
}

// explainZeroRows turns a guarded UPDATE's result into the port's errors:
// nil when a row changed; otherwise ErrBeliefNotFound when no row has id and
// ErrBeliefStatusConflict when the row exists but a guard refused it. The
// disambiguating read runs only after a zero-row UPDATE, so the guard in the
// statement is the single decision and nothing can slip between a check and
// a write.
func (r *SelfModelRepo) explainZeroRows(ctx context.Context, res sql.Result, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n > 0 {
		return nil
	}

	var present int
	err = r.db.QueryRowContext(ctx, `SELECT 1 FROM self_beliefs WHERE id = ?`, id).Scan(&present)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrBeliefNotFound
	}
	if err != nil {
		return fmt.Errorf("read belief %q: %w", id, err)
	}
	return ports.ErrBeliefStatusConflict
}

// UpsertByTopicKey implements ports.SelfModelRepo. Conflicts on
// self_beliefs.topic_key (UNIQUE, migration 0001:75) — RelationRepo.Upsert's
// own pattern (design §4.3, spec R2.1). id is never SET on conflict, so a
// second write over the same topic_key updates the FIRST row's identity in
// place rather than creating a duplicate.
//
// The conflict arm is guarded: it overwrites only a row that is active AND
// derived. Over a retired row or a seed/user_stated one the WHERE is false,
// SQLite reports zero rows changed (probed by
// TestSelfModelRepo_DoUpdateWhereReportsChanges) and the result is
// ports.ErrBeliefProtected with the row untouched.
func (r *SelfModelRepo) UpsertByTopicKey(ctx context.Context, b ports.Belief) error {
	const q = `
INSERT INTO self_beliefs (id, facet, topic_key, content, confidence, origin,
                           source_unit_id, status, last_reinforced_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (topic_key) DO UPDATE SET
  content            = excluded.content,
  confidence         = excluded.confidence,
  facet              = excluded.facet,
  origin             = excluded.origin,
  source_unit_id     = excluded.source_unit_id,
  status             = excluded.status,
  last_reinforced_at = excluded.last_reinforced_at,
  updated_at         = excluded.updated_at
WHERE self_beliefs.status = 'active' AND self_beliefs.origin = 'derived'`

	res, err := r.db.ExecContext(ctx, q,
		b.ID, string(b.Facet), b.TopicKey, b.Content, b.Confidence, string(b.Origin),
		stringPtrToNull(b.SourceUnitID), string(b.Status), formatUnitTime(b.LastReinforcedAt),
		formatUnitTime(b.CreatedAt), formatUnitTime(b.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upserting belief for topic_key %q: %w", b.TopicKey, err)
	}
	return requireRowAffected(res, ports.ErrBeliefProtected)
}

// ReinforceByID implements ports.SelfModelRepo. Updates only confidence and
// last_reinforced_at, leaving topic_key, content, facet, origin and
// source_unit_id unchanged — spec R2.2. Returns ports.ErrBeliefNotFound
// rather than creating a row when id does not exist, and
// ports.ErrBeliefStatusConflict for a belief that is not active: the
// statement's own status guard keeps a retired belief from being reinforced.
func (r *SelfModelRepo) ReinforceByID(ctx context.Context, id string, confidence float64, at time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE self_beliefs SET confidence = ?, last_reinforced_at = ? WHERE id = ? AND status = 'active'`,
		confidence, formatUnitTime(at), id,
	)
	if err != nil {
		return fmt.Errorf("reinforcing belief %q: %w", id, err)
	}
	return r.explainZeroRows(ctx, res, id)
}

// selfBeliefRow is satisfied by *sql.Rows, following unitrepo.go's unitRow
// precedent.
type selfBeliefRow interface {
	Scan(dest ...any) error
}

// scanBelief reads one self_beliefs row into a ports.Belief, in
// selfBeliefSelectColumns' exact column order.
func scanBelief(row selfBeliefRow) (ports.Belief, error) {
	var (
		b                                                  ports.Belief
		facet, origin, status                              string
		sourceUnitID                                       sql.NullString
		lastReinforcedAtText, createdAtText, updatedAtText string
	)

	err := row.Scan(
		&b.ID, &facet, &b.TopicKey, &b.Content, &b.Confidence, &origin,
		&sourceUnitID, &status, &lastReinforcedAtText, &createdAtText, &updatedAtText,
	)
	if err != nil {
		return ports.Belief{}, err
	}

	b.Facet = selfmodel.Facet(facet)
	b.Origin = selfmodel.Origin(origin)
	b.Status = selfmodel.Status(status)
	if sourceUnitID.Valid {
		b.SourceUnitID = &sourceUnitID.String
	}
	if b.LastReinforcedAt, err = time.Parse(unitTimeLayout, lastReinforcedAtText); err != nil {
		return ports.Belief{}, fmt.Errorf("belief %q: last_reinforced_at: %w", b.ID, err)
	}
	if b.CreatedAt, err = time.Parse(unitTimeLayout, createdAtText); err != nil {
		return ports.Belief{}, fmt.Errorf("belief %q: created_at: %w", b.ID, err)
	}
	if b.UpdatedAt, err = time.Parse(unitTimeLayout, updatedAtText); err != nil {
		return ports.Belief{}, fmt.Errorf("belief %q: updated_at: %w", b.ID, err)
	}
	return b, nil
}
