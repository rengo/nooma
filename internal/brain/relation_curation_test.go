package brain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

type curationClock struct{}

func (curationClock) Now() time.Time { return unitsNow }

type curationIDs struct{ n int }

func (g *curationIDs) New() string { g.n++; return "id-" + string(rune('0'+g.n)) }

type curationFixture struct {
	rels    *memrepo.Relations
	signals *memrepo.Signals
	svc     *brain.CurationService
}

func newCurationFixture(t *testing.T, log ports.DecisionLog) curationFixture {
	t.Helper()
	units := memrepo.NewUnits()
	seedUnitsUnit(t, units, "a", unit.StatusPool)
	seedUnitsUnit(t, units, "b", unit.StatusPool)
	rels := memrepo.NewRelations()
	if err := rels.Upsert(context.Background(), ports.Relation{
		ID: "r", FromUnitID: "a", ToUnitID: "b", Type: "same_topic", Strength: 0.4, Confidence: 0.35, CreatedBy: "system", CreatedAt: unitsNow,
	}); err != nil {
		t.Fatal(err)
	}
	signals := memrepo.NewSignals()
	svc := brain.NewCurationService(curationClock{}, &curationIDs{}, units, rels, signals, log, memrepo.NewPendingQuestions())
	return curationFixture{rels: rels, signals: signals, svc: svc}
}

// A delete whose pre-image row cannot be written does not happen: nothing
// else would remember the relation (ADR-0016's order), and no signal is
// left for a rejection that did not take place.
func TestCurationReject_AFailedPreImageDeletesNothing(t *testing.T) {
	f := newCurationFixture(t, memrepo.NewFailingDecisionLog(errors.New("log down")))

	err := f.svc.Reject(context.Background(), "r")
	if err == nil || errors.Is(err, brain.ErrWriteLanded) {
		t.Fatalf("Reject = %v, want a plain failure", err)
	}
	if _, err := f.rels.ByID(context.Background(), "r"); err != nil {
		t.Errorf("the relation was deleted without its pre-image: %v", err)
	}
	if sigs, _ := f.signals.Since(context.Background(), time.Time{}, 10); len(sigs) != 0 {
		t.Errorf("signals = %+v, want none", sigs)
	}
}

// A confirm whose row cannot be written has still landed: the raise and its
// signal stand, and the error says only the record is missing.
func TestCurationConfirm_AFailedRecordIsAWriteThatLanded(t *testing.T) {
	f := newCurationFixture(t, memrepo.NewFailingDecisionLog(errors.New("log down")))

	err := f.svc.Confirm(context.Background(), "r")
	var landed *brain.WriteLandedError
	if !errors.As(err, &landed) || !landed.Record || landed.Signal {
		t.Fatalf("Confirm = %v, want a WriteLandedError naming the record only", err)
	}
	if rel, _ := f.rels.ByID(context.Background(), "r"); rel.Confidence != 0.5 {
		t.Errorf("confidence = %v, want the raise to stand at 0.5", rel.Confidence)
	}
	if sigs, _ := f.signals.Since(context.Background(), time.Time{}, 10); len(sigs) != 1 || sigs[0].Type != ports.SignalRelationConfirm {
		t.Errorf("signals = %+v, want one relation_confirm", sigs)
	}
}
