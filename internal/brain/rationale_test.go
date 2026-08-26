package brain

import (
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
)

// TestArmRationale_StatesTheRealDistanceNotTheConfiguredOne is doc 02 §11's
// glass box under test, and it had none.
//
// The recorded reason read "armed a trigger to fire at
// 2026-08-26T14:17:33Z, 7 days ahead of the event" for a trigger that
// fired the same day it was captured. Seven is EventLeadDays — the
// configured horizon — and clampToNow had already pulled the firing to the
// capture instant because that horizon was behind. A table whose purpose is
// evidence recorded a gap that never existed.
//
// This test exists because the mutation that restores the old wording was
// caught by nothing: the chat reply had tests, the decision_log did not,
// and the half nobody reads is the half worth pinning.
//
// Mutation: drop the Immediate branch from leadPhrase and the first
// subtest fails.
func TestArmRationale_StatesTheRealDistanceNotTheConfiguredOne(t *testing.T) {
	capture := time.Date(2026, 8, 26, 14, 17, 33, 0, time.UTC)
	appointment := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	t.Run("a clamped firing says so instead of claiming a lead", func(t *testing.T) {
		got := armRationale(prospection.Plan{
			What:      prospection.ArmTrigger,
			FireAt:    capture,
			About:     appointment,
			LeadDays:  prospection.EventLeadDays,
			Immediate: true,
		})

		if strings.Contains(got, "7 days") {
			t.Errorf("rationale %q claims the configured seven-day lead for a firing that "+
				"happens at capture time", got)
		}
		if !strings.Contains(got, "at once") {
			t.Errorf("rationale %q does not say the firing was immediate, which is the only "+
				"reason its instant equals the capture's", got)
		}
		if !strings.Contains(got, appointment.Format(time.RFC3339)) {
			t.Errorf("rationale %q never names what was armed for", got)
		}
	})

	t.Run("an unclamped firing states the distance it really has", func(t *testing.T) {
		event := capture.AddDate(0, 0, 30)
		fireAt := event.AddDate(0, 0, -7)

		got := armRationale(prospection.Plan{
			What:     prospection.ArmTrigger,
			FireAt:   fireAt,
			About:    event,
			LeadDays: prospection.EventLeadDays,
		})

		if !strings.Contains(got, "7 days ahead") {
			t.Errorf("rationale %q does not state the real seven-day gap", got)
		}
		if strings.Contains(got, "at once") {
			t.Errorf("rationale %q calls a firing three weeks out immediate", got)
		}
	})

	t.Run("the distance is measured, not restated from config", func(t *testing.T) {
		// LeadDays says seven and the instants say two. A rationale reading
		// the field would print seven; one measuring prints two.
		event := capture.AddDate(0, 0, 2)

		got := armRationale(prospection.Plan{
			What:     prospection.ArmTrigger,
			FireAt:   capture,
			About:    event,
			LeadDays: prospection.EventLeadDays,
		})

		if strings.Contains(got, "7 days") {
			t.Errorf("rationale %q printed the configured lead over the measured one: the "+
				"firing is two days from what it is about, not seven", got)
		}
		if !strings.Contains(got, "2 days ahead") {
			t.Errorf("rationale %q does not state the measured two-day gap", got)
		}
	})
}
