package ports

import (
	"context"
	"errors"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
)

// TriggerStatus, TriggerKind and the TriggerRepo shape below are the red
// step's minimal declarations (PR 1, commit 1): enough for the contract
// suite to compile, nothing more. The vocabularies answer with no members
// and the fake answers with zero values, so every substantive assertion in
// test/support/repocontract/triggerrepo.go fails for its own reason.
type TriggerStatus string

// The triggers.status vocabulary (migration 0001:46).
const (
	TriggerStatusArmed     TriggerStatus = "armed"
	TriggerStatusFired     TriggerStatus = "fired"
	TriggerStatusDismissed TriggerStatus = "dismissed"
	TriggerStatusExpired   TriggerStatus = "expired"
)

// TriggerKind is triggers.kind's vocabulary.
type TriggerKind string

// The triggers.kind vocabulary (migration 0001:45).
const (
	TriggerKindTimeBased    TriggerKind = "time_based"
	TriggerKindEventBased   TriggerKind = "event_based"
	TriggerKindPatternBased TriggerKind = "pattern_based"
)

// AllTriggerStatuses is unimplemented in the red step.
func AllTriggerStatuses() []TriggerStatus { return nil }

// AllTriggerKinds is unimplemented in the red step.
func AllTriggerKinds() []TriggerKind { return nil }

// TriggerPayload is triggers.payload's declared shape.
type TriggerPayload struct {
	ActionText string
	Rationale  string
	LeadDays   int
}

// Trigger is the write shape.
type Trigger struct {
	ID               string
	UnitID           *string
	Kind             TriggerKind
	InterruptLevel   *float64
	Payload          TriggerPayload
	FireAt           *time.Time
	RecurrenceRule   *prospection.Rule
	RecurrenceAnchor *prospection.Anchor
	CreatedAt        time.Time
}

// DueTrigger is the read shape.
type DueTrigger struct {
	ID               string
	UnitID           *string
	FireAt           time.Time
	InterruptLevel   *float64
	RecurrenceRule   *prospection.Rule
	RecurrenceAnchor *prospection.Anchor
}

// TriggerRepo is the repository port over triggers.
type TriggerRepo interface {
	Create(ctx context.Context, t Trigger) error
	Due(ctx context.Context, at time.Time) ([]DueTrigger, error)
	Fire(ctx context.Context, id string, at time.Time) error
	Expire(ctx context.Context, id string) error
}

// Sentinel errors ports.TriggerRepo implementations return.
var (
	ErrTriggerNotFound       = errors.New("trigger not found")
	ErrTriggerExists         = errors.New("trigger already exists")
	ErrTriggerStatusConflict = errors.New("trigger is not in the expected status")
)
