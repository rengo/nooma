// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// degradationRow is the part of a decision's context that records what the
// decoder lost (doc 02 §5.1, I12): present only when something was lost.
type degradationRow struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// captureDecisions captures message against the synthetic response (the
// test's own, non-negotiable 5) and returns every decision_log row written.
func captureDecisions(t *testing.T, message, response string) []ports.Decision {
	t.Helper()
	rows, err := captureAnyDecisions(t, message, response)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return rows
}

// captureAnyDecisions is captureDecisions for a capture that may fail: a
// failed capture writes decision rows too (an unclassifiable one among them).
func captureAnyDecisions(t *testing.T, message, response string) ([]ports.Decision, error) {
	t.Helper()
	return captureWithChat(t, message, response, "")
}

// captureWithChat scripts the chat task from a recorded case when chatCase is
// named; otherwise the chat task shares the classifier's provider.
func captureWithChat(t *testing.T, message, response, chatCase string) ([]ports.Decision, error) {
	t.Helper()
	return captureFull(t, message, response, chatCase, nil)
}

// captureFull also lets the embedding provider fail when embedErr is set.
func captureFull(t *testing.T, message, response, chatCase string, embedErr error) ([]ports.Decision, error) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	decisions, embeddings := memrepo.NewDecisionLog(), memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, i28Case(t, "degraded", message, response), "degraded")
	chat := llm
	if chatCase != "" {
		chat = fakeprovider.New(t, testdataLLMCasesDir(t), chatCase)
	}
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	if embedErr != nil {
		embed = fakeprovider.NewEmbeddingFakeWithError(embedFakeModel, embedErr)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, memrepo.NewUnits(), embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), decisions, llm, llm, chat, embed,
		brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)
	_, capErr := svc.Capture(ctx, brain.CaptureInput{Text: message, Channel: "chat"})
	rows, err := decisions.Since(ctx, time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	return rows, capErr
}

// decisionByAction returns the single row written under action.
func decisionByAction(t *testing.T, rows []ports.Decision, action ports.DecisionAction) ports.Decision {
	t.Helper()
	for _, r := range rows {
		if r.Action == action {
			return r
		}
	}
	t.Fatalf("no %s row among %d", action, len(rows))
	return ports.Decision{}
}

func degradationsOf(t *testing.T, r ports.Decision) (list []degradationRow, present bool) {
	t.Helper()
	var c map[string]json.RawMessage
	if err := json.Unmarshal(r.Context, &c); err != nil {
		t.Fatal(err)
	}
	raw, present := c["degradations"]
	if present {
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatal(err)
		}
	}
	return list, present
}

const (
	// A title holding an ISO date is dropped by ResolveTitle: bad_format.
	badTitleTask = `{"type":"task","normalized_content":"Llamar a Ana","weight":0.5,"decay_rate":0.03,"title":"Llamar 2026-10-15","language":"es"}`
	cleanTask    = `{"type":"task","normalized_content":"Llamar a Ana","weight":0.5,"decay_rate":0.03,"title":"Llamar a Ana","language":"es"}`
	badTitleAmbi = `{"type":"task","normalized_content":"Llamar a Ana","weight":0.5,"decay_rate":0.03,"title":"Llamar 2026-10-15","person_ref_status":"ambiguous","language":"es"}`
)

// TestI12_CaptureRecordsAnInvalidTitleDegradation: doc 02 §5.1 says a degraded
// field is recorded; the capture.classify row names it, field and reason, and
// its rationale says so.
func TestI12_CaptureRecordsAnInvalidTitleDegradation(t *testing.T) {
	row := decisionByAction(t, captureDecisions(t, "llamar a Ana", badTitleTask), ports.ActionCaptureClassify)

	got, present := degradationsOf(t, row)
	if !present || len(got) != 1 || got[0] != (degradationRow{Field: "title", Reason: "bad_format"}) {
		t.Errorf("degradations = %+v (present %v), want exactly [title: bad_format]", got, present)
	}
	if !strings.Contains(row.Rationale, "title") || !strings.Contains(row.Rationale, "bad_format") {
		t.Errorf("rationale %q does not name the degradation", row.Rationale)
	}
}

