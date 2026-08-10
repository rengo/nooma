package memrepo

import (
	"context"
	"sync"
	"time"

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
