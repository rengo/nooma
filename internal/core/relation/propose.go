package relation

// TypeDuplicate is the type a duplicate is stored under (doc 02 §4: a
// duplicate is recorded as a relation, not merged). The judge is told to omit
// a type for one.
const TypeDuplicate = "duplicate"

// Proposal is the relation a judgment proposes, before its type's thresholds
// decide whether it is stored: what capture, the nightly connect and the
// recorded judge set (internal/reading) all read a judgment as.
type Proposal struct {
	Outcome      Outcome
	TargetUnitID string
	// Type is the judge's type for a related one, TypeDuplicate for a
	// duplicate.
	Type string
	// Strength is 0 when the judge gave none: doc 02 §4 does not count it
	// among the fields whose absence means nothing was decided.
	Strength   float64
	Confidence float64
}

// Reading is what Propose made of a judgment.
type Reading int

const (
	// DecidedNothing: outcome new, or the outcome, the confidence or the
	// target missing, or a related one with no type to store it under. Doc
	// 02 §4: a judgment that decided nothing writes nothing, not even a row.
	DecidedNothing Reading = iota
	// TargetUnknown: the judge named a unit it was not shown (ADR-0026). It
	// stores nothing either, but said something impossible, and the caller
	// records it.
	TargetUnknown
	// Proposed: the Proposal stands, for Decide to place in its band.
	Proposed
)

// Propose reads a judgment as the relation it proposes, offered being the
// candidate ids the judge was shown. It is the one rule both judge call sites
// apply before their type's thresholds (Decide), so that a judgment cannot be
// stored by one and dropped by the other.
func Propose(j Judgment, offered []string) (Proposal, Reading) {
	if j.Outcome == nil || *j.Outcome == OutcomeNew || j.Confidence == nil || j.TargetUnitID == nil {
		return Proposal{}, DecidedNothing
	}
	if !TargetOffered(*j.TargetUnitID, offered) {
		return Proposal{}, TargetUnknown
	}
	p := Proposal{Outcome: *j.Outcome, TargetUnitID: *j.TargetUnitID, Type: TypeDuplicate, Confidence: *j.Confidence}
	if p.Outcome == OutcomeRelated {
		if j.Type == nil || *j.Type == "" {
			return Proposal{}, DecidedNothing
		}
		p.Type = *j.Type
	}
	if j.Strength != nil {
		p.Strength = *j.Strength
	}
	return p, Proposed
}
