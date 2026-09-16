package brain

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/relation"
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

	err := r.RejectRelation(context.Background(), "rel-1", checkInNow)
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

	if err := r.RejectRelation(context.Background(), "rel-1", checkInNow); err == nil {
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

// --- m3e: the relation check-in --------------------------------------

// openQuestions returns a fixed set from Open and records every
// resolution into the shared event log, so an ordering can be asserted
// rather than two independent facts.
type openQuestions struct {
	ports.PendingQuestionRepo
	events   *[]string
	open     []ports.RelationQuestion
	resolved []string
	as       []ports.QuestionResolution
}

func (r *openQuestions) Open(context.Context) ([]ports.RelationQuestion, error) { return r.open, nil }

func (r *openQuestions) Confirm(_ context.Context, id string, _ time.Time) error {
	return r.resolve(id, ports.QuestionConfirmed)
}

func (r *openQuestions) Reject(_ context.Context, id string, _ time.Time) error {
	return r.resolve(id, ports.QuestionRejected)
}

func (r *openQuestions) resolve(id string, as ports.QuestionResolution) error {
	if r.events != nil {
		*r.events = append(*r.events, "question:"+string(as)+":"+id)
	}
	r.resolved = append(r.resolved, id)
	r.as = append(r.as, as)
	return nil
}

// confirmingRelations answers the confirm path's reads and records its
// Upsert into the shared event log. It embeds deletingRelations so one
// fake serves both halves of a relation check-in.
type confirmingRelations struct {
	deletingRelations
	byID       map[string]ports.Relation
	thresholds *relation.Thresholds
	upserted   []ports.Relation
	byIDErr    error
	upsertErr  error
}

func (r *confirmingRelations) ByID(_ context.Context, id string) (ports.Relation, error) {
	if r.byIDErr != nil {
		return ports.Relation{}, r.byIDErr
	}
	rel, ok := r.byID[id]
	if !ok {
		return ports.Relation{}, ports.ErrRelationNotFound
	}
	return rel, nil
}

func (r *confirmingRelations) ThresholdsFor(context.Context, string) (*relation.Thresholds, error) {
	return r.thresholds, nil
}

func (r *confirmingRelations) Upsert(_ context.Context, rel ports.Relation) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	*r.events = append(*r.events, "upsert:"+rel.ID)
	r.upserted = append(r.upserted, rel)
	return nil
}

func relationOutcome(o classify.RelationOutcome) classify.Classification {
	return classify.Classification{RelationOutcome: &o}
}

// relationQuestion is one open question, spelled short.
func relationQuestion(id, relationID string) ports.RelationQuestion {
	return ports.RelationQuestion{ID: id, RelationID: relationID, RelationType: "same_topic"}
}

// relationCheckInRunner wires the four ports this path touches over one
// shared event log.
func relationCheckInRunner(events *[]string, questions *openQuestions, rels *confirmingRelations, log ports.DecisionLog) captureRunner {
	return captureRunner{
		ids: &countingIDs{}, log: log,
		signals: &recordingSignals{events: events},
		rels:    rels, questions: questions,
	}
}

// relationCheckInContext is the shape recordRelationCheckIn writes —
// decoded here rather than string-matched, so a renamed field fails.
type relationCheckInContext struct {
	QuestionID            string `json:"question_id"`
	RelationID            string `json:"relation_id"`
	Resolution            string `json:"resolution"`
	OpenRelationQuestions int    `json:"open_relation_questions"`
}

func decodeRelationCheckIn(t *testing.T, raw json.RawMessage) relationCheckInContext {
	t.Helper()
	var got relationCheckInContext
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decoding the relation check-in context %s: %v", raw, err)
	}
	return got
}

