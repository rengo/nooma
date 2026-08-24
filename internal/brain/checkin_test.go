package brain

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/ports"
)

// openCheckIns returns a fixed set from Delivered and records what was
// resolved.
type openCheckIns struct {
	emptyTriggers
	open     []ports.DueTrigger
	resolved []string
	as       []ports.TriggerResolution
}

func (r *openCheckIns) Delivered(context.Context) ([]ports.DueTrigger, error) { return r.open, nil }

func (r *openCheckIns) Resolve(_ context.Context, id string, to ports.TriggerResolution, _ time.Time) error {
	r.resolved = append(r.resolved, id)
	r.as = append(r.as, to)
	return nil
}

var checkInNow = time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)

func nudge(o classify.NudgeOutcome) classify.Classification {
	return classify.Classification{NudgeOutcome: &o}
}

func taskCheckin(o classify.TaskCheckinOutcome) classify.Classification {
	return classify.Classification{TaskCheckinOutcome: &o}
}

func checkInRunner(triggers ports.TriggerRepo, log ports.DecisionLog) captureRunner {
	return captureRunner{triggers: triggers, log: log, ids: &countingIDs{}}
}

// TestCheckInResolution_CoversEveryNudgeOutcome is R5.1, iterated over the
// vocabulary rather than listed — a third member added later fails the
// loop pass with no expectation instead of being silently unhandled.
func TestCheckInResolution_CoversEveryNudgeOutcome(t *testing.T) {
	want := map[classify.NudgeOutcome]ports.TriggerResolution{
		classify.NudgeOutcomeEngaged:  ports.ResolutionEngaged,
		classify.NudgeOutcomeDeclined: ports.ResolutionDeclined,
	}

	outcomes := classify.AllNudgeOutcomes()
	if len(outcomes) == 0 {
		t.Fatal("classify.AllNudgeOutcomes() is empty — this sweep proves nothing")
	}

	for _, o := range outcomes {
		expected, known := want[o]
		if !known {
			t.Errorf("nudge outcome %q has no resolution — a member was added and this mapping was not revisited", o)
			continue
		}

		triggers := &openCheckIns{open: []ports.DueTrigger{{ID: "trg-1"}}}
		resolved, err := checkInRunner(triggers, &recordingLog{}).
			resolveCheckIn(context.Background(), nudge(o), checkInNow)
		if err != nil {
			t.Fatalf("resolveCheckIn(%q): %v", o, err)
		}

		if !resolved || len(triggers.as) != 1 || triggers.as[0] != expected {
			t.Errorf("%q resolved as %v, want %q", o, triggers.as, expected)
		}
	}
}

// TestCheckInResolution_CoversEveryTaskOutcome, and snooze is the one that
// deliberately resolves nothing.
func TestCheckInResolution_CoversEveryTaskOutcome(t *testing.T) {
	want := map[classify.TaskCheckinOutcome]struct {
		to       ports.TriggerResolution
		resolves bool
	}{
		classify.TaskCheckinOutcomeDone: {ports.ResolutionEngaged, true},
		classify.TaskCheckinOutcomeDrop: {ports.ResolutionDeclined, true},
		// Snooze means "ask me later". The check-in is neither engaged
		// nor declined, and forcing it into either would record an answer
		// the user did not give.
		classify.TaskCheckinOutcomeSnooze: {resolves: false},
	}

	for _, o := range classify.AllTaskCheckinOutcomes() {
		expected, known := want[o]
		if !known {
			t.Errorf("task outcome %q has no expectation — a member was added and this mapping was not revisited", o)
			continue
		}

		triggers := &openCheckIns{open: []ports.DueTrigger{{ID: "trg-1"}}}
		resolved, err := checkInRunner(triggers, &recordingLog{}).
			resolveCheckIn(context.Background(), taskCheckin(o), checkInNow)
		if err != nil {
			t.Fatalf("resolveCheckIn(%q): %v", o, err)
		}

		if resolved != expected.resolves {
			t.Errorf("%q resolved = %v, want %v", o, resolved, expected.resolves)
			continue
		}
		if expected.resolves && triggers.as[0] != expected.to {
			t.Errorf("%q resolved as %q, want %q", o, triggers.as[0], expected.to)
		}
		if !expected.resolves && len(triggers.resolved) != 0 {
			t.Errorf("%q resolved %v — snooze must leave the check-in open so the next pass asks again", o, triggers.resolved)
		}
	}
}

// TestCheckIn_AnAnswerWithNothingOpenIsRecordedAndChangesNothing is R5.1's
// second MUST. A user saying "done" out of the blue is not an error.
func TestCheckIn_AnAnswerWithNothingOpenIsRecordedAndChangesNothing(t *testing.T) {
	triggers := &openCheckIns{}
	log := &recordingLog{}

	resolved, err := checkInRunner(triggers, log).
		resolveCheckIn(context.Background(), nudge(classify.NudgeOutcomeEngaged), checkInNow)
	if err != nil {
		t.Fatalf("resolveCheckIn: %v", err)
	}

	if resolved {
		t.Error("an answer with nothing open reported a resolution")
	}
	if len(triggers.resolved) != 0 {
		t.Errorf("it resolved %v", triggers.resolved)
	}
	if n := log.count(ports.ActionCaptureCheckInUnmatched); n != 1 {
		t.Errorf("%d unmatched rows, want 1 — a check-in that vanished between question and answer is worth seeing", n)
	}
}

// TestCheckIn_TheMostRecentIsChosenAndTheChoiceIsRecorded is design D4.
//
// An answer carries an outcome and not an id, so with several open the
// pass takes the head of Delivered — which orders most recent first. The
// count goes into the row: a 1 means there was nothing to choose, a 3
// means there was, and without it the audit trail would imply there was
// only ever one.
func TestCheckIn_TheMostRecentIsChosenAndTheChoiceIsRecorded(t *testing.T) {
	triggers := &openCheckIns{open: []ports.DueTrigger{
		{ID: "trg-most-recent"}, {ID: "trg-older"}, {ID: "trg-oldest"},
	}}
	log := &recordingLog{}

	if _, err := checkInRunner(triggers, log).
		resolveCheckIn(context.Background(), nudge(classify.NudgeOutcomeEngaged), checkInNow); err != nil {
		t.Fatalf("resolveCheckIn: %v", err)
	}

	if len(triggers.resolved) != 1 || triggers.resolved[0] != "trg-most-recent" {
		t.Fatalf("resolved %v, want only the most recent", triggers.resolved)
	}
	if n := log.count(ports.ActionCaptureCheckInResolved); n != 1 {
		t.Errorf("%d resolved rows, want 1", n)
	}
}

// TestCheckIn_AClassificationWithNoOutcomeTouchesNothing: most captures
// answer no check-in, and they must not read Delivered or write a row.
func TestCheckIn_AClassificationWithNoOutcomeTouchesNothing(t *testing.T) {
	triggers := &openCheckIns{open: []ports.DueTrigger{{ID: "trg-1"}}}
	log := &recordingLog{}

	resolved, err := checkInRunner(triggers, log).
		resolveCheckIn(context.Background(), classify.Classification{}, checkInNow)
	if err != nil {
		t.Fatalf("resolveCheckIn: %v", err)
	}

	if resolved || len(triggers.resolved) != 0 || len(log.actions) != 0 {
		t.Fatalf("a capture answering nothing resolved %v and wrote %v — most captures answer no check-in and must cost nothing",
			triggers.resolved, log.actions)
	}
}
