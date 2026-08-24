package memrepo

import (
	"context"
	"sync"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// State is an in-memory ports.StateRepo. The zero value is not usable —
// call NewState. Two instances share no state, matching memrepo.Units's
// own isolation rule.
type State struct {
	mu sync.Mutex
	// consolidationRows holds every row OpenHypothesis has appended, in
	// append order — this fake only ever writes source =
	// ports.StateSourceConsolidation (the port declares no other write),
	// so it needs no separate source field to filter on.
	consolidationRows []ports.StateHypothesis
	// energy holds every reading RecordEnergy appended, in append order.
	energy []prospection.EnergyReading
}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent.
var _ ports.StateRepo = (*State)(nil)

// NewState returns an empty, ready-to-use in-memory ports.StateRepo.
// Every call returns an independent instance.
func NewState() *State {
	return &State{}
}

// OpenHypothesis implements ports.StateRepo. Append-only: appends h to the
// stored rows, never touching a previously appended row.
func (s *State) OpenHypothesis(_ context.Context, h ports.StateHypothesis) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.consolidationRows = append(s.consolidationRows, h)
	return nil
}

// LastHypothesisAt implements ports.StateRepo. Returns the greatest
// RecordedAt among every appended row — not the last one appended, which
// would answer a different question when rows arrive out of chronological
// order (design §4.4: this feeds consolidation.EvaluateLoad's
// lastHypothesisAt parameter directly, a time comparison, not an
// insertion-order one).
func (s *State) LastHypothesisAt(_ context.Context) (*time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.consolidationRows) == 0 {
		return nil, nil
	}
	latest := s.consolidationRows[0].RecordedAt
	for _, h := range s.consolidationRows[1:] {
		if h.RecordedAt.After(latest) {
			latest = h.RecordedAt
		}
	}
	return &latest, nil
}

// LatestEnergy implements ports.StateRepo. nil when nothing was recorded —
// see the port's doc comment for why that is not a zero value.
func (s *State) LatestEnergy(_ context.Context) (*prospection.EnergyReading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.energy) == 0 {
		return nil, nil
	}
	latest := s.energy[0]
	for _, r := range s.energy[1:] {
		if r.RecordedAt.After(latest.RecordedAt) {
			latest = r
		}
	}
	return &latest, nil
}

// RecordEnergy seeds a reading. Test-only: nothing in m3d writes energy
// yet — the check-in that does is PR 7's — and a digest test needs one to
// exist without waiting for it.
func (s *State) RecordEnergy(r prospection.EnergyReading) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.energy = append(s.energy, r)
}
