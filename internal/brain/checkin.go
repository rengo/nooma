package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/relation"
	"github.com/rengo/nooma/internal/ports"
)

// resolveCheckIn closes the open check-in an inbound answer answers, if it
// answers one.
//
// It runs as a side effect of a capture rather than as a fork in it: an
// answer carries an OUTCOME, not a Kind, and the two are orthogonal. "Yes,
// done, and also remind me to call the dentist tomorrow" is one message
// that both resolves a nudge and arms a timer, and a fork would have to
// choose. So this resolves what it can and the pipeline continues.
//
// It returns whether anything was resolved, for the caller's own report.
//
// **Beside a chat, the item it names** (ADR-0044): what Nooma asked is a
// pushed turn of the chat shown (shown maps a letter to its unit), and the
// answer names that turn's item in referent. An answer naming no item, or one
// with no open check-in, answers none. A snooze naming an item puts off its
// check-in; one naming none, the newest. Read alone, the newest open one.
func (r captureRunner) resolveCheckIn(ctx context.Context, c classify.Classification, shown []string, now time.Time) (bool, error) {
	resolution, ok := checkInResolution(c)
	snoozed := classify.Snoozes(c)
	if !ok && !snoozed {
		return false, nil
	}

	open, err := r.triggers.Delivered(ctx)
	if err != nil {
		return false, fmt.Errorf("capture: reading open check-ins: %w", err)
	}
	// A goal check-in is answered only by naming it (ADR-0054).
	open = withoutGoalCheckIns(open)
	if snoozed {
		// A not-yet that names a shown item puts off that item's check-in, or
		// none when it has none (ADR-0044); one naming no item, the newest.
		if named := namedItem(c, shown); named != "" {
			open = checkInsOf(open, named)
		}
		if len(open) == 0 {
			return false, nil
		}
		// Nothing closes: the row is what lets the morning digest ask the
		// newest open check-in again (doc 02 §7).
		if err := r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInSnoozed,
			fmt.Sprintf("check-in %q was put off; the morning digest asks it again", open[0].ID), open[0].ID, "", len(open)); err != nil {
			return false, err
		}
		return false, recordNudgeSignal(ctx, r.signals, r.ids, ports.SignalNudgeAck, open[0].ID, open[0].UnitID, now)
	}
	if len(shown) > 0 {
		open = checkInsOf(open, namedItem(c, shown))
		if len(open) == 0 {
			return false, r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInUnmatched,
				fmt.Sprintf("an answer resolving to %q named no item of the chat with an open check-in", resolution), "", resolution, 0)
		}
	}
	if len(open) == 0 {
		// An answer with nothing open. Recorded and otherwise ignored: a
		// user saying "done" out of the blue is not an error, and it is
		// worth one row because a check-in that vanished between the
		// question and the answer is a thing an auditor would want to
		// see.
		return false, r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInUnmatched,
			fmt.Sprintf("an answer resolving to %q arrived with no open check-in", resolution), "", resolution, 0)
	}

	// The most recent, because Delivered orders that way and an answer
	// carries no id. Ambiguity is not resolved by guessing at meaning: if
	// several are open, the choice is recorded with how many there were,
	// so the audit trail shows a choice was made rather than implying
	// there was only one.
	target := open[0]
	if err := r.triggers.Resolve(ctx, target.ID, resolution, now); err != nil {
		if !errors.Is(err, ports.ErrTriggerStatusConflict) {
			return false, fmt.Errorf("capture: resolving check-in %q: %w", target.ID, err)
		}
		// The digest closed it under this answer (doc 02 §7): the answer now
		// matches nothing, and the capture goes on.
		return false, r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInUnmatched,
			fmt.Sprintf("an answer resolving to %q arrived as check-in %q closed unanswered", resolution, target.ID), target.ID, resolution, len(open))
	}

	// The row before the signal (I12): a signal that fails after the close
	// leaves a logged close the learner missed, never an unlogged one.
	if err := r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInResolved,
		fmt.Sprintf("check-in %q resolved as %q", target.ID, resolution), target.ID, resolution, len(open)); err != nil {
		return false, err
	}
	verdict := ports.SignalNudgeEngaged
	if resolution == ports.ResolutionDeclined {
		verdict = ports.SignalNudgeDeclined
	}
	return true, recordNudgeSignal(ctx, r.signals, r.ids, verdict, target.ID, target.UnitID, now)
}

