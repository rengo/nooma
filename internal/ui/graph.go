package ui

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/graph"
	"github.com/rengo/nooma/internal/core/unit"
)

// GraphReader is /ui/graph's one entrance into the brain (ADR-0050): one
// live unit's neighbourhood, already bounded and laid out by
// internal/core/graph. *brain.GraphService satisfies it.
type GraphReader interface {
	Neighbourhood(ctx context.Context, id string) (brain.Neighbourhood, bool, error)
}

// The drawing's own geometry, in the viewBox's units (graph.ViewSize).
// Like graph.RingRadius they place pixels and decide nothing.
const (
	graphCentreRadius = 14
	graphNodeRadius   = 10
	// graphLabelGap is how far below its node's centre a label's baseline
	// sits: the node, then a gap, then one line of text.
	graphLabelGap = 30
	// graphLabelRunes is the longest label drawn under a node, ellipsis
	// included; the full text is the link's accessible name. Twelve labels
	// of this length stay apart on the ring at phone width.
	graphLabelRunes = 14
)

// serveGraph answers GET /ui/graph?unit=<id>: the unit's neighbourhood, or
// 404 when it is not live (I02, decided by GraphService through
// LiveByIDs), 400 with no unit to centre on.
func (h *Handler) serveGraph(w http.ResponseWriter, r *http.Request) {
	if h.deps.Graph == nil {
		http.Error(w, "graph: not wired in this build", http.StatusServiceUnavailable)
		return
	}
	id := r.URL.Query().Get("unit")
	if id == "" {
		http.Error(w, "graph: unit is required", http.StatusBadRequest)
		return
	}
	n, found, err := h.deps.Graph.Neighbourhood(r.Context(), id)
	if err != nil {
		slog.Error("graph: neighbourhood failed", "err", err)
		http.Error(w, "graph: internal error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = GraphPage(n).Render(r.Context(), w)
}

// graphLabel is how a unit is named in the graph: its title when it has
// one, else its content, trimmed.
func graphLabel(u unit.Unit) string {
	if t := strings.TrimSpace(u.Title); t != "" {
		return t
	}
	return strings.TrimSpace(u.Content)
}

// graphShortLabel is graphLabel clipped to graphLabelRunes, counted in
// runes so a multi-byte letter is never cut in half.
func graphShortLabel(u unit.Unit) string {
	r := []rune(graphLabel(u))
	if len(r) <= graphLabelRunes {
		return string(r)
	}
	return strings.TrimSpace(string(r[:graphLabelRunes-1])) + "…"
}

// graphLine is one relation as drawn: a straight segment when it touches
// the centre, else a curve whose path is d (graph.ChordControl).
type graphLine struct {
	edge           graph.Edge
	x1, y1, x2, y2 string
	d              string
}

// graphLines places every edge between its endpoints' points. Every edge
// GraphService returns joins two drawn units (graph.Within), so a lookup
// never misses.
func graphLines(n brain.Neighbourhood) []graphLine {
	at := map[string]graph.Point{n.Centre.Unit.ID: n.Centre.At}
	for _, nb := range n.Neighbours {
		at[nb.Unit.ID] = nb.At
	}
	lines := make([]graphLine, len(n.Edges))
	for i, e := range n.Edges {
		a, b := at[e.From], at[e.To]
		lines[i] = graphLine{edge: e, x1: itoa(a.X), y1: itoa(a.Y), x2: itoa(b.X), y2: itoa(b.Y)}
		if e.From != n.Centre.Unit.ID && e.To != n.Centre.Unit.ID {
			c := graph.ChordControl(a, b)
			lines[i].d = "M" + lines[i].x1 + " " + lines[i].y1 + " Q" + itoa(c.X) + " " + itoa(c.Y) + " " + lines[i].x2 + " " + lines[i].y2
		}
	}
	return lines
}

// graphEdgeTitle is a relation's tooltip and accessible description.
func graphEdgeTitle(e graph.Edge) string {
	return e.Type + ", confidence " + formatConfidence(e.Confidence)
}

func itoa(i int) string { return strconv.Itoa(i) }

// graphViewBox is the SVG's viewBox attribute.
func graphViewBox() string { return "0 0 " + itoa(graph.ViewSize) + " " + itoa(graph.ViewSize) }

// graphURL is the link that redraws the graph around id.
func graphURL(id string) string { return "/ui/graph?unit=" + url.QueryEscape(id) }
