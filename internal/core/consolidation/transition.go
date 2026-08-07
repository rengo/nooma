package consolidation

import "github.com/rengo/nooma/internal/core/unit"

// Reason is the machine-readable justification a Transition carries (doc 02
// §6.1/§6.2, spec R1.4). It is a defined string type, not free text: a
// caller — m2c's decision_log writer, once I12 has one — matches on the
// closed vocabulary below rather than pattern-matching a sentence.
type Reason string

// The three Reason vocabulary members this PR's two producers,
// ExpireIncomplete and Archive, can emit (spec R1.4). Design.md §5.1
// declares no more than these three for feat/core-consolidation-expire-archive;
// a later PR that needs a new Reason adds it here, in the file that already
// owns the vocabulary.
const (
	ReasonIncompletePromoted   Reason = "incomplete_promoted"
	ReasonIncompleteExpired    Reason = "incomplete_expired"
	ReasonBelowWeightThreshold Reason = "below_weight_threshold"
)

// AllReasons returns a fresh slice holding the three Reason vocabulary
// members — unit.AllStatuses's own house pattern (spec R1.4): a function,
// not an exported var, so a caller mutating one call's result cannot affect
// another.
func AllReasons() []Reason {
	return nil
}

// Transition is the sole payload a phase's producer emits for a planned
// unit status change (doc 02 §6, spec R1.4). Every Transition any producer
// in this package emits is a (From, To) pair unit.ValidateTransition
// accepts without error — this PR's own exhaustiveness table
// (TestEveryEmittedTransitionIsLegal) drives ExpireIncomplete's and
// Archive's outputs through it rather than re-asserting the legal pairs by
// hand a second time.
type Transition struct {
	UnitID string
	From   unit.Status
	To     unit.Status
	Reason Reason
}
