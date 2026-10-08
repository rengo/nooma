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
// Saving a derived or seed belief unchanged CLAIMS it: the same writes run
// with the stored text as the new text. Saving a user_stated belief
// unchanged writes nothing.
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
	// An unchanged submit is a no-op only on a belief the user already owns.
	// On any other origin it is a claim (owner ruling 2026-10-08): the user
	// read the text and saved it, which is how a derived belief becomes
	// user_stated and stops being rewritten by derive. The claim writes the
	// STORED text back, not the normalised one, so it stays byte for byte.
	claim := normalised == selfmodel.NormalizeText(current.Content)
	if claim && current.Origin == selfmodel.OriginUserStated {
		return nil
	}
	to := normalised
	if claim {
		to = current.Content
	}

	now := s.clock.Now()
	decisionID, err := s.recordEditPreImage(ctx, current, to, claim, now)
	if err != nil {
		return err
	}
	if err := s.beliefs.EditContent(ctx, id, current.Content, to, now); err != nil {
		return fmt.Errorf("belief edit: write content of belief %q: %w", id, err)
	}
	// A claim emits a belief_edit signal too, but POSITIVE (owner ruling
	// 2026-10-08): the user read the derived text and kept it, which says the
	// system derived it right. A real edit stays negative.
	valence := ports.ValenceNegative
	if claim {
		valence = ports.ValencePositive
	}
	if err := s.recordBeliefSignal(ctx, ports.SignalBeliefEdit, valence, current, decisionID, now); err != nil {
		return &WriteLandedError{Signal: true, Err: err}
	}
	return nil
}

// recordEditPreImage writes the belief.edited row: the field(s) about to
// change with their previous and next values, keyed by column name (the
// correction pre-image's shape). It returns the row's id for the signal to
// link.
//
// A claim (an unchanged submit on a belief the user did not own) is the same
// row with "claimed": true and next.content equal to previous.content; the
// field is absent on a real edit.
func (s *BeliefsService) recordEditPreImage(ctx context.Context, current ports.Belief, content string, claim bool, now time.Time) (string, error) {
	type value struct {
		Content string           `json:"content"`
		Origin  selfmodel.Origin `json:"origin"`
	}
	contextJSON, err := json.Marshal(struct {
		BeliefID string   `json:"belief_id"`
		TopicKey string   `json:"topic_key"`
		Claimed  bool     `json:"claimed,omitempty"`
		Fields   []string `json:"fields"`
		Previous value    `json:"previous"`
		Next     value    `json:"next"`
	}{
		BeliefID: current.ID,
		TopicKey: current.TopicKey,
		Claimed:  claim,
		Fields:   []string{"content", "origin"},
		Previous: value{Content: current.Content, Origin: current.Origin},
		Next:     value{Content: content, Origin: selfmodel.OriginUserStated},
	})
	if err != nil {
		return "", fmt.Errorf("belief edit: encode pre-image context: %w", err)
	}

	rationale := fmt.Sprintf("belief edit about to replace the content of belief %q and mark it user_stated; previous values recorded before the edit", current.ID)
	if claim {
		rationale = fmt.Sprintf("belief claim about to mark belief %q user_stated with its content unchanged; previous values recorded before the write", current.ID)
	}
	d := ports.Decision{
		ID:         s.ids.New(),
		Action:     ports.ActionBeliefEdited,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := s.log.Record(ctx, d); err != nil {
		return "", fmt.Errorf("belief edit: record pre-image for belief %q: %w", current.ID, err)
	}
	return d.ID, nil
}
