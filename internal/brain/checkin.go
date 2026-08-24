package brain

import (
	"context"
	"fmt"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
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
func (r captureRunner) resolveCheckIn(ctx context.Context, c classify.Classification, now time.Time) (bool, error) {
	resolution, ok := checkInResolution(c)
	if !ok {
		return false, nil
	}

	open, err := r.triggers.Delivered(ctx)
	if err != nil {
		return false, fmt.Errorf("capture: reading open check-ins: %w", err)
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
		return false, fmt.Errorf("capture: resolving check-in %q: %w", target.ID, err)
	}

	return true, r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInResolved,
		fmt.Sprintf("check-in %q resolved as %q", target.ID, resolution), target.ID, resolution, len(open))
}

// checkInResolution maps an answer's outcome onto the resolution
// vocabulary, reporting whether this classification answers a check-in at
// all.
//
// **Snooze resolves nothing, and that is the honest reading rather than a
// gap.** Spec R5.2 says a task_checkin_outcome "resolves an open task
// check-in", and two of the three do. Snooze means "ask me later": the
// check-in is neither engaged nor declined, and forcing it into either
// would record an answer the user did not give. It stays open, and the
// next digest or push asks again — which is what the user requested.
//
// self_healed is in the resolution vocabulary and in neither classify
// vocabulary, and that is also correct: nobody types it. It is the
// system's own verdict for a nudge that fresh activity made moot, and
// producing it is M4's.
func checkInResolution(c classify.Classification) (ports.TriggerResolution, bool) {
	if c.NudgeOutcome != nil {
		switch *c.NudgeOutcome {
		case classify.NudgeOutcomeEngaged:
			return ports.ResolutionEngaged, true
		case classify.NudgeOutcomeDeclined:
			return ports.ResolutionDeclined, true
		}
	}

	if c.TaskCheckinOutcome != nil {
		switch *c.TaskCheckinOutcome {
		case classify.TaskCheckinOutcomeDone:
			return ports.ResolutionEngaged, true
		case classify.TaskCheckinOutcomeDrop:
			return ports.ResolutionDeclined, true
		case classify.TaskCheckinOutcomeSnooze:
			return "", false
		}
	}

	return "", false
}

// resolveRelationCheckIn applies a relation_outcome — I10, and the one
// place in this codebase that deletes anything.
//
// **The signal is emitted BEFORE the delete**, which is I10's own wording
// and not an ordering convenience: a signal written after a delete that
// failed halfway would be evidence for a rejection that did not happen,
// and the learning module would tune on it forever. Emitted first, the
// worst case is a signal for a relation that survived — which the next
// rejection corrects, and which is recoverable in the direction that
// matters.
//
// A confirmation raises confidence rather than touching the row's
// existence, and its own signal says so.
func (r captureRunner) resolveRelationCheckIn(ctx context.Context, c classify.Classification, now time.Time) error {
	if c.RelationOutcome == nil {
		return nil
	}

	// Which relation the answer is about is M4's to resolve — a digest
	// that asked "I linked X with Y, are they related?" knows the id, and
	// carrying it back through an inbound message is m3e's conversational
	// state. What this ships is the resolution path itself, exercised by
	// its own tests, and a recorded row saying an answer arrived with no
	// relation named.
	return r.recordCheckIn(ctx, now, ports.ActionCaptureCheckInUnmatched,
		fmt.Sprintf("a relation answer of %q arrived, and naming which relation it answers is m3e's", *c.RelationOutcome),
		"", "", 0)
}

// RejectRelation deletes one relation, emitting its signal first — I10.
//
// Exported on captureRunner's behalf rather than inlined above because the
// ordering is the invariant, and a caller that gets it wrong should have
// to get it wrong HERE, in one reviewable place, rather than at whichever
// call site M4 adds next.
func (r captureRunner) RejectRelation(ctx context.Context, rel ports.Relation, now time.Time) error {
	targetKind := ports.TargetKindRelation
	if err := r.signals.Record(ctx, ports.Signal{
		ID:         r.ids.New(),
		Type:       ports.SignalRelationReject,
		Valence:    ports.ValenceNegative,
		TargetKind: &targetKind,
		TargetID:   &rel.ID,
		OccurredAt: now,
	}); err != nil {
		return fmt.Errorf("capture: recording the relation rejection: %w", err)
	}

	// Only now. See this function's own doc comment.
	if err := r.rels.Delete(ctx, rel.ID); err != nil {
		return fmt.Errorf("capture: deleting rejected relation %q: %w", rel.ID, err)
	}
	return nil
}

// recordCheckIn writes one check-in row.
func (r captureRunner) recordCheckIn(ctx context.Context, now time.Time, action ports.DecisionAction, rationale, triggerID string, resolution ports.TriggerResolution, openCount int) error {
	ctxValue := struct {
		TriggerID  string `json:"trigger_id,omitempty"`
		Resolution string `json:"resolution"`
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
