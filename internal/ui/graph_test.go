package ui_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/graph"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ui"
)

type stubGraph struct {
	n     brain.Neighbourhood
	found bool
	err   error
	got   *string
}

func (s stubGraph) Neighbourhood(_ context.Context, id string) (brain.Neighbourhood, bool, error) {
	if s.got != nil {
		*s.got = id
	}
	return s.n, s.found, s.err
}

func graphGet(t *testing.T, deps ui.Deps, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Pattern = "GET /ui/graph"
	rec := httptest.NewRecorder()
	ui.New(deps).ServeHTTP(rec, req)
	return rec
}

func threeNeighbours() brain.Neighbourhood {
	ring := graph.Ring(3)
	return brain.Neighbourhood{
		Centre: brain.GraphNode{Unit: unit.Unit{ID: "c", Title: "Lisbon trip", Content: "Plan the trip"}, At: graph.CentrePoint()},
		Neighbours: []brain.GraphNode{
			{Unit: unit.Unit{ID: "n1", Content: "Book flights"}, At: ring[0]},
			{Unit: unit.Unit{ID: "n2", Title: "Hotel", Content: "Find a hotel"}, At: ring[1]},
			{Unit: unit.Unit{ID: "n3", Content: "Renew the passport"}, At: ring[2]},
		},
		Edges: []graph.Edge{
			{ID: "r1", From: "c", To: "n1", Type: "same_topic", Strength: 0.8, Confidence: 0.9},
			{ID: "r2", From: "n2", To: "c", Type: "same_topic", Strength: 0.5, Confidence: 0.6},
		},
	}
}

func TestGraphView_EveryNeighbourIsALinkThatRecentresOnIt(t *testing.T) {
	t.Parallel()
	var got string
	rec := graphGet(t, ui.Deps{Graph: stubGraph{n: threeNeighbours(), found: true, got: &got}}, "/ui/graph?unit=c")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ui/graph?unit=c = %d: %s", rec.Code, rec.Body.String())
	}
	if got != "c" {
		t.Fatalf("Neighbourhood read %q, want c", got)
	}
	page := rec.Body.String()
	for _, want := range []string{
		"<h2>GRAPH</h2>",
		`<a href="/ui/graph?unit=n1" aria-label="Book flights"`,
		`<a href="/ui/graph?unit=n2" aria-label="Hotel"`,
		`<a href="/ui/graph?unit=n3" aria-label="Renew the passport"`,
		// The centre opens its own unit page rather than redrawing itself.
		`<a href="/ui/units/c" aria-label="Lisbon trip" data-centre`,
		`data-relation-id="r1"`,
		`data-relation-id="r2"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "data-empty") || strings.Contains(page, "data-omitted") {
		t.Errorf("a full neighbourhood shows an empty or omitted note:\n%s", page)
	}
}

func TestGraphView_AnUnconnectedUnitSaysSoInOneLine(t *testing.T) {
	t.Parallel()
	n := brain.Neighbourhood{Centre: brain.GraphNode{Unit: unit.Unit{ID: "c", Content: "Alone"}, At: graph.CentrePoint()}}
	page := graphGet(t, ui.Deps{Graph: stubGraph{n: n, found: true}}, "/ui/graph?unit=c").Body.String()

	if !strings.Contains(page, "<p data-empty>Not connected to anything yet.") {
		t.Errorf("no one-line empty state:\n%s", page)
	}
	if strings.Contains(page, "<svg") {
		t.Errorf("an unconnected unit still draws a graph:\n%s", page)
	}
}

func TestGraphView_SaysHowManyNeighboursWereLeftOut(t *testing.T) {
	t.Parallel()
	n := threeNeighbours()
	n.Omitted = 4
	page := graphGet(t, ui.Deps{Graph: stubGraph{n: n, found: true}}, "/ui/graph?unit=c").Body.String()

	if !strings.Contains(page, "<p data-omitted>+4 more related units not shown.</p>") {
		t.Errorf("no omitted note:\n%s", page)
	}
}

func TestGraphView_RefusesWhatItCannotDraw(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		deps   ui.Deps
		target string
		want   int
	}{
		{"not wired", ui.Deps{}, "/ui/graph?unit=c", http.StatusServiceUnavailable},
		{"no unit", ui.Deps{Graph: stubGraph{found: true}}, "/ui/graph", http.StatusBadRequest},
		{"not live", ui.Deps{Graph: stubGraph{found: false}}, "/ui/graph?unit=gone", http.StatusNotFound},
		{"read fails", ui.Deps{Graph: stubGraph{err: errors.New("disk on fire")}}, "/ui/graph?unit=c", http.StatusInternalServerError},
	}
	for _, c := range cases {
		rec := graphGet(t, c.deps, c.target)
		if rec.Code != c.want {
			t.Errorf("%s: %d, want %d", c.name, rec.Code, c.want)
		}
		if strings.Contains(rec.Body.String(), "disk on fire") {
			t.Errorf("%s: the error reached the browser", c.name)
		}
	}
}

func TestUnitView_LinksToItsGraphWhenItHasRelations(t *testing.T) {
	t.Parallel()
	related := brain.UnitDetail{
		Unit:      unit.Unit{ID: "u1", Type: unit.TypeTask, Content: "x"},
		Relations: []brain.RelatedUnit{{RelationID: "r1", Type: "same_topic", Other: unit.Unit{ID: "u2", Content: "y"}}},
	}
	h := ui.New(ui.Deps{Units: stubUnitsReader{detail: related, detailFound: true}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("u1"))
	if !strings.Contains(rec.Body.String(), `<a href="/ui/graph?unit=u1">`) {
		t.Errorf("unit page with relations has no graph link:\n%s", rec.Body.String())
	}

	alone := brain.UnitDetail{Unit: unit.Unit{ID: "u1", Type: unit.TypeTask, Content: "x"}}
	h = ui.New(ui.Deps{Units: stubUnitsReader{detail: alone, detailFound: true}})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, unitRequest("u1"))
	if strings.Contains(rec.Body.String(), "/ui/graph") {
		t.Errorf("unit page with no relations links to an empty graph:\n%s", rec.Body.String())
	}
}

// TestUnitsView_RowsDoNotLinkToTheGraph: a row does not know whether its
// unit has relations, and a link that lands on the empty line is noise
// (client ruling 2026-10-23). The graph is entered from a unit page that
// has relations.
func TestUnitsView_RowsDoNotLinkToTheGraph(t *testing.T) {
	t.Parallel()
	h := ui.New(ui.Deps{Units: stubUnitsReader{page: fixedBrowsePage()}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, unitsRequest(""))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-unit-id="unit-1"`) {
		t.Fatalf("GET /ui/units = %d, rows not rendered:\n%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "/ui/graph") {
		t.Errorf("a Units row links to the graph:\n%s", rec.Body.String())
	}
}
