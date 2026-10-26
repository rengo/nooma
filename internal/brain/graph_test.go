package brain_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/graph"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

func seedGraphRelation(t *testing.T, rels *memrepo.Relations, id, from, to string, strength float64, offset int) {
	t.Helper()
	if err := rels.Upsert(context.Background(), ports.Relation{
		ID: id, FromUnitID: from, ToUnitID: to, Type: "same_topic",
		Strength: strength, Confidence: 0.6, CreatedBy: "test",
		CreatedAt: unitsNow.Add(time.Duration(offset) * time.Second),
	}); err != nil {
		t.Fatalf("seed relation %s: %v", id, err)
	}
}

// TestGraphService_NeighbourhoodNotFoundForNonLiveCentre: the graph is
// entered from a live unit only (I02), as the unit page is.
func TestGraphService_NeighbourhoodNotFoundForNonLiveCentre(t *testing.T) {
	units := memrepo.NewUnits()
	seedUnitsUnit(t, units, "u-archived", unit.StatusArchived)
	seedUnitsUnit(t, units, "u-superseded", unit.StatusSuperseded)
	seedUnitsUnit(t, units, "u-incomplete", unit.StatusIncomplete)
	svc := brain.NewGraphService(units, memrepo.NewRelations())

	for _, id := range []string{"u-archived", "u-superseded", "u-incomplete", "u-absent"} {
		n, found, err := svc.Neighbourhood(context.Background(), id)
		if err != nil {
			t.Fatalf("Neighbourhood(%q): %v", id, err)
		}
		if found || !reflect.DeepEqual(n, brain.Neighbourhood{}) {
			t.Fatalf("Neighbourhood(%q) = %+v, %v, want zero and not found", id, n, found)
		}
	}
}

// TestGraphService_NeighbourhoodDrawsLiveNeighboursAndTheEdgesAmongThem
// pins I02 for the graph (a non-live neighbour is not drawn, and an edge
// to one is not either), the ranking and the ring placement, and that an
// edge between two neighbours is drawn while one leaving the
// neighbourhood is not.
func TestGraphService_NeighbourhoodDrawsLiveNeighboursAndTheEdgesAmongThem(t *testing.T) {
	ctx := context.Background()
	units := memrepo.NewUnits()
	for _, id := range []string{"centre", "strong", "weak", "outsider"} {
		seedUnitsUnit(t, units, id, unit.StatusPool)
	}
	seedUnitsUnit(t, units, "archived", unit.StatusArchived)
	seedUnitsUnit(t, units, "superseded", unit.StatusSuperseded)
	seedUnitsUnit(t, units, "incomplete", unit.StatusIncomplete)

	rels := memrepo.NewRelations()
	seedGraphRelation(t, rels, "r-weak", "centre", "weak", 0.2, 0)
	seedGraphRelation(t, rels, "r-strong", "strong", "centre", 0.8, 1)
	seedGraphRelation(t, rels, "r-chord", "weak", "strong", 0.5, 2)
	seedGraphRelation(t, rels, "r-out", "strong", "outsider", 0.9, 3)
	// The strongest edges of all lead to non-live units: none is drawn.
	seedGraphRelation(t, rels, "r-archived", "centre", "archived", 1, 4)
	seedGraphRelation(t, rels, "r-superseded", "superseded", "centre", 1, 5)
	seedGraphRelation(t, rels, "r-incomplete", "centre", "incomplete", 1, 6)
	seedGraphRelation(t, rels, "r-absent", "centre", "absent", 1, 7)
	seedGraphRelation(t, rels, "r-weak-archived", "weak", "archived", 1, 8)

	n, found, err := brain.NewGraphService(units, rels).Neighbourhood(ctx, "centre")
	if err != nil || !found {
		t.Fatalf("Neighbourhood(centre) = %v, %v", found, err)
	}

	if n.Centre.Unit.ID != "centre" || n.Centre.At != graph.CentrePoint() {
		t.Fatalf("Centre = %s at %v, want centre at %v", n.Centre.Unit.ID, n.Centre.At, graph.CentrePoint())
	}
	ring := graph.Ring(2)
	var got []string
	for i, nb := range n.Neighbours {
		got = append(got, nb.Unit.ID)
		if nb.At != ring[i] {
			t.Fatalf("neighbour %s at %v, want %v", nb.Unit.ID, nb.At, ring[i])
		}
	}
	if !reflect.DeepEqual(got, []string{"strong", "weak"}) {
		t.Fatalf("Neighbours = %v, want [strong weak]: only live units, strongest first", got)
	}

	var edges []string
	for _, e := range n.Edges {
		edges = append(edges, e.ID)
	}
	if !reflect.DeepEqual(edges, []string{"r-chord", "r-strong", "r-weak"}) {
		t.Fatalf("Edges = %v, want [r-chord r-strong r-weak]", edges)
	}
	want := graph.Edge{ID: "r-strong", From: "strong", To: "centre", Type: "same_topic", Strength: 0.8, Confidence: 0.6}
	if n.Edges[1] != want {
		t.Fatalf("Edges[1] = %+v, want %+v", n.Edges[1], want)
	}
	if n.Omitted != 0 {
		t.Fatalf("Omitted = %d, want 0: non-live units are not counted as left out", n.Omitted)
	}
}

