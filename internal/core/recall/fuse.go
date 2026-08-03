package recall

import "sort"

// RRFK is Reciprocal Rank Fusion's k (Cormack et al.'s original value and
// the de facto industry default), the formula's own bias term —
// ADR-0010's decision. docs/02-cognitive-core.md §13 already lists RRF's
// k = 60; this is its first Go declaration (design D5).
const RRFK = 60

// RecallTopK bounds how many results each leg (vector, lexical) contributes
// to fusion — the same K for both legs, per spec R2.5. docs/02-cognitive-core.md
// §13, new row (design D5).
const RecallTopK = 20

// WeightVector and WeightLexical are RRF's per-list weight w_i in
// score(d) = Σ w_i/(k + rank_i(d)) — ADR-0010:48-49 requires each list's
// relative weight to be a named constant, not a literal repeated at a call
// site, so calibration later touches exactly one place. Both start at 1.0:
// no calibration data exists yet. docs/02-cognitive-core.md §13, new rows
// (design D5).
const (
	WeightVector  = 1.0
	WeightLexical = 1.0
)

// fuseWeight returns the RRF weight for lists[listIndex]. Fuse always
// receives the vector leg first and the lexical leg second (design D5:
// "Phase B always passes exactly two, vector first"); any list beyond the
// second defaults to weight 1.0, since ADR-0010 names only these two legs
// and Fuse's variadic signature exists for the formula's own generality,
// not because Phase B calls it with more than two.
func fuseWeight(listIndex int) float64 {
	switch listIndex {
	case 0:
		return WeightVector
	case 1:
		return WeightLexical
	default:
		return 1.0
	}
}

// FusedCandidate is one id and the RRF score that ranked it — design D1.
// Score is a plain float64, deliberately distinct from recall.Scored's
// float32: at RRFK = 60 a candidate first on both legs scores 2/61 and one
// second on both legs scores 2/62, a difference of 5e-4 that D2's margin
// gate needs to resolve without losing precision to compression.
type FusedCandidate struct {
	ID    string
	Score float64
}

// FuseScored combines two or more ranked id lists into one fused ranking by
// Reciprocal Rank Fusion, keeping each candidate's score — ADR-0010:
// score(d) = Σ w_i/(k + rank_i(d)) over the lists d appears in, rank_i(d)
// 1-indexed, an id present in only one list contributing a single term.
// FuseScored is variadic because ADR-0010's formula generalizes to N lists
// in its own wording.
//
// Every returned score is strictly positive (design D1, load-bearing for
// D2's margin gate, which divides by the runner-up's score): an id is
// returned only when present in at least one list, and every term
// w_i/(RRFK + rank_i) is positive for w_i > 0 (WeightVector, WeightLexical),
// RRFK = 60, and rank_i >= 1.
//
// Ties are not left to sort.Slice's unstable order — make test runs
// -shuffle=on (Makefile:48), and score(d) produces exact float ties for
// symmetric cases. Design D5's rule, in order: higher score first; on a
// tie, the id whose earliest list (in argument order) comes first; on a
// further tie, lexicographic by id.
func FuseScored(lists ...[]string) []FusedCandidate {
	scores := make(map[string]float64)
	earliestList := make(map[string]int)
	var ids []string

	for i, list := range lists {
		w := fuseWeight(i)
		for rank, id := range list {
			scores[id] += w / (RRFK + float64(rank+1))
			if _, seen := earliestList[id]; !seen {
				earliestList[id] = i
				ids = append(ids, id)
			}
		}
	}

	sort.Slice(ids, func(i, j int) bool {
		a, b := ids[i], ids[j]
		if scores[a] != scores[b] {
			return scores[a] > scores[b]
		}
		if earliestList[a] != earliestList[b] {
			return earliestList[a] < earliestList[b]
		}
		return a < b
	})

	candidates := make([]FusedCandidate, len(ids))
	for i, id := range ids {
		candidates[i] = FusedCandidate{ID: id, Score: scores[id]}
	}
	return candidates
}

// Fuse combines two or more ranked id lists into one fused ranking by
// Reciprocal Rank Fusion, returning only the ranked ids — ADR-0010. Fuse is
// a projection of FuseScored (design D1): one implementation of the
// formula and its tie-break, so ADR-0010's own bias lives in exactly one
// place. This reimplementation changes Fuse's body only — its exported
// signature, ordering and three-level tie-break are unchanged (spec R1.2's
// own MUST NOT).
func Fuse(lists ...[]string) []string {
	scored := FuseScored(lists...)
	ids := make([]string, len(scored))
	for i, c := range scored {
		ids[i] = c.ID
	}
	return ids
}
