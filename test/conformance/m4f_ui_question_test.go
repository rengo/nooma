// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/httpapi"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
)

// m4f's UI half, Today's question (doc 02 §4): the question Today previews is
// answered by its id through the question store (I09), never by guessing.

// todayStub renders the question it is given as Today's.
type todayStub struct{ q *ports.RelationQuestion }

func (s todayStub) Today(context.Context) (brain.Today, error) {
	var t brain.Today
	t.Digest.Question = s.q
	return t, nil
}

// newQuestionVault is the curation vault with Today wired, answering through
// the question routes.
func newQuestionVault(t *testing.T, asked ...bool) *curationVault {
	t.Helper()
	v := newCurationVault(t, asked...)
	curation := brain.NewCurationService(fixedClock{now: curationAt}, &counterIDs{}, v.units, v.rels, v.signals, v.log, v.questions)
	v.h = httpapi.Handler(httpapi.Deps{Version: "test", UI: ui.New(ui.Deps{
		Today: todayStub{}, Questions: curation,
	})})
	return v
}

// TestI09_TodaysQuestionCarriesItsIDToBothAnswers: the question Today
// previews is answered by its id, from two forms, never by a relation the
// page would have to name.
func TestI09_TodaysQuestionCarriesItsIDToBothAnswers(t *testing.T) {
	q := &ports.RelationQuestion{ID: "q-near", RelationID: "r-near", FromContent: "the centre", ToContent: "a live neighbour"}
	h := httpapi.Handler(httpapi.Deps{Version: "test", UI: ui.New(ui.Deps{Today: todayStub{q: q}})})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui", nil))
	body := rec.Body.String()
	for _, want := range []string{`action="/ui/questions/q-near/confirm"`, `action="/ui/questions/q-near/reject"`, "the centre", "a live neighbour"} {
		if !strings.Contains(body, want) {
			t.Errorf("Today lacks %q:\n%s", want, body)
		}
	}
}

// TestI09_AYesFromTodayConfirmsTheRelationItsQuestionNames: a yes resolves
// the question by its id through the store: the relation it names rises to
// its type's surface threshold, relation_confirm is emitted, the question
// closes confirmed, and one relation.confirmed row names the question.
func TestI09_AYesFromTodayConfirmsTheRelationItsQuestionNames(t *testing.T) {
	for name, asked := range map[string]bool{"queued": false, "asked by a digest": true} {
		t.Run(name, func(t *testing.T) {
			v := newQuestionVault(t, asked)

			rec := v.post(t, "/ui/questions/q-near/confirm")
			if rec.Code != http.StatusOK {
				t.Fatalf("confirm = %d: %s", rec.Code, rec.Body.String())
			}
			if body := rec.Body.String(); !strings.Contains(body, "Link confirmed.") {
				t.Errorf("Today should say the link was confirmed:\n%s", body)
			}
			rel, err := v.rels.ByID(context.Background(), "r-near")
			if err != nil || rel.Confidence != 0.5 {
				t.Fatalf("r-near after the yes = %+v, %v; want confidence 0.5", rel, err)
			}
			if got := v.questions.closed["q-near"]; got != ports.QuestionConfirmed {
				t.Errorf("q-near closed as %q, want confirmed", got)
			}
			sigs := v.signalsOf(t)
			if len(sigs) != 1 || sigs[0].Type != ports.SignalRelationConfirm || *sigs[0].TargetID != "r-near" {
				t.Errorf("signals = %+v, want one relation_confirm on r-near", sigs)
			}
			rows := v.rows(t)
			if len(rows) != 1 || rows[0].Action != ports.ActionRelationConfirmed {
				t.Fatalf("decision rows = %+v, want one %s", rows, ports.ActionRelationConfirmed)
			}
			top, _, _ := decodeCurationRow(t, rows[0])
			if top["question_id"] != "q-near" || !strings.Contains(rows[0].Rationale, "Today") {
				t.Errorf("row = %s / %q, want it to name q-near and Today", rows[0].Context, rows[0].Rationale)
			}
		})
	}
}

