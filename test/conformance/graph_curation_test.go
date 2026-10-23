// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/httpapi"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/internal/ui"
	"github.com/rengo/nooma/test/support/memrepo"
)

var curationAt = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

// curationVault is the graph curation's whole stack over memrepos: the real
// handler, GraphService and CurationService, with the relation row r-near
// (centre -> near, confidence 0.35, uncertain under the default band) and
// r-gone (centre -> an archived unit), and a question queued about r-near.
type curationVault struct {
	units     *memrepo.Units
	rels      *curationRelations
	signals   *memrepo.Signals
	log       *memrepo.DecisionLog
	questions *memrepo.PendingQuestions
	h         http.Handler
}

// curationRelations records, at the moment Delete is called, what the log
// and the signals already hold: I10's order is observed, not inferred.
type curationRelations struct {
	*memrepo.Relations
	log            *memrepo.DecisionLog
	signals        *memrepo.Signals
	rowsAtDelete   int
	rejectAtDelete bool
}

func (r *curationRelations) Delete(ctx context.Context, id string) error {
	rows, _ := r.log.Since(ctx, time.Time{}, 100)
	r.rowsAtDelete = len(rows)
	sigs, _ := r.signals.Since(ctx, time.Time{}, 100)
	for _, s := range sigs {
		if s.Type == ports.SignalRelationReject {
			r.rejectAtDelete = true
		}
	}
	return r.Relations.Delete(ctx, id)
}

func newCurationVault(t *testing.T) *curationVault {
	t.Helper()
	ctx := context.Background()
	v := &curationVault{
		units: memrepo.NewUnits(), signals: memrepo.NewSignals(),
		log: memrepo.NewDecisionLog(), questions: memrepo.NewPendingQuestions(),
	}
	v.rels = &curationRelations{Relations: memrepo.NewRelations(), log: v.log, signals: v.signals}
	for _, u := range []unit.Unit{
		{ID: "centre", Status: unit.StatusPool, Content: "the centre"},
		{ID: "near", Status: unit.StatusPool, Content: "a live neighbour"},
		{ID: "gone", Status: unit.StatusArchived, Content: "an archived unit"},
	} {
		u.Type, u.CreatedAt, u.UpdatedAt, u.LastTouchedAt = unit.TypeKnowledge, curationAt, curationAt, curationAt
		if err := v.units.Create(ctx, u); err != nil {
			t.Fatalf("seed unit %s: %v", u.ID, err)
		}
	}
	for _, r := range []ports.Relation{
		{ID: "r-near", FromUnitID: "centre", ToUnitID: "near", Type: "same_topic", Strength: 0.4, Confidence: 0.35, CreatedBy: "consolidation", CreatedAt: curationAt},
		{ID: "r-gone", FromUnitID: "centre", ToUnitID: "gone", Type: "same_topic", Strength: 0.9, Confidence: 0.35, CreatedBy: "consolidation", CreatedAt: curationAt},
	} {
		if err := v.rels.Upsert(ctx, r); err != nil {
			t.Fatalf("seed relation %s: %v", r.ID, err)
		}
	}
	v.questions.EnsureRelation(t, "r-near", "same_topic", "centre", "near", "the centre", "a live neighbour")
	if err := v.questions.Create(ctx, ports.PendingQuestion{ID: "q-near", Kind: ports.QuestionKindRelation, RelationID: "r-near", CreatedAt: curationAt}); err != nil {
		t.Fatalf("seed question: %v", err)
	}

	clock := fixedClock{now: curationAt}
	curation := brain.NewCurationService(clock, &counterIDs{}, v.units, v.rels, v.signals, v.log, v.questions)
	v.h = httpapi.Handler(httpapi.Deps{Version: "test", UI: ui.New(ui.Deps{
		Graph: brain.NewGraphService(v.units, v.rels), Curation: curation,
	})})
	return v
}