// namedItem is the unit c's referent names among shown, or "" when it names
// none of them.
func namedItem(c classify.Classification, shown []string) string {
	if c.Referent == nil || c.Referent.Unclear || c.Referent.Position < 1 || c.Referent.Position > len(shown) {
		return ""
	}
	return shown[c.Referent.Position-1]
}

// checkInsOf is the open check-ins hanging off unitID, in their order; none
// for "".
func checkInsOf(open []ports.OpenCheckIn, unitID string) []ports.OpenCheckIn {
	var of []ports.OpenCheckIn
	for _, o := range open {
		if unitID != "" && o.UnitID != nil && *o.UnitID == unitID {
			of = append(of, o)
		}
	}
	return of
}

// checkInResolution is classify.ResolveCheckin in the store's vocabulary: the
// mapping itself, nudge over task and snooze resolving nothing, is core's, so
// the recorded-reading judge (internal/reading) sees the effect this has.
//
// self_healed is in the resolution vocabulary and in neither classify
// vocabulary, and that is also correct: nobody types it. It is the
// system's own verdict for a nudge that fresh activity made moot, and
// producing it is M4's.
func checkInResolution(c classify.Classification) (ports.TriggerResolution, bool) {
	switch resolution, ok := classify.ResolveCheckin(c); {
	case !ok:
		return "", false
	case resolution == classify.CheckinEngaged:
		return ports.ResolutionEngaged, true
	case resolution == classify.CheckinDeclined:
		return ports.ResolutionDeclined, true
	}
	return "", false
}

// resolveRelationCheckIn applies a relation_outcome to the relation
// question it answers — I10's path, and the one place in this codebase
// that deletes anything.
//
// **Which relation an answer is about is resolved from the STORE, never
// from the model** (owner ruling Q3). The digest asked exactly one
// question and marked it asked; this reads what is open and takes the most
// recently asked, exactly as resolveCheckIn does over Delivered. The
// classify prompt is never widened to carry open check-ins into the
// model's context — a model asked to pick an id can pick a plausible wrong
// one, and the wrong pick here is a deletion.
//
// It mirrors resolveCheckIn shape for shape, including the two orderings
// that look inconsistent and are not: RejectRelation emits its signal
// BEFORE deleting (I10 — the effect is irreversible, so the recoverable
// error is a signal for a relation that survived), and ConfirmRelation
// emits AFTER raising (the effect is idempotent, so the recoverable error
// is a raise with no signal). Both follow the same rule — never leave
// evidence for an effect that may not have happened — pointed at the
// direction each effect can fail in.
func (r captureRunner) resolveRelationCheckIn(ctx context.Context, c classify.Classification, now time.Time) error {
	if c.RelationOutcome == nil {
		// Including an answer the model worded outside the vocabulary:
		// classify.Decode degrades an unknown enum to null (I14), and a
		// near-miss coerced into "rejected" would be a guess with no undo.
		return nil
	}
	outcome := *c.RelationOutcome

	open, err := r.openRelationQuestions(ctx)
	if err != nil {
		return err
	}
	if len(open) == 0 {
		// Owner ruling Q5, and it covers the rejection too. An answer
		// that matches no open question gives disambiguation nothing to
		// work with, and deletion is the one irreversible act in this
		// vault — refusing to guess which relation to delete is the only
		// safe direction. Recorded, because a question that vanished
		// between the asking and the answer is a thing an auditor would
		// want to see.
		return r.recordRelationCheckIn(ctx, now, ports.ActionCaptureRelationCheckInUnmatched,
			fmt.Sprintf("a relation answer of %q arrived with no open relation question", outcome),
			"", "", outcome, 0)
	}

	// The most recent, because Open orders that way and an answer carries
	// no id. Ambiguity is not resolved by guessing at meaning: the choice
	// is recorded with how many there were, so the audit trail shows a
	// choice was made rather than implying there was only one.
	target := open[0]
	switch outcome {
	case classify.RelationOutcomeConfirmed:
		if _, err := r.curator().ConfirmRelation(ctx, target.RelationID, now); err != nil {
			return err
		}
		if err := r.questions.Confirm(ctx, target.ID, now); err != nil {
			return fmt.Errorf("capture: closing confirmed relation question %q: %w", target.ID, err)
		}
	case classify.RelationOutcomeRejected:
		if err := r.curator().RejectRelation(ctx, target.RelationID, now); err != nil {
			return err
		}
		// Only after the delete returned. Reversed, a reject that failed
		// halfway would have closed the one question still able to ask
		// again — and both of this port's reads inner-join relations, so
		// a question whose relation IS gone is invisible rather than
		// stuck (the digest's own sweep closes it as expired).
		if err := r.questions.Reject(ctx, target.ID, now); err != nil {
			return fmt.Errorf("capture: closing rejected relation question %q: %w", target.ID, err)
		}
	default:
		// Unreachable while AllRelationOutcomes has two members, and
		// given an answer rather than left to fall through into a silent
		// success that resolved nothing.
		return fmt.Errorf("capture: no resolution path for relation outcome %q", outcome)
	}

	return r.recordRelationCheckIn(ctx, now, ports.ActionCaptureRelationCheckInResolved,
		fmt.Sprintf("relation question %q about relation %q resolved as %q", target.ID, target.RelationID, outcome),
		target.ID, target.RelationID, outcome, len(open))
}