func TestGraphService_NeighbourhoodCountsLiveNeighboursPastTheBudget(t *testing.T) {
	ctx := context.Background()
	units := memrepo.NewUnits()
	rels := memrepo.NewRelations()
	seedUnitsUnit(t, units, "centre", unit.StatusPool)
	for i := 0; i < graph.MaxNeighbours+2; i++ {
		id := fmt.Sprintf("n%02d", i)
		seedUnitsUnit(t, units, id, unit.StatusPool)
		seedGraphRelation(t, rels, "r-"+id, "centre", id, 0.5, i)
	}

	n, _, err := brain.NewGraphService(units, rels).Neighbourhood(ctx, "centre")
	if err != nil {
		t.Fatalf("Neighbourhood: %v", err)
	}
	if len(n.Neighbours) != graph.MaxNeighbours || n.Omitted != 2 {
		t.Fatalf("drew %d, omitted %d; want %d and 2", len(n.Neighbours), n.Omitted, graph.MaxNeighbours)
	}
}

func TestGraphService_NeighbourhoodOfAnUnconnectedUnitIsJustTheCentre(t *testing.T) {
	units := memrepo.NewUnits()
	seedUnitsUnit(t, units, "alone", unit.StatusPool)

	n, found, err := brain.NewGraphService(units, memrepo.NewRelations()).Neighbourhood(context.Background(), "alone")
	if err != nil || !found {
		t.Fatalf("Neighbourhood(alone) = %v, %v", found, err)
	}
	if n.Centre.Unit.ID != "alone" || len(n.Neighbours) != 0 || len(n.Edges) != 0 || n.Omitted != 0 {
		t.Fatalf("Neighbourhood(alone) = %+v, want the centre alone", n)
	}
}

// TestGraphService_MarksOnlyRelationsBelowTheirTypesSurfaceAsConfirmable:
// each drawn relation is judged against its own type's resolved surface
// threshold, the one the confirm itself raises to: a learned row for
// derived_from, the default for same_topic.
func TestGraphService_MarksOnlyRelationsBelowTheirTypesSurfaceAsConfirmable(t *testing.T) {
	ctx := context.Background()
	units := memrepo.NewUnits()
	for _, id := range []string{"centre", "a", "b", "c"} {
		seedUnitsUnit(t, units, id, unit.StatusPool)
	}
	rels := memrepo.NewRelations()
	rels.SeedThreshold(t, "derived_from", 0.55, 0.75)
	for _, r := range []ports.Relation{
		// 0.70 is above the default surface but below derived_from's own.
		{ID: "r-learned", FromUnitID: "centre", ToUnitID: "a", Type: "derived_from", Strength: 0.5, Confidence: 0.70},
		{ID: "r-asserted", FromUnitID: "centre", ToUnitID: "b", Type: "same_topic", Strength: 0.5, Confidence: 0.99},
		{ID: "r-uncertain", FromUnitID: "c", ToUnitID: "centre", Type: "same_topic", Strength: 0.5, Confidence: 0.40},
	} {
		r.CreatedBy, r.CreatedAt = "test", unitsNow
		if err := rels.Upsert(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	n, _, err := brain.NewGraphService(units, rels).Neighbourhood(ctx, "centre")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"r-learned": true, "r-uncertain": true}
	if !reflect.DeepEqual(n.Confirmable, want) {
		t.Fatalf("Confirmable = %v, want %v", n.Confirmable, want)
	}
}
