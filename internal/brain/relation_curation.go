package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/ports"
)

// CurationService confirms or deletes one relation from the graph view
// (ADR-0050) through the chat answer's own relationCurator (doc 02 §4,
// I10). It adds what a hand-made act needs and an answered question does
// not: both ends must be live, its own decision_log row, and closing any
// question still waiting about the relation.
type CurationService struct {
	clock     ports.Clock
	units     ports.UnitRepo
	rels      ports.RelationRepo
	log       ports.DecisionLog
	questions ports.PendingQuestionRepo
	curator   relationCurator
}

// NewCurationService wires a CurationService.
func NewCurationService(clock ports.Clock, ids ports.IDGen, units ports.UnitRepo, rels ports.RelationRepo, signals ports.SignalRepo, log ports.DecisionLog, questions ports.PendingQuestionRepo) *CurationService {
	return &CurationService{
		clock: clock, units: units, rels: rels, log: log, questions: questions,
		curator: relationCurator{rels: rels, signals: signals, ids: ids},
	}
}

// Confirm raises relationID and emits its signal, as the chat does, then
// writes relation.confirmed with the confidence before and after, as
// config.updated does: nothing is lost that the pair cannot tell. Once the
// raise landed, a missing row or an unclosed question is a
// *WriteLandedError. A relation the graph does not draw (absent, or an end
// not live, I02) is ports.ErrRelationNotFound and nothing is written.
func (s *CurationService) Confirm(ctx context.Context, relationID string) error {
	return s.curate(ctx, relationID, s.confirm)
}

func (s *CurationService) confirm(ctx context.Context, before ports.Relation, queued []string, now time.Time) error {
	after, err := s.curator.ConfirmRelation(ctx, before.ID, now)
	if err != nil {
		return err
	}
	recordErr := s.record(ctx, ports.ActionRelationConfirmed, before, now,
		fmt.Sprintf("relation %q confirmed by the user from the graph; confidence raised to the type's surface threshold", before.ID),
		map[string]any{"confidence": before.Confidence}, map[string]any{"confidence": after.Confidence})
	closeErr := s.close(ctx, queued, now, s.questions.Confirm)
	if recordErr == nil && closeErr == nil {
		return nil
	}
	return &WriteLandedError{Record: recordErr != nil, Err: errors.Join(recordErr, closeErr)}
}

// Reject writes relation.rejected FIRST, carrying the whole row with next
// as nulls: once deleted, nothing else remembers the relation, so a row
// that cannot be written stops the act (ADR-0016's order). Then the chat's
// reject, signal before delete (I10), then the waiting question closes, as
// the chat closes it after the delete. Confirm's refusal applies.
func (s *CurationService) Reject(ctx context.Context, relationID string) error {
	return s.curate(ctx, relationID, s.reject)
}

func (s *CurationService) reject(ctx context.Context, rel ports.Relation, queued []string, now time.Time) error {
	if err := s.record(ctx, ports.ActionRelationRejected, rel, now,
		fmt.Sprintf("relation %q about to be deleted by the user from the graph; the row as it was is recorded before the delete", rel.ID),
		map[string]any{"type": rel.Type, "strength": rel.Strength, "confidence": rel.Confidence, "created_by": rel.CreatedBy},
		map[string]any{"type": nil, "strength": nil, "confidence": nil, "created_by": nil}); err != nil {
		return err
	}
	if err := s.curator.RejectRelation(ctx, rel.ID, now); err != nil {
		return err
	}
	if err := s.close(ctx, queued, now, s.questions.Reject); err != nil {
		return &WriteLandedError{Err: err}
	}
	return nil
}

// curate is both acts' start: the relation, refused unless both ends are
// live, the questions waiting about it (read now, since a deleted
// relation's questions are no longer visible), and the act's one instant.
func (s *CurationService) curate(ctx context.Context, relationID string, act func(context.Context, ports.Relation, []string, time.Time) error) error {
	rel, err := s.rels.ByID(ctx, relationID)
	if err != nil {
		return fmt.Errorf("curation: read relation %q: %w", relationID, err)
	}
	live, err := s.units.LiveByIDs(ctx, []string{rel.FromUnitID, rel.ToUnitID})
	if err != nil {
		return fmt.Errorf("curation: read the ends of relation %q: %w", relationID, err)
	}
	if len(live) != 2 {
		return fmt.Errorf("curation: relation %q has an end that is not live: %w", relationID, ports.ErrRelationNotFound)
	}
	unasked, err := s.questions.Unasked(ctx)
	if err != nil {
		return fmt.Errorf("curation: read queued questions: %w", err)
	}
	open, err := s.questions.Open(ctx)
	if err != nil {
		return fmt.Errorf("curation: read open questions: %w", err)
	}
	var queued []string
	for _, q := range append(unasked, open...) {
		if q.RelationID == relationID {
			queued = append(queued, q.ID)
		}
	}
	return act(ctx, rel, queued, s.clock.Now())
}

// close resolves each question with resolve, the user having settled it.
func (s *CurationService) close(ctx context.Context, ids []string, now time.Time, resolve func(context.Context, string, time.Time) error) error {
	var errs []error
	for _, id := range ids {
		if err := resolve(ctx, id, now); err != nil {
			errs = append(errs, fmt.Errorf("curation: close question %q: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// record writes one curation row in the previous/next shape the activity
// page renders; from_unit_id names its subject there.
func (s *CurationService) record(ctx context.Context, action ports.DecisionAction, rel ports.Relation, now time.Time, rationale string, previous, next map[string]any) error {
	contextJSON, err := json.Marshal(struct {
		RelationID string         `json:"relation_id"`
		FromUnitID string         `json:"from_unit_id"`
		ToUnitID   string         `json:"to_unit_id"`
		Type       string         `json:"type"`
		Previous   map[string]any `json:"previous"`
		Next       map[string]any `json:"next"`
	}{rel.ID, rel.FromUnitID, rel.ToUnitID, rel.Type, previous, next})
	if err != nil {
		return fmt.Errorf("curation: encode context: %w", err)
	}
	if err := s.log.Record(ctx, ports.Decision{
		ID: s.curator.ids.New(), Action: action, Rationale: rationale, Context: contextJSON, OccurredAt: now,
	}); err != nil {
		return fmt.Errorf("curation: record %s for relation %q: %w", action, rel.ID, err)
	}
	return nil
}
