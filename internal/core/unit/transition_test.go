package unit

import (
	"errors"
	"testing"
)

// TestValidateTransition_ExactlyFivePairsAreLegal is an exhaustive table
// over all 16 ordered pairs from AllStatuses() x AllStatuses(), asserting
// ValidateTransition returns nil for exactly the five legal pairs design
// D3 names (per C1's resolution of spec.md R2.3 vs design.md D3) and
// ErrIllegalTransition for the other eleven, including all four
// self-pairs: a status transitioning to itself is illegal, not a no-op —
// permitting pool -> pool would let the brain write a no-op UPDATE while
// logging an effect (I12).
//
// The test asserts its own completeness (design D9 point 3): the
// expectation map's size equals len(AllStatuses())^2, so a status added
// later without a matching expectation fails loudly instead of silently
// reading as illegal.
func TestValidateTransition_ExactlyFivePairsAreLegal(t *testing.T) {
	type pair struct{ from, to Status }

	legal := map[pair]bool{
		{StatusPool, StatusArchived}:       true,
		{StatusPool, StatusSuperseded}:     true,
		{StatusArchived, StatusPool}:       true,
		{StatusIncomplete, StatusPool}:     true,
		{StatusIncomplete, StatusArchived}: true,
	}

	cases := map[pair]bool{}
	for _, from := range AllStatuses() {
		for _, to := range AllStatuses() {
			cases[pair{from, to}] = legal[pair{from, to}]
		}
	}

	if want := len(AllStatuses()) * len(AllStatuses()); len(cases) != want {
		t.Fatalf("test table covers %d pairs, want %d (len(AllStatuses())^2) — table is out of sync", len(cases), want)
	}
	if len(legal) != 5 {
		t.Fatalf("legal-pair table names %d pairs, want exactly 5 (design D3)", len(legal))
	}

	for p, wantLegal := range cases {
		err := ValidateTransition(p.from, p.to)
		if wantLegal {
			if err != nil {
				t.Errorf("ValidateTransition(%q, %q) = %v, want nil (legal)", p.from, p.to, err)
			}
			continue
		}
		if !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("ValidateTransition(%q, %q) = %v, want ErrIllegalTransition", p.from, p.to, err)
		}
	}
}

// TestValidateTransition_RejectsUnknownStatus proves ValidateTransition
// reuses ParseStatus's sentinel (2.2) for out-of-vocabulary inputs, rather
// than reporting an unknown status as merely illegal.
func TestValidateTransition_RejectsUnknownStatus(t *testing.T) {
	err := ValidateTransition(Status("bogus"), StatusPool)
	if !errors.Is(err, ErrUnknownStatus) {
		t.Errorf("ValidateTransition(bogus, pool) = %v, want ErrUnknownStatus", err)
	}

	err = ValidateTransition(StatusPool, Status("bogus"))
	if !errors.Is(err, ErrUnknownStatus) {
		t.Errorf("ValidateTransition(pool, bogus) = %v, want ErrUnknownStatus", err)
	}
}
