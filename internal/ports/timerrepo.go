package ports

import (
	"context"
	"errors"
	"time"
)

// TimerStatus is timers.status's vocabulary — red-step declaration, see
// triggerrepo.go's own note.
type TimerStatus string

// The timers.status vocabulary (migration 0001:66).
const (
	TimerStatusPending   TimerStatus = "pending"
	TimerStatusFired     TimerStatus = "fired"
	TimerStatusCancelled TimerStatus = "cancelled"
)

// AllTimerStatuses is unimplemented in the red step.
func AllTimerStatuses() []TimerStatus { return nil }

// Timer is the write shape.
type Timer struct {
	ID         string
	FireAt     time.Time
	ActionText *string
	CreatedAt  time.Time
}

// DueTimer is the read shape.
type DueTimer struct {
	ID         string
	FireAt     time.Time
	ActionText *string
}

// TimerRepo is the repository port over timers.
type TimerRepo interface {
	Create(ctx context.Context, t Timer) error
	Due(ctx context.Context, at time.Time) ([]DueTimer, error)
	Fire(ctx context.Context, id string, at time.Time) error
	Cancel(ctx context.Context, id string) error
}

// Sentinel errors ports.TimerRepo implementations return.
var (
	ErrTimerNotFound       = errors.New("timer not found")
	ErrTimerExists         = errors.New("timer already exists")
	ErrTimerStatusConflict = errors.New("timer is not in the expected status")
)
