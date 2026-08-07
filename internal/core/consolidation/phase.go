package consolidation

import "errors"

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

// Order returns a fresh slice holding the eight Phase vocabulary members,
// ascending from Phase(0), with Order()[7] == PhaseLearn (R1.1).
func Order() []Phase {
	return nil
}

// String renders p's name, or "Phase(n)" for a value outside Order() —
// total over every int value, never panics (R1.1).
func (p Phase) String() string {
	return ""
}

// ParsePhase is the sole entry point from untrusted text — a CLI flag —
// into the Phase vocabulary. It returns ErrUnknownPhase, naming the
// rejected value, for anything that is not one of Order()'s eight names.
func ParsePhase(s string) (Phase, error) {
	return 0, ErrUnknownPhase
}
