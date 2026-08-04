package weight

import "time"

// Edge is one relation edge inside a Neighbourhood, carrying the strength
// Resurface propagates along. Storage direction does not matter — doc 02
// §4 states a relation's direction is what the judge said, not a
// canonical form, so Resurface treats every edge as undirected (spec
// R2.5).
type Edge struct {
	From, To string
	Strength float64
}

// Neighbourhood is Resurface's whole input: the origin unit's id, the
// decay-relevant state of every unit the caller wants considered, and
// every edge among them.
type Neighbourhood struct {
	Origin string
	States []Current
	Edges  []Edge
}

// ResurfaceMaxHops bounds how far a boost propagates from
// Neighbourhood.Origin. Default 2 (doc 02 §13): one hop is not spreading,
// three reaches most of a personal graph and costs branching cubed.
const ResurfaceMaxHops = 2

// ResurfaceAttenuation is the per-hop cost gain pays, independent of how
// strong the judge said any single edge was. Default 0.5 (doc 02 §13).
const ResurfaceAttenuation = 0.5

// Resurface computes doc 02 §2's "propagates a boost along the graph
// edges" as a formula (spec R2.5-R2.7, design F3): F2's asymptotic boost
// applied to a target scaled by graph distance instead of Revive's fixed
// WeightCeiling. Implemented in the commit that follows this stub.
func Resurface(n Neighbourhood, now time.Time) []Boost {
	return nil
}
