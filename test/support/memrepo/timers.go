package memrepo

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// Timers is an in-memory ports.TimerRepo — see Triggers for the isolation
// rule and for why the status lives beside the written value rather than
// inside it.
type Timers struct {
	mu     sync.Mutex
	timers map[string]storedTimer
}

type storedTimer struct {
	timer  ports.Timer
	status ports.TimerStatus
}

var _ ports.TimerRepo = (*Timers)(nil)

// NewTimers returns an empty, ready-to-use in-memory ports.TimerRepo.
func NewTimers() *Timers {
	return &Timers{timers: make(map[string]storedTimer)}
}

// Create implements ports.TimerRepo. The row is stored pending.
func (r *Timers) Create(_ context.Context, t ports.Timer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.timers[t.ID]; exists {
		return ports.ErrTimerExists
	}
	t.ActionText = copyString(t.ActionText)
	r.timers[t.ID] = storedTimer{timer: t, status: ports.TimerStatusPending}
	return nil
}

// Due implements ports.TimerRepo.
func (r *Timers) Due(_ context.Context, at time.Time) ([]ports.DueTimer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	due := make([]ports.DueTimer, 0, len(r.timers))
	for _, stored := range r.timers {
		if stored.status != ports.TimerStatusPending || stored.timer.FireAt.After(at) {
			continue
		}
		due = append(due, ports.DueTimer{
			ID:         stored.timer.ID,
			FireAt:     stored.timer.FireAt,
			ActionText: copyString(stored.timer.ActionText),
		})
	}

	sort.Slice(due, func(i, j int) bool {
		if !due[i].FireAt.Equal(due[j].FireAt) {
			return due[i].FireAt.Before(due[j].FireAt)
		}
		return due[i].ID < due[j].ID
	})
	return due, nil
}

// Fire implements ports.TimerRepo.
func (r *Timers) Fire(_ context.Context, id string, _ time.Time) error {
	return r.transition(id, ports.TimerStatusFired)
}

// Cancel implements ports.TimerRepo.
func (r *Timers) Cancel(_ context.Context, id string) error {
	return r.transition(id, ports.TimerStatusCancelled)
}

// transition is the pending precondition — see Triggers.transition for
// why the at parameter is dropped and where surfaced_at is asserted
// instead.
func (r *Timers) transition(id string, to ports.TimerStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, ok := r.timers[id]
	if !ok {
		return ports.ErrTimerNotFound
	}
	if stored.status != ports.TimerStatusPending {
		return ports.ErrTimerStatusConflict
	}
	stored.status = to
	r.timers[id] = stored
	return nil
}
