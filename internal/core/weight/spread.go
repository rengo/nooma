package weight

import (
	"math"
	"sort"
	"time"
)

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
// applied to a TARGET scaled by graph distance, in place of Revive's
// fixed WeightCeiling.
//
// For every unit v reachable from n.Origin within ResurfaceMaxHops hops
// (other than the origin itself):
//
//	gain(v)   = max over paths p, |p| <= ResurfaceMaxHops, of
//	              (product of strength(e) for e in p) * ResurfaceAttenuation^|p|
//	target(v) = gain(v) * WeightCeiling
//	e_v       = Effective(v.Weight, v.DecayRate, v.LastTouchedAt, now)
//
// Resurface emits Boost{v, e_v + ReviveGain*(target(v)-e_v), now} only
// when e_v < target(v); when e_v >= target(v) it emits nothing for v — a
// shorter slice, never a zero-delta entry (spec R2.6).
//
// Resurface REFUSES rather than coerces when a neighbour's own state, or
// the graph reaching it, is corrupt — the same posture Revive takes
// (boost.go's own doc comment, C4) and for the same reason: a corrupted
// Current would otherwise flow straight into an ordinary Boost{Weight: NaN},
// and since Boost is "the only shape this package lets a caller persist"
// (boost.go) and nothing in the vault is ever deleted, that NaN would make
// every later Effective on the unit return NaN forever. Coercing to a
// finite number, 0 in particular, would be worse than refusing: a coerced
// 0 could drive the unit under weight_threshold and archive it on the
// strength of a read error.
//
// Two structurally distinct inputs are corrupt, and each is validated
// where it ENTERS — before any comparison downstream gets a chance to skip
// it — rather than by inspecting the fully-computed boosted weight
// (Judgment Day round 2 on PR #140, both judges, independently, C15):
//
//   - c.Weight or c.DecayRate is NaN or ±Inf. Checked directly against the
//     Current, before target or e are computed at all, for every unit
//     gains reaches. An earlier version of this refusal instead computed
//     w = e + ReviveGain*(target-e) and tested w's own finiteness — but
//     target is always finite (target <= WeightCeiling), and "e >= target"
//     is a perfectly valid TRUE comparison when e is +Inf, not the NaN
//     quirk this comment used to invoke, so the "e >= target { continue }"
//     branch above fired first, before w was ever computed, for any
//     Current whose effective weight is +Inf without also being NaN
//     (Weight=+Inf, DecayRate=0: Effective returns +Inf, not NaN — decay.go's
//     own enumeration). Validating the raw Current before either
//     comparison closes every shape decay.go documents in one place: in
//     each of its four reachable-looking NaN shapes (Weight NaN; DecayRate
//     NaN; DecayRate=+Inf with Δt=0; Weight=+Inf with a DecayRate/Δt
//     product large enough to underflow exp to 0.0) either Weight or
//     DecayRate is already non-finite in the Current itself — so this
//     check subsumes all four, and the +Inf-alone shape besides, without
//     computing anything downstream that a comparison could skip past.
//   - An Edge.Strength that is NaN. buildAdjacency's own "strongest wins"
//     comparison (strength > adjacency[from][to]) is false for NaN against
//     any value, including the map's own zero default, so a NaN-strength
//     edge used to vanish before ever becoming a key: if it was a
//     neighbour's only edge, that neighbour was never visited at all — no
//     boost, no corrupted, indistinguishable from a neighbour genuinely
//     outside the graph. buildAdjacency now detects a NaN strength
//     explicitly, before clampStrength or the comparison ever see it (see
//     buildAdjacency's own doc comment), and reports both of the edge's
//     endpoints; Resurface merges that report into corrupted below for
//     every reported unit that (a) has a Current, (b) is not the origin,
//     and (c) gains does not otherwise reach — a corrupt edge that merely
//     duplicates a healthy path to the same neighbour contributes nothing
//     either way, the same "the strongest wins, a redundant path never
//     changes the outcome" rule R2.5 already uses elsewhere.
//
// A neighbour already refused through its own non-finite Current is never
// also reported through a corrupt edge; the two checks report the same
// unit at most once. With both entry points validated, the boosted weight
// w computed below is provably always finite — a finite Current combined
// with a finite gain (every adjacency entry is now either absent or in
// (0, 1], so spreadGains's products and WeightCeiling's own finite
// multiplication cannot introduce a NaN or ±Inf that was not already
// caught above) — so this function carries no separate check on w itself;
// this project's own convention holds that a branch no fixture can tell
// apart from removing it should not exist (clampStrength's own doc
// comment, C13), and no fixture can make w non-finite once its two inputs
// are validated at the door.
//
// Unlike Revive — a single Current, where "refused" collapses naturally
// onto a bool — Resurface fans out over a whole Neighbourhood, and a
// caller genuinely needs to tell "no boost because v is already at or
// above its target" (R2.6, a legitimate no-op) apart from "no boost
// because v's own state is corrupt" (this refusal): the first needs no
// record, the second is an event m2c should write to decision_log, not
// silently drop. So the refusal is reported in the second return value,
// corrupted, one entry per refused unit id, sorted the same way boosts
// is — rather than folded into the same shorter slice R2.6 already
// produces for its own, different reason.
//
// The gain scales the TARGET, never the step. Scaling the step instead —
// e + ReviveGain*gain*(WeightCeiling-e) — would let a unit merely adjacent
// to something used daily converge on the full ceiling: each pass closes a
// fraction of the remaining gap, while one day of decay at a typical λ
// removes only about 1% of it, so the neighbourhood of anything hot would
// become permanently hot and decay would never bite. Scaling the target
// caps *where* propagation can hold a unit, which is what makes spreading
// activation safe (spec R2.5).
//
// A unit reachable by more than one path takes the maximum gain among
// them, never the sum — the same rule design's F1/F3 use for combining
// graph evidence, so a unit's boost never depends on how many redundant
// edges happen to exist between it and the origin (spec R2.5). Traversal
// is undirected — a relation's storage direction is what the judge said,
// not a canonical form (doc 02 §4) — and where two units are joined by
// more than one edge, the strongest is used, by the same max rule. The
// origin is never a recipient: it already received its own direct revive,
// and a cycle back to it would double-count.
//
// Termination on a cyclic graph is by the hop bound alone, never a runtime
// timeout: gain is strictly decreasing along a path (ResurfaceAttenuation
// < 1, strength <= 1) and depth is capped at ResurfaceMaxHops, so a cycle
// can only ever produce a strictly worse path that the max comparison
// discards.
//
// Where Resurface DOES write, it resets LastTouchedAt to now — weight is
// *defined* as the value at last_touched_at, so writing one without the
// other would let the very next read re-apply the whole stale Δt to a
// value that was never true at its own timestamp (I24, spec R2.6). The
// worry that this makes a resurfaced unit look directly used is answered
// by the target cap, not by leaving the timestamp alone: a resurfaced unit
// converges on gain*WeightCeiling, never on WeightCeiling itself, so the
// clock resetting is harmless because the level it resets from is bounded
// by graph distance.
//
// Both returned slices are sorted by UnitID: the suite runs -shuffle=on
// with -race (Makefile:48), any implementation here uses maps internally,
// and m2c needs a reproducible decision_log order for the demo.
func Resurface(n Neighbourhood, now time.Time) (boosts []Boost, corrupted []string) {
	adjacency, corruptEdges := buildAdjacency(n.Edges)
	gains := spreadGains(n.Origin, adjacency)

	states := make(map[string]Current, len(n.States))
	for _, c := range n.States {
		states[c.UnitID] = c
	}

	for unitID, gain := range gains {
		c, ok := states[unitID]
		if !ok {
			continue
		}

		if nonFinite(c.Weight) || nonFinite(c.DecayRate) {
			corrupted = append(corrupted, unitID)
			continue
		}

		target := gain * WeightCeiling
		e := Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
		if e >= target {
			continue
		}

		boosts = append(boosts, Boost{
			UnitID:        unitID,
			Weight:        e + ReviveGain*(target-e),
			LastTouchedAt: now,
		})
	}

	// A unit reported by a NaN-strength edge is corrupt only when gains
	// does not otherwise reach it — a redundant healthy path to the same
	// neighbour already produced its correct boost or R2.6 no-op above,
	// and the corrupt edge contributed nothing to that outcome either way.
	// A unit already refused above (non-finite Weight/DecayRate) is
	// necessarily a member of gains — the loop above only ever inspects
	// unitIDs it iterates from gains — so the "reachable" check just below
	// already excludes it on its own; an earlier revision also tracked a
	// separate refused map and skipped it here, which was provably dead
	// (m2a C17: refused ⊆ gains, and the next line already skips everything
	// in gains). Deleted in feat/core-consolidation-strengthen-reweight,
	// the PR that makes Resurface reachable through a real caller.
	for unitID := range corruptEdges {
		if unitID == n.Origin {
			continue
		}
		if _, ok := states[unitID]; !ok {
			continue
		}
		if _, reachable := gains[unitID]; reachable {
			continue
		}
		corrupted = append(corrupted, unitID)
	}

	sort.Slice(boosts, func(i, j int) bool { return boosts[i].UnitID < boosts[j].UnitID })
	sort.Strings(corrupted)
	return boosts, corrupted
}

