package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/ports"
)

// Edit replaces the content of the active belief id with content, marks
// the belief user_stated, records the change and emits a belief_edit
// signal (m4e design §3.4). Content is the only thing it can change: there
// is no facet or confidence parameter to ignore.
//
// The submitted text is normalised and validated once, before any read; the
// normalised value is what is compared, stored and logged. The comparison
// target is the stored text normalised WITHOUT validation, so a derived
// belief that is over the bound, or blank-looking, can still be edited and a
// browser's CRLF resubmission of an unchanged textarea is a no-op.
//
// An edit overwrites text a belief has no history table for, so ADR-0016's
// order applies by analogy: the pre-image row first (a failed record means
// no write), then the compare-and-swap, then the signal. A CAS that loses to
// a concurrent retire leaves a pre-image row describing an edit that did not
// land; that is the accepted ADR-0016 window, and it emits no signal. When
// the write landed and only the signal failed, the error is a
// *WriteLandedError.
func (s *BeliefsService) Edit(ctx context.Context, id, content string) error {
	normalised, err := selfmodel.NormalizeContent(content)
	if err != nil {
		return fmt.Errorf("belief edit: %w", err)
	}
	current, err := s.beliefs.BeliefByID(ctx, id)
	if err != nil {
		return fmt.Errorf("belief edit: read belief %q: %w", id, err)
	}
	if current.Status != selfmodel.StatusActive {
		return fmt.Errorf("belief edit: belief %q is %s: %w", id, current.Status, ports.ErrBeliefStatusConflict)
	}
	if normalised == selfmodel.NormalizeText(current.Content) {
		return nil
	}

	now := s.clock.Now()
	decisionID, err := s.recordEditPreImage(ctx, current, normalised, now)
	if err != nil {
		return err
	}
	if err := s.beliefs.EditContent(ctx, id, current.Content, normalised, now); err != nil {
		return fmt.Errorf("belief edit: write content of belief %q: %w", id, err)
	}
	if err := s.recordBeliefSignal(ctx, ports.SignalBeliefEdit, current, decisionID, now); err != nil {
		return &WriteLandedError{Signal: true, Err: err}
	}
	return nil
}

// recordEditPreImage writes the belief.edited row: the field(s) about to
// change with their previous and next values, keyed by column name (the
// correction pre-image's shape). It returns the row's id for the signal to
// link.
func (s *BeliefsService) recordEditPreImage(ctx context.Context, current ports.Belief, content string, now time.Time) (string, error) {
	type value struct {
		Content string           `json:"content"`
		Origin  selfmodel.Origin `json:"origin"`
	}
	contextJSON, err := json.Marshal(struct {
		BeliefID string   `json:"belief_id"`
		TopicKey string   `json:"topic_key"`
		Fields   []string `json:"fields"`
		Previous value    `json:"previous"`
		Next     value    `json:"next"`
	}{
		BeliefID: current.ID,
		TopicKey: current.TopicKey,
		Fields:   []string{"content", "origin"},
		Previous: value{Content: current.Content, Origin: current.Origin},
		Next:     value{Content: content, Origin: selfmodel.OriginUserStated},
	})
	if err != nil {
		return "", fmt.Errorf("belief edit: encode pre-image context: %w", err)
	}

	d := ports.Decision{
		ID:         s.ids.New(),
		Action:     ports.ActionBeliefEdited,
		Rationale:  fmt.Sprintf("belief edit about to replace the content of belief %q and mark it user_stated; previous values recorded before the edit", current.ID),
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := s.log.Record(ctx, d); err != nil {
		return "", fmt.Errorf("belief edit: record pre-image for belief %q: %w", current.ID, err)
	}
	return d.ID, nil
}
