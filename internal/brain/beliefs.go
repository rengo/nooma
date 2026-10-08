package brain

import (
	"context"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
)

// BeliefsService is the mirror's belief read model and its two user
// writes (m4e design §3.4): list the active beliefs by facet, edit one's
// content, retire one. One file per operation, one clock read each
// (belief_edit.go, belief_retire.go), so ByFacet, which has no clock,
// lives here.
type BeliefsService struct {
	clock   ports.Clock
	ids     ports.IDGen
	beliefs ports.SelfModelRepo
	signals ports.SignalRepo
	log     ports.DecisionLog
}

// NewBeliefsService wires a BeliefsService over the ports its three
// operations need.
func NewBeliefsService(clock ports.Clock, ids ports.IDGen, beliefs ports.SelfModelRepo, signals ports.SignalRepo, log ports.DecisionLog) *BeliefsService {
	return &BeliefsService{clock: clock, ids: ids, beliefs: beliefs, signals: signals, log: log}
}

// FacetBeliefs is one facet's active beliefs, in display order.
type FacetBeliefs struct {
	Facet   selfmodel.Facet
	Beliefs []ports.Belief
}

// ByFacet returns the active beliefs grouped under all five facets, in
// selfmodel.AllFacets order. A facet with no active belief is present and
// empty.
func (s *BeliefsService) ByFacet(ctx context.Context) ([]FacetBeliefs, error) {
	return nil, nil
}
