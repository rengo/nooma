package classify

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrNoFieldsSalvaged reports that the payload yielded no completed members
// at all — a non-object, an empty string, or a document cut before its first
// value. It is design D1's stated floor: a classification with no fields has
// none to degrade, so this is an error rather than a Classification whose
// every field is absent. It is the only error Decode returns.
var ErrNoFieldsSalvaged = errors.New("classify: no fields salvaged from the response")

// Classification is a decoded capture response — docs/02-cognitive-core.md §5
// step 1. Every field is optional at the type level, because I14 requires a
// malformed field to degrade to null while the rest of the classification
// survives (02:124-125), and Degradations carries what was lost.
//
// Pointers rather than zero values, for the reason goldenset.ClassifyExpected
// already recorded for this exact problem (types.go:152-165): with a plain
// float64, "weight": null and a missing "weight" key both decode to 0.0,
// indistinguishable from a case that genuinely means zero.
type Classification struct {
	Kind              *Kind // the thirteen-member taxonomy — doc 02 §5
	NormalizedContent *string
	StructuredData    json.RawMessage
	Weight            *float64
	DecayRate         *float64
	EventAt           *time.Time
	DueAt             *time.Time

	// The six orthogonal resolution fields — doc 02 §5 at 02:120-123. Each
	// rides alongside any Kind and degrades independently of the others.
	NudgeOutcome       *NudgeOutcome
	RelationOutcome    *RelationOutcome
	StateOutcome       *StateOutcome
	TaskCheckinOutcome *TaskCheckinOutcome
	ListOp             *ListOp
	PersonRefStatus    *PersonRefStatus

	// Degradations records what was lost and why, in fieldSpecs' order. It
	// exists because I12 requires internal/brain to write a rationale into
	// decision_log: a decoder that discarded *why* a field vanished would
	// force the orchestrator to guess.
	Degradations []Degradation
}

// Reason names why a field did not survive decoding. The five values are
// distinct because brain writes a different rationale for each (I12): "the
// model did not say" and "the stream was cut" are not the same event, and
// neither is "the value was the wrong JSON type" versus "the value was the
// right type but outside its vocabulary".
type Reason string

const (
	// ReasonAbsent — the payload closed cleanly and a required field was
	// simply not there.
	ReasonAbsent Reason = "absent"
	// ReasonWrongType — the field held a JSON value of the wrong Go-side
	// type, e.g. weight recorded as a string.
	ReasonWrongType Reason = "wrong_type"
	// ReasonUnknownEnum — the JSON type was right, the value was outside
	// the field's closed vocabulary.
	ReasonUnknownEnum Reason = "unknown_enum"
	// ReasonTruncated — the stream ended before a required field arrived.
	ReasonTruncated Reason = "truncated"
	// ReasonBadFormat — a string field that parses, e.g. a date matching
	// neither RFC3339 nor 2006-01-02.
	ReasonBadFormat Reason = "bad_format"
)

// Degradation is one lost field and the reason it was lost.
type Degradation struct {
	Field  string
	Reason Reason
}
