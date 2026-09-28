package brain

import (
	"context"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// UnitsService is the mirror's second read model (design §3.3): browsing
// a page of live units and inspecting one, with its live relations. No
// ports.Clock field — nothing Browse or Detail does is a function of
// time (spec R1, R3).
//
// Real logic lands in task 2.2; this signature stage returns zero values
// so RunUnitsService... callers fail on assertions, not on a missing
// symbol.
type UnitsService struct {
	units ports.UnitRepo
	rels  ports.RelationRepo
}

// NewUnitsService wires a UnitsService over the ports Browse and Detail
// need.
func NewUnitsService(units ports.UnitRepo, rels ports.RelationRepo) *UnitsService {
	return &UnitsService{units: units, rels: rels}
}

// UnitDetail is one live unit and its live relations — what
// /ui/units/{id} renders (spec R3).
type UnitDetail struct {
	Unit      unit.Unit
	Relations []RelatedUnit
}

// RelatedUnit is one relation from UnitDetail.Unit's own perspective,
// joined to the other endpoint's live unit row — never a raw
// ports.Relation, which names an id and not the content a view needs.
type RelatedUnit struct {
	RelationID string
	Type       string
	Outgoing   bool
	Confidence float64
	Other      unit.Unit
}

// Browse returns one page of live units — stub, real logic in task 2.2.
func (s *UnitsService) Browse(ctx context.Context, types []unit.Type, after *ports.BrowseCursor) (ports.BrowsePage, error) {
	return ports.BrowsePage{}, nil
}

// Detail returns id's live unit and its live relations, or found=false
// when id is not live — stub, real logic in task 2.2.
func (s *UnitsService) Detail(ctx context.Context, id string) (UnitDetail, bool, error) {
	return UnitDetail{}, false, nil
}