// nonFinite reports whether f is NaN or ±Inf — the same "not a real
// number this formula can reason about" test Resurface and buildAdjacency
// both apply to their own inputs, at the point each one enters (Resurface's
// own doc comment, C15).
func nonFinite(f float64) bool {
	return math.IsNaN(f) || math.IsInf(f, 0)
}

// buildAdjacency turns Edges into an undirected map: for every ordered
// pair (from, to), the strongest strength seen for that pair in EITHER
// stored direction (spec R2.5's "the strongest is used"). Every strength
// is clamped through clampStrength first, so an out-of-domain edge cannot
// out-compete a genuine one for "strongest" by being larger than the
// domain allows.
//
// A NaN strength is intercepted before it ever reaches clampStrength or
// the "strongest wins" comparison below: `strength > adjacency[from][to]`
// is false for NaN against any value under IEEE 754, including the map's
// own zero-value default, so a NaN-strength edge used to vanish silently —
// never becoming a key, and if it was a neighbour's only edge, that
// neighbour was never visited by spreadGains at all. buildAdjacency now
// reports both of a NaN-strength edge's endpoints through the second
// return value, corruptEdges, instead: Resurface merges it into corrupted
// for whichever endpoint gains does not otherwise reach (see Resurface's
// own doc comment, C15 — Judgment Day round 2 on PR #140, CRITICAL 2).
//
// corruptEdges intentionally reports BOTH endpoints of a NaN-strength
// edge, not only the one furthest from a given origin: buildAdjacency has
// no notion of "origin" — that is Resurface's own parameter — so it cannot
// know here which endpoint would have been the recipient.
func buildAdjacency(edges []Edge) (adjacency map[string]map[string]float64, corruptEdges map[string]bool) {
	adjacency = make(map[string]map[string]float64)
	corruptEdges = make(map[string]bool)
	add := func(from, to string, strength float64) {
		if math.IsNaN(strength) {
			corruptEdges[to] = true
			return
		}
		strength = clampStrength(strength)
		if adjacency[from] == nil {
			adjacency[from] = make(map[string]float64)
		}
		if strength > adjacency[from][to] {
			adjacency[from][to] = strength
		}
	}
	for _, e := range edges {
		add(e.From, e.To, e.Strength)
		add(e.To, e.From, e.Strength)
	}
	return adjacency, corruptEdges
}

