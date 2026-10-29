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
// (ADR-0050), or answers the pending question Today shows (I09), through the
// chat answer's own relationCurator (doc 02 §4, I10). It adds what a hand-made act needs and an answered question does
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
	return s.curate(ctx, relationID, curationVia{words: viaGraph}, s.confirm)
}

// ConfirmQuestion answers yes to the pending question questionID, the one
// Today shows (doc 02 §4, I09): the relation is resolved from the question
// store by that id, never guessed, and confirmed as Confirm confirms it, the
// question closing with it. A question the store does not hold open is
// ErrQuestionNotOpen and nothing is written.
func (s *CurationService) ConfirmQuestion(ctx context.Context, questionID string) error {
	return s.answer(ctx, questionID, s.confirm)
}

// RejectQuestion answers no to questionID: Reject's delete, resolved as
// ConfirmQuestion resolves it.
func (s *CurationService) RejectQuestion(ctx context.Context, questionID string) error {
	return s.answer(ctx, questionID, s.reject)
}

// ErrQuestionNotOpen is an answer to a question the store does not hold open:
// answered or expired already, or never asked about.
var ErrQuestionNotOpen = errors.New("the question is no longer open")

// curationVia is where a curation came from, as its row tells it.
type curationVia struct {
	words      string
	questionID string
}

// viaGraph is how a graph curation's row names where it came from.
const viaGraph = "from the graph"

// answer finds questionID among the questions waiting, queued or asked, and
// curates the relation it names.
func (s *CurationService) answer(ctx context.Context, questionID string, act curationAct) error {
	unasked, err := s.questions.Unasked(ctx)
	if err != nil {
		return fmt.Errorf("curation: read queued questions: %w", err)
	}
	open, err := s.questions.Open(ctx)
	if err != nil {
		return fmt.Errorf("curation: read open questions: %w", err)
	}
	for _, q := range append(unasked, open...) {
		if q.ID == questionID {
			return s.curate(ctx, q.RelationID, curationVia{words: fmt.Sprintf("answering question %q on Today", q.ID), questionID: q.ID}, act)
		}
	}
	return fmt.Errorf("curation: question %q: %w", questionID, ErrQuestionNotOpen)
}

// curationAct is Confirm's or Reject's effect on one relation.
type curationAct func(ctx context.Context, rel ports.Relation, queued []string, now time.Time, via curationVia) error

func (s *CurationService) confirm(ctx context.Context, before ports.Relation, queued []string, now time.Time, via curationVia) error {
	after, err := s.curator.ConfirmRelation(ctx, before.ID, now)
	if err != nil {
		return err
	}
	recordErr := s.record(ctx, ports.ActionRelationConfirmed, before, now, via,
		fmt.Sprintf("relation %q confirmed by the user %s; confidence raised to the type's surface threshold", before.ID, via.words),
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
	return s.curate(ctx, relationID, curationVia{words: viaGraph}, s.reject)
}

func (s *CurationService) reject(ctx context.Context, rel ports.Relation, queued []string, now time.Time, via curationVia) error {
	if err := s.record(ctx, ports.ActionRelationRejected, rel, now, via,
		fmt.Sprintf("relation %q about to be deleted by the user %s; the row as it was is recorded before the delete", rel.ID, via.words),
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
func (s *CurationService) curate(ctx context.Context, relationID string, via curationVia, act curationAct) error {
	rel, err := s.rels.ByID(ctx, relationID)
	if err != nil {
		return fmt.Errorf("curation: read relation %q: %w", relationID, err)
	}
	// A dormant relation is not drawn, so it is not curated either (I49).
	if dormant, err := newBars(s.rels).dormant(ctx, rel); err != nil {
		return fmt.Errorf("curation: %w", err)
	} else if dormant {
		return fmt.Errorf("curation: relation %q is dormant: %w", relationID, ports.ErrRelationNotFound)
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
	return act(ctx, rel, queued, s.clock.Now(), via)
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
// page renders; from_unit_id names its subject there, and question_id the
// question it answered, when it answered one.
func (s *CurationService) record(ctx context.Context, action ports.DecisionAction, rel ports.Relation, now time.Time, via curationVia, rationale string, previous, next map[string]any) error {
	contextJSON, err := json.Marshal(struct {
		RelationID string         `json:"relation_id"`
		FromUnitID string         `json:"from_unit_id"`
		ToUnitID   string         `json:"to_unit_id"`
		Type       string         `json:"type"`
		QuestionID string         `json:"question_id,omitempty"`
		Previous   map[string]any `json:"previous"`
		Next       map[string]any `json:"next"`
	}{rel.ID, rel.FromUnitID, rel.ToUnitID, rel.Type, via.questionID, previous, next})
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
