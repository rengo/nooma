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

// nudgeSignalContext names what the check-in was about, its unit or, for a
// goal check-in, its goal's belief (ADR-0054), so a reader such as goal
// cadence learning need not join the trigger to know.
type nudgeSignalContext struct {
	UnitID   *string `json:"unit_id,omitempty"`
	BeliefID string  `json:"belief_id,omitempty"`
}

// recordNudgeSignal emits the learning signal of one check-in verdict (I47):
// typ about the trigger triggerID. It runs after the verdict's effect landed
// and after its decision_log row: a signal for an answer that did not close
// anything would be evidence for an event that did not happen, and one that
// fails before the row would leave a closed check-in with no row (I12) that
// no retry can write, since nothing is open any more.
//
// A nil signals repo records nothing: a test that wires none has no learner to
// teach. Production wiring always passes one (cmd/nooma).
func recordNudgeSignal(ctx context.Context, signals ports.SignalRepo, ids ports.IDGen, typ ports.SignalType, triggerID string, unitID *string, beliefID string, now time.Time) error {
	if signals == nil {
		return nil
	}
	valence, ok := nudgeValence(typ)
	if !ok {
		return fmt.Errorf("signal: %q is not a check-in verdict", typ)
	}
	contextJSON, err := json.Marshal(nudgeSignalContext{UnitID: unitID, BeliefID: beliefID})
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
