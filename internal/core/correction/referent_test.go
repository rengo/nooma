package correction

import (
	"testing"

	"github.com/rengo/nooma/internal/core/recall"
)

// TestReferent covers R1.3's own Verified-by table: zero candidates, one
// candidate, and the ratio boundary at ReferentMargin approached from both
// sides plus exactly on it — pinned inclusive on the pick side, per doc 02
// §5 step 4's "closer together than" being a strict inequality on the ask
// side only (design D2, m1b D7's own lesson about conf == Surface applied
// here).
func TestReferent(t *testing.T) {
	cases := []struct {
		name   string
		cands  []recall.FusedCandidate
		margin float64
		wantID string
		wantOK bool
	}{
		{
			name:   "zero candidates asks — nothing to correct",
			cands:  nil,
			margin: ReferentMargin,
			wantID: "",
			wantOK: false,
		},
		{
			name:   "one candidate picks — no second score to be close to",
			cands:  []recall.FusedCandidate{{ID: "solo", Score: 0.001}},
			margin: ReferentMargin,
			wantID: "solo",
			wantOK: true,
		},
		{
			name: "ratio just below the margin asks — 2.9998/2.0 = 1.4999",
			cands: []recall.FusedCandidate{
				{ID: "top", Score: 2.9998},
				{ID: "second", Score: 2.0},
			},
			margin: ReferentMargin,
			wantID: "",
			wantOK: false,
		},
		{
			name: "ratio exactly at the margin picks — 3.0/2.0 = 1.5, the boundary is inclusive",
			cands: []recall.FusedCandidate{
				{ID: "top", Score: 3.0},
				{ID: "second", Score: 2.0},
			},
			margin: ReferentMargin,
			wantID: "top",
			wantOK: true,
		},
		{
			name: "ratio just above the margin picks — 3.0002/2.0 = 1.5001",
			cands: []recall.FusedCandidate{
				{ID: "top", Score: 3.0002},
				{ID: "second", Score: 2.0},
			},
			margin: ReferentMargin,
			wantID: "top",
			wantOK: true,
		},
		{
			// top/second = 10.0/8.0 = 1.25 < 1.5 -> ask. A third candidate
			// scored 1.0 would make top/third = 10.0 >= 1.5 -> pick, if it
			// were allowed to participate. Only cands[0] and cands[1] ever
			// do — R1.3's own MUST.
			name: "a third candidate never participates, even though it would flip the answer if it did",
			cands: []recall.FusedCandidate{
				{ID: "top", Score: 10.0},
				{ID: "second", Score: 8.0},
				{ID: "third", Score: 1.0},
			},
			margin: ReferentMargin,
			wantID: "",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotOK := Referent(tc.cands, tc.margin)
			if gotID != tc.wantID || gotOK != tc.wantOK {
				t.Errorf("Referent(%v, %v) = (%q, %v), want (%q, %v)",
					tc.cands, tc.margin, gotID, gotOK, tc.wantID, tc.wantOK)
			}
		})
	}
}

// TestReferentMargin pins design D2/R1.4's named constant against doc 02
// §13's correction_referent_margin row: the Go constant must equal it, and
// Referent must reference this constant rather than a re-literaled value at
// any call site (docs/06-harness.md §7's calibratable-number rule).
func TestReferentMargin(t *testing.T) {
	if ReferentMargin != 1.5 {
		t.Errorf("ReferentMargin = %v, want 1.5 (doc 02 §13's correction_referent_margin)", ReferentMargin)
	}
}