// TestI09_ANoFromTodayDeletesTheRelationAfterItsSignal: a no is the chat's
// reject (I10): the pre-image row, then the signal, then the delete, and the
// question closes rejected.
func TestI09_ANoFromTodayDeletesTheRelationAfterItsSignal(t *testing.T) {
	v := newQuestionVault(t)

	rec := v.post(t, "/ui/questions/q-near/reject")
	if rec.Code != http.StatusOK {
		t.Fatalf("reject = %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Link deleted.") {
		t.Errorf("Today should say the link was deleted:\n%s", body)
	}
	if _, err := v.rels.ByID(context.Background(), "r-near"); !errors.Is(err, ports.ErrRelationNotFound) {
		t.Fatalf("r-near still exists after the no: %v", err)
	}
	if !v.rels.rejectAtDelete || v.rels.rowsAtDelete != 1 {
		t.Errorf("Delete ran before the pre-image row (%d rows) or the signal (%v), I10", v.rels.rowsAtDelete, v.rels.rejectAtDelete)
	}
	if got := v.questions.closed["q-near"]; got != ports.QuestionRejected {
		t.Errorf("q-near closed as %q, want rejected", got)
	}
	rows := v.rows(t)
	if len(rows) != 1 || rows[0].Action != ports.ActionRelationRejected {
		t.Fatalf("decision rows = %+v, want one %s", rows, ports.ActionRelationRejected)
	}
	if top, _, _ := decodeCurationRow(t, rows[0]); top["question_id"] != "q-near" {
		t.Errorf("row context %s does not name q-near", rows[0].Context)
	}
}

// TestI09_AnAnswerActsOnTheRelationItsQuestionNames: with two questions
// waiting about two relations, an answer to the second by its id acts on the
// second's relation alone; the first question, queued ahead of it, and its
// relation are untouched. Acting on whichever question waits first would be
// the guessing I09 forbids.
func TestI09_AnAnswerActsOnTheRelationItsQuestionNames(t *testing.T) {
	for _, answer := range []string{"confirm", "reject"} {
		t.Run(answer, func(t *testing.T) {
			v := newQuestionVault(t)
			ctx := context.Background()
			far := unit.Unit{ID: "far", Type: unit.TypeKnowledge, Status: unit.StatusPool, Content: "a second neighbour",
				CreatedAt: curationAt, UpdatedAt: curationAt, LastTouchedAt: curationAt}
			if err := v.units.Create(ctx, far); err != nil {
				t.Fatal(err)
			}
			if err := v.rels.Upsert(ctx, ports.Relation{ID: "r-far", FromUnitID: "centre", ToUnitID: "far", Type: "same_topic",
				Strength: 0.4, Confidence: 0.35, CreatedBy: "consolidation", CreatedAt: curationAt}); err != nil {
				t.Fatal(err)
			}
			v.questions.EnsureRelation(t, "r-far", "same_topic", "centre", "far", "the centre", "a second neighbour")
			// Created after q-near, so q-near is the one waiting first.
			if err := v.questions.Create(ctx, ports.PendingQuestion{ID: "q-far", Kind: ports.QuestionKindRelation, RelationID: "r-far", CreatedAt: curationAt.Add(time.Minute)}); err != nil {
				t.Fatal(err)
			}
			if queued, _ := v.questions.Unasked(ctx); len(queued) != 2 || queued[0].ID != "q-near" {
				t.Fatalf("queued = %+v, want q-near first, then q-far", queued)
			}

			if rec := v.post(t, "/ui/questions/q-far/"+answer); rec.Code != http.StatusOK {
				t.Fatalf("%s q-far = %d: %s", answer, rec.Code, rec.Body.String())
			}

			farRel, farErr := v.rels.ByID(ctx, "r-far")
			switch answer {
			case "confirm":
				if farErr != nil || farRel.Confidence != 0.5 {
					t.Errorf("r-far = %+v, %v; want it confirmed at 0.5", farRel, farErr)
				}
			case "reject":
				if !errors.Is(farErr, ports.ErrRelationNotFound) {
					t.Errorf("r-far after the no: %v, want it deleted", farErr)
				}
			}
			if near, err := v.rels.ByID(ctx, "r-near"); err != nil || near.Confidence != 0.35 {
				t.Errorf("r-near = %+v, %v; want the first question's relation untouched at 0.35", near, err)
			}
			if _, closed := v.questions.closed["q-near"]; closed {
				t.Error("q-near was closed by an answer to q-far")
			}
			if _, closed := v.questions.closed["q-far"]; !closed {
				t.Error("q-far, the question answered, is still open")
			}
			rows := v.rows(t)
			if len(rows) != 1 {
				t.Fatalf("decision rows = %+v, want one", rows)
			}
			if top, _, _ := decodeCurationRow(t, rows[0]); top["relation_id"] != "r-far" || top["question_id"] != "q-far" {
				t.Errorf("row context %s, want it about r-far, answering q-far", rows[0].Context)
			}
			if sigs := v.signalsOf(t); len(sigs) != 1 || sigs[0].TargetID == nil || *sigs[0].TargetID != "r-far" {
				t.Errorf("signals = %+v, want one, on r-far", sigs)
			}
		})
	}
}

// TestI09_AnAnswerToAQuestionNoLongerOpenChangesNothing: an id the store does
// not hold open — answered already, never asked about, or about a relation
// gone or dormant (below its type's persist bar, I49) — is refused with the
// notice, never a 500, and nothing is written: the relation is never guessed
// from the page.
func TestI09_AnAnswerToAQuestionNoLongerOpenChangesNothing(t *testing.T) {
	for name, path := range map[string]string{
		"answered": "/ui/questions/q-near/reject", "unknown": "/ui/questions/q-none/confirm",
		"dormant": "/ui/questions/q-near/reject", "gone": "/ui/questions/q-near/confirm",
	} {
		t.Run(name, func(t *testing.T) {
			v := newQuestionVault(t)
			ctx := context.Background()
			switch name {
			case "answered":
				if err := v.questions.PendingQuestions.Confirm(ctx, "q-near", curationAt); err != nil {
					t.Fatal(err)
				}
			case "dormant":
				// The bar rises over r-near's 0.35.
				v.rels.SeedThreshold(t, "same_topic", 0.40, 0.55)
			case "gone":
				if err := v.rels.Relations.Delete(ctx, "r-near"); err != nil {
					t.Fatal(err)
				}
			}
			rec := v.post(t, path)
			if rec.Code != http.StatusConflict {
				t.Errorf("POST %s = %d, want 409", path, rec.Code)
			}
			if body := rec.Body.String(); !strings.Contains(body, "That question is no longer open") {
				t.Errorf("Today should say the question is no longer open:\n%s", body)
			}
			if rel, err := v.rels.ByID(ctx, "r-near"); name != "gone" && (err != nil || rel.Confidence != 0.35) {
				t.Errorf("r-near = %+v, %v; want it untouched at 0.35", rel, err)
			}
			if rows, sigs := v.rows(t), v.signalsOf(t); len(rows) != 0 || len(sigs) != 0 {
				t.Errorf("rows %+v, signals %+v; want nothing written", rows, sigs)
			}
		})
	}
}
