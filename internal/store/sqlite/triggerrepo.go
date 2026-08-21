package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// TriggerRepo is the SQLite implementation of ports.TriggerRepo — red-step
// stub (PR 2, commit 1): every method is a no-op returning a zero value, so
// the L3 suite compiles and fails on behaviour rather than on a missing
// symbol.
type TriggerRepo struct {
	db *sql.DB
}

var _ ports.TriggerRepo = (*TriggerRepo)(nil)

// NewTriggerRepo returns a ports.TriggerRepo backed by v's already-migrated
// vault. triggers exists since migration 0001; this repository adds none.
func NewTriggerRepo(v *Vault) *TriggerRepo {
	return &TriggerRepo{db: v.db}
}

// Create implements ports.TriggerRepo.
func (r *TriggerRepo) Create(_ context.Context, _ ports.Trigger) error { return nil }

// Due implements ports.TriggerRepo.
func (r *TriggerRepo) Due(_ context.Context, _ time.Time) ([]ports.DueTrigger, error) {
	return nil, nil
}

// Fire implements ports.TriggerRepo.
func (r *TriggerRepo) Fire(_ context.Context, _ string, _ time.Time) error { return nil }

// Expire implements ports.TriggerRepo.
func (r *TriggerRepo) Expire(_ context.Context, _ string) error { return nil }
