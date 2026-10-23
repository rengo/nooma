// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/graph"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ui"
)

// TestGraphSVG_Golden pins the drawing itself (ADR-0050, doc 06 "The graph
// is L1 too"): the SVG fragment alone, never the page around it. It lives
// here, not in internal/ui, because the ui-boundary rule keeps that package
// off the filesystem, tests included; -update rewrites it. It covers
// the centre, a ring of four, relations between two adjacent and two
// opposite neighbours (curves, graph.ChordControl), a title
// used over content, a label clipped to its rune limit, and a label that
// needs escaping. One element per line, so a diff reads.
func TestGraphSVG_Golden(t *testing.T) {
	ring := graph.Ring(4)
	n := brain.Neighbourhood{
		Centre: brain.GraphNode{Unit: unit.Unit{ID: "c", Title: "Lisbon trip", Content: "Plan the trip to Lisbon"}, At: graph.CentrePoint()},
		Neighbours: []brain.GraphNode{
			{Unit: unit.Unit{ID: "n1", Content: "Book flights for the whole family in March"}, At: ring[0]},
			{Unit: unit.Unit{ID: "n2", Title: "Hotel", Content: "Find a hotel near the river"}, At: ring[1]},
			{Unit: unit.Unit{ID: "n3", Content: "Passport <renewal> & photos"}, At: ring[2]},
			{Unit: unit.Unit{ID: "n4", Content: "  Pack  "}, At: ring[3]},
		},
		Edges: []graph.Edge{
			{ID: "r1", From: "c", To: "n1", Type: "same_topic", Strength: 0.8, Confidence: 0.9},
			{ID: "r2", From: "n1", To: "n2", Type: "derived_from", Strength: 0.4, Confidence: 0.35},
			{ID: "r3", From: "n2", To: "c", Type: "same_topic", Strength: 0.5, Confidence: 0.6},
			{ID: "r4", From: "c", To: "n3", Type: "same_topic", Strength: 0.3, Confidence: 0.55},
			{ID: "r5", From: "n4", To: "c", Type: "same_topic", Strength: 0.2, Confidence: 0.5},
			// Opposite neighbours: the curve must clear the centre.
			{ID: "r6", From: "n1", To: "n3", Type: "same_topic", Strength: 0.3, Confidence: 0.4},
		},
	}

	var buf bytes.Buffer
	if err := ui.GraphSVG(n).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := strings.ReplaceAll(buf.String(), "><", ">\n<") + "\n"

	path := filepath.Join(repoRootFromCaller(t), "testdata", "ui", "graph.svg.golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("SVG differs from %s (rerun with -update and read the diff):\n%s", path, got)
	}
}