// openRelationQuestions is the disambiguation pool, or empty.
//
// A nil questions repo reads as "nothing open" rather than as a crash —
// checkRunner.questions' own nil-tolerance, for the same reason: a vault
// wired without a question store has nothing to disambiguate against, and
// that is a true statement about it rather than a failure of this capture.
func (r captureRunner) openRelationQuestions(ctx context.Context) ([]ports.RelationQuestion, error) {
	if r.questions == nil {
		return nil, nil
	}
	open, err := r.questions.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture: reading open relation questions: %w", err)
	}
	return open, nil
}

// relationCurator is the one implementation of confirming and rejecting a
// relation (doc 02 §4). The chat answer to a relation question
// (resolveRelationCheckIn) and the graph view (CurationService) both reach
// it, so the two surfaces cannot drift: same effect, same signal, same
// order. Each surface writes its own decision_log row, because each one's
// context differs (a question answered; a relation curated by hand).
type relationCurator struct {
	rels    ports.RelationRepo
	signals ports.SignalRepo
	ids     ports.IDGen
}

// curator is captureRunner's view of the shared curator.
func (r captureRunner) curator() relationCurator {
	return relationCurator{rels: r.rels, signals: r.signals, ids: r.ids}
}

// RejectRelation deletes one relation, emitting its signal first — I10.
//
// The ordering is the invariant, and a caller that gets it wrong should
// have to get it wrong HERE, in one reviewable place, rather than at each
// surface that rejects a relation.
//
// It takes the id and reads the row itself: the chat path holds only the
// id its question names, and the signal needs the row's type, maker and
// confidence (I47). A relation already gone fails the read, before any
// signal: there is no rejection to record.
func (r relationCurator) RejectRelation(ctx context.Context, relationID string, now time.Time) error {
	// Read first: once deleted, nothing else remembers the relation's type,
	// and a rejection the learner cannot count against its type teaches it
	// nothing (I47).
	rel, err := r.rels.ByID(ctx, relationID)
	if err != nil {
		return fmt.Errorf("capture: reading relation %q to reject: %w", relationID, err)
	}
	sig, err := relationSignal(r.ids.New(), ports.SignalRelationReject, ports.ValenceNegative, rel, rel.Confidence, now)
	if err != nil {
		return err
	}
	if err := r.signals.Record(ctx, sig); err != nil {
		return fmt.Errorf("capture: recording the relation rejection: %w", err)
	}

	// Only now. See this function's own doc comment.
	if err := r.rels.Delete(ctx, relationID); err != nil {
		return fmt.Errorf("capture: deleting rejected relation %q: %w", relationID, err)
	}
	return nil
}