// clampStrength bounds an edge's strength to relation's own upper domain
// bound, 1, per doc 02 §4 ("strength (0-1, returned by the judge)") — the
// same "no sign, no range" sanitization posture Effective already takes
// toward weight and decay_rate (doc 02 §2, decay.go's own doc comment):
// the relation judge's JSON decode (internal/core/relation/judgment.go)
// validates only that Strength is a number, and the schema's `strength
// REAL NOT NULL DEFAULT 0.5` column carries no CHECK
// (0001_core_tables.sql), so core cannot vouch for it landing in [0, 1]
// any more than it can vouch for weight or decay_rate. Left unclamped, a
// single edge with Strength: 100.0 reaches a boosted weight 17.8x
// WeightCeiling, falsifying R2.5's own cycle-termination argument, which
// assumes "attenuation < 1, strength <= 1" explicitly (Resurface's own
// doc comment above) — a strength above 1 is not a stronger relation, it
// is a corrupt one.
//
// The lower bound is deliberately NOT clamped here, unlike Effective's
// symmetric treatment of a negative weight or decay_rate: buildAdjacency's
// own "the strongest wins" comparison (`strength > adjacency[from][to]`,
// above) already races every strength against a Go map's zero-value
// default, so a negative-only strength for a given pair never wins that
// comparison and never enters the adjacency map at all — the exact
// no-conductance outcome an explicit clamp-to-0 would also produce, and
// verifiably so: a clampStrength that also mapped negative to 0 changes
// nothing observable in Resurface's output, because 0 does not beat the
// same zero default either. Adding a branch with no fixture able to tell
// it apart from removing it would be untested code by this project's own
// standard, not a fix (this project's own convention — a test that cannot
// be red says so; a clamp branch that cannot be red should not exist at
// all). If buildAdjacency's comparison ever changes to admit a negative
// strength, this bound needs revisiting.
//
// NaN never reaches this function at all, and that is deliberate, not an
// oversight this comment used to claim was covered elsewhere: `s > 1` is
// false for NaN under IEEE 754, so a clamp here could not catch it either
// way. An earlier revision of this comment claimed a NaN strength was
// "still caught — Resurface's own non-finite refusal always sees it
// downstream" — false, and falsified by Judgment Day round 2 on PR #140
// (both judges, independently, C15): buildAdjacency's own comparison,
// `strength > adjacency[from][to]`, is also false for NaN against any
// value, so a NaN-strength edge never became an adjacency key, never
// propagated into any gain or target, and never reached a boosted weight
// for that refusal to see. buildAdjacency now detects a NaN strength
// explicitly, in its own `add` closure, before clampStrength or this
// comparison ever run — see buildAdjacency's own doc comment. judgment.go,
// capture.go and the migration's CHECK constraint remain out of scope for
// this change (openspec/changes/m2a-weight-focus/tasks.md's Conflicts,
// C13).
func clampStrength(s float64) float64 {
	if s > 1 {
		return 1
	}
	return s
}

