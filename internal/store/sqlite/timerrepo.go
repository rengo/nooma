package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ncruces/go-sqlite3"

	"github.com/rengo/nooma/internal/ports"
)

// TimerRepo is the SQLite implementation of ports.TimerRepo — see
// TriggerRepo for the "answered twice" rule this type follows. It adds no
// migration: the timers table has existed since 0001_core_tables.sql.
type TimerRepo struct {
	db *sql.DB
}

// NewTimerRepo returns a ports.TimerRepo backed by v's already-migrated
// vault.
func NewTimerRepo(v *Vault) *TimerRepo {
	return &TimerRepo{db: v.db}
}

var _ ports.TimerRepo = (*TimerRepo)(nil)

// Create implements ports.TimerRepo. The pending literal is written here
// rather than left to the column default — TriggerRepo.Create's own
// reasoning.
func (r *TimerRepo) Create(ctx context.Context, t ports.Timer) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO timers (id, fire_at, action_text, status, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		t.ID, formatUnitTime(t.FireAt), stringPtrToNull(t.ActionText),
		string(ports.TimerStatusPending), formatUnitTime(t.CreatedAt),
	)
	if err != nil {
		if errors.Is(err, sqlite3.CONSTRAINT_PRIMARYKEY) {
			return ports.ErrTimerExists
		}
		return fmt.Errorf("insert timer %q: %w", t.ID, err)
	}
	return nil
}

// Due implements ports.TimerRepo.
//
// timers carries no index at all — migration 0001 declares one for
// triggers (idx_triggers_status_fire) and none here — so this is a full
// table scan. Accepted for v1 and named rather than mitigated: adding an
// index means a migration, and this slice ships none. On a personal
// vault's timer volume the scan is not the cost that matters; if it ever
// becomes one, the fix is a migration, not a query rewrite.
func (r *TimerRepo) Due(ctx context.Context, at time.Time) ([]ports.DueTimer, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, fire_at, action_text
		 FROM timers
		 WHERE status = ? AND fire_at <= ?
		 ORDER BY fire_at, id`,
		string(ports.TimerStatusPending), formatUnitTime(at),
	)
	if err != nil {
		return nil, fmt.Errorf("select due timers: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read-only query, nothing left to clean up on error

	due := make([]ports.DueTimer, 0)
	for rows.Next() {
		var (
			d          ports.DueTimer
			fireAt     string
			actionText sql.NullString
		)
		if err := rows.Scan(&d.ID, &fireAt, &actionText); err != nil {
			return nil, fmt.Errorf("scan due timer: %w", err)
		}
		if d.FireAt, err = time.Parse(unitTimeLayout, fireAt); err != nil {
			return nil, fmt.Errorf("timer %q: fire_at: %w", d.ID, err)
		}
		d.ActionText = nullStringToPtr(actionText)
		due = append(due, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select due timers: %w", err)
	}
	return due, nil
}

// Fire implements ports.TimerRepo. status and surfaced_at move in one
// statement, and the asymmetry with TriggerRepo.Fire is deliberate: a
// timer's firing IS its delivery, with no rendering step to wait on, so
// surfaced_at is written here where a trigger's stays NULL.
//
// rendered_text is untouched, not defaulted. m3d's fire-time rephrasing
// writes it when it has a caller; writing "" here would make the column's
// own NULL meaning unrecoverable.
func (r *TimerRepo) Fire(ctx context.Context, id string, at time.Time) error {
	return r.transition(ctx, id, ports.TimerStatusFired, &at)
}

// Cancel implements ports.TimerRepo — doc 02 §8's own vocabulary for a
// timer too stale to deliver. timers carries no cancelled_at column, so
// nothing but the status is written.
func (r *TimerRepo) Cancel(ctx context.Context, id string) error {
	return r.transition(ctx, id, ports.TimerStatusCancelled, nil)
}

// transition moves id out of pending, optionally stamping surfaced_at —
// see TriggerRepo.transition for why it is two statements and which one
// decides a race.
func (r *TimerRepo) transition(ctx context.Context, id string, to ports.TimerStatus, surfacedAt *time.Time) error {
	var current string
	err := r.db.QueryRowContext(ctx, `SELECT status FROM timers WHERE id = ?`, id).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ErrTimerNotFound
	}
	if err != nil {
		return fmt.Errorf("read timer %q status: %w", id, err)
	}
	if current != string(ports.TimerStatusPending) {
		return ports.ErrTimerStatusConflict
	}

	var res sql.Result
	if surfacedAt == nil {
		res, err = r.db.ExecContext(ctx,
			`UPDATE timers SET status = ? WHERE id = ? AND status = ?`,
			string(to), id, string(ports.TimerStatusPending))
	} else {
		res, err = r.db.ExecContext(ctx,
			`UPDATE timers SET status = ?, surfaced_at = ? WHERE id = ? AND status = ?`,
			string(to), formatUnitTime(*surfacedAt), id, string(ports.TimerStatusPending))
	}
	if err != nil {
		return fmt.Errorf("update timer %q status: %w", id, err)
	}
	return requireRowAffected(res, ports.ErrTimerStatusConflict)
}
