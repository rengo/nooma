// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI04_TimerNeverPersistsAUnit is I04's own
// conformance test (docs/06-harness.md §4).
//
// docs/02-cognitive-core.md §8 states in bold "A timer is NEVER a unit: no
// weight, no decay, no graph, no belief derivation." Until this change the
// invariant held for the emptiest of reasons — capture refused timers
// outright, and internal/ports declared no repository through which one
// could have been written. Now capture arms them, so the invariant is
// under real load for the first time and this test asserts it against what
// was actually persisted.
//
// **Timers only.** This test was named for two kinds and asserted the
// MUST below for both, but I04's own row (docs/06-harness.md §4) reads "A
// timer is never a unit", and doc 02 §8 reads "A timer is NEVER a unit".
// Neither mentions a recurring reminder. The second case came from §5's
// broader "a capture that arms something persists no unit at all" — a
// different claim, borrowing this invariant's name for authority it never
// granted, and one doc 02 now states the other way: a birthday is memory
// whose nudge repeats. Narrowing this back to timers restores it to the
// invariant it is named for; the recurring case lives in
// arming_capture_keeps_its_unit_test.go, asserting the opposite.
//
// A timer classification MUST leave:
//   - zero units rows,
//   - exactly one row in the table its armament belongs to and zero in the
//     other, and
//   - a CaptureResult naming what was armed (Outcome: OutcomeArmed, Armed
//     carrying the armament, the created id, and the fire instant).
func TestI04_TimerNeverPersistsAUnit(t *testing.T) {
	tests := []struct {
		name         string
		llmCase      string
		wantKind     classify.Kind
		wantArmament prospection.Armament
		wantTimers   int
		wantTriggers int
	}{
		{
			name:         "timer",
			llmCase:      "classify-timer-armed-bread-in-the-oven",
			wantKind:     classify.KindTimer,
			wantArmament: prospection.ArmTimer,
			wantTimers:   1,
			wantTriggers: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
			units := memrepo.NewUnits()
			decisions := memrepo.NewDecisionLog()
			embeddings := memrepo.NewEmbeddings()
			lexical := memrepo.NewLexical()
			relations := memrepo.NewRelations()
			triggers := memrepo.NewTriggers()
			timers := memrepo.NewTimers()
			llm := fakeprovider.New(t, testdataLLMCasesDir(t), tt.llmCase)
			embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

			idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
			if err != nil {
				t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
			}
			svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, lexical, relations, decisions, llm, llm, embed, brain.NewIndex(idx), memrepo.NewSignals(), triggers, timers)

			result, err := svc.Capture(ctx, brain.CaptureInput{
				Text:    "irrelevant — the fake replays by case id, not prompt text",
				Channel: "chat",
			})
			if err != nil {
				t.Fatalf("Capture error = %v, want nil", err)
			}

			// The invariant itself.
			if got := units.Count(); got != 0 {
				t.Fatalf("units.Count() = %d, want 0 — a %s classification must never persist a unit (doc 02 §8)", got, tt.wantKind)
			}

			// Exactly one row, in exactly one of the two tables. Asserting
			// both counts, not just the one expected to be 1, is what
			// catches an arming that wrote to both.
			if got := timers.Count(); got != tt.wantTimers {
				t.Errorf("timers.Count() = %d, want %d", got, tt.wantTimers)
			}
			if got := triggers.Count(); got != tt.wantTriggers {
				t.Errorf("triggers.Count() = %d, want %d", got, tt.wantTriggers)
			}

			if result.Outcome != brain.OutcomeArmed {
				t.Fatalf("CaptureResult.Outcome = %q, want %q", result.Outcome, brain.OutcomeArmed)
			}
			if result.Armed == nil {
				t.Fatal("CaptureResult.Armed = nil, want a non-nil Armed naming what was scheduled")
			}
			if result.Armed.What != tt.wantArmament {
				t.Errorf("Armed.What = %q, want %q", result.Armed.What, tt.wantArmament)
			}
			if result.Armed.ID == "" {
				t.Error("Armed.ID is empty — a caller must be able to name the row it just scheduled")
			}
			if !result.Armed.FireAt.After(now) {
				t.Errorf("Armed.FireAt = %s, want an instant after the capture's own %s", result.Armed.FireAt, now)
			}
		})
	}
}

// TestI04_NoTimerPortMethodCanCarryAUnit is the structural half, and it is
// the half that cannot be entered from underneath: no method on
// ports.TimerRepo takes or returns a unit.Unit in any form, so a timer
// cannot become a unit through the port at all, whatever the pipeline
// above it does.
//
// The scan is over the interface's own method set, and over every
// parameter and result type of every method — never a hand-checked list of
// the four method names. A fifth method added tomorrow is covered without
// a test edit; a list would only ever cover what someone remembered to add
// to it.
//
// This sub-test already passed before arming existed (ports.TimerRepo has
// never had such a method), which is disclosed rather than counted as
// proof of this change. It is here to stay true.
func TestI04_NoTimerPortMethodCanCarryAUnit(t *testing.T) {
	timerRepo := reflect.TypeOf((*ports.TimerRepo)(nil)).Elem()
	unitType := reflect.TypeOf(unit.Unit{})

	if timerRepo.NumMethod() == 0 {
		t.Fatal("ports.TimerRepo declares zero methods — nothing to check yet")
	}

	// carriesUnit reports whether t is unit.Unit, or any pointer, slice,
	// array, map or channel that reaches it — so *unit.Unit, []unit.Unit
	// and map[string]unit.Unit are all caught, not just the bare type.
	var carriesUnit func(reflect.Type) bool
	carriesUnit = func(t reflect.Type) bool {
		if t == unitType {
			return true
		}
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
			return carriesUnit(t.Elem())
		case reflect.Map:
			return carriesUnit(t.Key()) || carriesUnit(t.Elem())
		default:
			return false
		}
	}

	for i := 0; i < timerRepo.NumMethod(); i++ {
		m := timerRepo.Method(i)
		for j := 0; j < m.Type.NumIn(); j++ {
			if carriesUnit(m.Type.In(j)) {
				t.Errorf("ports.TimerRepo.%s takes %s: a timer is never a unit (doc 02 §8)", m.Name, m.Type.In(j))
			}
		}
		for j := 0; j < m.Type.NumOut(); j++ {
			if carriesUnit(m.Type.Out(j)) {
				t.Errorf("ports.TimerRepo.%s returns %s: a timer is never a unit (doc 02 §8)", m.Name, m.Type.Out(j))
			}
		}
	}

	// The same claim about the package, not just the interface: nothing
	// named Timer in internal/ports mentions a unit id either. timers has
	// no unit_id column (migration 0001:62-70), and ports.Timer having one
	// would be the first step toward the invariant failing at the schema
	// level rather than at this port.
	timerStruct := reflect.TypeOf(ports.Timer{})
	for i := 0; i < timerStruct.NumField(); i++ {
		if name := timerStruct.Field(i).Name; strings.Contains(strings.ToLower(name), "unit") {
			t.Errorf("ports.Timer declares a field named %s — timers carries no unit_id", name)
		}
	}
}