// spreadGains returns, for every node other than origin reachable within
// ResurfaceMaxHops hops of origin, the maximum gain among all paths from
// origin to it. Depth is bounded by ResurfaceMaxHops on every branch, so
// this terminates on a cyclic adjacency graph regardless of its size —
// see Resurface's own doc comment for why a cycle can only ever worsen a
// path, never extend the search past the bound.
func spreadGains(origin string, adjacency map[string]map[string]float64) map[string]float64 {
	gains := make(map[string]float64)

	var walk func(node string, product float64, hops int)
	walk = func(node string, product float64, hops int) {
		if hops >= ResurfaceMaxHops {
			return
		}
		for neighbour, strength := range adjacency[node] {
			nextProduct := product * strength
			nextHops := hops + 1
			gain := nextProduct * attenuationPow(nextHops)
			if neighbour != origin && gain > gains[neighbour] {
				gains[neighbour] = gain
			}
			walk(neighbour, nextProduct, nextHops)
		}
	}
	walk(origin, 1, 0)

	return gains
}

// attenuationPow raises ResurfaceAttenuation to a small non-negative
// integer exponent (bounded by ResurfaceMaxHops, 2 by default) — a loop is
// simpler here than reaching for math.Pow for a single, tightly-bounded
// call site.
func attenuationPow(hops int) float64 {
	result := 1.0
	for i := 0; i < hops; i++ {
		result *= ResurfaceAttenuation
	}
	return result
}
