package prospection

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

// TestPlan_CarriesWhatItArmedFor is the fact a plan was missing, and the
// one two separate defects both needed.
//
// A Plan said WHEN it would fire and never WHAT FOR. Everything downstream
// therefore reported the nudge instead of the thing:
//
//	Pablo:  tengo dentista el viernes a las 9am
//	Nooma:  Reminder set for Wed 26 Aug, 15:26.
//
// The date was understood perfectly — the unit holds 2026-08-28T12:00:00Z,
// which is 09:00 in the user's own zone. What was reported is fire_at, the
// instant the nudge goes out, which LeadTime pulled seven days earlier and
// clampToNow then pulled to the capture instant because that horizon had
// already passed. The reader has no way to tell a correct reading from a
// misparse, and read it as a misparse twice.
//
// The decision_log had the mirror image of the same hole: "armed a trigger
// to fire at 2026-08-26T14:17:33Z, 7 days ahead of the event" — it fired
// the same day, and 7 is the CONFIGURED lead rather than the real distance.
// Doc 02 §11 calls that table the glass box, and a glass box recording
// something false is worse than an empty one.
//
// Neither could be fixed where it was read. Both needed the plan to carry
// the instant it armed for.
//
// Mutation: drop About from any branch and that branch's subtest fails.
func TestPlan_CarriesWhatItArmedFor(t *testing.T) {
	loc := time.FixedZone("UTC-3", -3*60*60)
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, loc)
	kind := func(k classify.Kind) *classify.Kind { return &k }
	ptr := func(t time.Time) *time.Time { return &t }

	t.Run("a timer arms for its due instant", func(t *testing.T) {
		due := now.Add(40 * time.Minute)
		plan, ok := Arm(classify.Classification{
			Kind: kind(classify.KindTimer), DueAt: ptr(due),
		}, now)

		if !ok || plan.What != ArmTimer {
			t.Fatalf("Arm = (%+v, %v), want a timer", plan, ok)
		}
		if !plan.About.Equal(due) {
			t.Errorf("About = %v, want the due instant %v", plan.About, due)
		}
	})

	t.Run("a dated event arms for the event, not the nudge", func(t *testing.T) {
		// Two days out, so LeadTime's seven-day horizon is already past
		// and clampToNow moves the firing to now — the exact shape that
		// made the reply look like a misparse.
		event := time.Date(2026, 8, 28, 9, 0, 0, 0, loc)
		plan, ok := Arm(classify.Classification{
			Kind: kind(classify.KindEvent), EventAt: ptr(event),
		}, now)

		if !ok || plan.What != ArmTrigger {
			t.Fatalf("Arm = (%+v, %v), want a trigger", plan, ok)
		}
		if !plan.About.Equal(event) {
			t.Errorf("About = %v, want the event %v", plan.About, event)
		}
		if plan.FireAt.Equal(plan.About) {
			t.Error("FireAt equals About, so this fixture no longer exercises the case where " +
				"they differ — which is the whole reason About exists")
		}
		if !plan.FireAt.Equal(now) {
			t.Errorf("FireAt = %v, want the capture instant: a seven-day horizon two days out "+
				"is already behind, and the system is not late for what it just learned", plan.FireAt)
		}
	})

	t.Run("a recurring reminder arms for its next occurrence", func(t *testing.T) {
		event := time.Date(2026, 9, 4, 9, 0, 0, 0, loc)
		plan, ok := Arm(classify.Classification{
			Kind:           kind(classify.KindRecurringReminder),
			EventAt:        ptr(event),
			RecurrenceRule: ptr2(classify.RecurrenceRuleYearly),
		}, now)

		if !ok || plan.What != ArmRecurring {
			t.Fatalf("Arm = (%+v, %v), want a recurring trigger", plan, ok)
		}
		// The next occurrence re-derived from the anchor, not the date the
		// user happened to state: a birthday captured after this year's has
		// passed arms for next year's, and saying the stated date back
		// would name an instant already gone.
		want := NextOccurrence(plan.Rule, plan.Anchor, now)
		if !plan.About.Equal(want) {
			t.Errorf("About = %v, want the next occurrence %v", plan.About, want)
		}
	})
}

func ptr2[T any](v T) *T { return &v }
