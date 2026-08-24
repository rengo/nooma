package memrepo

import (
	"context"
	"sort"
	"sync"
	"testing"
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
	// surfacedAt, respondedAt and resolution are the delivery half of the
	// row. ports.Trigger carries none of them: Create only ever writes an
	// armed row, and every one of these is written by a later transition.
	surfacedAt  *time.Time
	respondedAt *time.Time
	resolution  ports.TriggerResolution
}

var _ ports.TriggerRepo = (*Triggers)(nil)

// NewTriggers returns an empty, ready-to-use in-memory ports.TriggerRepo.
func NewTriggers() *Triggers {
	return &Triggers{triggers: make(map[string]storedTrigger)}
}

// EnsureUnit implements repocontract.TriggerHarness. It enforces no
// foreign key — every unit id is already a valid trigger target over this
// fake — so it does nothing at all. The store's own EnsureUnit
// (internal/store/sqlite/triggerrepo_integration_test.go) inserts a real
// units row instead; see repocontract.TriggerHarness for why the store
// needs the hook and this fake does not.
func (r *Triggers) EnsureUnit(_ *testing.T, _ string) {}

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

// Surface implements ports.TriggerRepo.
func (r *Triggers) Surface(_ context.Context, id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, ok := r.triggers[id]
	if !ok {
		return ports.ErrTriggerNotFound
	}
	if stored.status != ports.TriggerStatusFired || stored.surfacedAt != nil {
		return ports.ErrTriggerStatusConflict
	}
	when := at
	stored.surfacedAt = &when
	r.triggers[id] = stored
	return nil
}

// Undelivered implements ports.TriggerRepo.
func (r *Triggers) Undelivered(_ context.Context) ([]ports.DueTrigger, error) {
	return r.fired(func(s storedTrigger) bool { return s.surfacedAt == nil }), nil
}

// Delivered implements ports.TriggerRepo. Most recent first, so a caller
// resolving one answer takes the head.
func (r *Triggers) Delivered(_ context.Context) ([]ports.DueTrigger, error) {
	out := r.fired(func(s storedTrigger) bool { return s.surfacedAt != nil && s.respondedAt == nil })
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// fired collects every fired trigger matching keep, ordered by id.
func (r *Triggers) fired(keep func(storedTrigger) bool) []ports.DueTrigger {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]ports.DueTrigger, 0, len(r.triggers))
	for _, stored := range r.triggers {
		if stored.status != ports.TriggerStatusFired || !keep(stored) {
			continue
		}
		t := stored.trigger
		d := ports.DueTrigger{
			ID:               t.ID,
			UnitID:           copyString(t.UnitID),
			InterruptLevel:   copyFloat64(t.InterruptLevel),
			RecurrenceRule:   copyRule(t.RecurrenceRule),
			RecurrenceAnchor: copyAnchor(t.RecurrenceAnchor),
		}
		if t.FireAt != nil {
			d.FireAt = *t.FireAt
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Resolve implements ports.TriggerRepo.
func (r *Triggers) Resolve(_ context.Context, id string, to ports.TriggerResolution, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, ok := r.triggers[id]
	if !ok {
		return ports.ErrTriggerNotFound
	}
	if stored.surfacedAt == nil || stored.respondedAt != nil {
		return ports.ErrTriggerStatusConflict
	}
	when := at
	stored.respondedAt = &when
	stored.resolution = to
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

// Count returns the number of triggers currently held, at any status.
// Test-only, and it exists for the same reason memrepo.Units.Count does: a
// conformance test asserting "exactly one triggers row" cannot know an id
// to look up, and a counter is not a contract case.
func (r *Triggers) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.triggers)
}

// All returns every stored trigger's written value, ordered by id — the
// read a conformance test needs to assert what was persisted, which
// ports.TriggerRepo itself deliberately offers no method for. Test-only.
func (r *Triggers) All() []ports.Trigger {
	r.mu.Lock()
	defer r.mu.Unlock()

	all := make([]ports.Trigger, 0, len(r.triggers))
	for _, stored := range r.triggers {
		all = append(all, copyTrigger(stored.trigger))
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all
}
