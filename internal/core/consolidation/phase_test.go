package consolidation

import (
	"errors"
	"testing"
)

// TestOrder_HasExactlyEightPhasesAscendingWithLearnLast is the C14 length
// guard: it must fail on Order()'s length before any name assertion runs,
// against a zero-value stub that returns nil.
func TestOrder_HasExactlyEightPhasesAscendingWithLearnLast(t *testing.T) {
	order := Order()
	if len(order) != 8 {
		t.Fatalf("Order() returned %d phases, want 8: %v", len(order), order)
	}
	for i, p := range order {
		if int(p) != i {
			t.Fatalf("Order()[%d] = %v (int %d), want ascending from Phase(0): %v", i, p, int(p), order)
		}
	}
	if order[7] != PhaseLearn {
		t.Fatalf("Order()[7] = %v, want PhaseLearn — I11: learn is always last", order[7])
	}
}

// TestOrder_ReturnsAFreshSlice guards against a shared backing array: a
// caller mutating one call's result must not affect a later call.
func TestOrder_ReturnsAFreshSlice(t *testing.T) {
	a := Order()
	if len(a) == 0 {
		t.Fatal("Order() returned zero phases — nothing to mutate-guard")
	}
	a[0] = Phase(-1)

	b := Order()
	if len(b) == 0 {
		t.Fatal("Order() returned zero phases on the second call — nothing to compare")
	}
	if b[0] == Phase(-1) {
		t.Fatal("Order() does not return a fresh slice — mutating one call's result affected a later call")
	}
}

// TestPhaseString_IsTotalAndNeverPanics sweeps a range including negative
// and above-range ints. R1.1: String() must never panic and must render
// something for every int value.
func TestPhaseString_IsTotalAndNeverPanics(t *testing.T) {
	for n := -5; n < int(phaseCount)+5; n++ {
		p := Phase(n)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Phase(%d).String() panicked: %v", n, r)
				}
			}()
			s := p.String()
			if s == "" {
				t.Errorf("Phase(%d).String() returned an empty string, want a non-empty rendering for every int value", n)
			}
		}()
	}
}

// TestParsePhase_RoundTripsEveryOrderMember: ParsePhase(s.String()) must
// round-trip to s, for every s in Order().
func TestParsePhase_RoundTripsEveryOrderMember(t *testing.T) {
	for _, want := range Order() {
		name := want.String()
		got, err := ParsePhase(name)
		if err != nil {
			t.Fatalf("ParsePhase(%q) returned error %v, want nil", name, err)
		}
		if got != want {
			t.Fatalf("ParsePhase(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestParsePhase_RejectsUnknownText: an unknown string must be rejected
// with ErrUnknownPhase, never silently accepted or mapped to a zero value.
func TestParsePhase_RejectsUnknownText(t *testing.T) {
	_, err := ParsePhase("not-a-phase")
	if !errors.Is(err, ErrUnknownPhase) {
		t.Fatalf("ParsePhase(%q) error = %v, want ErrUnknownPhase", "not-a-phase", err)
	}
}

// TestPhase_NamedConstantsMatchTheirPositionAndName pins each of the
// eight named constants to both its expected position in Order() and its
// expected String() name — a relative oracle (ascending, as
// TestOrder_HasExactlyEightPhasesAscendingWithLearnLast checks) cannot
// tell a correct sequence from two adjacent phase names swapped in the
// const block, since either arrangement is still "ascending". This table
// pins against the computed literal instead.
func TestPhase_NamedConstantsMatchTheirPositionAndName(t *testing.T) {
	tests := []struct {
		phase Phase
		name  string
		want  string
	}{
		{PhaseExpireIncomplete, "PhaseExpireIncomplete", "expire_incomplete"},
		{PhaseArchive, "PhaseArchive", "archive"},
		{PhaseStrengthen, "PhaseStrengthen", "strengthen"},
		{PhaseConnect, "PhaseConnect", "connect"},
		{PhaseDerive, "PhaseDerive", "derive"},
		{PhaseReweight, "PhaseReweight", "reweight"},
		{PhasePatternEval, "PhasePatternEval", "pattern_eval"},
		{PhaseLearn, "PhaseLearn", "learn"},
	}

	order := Order()
	if len(order) != len(tests) {
		t.Fatalf("Order() has %d phases, want %d", len(order), len(tests))
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if order[i] != tt.phase {
				t.Fatalf("Order()[%d] = %v, want %s at that position", i, order[i], tt.name)
			}
			if got := tt.phase.String(); got != tt.want {
				t.Fatalf("%s.String() = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
