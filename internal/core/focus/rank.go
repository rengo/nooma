package focus

import (
	"math"
	"sort"
	"time"
)

// Ranked pairs a Candidate with the Score Rank computed for it (spec R3.6,
// design D6). Score is the literal value Priority returned — including a
// NaN or +Inf one (Rank's own doc comment, below) — Rank orders around a
// corrupted or extreme Score, it never coerces or hides it from the
// caller.
type Ranked struct {
	Candidate Candidate
	Score     float64
}

// Rank computes doc 02 §3's ranking over cs (spec R3.6, design D6): call
// Priority per Candidate — a Candidate's id missing from adjacency, or
// adjacency itself nil, reads as 0 (indexing a nil Go map, or a non-nil one
// for an absent key, always returns the zero value, so no special case is
// needed for either shape) — then sort by a three-level tie-break mirroring
// recall.FuseScored's own precedent (internal/core/recall/fuse.go:66-97)
// for the identical stated reason: make test runs -shuffle=on, and Priority
// produces exact float ties for symmetric inputs, so ties cannot be left to
// sort.Slice's own unstable order.
//
// Tie-break, in order:
//  1. higher Score first;
//  2. earlier DueAt first, with a non-nil DueAt always ordered before a
//     nil one regardless of the actual instant it names — nil is "no
//     deadline", not the zero time.Time (I18), so it must never compare as
//     an ordinary earlier-or-later time.Time would;
//  3. lexicographic by ID.
//
// Score can be NaN: Priority returns NaN whenever weight.Effective's own
// Weight or DecayRate takes one of the four NaN-producing shapes decay.go's
// own doc comment enumerates — not only a literal NaN input, since a
// finite-looking Weight = +Inf with a DecayRate large enough to underflow
// math.Exp to exactly 0.0 reaches NaN the same way (decay.go's own doc
// comment; Priority inherits that boundary unchanged, spec R3.1's
// finite-Weight/DecayRate qualifier).
// Comparing two Scores with an ordinary > is not a strict weak ordering
// once either side can be NaN — every IEEE 754 comparison against NaN is
// false ("NaN > x", "NaN < x" and "NaN >= x" all false, the identical trap
// clamp's own doc comment records and closes for adjacency, C24) — and an
// inconsistent sort.Slice comparator is undefined behavior, not merely a
// wrong order. Rank's decision, made explicit here rather than left
// unstated: a NaN Score sorts to the very bottom of the ranking, tied with
// any other NaN Score at this level and falling through correctly to the
// DueAt/ID levels between them — the same "a corrupted input contributes
// no promotion, never a crash and never an arbitrary position" posture
// this milestone has taken consistently for a value core cannot vouch for
// (clamp's own NaN -> lo; spread.go's buildAdjacency and clampStrength
// refusing to let a corrupt edge win a comparison, C15/C19). The returned
// Ranked still carries the literal NaN Score — Rank orders around a
// corrupted Candidate, it does not coerce or hide it. +Inf needs no
// equivalent decision: it is also reachable through weight.Effective
// (decay.go: a large, finite Weight with DecayRate 0 stays +Inf with no
// underflow) but IEEE 754 already orders +Inf correctly against every
// finite value under an ordinary >, so it sorts to the very top with no
// special-casing at all — see scoreKey, below, for the mechanism.
func Rank(cs []Candidate, adjacency map[string]float64, now time.Time) []Ranked {
	ranked := make([]Ranked, len(cs))
	for i, c := range cs {
		ranked[i] = Ranked{Candidate: c, Score: Priority(c, adjacency[c.ID], now)}
	}

	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]

		ak, bk := scoreKey(a.Score), scoreKey(b.Score)
		if ak != bk {
			return ak > bk
		}

		ad, bd := a.Candidate.DueAt, b.Candidate.DueAt
		if (ad == nil) != (bd == nil) {
			return ad != nil
		}
		if ad != nil && bd != nil && !ad.Equal(*bd) {
			return ad.Before(*bd)
		}

		return a.Candidate.ID < b.Candidate.ID
	})

	return ranked
}

// scoreKey returns score remapped to negative infinity when score is NaN —
// the total-order-preserving key Rank's sort.Slice comparator actually
// compares, kept distinct from the Score field Ranked reports. See Rank's
// own doc comment above for why this mapping is Rank's decision and why
// -Inf — not +Inf, not a panic, not an arbitrary position — is the chosen
// image: the same conservative reading clamp already gives a corrupt
// adjacency (mapping it to lo, no promotion), applied here to a corrupt
// Score — it sorts as if it had nothing going for it, last rather than
// anywhere else.
//
// A genuinely -Inf Score is not reachable through weight.Effective —
// decay.go's own doc comment: a negative Weight, including -Inf, clamps to
// 0 before the multiplication — but scoreKey does not special-case that
// away, since Rank's contract is over any float64 Priority could someday
// return, not only today's reachable subset.
func scoreKey(score float64) float64 {
	if math.IsNaN(score) {
		return math.Inf(-1)
	}
	return score
}
