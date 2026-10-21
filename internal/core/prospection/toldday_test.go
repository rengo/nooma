package prospection

import (
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

// The told-day rule (ADR-0037): an event lead the user did not ask for that
// would ring on the local day they told Nooma about an event on a later day is
// not armed, unless it is the last reminder the event would have.

var toldZone = time.FixedZone("UTC-3", -3*60*60)

func toldAt(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, toldZone)
}

func toldFires(plans []Plan) []time.Time {
	var out []time.Time
	for _, p := range plans {
		if p.What == ArmTrigger {
			out = append(out, p.FireAt)
		}
	}
	return out
}

func TestArm_ToldDay(t *testing.T) {
	event := classify.KindEvent
	timed := toldAt(20, 10, 0)
	bare := toldAt(20, 0, 0)
	late := toldAt(20, 20, 0)
	custom := DefaultReminderPrefs()
	custom.TimedLeads = []time.Duration{26 * time.Hour, 25 * time.Hour}

	cases := []struct {
		name  string
		c     classify.Classification
		asked bool
		prefs ReminderPrefs
		now   time.Time
		want  []time.Time
	}{
		{"tomorrow at 10 told at 08:44: the 24-hour ring today is not armed",
			classify.Classification{Kind: &event, EventAt: &timed}, false, DefaultReminderPrefs(), toldAt(19, 8, 44),
			[]time.Time{toldAt(20, 8, 0)}},
		{"tomorrow at 10 told at 11:00: the 24-hour lead is behind anyway",
			classify.Classification{Kind: &event, EventAt: &timed}, false, DefaultReminderPrefs(), toldAt(19, 11, 0),
			[]time.Time{toldAt(20, 8, 0)}},
		{"told two days ahead: both leads ring",
			classify.Classification{Kind: &event, EventAt: &timed}, false, DefaultReminderPrefs(), toldAt(18, 8, 44),
			[]time.Time{toldAt(19, 10, 0), toldAt(20, 8, 0)}},
		{"a date-only event tomorrow told before 09:00: its only ring stays",
			classify.Classification{Kind: &event, EventAt: &bare}, false, DefaultReminderPrefs(), toldAt(19, 8, 0),
			[]time.Time{toldAt(19, 9, 0)}},
		{"every lead lands today: the nearest one stays",
			classify.Classification{Kind: &event, EventAt: &late}, false, custom, toldAt(19, 8, 0),
			[]time.Time{toldAt(19, 19, 0)}},
		{"an event later today keeps its leads",
			classify.Classification{Kind: &event, EventAt: &late}, false, DefaultReminderPrefs(), toldAt(20, 8, 0),
			[]time.Time{toldAt(20, 18, 0)}},
		{"an asked event: the asked lead rings, the 24-hour one today does not",
			classify.Classification{Kind: &event, EventAt: &timed}, true, DefaultReminderPrefs(), toldAt(19, 8, 44),
			[]time.Time{toldAt(20, 8, 0), toldAt(20, 9, 45)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plans, ok := Arm(tc.c, tc.asked, tc.prefs, tc.now)
			if got := toldFires(plans); !ok || !slices.EqualFunc(got, tc.want, time.Time.Equal) {
				t.Errorf("Arm fires at %v (armed %v), want %v", got, ok, tc.want)
			}
		})
	}
}

func TestArm_ToldDayAskedLeadTodayIsKept(t *testing.T) {
	event := classify.KindEvent
	at := toldAt(20, 10, 0)
	prefs := DefaultReminderPrefs()
	prefs.RemindMeLead = 23 * time.Hour
	plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &at}, true, prefs, toldAt(19, 8, 44))
	want := []time.Time{toldAt(19, 11, 0), toldAt(20, 8, 0)}
	if got := toldFires(plans); !ok || !slices.EqualFunc(got, want, time.Time.Equal) {
		t.Errorf("Arm fires at %v, want %v: the asked lead today stays, the unasked 24-hour one does not", got, want)
	}
}

