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
// already recorded for this exact problem (types.go:210-217): with a plain
// float64, "weight": null and a missing "weight" key both decode to 0.0,
// indistinguishable from a case that genuinely means zero.
//
// A pointer field is necessary and not sufficient: the assigner has to
// unmarshal into a pointer too. json.Unmarshal accepts null for a
// non-pointer destination without error and leaves the zero value, so an
// assigner that reads into a local float64 and takes its address produces a
// non-nil pointer to 0.0 for an explicit null, defeating this field's own
// shape. assignInterruptLevel reads into a *float64 for exactly that reason.
// assignFloat — weight and decay_rate — still does not, so the defect this
// comment describes is live for those two fields; it is being fixed as its
// own work unit rather than inside m3a-prospection's PR 4.
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

	// InterruptLevel is prospection's own capture field
	// (docs/02-cognitive-core.md §7), not part of the six orthogonal
	// resolutions above: it carries no answer to a pending question, it
	// feeds internal/core/prospection's delivery split instead (design.md
	// §3.8, m3a-prospection). It degrades independently of every field
	// above, the same posture I14 already requires of everything else in
	// this struct.
	InterruptLevel *float64 // doc 02 §7, [0,1]

	// RecurrenceRule is prospection's other capture field — doc 02 §7's
	// closed vocabulary as classify decodes it, not opaque structured_data
	// (§5.1: "structured_data ... is opaque to the brain and stays
	// opaque"). It is classify's own vocabulary, never *prospection.Rule
	// (see RecurrenceRule's own doc comment below).
	RecurrenceRule *RecurrenceRule

	// Language is what language the message was written in — doc 02 §5
	// step 1, ADR-0022. It answers a question no other field here does:
	// every field above describes what the message MEANT, and this one
	// describes how to answer it.
	//
	// Nil is the ordinary case rather than a failure: the field is
	// optional on the wire, so a model that omits it costs nothing.
	// Language.Or() is what turns nil into something a renderer can
	// switch on.
	Language *Language

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
	// ReasonBadFormat — the JSON type was right and the value is not one
	// this field reads: a date matching neither RFC3339 nor 2006-01-02, or
	// a number outside the range its field accepts (interrupt_level's
	// [0,1]). It is deliberately not split per shape — §9's learning loop
	// has no separate use for "wrong shape of string" versus "wrong range
	// of number" today, and a Reason with no consumer is a vocabulary
	// addition smuggled in (m3a-prospection owner-review R3).
	ReasonBadFormat Reason = "bad_format"
)

// Degradation is one lost field and the reason it was lost.
type Degradation struct {
	Field  string
	Reason Reason
}

// RecurrenceRule is doc 02 §7's recurrence vocabulary as classify decodes
// it. It is declared here, beside the field that carries it, rather than in
// outcomes.go — it is a capture field, not one of that file's six orthogonal
// resolutions — in exactly the shape outcomes.go uses for each of those six:
// a ~string type, its members, and an AllX() the decoder matches against.
// There is deliberately no ParseX; decodeEnum serves all of them (design
// D11 point 2).
//
// It is classify's own type, never *prospection.Rule: internal/core/
// prospection imports internal/core/classify (design.md §4), so the
// reverse — a classify field typed from prospection — would be the import
// cycle Go refuses to compile (m3a-prospection Finding F3). PR 7's
// prospection.Arm converts one vocabulary into the other at its own call
// site, on the legal side of that edge; nothing is lost across it, since a
// recurrence_rule classify could not decode is already nil and already
// means "no recurrence was claimed".
type RecurrenceRule string

const (
	RecurrenceRuleYearly  RecurrenceRule = "yearly"
	RecurrenceRuleMonthly RecurrenceRule = "monthly"
	RecurrenceRuleDaily   RecurrenceRule = "daily"
	RecurrenceRuleWeekly  RecurrenceRule = "weekly"
)

// AllRecurrenceRules returns a fresh slice of the RecurrenceRule vocabulary,
// in doc 02's declared order — the closed set decodeEnum matches against.
func AllRecurrenceRules() []RecurrenceRule {
	return []RecurrenceRule{RecurrenceRuleYearly, RecurrenceRuleMonthly, RecurrenceRuleDaily, RecurrenceRuleWeekly}
}
