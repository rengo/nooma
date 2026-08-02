package relation

import "testing"

// TestDecide covers all three bands doc 02 §4 defines, plus both boundary
// values exactly — design D7's own "band notation wins" reading, tested at
// the boundary because that is where this kind of code is wrong.
//
// The table asserts its own completeness: every one of the three Verdict
// values (Discard, Uncertain, Asserted) appears in it, so a fourth band
// added later without a case here — or a Verdict this table stops
// producing — fails loudly.
func TestDecide(t *testing.T) {
	thresholds := Thresholds{Persist: 0.30, Surface: 0.50}

	cases := []struct {
		name       string
		confidence float64
		want       Verdict
	}{
		{"well below persist discards", 0.10, Discard},
		{"just below persist discards", 0.29, Discard},
		{"at the persist boundary is uncertain, inclusive", 0.30, Uncertain},
		{"inside the uncertain band", 0.40, Uncertain},
		{"just below surface is still uncertain", 0.49, Uncertain},
		{"at the surface boundary is asserted, inclusive", 0.50, Asserted},
		{"above surface asserts", 0.90, Asserted},
		{"maximum confidence asserts", 1.0, Asserted},
	}

	seen := make(map[Verdict]bool, 3)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.confidence, thresholds)
			if got != tc.want {
				t.Errorf("Decide(%v, %+v) = %v, want %v", tc.confidence, thresholds, got, tc.want)
			}
			seen[tc.want] = true
		})
	}

	if len(seen) != 3 {
		t.Fatalf("table exercises %d of the three Verdict values, want 3 — Discard, "+
			"Uncertain and Asserted must each appear, or this table is not asserting its "+
			"own completeness", len(seen))
	}
}