// TestI12_CaptureWithoutDegradationsRecordsNone: the control. A clean
// classification leaves the row exactly as it was — no key, no rationale tail.
func TestI12_CaptureWithoutDegradationsRecordsNone(t *testing.T) {
	row := decisionByAction(t, captureDecisions(t, "llamar a Ana", cleanTask), ports.ActionCaptureClassify)

	if got, present := degradationsOf(t, row); present {
		t.Errorf("degradations = %+v, want the key absent on a clean capture", got)
	}
	if strings.Contains(row.Rationale, "degrad") {
		t.Errorf("rationale %q mentions a degradation on a clean capture", row.Rationale)
	}
}

// TestI12_AmbiguousPersonCaptureRecordsTheDegradation: the ambiguous-person
// capture writes capture.unit.created instead of capture.classify, and that row
// is the classification's account there, so it carries the degradation too.
func TestI12_AmbiguousPersonCaptureRecordsTheDegradation(t *testing.T) {
	row := decisionByAction(t, captureDecisions(t, "llamar a Ana", badTitleAmbi), ports.ActionCaptureUnitCreated)

	got, present := degradationsOf(t, row)
	if !present || len(got) != 1 || got[0] != (degradationRow{Field: "title", Reason: "bad_format"}) {
		t.Errorf("degradations = %+v (present %v), want exactly [title: bad_format]", got, present)
	}
	if !strings.Contains(row.Rationale, "title") || !strings.Contains(row.Rationale, "bad_format") {
		t.Errorf("rationale %q does not name the degradation", row.Rationale)
	}
}

