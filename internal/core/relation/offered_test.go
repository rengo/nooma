package relation

import "testing"

// TestTargetOffered covers ADR-0026: the judge answers about what it was
// shown, and an answer about anything else decided nothing.
//
// The rule exists because the two judge call sites hand the model a specific
// candidate list and then trusted the ID that came back. In the consolidation
// pass the list is exactly one unit, so any other ID is wrong by construction;
// on the capture path it is at most relation.DedupCandidateK of them.
func TestTargetOffered(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		target  string
		offered []string
		want    bool
	}{
		{
			name:    "the only candidate there was",
			target:  "unit-7",
			offered: []string{"unit-7"},
			want:    true,
		},
		{
			name:    "one of several",
			target:  "unit-3",
			offered: []string{"unit-1", "unit-3", "unit-9"},
			want:    true,
		},
		{
			name:    "a unit that exists but was never shown",
			target:  "unit-42",
			offered: []string{"unit-1", "unit-3"},
			want:    false,
		},
		{
			name:    "nothing was offered",
			target:  "unit-1",
			offered: nil,
			want:    false,
		},
		{
			name:    "an empty answer is not a match for an empty slot",
			target:  "",
			offered: []string{"unit-1"},
			want:    false,
		},
		{
			name:    "an empty answer does not match an empty candidate either",
			target:  "",
			offered: []string{""},
			want:    false,
		},
		{
			name:    "IDs are compared exactly, not by prefix",
			target:  "unit-1",
			offered: []string{"unit-10", "unit-100"},
			want:    false,
		},
		{
			name:    "IDs are compared exactly, not case-insensitively",
			target:  "UNIT-1",
			offered: []string{"unit-1"},
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := TargetOffered(tc.target, tc.offered); got != tc.want {
				t.Errorf("TargetOffered(%q, %v) = %v, want %v", tc.target, tc.offered, got, tc.want)
			}
		})
	}
}

// TestTargetOfferedDoesNotMutateItsInput pins the purity internal/core owes
// its callers: brain hands this function the candidate slice it is about to
// reuse for the decision_log row, and a predicate that sorted in place would
// corrupt the record of what was actually shown.
func TestTargetOfferedDoesNotMutateItsInput(t *testing.T) {
	t.Parallel()

	offered := []string{"unit-9", "unit-1", "unit-5"}
	before := append([]string(nil), offered...)

	TargetOffered("unit-1", offered)

	for i := range before {
		if offered[i] != before[i] {
			t.Fatalf("TargetOffered reordered its input: got %v, want %v", offered, before)
		}
	}
}
