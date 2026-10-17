package prospection

import (
	"testing"
	"time"
)

var followAskedZone = time.FixedZone("UTC-3", -3*60*60)

func followAskedAt(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, followAskedZone)
}

// TestFollowAsked_CarriesTheAskedLead: an asked event at Fri 10:00, re-planned
// on Thursday night after its 24-hour lead went stale, keeps its 2-hour and its
// asked reminder, each carried onto its own live row, and plans nothing at once.
func TestFollowAsked_CarriesTheAskedLead(t *testing.T) {
	now, event := followAskedAt(22, 21, 30), followAskedAt(23, 10, 0)
	live := []Live{{ID: "two-hours", FireAt: followAskedAt(23, 8, 0)}, {ID: "asked", FireAt: followAskedAt(23, 9, 45)}}
	f := FollowAsked(event, true, false, live, nil, DefaultReminderPrefs(), nil, now)
	if len(f.Plans) != 2 || len(f.Expire) != 0 || f.Carry[0] != "two-hours" || f.Carry[1] != "asked" {
		t.Fatalf("following = %+v, want both live rows carried", f)
	}
	if !f.Plans[0].FireAt.Equal(followAskedAt(23, 8, 0)) || !f.Plans[1].FireAt.Equal(followAskedAt(23, 9, 45)) || !f.Plans[1].Asked {
		t.Errorf("plans = %+v, want 08:00 and the asked 09:45", f.Plans)
	}
}

// TestFollowAsked_ADayKeepsItsDay: an asked date-only event on Friday, its
// day-before lead behind, keeps its own day's ring and gains nothing at once.
func TestFollowAsked_ADayKeepsItsDay(t *testing.T) {
	now, day := followAskedAt(22, 21, 30), followAskedAt(23, 0, 0)
	live := []Live{{ID: "that-day", FireAt: followAskedAt(23, 9, 0)}}
	f := FollowAsked(day, true, true, live, nil, DefaultReminderPrefs(), nil, now)
	if len(f.Plans) != 1 || f.Carry[0] != "that-day" || !f.Plans[0].FireAt.Equal(followAskedAt(23, 9, 0)) || f.Plans[0].Immediate {
		t.Errorf("following = %+v, want the day's 09:00 carried", f)
	}
}

// TestFollowAsked_EveryLeadBehindPlansNothing: an asked day alone whose ring is
// behind plans nothing and expires what is live; the catch-up is CatchUpAsked's.
func TestFollowAsked_EveryLeadBehindPlansNothing(t *testing.T) {
	now, day := followAskedAt(22, 21, 30), followAskedAt(22, 0, 0)
	f := FollowAsked(day, false, true, []Live{{ID: "x", FireAt: followAskedAt(22, 21, 30)}}, nil, DefaultReminderPrefs(), nil, now)
	if len(f.Plans) != 1 || f.Plans[0].What != ArmNothing || f.Plans[0].Why != RefusalNone || len(f.Expire) != 1 {
		t.Errorf("following = %+v, want nothing planned and the live row expired", f)
	}
	// A day already over is a refusal, not a catch-up.
	if f := FollowAsked(day, false, true, nil, nil, DefaultReminderPrefs(), nil, followAskedAt(23, 1, 0)); f.Plans[0].Why != RefusalAlreadyPast {
		t.Errorf("a past day: %+v, want already_past", f.Plans[0])
	}
}

// TestCatchUpAsked: the at-once reminder an asked unit's capture would arm,
// owed only with nothing live, nothing sent about the instant, and its day or
// instant still far enough ahead.
func TestCatchUpAsked(t *testing.T) {
	day, prefs, interrupt := followAskedAt(22, 0, 0), DefaultReminderPrefs(), ResolveInterrupt(nil)
	now := followAskedAt(22, 21, 30)
	plan, owed := CatchUpAsked(day, false, true, nil, nil, prefs, interrupt, now)
	if !owed || plan.What != ArmTrigger || !plan.FireAt.Equal(now) || !plan.Immediate || !plan.Asked || plan.NightExempt || !plan.About.Equal(day) {
		t.Errorf("catch-up = %+v, %v; want an asked at-once reminder about the day, under the night rules", plan, owed)
	}
	for name, tc := range map[string]struct {
		live []Live
		sent []Sent
		now  time.Time
	}{
		"something live":            {live: []Live{{ID: "x", FireAt: now}}, now: now},
		"something sent about it":   {sent: []Sent{{About: day, At: followAskedAt(22, 9, 0)}}, now: now},
		"too little of the day":     {now: followAskedAt(22, 23, 55)},
		"the day is over":           {now: followAskedAt(23, 0, 5)},
		"a lead is still ahead now": {now: followAskedAt(22, 8, 0)},
	} {
		if plan, owed := CatchUpAsked(day, false, true, tc.live, tc.sent, prefs, interrupt, tc.now); owed {
			t.Errorf("%s: owed %+v, want nothing", name, plan)
		}
	}
	// A sent reminder about another instant covers nothing.
	if _, owed := CatchUpAsked(day, false, true, nil, []Sent{{About: followAskedAt(21, 0, 0)}}, prefs, interrupt, now); !owed {
		t.Error("a send about another day covered this one")
	}
}

// TestCatchUpAsked_KeepsTheNightRules: a catch-up's instant is Nooma's choice,
// not a time the user named, so it keeps the night rules (ADR-0031 point 6):
// due inside quiet hours it waits for quiet end when that is before the
// instant, and when it is not, no instant is left and it is refused.
func TestCatchUpAsked_KeepsTheNightRules(t *testing.T) {
	prefs, interrupt := DefaultReminderPrefs(), ResolveInterrupt(nil)
	prefs.RemindMeLead = 12 * time.Hour // the asked lead is behind at 04:30
	now := followAskedAt(17, 4, 30)
	plan, owed := CatchUpAsked(followAskedAt(17, 10, 0), false, false, nil, nil, prefs, interrupt, now)
	if !owed || plan.What != ArmTrigger || !plan.FireAt.Equal(followAskedAt(17, 7, 0)) || plan.Immediate || plan.NightExempt ||
		!plan.Asked || plan.LeadMinutes != 180 {
		t.Errorf("catch-up at 04:30 for 10:00 = %+v, %v; want it at quiet end, 180 minutes before", plan, owed)
	}
	plan, owed = CatchUpAsked(followAskedAt(17, 6, 0), false, false, nil, nil, prefs, interrupt, now)
	if !owed || plan.What != ArmNothing || plan.Why != RefusalQuietHours {
		t.Errorf("catch-up at 04:30 for 06:00 = %+v, %v; want refused for quiet hours", plan, owed)
	}
}
