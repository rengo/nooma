package brain

import (
	"math"
	"testing"

	"github.com/rengo/nooma/internal/core/prospection"
)

// TestInterruptColumn_RoundTripsEveryResolution proves interruptColumn and
// prospection.ResolveInterrupt are inverses: resolving a column value,
// converting it back, and resolving that again lands on the same
// Interrupt, for every Interrupt ResolveInterrupt can produce.
//
// The identity is what makes the NULL <-> degraded contract checkable at
// all. Written as a round trip rather than as a table of expected pointers
// because the property is the point: a change to either side that breaks
// the pairing fails here, whichever side moved.
//
// L1, white-box, no SQLite: the column's storage behaviour is L3's
// (internal/store/sqlite/triggerrepo_integration_test.go), and this is the
// conversion, which is a decision about a value.
func TestInterruptColumn_RoundTripsEveryResolution(t *testing.T) {
	pushThreshold := float64(prospection.PushThreshold)
	nan := math.NaN()
	inf := math.Inf(1)
	outOfRange := 1.7
	zero := 0.0
	one := 1.0

	for _, tc := range []struct {
		name         string
		input        *float64
		wantColumn   *float64
		wantDegraded bool
	}{
		{"absent stays absent", nil, nil, true},
		{"a claimed 0.0 is not the same as no claim", &zero, &zero, false},
		{"the push threshold survives", &pushThreshold, &pushThreshold, false},
		{"1.0 survives", &one, &one, false},
		{"NaN degrades to NULL", &nan, nil, true},
		{"+Inf degrades to NULL", &inf, nil, true},
		{"out of range degrades to NULL", &outOfRange, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved := prospection.ResolveInterrupt(tc.input)
			column := interruptColumn(resolved)

			switch {
			case tc.wantColumn == nil && column != nil:
				t.Fatalf("interruptColumn = %v, want nil", *column)
			case tc.wantColumn != nil && column == nil:
				t.Fatalf("interruptColumn = nil, want %v", *tc.wantColumn)
			case tc.wantColumn != nil && *column != *tc.wantColumn:
				t.Fatalf("interruptColumn = %v, want %v", *column, *tc.wantColumn)
			}

			// The round trip: what the column holds, read back, resolves
			// to the same Interrupt it came from.
			reresolved := prospection.ResolveInterrupt(column)
			if reresolved != resolved {
				t.Fatalf("re-resolving the column gives %+v, want %+v", reresolved, resolved)
			}
			if resolved.Degraded() != tc.wantDegraded {
				t.Fatalf("Degraded() = %v, want %v", resolved.Degraded(), tc.wantDegraded)
			}
		})
	}
}

// TestInterruptColumn_DegradedIsNullAndNotZero is the one case worth its
// own test, because it is the one a plausible implementation gets wrong:
// writing 0.0 for a degraded Interrupt would look correct — the level IS
// DefaultInterruptLevel — and would destroy the distinction the column
// exists to carry. A 0.0 read back reports Degraded() == false, so "no
// claim was made" would silently become "the user claimed zero".
func TestInterruptColumn_DegradedIsNullAndNotZero(t *testing.T) {
	degraded := prospection.ResolveInterrupt(nil)
	if !degraded.Degraded() {
		t.Fatal("ResolveInterrupt(nil) is not degraded — this test's premise is gone")
	}

	if column := interruptColumn(degraded); column != nil {
		t.Fatalf("interruptColumn(degraded) = %v, want nil — 0.0 is a claim, absence is not", *column)
	}

	zero := 0.0
	claimed := prospection.ResolveInterrupt(&zero)
	if claimed.Degraded() {
		t.Fatal("a claimed 0.0 reports itself degraded — the two are no longer distinguishable")
	}
	if column := interruptColumn(claimed); column == nil || *column != 0 {
		t.Fatalf("interruptColumn(claimed 0.0) = %v, want a pointer to 0", column)
	}
}
