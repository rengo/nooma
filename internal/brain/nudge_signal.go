package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// nudgeValence is a check-in verdict's polarity (doc 02 §9). Engaging is the
// check-in landing; declining and leaving it unanswered are it missing; a
// snooze (nudge_ack) saw it and put it off, which says neither.
func nudgeValence(typ ports.SignalType) (ports.Valence, bool) {
	switch typ {
	case ports.SignalNudgeEngaged:
		return ports.ValencePositive, true
	case ports.SignalNudgeDeclined, ports.SignalNudgeIgnored:
		return ports.ValenceNegative, true
	case ports.SignalNudgeAck:
		return ports.ValenceNeutral, true
	}
	return "", false
}

// nudgeSignalContext names the check-in's unit, when it has one, so a reader
// need not join the trigger to know what it was about.
type nudgeSignalContext struct {
	UnitID *string `json:"unit_id,omitempty"`
}

// recordNudgeSignal emits the learning signal of one check-in verdict (I47):
// typ about the trigger triggerID. It runs after the verdict's effect landed,
// never before: a signal for an answer that did not close anything would be
// evidence the learner tunes on for an event that did not happen.
//
// A nil signals repo records nothing: a test that wires none has no learner to
// teach. Production wiring always passes one (cmd/nooma).
func recordNudgeSignal(ctx context.Context, signals ports.SignalRepo, ids ports.IDGen, typ ports.SignalType, triggerID string, unitID *string, now time.Time) error {
	if signals == nil {
		return nil
	}
	valence, ok := nudgeValence(typ)
	if !ok {
		return fmt.Errorf("signal: %q is not a check-in verdict", typ)
	}
	contextJSON, err := json.Marshal(nudgeSignalContext{UnitID: unitID})
	if err != nil {
		return fmt.Errorf("signal: encode the %s context: %w", typ, err)
	}
	kind := ports.TargetKindTrigger
	if err := signals.Record(ctx, ports.Signal{
		ID: ids.New(), Type: typ, Valence: valence, TargetKind: &kind, TargetID: &triggerID,
		Context: contextJSON, OccurredAt: now,
	}); err != nil {
		return fmt.Errorf("signal: record %s for check-in %q: %w", typ, triggerID, err)
	}
	return nil
}
