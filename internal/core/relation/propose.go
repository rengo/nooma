package relation

// TypeDuplicate is the type a duplicate is stored under (doc 02 §4: a
// duplicate is recorded as a relation, not merged). The judge is told to omit
// a type for one.
const TypeDuplicate = "duplicate"

// Proposal is the relation a judgment proposes, before its type's thresholds
// decide whether it is stored: what capture, the nightly connect and the
// recorded judge set (internal/reading) all read a judgment as.
//
// Its fields are unexported so Propose is the only way to build one outside
// this package: a proposal therefore never names a target the judge was not
// shown (ADR-0026).
type Proposal struct {
	outcome      Outcome
	targetUnitID string
	typ          string
	strength     float64
	confidence   float64
}

// Outcome is the judge's outcome: related or duplicate.
func (p Proposal) Outcome() Outcome { return p.outcome }

// TargetUnitID is the candidate the judge named, one it was shown.
func (p Proposal) TargetUnitID() string { return p.targetUnitID }

// Type is the judge's type for a related one, TypeDuplicate for a duplicate.
func (p Proposal) Type() string { return p.typ }

// Strength is the judge's strength, 0 when it gave none: doc 02 §4 does not
// count it among the fields whose absence means nothing was decided.
func (p Proposal) Strength() float64 { return p.strength }

// Confidence is the judge's confidence, for Decide.
func (p Proposal) Confidence() float64 { return p.confidence }

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
	p := Proposal{outcome: *j.Outcome, targetUnitID: *j.TargetUnitID, typ: TypeDuplicate, confidence: *j.Confidence}
	if p.outcome == OutcomeRelated {
		if j.Type == nil || *j.Type == "" {
			return Proposal{}, DecidedNothing
		}
		p.typ = *j.Type
	}
	if j.Strength != nil {
		p.strength = *j.Strength
	}
	return p, Proposed
}
