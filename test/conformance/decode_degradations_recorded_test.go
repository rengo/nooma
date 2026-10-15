// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
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
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	decisions, embeddings := memrepo.NewDecisionLog(), memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, i28Case(t, "degraded", message, response), "degraded")
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, memrepo.NewUnits(), embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), decisions, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel),
		brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)
	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: message, Channel: "chat"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	rows, err := decisions.Since(ctx, time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	return rows
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
