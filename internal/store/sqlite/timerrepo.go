package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// TimerRepo is the SQLite implementation of ports.TimerRepo — red-step
// stub, see TriggerRepo's own note.
type TimerRepo struct {
	db *sql.DB
}

var _ ports.TimerRepo = (*TimerRepo)(nil)

// NewTimerRepo returns a ports.TimerRepo backed by v's already-migrated
// vault.
func NewTimerRepo(v *Vault) *TimerRepo {
	return &TimerRepo{db: v.db}
}

// Create implements ports.TimerRepo.
func (r *TimerRepo) Create(_ context.Context, _ ports.Timer) error { return nil }

// Due implements ports.TimerRepo.
func (r *TimerRepo) Due(_ context.Context, _ time.Time) ([]ports.DueTimer, error) {
	return nil, nil
}

// Fire implements ports.TimerRepo.
func (r *TimerRepo) Fire(_ context.Context, _ string, _ time.Time) error { return nil }

// Cancel implements ports.TimerRepo.
func (r *TimerRepo) Cancel(_ context.Context, _ string) error { return nil }
