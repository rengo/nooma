package graph

import "sort"

// MaxNeighbours is the render budget ADR-0019 named and left unvalued, and
// ADR-0050 keeps: the most related units one graph view draws around its
// centre (doc 02 §13, graph_max_neighbours). A ring of twelve keeps its
// labels apart at phone width.
const MaxNeighbours = 12

// Edge is one relation as the graph sees it: core cannot import ports, so
// internal/brain copies each ports.Relation into one of these.
type Edge struct {
	ID         string
	From       string
	To         string
	Type       string
	Strength   float64
	Confidence float64
}

// Neighbours returns the units edges connect to centre, at most
// MaxNeighbours of them, and how many more there were. A neighbour ranks by
// the highest strength among the edges it shares with the centre, then by
// the highest confidence among them, then by id, so the order never depends
// on the input's. Edges not touching centre and self-loops are ignored. A
// NaN ranks below every number.
func Neighbours(centre string, edges []Edge) ([]string, int) {
	type rank struct {
		id                   string
		strength, confidence float64
		seen                 bool
	}
	byID := map[string]*rank{}
	for _, e := range edges {
		other := ""
		switch {
		case e.From == centre && e.To != centre:
			other = e.To
		case e.To == centre && e.From != centre:
			other = e.From
		default:
			continue
		}
		r, ok := byID[other]
		if !ok {
			r = &rank{id: other}
			byID[other] = r
		}
		if !r.seen || greater(e.Strength, r.strength) {
			r.strength = e.Strength
		}
		if !r.seen || greater(e.Confidence, r.confidence) {
			r.confidence = e.Confidence
		}
		r.seen = true
	}

	ranks := make([]*rank, 0, len(byID))
	for _, r := range byID {
		ranks = append(ranks, r)
	}
	sort.Slice(ranks, func(i, j int) bool {
		a, b := ranks[i], ranks[j]
		if greater(a.strength, b.strength) || greater(b.strength, a.strength) {
			return greater(a.strength, b.strength)
		}
		if greater(a.confidence, b.confidence) || greater(b.confidence, a.confidence) {
			return greater(a.confidence, b.confidence)
		}
		return a.id < b.id
	})

	kept := min(len(ranks), MaxNeighbours)
	ids := make([]string, kept)
	for i := range ids {
		ids[i] = ranks[i].id
	}
	return ids, len(ranks) - kept
}

// greater orders NaN below every number, so the ranking above is a strict
// weak ordering even over a REAL column with no CHECK.
func greater(a, b float64) bool {
	if a != a { // a is NaN
		return false
	}
	if b != b {
		return true
	}
	return a > b
}

// Within returns the edges whose two endpoints are both in nodes, each edge
// once, ordered by id. A relation from a unit to itself is left out, as
// Neighbours leaves it out: it joins nothing to draw. internal/brain reads each node's relations
// separately, so an edge between two nodes arrives twice.
func Within(nodes []string, edges []Edge) []Edge {
	in := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		in[n] = true
	}
	seen := map[string]bool{}
	var out []Edge
	for _, e := range edges {
		if !in[e.From] || !in[e.To] || e.From == e.To || seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
