package brain

import (
	"context"

	"github.com/rengo/nooma/internal/core/graph"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// GraphService is the read model behind /ui/graph (ADR-0050): one live
// unit's bounded neighbourhood, already laid out. It writes nothing and
// reads no clock: the drawing is a function of the vault alone.
type GraphService struct {
	units ports.UnitRepo
	rels  ports.RelationRepo
}

// NewGraphService wires a GraphService over the two repositories it reads.
func NewGraphService(units ports.UnitRepo, rels ports.RelationRepo) *GraphService {
	return &GraphService{units: units, rels: rels}
}

// GraphNode is one drawn unit and where it is drawn.
type GraphNode struct {
	Unit unit.Unit
	At   graph.Point
}

// Neighbourhood is one centre unit, its neighbours in rank order on the
// ring, every relation among those units, and how many live neighbours the
// budget left out (doc 02 §4).
type Neighbourhood struct {
	Centre     GraphNode
	Neighbours []GraphNode
	Edges      []graph.Edge
	Omitted    int
	// Confirmable holds the ids of the drawn relations a confirm would
	// raise (relation.Confirmable); every other one is already in force.
	Confirmable map[string]bool
}

// Neighbourhood returns id's neighbourhood, or found=false when id is not
// live. Liveness comes from UnitRepo.LiveByIDs, never a status check here
// (I02, as UnitsService.Detail does): a relation to a unit that is not
// live is dropped before ranking, so it neither takes a place on the ring
// nor counts as left out.
//
// Only the kept neighbours' relations are read, one ByUnit each, so the
// reads are bounded by graph.MaxNeighbours whatever the centre's degree.
func (s *GraphService) Neighbourhood(ctx context.Context, id string) (Neighbourhood, bool, error) {
	live, err := s.units.LiveByIDs(ctx, []string{id})
	if err != nil || len(live) == 0 {
		return Neighbourhood{}, false, err
	}

	rels, err := s.rels.ByUnit(ctx, id)
	if err != nil {
		return Neighbourhood{}, false, err
	}
	others := make([]string, len(rels))
	for i, r := range rels {
		others[i] = otherEndpoint(r, id)
	}
	liveOthers, err := s.units.LiveByIDs(ctx, others)
	if err != nil {
		return Neighbourhood{}, false, err
	}
	byID := map[string]unit.Unit{id: live[0]}
	for _, u := range liveOthers {
		byID[u.ID] = u
	}

	var centreEdges []graph.Edge
	for _, r := range rels {
		if _, ok := byID[otherEndpoint(r, id)]; ok {
			centreEdges = append(centreEdges, graphEdge(r))
		}
	}
	kept, omitted := graph.Neighbours(id, centreEdges)

	all := centreEdges
	for _, n := range kept {
		nrels, err := s.rels.ByUnit(ctx, n)
		if err != nil {
			return Neighbourhood{}, false, err
		}
		for _, r := range nrels {
			all = append(all, graphEdge(r))
		}
	}

	ring := graph.Ring(len(kept))
	out := Neighbourhood{
		Centre:     GraphNode{Unit: live[0], At: graph.CentrePoint()},
		Neighbours: make([]GraphNode, len(kept)),
		Edges:      graph.Within(append([]string{id}, kept...), all),
		Omitted:    omitted,
	}
	for i, n := range kept {
		out.Neighbours[i] = GraphNode{Unit: byID[n], At: ring[i]}
	}
	if out.Confirmable, err = s.confirmable(ctx, out.Edges); err != nil {
		return Neighbourhood{}, false, err
	}
	return out, true, nil
}

// confirmable marks each edge below its type's resolved surface threshold,
// read through the same ThresholdsFor -> relation.Resolve pair the confirm
// itself raises by, once per type.
func (s *GraphService) confirmable(ctx context.Context, edges []graph.Edge) (map[string]bool, error) {
	byType := map[string]relation.Thresholds{}
	out := map[string]bool{}
	for _, e := range edges {
		t, ok := byType[e.Type]
		if !ok {
			row, err := s.rels.ThresholdsFor(ctx, e.Type)
			if err != nil {
				return nil, err
			}
			t = relation.Resolve(row)
			byType[e.Type] = t
		}
		if relation.Confirmable(e.Confidence, t) {
			out[e.ID] = true
		}
	}
	return out, nil
}

// graphEdge copies a relation into the shape core/graph reads.
func graphEdge(r ports.Relation) graph.Edge {
	return graph.Edge{
		ID: r.ID, From: r.FromUnitID, To: r.ToUnitID, Type: r.Type,
		Strength: r.Strength, Confidence: r.Confidence,
	}
}
