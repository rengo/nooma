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
	if p.QuietEnd != (TimeOfDay{Hour: QuietHoursEndHour}) || p.EveningAt != (TimeOfDay{Hour: DefaultEveningReminderHour}) {
		t.Errorf("QuietEnd, EveningAt = %+v, %+v, want 07:00 and 22:00 (ADR-0030)", p.QuietEnd, p.EveningAt)
	}
	if DefaultEventLeadFarHours != 24 || DefaultEventLeadNearHours != 2 || DefaultDateOnlyReminderHour != 9 || DefaultEveningReminderHour != 22 {
		t.Error("the default constants drifted from doc 02 §13's rows")
	}
}

// TestArm_EventReminders is doc 02 §7's "Lead time" (ADR-0029 points 1-3):
// one plan per lead still ahead, in firing order; none at once when every
// lead is behind (ADR-0047).
func TestArm_EventReminders(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	event := classify.KindEvent
	arm := func(eventAt, now time.Time, prefs ReminderPrefs) []Plan {
		t.Helper()
		plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &eventAt}, false, prefs, now)
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
	t.Run("every lead behind and the event ahead: none, not one at once (ADR-0047)", func(t *testing.T) {
		e := at(16, 10, 0)
		plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &e}, false, defaults, at(16, 9, 30))
		if ok || plans[0].What != ArmNothing || plans[0].Why != RefusalLeadsBehind {
			t.Errorf("Arm = %+v, want nothing armed, leads_behind", plans)
		}
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
	t.Run("a date-only event after the reminder time the day before: 09:00 on its day (ADR-0039)", func(t *testing.T) {
		e := at(16, 0, 0)
		check(t, arm(e, at(15, 12, 0), defaults), e, want{at(16, 9, 0), 0, false})
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
		plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &e}, false, defaults, at(10, 8, 0))
		if ok || len(plans) != 1 || plans[0].What != ArmNothing || plans[0].Why != RefusalAlreadyPast {
			t.Errorf("Arm = %+v, %v, want one already_past refusal", plans, ok)
		}
	})
}

// TestDeliverAt is ADR-0030 points 2-4, step by step: outside quiet hours
// unchanged; inside, quiet end when it is before the event, else the
// evening before; an instant left behind now is pulled to now outside
// quiet hours and dropped inside them.
func TestDeliverAt(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	p := DefaultReminderPrefs()
	cases := []struct {
		name                string
		nominal, event, now time.Time
		want                time.Time
		ok                  bool
	}{
		{"daytime is unchanged", at(16, 8, 0), at(16, 10, 0), at(13, 10, 0), at(16, 8, 0), true},
		{"quiet end itself is outside quiet hours", at(16, 7, 0), at(16, 9, 0), at(13, 10, 0), at(16, 7, 0), true},
		{"the last quiet minute moves to quiet end", at(16, 6, 59), at(16, 8, 0), at(13, 10, 0), at(16, 7, 0), true},
		{"midnight is quiet: quiet end before the event", at(16, 0, 0), at(16, 7, 1), at(13, 10, 0), at(16, 7, 0), true},
		{"the event at quiet end: the evening before", at(16, 5, 0), at(16, 7, 0), at(13, 10, 0), at(15, 22, 0), true},
		{"the event before quiet end: the evening before", at(16, 3, 0), at(16, 5, 0), at(13, 10, 0), at(15, 22, 0), true},
		{"the evening behind now, now allowed: at once", at(16, 3, 0), at(16, 5, 0), at(15, 22, 30), at(15, 22, 30), true},
		{"the evening behind now, now quiet: dropped", at(16, 3, 0), at(16, 5, 0), at(16, 0, 0), time.Time{}, false},
		{"now itself in quiet hours moves to quiet end", at(16, 6, 0), at(16, 7, 30), at(16, 6, 0), at(16, 7, 0), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := deliverAt(tc.nominal, tc.event, tc.now, p)
			if ok != tc.ok || (ok && !got.Equal(tc.want)) {
				t.Errorf("deliverAt(%v) = %v, %v; want %v, %v", tc.nominal, got, ok, tc.want, tc.ok)
			}
		})
	}
	t.Run("built on the zone's wall clock, not UTC's", func(t *testing.T) {
		// 08:00 UTC is 05:00 ART: quiet in the user's zone, daytime in UTC.
		nominal := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
		got, ok := deliverAt(nominal, at(16, 9, 0), at(13, 10, 0), p)
		if !ok || !got.Equal(at(16, 7, 0)) {
			t.Errorf("deliverAt = %v, %v; want 07:00 ART", got, ok)
		}
	})
}

// TestArm_EventRemindersAroundQuietHours: two leads the adjustment lands on
// one instant are armed once, and an event with no allowed instant arms
// nothing, saying quiet_hours.
func TestArm_EventRemindersAroundQuietHours(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, zone) }
	event := classify.KindEvent
	eventAt := at(16, 5)
	p := DefaultReminderPrefs()
	p.TimedLeads = []time.Duration{3 * time.Hour, 2 * time.Hour}

	plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &eventAt}, false, p, at(13, 10))
	if !ok || len(plans) != 1 || !plans[0].FireAt.Equal(at(15, 22)) || plans[0].LeadMinutes != 7*60 {
		t.Errorf("plans = %+v, want one at 22:00 the evening before, lead 420", plans)
	}
	plans, ok = Arm(classify.Classification{Kind: &event, EventAt: &eventAt}, false, p, at(16, 2))
	if ok || len(plans) != 1 || plans[0].What != ArmNothing || plans[0].Why != RefusalQuietHours {
		t.Errorf("plans = %+v, want nothing armed, why quiet_hours", plans)
	}
}