// TestRelationCheckIn_AnAnswerWithNothingOpenResolvesAndDeletesNothing is
// R8 and owner ruling Q5, and it is asserted over BOTH outcome values
// because the rejected one is the one that matters.
//
// Deletion is the only irreversible act in this vault and disambiguation
// is a heuristic. An answer that names no question gives the heuristic
// nothing to work with, so refusing to guess which relation to delete is
// the only safe direction — the row is written and nothing else happens.
func TestRelationCheckIn_AnAnswerWithNothingOpenResolvesAndDeletesNothing(t *testing.T) {
	outcomes := classify.AllRelationOutcomes()
	if len(outcomes) == 0 {
		t.Fatal("classify.AllRelationOutcomes() is empty — this sweep proves nothing")
	}

	for _, o := range outcomes {
		t.Run(string(o), func(t *testing.T) {
			var events []string
			questions := &openQuestions{events: &events}
			rels := &confirmingRelations{deletingRelations: deletingRelations{events: &events}}
			log := &recordingLog{}

			if err := relationCheckInRunner(&events, questions, rels, log).
				resolveRelationCheckIn(context.Background(), relationOutcome(o), checkInNow); err != nil {
				t.Fatalf("resolveRelationCheckIn(%q): %v", o, err)
			}

			if len(events) != 0 {
				t.Fatalf("an answer with nothing open produced %v — it must resolve nothing and, above all, delete nothing", events)
			}
			if len(rels.deleted) != 0 {
				t.Fatalf("it deleted %v", rels.deleted)
			}
			got := decodeRelationCheckIn(t, log.contextFor(t, ports.ActionCaptureRelationCheckInUnmatched))
			if got.Resolution != string(o) || got.OpenRelationQuestions != 0 {
				t.Errorf("context = %+v, want resolution %q and open_relation_questions 0", got, o)
			}
			if got.QuestionID != "" || got.RelationID != "" {
				t.Errorf("context = %+v, want no question and no relation named — there was none", got)
			}
		})
	}
}

// TestRelationCheckIn_TheMostRecentlyAskedIsChosenAndTheCountIsRecorded is
// R4, and it mirrors resolveCheckIn's own D4 shape: an answer carries an
// outcome and not an id, so with several open the pass takes the head of
// Open — which orders most recently asked first.
//
// The count is the field that makes "we chose the most recent" auditable
// rather than invisible. A 1 means there was nothing to choose; a 3 means
// there was.
func TestRelationCheckIn_TheMostRecentlyAskedIsChosenAndTheCountIsRecorded(t *testing.T) {
	var events []string
	questions := &openQuestions{events: &events, open: []ports.RelationQuestion{
		relationQuestion("q-most-recent", "rel-most-recent"),
		relationQuestion("q-older", "rel-older"),
		relationQuestion("q-oldest", "rel-oldest"),
	}}
	rels := &confirmingRelations{deletingRelations: deletingRelations{events: &events}}
	log := &recordingLog{}

	if err := relationCheckInRunner(&events, questions, rels, log).
		resolveRelationCheckIn(context.Background(), relationOutcome(classify.RelationOutcomeRejected), checkInNow); err != nil {
		t.Fatalf("resolveRelationCheckIn: %v", err)
	}

	if len(rels.deleted) != 1 || rels.deleted[0] != "rel-most-recent" {
		t.Fatalf("deleted %v, want exactly the most recently asked question's relation — the head of Open, never the tail", rels.deleted)
	}
	if len(questions.resolved) != 1 || questions.resolved[0] != "q-most-recent" {
		t.Fatalf("resolved %v, want only q-most-recent", questions.resolved)
	}

	got := decodeRelationCheckIn(t, log.contextFor(t, ports.ActionCaptureRelationCheckInResolved))
	if got.QuestionID != "q-most-recent" || got.RelationID != "rel-most-recent" {
		t.Errorf("context = %+v, want the chosen question and its relation named", got)
	}
	if got.OpenRelationQuestions != 3 {
		t.Errorf("open_relation_questions = %d, want 3 — without it the audit trail implies there was only ever one", got.OpenRelationQuestions)
	}
}

// TestRelationCheckIn_OneOpenQuestionIsResolvedTrivially: the count still
// goes in, and it reads 1.
func TestRelationCheckIn_OneOpenQuestionIsResolvedTrivially(t *testing.T) {
	var events []string
	questions := &openQuestions{events: &events, open: []ports.RelationQuestion{relationQuestion("q-1", "rel-1")}}
	rels := &confirmingRelations{
		deletingRelations: deletingRelations{events: &events},
		byID:              map[string]ports.Relation{"rel-1": {ID: "rel-1", Type: "same_topic", Confidence: 0.4}},
	}
	log := &recordingLog{}

	if err := relationCheckInRunner(&events, questions, rels, log).
		resolveRelationCheckIn(context.Background(), relationOutcome(classify.RelationOutcomeConfirmed), checkInNow); err != nil {
		t.Fatalf("resolveRelationCheckIn: %v", err)
	}

	got := decodeRelationCheckIn(t, log.contextFor(t, ports.ActionCaptureRelationCheckInResolved))
	if got.QuestionID != "q-1" || got.OpenRelationQuestions != 1 {
		t.Errorf("context = %+v, want question q-1 and open_relation_questions 1", got)
	}
	if len(questions.as) != 1 || questions.as[0] != ports.QuestionConfirmed {
		t.Errorf("the question was resolved %v, want confirmed", questions.as)
	}
}

