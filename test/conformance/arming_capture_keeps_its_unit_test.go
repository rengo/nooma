// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// TestArmingCaptureKeepsItsUnit is the memory half of an armed capture, and
// it exists because a live vault had none.
//
// "tengo dentista el viernes a las 9am" over Telegram produced a trigger
// and, in the same vault, zero units. The reminder survived; the memory did
// not. Asked afterwards what it knew about the appointment, Nooma answered
// that it could not find anything — correctly, because nothing was there.
//
// The cause is the arming fork's position (internal/brain/capture.go): it
// sits before classify.ToUnit so a TIMER never becomes a unit, which is I04
// and is right — a timer is ephemeral. But it returns for every armable
// kind, and an event or a recurring reminder is memory that also happens to
// carry a nudge. Their units were never built, and the triggers were
// written with a NULL unit_id, which no foreign key rejects: a NULL
// reference violates nothing, so the database was as quiet as the tests.
//
// I17 — "firing a recurring trigger creates the next one pointing at the
// SAME unit" — was being satisfied vacuously, both rows pointing at the
// same nothing.
//
// The two halves are asserted together, in one test, because the rule is
// the contrast between them and not either one alone: the SAME fork must
// keep a unit for one kind and refuse one for the other, and a test that
// covers one kind cannot see the fork at all.
func TestArmingCaptureKeepsItsUnit(t *testing.T) {
	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)

	t.Run("a recurring reminder is memory that also nudges", func(t *testing.T) {
		units, triggers, _, result := captureAndArmWithUnits(t, now,
			"classify-recurring-reminder-armed-mothers-birthday")

		if result.Armed == nil {
			t.Fatalf("Armed = nil, want an armed plan: %+v", result)
		}
		if got := units.Count(); got != 1 {
			t.Fatalf("units.Count() = %d, want 1 — the capture armed a nudge and kept no "+
				"memory, so nothing can ever be recalled about it", got)
		}
		if result.UnitID == "" {
			t.Error("CaptureResult.UnitID is empty — a caller cannot name what was stored")
		}

		armed := triggers.All()
		if len(armed) != 1 {
			t.Fatalf("triggers.All() = %d rows, want 1", len(armed))
		}
		if armed[0].UnitID == nil {
			t.Fatal("the trigger's UnitID is nil — ports.Trigger.UnitID is nil only for a " +
				"pattern_based trigger, and this one is time_based. A NULL reference " +
				"violates no foreign key, which is why nothing caught this")
		}
		if *armed[0].UnitID != result.UnitID {
			t.Errorf("the trigger hangs off %q and the capture stored %q — a trigger pointing "+
				"at a different unit than the one it came from cannot re-arm onto the same "+
				"memory (I17)", *armed[0].UnitID, result.UnitID)
		}
	})

	t.Run("a timer stays ephemeral", func(t *testing.T) {
		// I04, and the reason the fork sits where it does. Asserted here as
		// the contrast: the fix must keep a unit for the kind above WITHOUT
		// creating one here, and only both assertions together say that.
		units, triggers, timers, _ := captureAndArmWithUnits(t, now,
			"classify-timer-armed-bread-in-the-oven")

		if got := units.Count(); got != 0 {
			t.Errorf("units.Count() = %d, want 0 — a timer is ephemeral and never becomes a "+
				"unit (I04)", got)
		}
		if got := timers.Count(); got != 1 {
			t.Errorf("timers.Count() = %d, want 1", got)
		}
		if got := triggers.Count(); got != 0 {
			t.Errorf("triggers.Count() = %d, want 0", got)
		}
	})
}

// TestArmingCaptureIsGatedOnTheKindsOwnUnitType pins WHICH kinds keep a
// unit to the declaration that already answers that question, rather than
// to a list this test would restate.
//
// classify.Kind.UnitType() returns whether a kind persists a unit at all —
// it is what classify.ToUnit itself reads. A future armable kind is covered
// with no edit here, which is the same anti-drift shape joinVocabulary
// gives the prompt.
func TestArmingCaptureIsGatedOnTheKindsOwnUnitType(t *testing.T) {
	for _, kind := range []classify.Kind{
		classify.KindTimer,
		classify.KindEvent,
		classify.KindRecurringReminder,
	} {
		_, persists := kind.UnitType()
		if kind == classify.KindTimer && persists {
			t.Errorf("%q persists a unit — I04 says a timer never becomes one", kind)
		}
		if kind != classify.KindTimer && !persists {
			t.Errorf("%q does not persist a unit, so an armed capture of it would keep no "+
				"memory at all", kind)
		}
	}
}

// captureAndArmWithUnits is captureAndArm plus the units repository, which
// that helper drops on the floor — it was written for a question about
// instants, and the question here is about what was stored.
func captureAndArmWithUnits(t *testing.T, now time.Time, llmCase string) (
	*memrepo.Units, *memrepo.Triggers, *memrepo.Timers, brain.CaptureResult) {
	t.Helper()

	ctx := context.Background()
	units := memrepo.NewUnits()
	embeddings := memrepo.NewEmbeddings()
	triggers := memrepo.NewTriggers()
	timers := memrepo.NewTimers()
	llm := fakeprovider.New(t, testdataLLMCasesDir(t), llmCase)
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)

	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("embeddings.LoadIndex(%q): %v", embedFakeModel, err)
	}
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings,
		memrepo.NewLexical(), memrepo.NewRelations(), memrepo.NewDecisionLog(), llm, llm, embed,
		brain.NewIndex(idx), memrepo.NewSignals(), triggers, timers)

	result, err := svc.Capture(ctx, brain.CaptureInput{Text: "replayed by case id", Channel: "chat"})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	return units, triggers, timers, result
}
