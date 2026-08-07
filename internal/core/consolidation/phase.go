package consolidation

import (
	"errors"
	"fmt"
)

// Phase is one of doc 02 §6's eight nightly phases. Its VALUE is its
// position: Phase(0) runs first and phaseCount-1 runs last. The order is
// not data the sequence carries, it is the type's own numbering.
type Phase int

const (
	PhaseExpireIncomplete Phase = iota
	PhaseArchive
	PhaseStrengthen
	PhaseConnect
	PhaseDerive
	PhaseReweight
	PhasePatternEval
	PhaseLearn

	phaseCount // not a phase: the count, and Order's upper bound
)

// Compile-time proof of doc 02 §6's "learn is ALWAYS last" (I11) —
// design.md §3.2 leg 1, the strongest of the four legs and the only one
// that is a proof rather than a test.
//
// Proves: PhaseLearn occupies the last slot, phaseCount-1. If a phase
// constant is ever declared between PhaseLearn and phaseCount, or the
// const block above is reordered so PhaseLearn is no longer the greatest
// value, this expression goes negative and uint() cannot convert a
// negative constant — the package stops compiling, on go build, go vet,
// every editor and every CI job at once, not merely a test that could be
// skipped, weakened or deleted.
//
// Does NOT prove: that there are exactly eight phases (leg 3's doc-parse
// half — test/conformance/i11_consolidation_phase_order_test.go, checked
// against docs/02-cognitive-core.md §6's own arrow line) or that a runner
// ever executes Order()'s sequence (I11's behavioural half, m2c's).
const _ uint = uint(int(PhaseLearn) - int(phaseCount) + 1)

// ErrUnknownPhase is returned by ParsePhase when s does not name a member
// of Order().
var ErrUnknownPhase = errors.New("consolidation: unknown phase")

// phaseNames is an array sized by phaseCount, not a slice literal — this
// is what makes leg 3's totality check meaningful (design.md §3.2): a
// ninth phase constant declared before PhaseLearn and left unnamed here
// yields "" at that index, so String() (and the doc 02 pin) fails on an
// empty name rather than on a subtle mis-order.
func phaseNames() [phaseCount]string {
	return [phaseCount]string{
		PhaseExpireIncomplete: "expire_incomplete",
		PhaseArchive:          "archive",
		PhaseStrengthen:       "strengthen",
		PhaseConnect:          "connect",
		PhaseDerive:           "derive",
		PhaseReweight:         "reweight",
		PhasePatternEval:      "pattern_eval",
		PhaseLearn:            "learn",
	}
}

// Order returns a fresh slice holding the eight Phase vocabulary members,
// ascending from Phase(0), with Order()[7] == PhaseLearn (R1.1).
//
// Generated from [0, phaseCount), never a slice literal: there is no list
// of eight names anywhere in this package for a reorder to leave stale —
// permuting the sequence means renumbering the constants above, which
// simultaneously changes every String() output and ParsePhase round-trip
// (design.md §3.2 leg 2).
func Order() []Phase {
	order := make([]Phase, 0, phaseCount)
	for p := Phase(0); p < phaseCount; p++ {
		order = append(order, p)
	}
	return order
}

// String renders p's name, or "Phase(n)" for a value outside Order() —
// total over every int value, never panics (R1.1).
func (p Phase) String() string {
	if p < 0 || p >= phaseCount {
		return fmt.Sprintf("Phase(%d)", int(p))
	}
	return phaseNames()[p]
}

// ParsePhase is the sole entry point from untrusted text — a CLI flag —
// into the Phase vocabulary. It returns ErrUnknownPhase, naming the
// rejected value, for anything that is not one of Order()'s eight names.
func ParsePhase(s string) (Phase, error) {
	for _, p := range Order() {
		if p.String() == s {
			return p, nil
		}
	}
	return 0, fmt.Errorf("%w: %q", ErrUnknownPhase, s)
}
