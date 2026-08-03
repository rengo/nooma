package ports

import (
	"context"
	"encoding/json"
	"time"
)

// SignalType names one member of the learning_signals.signal_type
// vocabulary — migration 0002:10's own DDL comment enumerates all eleven,
// and design D6 declares the whole vocabulary in this port even though M1
// produces only one member. A closed vocabulary is what makes an
// out-of-vocabulary value detectable at all (doc 02 §5.1's own argument,
// applied to the write side): ten members with no M1 producer is a
// vocabulary, not ten unbuilt features.
type SignalType string

// The eleven members of the SignalType vocabulary — migration 0002:10.
// Order matches that DDL comment's own enumeration.
const (
	SignalCorrection      SignalType = "correction"
	SignalNudgeAck        SignalType = "nudge_ack"
	SignalNudgeIgnored    SignalType = "nudge_ignored"
	SignalNudgeEngaged    SignalType = "nudge_engaged"
	SignalNudgeDeclined   SignalType = "nudge_declined"
	SignalBeliefDelete    SignalType = "belief_delete"
	SignalBeliefEdit      SignalType = "belief_edit"
	SignalRelationReject  SignalType = "relation_reject"
	SignalRelationConfirm SignalType = "relation_confirm"
	SignalStateConfirmed  SignalType = "state_confirmed"
	SignalStateDenied     SignalType = "state_denied"
)

// AllSignalTypes returns a fresh slice holding the eleven SignalType
// vocabulary members, in the order the constants above declare them.
//
// A function, not an exported var (design D1's reasoning, the same rule
// AllDecisionActions and unit.AllStatuses already follow): an exported
// slice is mutable by any importer, and a mutated result could defeat a
// completeness check run from outside this package.
func AllSignalTypes() []SignalType {
	return []SignalType{
		SignalCorrection, SignalNudgeAck, SignalNudgeIgnored, SignalNudgeEngaged,
		SignalNudgeDeclined, SignalBeliefDelete, SignalBeliefEdit, SignalRelationReject,
		SignalRelationConfirm, SignalStateConfirmed, SignalStateDenied,
	}
}

// Valence is a learning_signals row's polarity — migration 0002:11.
type Valence string

const (
	ValencePositive Valence = "positive"
	ValenceNegative Valence = "negative"
	ValenceNeutral  Valence = "neutral"
)

// AllValences returns a fresh slice holding the three Valence vocabulary
// members, in the order the constants above declare them.
func AllValences() []Valence {
	return []Valence{ValencePositive, ValenceNegative, ValenceNeutral}
}

// TargetKind names what kind of thing a Signal's TargetID refers to —
// migration 0002:12.
type TargetKind string

const (
	TargetKindUnit     TargetKind = "unit"
	TargetKindTrigger  TargetKind = "trigger"
	TargetKindBelief   TargetKind = "belief"
	TargetKindRelation TargetKind = "relation"
)

// Signal is one learning_signals row (migration 0002:8-19,
// docs/03-data-model.md's "Learning" section) — design D6.
//
// TargetID carries deliberately no foreign key (I13): a signal outlives
// whatever it points at, including a target that is later deleted — or,
// as design D6's own L3 case proves, one that never existed in the first
// place. A learning signal records what the system was told, not a live
// join against current state.
type Signal struct {
	ID      string
	Type    SignalType
	Valence Valence

	// TargetKind and TargetID are both optional: not every SignalType
	// names a target, and migration 0002:12-13 declares both columns
	// nullable.
	TargetKind *TargetKind
	TargetID   *string // NO FK — I13

	// DecisionAction names the decision_log bucket this signal is
	// evidence against, when the caller can identify one. Left nil
	// rather than guessed when it cannot — design D6's own reasoning for
	// the correction signal: the bucket a correction is evidence against
	// is whichever decision produced the corrected value, and
	// decision_log carries no unit index to find it by.
	DecisionAction *DecisionAction

	// RelationType is set only for relation_reject/relation_confirm
	// signals — migration 0002:15.
	RelationType *string

	// Magnitude is left nil when no document defines a scale for this
	// signal's strength (design D6): an unused column left null for its
	// real consumer, not filled with an invented semantics.
	Magnitude *float64

	Context    json.RawMessage
	OccurredAt time.Time
}

// SignalRepo is the repository port over learning_signals — design D6,
// spec R1.10.
//
// Two methods, following ports.DecisionLog's own Record/Since shape: a
// learning signal is an append-only audit fact, not a mutable row.
//
// No Delete*-prefixed method: CLAUDE.md non-negotiable #6 ("nothing is
// deleted in the vault") applies to a learning signal the same as it does
// to a unit or a decision.
type SignalRepo interface {
	// Record persists s.
	Record(ctx context.Context, s Signal) error

	// Since returns every signal recorded strictly after t — occurred_at
	// > t, never >=, the same read-forward-from-a-cursor contract
	// DecisionLog.Since documents. Results are ordered by OccurredAt
	// ascending, tie-broken by ID. limit bounds how many rows come back.
	Since(ctx context.Context, t time.Time, limit int) ([]Signal, error)
}
