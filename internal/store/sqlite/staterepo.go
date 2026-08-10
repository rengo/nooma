package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// StateRepo is the SQLite-backed ports.StateRepo over current_state's
// consolidation-sourced rows (migration 0001:87-93, migration 0003's
// source column) — design §4.4.
type StateRepo struct {
	db *sql.DB
}

// NewStateRepo returns a ports.StateRepo backed by v's already-migrated
// vault.
func NewStateRepo(v *Vault) *StateRepo {
	return &StateRepo{db: v.db}
}

var _ ports.StateRepo = (*StateRepo)(nil)

// OpenHypothesis implements ports.StateRepo. Appends one current_state row
// with source = ports.StateSourceConsolidation, energy left NULL (design
// §4.4: writing a value there would feed doc 02 §7's digest care gate a
// number the watcher never observed) and active = 1. Append-only: this
// method never updates an existing row, matching StateRepo's own doc
// comment ("no update path, no removal-prefixed method").
func (r *StateRepo) OpenHypothesis(ctx context.Context, h ports.StateHypothesis) error {
	const q = `
INSERT INTO current_state (id, energy, mood, active, recorded_at, source)
VALUES (?, NULL, ?, 1, ?, ?)`

	_, err := r.db.ExecContext(ctx, q, h.ID, h.Mood, formatUnitTime(h.RecordedAt), ports.StateSourceConsolidation)
	if err != nil {
		return fmt.Errorf("opening hypothesis %q: %w", h.ID, err)
	}
	return nil
}

// LastHypothesisAt implements ports.StateRepo. Returns the recorded_at of
// the most recent row written with source = ports.StateSourceConsolidation,
// or nil when there is none — feeds consolidation.EvaluateLoad's
// lastHypothesisAt parameter directly (m2b §9 Q6).
func (r *StateRepo) LastHypothesisAt(ctx context.Context) (*time.Time, error) {
	const q = `
SELECT recorded_at FROM current_state
WHERE source = ?
ORDER BY recorded_at DESC, id DESC
LIMIT 1`

	var recordedAtText string
	err := r.db.QueryRowContext(ctx, q, ports.StateSourceConsolidation).Scan(&recordedAtText)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // "no row written yet" is the ordinary case (design §4.4), not an error
	}
	if err != nil {
		return nil, fmt.Errorf("reading last hypothesis: %w", err)
	}

	at, err := time.Parse(unitTimeLayout, recordedAtText)
	if err != nil {
		return nil, fmt.Errorf("current_state.recorded_at: %w", err)
	}
	return &at, nil
}
