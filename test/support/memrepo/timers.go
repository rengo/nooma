package memrepo

import (
	"context"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// Timers is the red step's no-op ports.TimerRepo — see Triggers's own note.
type Timers struct{}

var _ ports.TimerRepo = (*Timers)(nil)

// NewTimers returns the red step's no-op fake.
func NewTimers() *Timers { return &Timers{} }

// Create implements ports.TimerRepo.
func (r *Timers) Create(_ context.Context, _ ports.Timer) error { return nil }

// Due implements ports.TimerRepo.
func (r *Timers) Due(_ context.Context, _ time.Time) ([]ports.DueTimer, error) { return nil, nil }

// Fire implements ports.TimerRepo.
func (r *Timers) Fire(_ context.Context, _ string, _ time.Time) error { return nil }

// Cancel implements ports.TimerRepo.
func (r *Timers) Cancel(_ context.Context, _ string) error { return nil }
