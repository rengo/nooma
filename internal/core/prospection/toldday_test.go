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
