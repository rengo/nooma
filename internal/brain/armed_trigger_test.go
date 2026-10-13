package brain

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/ports"
)

// TestArmedTrigger pins the stored row a plan becomes: the columns every
// arming shares, the recurrence columns only for a recurring plan, and a
// degraded interrupt reading stored as NULL.
func TestArmedTrigger(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	fireAt := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	about := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	level := 0.8
	unitID := "u1"
	oneShot := prospection.Plan{What: prospection.ArmTrigger, FireAt: fireAt, About: about,
		LeadDays: prospection.EventLeadDays, Interrupt: prospection.ResolveInterrupt(&level)}

	t.Run("a one-shot plan", func(t *testing.T) {
		got := armedTrigger("t1", &unitID, "Dentista", oneShot, now)
		if got.ID != "t1" || got.UnitID == nil || *got.UnitID != "u1" || got.Kind != ports.TriggerKindTimeBased ||
			got.FireAt == nil || !got.FireAt.Equal(fireAt) || !got.CreatedAt.Equal(now) {
			t.Errorf("row = %+v", got)
		}
		if got.InterruptLevel == nil || *got.InterruptLevel != level {
			t.Errorf("InterruptLevel = %v, want %v", got.InterruptLevel, level)
		}
		// A one-shot reminder records the event instant it is about.
		want := ports.TriggerPayload{ActionText: "Dentista", Rationale: armRationale(oneShot), LeadDays: prospection.EventLeadDays, About: about}
		if !reflect.DeepEqual(got.Payload, want) || !strings.Contains(got.Payload.Rationale, "2026-10-30T10:00:00Z") {
			t.Errorf("Payload = %+v, want %+v", got.Payload, want)
		}
		if got.RecurrenceRule != nil || got.RecurrenceAnchor != nil {
			t.Errorf("recurrence = %v %v, want none for a one-shot", got.RecurrenceRule, got.RecurrenceAnchor)
		}
	})

	t.Run("a one-shot plan records every user lead it serves", func(t *testing.T) {
		withLead := oneShot
		withLead.Leads = []time.Duration{3 * time.Hour, 2 * time.Hour}
		if got := armedTrigger("t1", &unitID, "Dentista", withLead, now).Payload; !reflect.DeepEqual(got.UserLeadsMinutes, []int{180, 120}) || !got.About.Equal(about) {
			t.Errorf("Payload = %+v, want user leads [180 120] and the event instant", got)
		}
	})

	t.Run("a recurring plan", func(t *testing.T) {
		plan := oneShot
		plan.What, plan.Rule = prospection.ArmRecurring, prospection.RuleYearly
		plan.Anchor = prospection.Anchor{Month: time.October, Day: 30}
		got := armedTrigger("t2", &unitID, "Cumple", plan, now)
		if got.RecurrenceRule == nil || *got.RecurrenceRule != prospection.RuleYearly ||
			got.RecurrenceAnchor == nil || got.RecurrenceAnchor.Month != time.October || got.RecurrenceAnchor.Day != 30 {
			t.Errorf("recurrence = %v %v, want yearly on Oct 30", got.RecurrenceRule, got.RecurrenceAnchor)
		}
	})

	t.Run("a degraded interrupt reading is stored as NULL", func(t *testing.T) {
		plan := oneShot
		plan.Interrupt = prospection.ResolveInterrupt(nil)
		if got := armedTrigger("t3", &unitID, "x", plan, now); got.InterruptLevel != nil {
			t.Errorf("InterruptLevel = %v, want nil", *got.InterruptLevel)
		}
	})
}
