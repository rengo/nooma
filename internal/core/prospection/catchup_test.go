package prospection

import (
	"testing"
	"time"
)

// TestCatchUp pins when an event is owed its one at-once reminder (ADR-0032
// point 3): ahead, every lead behind, nothing live, nothing sent about it. It
// goes now, or at quiet end when that is before the event, and is refused (with
// its reason) when quiet end is not.
func TestCatchUp(t *testing.T) {
	prefs := DefaultReminderPrefs()
	day := func(h, m int) time.Time { return time.Date(2026, 10, 14, h, m, 0, 0, time.UTC) }
	event := day(11, 0)
	about := func(at time.Time) Sent { return Sent{About: at, At: day(9, 0)} }

	cases := []struct {
		name      string
		event     time.Time
		live      []Live
		sent      []Sent
		now       time.Time
		owed      bool
		wantFire  time.Time
		wantNone  Refusal
		immediate bool
	}{
		{"every lead behind: now", event, nil, nil, day(10, 0), true, day(10, 0), "", true},
		{"a lead still ahead is not owed a catch-up", day(13, 0), nil, nil, day(10, 0), false, time.Time{}, "", false},
		{"a live reminder carries it", event, []Live{{ID: "x", FireAt: day(10, 30)}}, nil, day(10, 0), false, time.Time{}, "", false},
		{"one sent about the instant covers it", event, nil, []Sent{about(event)}, day(10, 0), false, time.Time{}, "", false},
		{"one sent about another instant covers nothing", event, nil, []Sent{about(day(9, 0))}, day(10, 0), true, day(10, 0), "", true},
		{"the event has passed", event, nil, nil, day(11, 0), false, time.Time{}, "", false},
		{"a date-only event on its own day", day(0, 0), nil, nil, day(10, 0), true, day(10, 0), "", true},
		{"a date-only event once its day ended", day(0, 0), nil, nil, day(0, 0).AddDate(0, 0, 1), false, time.Time{}, "", false},
		{"in quiet hours: at quiet end, before the event", day(7, 30), nil, nil, day(6, 0), true, day(7, 0), "", false},
		{"in quiet hours with quiet end at the event: refused", day(7, 0), nil, nil, day(6, 0), true, time.Time{}, RefusalQuietHours, false},
		{"in quiet hours with quiet end after the event: refused", day(6, 30), nil, nil, day(6, 0), true, time.Time{}, RefusalQuietHours, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, owed := CatchUp(tc.event, tc.live, tc.sent, prefs, ResolveInterrupt(nil), tc.now)
			if owed != tc.owed {
				t.Fatalf("owed = %v, want %v (plan %+v)", owed, tc.owed, plan)
			}
			if !owed {
				return
			}
			if tc.wantNone != "" {
				if plan.What != ArmNothing || plan.Why != tc.wantNone {
					t.Errorf("plan = %+v, want a %s refusal", plan, tc.wantNone)
				}
				return
			}
			if plan.What != ArmTrigger || !plan.FireAt.Equal(tc.wantFire) || !plan.About.Equal(tc.event) || plan.Immediate != tc.immediate {
				t.Errorf("plan = %+v, want a trigger at %v about %v (immediate %v)", plan, tc.wantFire, tc.event, tc.immediate)
			}
			if tc.immediate && plan.LeadMinutes != 0 || !tc.immediate && plan.LeadMinutes != int(tc.event.Sub(tc.wantFire)/time.Minute) {
				t.Errorf("LeadMinutes = %d for a catch-up at %v of an event at %v", plan.LeadMinutes, tc.wantFire, tc.event)
			}
		})
	}
}
