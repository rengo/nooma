package memrepo

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// Triggers is an in-memory ports.TriggerRepo. The zero value is not usable
// — call NewTriggers. Two instances constructed by separate NewTriggers
// calls share no state, matching memrepo.Units's own isolation rule.
type Triggers struct {
	mu       sync.Mutex
	triggers map[string]storedTrigger
}

// storedTrigger is one row: the written value plus the status column,
// which ports.Trigger deliberately does not carry — Create only ever
// writes an armed row, and the status literal never crosses the port.
type storedTrigger struct {
	trigger ports.Trigger
	status  ports.TriggerStatus
}

var _ ports.TriggerRepo = (*Triggers)(nil)

// NewTriggers returns an empty, ready-to-use in-memory ports.TriggerRepo.
func NewTriggers() *Triggers {
	return &Triggers{triggers: make(map[string]storedTrigger)}
}

// Create implements ports.TriggerRepo. The row is stored armed, and every
// pointer field is copied by value: a caller that mutates its own
// *float64 after Create must not reach into this store.
func (r *Triggers) Create(_ context.Context, t ports.Trigger) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.triggers[t.ID]; exists {
		return ports.ErrTriggerExists
	}
	r.triggers[t.ID] = storedTrigger{trigger: copyTrigger(t), status: ports.TriggerStatusArmed}
	return nil
}

// Due implements ports.TriggerRepo. Armed rows with a non-nil FireAt at or
// before at, ordered by (fire_at, id), each with freshly allocated
// pointers.
func (r *Triggers) Due(_ context.Context, at time.Time) ([]ports.DueTrigger, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	due := make([]ports.DueTrigger, 0, len(r.triggers))
	for _, stored := range r.triggers {
		if stored.status != ports.TriggerStatusArmed {
			continue
		}
		t := stored.trigger
		if t.FireAt == nil || t.FireAt.After(at) {
			continue
		}
		due = append(due, ports.DueTrigger{
			ID:               t.ID,
			UnitID:           copyString(t.UnitID),
			FireAt:           *t.FireAt,
			InterruptLevel:   copyFloat64(t.InterruptLevel),
			RecurrenceRule:   copyRule(t.RecurrenceRule),
			RecurrenceAnchor: copyAnchor(t.RecurrenceAnchor),
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

// Fire implements ports.TriggerRepo.
func (r *Triggers) Fire(_ context.Context, id string, _ time.Time) error {
	return r.transition(id, ports.TriggerStatusFired)
}

// Expire implements ports.TriggerRepo.
func (r *Triggers) Expire(_ context.Context, id string) error {
	return r.transition(id, ports.TriggerStatusExpired)
}

// transition is the armed precondition, enforced once for both callers
// under the same mutex — the real implementation keeps it in a single
// UPDATE's WHERE clause for the same reason: two scans racing over one due
// trigger must produce exactly one winner.
//
// The at parameter Fire receives is dropped here on purpose: fired_at and
// surfaced_at are columns this fake has no reader for, and storing a value
// nothing can observe would be a promise the fake cannot keep. The real
// repository's write of fired_at is asserted at L3, against raw SQL.
func (r *Triggers) transition(id string, to ports.TriggerStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, ok := r.triggers[id]
	if !ok {
		return ports.ErrTriggerNotFound
	}
	if stored.status != ports.TriggerStatusArmed {
		return ports.ErrTriggerStatusConflict
	}
	stored.status = to
	r.triggers[id] = stored
	return nil
}

// copyTrigger returns t with every pointer field freshly allocated.
func copyTrigger(t ports.Trigger) ports.Trigger {
	t.UnitID = copyString(t.UnitID)
	t.InterruptLevel = copyFloat64(t.InterruptLevel)
	t.FireAt = copyTime(t.FireAt)
	t.RecurrenceRule = copyRule(t.RecurrenceRule)
	t.RecurrenceAnchor = copyAnchor(t.RecurrenceAnchor)
	return t
}

func copyString(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyFloat64(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyTime(p *time.Time) *time.Time {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyRule(p *prospection.Rule) *prospection.Rule {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyAnchor(p *prospection.Anchor) *prospection.Anchor {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