// correctionDecisions corrects an existing meeting through an explicit
// referent with the synthetic response given and returns every decision row.
func correctionDecisions(t *testing.T, response string) []ports.Decision {
	t.Helper()
	const message = "no, es el viernes"
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	original := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	units, decisions, embeddings := memrepo.NewUnits(), memrepo.NewDecisionLog(), memrepo.NewEmbeddings()
	if err := units.Create(ctx, unit.Unit{
		ID: "meeting", Type: unit.TypeEvent, Status: unit.StatusPool, Language: "es",
		Content: "Reunión con Juan el 2026-10-15 a las 10:00", EventAt: &original,
		Source: "cli", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, i28Case(t, "degraded-correction", message, response), "degraded-correction")
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), decisions, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel),
		brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)
	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: message, Channel: "ui", ReferentID: "meeting"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	rows, err := decisions.Since(ctx, time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

const (
	movedBadTitle = `{"type":"correction","normalized_content":"Reunión con Juan el 2026-10-16 a las 10:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-16T10:00:00Z","title":"Reunión 2026-10-16","language":"es"}`
	movedClean    = `{"type":"correction","normalized_content":"Reunión con Juan el 2026-10-16 a las 10:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-16T10:00:00Z","title":"Reunión con Juan","language":"es"}`
	notAnEditBad  = `{"type":"chitchat","normalized_content":"hola","weight":0.1,"decay_rate":0.03,"title":"Hola 2026-10-16","language":"es"}`
)

// TestI12_CorrectionRecordsADegradation: a correction decodes through the same
// Decode as a capture, and its pre-image row is where the classification is
// accounted for, so that is where the lost field is written.
func TestI12_CorrectionRecordsADegradation(t *testing.T) {
	row := decisionByAction(t, correctionDecisions(t, movedBadTitle), ports.ActionCorrectionApplied)

	got, present := degradationsOf(t, row)
	if !present || len(got) != 1 || got[0] != (degradationRow{Field: "title", Reason: "bad_format"}) {
		t.Errorf("degradations = %+v (present %v), want exactly [title: bad_format]", got, present)
	}
	if !strings.Contains(row.Rationale, "title") || !strings.Contains(row.Rationale, "bad_format") {
		t.Errorf("rationale %q does not name the degradation", row.Rationale)
	}
}

// TestI12_CleanCorrectionRecordsNone is the control: the pre-image keeps its
// ADR-0016 shape and gains nothing.
func TestI12_CleanCorrectionRecordsNone(t *testing.T) {
	row := decisionByAction(t, correctionDecisions(t, movedClean), ports.ActionCorrectionApplied)

	if got, present := degradationsOf(t, row); present {
		t.Errorf("degradations = %+v, want the key absent on a clean correction", got)
	}
	if strings.Contains(row.Rationale, "degrad") {
		t.Errorf("rationale %q mentions a degradation on a clean correction", row.Rationale)
	}
}

// TestI12_AskedCorrectionRecordsTheDegradation: a correction that asks instead
// of editing has no pre-image, and the degraded reading may be why it asked, so
// its ask row carries the list as well.
func TestI12_AskedCorrectionRecordsTheDegradation(t *testing.T) {
	row := decisionByAction(t, correctionDecisions(t, notAnEditBad), ports.ActionCorrectionAmbiguous)

	got, present := degradationsOf(t, row)
	if !present || len(got) != 1 || got[0] != (degradationRow{Field: "title", Reason: "bad_format"}) {
		t.Errorf("degradations = %+v (present %v), want exactly [title: bad_format]", got, present)
	}
	if !strings.Contains(row.Rationale, "title") || !strings.Contains(row.Rationale, "bad_format") {
		t.Errorf("rationale %q does not name the degradation", row.Rationale)
	}
}

// everyRowCarries fails for each row that does not hold exactly the title
// degradation: "every degradation" is every row the flow writes after Decode,
// not the rows somebody remembered.
func everyRowCarries(t *testing.T, rows []ports.Decision, want ...degradationRow) {
	t.Helper()
	if len(want) == 0 {
		want = []degradationRow{{Field: "title", Reason: "bad_format"}}
	}
	for _, r := range rows {
		got, present := degradationsOf(t, r)
		if !present || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: degradations = %+v (present %v), want exactly %+v", r.Action, got, present, want)
		}
		if !strings.Contains(r.Rationale, want[0].Reason) {
			t.Errorf("%s: rationale %q does not name the degradation", r.Action, r.Rationale)
		}
	}
}

// everyRowClean is the control: no row carries the key or the sentence.
func everyRowClean(t *testing.T, rows []ports.Decision) {
	t.Helper()
	for _, r := range rows {
		if got, present := degradationsOf(t, r); present {
			t.Errorf("%s: degradations = %+v, want the key absent on a clean reading", r.Action, got)
		}
		if strings.Contains(r.Rationale, "degrad") {
			t.Errorf("%s: rationale %q mentions a degradation on a clean reading", r.Action, r.Rationale)
		}
	}
}

