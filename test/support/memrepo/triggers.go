package memrepo

import (
	"context"
	"slices"
	"sort"
	"strings"
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
	firedAt     *time.Time
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
			Payload:          t.Payload,
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
func (r *Triggers) Fire(_ context.Context, id string, at time.Time) error {
	if err := r.transition(id, ports.TriggerStatusFired); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := r.triggers[id]
	stored.firedAt = &at
	r.triggers[id] = stored
	return nil
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
// Fire stores its at after the transition: FiredForUnit reads it.
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

// Lapse implements ports.TriggerRepo: fired and unsurfaced to expired,
// keeping the instant Fire was given.
func (r *Triggers) Lapse(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, ok := r.triggers[id]
	if !ok {
		return ports.ErrTriggerNotFound
	}
	if stored.status != ports.TriggerStatusFired || stored.surfacedAt != nil {
		return ports.ErrTriggerStatusConflict
	}
	stored.status = ports.TriggerStatusExpired
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
func (r *Triggers) Delivered(_ context.Context) ([]ports.OpenCheckIn, error) {
	fired := r.fired(func(s storedTrigger) bool { return s.surfacedAt != nil && s.respondedAt == nil })
	r.mu.Lock()
	out := make([]ports.OpenCheckIn, 0, len(fired))
	for _, t := range fired {
		out = append(out, ports.OpenCheckIn{DueTrigger: t, SurfacedAt: *r.triggers[t.ID].surfacedAt})
	}
	r.mu.Unlock()
	// surfaced_at DESC, id DESC, as the SQLite read orders.
	slices.SortFunc(out, func(a, b ports.OpenCheckIn) int {
		if c := b.SurfacedAt.Compare(a.SurfacedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out, nil
}

// ExpireUnanswered implements ports.TriggerRepo.
func (r *Triggers) ExpireUnanswered(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, ok := r.triggers[id]
	if !ok {
		return ports.ErrTriggerNotFound
	}
	if stored.status != ports.TriggerStatusFired || stored.surfacedAt == nil || stored.respondedAt != nil {
		return ports.ErrTriggerStatusConflict
	}
	stored.status = ports.TriggerStatusExpired
	r.triggers[id] = stored
	return nil
}

// Reask implements ports.TriggerRepo.
func (r *Triggers) Reask(_ context.Context, id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, ok := r.triggers[id]
	if !ok {
		return ports.ErrTriggerNotFound
	}
	if stored.status != ports.TriggerStatusFired || stored.surfacedAt == nil || stored.respondedAt != nil {
		return ports.ErrTriggerStatusConflict
	}
	when := at
	stored.surfacedAt = &when
	stored.trigger.Payload.Reasks++
	r.triggers[id] = stored
	return nil
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
			Payload:          t.Payload,
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
	if stored.status != ports.TriggerStatusFired || stored.surfacedAt == nil || stored.respondedAt != nil {
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

// copyAnchor deep-copies an anchor, weekday included.
//
// `v := *p` copies the struct, and for a struct of plain values that is a
// deep copy. prospection.Anchor holds a *time.Weekday, so the shallow copy
// hands back the caller's own pointer inside a fresh struct: a caller
// mutating it reaches into what this fake is storing. The SQLite repository
// cannot have that bug — it re-decodes JSON on every read — so the shared
// contract case is what forces the two to agree here.
func copyAnchor(p *prospection.Anchor) *prospection.Anchor {
	if p == nil {
		return nil
	}
	v := *p
	if p.Weekday != nil {
		wd := *p.Weekday
		v.Weekday = &wd
	}
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

// ArmedForUnit implements ports.TriggerRepo. Armed rows with a non-nil
// FireAt hanging off unitID, ordered by (fire_at, id).
func (r *Triggers) ArmedForUnit(_ context.Context, unitID string) ([]ports.DueTrigger, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]ports.DueTrigger, 0)
	for _, stored := range r.triggers {
		t := stored.trigger
		if stored.status != ports.TriggerStatusArmed || t.FireAt == nil || t.UnitID == nil || *t.UnitID != unitID {
			continue
		}
		out = append(out, ports.DueTrigger{
			ID: t.ID, UnitID: copyString(t.UnitID), FireAt: *t.FireAt, Payload: t.Payload,
			InterruptLevel: copyFloat64(t.InterruptLevel), RecurrenceRule: copyRule(t.RecurrenceRule),
			RecurrenceAnchor: copyAnchor(t.RecurrenceAnchor),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].FireAt.Equal(out[j].FireAt) {
			return out[i].FireAt.Before(out[j].FireAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// ArmedUnitIDs implements ports.TriggerRepo: the distinct unit ids of armed
// rows with a non-nil FireAt, in id order.
func (r *Triggers) ArmedUnitIDs(_ context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	seen := map[string]bool{}
	out := make([]string, 0)
	for _, stored := range r.triggers {
		t := stored.trigger
		if stored.status != ports.TriggerStatusArmed || t.FireAt == nil || t.UnitID == nil || seen[*t.UnitID] {
			continue
		}
		seen[*t.UnitID] = true
		out = append(out, *t.UnitID)
	}
	sort.Strings(out)
	return out, nil
}

// OneShotForUnit implements ports.TriggerRepo.
func (r *Triggers) OneShotForUnit(_ context.Context, unitID string) ([]ports.DueTrigger, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]ports.DueTrigger, 0)
	for _, stored := range r.triggers {
		t := stored.trigger
		if t.UnitID == nil || *t.UnitID != unitID || t.RecurrenceRule != nil || t.FireAt == nil {
			continue
		}
		out = append(out, ports.DueTrigger{ID: t.ID, UnitID: copyString(t.UnitID), FireAt: *t.FireAt, Payload: t.Payload, InterruptLevel: copyFloat64(t.InterruptLevel)})
	}
	return out, nil
}

// FiredForUnit implements ports.TriggerRepo: unitID's one-shot triggers
// still fired, with the instant Fire was given. A lapsed row is not one.
func (r *Triggers) FiredForUnit(_ context.Context, unitID string) ([]ports.FiredTrigger, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]ports.FiredTrigger, 0)
	for _, stored := range r.triggers {
		t := stored.trigger
		sent := stored.status == ports.TriggerStatusFired || (stored.status == ports.TriggerStatusExpired && stored.surfacedAt != nil)
		if !sent || stored.firedAt == nil || t.FireAt == nil || t.UnitID == nil || *t.UnitID != unitID || t.RecurrenceRule != nil {
			continue
		}
		out = append(out, ports.FiredTrigger{ID: t.ID, FireAt: *t.FireAt, FiredAt: *stored.firedAt, Payload: t.Payload})
	}
	return out, nil
}

// Reschedule implements ports.TriggerRepo, under the same armed
// precondition transition enforces.
func (r *Triggers) Reschedule(_ context.Context, id string, m ports.TriggerMove) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	stored, ok := r.triggers[id]
	if !ok {
		return ports.ErrTriggerNotFound
	}
	if stored.status != ports.TriggerStatusArmed {
		return ports.ErrTriggerStatusConflict
	}
	fireAt := m.FireAt
	stored.trigger.FireAt = &fireAt
	stored.trigger.Payload = m.Payload
	if m.RecurrenceAnchor != nil {
		stored.trigger.RecurrenceAnchor = copyAnchor(m.RecurrenceAnchor)
	}
	r.triggers[id] = stored
	return nil
}