// TestRelationCheckIn_AClassificationWithNoRelationOutcomeTouchesNothing:
// most captures answer no relation question, and they must not read Open.
func TestRelationCheckIn_AClassificationWithNoRelationOutcomeTouchesNothing(t *testing.T) {
	var events []string
	questions := &openQuestions{events: &events, open: []ports.RelationQuestion{relationQuestion("q-1", "rel-1")}}
	rels := &confirmingRelations{deletingRelations: deletingRelations{events: &events}}
	log := &recordingLog{}

	if err := relationCheckInRunner(&events, questions, rels, log).
		resolveRelationCheckIn(context.Background(), classify.Classification{}, checkInNow); err != nil {
		t.Fatalf("resolveRelationCheckIn: %v", err)
	}
	if len(events) != 0 || len(log.actions) != 0 {
		t.Fatalf("a capture answering no relation question produced %v and wrote %v", events, log.actions)
	}
}

// TestConfirmRelation_RaisesConfidenceThenEmitsTheSignal is R5 and design
// §3.6's deliberate REVERSAL of I10's ordering, asserted as a sequence.
//
// I10 emits first because its effect is a DELETE: irreversible, so a
// signal for a relation that survived is the recoverable error to prefer.
// Here the effect is idempotent — ConfirmedConfidence applied twice is
// ConfirmedConfidence applied once — so re-running the raise costs
// nothing, while a signal emitted for a raise that then failed is evidence
// the learning module would tune on forever. Same principle, opposite
// direction, and the two are asserted apart so neither is harmonised into
// the other.
func TestConfirmRelation_RaisesConfidenceThenEmitsTheSignal(t *testing.T) {
	var events []string
	createdAt := checkInNow.AddDate(0, 0, -3)
	rels := &confirmingRelations{
		deletingRelations: deletingRelations{events: &events},
		byID: map[string]ports.Relation{"rel-1": {
			ID: "rel-1", FromUnitID: "u-a", ToUnitID: "u-b", Type: "same_topic",
			Strength: 0.7, Confidence: 0.4, CreatedBy: "consolidation", CreatedAt: createdAt,
		}},
	}
	r := relationCheckInRunner(&events, &openQuestions{events: &events}, rels, &recordingLog{})

	if err := r.ConfirmRelation(context.Background(), relationQuestion("q-1", "rel-1"), checkInNow); err != nil {
		t.Fatalf("ConfirmRelation: %v", err)
	}

	want := []string{"upsert:rel-1", "signal:" + string(ports.SignalRelationConfirm)}
	if len(events) != 2 || events[0] != want[0] || events[1] != want[1] {
		t.Fatalf("events = %v, want %v — the signal is emitted AFTER the raise, which is the OPPOSITE of I10's order and deliberately so", events, want)
	}

	if len(rels.upserted) != 1 {
		t.Fatalf("upserted %d row(s), want 1", len(rels.upserted))
	}
	got := rels.upserted[0]
	if got.Confidence != relation.DefaultMinConfidenceToSurface {
		t.Errorf("Confidence = %v, want the surface floor %v — confirming lifts a relation OUT of the uncertain band", got.Confidence, relation.DefaultMinConfidenceToSurface)
	}
	// Only Confidence is replaced. The rest travels back untouched —
	// belt and braces beside Upsert's own contract, which already refuses
	// to rewrite them.
	if got.Strength != 0.7 || got.CreatedBy != "consolidation" || !got.CreatedAt.Equal(createdAt) {
		t.Errorf("Upsert got %+v — Strength, CreatedBy and CreatedAt must travel back unchanged", got)
	}
	if got.FromUnitID != "u-a" || got.ToUnitID != "u-b" || got.Type != "same_topic" {
		t.Errorf("Upsert got %+v — a confirmation revises one relation in place (I07), it does not re-point it", got)
	}
}