// TestI12_EveryRowAfterDecodeCarriesTheDegradations pins each action the
// capture flow writes after Decode, one scenario per action family.
func TestI12_EveryRowAfterDecodeCarriesTheDegradations(t *testing.T) {
	const bad = `"title":"Cosa 2026-10-16"`
	cases := []struct {
		name     string
		response string
		want     []ports.DecisionAction
		lost     []degradationRow
	}{
		{"unclassifiable", `{"normalized_content":"algo","weight":0.5,"decay_rate":0.03,` + bad + `}`,
			[]ports.DecisionAction{ports.ActionCaptureUnclassifiable},
			// The missing type is the whole explanation of this row.
			[]degradationRow{{Field: "type", Reason: "absent"}, {Field: "title", Reason: "bad_format"}}},
		{"out of scope", `{"type":"out_of_scope","normalized_content":"el clima","weight":0.1,"decay_rate":0.5,` + bad + `}`,
			[]ports.DecisionAction{ports.ActionCaptureOutOfScope}, nil},
		{"timer armed", `{"type":"timer","normalized_content":"Sacar el pan","weight":0.1,"decay_rate":0.5,"due_at":"2026-10-14T08:40:00Z",` + bad + `}`,
			[]ports.DecisionAction{ports.ActionCaptureArmedTimer}, nil},
		{"timer refused", `{"type":"timer","normalized_content":"Sacar el pan","weight":0.1,"decay_rate":0.5,` + bad + `}`,
			[]ports.DecisionAction{ports.ActionCaptureArmRefused}, nil},
		{"dated event", `{"type":"event","normalized_content":"Dentista el 2026-10-16 a las 10:00","weight":0.7,"decay_rate":0.05,"event_at":"2026-10-16T10:00:00Z",` + bad + `}`,
			[]ports.DecisionAction{ports.ActionCaptureClassify}, nil},
		{"ambiguous person", `{"type":"task","normalized_content":"Llamar a Ana","weight":0.5,"decay_rate":0.03,"person_ref_status":"ambiguous",` + bad + `}`,
			[]ports.DecisionAction{ports.ActionCapturePersonRefAmbiguous}, nil},
		{"embedding failed", `{"type":"task","normalized_content":"Llamar a Ana","weight":0.5,"decay_rate":0.03,` + bad + `}`,
			[]ports.DecisionAction{ports.ActionCaptureEmbeddingFailed}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var embedErr error
			if tc.name == "embedding failed" {
				embedErr = errors.New("embedding provider down")
			}
			rows, _ := captureFull(t, "algo", tc.response, "", embedErr)
			for _, want := range tc.want {
				decisionByAction(t, rows, want)
			}
			everyRowCarries(t, rows, tc.lost...)
		})
	}
}

// TestI12_ChitchatRowsCarryTheDegradations: the chat task answers or fails, and
// either row is one the flow wrote after Decode.
func TestI12_ChitchatRowsCarryTheDegradations(t *testing.T) {
	const response = `{"type":"chitchat","normalized_content":"hola","weight":0.1,"decay_rate":0.5,"title":"Hola 2026-10-16"}`
	for chatCase, want := range map[string]ports.DecisionAction{
		"chat-hello-reply":     ports.ActionCaptureConversed,
		"chat-provider-outage": ports.ActionCaptureChatFailed,
	} {
		rows, _ := captureWithChat(t, "hola", response, chatCase)
		decisionByAction(t, rows, want)
		everyRowCarries(t, rows)
	}
}

// TestI12_CleanAmbiguousPersonCaptureRecordsNone and its siblings are the
// controls for the rows above: a clean reading leaves every row unchanged.
func TestI12_CleanAmbiguousPersonCaptureRecordsNone(t *testing.T) {
	rows := captureDecisions(t, "llamar a Ana", `{"type":"task","normalized_content":"Llamar a Ana","weight":0.5,"decay_rate":0.03,"title":"Llamar a Ana","person_ref_status":"ambiguous","language":"es"}`)
	decisionByAction(t, rows, ports.ActionCaptureUnitCreated)
	everyRowClean(t, rows)
}

func TestI12_CleanAskedCorrectionRecordsNone(t *testing.T) {
	rows := correctionDecisions(t, `{"type":"chitchat","normalized_content":"hola","weight":0.1,"decay_rate":0.03,"title":"Hola","language":"es"}`)
	decisionByAction(t, rows, ports.ActionCorrectionAmbiguous)
	everyRowClean(t, rows)
}

