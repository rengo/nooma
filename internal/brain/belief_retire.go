package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
)

// Retire moves the active belief id to retired, records the transition and
// emits a belief_delete signal (m4e design §3.4). No row is removed: a
// retired belief keeps its content and is only excluded from every read of
// active beliefs.
//
// Unlike Edit it writes first and records after. The pre-image of a retire
// is trivial (active, and the row untouched), so record-first would buy
// nothing and would cost a false row under a race: the read passes,
// SetStatus loses, and the log asserts a retirement that did not happen. A
// missing row is an absence; a false one is a lie.
//
// A conflict or a missing id is decided before any write. Once SetStatus
// has landed the user's act is done, so a failed row or signal is not a
// failure: Retire returns a *WriteLandedError naming what is missing, and
// the signal is still attempted when only the row failed (without a
// decision_id), because the learning pass should hear the act.
func (s *BeliefsService) Retire(ctx context.Context, id string) error {
	current, err := s.beliefs.BeliefByID(ctx, id)
	if err != nil {
		return fmt.Errorf("belief retire: read belief %q: %w", id, err)
	}
	if current.Status != selfmodel.StatusActive {
		return fmt.Errorf("belief retire: belief %q is %s: %w", id, current.Status, ports.ErrBeliefStatusConflict)
	}

	now := s.clock.Now()
	if err := s.beliefs.SetStatus(ctx, id, selfmodel.StatusActive, selfmodel.StatusRetired, now); err != nil {
		return fmt.Errorf("belief retire: set status of belief %q: %w", id, err)
	}

	decisionID, recordErr := s.recordRetired(ctx, current, now)
	signalErr := s.recordBeliefSignal(ctx, ports.SignalBeliefDelete, ports.ValenceNegative, current, decisionID, now)
	if recordErr == nil && signalErr == nil {
		return nil
	}
	return &WriteLandedError{Record: recordErr != nil, Signal: signalErr != nil, Err: errors.Join(recordErr, signalErr)}
}

// recordRetired writes the belief.retired row and returns its id, or ""
// when it could not be written.
func (s *BeliefsService) recordRetired(ctx context.Context, current ports.Belief, now time.Time) (string, error) {
	contextJSON, err := json.Marshal(struct {
		BeliefID string           `json:"belief_id"`
		TopicKey string           `json:"topic_key"`
		Content  string           `json:"content"`
		From     selfmodel.Status `json:"from"`
		To       selfmodel.Status `json:"to"`
	}{
		BeliefID: current.ID,
		TopicKey: current.TopicKey,
		Content:  current.Content,
		From:     selfmodel.StatusActive,
		To:       selfmodel.StatusRetired,
	})
	if err != nil {
		return "", fmt.Errorf("belief retire: encode context: %w", err)
	}

	d := ports.Decision{
		ID:         s.ids.New(),
		Action:     ports.ActionBeliefRetired,
		Rationale:  fmt.Sprintf("belief %q retired by the user (active -> retired); the row and its content are kept", current.ID),
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := s.log.Record(ctx, d); err != nil {
		return "", fmt.Errorf("belief retire: record retirement of belief %q: %w", current.ID, err)
	}
	return d.ID, nil
}
