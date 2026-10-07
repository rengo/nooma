// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/focus"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/core/weight"
	"github.com/rengo/nooma/test/support/memrepo"
)

// hysteresisVault is the FX-H fixture of design m4c §6, built from public
// surface only: six task fillers F1..F6 that outrank everything contested,
// the incumbent-to-be A (weight 1.0) and the challenger B (0.99), so A and B
// contest slot 7 against slot 8 of the task focus. Every unit carries the
// same timestamps, so a different clock instant scales every score together
// and cannot reorder them. With no relations and no age a unit's score is
// exactly its weight.
type hysteresisVault struct {
	units *memrepo.Units
	cfg   *memrepo.Config
	rels  *memrepo.Relations
	now   time.Time
}

func newHysteresisVault(t *testing.T, now time.Time) *hysteresisVault {
	t.Helper()
	v := &hysteresisVault{units: memrepo.NewUnits(), cfg: memrepo.NewConfig(), rels: memrepo.NewRelations(), now: now}
	for i := 1; i <= 6; i++ {
		v.seed(t, fmt.Sprintf("F%d", i), 10)
	}
	v.seed(t, "A", 1.0)
	v.seed(t, "B", 0.99)
	return v
}

func (v *hysteresisVault) seed(t *testing.T, id string, w float64) {
	t.Helper()
	if err := v.units.Create(context.Background(), unit.Unit{
		ID: id, Type: unit.TypeTask, Status: unit.StatusPool, Content: id,
		Weight: w, LastTouchedAt: v.now, CreatedAt: v.now, UpdatedAt: v.now,
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func (v *hysteresisVault) setWeight(t *testing.T, id string, w float64) {
	t.Helper()
	if err := v.units.ApplyBoosts(context.Background(), []weight.Boost{{UnitID: id, Weight: w, LastTouchedAt: v.now}}, v.now); err != nil {
		t.Fatalf("set weight of %s: %v", id, err)
	}
}

func (v *hysteresisVault) keeper() *brain.FocusKeeper {
	return brain.NewFocusKeeper(v.units, v.cfg, v.rels)
}

// taskFocus serves one Today request through a TodayService sharing keeper
// and returns the task focus's member ids.
func (v *hysteresisVault) taskFocus(t *testing.T, keeper *brain.FocusKeeper) []string {
	t.Helper()
	svc := brain.NewTodayService(fixedClock{now: v.now}, v.units, v.cfg, memrepo.NewState(), memrepo.NewTriggers(), memrepo.NewPendingQuestions(), memrepo.NewDecisionLog(), keeper)
	today, err := svc.Today(context.Background())
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	var ids []string
	for _, f := range today.Focuses {
		if f.Kind != focus.KindTask {
			continue
		}
		for _, m := range f.Members {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// assertTaskFocus checks the focus is the six fillers followed by last.
func assertTaskFocus(t *testing.T, what string, got []string, last string) {
	t.Helper()
	want := []string{"F1", "F2", "F3", "F4", "F5", "F6", last}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// TestI01_IncumbentDoesNotSurviveAFreshService is I01's behavioural half for
// the previous focus (docs/02-cognitive-core.md §3, docs/06-harness.md §4):
// the incumbent lives in one keeper's memory and nowhere else, so a service
// built over a freshly constructed keeper starts with none, over the very
// same vault.
//
// The first keeper must actually HOLD something for the claim to mean
// anything: it is seeded with A in slot 7, B then rises inside A's margin,
// and it must keep A. The second keeper, built over the same stores, has
// nothing to hold and ranks B by plain score: the accepted restart cost.
func TestI01_IncumbentDoesNotSurviveAFreshService(t *testing.T) {
	v := newHysteresisVault(t, time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))

	first := v.keeper()
	assertTaskFocus(t, "first keeper, seeding request", v.taskFocus(t, first), "A")

	v.setWeight(t, "B", 1.03) // inside A's 5% margin: 1.03 < 1.0*1.05
	assertTaskFocus(t, "first keeper, B inside the margin", v.taskFocus(t, first), "A")

	assertTaskFocus(t, "fresh keeper over the same vault", v.taskFocus(t, v.keeper()), "B")
}