// TestI12_UnclassifiableCaptureNamesTheMissingType: with nothing else lost, the
// row says the type was absent, and says nothing more.
func TestI12_UnclassifiableCaptureNamesTheMissingType(t *testing.T) {
	rows, _ := captureAnyDecisions(t, "algo", `{"normalized_content":"algo","weight":0.5,"decay_rate":0.03}`)
	decisionByAction(t, rows, ports.ActionCaptureUnclassifiable)
	everyRowCarries(t, rows, degradationRow{Field: "type", Reason: "absent"})
}

// TestI12_CorrectionReminderRowsCarryTheDegradations: the row a correction
// writes when it moves an armed reminder is change-shaped for /ui/activity, and
// carries the list beside previous and next.
func TestI12_CorrectionReminderRowsCarryTheDegradations(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	original := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	const response = `{"type":"event","normalized_content":"Dentista hoy a las 10:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T10:00:00Z","title":"Dentista 2026-10-09","language":"es"}`
	u := unit.Unit{ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "Dentista el 2026-10-16 a las 10:00",
		EventAt: &original, Source: "cli", CreatedAt: now, UpdatedAt: now}
	f := newI29(t, now, u, response)
	f.arm(t, "t1", "dentist", time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC), u.Content, nil, nil)

	f.capture(t, "dentist")

	rows := f.rows(t, ports.ActionCorrectionReminderMoved)
	if len(rows) != 1 {
		t.Fatalf("%d correction.reminder.moved rows, want 1", len(rows))
	}
	everyRowCarries(t, rows)
}

// TestI12_RelationRowsOfACaptureCarryTheDegradations: the judge's rows for a
// capture (here relation.persisted) are written after the decode of that same
// message, so they carry its list; the nightly job's relation.* rows involve no
// reading and carry none.
func TestI12_RelationRowsOfACaptureCarryTheDegradations(t *testing.T) {
	const candidateID, judgeCase = "3527ca73-93c4-4688-a680-145243ce1e04", "relation-related-uncertain-band"
	const response = `{"type":"task","normalized_content":"Pick up the dry cleaning","weight":0.5,"decay_rate":0.03,"title":"Pick up 2026-10-16"}`
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	dir := i28Case(t, "degraded", "Pick up the dry cleaning", response)
	judge, err := os.ReadFile(filepath.Join(testdataLLMCasesDir(t), judgeCase+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, judgeCase+".json"), judge, 0o600); err != nil {
		t.Fatal(err)
	}

	units, decisions, embeddings := memrepo.NewUnits(), memrepo.NewDecisionLog(), memrepo.NewEmbeddings()
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	vec, err := embed.Embed(ctx, ports.EmbedRequest{Text: "Pick up the dry cleaning"})
	if err != nil {
		t.Fatal(err)
	}
	if err := units.Create(ctx, poolUnit(candidateID, "unrelated stored text")); err != nil {
		t.Fatal(err)
	}
	if err := embeddings.Put(ctx, ports.Embedding{UnitID: candidateID, Model: embedFakeModel, Vector: vec.Vector, At: now}); err != nil {
		t.Fatal(err)
	}
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, dir, "degraded", judgeCase)
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), decisions, llm, llm, llm, embed,
		brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)
	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: "Pick up the dry cleaning", Channel: "chat"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	rows, err := decisions.Since(ctx, time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	decisionByAction(t, rows, ports.ActionRelationPersisted)
	everyRowCarries(t, rows)
}

// TestI12_CheckInRowCarriesTheDegradations: an answer to a question nobody is
// asking writes capture.relation_checkin.unmatched after the decode, so it
// carries what that reading lost.
func TestI12_CheckInRowCarriesTheDegradations(t *testing.T) {
	const response = `{"type":"chitchat","normalized_content":"yes, they're related","weight":0.1,"decay_rate":0.5,"relation_outcome":"confirmed","title":"Yes 2026-10-16"}`
	rows, _ := captureWithChat(t, "yes, they're related", response, "chat-hello-reply")

	decisionByAction(t, rows, ports.ActionCaptureRelationCheckInUnmatched)
	everyRowCarries(t, rows)
}
