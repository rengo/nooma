package brain

import (
	"context"
	"errors"
	"reflect"
	"strings"
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

// recordingSignals records what was written and when, so an ordering can
// be asserted rather than just two facts.
type recordingSignals struct {
	events *[]string
}

func (s *recordingSignals) Record(_ context.Context, sig ports.Signal) error {
	*s.events = append(*s.events, "signal:"+string(sig.Type))
	return nil
}

func (s *recordingSignals) Since(context.Context, time.Time, int) ([]ports.Signal, error) {
	return nil, nil
}

// deletingRelations records deletions into the same event log.
type deletingRelations struct {
	ports.RelationRepo
	events  *[]string
	deleted []string
	err     error
}

func (r *deletingRelations) Delete(_ context.Context, id string) error {
	if r.err != nil {
		return r.err
	}
	*r.events = append(*r.events, "delete:"+id)
	r.deleted = append(r.deleted, id)
	return nil
}

// TestRejectRelation_EmitsTheSignalBeforeDeleting is I10, asserted as an
// ORDERING rather than as two independent facts.
//
// The ordering is the invariant's own wording, and it is not a
// convenience: a signal written after a delete that failed halfway would
// be evidence for a rejection that did not happen, and the learning module
// would tune on it forever. Emitted first, the worst case is a signal for
// a relation that survived — recoverable in the direction that matters.
func TestRejectRelation_EmitsTheSignalBeforeDeleting(t *testing.T) {
	var events []string
	rels := &deletingRelations{events: &events}
	r := captureRunner{
		ids:     &countingIDs{},
		log:     &recordingLog{},
		signals: &recordingSignals{events: &events},
		rels:    rels,
	}

	err := r.RejectRelation(context.Background(), ports.Relation{ID: "rel-1"}, checkInNow)
	if err != nil {
		t.Fatalf("RejectRelation: %v", err)
	}

	want := []string{"signal:" + string(ports.SignalRelationReject), "delete:rel-1"}
	if len(events) != 2 || events[0] != want[0] || events[1] != want[1] {
		t.Fatalf("events = %v, want %v — I10 names the ordering, and a signal written after a half-failed delete is evidence for a rejection that did not happen", events, want)
	}
}

// TestRejectRelation_AFailedDeleteStillLeftItsSignal is the direction the
// ordering was chosen for: the recoverable failure.
func TestRejectRelation_AFailedDeleteStillLeftItsSignal(t *testing.T) {
	var events []string
	rels := &deletingRelations{events: &events, err: errors.New("the vault is closed")}
	r := captureRunner{
		ids:     &countingIDs{},
		log:     &recordingLog{},
		signals: &recordingSignals{events: &events},
		rels:    rels,
	}

	if err := r.RejectRelation(context.Background(), ports.Relation{ID: "rel-1"}, checkInNow); err == nil {
		t.Fatal("RejectRelation returned nil for a failed delete")
	}
	if len(events) != 1 || !strings.HasPrefix(events[0], "signal:") {
		t.Fatalf("events = %v, want only the signal — it is written first precisely so this case leaves evidence rather than silence", events)
	}
	if len(rels.deleted) != 0 {
		t.Errorf("the relation was recorded as deleted: %v", rels.deleted)
	}
}

// TestStateCheckIn_CoversEveryStateOutcome, iterated over the vocabulary.
func TestStateCheckIn_CoversEveryStateOutcome(t *testing.T) {
	want := map[classify.StateOutcome]ports.SignalType{
		classify.StateOutcomeConfirmed: ports.SignalStateConfirmed,
		classify.StateOutcomeDenied:    ports.SignalStateDenied,
	}

	outcomes := classify.AllStateOutcomes()
	if len(outcomes) == 0 {
		t.Fatal("classify.AllStateOutcomes() is empty")
	}

	for _, o := range outcomes {
		expected, known := want[o]
		if !known {
			t.Errorf("state outcome %q has no signal — a member was added and this mapping was not revisited", o)
			continue
		}

		var events []string
		outcome := o
		r := captureRunner{
			ids: &countingIDs{}, log: &recordingLog{},
			signals: &recordingSignals{events: &events},
		}
		if err := r.resolveStateCheckIn(context.Background(), classify.Classification{StateOutcome: &outcome}, checkInNow); err != nil {
			t.Fatalf("resolveStateCheckIn(%q): %v", o, err)
		}

		if len(events) != 1 || events[0] != "signal:"+string(expected) {
			t.Errorf("%q wrote %v, want the %q signal", o, events, expected)
		}
	}
}

// TestStateCheckIn_DoesNotEditTheHypothesisRow: current_state is
// append-only (doc 02 §10), and the answer is a new observation rather
// than a correction of the old one.
func TestStateCheckIn_DoesNotEditTheHypothesisRow(t *testing.T) {
	// ports.StateRepo declares no update path at all, so this is
	// structural rather than behavioural — asserted by reflection so it
	// stays true when the port widens.
	stateRepo := reflect.TypeOf((*ports.StateRepo)(nil)).Elem()
	for i := 0; i < stateRepo.NumMethod(); i++ {
		name := stateRepo.Method(i).Name
		for _, forbidden := range []string{"Update", "Edit", "Set", "Amend"} {
			if strings.HasPrefix(name, forbidden) {
				t.Errorf("ports.StateRepo declares %s — current_state is append-only (doc 02 §10), and an answer is a new observation rather than a correction of the old one", name)
			}
		}
	}
}
