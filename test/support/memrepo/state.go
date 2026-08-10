package memrepo

import (
	"context"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// State is an in-memory ports.StateRepo. The zero value is not usable —
// call NewState.
//
// TODO(PR3 GREEN): this is the RED-commit stub — OpenHypothesis is a
// no-op and LastHypothesisAt always reports none, deliberately failing
// repocontract.RunOpenHypothesis/RunLastHypothesisAt for the right reason
// (nothing is stored) rather than for a compile error. The GREEN commit
// in this same PR replaces every body below.
type State struct{}

// Assert at compile time, following internal/store/sqlite/unitrepo.go:33's
// precedent.
var _ ports.StateRepo = (*State)(nil)

// NewState returns an empty, ready-to-use in-memory ports.StateRepo.
// Every call returns an independent instance.
func NewState() *State {
	return &State{}
}

// OpenHypothesis implements ports.StateRepo. RED-commit stub: a no-op.
func (s *State) OpenHypothesis(_ context.Context, _ ports.StateHypothesis) error {
	return nil
}

// LastHypothesisAt implements ports.StateRepo. RED-commit stub: always
// none.
func (s *State) LastHypothesisAt(_ context.Context) (*time.Time, error) {
	return nil, nil
}