func TestFollow_ToldDay(t *testing.T) {
	prefs := DefaultReminderPrefs()
	moved := toldAt(20, 11, 0)

	t.Run("a correction the day it is told does not bring the dropped ring back", func(t *testing.T) {
		got := Follow(moved, []Live{{ID: "a", FireAt: toldAt(20, 8, 0)}}, prefs, nil, toldAt(19, 9, 0))
		if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{toldAt(20, 9, 0)}, time.Time.Equal) ||
			!slices.Equal(got.Carry, []string{"a"}) {
			t.Errorf("Follow = %+v, want only the 2-hour ring, carried", got)
		}
	})
	t.Run("a correction is the user telling: a lead on its day is not armed", func(t *testing.T) {
		live := []Live{{ID: "a", FireAt: toldAt(19, 10, 0)}, {ID: "b", FireAt: toldAt(20, 8, 0)}}
		got := Follow(moved, live, prefs, nil, toldAt(19, 9, 0))
		if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{toldAt(20, 9, 0)}, time.Time.Equal) ||
			!slices.Equal(got.Expire, []string{"b"}) {
			t.Errorf("Follow = %+v, want the 2-hour ring alone, the surplus expired", got)
		}
	})
	t.Run("a re-arm on the capture day does not bring it back", func(t *testing.T) {
		got := FollowSent(toldAt(20, 10, 0), []Live{{ID: "a", FireAt: toldAt(20, 8, 0)}}, nil, prefs, nil, toldAt(19, 8, 44), toldAt(19, 9, 0))
		if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{toldAt(20, 8, 0)}, time.Time.Equal) || len(got.Expire) != 0 {
			t.Errorf("FollowSent = %+v, want the 2-hour ring alone", got)
		}
	})
	t.Run("a re-arm a day after the capture keeps the day-before ring", func(t *testing.T) {
		live := []Live{{ID: "a", FireAt: toldAt(19, 10, 0)}, {ID: "b", FireAt: toldAt(20, 8, 0)}}
		got := FollowSent(toldAt(20, 10, 0), live, nil, prefs, nil, toldAt(18, 20, 0), toldAt(19, 9, 0))
		if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{toldAt(19, 10, 0), toldAt(20, 8, 0)}, time.Time.Equal) ||
			len(got.Expire) != 0 {
			t.Errorf("FollowSent = %+v, want both rings kept", got)
		}
	})
	t.Run("an asked unit re-planned on its capture day keeps the asked lead only", func(t *testing.T) {
		got := FollowAsked(toldAt(20, 10, 0), true, false, nil, nil, prefs, nil, toldAt(19, 8, 44), toldAt(19, 9, 0))
		if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{toldAt(20, 8, 0), toldAt(20, 9, 45)}, time.Time.Equal) {
			t.Errorf("FollowAsked fires at %v, want the 2-hour and the asked ring", fires)
		}
	})
}

// TestFollow_AnEventsOwnLeadsAreAsked: leads the user named for this event
// (I40, ADR-0038) are asked for, so the told-day rule leaves them: "avisame 1
// día antes y 1 hora antes" today, about tomorrow at 10, rings today at 10 too.
func TestFollow_AnEventsOwnLeadsAreAsked(t *testing.T) {
	now, event := toldAt(19, 8, 44), toldAt(20, 10, 0)
	own := DefaultReminderPrefs().WithEventLeads([]int{1440, 60})
	if got := toldFires(Follow(event, nil, own, nil, now).Plans); !slices.Equal(got, []time.Time{toldAt(19, 10, 0), toldAt(20, 9, 0)}) {
		t.Errorf("own leads fire at %v, want today at 10:00 and tomorrow at 09:00", got)
	}
	if got := toldFires(Follow(event, nil, DefaultReminderPrefs(), nil, now).Plans); !slices.Equal(got, []time.Time{toldAt(20, 8, 0)}) {
		t.Errorf("the preferences' leads fire at %v, want the told-day rule to keep only 08:00 tomorrow", got)
	}
}

