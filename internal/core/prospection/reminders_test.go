package prospection

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

// TestDefaultReminderPrefs pins ADR-0029's defaults to doc 02 §13's rows.
func TestDefaultReminderPrefs(t *testing.T) {
	p := DefaultReminderPrefs()
	if len(p.TimedLeads) != 2 || p.TimedLeads[0] != 24*time.Hour || p.TimedLeads[1] != 2*time.Hour {
		t.Errorf("TimedLeads = %v, want [24h 2h]", p.TimedLeads)
	}
	if p.DateOnlyAt != (TimeOfDay{Hour: 9}) {
		t.Errorf("DateOnlyAt = %+v, want 09:00", p.DateOnlyAt)
	}
	if DefaultEventLeadFarHours != 24 || DefaultEventLeadNearHours != 2 || DefaultDateOnlyReminderHour != 9 {
		t.Error("the default constants drifted from doc 02 §13's rows")
	}
}

// TestArm_EventReminders is doc 02 §7's "Lead time" (ADR-0029 points 1-3):
// one plan per lead still ahead, in firing order; one at once only when
// every lead is behind and the event is not.
func TestArm_EventReminders(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	event := classify.KindEvent
	arm := func(eventAt, now time.Time, prefs ReminderPrefs) []Plan {
		t.Helper()
		plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &eventAt}, prefs, now)
		if !ok {
			t.Fatalf("Arm(%v at %v) armed nothing: %+v", eventAt, now, plans)
		}
		return plans
	}
	type want struct {
		fireAt    time.Time
		lead      int
		immediate bool
	}
	check := func(t *testing.T, got []Plan, eventAt time.Time, wants ...want) {
		t.Helper()
		if len(got) != len(wants) {
			t.Fatalf("plans = %d, want %d: %+v", len(got), len(wants), got)
		}
		for i, w := range wants {
			p := got[i]
			if p.What != ArmTrigger || !p.FireAt.Equal(w.fireAt) || p.LeadMinutes != w.lead ||
				p.Immediate != w.immediate || !p.About.Equal(eventAt) {
				t.Errorf("plan %d = %+v, want a trigger about %v firing %v (lead %d, immediate %v)",
					i, p, eventAt, w.fireAt, w.lead, w.immediate)
			}
		}
	}
	defaults := DefaultReminderPrefs()

	t.Run("a timed event days ahead: 24 hours and 2 hours before, in order", func(t *testing.T) {
		e := at(16, 10, 0)
		check(t, arm(e, at(10, 8, 0), defaults), e, want{at(15, 10, 0), 1440, false}, want{at(16, 8, 0), 120, false})
	})
	t.Run("a lead behind is skipped, the rest armed", func(t *testing.T) {
		e := at(16, 10, 0)
		check(t, arm(e, at(15, 11, 0), defaults), e, want{at(16, 8, 0), 120, false})
	})
	t.Run("a lead exactly at now is still armed, not pulled", func(t *testing.T) {
		e := at(16, 10, 0)
		check(t, arm(e, at(16, 8, 0), defaults), e, want{at(16, 8, 0), 120, false})
	})
	t.Run("every lead behind and the event ahead: one at once, no lead", func(t *testing.T) {
		e := at(16, 10, 0)
		now := at(16, 9, 30)
		check(t, arm(e, now, defaults), e, want{now, 0, true})
	})
	t.Run("leads are absolute: 24 hours across a DST change is 24 hours", func(t *testing.T) {
		ny, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skip("no tzdata")
		}
		e := time.Date(2026, 11, 1, 12, 0, 0, 0, ny) // the day clocks fall back
		got := arm(e, time.Date(2026, 10, 20, 0, 0, 0, 0, ny), defaults)
		if !got[0].FireAt.Equal(e.Add(-24 * time.Hour)) {
			t.Errorf("FireAt = %v, want exactly 24 hours before %v", got[0].FireAt, e)
		}
	})
	t.Run("a date-only event: the day before at 09:00 in the clock's zone", func(t *testing.T) {
		e := at(16, 0, 0)
		check(t, arm(e, at(10, 8, 0), defaults), e, want{at(15, 9, 0), 15 * 60, false})
	})
	t.Run("a date-only event after the reminder time the day before: at once", func(t *testing.T) {
		e := at(16, 0, 0)
		now := at(15, 12, 0)
		check(t, arm(e, now, defaults), e, want{now, 0, true})
	})
	t.Run("midnight in another zone is a timed event", func(t *testing.T) {
		e := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC) // 21:00 the 15th in the clock's zone
		got := arm(e, at(10, 8, 0), defaults)
		if len(got) != 2 {
			t.Errorf("plans = %+v, want the two timed leads", got)
		}
	})
	t.Run("a second off midnight is a timed event", func(t *testing.T) {
		e := at(16, 0, 0).Add(time.Second)
		if got := arm(e, at(10, 8, 0), defaults); len(got) != 2 {
			t.Errorf("plans = %+v, want the two timed leads", got)
		}
	})
	t.Run("the preferences given are the ones used", func(t *testing.T) {
		e := at(16, 10, 0)
		prefs := ReminderPrefs{TimedLeads: []time.Duration{3 * time.Hour}, DateOnlyAt: TimeOfDay{Hour: 20, Minute: 30}}
		check(t, arm(e, at(10, 8, 0), prefs), e, want{at(16, 7, 0), 180, false})
		d := at(16, 0, 0)
		check(t, arm(d, at(10, 8, 0), prefs), d, want{at(15, 20, 30), 210, false})
	})
	t.Run("an event already past arms nothing", func(t *testing.T) {
		e := at(10, 7, 0)
		plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &e}, defaults, at(10, 8, 0))
		if ok || len(plans) != 1 || plans[0].What != ArmNothing || plans[0].Why != RefusalAlreadyPast {
			t.Errorf("Arm = %+v, %v, want one already_past refusal", plans, ok)
		}
	})
}
