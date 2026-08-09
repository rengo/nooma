// Package memrepo is the in-memory ports.UnitRepo test double (design D6,
// spec R3.3), for L2 use (test/conformance, this PR) and L3/L4 and
// internal/brain's own tests later. It ships no SQL, no file I/O, and no
// production import path — the established test/support precedent
// (test/support/schema, test/support/goldenset) — so internal/core never
// sees test-only code.
//
// Package memrepo is untagged: both the untagged L2 suite
// (test/conformance) and internal/brain's own package tests can import it.
package memrepo

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
	"github.com/rengo/nooma/internal/ports"
)

// Units is an in-memory ports.UnitRepo. The zero value is not usable — call
// NewUnits. Two instances constructed by separate NewUnits calls share no
// state (R3.3's isolation MUST): each holds its own map behind its own
// mutex.
type Units struct {
	mu    sync.Mutex
	units map[string]unit.Unit
}

// NewUnits returns an empty, ready-to-use in-memory ports.UnitRepo. Every
// call returns an independent instance — see Units's doc comment.
func NewUnits() *Units {
	return &Units{units: make(map[string]unit.Unit)}
}

// Create implements ports.UnitRepo.
func (r *Units) Create(_ context.Context, u unit.Unit) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.units[u.ID]; exists {
		return ports.ErrUnitExists
	}
	r.units[u.ID] = deepCopy(u)
	return nil
}

// ByID implements ports.UnitRepo.
func (r *Units) ByID(_ context.Context, id string) (unit.Unit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.units[id]
	if !ok {
		return unit.Unit{}, ports.ErrUnitNotFound
	}
	return deepCopy(u), nil
}

// LiveByIDs implements ports.UnitRepo.
func (r *Units) LiveByIDs(_ context.Context, ids []string) ([]unit.Unit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	live := make([]unit.Unit, 0, len(ids))
	for _, id := range ids {
		u, ok := r.units[id]
		if !ok || !u.Status.IsLive() {
			continue
		}
		live = append(live, deepCopy(u))
	}
	return live, nil
}

// UpdateContent implements ports.UnitRepo.
func (r *Units) UpdateContent(_ context.Context, id, content string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.units[id]
	if !ok {
		return ports.ErrUnitNotFound
	}
	u.Content = content
	u.UpdatedAt = at
	r.units[id] = u
	return nil
}

// UpdateEventAt implements ports.UnitRepo.
func (r *Units) UpdateEventAt(_ context.Context, id string, eventAt, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.units[id]
	if !ok {
		return ports.ErrUnitNotFound
	}
	u.EventAt = &eventAt
	u.UpdatedAt = at
	r.units[id] = u
	return nil
}

// UpdateDueAt implements ports.UnitRepo.
func (r *Units) UpdateDueAt(_ context.Context, id string, dueAt, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.units[id]
	if !ok {
		return ports.ErrUnitNotFound
	}
	u.DueAt = &dueAt
	u.UpdatedAt = at
	r.units[id] = u
	return nil
}

// SetStatus implements ports.UnitRepo.
func (r *Units) SetStatus(_ context.Context, id string, from, to unit.Status, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.units[id]
	if !ok {
		return ports.ErrUnitNotFound
	}
	if u.Status != from {
		return ports.ErrStatusConflict
	}
	u.Status = to
	u.UpdatedAt = at
	r.units[id] = u
	return nil
}

// ApplyBoosts implements ports.UnitRepo. All-or-nothing over the whole
// batch, mirroring the real sqlite transaction's own semantics (design
// §5.2) rather than merely the port's error contract: a non-finite
// Weight anywhere in boosts refuses the entire call before any row is
// touched, and a boost naming a unit id that does not exist leaves every
// row untouched — including boosts for units that do exist earlier or
// later in the slice.
func (r *Units) ApplyBoosts(_ context.Context, boosts []weight.Boost, at time.Time) error {
	for _, b := range boosts {
		if math.IsNaN(b.Weight) || math.IsInf(b.Weight, 0) {
			return ports.ErrNonFiniteWeight
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, b := range boosts {
		if _, ok := r.units[b.UnitID]; !ok {
			return ports.ErrUnitNotFound
		}
	}

	for _, b := range boosts {
		u := r.units[b.UnitID]
		u.Weight = b.Weight
		u.LastTouchedAt = b.LastTouchedAt
		u.UpdatedAt = at
		r.units[b.UnitID] = u
	}
	return nil
}

// Count returns the number of units currently held. Test-only: it exists so
// a conformance test can assert a capture created zero units rows (Q3a's
// timer/recurring_reminder refusal, spec R4.6) without knowing an ID to look
// up — the same "a counter is not a contract case" shape 10b-ii's
// EmbedCalls() already established for this reason (C11's lesson: a
// contract answered once by one author is that author's opinion, a counter
// is a real observation).
func (r *Units) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.units)
}

// deepCopy returns a copy of u that shares no pointee or backing array with
// u — design D6: "no caller can reach the fake's interior". A plain struct
// copy is not enough: unit.Unit carries *time.Time, *float64 and
// json.RawMessage ([]byte) fields, and a shallow copy would still let a
// caller mutate the fake's stored value through those pointers/slices.
func deepCopy(u unit.Unit) unit.Unit {
	cp := u

	if u.StructuredData != nil {
		cp.StructuredData = make(json.RawMessage, len(u.StructuredData))
		copy(cp.StructuredData, u.StructuredData)
	}
	if u.EventAt != nil {
		t := *u.EventAt
		cp.EventAt = &t
	}
	if u.DueAt != nil {
		t := *u.DueAt
		cp.DueAt = &t
	}
	if u.Confidence != nil {
		c := *u.Confidence
		cp.Confidence = &c
	}

	return cp
}