// TestRingOnTheDay is ADR-0039: an event on a later day than it is told, every
// lead behind, rings on its own day rather than at once, and a re-plan keeps
// that ring rather than bringing back an at-once one.
func TestRingOnTheDay(t *testing.T) {
	event := classify.KindEvent
	bare := toldAt(20, 0, 0)
	prefs := DefaultReminderPrefs()

	t.Run("a date-only event tomorrow told at 13:32 rings tomorrow at 09:00, serving no lead", func(t *testing.T) {
		plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &bare}, false, prefs, toldAt(19, 13, 32))
		if !ok || len(plans) != 1 || !plans[0].FireAt.Equal(toldAt(20, 9, 0)) || plans[0].Immediate || len(plans[0].Leads) != 0 {
			t.Errorf("Arm = %+v, want one ring at 09:00 tomorrow, not at once, serving no lead", plans)
		}
	})
	t.Run("05:00 tomorrow said at 22:30: no ring pulled to now, none on its day, quiet_hours", func(t *testing.T) {
		early := toldAt(20, 5, 0)
		plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &early}, false, prefs, toldAt(19, 22, 30))
		if ok || plans[0].Why != RefusalQuietHours {
			t.Errorf("Arm = %+v, want nothing armed, quiet_hours", plans)
		}
	})
	t.Run("05:00 tomorrow said at 20:00: the evening-before ring at 22:00 is not at once and stays", func(t *testing.T) {
		early := toldAt(20, 5, 0)
		plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &early}, false, prefs, toldAt(19, 20, 0))
		if fires := toldFires(plans); !ok || !slices.EqualFunc(fires, []time.Time{toldAt(19, 22, 0)}, time.Time.Equal) {
			t.Errorf("Arm fires at %v, want 22:00 today", fires)
		}
	})
	t.Run("an event later today has no ring at once and none on its day (ADR-0047)", func(t *testing.T) {
		later := toldAt(19, 15, 0)
		plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &later}, false, prefs, toldAt(19, 14, 0))
		if ok || len(plans) != 1 || plans[0].Why != RefusalLeadsBehind {
			t.Errorf("Arm = %+v, want nothing armed, leads_behind", plans)
		}
	})
	t.Run("a correction the day it is told carries the ring", func(t *testing.T) {
		live := []Live{{ID: "a", FireAt: toldAt(20, 9, 0)}}
		got := Follow(bare, live, prefs, nil, toldAt(19, 14, 0))
		if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{toldAt(20, 9, 0)}, time.Time.Equal) || got.Carry[0] != "a" {
			t.Errorf("Follow = %+v, want the 09:00 ring carried", got)
		}
	})
	t.Run("a correction with nothing live arms the ring a capture arms", func(t *testing.T) {
		got := Follow(bare, nil, prefs, nil, toldAt(19, 14, 0))
		if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{toldAt(20, 9, 0)}, time.Time.Equal) {
			t.Errorf("Follow = %+v, want the 09:00 ring created", got)
		}
	})
	t.Run("a re-plan on the capture day keeps the ring", func(t *testing.T) {
		live := []Live{{ID: "a", FireAt: toldAt(20, 9, 0)}}
		got := FollowSent(bare, live, nil, prefs, nil, toldAt(19, 13, 32), toldAt(19, 15, 0))
		if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{toldAt(20, 9, 0)}, time.Time.Equal) || len(got.Expire) != 0 {
			t.Errorf("FollowSent = %+v, want the 09:00 ring kept", got)
		}
	})
	t.Run("a re-plan on a later day is ADR-0032's, not this rule's", func(t *testing.T) {
		timed := toldAt(21, 15, 0)
		custom := prefs
		custom.TimedLeads = []time.Duration{36 * time.Hour}
		got := FollowSent(timed, nil, nil, custom, nil, toldAt(19, 13, 32), toldAt(20, 8, 0))
		if got.Plans[0].What != ArmNothing {
			t.Errorf("FollowSent = %+v, want nothing: the catch-up is owed by CatchUp", got)
		}
	})
	t.Run("a zero told is ADR-0029's at-once reminder", func(t *testing.T) {
		got := FollowSent(bare, []Live{{ID: "a", FireAt: toldAt(19, 13, 0)}}, nil, prefs, nil, time.Time{}, toldAt(19, 13, 32))
		if len(got.Plans) != 1 || !got.Plans[0].Immediate {
			t.Errorf("FollowSent = %+v, want the live one pulled to now", got)
		}
	})
}

// TestArm_AnEventsOwnLeadsNeverRingAtOnceOnTheToldDay (I44, ADR-0045): leads
// the user named are kept on the told day, but none rings at the moment the
// event is told: a lead already behind is not sent at once. With nothing else
// left, the one reminder rings on the event's own day (ADR-0039 point 1).
func TestArm_AnEventsOwnLeadsNeverRingAtOnceOnTheToldDay(t *testing.T) {
	event := classify.KindEvent
	at, now := toldAt(20, 10, 0), toldAt(19, 14, 0)
	c := classify.Classification{Kind: &event, EventAt: &at}
	for _, tc := range []struct {
		name  string
		leads []int
		want  []time.Time
	}{
		{"the day before, behind: one ring on its day", []int{1440}, []time.Time{toldAt(20, 9, 0)}},
		{"the day before, behind, and an hour before", []int{1440, 60}, []time.Time{toldAt(20, 9, 0)}},
		{"the day before, still ahead, rings today", []int{1440 - 60*6}, []time.Time{toldAt(19, 16, 0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plans, ok := Arm(c, false, DefaultReminderPrefs().WithEventLeads(tc.leads), now)
			if fires := toldFires(plans); !ok || !slices.EqualFunc(fires, tc.want, time.Time.Equal) {
				t.Errorf("Arm fires at %v, want %v", fires, tc.want)
			}
			for _, p := range plans {
				if p.Immediate {
					t.Errorf("Arm = %+v, want no ring at once", plans)
				}
			}
		})
	}
	t.Run("a correction carries no live ring to now", func(t *testing.T) {
		live := []Live{{ID: "r", FireAt: toldAt(20, 8, 0)}}
		got := Follow(at, live, DefaultReminderPrefs().WithEventLeads([]int{1440}), nil, now)
		if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{toldAt(20, 9, 0)}, time.Time.Equal) || got.Plans[0].Immediate {
			t.Errorf("Follow = %+v, want the live ring moved to 09:00 on the event's day", got)
		}
	})
}