func (v *curationVault) post(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(url.Values{"unit": {"centre"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	v.h.ServeHTTP(rec, req)
	return rec
}

func (v *curationVault) rows(t *testing.T) []ports.Decision {
	t.Helper()
	rows, err := v.log.Since(context.Background(), time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func (v *curationVault) signalsOf(t *testing.T) []ports.Signal {
	t.Helper()
	sigs, err := v.signals.Since(context.Background(), time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	return sigs
}

// decodeCurationRow reads a curation row's context.
func decodeCurationRow(t *testing.T, d ports.Decision) (top map[string]any, previous, next map[string]any) {
	t.Helper()
	if err := json.Unmarshal(d.Context, &top); err != nil {
		t.Fatalf("row %s context %s: %v", d.Action, d.Context, err)
	}
	previous, _ = top["previous"].(map[string]any)
	next, _ = top["next"].(map[string]any)
	return top, previous, next
}

// TestI10_GraphDeleteRecordsThePreImageThenTheSignalThenDeletes: deleting a
// relation from the graph is the chat's reject (doc 02 §4, I10): the
// relation_reject signal exists before the delete, and so does the
// relation.rejected row carrying the whole row as it was, since nothing
// else will remember it. The pending question about it closes, and the
// graph is redrawn without it.
func TestI10_GraphDeleteRecordsThePreImageThenTheSignalThenDeletes(t *testing.T) {
	v := newCurationVault(t)

	rec := v.post(t, "/ui/graph/relations/r-near/reject")
	if rec.Code != http.StatusOK {
		t.Fatalf("reject = %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "<h2>GRAPH</h2>") || strings.Contains(body, `data-relation-id="r-near"`) {
		t.Errorf("the answer is not the graph redrawn without r-near:\n%s", body)
	}

	if _, err := v.rels.ByID(context.Background(), "r-near"); !errors.Is(err, ports.ErrRelationNotFound) {
		t.Fatalf("r-near still exists after reject: %v", err)
	}
	if !v.rels.rejectAtDelete {
		t.Error("Delete ran before the relation_reject signal was recorded (I10)")
	}
	if v.rels.rowsAtDelete != 1 {
		t.Errorf("Delete ran with %d decision row(s) recorded, want the pre-image row first", v.rels.rowsAtDelete)
	}

	rows := v.rows(t)
	if len(rows) != 1 || rows[0].Action != ports.ActionRelationRejected {
		t.Fatalf("decision rows = %+v, want one %s", rows, ports.ActionRelationRejected)
	}
	top, previous, next := decodeCurationRow(t, rows[0])
	if top["relation_id"] != "r-near" || top["from_unit_id"] != "centre" || top["to_unit_id"] != "near" {
		t.Errorf("row context does not name the relation and its endpoints: %s", rows[0].Context)
	}
	wantPrev := map[string]any{"type": "same_topic", "strength": 0.4, "confidence": 0.35, "created_by": "consolidation"}
	if !reflect.DeepEqual(previous, wantPrev) {
		t.Errorf("pre-image = %v, want %v", previous, wantPrev)
	}
	for _, k := range []string{"type", "strength", "confidence", "created_by"} {
		if val, ok := next[k]; !ok || val != nil {
			t.Errorf("next[%q] = %v (present %v), want an explicit null: the row is gone", k, val, ok)
		}
	}

	sigs := v.signalsOf(t)
	if len(sigs) != 1 || sigs[0].Type != ports.SignalRelationReject || sigs[0].Valence != ports.ValenceNegative ||
		sigs[0].TargetID == nil || *sigs[0].TargetID != "r-near" {
		t.Errorf("signals = %+v, want one negative relation_reject on r-near", sigs)
	}

	// The memrepo keeps showing a question whose relation is gone, so this
	// sees only what the service itself closed.
	if open, _ := v.questions.Unasked(context.Background()); len(open) != 0 {
		t.Errorf("the question about the deleted relation is still queued: %+v", open)
	}
}

// TestGraphConfirm_RaisesConfidenceEmitsTheSignalAndLogsBeforeAndAfter: a
// confirm from the graph is the chat's confirm (doc 02 §4): confidence
// rises to the type's surface threshold, relation_confirm is emitted, the
// relation.confirmed row carries the confidence before and after, and the
// question queued about it closes so the digest never asks what the user
// already settled.
func TestGraphConfirm_RaisesConfidenceEmitsTheSignalAndLogsBeforeAndAfter(t *testing.T) {
	v := newCurationVault(t)

	rec := v.post(t, "/ui/graph/relations/r-near/confirm")
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm = %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `data-relation-id="r-near"`) || !strings.Contains(body, "0.50") {
		t.Errorf("the answer is not the graph redrawn with r-near at 0.50:\n%s", body)
	}

	rel, err := v.rels.ByID(context.Background(), "r-near")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Confidence != 0.5 || rel.Strength != 0.4 || rel.CreatedBy != "consolidation" {
		t.Errorf("relation after confirm = %+v, want confidence 0.5 and nothing else changed", rel)
	}

	rows := v.rows(t)
	if len(rows) != 1 || rows[0].Action != ports.ActionRelationConfirmed {
		t.Fatalf("decision rows = %+v, want one %s", rows, ports.ActionRelationConfirmed)
	}
	top, previous, next := decodeCurationRow(t, rows[0])
	if top["relation_id"] != "r-near" || top["from_unit_id"] != "centre" {
		t.Errorf("row context does not name the relation: %s", rows[0].Context)
	}
	if previous["confidence"] != 0.35 || next["confidence"] != 0.5 {
		t.Errorf("confidence previous/next = %v/%v, want 0.35/0.5", previous["confidence"], next["confidence"])
	}

	sigs := v.signalsOf(t)
	if len(sigs) != 1 || sigs[0].Type != ports.SignalRelationConfirm || sigs[0].Valence != ports.ValencePositive ||
		sigs[0].TargetID == nil || *sigs[0].TargetID != "r-near" {
		t.Errorf("signals = %+v, want one positive relation_confirm on r-near", sigs)
	}

	if open, _ := v.questions.Unasked(context.Background()); len(open) != 0 {
		t.Errorf("the question about the confirmed relation is still queued: %+v", open)
	}
}

// TestI02_GraphCurationRefusesARelationToAUnitThatIsNotLive: the graph
// never draws r-gone, and a hand-made POST cannot curate it either. Nothing
// is written, and the relation is left as it was.
func TestI02_GraphCurationRefusesARelationToAUnitThatIsNotLive(t *testing.T) {
	for _, action := range []string{"confirm", "reject"} {
		v := newCurationVault(t)
		rec := v.post(t, "/ui/graph/relations/r-gone/"+action)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s r-gone = %d, want 404", action, rec.Code)
		}
		rec = v.post(t, "/ui/graph/relations/r-absent/"+action)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s r-absent = %d, want 404", action, rec.Code)
		}
		if rel, err := v.rels.ByID(context.Background(), "r-gone"); err != nil || rel.Confidence != 0.35 {
			t.Errorf("%s changed r-gone: %+v, %v", action, rel, err)
		}
		if n := len(v.rows(t)) + len(v.signalsOf(t)); n != 0 {
			t.Errorf("%s of a relation to an archived unit wrote %d row(s) or signal(s)", action, n)
		}
	}
}
