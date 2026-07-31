package recall

import (
	"fmt"
	"math"
	"sort"
)

// VectorQuery is one vector-leg search request — I21's own anchor comment
// (test/conformance/i21_vector_search_filters_on_model_test.go), design D5.
// Vector is expected unit-normalized, matching what VectorIndex stores
// (design D6). K bounds how many results Search returns; K <= 0 means "no
// bound", returning every entry ranked.
type VectorQuery struct {
	Model  string
	Vector []float32
	K      int
}

// VectorIndex holds every embedding for exactly one model — I21's own
// "Assumed shape" comment states this precisely: a vault holding two
// models holds two VectorIndex values, one per model, never one index
// serving both (design D5, spec R2.3 corrected). IDs and Vectors are
// parallel slices; every row of Vectors shares one dimension, enforced by
// NewVectorIndex.
type VectorIndex struct {
	Model   string
	IDs     []string
	Vectors [][]float32
}

// Scored is one VectorIndex entry, ranked.
type Scored struct {
	ID    string
	Score float32
}

// ErrIDVectorCountMismatch is returned by NewVectorIndex when ids and
// vectors have different lengths — a mismatch that would silently pair the
// wrong id with the wrong row.
var ErrIDVectorCountMismatch = fmt.Errorf("recall: ids and vectors have different lengths")

// ErrRaggedVectors is returned by NewVectorIndex when vectors do not all
// share one dimension — Search cannot consistently score a ragged index.
var ErrRaggedVectors = fmt.Errorf("recall: index vectors do not share one dimension")

// ErrModelMismatch is returned by Search when q.Model differs from
// idx.Model — I21, ADR-0003: "the distance between them is noise shaped
// like a number".
var ErrModelMismatch = fmt.Errorf("recall: query model does not match index model")

// ErrDimMismatch is returned by Search when the query vector's length
// differs from the index's own dimension — a shorter dot product would
// still produce a number, silently.
var ErrDimMismatch = fmt.Errorf("recall: query vector dimension does not match index dimension")

// ErrZeroVector is returned by Normalize when v has zero magnitude — a
// zero vector has no direction, and dividing by its norm yields NaN, which
// sorts arbitrarily and silently (design D6).
var ErrZeroVector = fmt.Errorf("recall: vector has zero magnitude")

// NewVectorIndex builds a VectorIndex for model, validating that ids and
// vectors have matching lengths and that every row of vectors shares one
// dimension — construction is where a ragged index becomes unrepresentable
// (design D6), rather than a bug Search discovers later.
func NewVectorIndex(model string, ids []string, vectors [][]float32) (VectorIndex, error) {
	if len(ids) != len(vectors) {
		return VectorIndex{}, fmt.Errorf("%w: %d ids, %d vectors", ErrIDVectorCountMismatch, len(ids), len(vectors))
	}

	if len(vectors) > 0 {
		dim := len(vectors[0])
		for i, v := range vectors {
			if len(v) != dim {
				return VectorIndex{}, fmt.Errorf("%w: entry %d has dimension %d, want %d", ErrRaggedVectors, i, len(v), dim)
			}
		}
	}

	return VectorIndex{Model: model, IDs: ids, Vectors: vectors}, nil
}

// Search is a pure top-K selection over (VectorQuery, VectorIndex) by dot
// product — ADR-0012's brute-force decision, restated as a core-level
// contract (spec R2.2): exact results, no tuning, no I/O.
func Search(idx VectorIndex, q VectorQuery) ([]Scored, error) {
	if q.Model != idx.Model {
		return nil, fmt.Errorf("%w: query model %q, index model %q", ErrModelMismatch, q.Model, idx.Model)
	}

	if len(idx.Vectors) > 0 && len(q.Vector) != len(idx.Vectors[0]) {
		return nil, fmt.Errorf("%w: query dimension %d, index dimension %d", ErrDimMismatch, len(q.Vector), len(idx.Vectors[0]))
	}

	scored := make([]Scored, len(idx.IDs))
	for i, id := range idx.IDs {
		scored[i] = Scored{ID: id, Score: dot(q.Vector, idx.Vectors[i])}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	k := q.K
	if k <= 0 || k > len(scored) {
		k = len(scored)
	}
	return scored[:k], nil
}

// dot returns the dot product of a and b, assumed equal length — callers
// (Search) guarantee this via NewVectorIndex's ragged-free construction and
// Search's own ErrDimMismatch refusal.
func dot(a, b []float32) float32 {
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// Normalize returns v scaled to unit L2 norm — the storage-boundary
// obligation spec R3.1 states and design D6 places here as a pure function
// the store adapter calls immediately before encoding (design D6).
func Normalize(v []float32) ([]float32, error) {
	var sumSquares float64
	for _, x := range v {
		sumSquares += float64(x) * float64(x)
	}

	norm := math.Sqrt(sumSquares)
	if norm == 0 {
		return nil, ErrZeroVector
	}

	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out, nil
}