// ConfirmRelation raises one relation's confidence out of the uncertain
// band and emits relation_confirm — doc 02 §4, and that signal's first
// call site anywhere in this tree.
//
// **The signal is emitted AFTER the raise, which is the OPPOSITE of I10's
// order, for the same underlying reason pointed the other way.** I10 emits
// first because its effect is a DELETE: irreversible, so the recoverable
// error (a signal for a relation that survived) is the one to prefer. Here
// the effect is idempotent — ConfirmedConfidence applied twice is
// ConfirmedConfidence applied once — so re-running the raise costs nothing,
// while a signal emitted for a raise that then failed is evidence the
// learning module would tune on forever. Same principle (never emit
// evidence for an effect that may not have happened), opposite direction,
// and the two are stated together so the next reader harmonises neither
// into the other.
//
// The floor is the relation type's OWN min_confidence_to_surface, read
// through ThresholdsFor — doc 02 §4's confirmed_floor is an alias for it,
// not a constant of its own (ADR-0027's Related decision).
//
// It returns the relation as written, so a caller can record the
// confidence it reached.
func (r relationCurator) ConfirmRelation(ctx context.Context, relationID string, now time.Time) (ports.Relation, error) {
	rel, err := r.rels.ByID(ctx, relationID)
	if err != nil {
		return ports.Relation{}, fmt.Errorf("capture: reading relation %q to confirm: %w", relationID, err)
	}

	row, err := r.rels.ThresholdsFor(ctx, rel.Type)
	if err != nil {
		return ports.Relation{}, fmt.Errorf("capture: thresholds for relation type %q: %w", rel.Type, err)
	}

	// Only Confidence is replaced. Strength, CreatedBy and CreatedAt
	// travel back on the row ByID returned — belt and braces beside
	// Upsert's own contract, which already refuses to rewrite them, and
	// I07's "revised in place" rather than re-created.
	judged := rel.Confidence
	rel.Confidence = relation.ConfirmedConfidence(rel.Confidence, relation.Resolve(row))
	if err := r.rels.Upsert(ctx, rel); err != nil {
		return ports.Relation{}, fmt.Errorf("capture: raising confirmed relation %q: %w", rel.ID, err)
	}

	// Only now. See this function's own doc comment — and note the signal
	// is emitted even when the raise was a no-op, because it records the
	// CONFIRMATION and not the delta.
	sig, err := relationSignal(r.ids.New(), ports.SignalRelationConfirm, ports.ValencePositive, rel, judged, now)
	if err != nil {
		return ports.Relation{}, err
	}
	if err := r.signals.Record(ctx, sig); err != nil {
		return ports.Relation{}, fmt.Errorf("capture: recording the relation confirmation: %w", err)
	}
	return rel, nil
}

// relationSignalContext is a relation verdict's context beside its type (doc
// 02 §9): who made the relation, how sure the judge was when the user judged
// it, and when it was made. The learner counts only Nooma's own relations, and
// a deleted relation leaves nothing else to read these from.
type relationSignalContext struct {
	CreatedBy  string    `json:"created_by"`
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `json:"created_at"`
}

// relationSignal is the signal a verdict on rel emits, typed by rel's type;
// judged is the confidence the user saw, before a confirm raised it.
func relationSignal(id string, typ ports.SignalType, valence ports.Valence, rel ports.Relation, judged float64, now time.Time) (ports.Signal, error) {
	contextJSON, err := json.Marshal(relationSignalContext{CreatedBy: rel.CreatedBy, Confidence: judged, CreatedAt: rel.CreatedAt})
	if err != nil {
		return ports.Signal{}, fmt.Errorf("capture: encode the %s signal's context: %w", typ, err)
	}
	targetKind := ports.TargetKindRelation
	relType, relID := rel.Type, rel.ID
	return ports.Signal{
		ID: id, Type: typ, Valence: valence, TargetKind: &targetKind, TargetID: &relID,
		RelationType: &relType, Context: contextJSON, OccurredAt: now,
	}, nil
}

