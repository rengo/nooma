// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rengo/nooma/internal/core/recall"
	"github.com/rengo/nooma/test/support/goldenset"
)

// TestRecallCorpusFusesToItsExpectedRanking is the test that makes
// testdata/recall/cases/ a corpus rather than a set of files that parse.
//
// design.md §6's test matrix names it and attributes it to PR 8c — "the
// recall corpus: Search over stated vectors + Fuse over the stated lexical
// ranking equals expected_unit_ids" — and tasks.md's PR 8c never carried a
// task for it. The corpus would otherwise have landed with nothing reading
// it: goldenset's own tests check that a case is well-shaped, which is a
// claim about JSON, not about recall.
//
// What it runs, per query, is the real pipeline's pure half with the store
// removed: recall.Search over the units' stated vectors gives the vector
// leg's ranking, the case's stated lexical_ranking gives the other, and
// recall.Fuse over both must reproduce expected_unit_ids exactly — order
// included, because the ordering IS the behaviour under test.
//
// The vectors and the lexical ranking are stated in the case file rather
// than computed. fakeprovider's embedder is FNV-1a over the whole
// unnormalized string at dimension 8; a corpus driven by it would pin the
// hash function and call it recall. The real FTS5 leg reproducing a case's
// stated lexical_ranking is PR 9c's L3 test — this one closes the pure
// half, at L2, where it can run in the fast loop.
func TestRecallCorpusFusesToItsExpectedRanking(t *testing.T) {
	repoRoot := repoRootFromCaller(t)
	casesDir := filepath.Join(repoRoot, "testdata", "recall", "cases")

	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatalf("reading %s: %v", casesDir, err)
	}

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		loaded++

		t.Run(entry.Name(), func(t *testing.T) {
			var example goldenset.RecallExample
			path := filepath.Join(casesDir, entry.Name())
			if err := goldenset.Load(path, &example); err != nil {
				t.Fatalf("loading %s: %v", path, err)
			}

			ids := make([]string, 0, len(example.Units))
			vectors := make([][]float32, 0, len(example.Units))
			for _, u := range example.Units {
				ids = append(ids, u.ID)
				vectors = append(vectors, u.Vector)
			}

			// One model, because a VectorIndex is scoped to one (I21).
			// The corpus states no model name; the index and the query
			// agree on a fixed one so Search's model guard is satisfied
			// without the corpus having to carry a field it does not need.
			const corpusModel = "corpus"
			idx, err := recall.NewVectorIndex(corpusModel, ids, vectors)
			if err != nil {
				t.Fatalf("building the index for %s: %v", example.ID, err)
			}

			for _, q := range example.Queries {
				scored, err := recall.Search(idx, recall.VectorQuery{
					Model:  corpusModel,
					Vector: q.Vector,
					K:      len(ids),
				})
				if err != nil {
					t.Fatalf("Search for query %q: %v", q.Query, err)
				}

				vectorRanking := make([]string, len(scored))
				for i, s := range scored {
					vectorRanking[i] = s.ID
				}

				got := recall.Fuse(vectorRanking, q.LexicalRanking)

				if len(got) != len(q.ExpectedUnitIDs) {
					t.Fatalf("query %q fused to %d ids, want %d\n got: %v\nwant: %v",
						q.Query, len(got), len(q.ExpectedUnitIDs), got, q.ExpectedUnitIDs)
				}
				for i := range got {
					if got[i] != q.ExpectedUnitIDs[i] {
						t.Errorf("query %q fused ranking differs at position %d: got %q, want %q\n got: %v\nwant: %v\n"+
							"vector leg: %v\nlexical leg: %v",
							q.Query, i, got[i], q.ExpectedUnitIDs[i], got, q.ExpectedUnitIDs,
							vectorRanking, q.LexicalRanking)
					}
				}
			}
		})
	}

	// design D10's non-empty-corpus guard, in the shape this package already
	// uses elsewhere: a directory that lost its cases must fail loudly, not
	// pass by having nothing to disagree with.
	if loaded == 0 {
		t.Fatal("loaded zero cases from testdata/recall/cases — the corpus this test exists to read is empty")
	}
}
