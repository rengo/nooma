package recall

import (
	"errors"
	"testing"
)

// mustIndex builds a VectorIndex or fails the test — a fixture helper, not
// itself a test of NewVectorIndex's error paths (see TestNewVectorIndex_Errors).
func mustIndex(t *testing.T, model string, ids []string, vecs [][]float32) VectorIndex {
	t.Helper()
	idx, err := NewVectorIndex(model, ids, vecs)
	if err != nil {
		t.Fatalf("NewVectorIndex(%q): %v", model, err)
	}
	return idx
}

// TestSearch covers spec R2.1–R2.3 and design D5/D6 in one table: ranking
// the returned []Scored by hand-computed dot product, K truncation, I21's
// behavioral half (a VectorIndex never surfaces another index's entries,
// regardless of raw score), and the two refusals (model/dim mismatch).
func TestSearch(t *testing.T) {
	// idxA holds model-a's entries. Hand-computed dot products against
	// query {0.8, 0.6}: a1 = 0.80, a2 = 0.60, a3 = 0.96 — ranked a3, a1, a2.
	idxA := mustIndex(t, "model-a", []string{"a1", "a2", "a3"}, [][]float32{
		{1, 0}, {0, 1}, {0.6, 0.8},
	})
	// idxB is a SEPARATE index (model-b). b1's dot product against the same
	// query is 1.0 — higher than every idxA entry — yet idxB is never
	// passed to Search below, which is what proves I21's behavioral half:
	// there is no call that lets b1 outrank idxA's own entries.
	_ = mustIndex(t, "model-b", []string{"b1"}, [][]float32{{0.8, 0.6}})

	q := VectorQuery{Model: "model-a", Vector: []float32{0.8, 0.6}}

	tests := []struct {
		name    string
		idx     VectorIndex
		q       VectorQuery
		wantIDs []string
		wantErr error
	}{
		{"ranks by dot product descending", idxA, VectorQuery{Model: q.Model, Vector: q.Vector, K: 3}, []string{"a3", "a1", "a2"}, nil},
		{"K truncates the ranked result", idxA, VectorQuery{Model: q.Model, Vector: q.Vector, K: 2}, []string{"a3", "a1"}, nil},
		{"model mismatch refuses (I21)", idxA, VectorQuery{Model: "model-b", Vector: []float32{1, 0}, K: 1}, nil, ErrModelMismatch},
		{"dim mismatch refuses", idxA, VectorQuery{Model: q.Model, Vector: []float32{1, 0, 0}, K: 1}, nil, ErrDimMismatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Search(tt.idx, tt.q)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Search() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("Search returned %d results, want %d: %v", len(got), len(tt.wantIDs), got)
			}
			for i, want := range tt.wantIDs {
				if got[i].ID != want {
					t.Errorf("result[%d].ID = %q, want %q (full result: %v)", i, got[i].ID, want, got)
				}
			}
		})
	}
}

// TestNewVectorIndex_Errors proves construction refuses a mismatched
// id/vector count and a ragged (non-uniform-dimension) vector set — the
// two shapes Search could not otherwise score consistently (design D6).
func TestNewVectorIndex_Errors(t *testing.T) {
	if _, err := NewVectorIndex("m", []string{"u1", "u2"}, [][]float32{{1, 0}}); !errors.Is(err, ErrIDVectorCountMismatch) {
		t.Errorf("mismatched id/vector count: error = %v, want ErrIDVectorCountMismatch", err)
	}
	if _, err := NewVectorIndex("m", []string{"u1", "u2"}, [][]float32{{1, 0}, {1, 0, 0}}); !errors.Is(err, ErrRaggedVectors) {
		t.Errorf("ragged vectors: error = %v, want ErrRaggedVectors", err)
	}
}

// TestNormalize proves the L2-normalization ADR-0012 requires (a scaled
// vector normalizes to the same direction with magnitude 1) and the
// ErrZeroVector refusal design D6 states: a zero vector has no direction,
// and dividing by its norm would silently yield NaN.
func TestNormalize(t *testing.T) {
	got, err := Normalize([]float32{3, 4}) // 3-4-5 triangle: magnitude 5
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	want := []float32{0.6, 0.8}
	for i := range want {
		if diff := got[i] - want[i]; diff > 1e-6 || diff < -1e-6 {
			t.Errorf("Normalize()[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	if _, err := Normalize([]float32{0, 0, 0}); !errors.Is(err, ErrZeroVector) {
		t.Errorf("zero vector: error = %v, want ErrZeroVector", err)
	}
}