// TestFollow_OwnLeadsAllBehindWithNothingLiveRingOnTheEventsDay (ADR-0045 point
// 3): a correction that sets own leads, every one behind, on the told day, with
// no ring live, arms the one ring on the event's own day, as the preferences'
// leads do (ADR-0039), not nothing.
func TestFollow_OwnLeadsAllBehindWithNothingLiveRingOnTheEventsDay(t *testing.T) {
	got := Follow(toldAt(20, 10, 0), nil, DefaultReminderPrefs().WithEventLeads([]int{1440}), nil, toldAt(19, 14, 0))
	if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{toldAt(20, 9, 0)}, time.Time.Equal) || got.Plans[0].Immediate {
		t.Errorf("Follow = %+v, want one ring on the event's day at 09:00", got)
	}
}

// TestArm_NothingUnaskedRingsAtOnceOnTheToldDay is ADR-0047: an event today,
// told when a lead nobody asked for is behind, drops that lead and keeps one
// still ahead; with none left it arms nothing, leads_behind, the user's own
// leads included. The client's case: "hoy a las 21" told at 17:14 with a
// 4-hour lead.
func TestArm_NothingUnaskedRingsAtOnceOnTheToldDay(t *testing.T) {
	event := classify.KindEvent
	nine := toldAt(20, 21, 0)
	now := toldAt(20, 17, 14)
	four := DefaultReminderPrefs()
	four.TimedLeads = []time.Duration{4 * time.Hour}
	for _, tc := range []struct {
		name  string
		prefs ReminderPrefs
		want  []time.Time
	}{
		{"a 4-hour lead behind: nothing", four, nil},
		{"its own 4-hour lead behind: nothing", DefaultReminderPrefs().WithEventLeads([]int{240}), nil},
		{"the default leads: the 2-hour one still ahead, alone", DefaultReminderPrefs(), []time.Time{toldAt(20, 19, 0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &nine}, false, tc.prefs, now)
			if fires := toldFires(plans); !slices.EqualFunc(fires, tc.want, time.Time.Equal) || ok != (tc.want != nil) {
				t.Errorf("Arm = %+v, %v, want fires %v", plans, ok, tc.want)
			}
			if tc.want == nil && plans[0].Why != RefusalLeadsBehind {
				t.Errorf("Why = %q, want leads_behind", plans[0].Why)
			}
		})
	}
}

// TestFollow_ARingAlreadyDueKeepsItsInstantOnTheToldDay: a correction told
// when every lead is behind pulls nothing to now (ADR-0047), but a live ring
// already due is going out anyway and keeps its instant.
func TestFollow_ARingAlreadyDueKeepsItsInstantOnTheToldDay(t *testing.T) {
	due := Live{ID: "a", FireAt: toldAt(20, 16, 0)}
	got := Follow(toldAt(20, 18, 0), []Live{due, {ID: "b", FireAt: toldAt(21, 9, 0)}}, DefaultReminderPrefs(), nil, toldAt(20, 17, 0))
	if fires := toldFires(got.Plans); !slices.EqualFunc(fires, []time.Time{due.FireAt}, time.Time.Equal) ||
		got.Carry[0] != "a" || !slices.Equal(got.Expire, []string{"b"}) {
		t.Errorf("Follow = %+v, want a kept at %v, b expired", got, due.FireAt)
	}
}

// TestArmAsked_AnUnaskedLeadIsNotPulledToNowOnTheToldDay is ADR-0047 for an
// asked event: "recordame, mañana a las 5 tengo vuelo" said at 22:30 keeps its
// asked ring at 04:45, and its unasked 2-hour lead, pulled to now by the night,
// does not ring at once.
func TestArmAsked_AnUnaskedLeadIsNotPulledToNowOnTheToldDay(t *testing.T) {
	event := classify.KindEvent
	flight := toldAt(20, 5, 0)
	plans, ok := Arm(classify.Classification{Kind: &event, EventAt: &flight}, true, DefaultReminderPrefs(), toldAt(19, 22, 30))
	if fires := toldFires(plans); !ok || !slices.EqualFunc(fires, []time.Time{toldAt(20, 4, 45)}, time.Time.Equal) || !plans[0].Asked {
		t.Errorf("Arm = %+v, want the asked ring at 04:45 alone", plans)
	}
}
