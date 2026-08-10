package sqlite

import (
	"context"
	"database/sql"
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
	rows, err := r.db.QueryContext(ctx, selfBeliefSelectColumns+` FROM self_beliefs WHERE status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("reading active beliefs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []ports.Belief{}
	for rows.Next() {
		b, err := scanBelief(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning an active belief row: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading active beliefs: %w", err)
	}
	return out, nil
}

// UpsertByTopicKey implements ports.SelfModelRepo. Conflicts on
// self_beliefs.topic_key (UNIQUE, migration 0001:75) — RelationRepo.Upsert's
// own pattern (design §4.3, spec R2.1). id is never SET on conflict, so a
// second write over the same topic_key updates the FIRST row's identity in
// place rather than creating a duplicate.
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
  updated_at         = excluded.updated_at`

	_, err := r.db.ExecContext(ctx, q,
		b.ID, string(b.Facet), b.TopicKey, b.Content, b.Confidence, b.Origin,
		stringPtrToNull(b.SourceUnitID), b.Status, formatUnitTime(b.LastReinforcedAt),
		formatUnitTime(b.CreatedAt), formatUnitTime(b.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upserting belief for topic_key %q: %w", b.TopicKey, err)
	}
	return nil
}

// ReinforceByID implements ports.SelfModelRepo. Updates only confidence and
// last_reinforced_at, leaving topic_key, content, facet, origin and
// source_unit_id unchanged — spec R2.2. Returns ports.ErrBeliefNotFound
// rather than creating a row when id does not exist.
func (r *SelfModelRepo) ReinforceByID(ctx context.Context, id string, confidence float64, at time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE self_beliefs SET confidence = ?, last_reinforced_at = ? WHERE id = ?`,
		confidence, formatUnitTime(at), id,
	)
	if err != nil {
		return fmt.Errorf("reinforcing belief %q: %w", id, err)
	}
	return requireRowAffected(res, ports.ErrBeliefNotFound)
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
		facet                                              string
		sourceUnitID                                       sql.NullString
		lastReinforcedAtText, createdAtText, updatedAtText string
	)

	err := row.Scan(
		&b.ID, &facet, &b.TopicKey, &b.Content, &b.Confidence, &b.Origin,
		&sourceUnitID, &b.Status, &lastReinforcedAtText, &createdAtText, &updatedAtText,
	)
	if err != nil {
		return ports.Belief{}, err
	}

	b.Facet = selfmodel.Facet(facet)
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
