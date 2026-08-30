// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestI18_ArmingPersistsTheRightInstant is I18's persistence half.
//
// I18 at decision time — that Arm reads due_at for a timer and event_at for
// an event — is a property of Arm's own body, already asserted in
// internal/core/prospection. What is asserted here is what reached the
// row: the fire_at a capture actually wrote.
//
// Both fixtures carry a due_at AND an event_at, and the capture's own
// instant is a third value distinct from both. That is the whole design of
// this test: with two instants in play, a swapped assignment has a fifty
// per cent chance of looking right, and with the capture instant as a third
// it is caught whichever way it slipped. A fixture carrying one date proves
// nothing about which field was read.
func TestI18_ArmingPersistsTheRightInstant(t *testing.T) {
	var (
		now = time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
		// The two instants both fixtures carry, verbatim from their JSON.
		dueAt   = time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
		eventAt = time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)
	)

	t.Run("a timer fires at due_at, never event_at and never the capture instant", func(t *testing.T) {
		timers, triggers, result := captureAndArm(t, now, "classify-timer-three-distinct-instants")

		if result.Armed == nil || result.Armed.What != prospection.ArmTimer {
			t.Fatalf("Armed = %+v, want a timer", result.Armed)
		}
		if got := triggers.Count(); got != 0 {
			t.Fatalf("triggers.Count() = %d, want 0", got)
		}

		rows := timers.All()
		if len(rows) != 1 {
			t.Fatalf("timers.All() returned %d rows, want 1", len(rows))
		}
		assertInstant(t, "timers.fire_at", rows[0].FireAt, dueAt, map[string]time.Time{
			"event_at":            eventAt,
			"the capture instant": now,
		})
		if !rows[0].CreatedAt.Equal(now) {
			t.Errorf("timers.created_at = %s, want the capture's own instant %s", rows[0].CreatedAt, now)
		}
	})

	t.Run("a dated event fires ahead of event_at, never at due_at and never at the capture instant", func(t *testing.T) {
		timers, triggers, result := captureAndArm(t, now, "classify-event-three-distinct-instants")

		if result.Armed == nil || result.Armed.What != prospection.ArmTrigger {
			t.Fatalf("Armed = %+v, want a trigger", result.Armed)
		}
		if got := timers.Count(); got != 0 {
			t.Fatalf("timers.Count() = %d, want 0", got)
		}

		rows := triggers.All()
		if len(rows) != 1 {
			t.Fatalf("triggers.All() returned %d rows, want 1", len(rows))
		}
		if rows[0].FireAt == nil {
			t.Fatal("triggers.fire_at is NULL — an armed time_based trigger has one by definition")
		}

		// Derived from event_at, not equal to it: doc 02 §7's lead time
		// puts the nudge EventLeadDays ahead of the occurrence. Expressed
		// as an offset from the constant rather than as a literal date, so
		// a recalibration needs no fixture edit.
		want := prospection.LeadTime(eventAt)
		assertInstant(t, "triggers.fire_at", *rows[0].FireAt, want, map[string]time.Time{
			"event_at":            eventAt,
			"due_at":              dueAt,
			"the capture instant": now,
		})
		if rows[0].Payload.LeadDays != prospection.EventLeadDays {
			t.Errorf("payload.lead_days = %d, want %d", rows[0].Payload.LeadDays, prospection.EventLeadDays)
		}
	})
}

// captureAndArm runs one capture over fresh fakes and hands back the two
// arming repositories plus the result.
func captureAndArm(t *testing.T, now time.Time, llmCase string) (*memrepo.Timers, *memrepo.Triggers, brain.CaptureResult) {
	t.Helper()

	ctx := context.Background()
	embeddings := memrepo.NewEmbeddings()
	triggers := memrepo.NewTriggers()
	timers := memrepo.NewTimers()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), llmCase)
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, memrepo.NewUnits(), embeddings,
		memrepo.NewLexical(), memrepo.NewRelations(), memrepo.NewDecisionLog(), llm, llm, llm, embed,
		brain.NewIndex(idx), memrepo.NewSignals(), triggers, timers, 0.5)

	result, err := svc.Capture(ctx, brain.CaptureInput{Text: "replayed by case id", Channel: "chat"})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return timers, triggers, result
}

// assertInstant fails unless got equals want, and names every instant it
// must NOT equal — so the failure message says which field was read, not
// merely that two times differ.
func assertInstant(t *testing.T, column string, got, want time.Time, mustNotEqual map[string]time.Time) {
	t.Helper()

	if !got.Equal(want) {
		t.Errorf("%s = %s, want %s", column, got, want)
	}
	for name, wrong := range mustNotEqual {
		if got.Equal(wrong) {
			t.Errorf("%s = %s, which is %s — the wrong field was read", column, got, name)
		}
	}
}
