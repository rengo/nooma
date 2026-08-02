package relation

import "testing"

// TestDedupCandidateK pins the constant design D5/D7 name — the bound on how
// many recall candidates the relation judge is asked about per capture —
// and guards it against silently becoming non-positive, which would starve
// the judge of every candidate. docs/02-cognitive-core.md §13 carries the
// same 5 in its calibration table (task 11a.3); this is the L1 half design
// D9 requires of every exported core declaration.
func TestDedupCandidateK(t *testing.T) {
	if DedupCandidateK != 5 {
		t.Fatalf("DedupCandidateK = %d, want 5 — docs/02-cognitive-core.md §13's row and this "+
			"constant must be one number, not two that drift", DedupCandidateK)
	}
	if DedupCandidateK <= 0 {
		t.Fatalf("DedupCandidateK = %d, want > 0 — a non-positive bound leaves the judge with no "+
			"candidates to evaluate", DedupCandidateK)
	}
}

// TestResolve covers Q1's closed answer: a nil row (no relation_thresholds
// entry for this type yet) falls back to the two package defaults, and a
// present row passes through unchanged rather than being merged with them.
func TestResolve(t *testing.T) {
	t.Run("nil row falls back to the defaults", func(t *testing.T) {
		got := Resolve(nil)
		want := Thresholds{Persist: DefaultMinConfidenceToPersist, Surface: DefaultMinConfidenceToSurface}
		if got != want {
			t.Errorf("Resolve(nil) = %+v, want %+v", got, want)
		}
	})

	t.Run("a present row passes through, not merged with the defaults", func(t *testing.T) {
		row := &Thresholds{Persist: 0.35, Surface: 0.60}
		got := Resolve(row)
		if got != *row {
			t.Errorf("Resolve(%+v) = %+v, want the row unchanged", *row, got)
		}
	})
}
