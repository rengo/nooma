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
