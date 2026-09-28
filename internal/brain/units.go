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

// Browse returns one page of live units, delegating unchanged to
// UnitRepo.LiveBrowsePage — I02 is enforced there, not reimplemented here
// (design §3.3).
func (s *UnitsService) Browse(ctx context.Context, types []unit.Type, after *ports.BrowseCursor) (ports.BrowsePage, error) {
	return s.units.LiveBrowsePage(ctx, types, after)
}

// Detail returns id's live unit and its live relations, or found=false
// when id is not live — archived, superseded, incomplete or absent all
// answer alike (spec R3), through UnitRepo.LiveByIDs's existing positive
// filter rather than a status check this method invents. A relation
// whose other endpoint is not live is dropped, the same I02 filter
// applied to neighbours (spec R3's non-live-neighbour scenario) via a
// second LiveByIDs call, never a status comparison against the relation's
// own row.
func (s *UnitsService) Detail(ctx context.Context, id string) (UnitDetail, bool, error) {
	live, err := s.units.LiveByIDs(ctx, []string{id})
	if err != nil {
		return UnitDetail{}, false, err
	}
	if len(live) == 0 {
		return UnitDetail{}, false, nil
	}
	u := live[0]

	rels, err := s.rels.ByUnit(ctx, id)
	if err != nil {
		return UnitDetail{}, false, err
	}

	otherIDs := make([]string, len(rels))
	for i, r := range rels {
		otherIDs[i] = otherEndpoint(r, id)
	}
	liveOthers, err := s.units.LiveByIDs(ctx, otherIDs)
	if err != nil {
		return UnitDetail{}, false, err
	}
	liveByID := make(map[string]unit.Unit, len(liveOthers))
	for _, o := range liveOthers {
		liveByID[o.ID] = o
	}

	related := make([]RelatedUnit, 0, len(rels))
	for _, r := range rels {
		other, ok := liveByID[otherEndpoint(r, id)]
		if !ok {
			continue
		}
		related = append(related, RelatedUnit{
			RelationID: r.ID,
			Type:       r.Type,
			Outgoing:   r.FromUnitID == id,
			Confidence: r.Confidence,
			Other:      other,
		})
	}

	return UnitDetail{Unit: u, Relations: related}, true, nil
}

// otherEndpoint returns r's endpoint that is not id — ByUnit returns
// relations where id is either FromUnitID or ToUnitID (the port's own
// doc comment).
func otherEndpoint(r ports.Relation, id string) string {
	if r.FromUnitID == id {
		return r.ToUnitID
	}
	return r.FromUnitID
}
