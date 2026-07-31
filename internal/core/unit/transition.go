package unit

import "fmt"

// legalTransitions is the closed set of (from, to) status pairs a unit may
// move through, doc 02 §1/§2/§6.1 and I20. Unexported and data, not
// control flow (design D3, following M0's D10 "rules as data" precedent):
// an exported map would be mutable by any importer, the same defect that
// made AllStatuses a function instead of a var.
//
// incomplete -> archived is expiry's landing status: doc 02 says an
// unresolved incomplete unit is "expired after 24 h", the vocabulary has
// no expired member, and I03 forbids deletion — archived is the only
// status left. Self-transitions are deliberately absent: permitting
// pool -> pool would let the brain write a no-op UPDATE while logging an
// effect in decision_log, violating I12.
var legalTransitions = map[Status]map[Status]bool{
	StatusPool:       {StatusArchived: true, StatusSuperseded: true},
	StatusArchived:   {StatusPool: true},
	StatusIncomplete: {StatusPool: true, StatusArchived: true},
	StatusSuperseded: {},
}

// ErrIllegalTransition is returned by ValidateTransition when (from, to)
// is not one of legalTransitions' pairs.
var ErrIllegalTransition = fmt.Errorf("illegal status transition")

// ValidateTransition reports whether from -> to is a legal status
// transition. core/unit decides; brain/ is the only layer that acts on
// the decision (design D3) — the store never re-validates.
//
// It returns ErrUnknownStatus if either value is not an AllStatuses()
// member, and ErrIllegalTransition otherwise if the pair is not one of
// legalTransitions'.
func ValidateTransition(from, to Status) error {
	if _, err := ParseStatus(string(from)); err != nil {
		return err
	}
	if _, err := ParseStatus(string(to)); err != nil {
		return err
	}
	if legalTransitions[from][to] {
		return nil
	}
	return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, from, to)
}