// recordCheckIn writes one check-in row.
func (r captureRunner) recordCheckIn(ctx context.Context, now time.Time, action ports.DecisionAction, rationale, triggerID string, resolution ports.TriggerResolution, openCount int) error {
	ctxValue := struct {
		TriggerID string `json:"trigger_id,omitempty"`
		// Resolution is empty on a snooze, which resolves nothing.
		Resolution string `json:"resolution,omitempty"`
		// OpenCheckIns is how many were open when the answer arrived. It
		// is the field that makes "we chose the most recent" auditable
		// rather than invisible: a 1 means there was nothing to choose,
		// and a 3 means there was.
		OpenCheckIns int `json:"open_check_ins"`
	}{TriggerID: triggerID, Resolution: string(resolution), OpenCheckIns: openCount}

	contextJSON, err := marshalContext(ctxValue)
	if err != nil {
		return fmt.Errorf("capture: encode check-in decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     action,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record check-in decision: %w", err)
	}
	return nil
}

// recordRelationCheckIn writes one relation check-in row.
//
// Beside recordCheckIn rather than through it, and the two actions are
// distinct for the same reason: recordCheckIn's Context is
// {trigger_id, resolution, open_check_ins}, and a relation answer's is
// {question_id, relation_id, resolution, open_relation_questions}. m2c
// §7.5's rule splits effects when their Context shapes differ, and putting
// a question id into a field named trigger_id would be the audit row that
// misdescribes what happened — which doc 02 §11 forbids by name.
// relationCheckInDetail is questionDetail plus the count only an inbound
// answer has: how many questions were open when it arrived.
//
// OpenRelationQuestions carries no omitempty, unlike questionDetail's own
// Resolution: a zero here is the unmatched row's whole point (there was
// nothing to choose from), and omitting it would erase exactly the fact
// the field exists to record.
type relationCheckInDetail struct {
	questionDetail
	OpenRelationQuestions int `json:"open_relation_questions"`
}

func (r captureRunner) recordRelationCheckIn(ctx context.Context, now time.Time, action ports.DecisionAction, rationale, questionID, relationID string, outcome classify.RelationOutcome, openCount int) error {
	ctxValue := relationCheckInDetail{
		questionDetail: questionDetail{
			QuestionID: questionID,
			RelationID: relationID,
			Resolution: string(outcome),
		},
		OpenRelationQuestions: openCount,
	}

	contextJSON, err := marshalContext(ctxValue)
	if err != nil {
		return fmt.Errorf("capture: encode relation check-in decision context: %w", err)
	}

	d := ports.Decision{
		ID:         r.ids.New(),
		Action:     action,
		Rationale:  rationale,
		Context:    contextJSON,
		OccurredAt: now,
	}
	if err := r.log.Record(ctx, d); err != nil {
		return fmt.Errorf("capture: record relation check-in decision: %w", err)
	}
	return nil
}

// resolveStateCheckIn applies a state_outcome — the user's answer to the
// load hypothesis m2's pattern_eval opened.
//
// A confirmation writes a fresh current_state row carrying the user's own
// energy reading; a denial writes one saying the hypothesis was wrong. The
// hypothesis row is NOT edited: current_state is append-only (doc 02 §10),
// and the answer is a new observation rather than a correction of the old
// one — which is also what lets the digest's care gate read the latest
// reading without caring who wrote it.
func (r captureRunner) resolveStateCheckIn(ctx context.Context, c classify.Classification, now time.Time) error {
	if c.StateOutcome == nil {
		return nil
	}

	signalType := ports.SignalStateConfirmed
	valence := ports.ValencePositive
	if *c.StateOutcome == classify.StateOutcomeDenied {
		signalType = ports.SignalStateDenied
		valence = ports.ValenceNegative
	}

	if err := r.signals.Record(ctx, ports.Signal{
		ID:         r.ids.New(),
		Type:       signalType,
		Valence:    valence,
		OccurredAt: now,
	}); err != nil {
		return fmt.Errorf("capture: recording the state answer: %w", err)
	}

	return r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInResolved,
		fmt.Sprintf("the load hypothesis was answered %q", *c.StateOutcome), "", "", 0)
}