// TestConfirmRelation_ARelationAlreadyAboveTheFloorStillEmitsItsSignal is
// spec R5's own scenario: the raise is a no-op and the signal is not.
//
// The signal records the CONFIRMATION, not the delta. A user who says
// "yes, they're related" about something the system was already sure of
// has still told it something, and dropping that datum because the
// arithmetic happened to be a no-op would hide the one input the learning
// module exists to read.
func TestConfirmRelation_ARelationAlreadyAboveTheFloorStillEmitsItsSignal(t *testing.T) {
	var events []string
	rels := &confirmingRelations{
		deletingRelations: deletingRelations{events: &events},
		byID: map[string]ports.Relation{"rel-1": {
			ID: "rel-1", Type: "same_topic", Strength: 0.9, Confidence: 0.8,
		}},
	}
	r := relationCheckInRunner(&events, &openQuestions{events: &events}, rels, &recordingLog{})

	if err := r.ConfirmRelation(context.Background(), relationQuestion("q-1", "rel-1"), checkInNow); err != nil {
		t.Fatalf("ConfirmRelation: %v", err)
	}

	if len(rels.upserted) != 1 || rels.upserted[0].Confidence != 0.8 {
		t.Fatalf("upserted %+v, want the confidence unchanged at 0.8 — confirming never LOWERS a confidence", rels.upserted)
	}
	if len(events) != 2 || events[1] != "signal:"+string(ports.SignalRelationConfirm) {
		t.Fatalf("events = %v, want the signal emitted even though the raise was a no-op — the signal records the confirmation, not the delta", events)
	}
}

// TestConfirmRelation_ReadsThisRelationTypesOwnThresholds: the floor is
// the relation type's own min_confidence_to_surface, never a package
// constant assumed to apply (doc 02 §4, ADR-0027's Related decision).
func TestConfirmRelation_ReadsThisRelationTypesOwnThresholds(t *testing.T) {
	var events []string
	rels := &confirmingRelations{
		deletingRelations: deletingRelations{events: &events},
		byID:              map[string]ports.Relation{"rel-1": {ID: "rel-1", Type: "derived_from", Confidence: 0.4}},
		thresholds:        &relation.Thresholds{Persist: 0.2, Surface: 0.9},
	}
	r := relationCheckInRunner(&events, &openQuestions{events: &events}, rels, &recordingLog{})

	if err := r.ConfirmRelation(context.Background(), relationQuestion("q-1", "rel-1"), checkInNow); err != nil {
		t.Fatalf("ConfirmRelation: %v", err)
	}
	if len(rels.upserted) != 1 || rels.upserted[0].Confidence != 0.9 {
		t.Fatalf("upserted %+v, want confidence 0.9 — this type's own configured surface floor, not the package default", rels.upserted)
	}
}

// TestConfirmRelation_AFailedRaiseEmitsNoSignal is the direction this
// ordering was chosen for: nothing is written, nothing is claimed, and the
// question stays open so the next answer retries.
func TestConfirmRelation_AFailedRaiseEmitsNoSignal(t *testing.T) {
	var events []string
	rels := &confirmingRelations{
		deletingRelations: deletingRelations{events: &events},
		byID:              map[string]ports.Relation{"rel-1": {ID: "rel-1", Type: "same_topic", Confidence: 0.4}},
		upsertErr:         errors.New("the vault is closed"),
	}
	r := relationCheckInRunner(&events, &openQuestions{events: &events}, rels, &recordingLog{})

	if err := r.ConfirmRelation(context.Background(), relationQuestion("q-1", "rel-1"), checkInNow); err == nil {
		t.Fatal("ConfirmRelation returned nil for a failed raise")
	}
	if len(events) != 0 {
		t.Fatalf("events = %v, want none — a signal for a raise that failed is evidence the learning module would tune on forever", events)
	}
}

// TestRelationCheckIn_AFailedRejectLeavesItsQuestionOpen is task 6.6's own
// ordering: the question is closed only AFTER RejectRelation returns.
//
// Reversed, a failed reject would silently close the question that asked
// about it, and the relation would survive with nothing left open to ask
// again. Closing after keeps the path retry-safe.
func TestRelationCheckIn_AFailedRejectLeavesItsQuestionOpen(t *testing.T) {
	var events []string
	questions := &openQuestions{events: &events, open: []ports.RelationQuestion{relationQuestion("q-1", "rel-1")}}
	rels := &confirmingRelations{deletingRelations: deletingRelations{events: &events, err: errors.New("the vault is closed")}}
	log := &recordingLog{}

	if err := relationCheckInRunner(&events, questions, rels, log).
		resolveRelationCheckIn(context.Background(), relationOutcome(classify.RelationOutcomeRejected), checkInNow); err == nil {
		t.Fatal("resolveRelationCheckIn returned nil for a failed reject")
	}
	if len(questions.resolved) != 0 {
		t.Fatalf("the question was resolved %v despite the reject failing — a failed delete must leave something open to ask again", questions.resolved)
	}
	if n := log.count(ports.ActionCaptureRelationCheckInResolved); n != 0 {
		t.Errorf("%d resolved row(s) for a rejection that did not happen, want 0", n)
	}
}
