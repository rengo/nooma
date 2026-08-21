package memrepo

import (
	"context"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// Triggers is the red step's no-op ports.TriggerRepo: every method returns
// a zero value so the contract suite compiles and fails on behaviour, not
// on a missing symbol (PR 1, commit 1).
type Triggers struct{}

var _ ports.TriggerRepo = (*Triggers)(nil)

// NewTriggers returns the red step's no-op fake.
func NewTriggers() *Triggers { return &Triggers{} }

// Create implements ports.TriggerRepo.
func (r *Triggers) Create(_ context.Context, _ ports.Trigger) error { return nil }

// Due implements ports.TriggerRepo.
func (r *Triggers) Due(_ context.Context, _ time.Time) ([]ports.DueTrigger, error) {
	return nil, nil
}

// Fire implements ports.TriggerRepo.
func (r *Triggers) Fire(_ context.Context, _ string, _ time.Time) error { return nil }

// Expire implements ports.TriggerRepo.
func (r *Triggers) Expire(_ context.Context, _ string) error { return nil }
