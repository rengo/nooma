package prospection

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/classify"
)

func TestFollow(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	later := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	soon := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC) // both leads behind, the event ahead
	past := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	yearly := RuleYearly
	one := Live{ID: "a", FireAt: time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)}
	two := Live{ID: "b", FireAt: time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)}
	three := Live{ID: "c", FireAt: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}
	rec := Live{ID: "r", FireAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC), Rule: &yearly}
	prefs := DefaultReminderPrefs()

	event := classify.KindEvent
	fresh := func(at time.Time) []Plan {
		p, _ := Arm(classify.Classification{Kind: &event, EventAt: &at}, false, prefs, now)
		return p
	}

	t.Run("a date ahead moves the live triggers onto what a fresh capture arms, in order", func(t *testing.T) {
		want := Following{Plans: fresh(later), Carry: []string{"a", "b"}}
		if got := Follow(later, []Live{one, two}, prefs, nil, now); !reflect.DeepEqual(got, want) {
			t.Errorf("Follow = %+v, want %+v", got, want)
		}
	})
	t.Run("a missing reminder is created beside the carried one", func(t *testing.T) {
		got := Follow(later, []Live{one}, prefs, nil, now)
		if len(got.Plans) != 2 || !slices.Equal(got.Carry, []string{"a", ""}) || len(got.Expire) != 0 {
			t.Errorf("Follow = %+v, want a carried onto the first lead and the second created", got)
		}
	})
	t.Run("with nothing live, the whole set is created", func(t *testing.T) {
		got := Follow(later, nil, prefs, nil, now)
		if !reflect.DeepEqual(got.Plans, fresh(later)) || !slices.Equal(got.Carry, []string{"", ""}) {
			t.Errorf("Follow = %+v, want two new triggers", got)
		}
	})
	t.Run("surplus live triggers expire", func(t *testing.T) {
		got := Follow(later, []Live{one, two, three}, prefs, nil, now)
		if !slices.Equal(got.Carry, []string{"a", "b"}) || !slices.Equal(got.Expire, []string{"c"}) {
			t.Errorf("Follow = %+v, want a and b carried, c expired", got)
		}
	})
	t.Run("every lead behind: one live trigger is pulled to now, the rest expire", func(t *testing.T) {
		got := Follow(soon, []Live{one, two}, prefs, nil, now)
		if len(got.Plans) != 1 || !got.Plans[0].Immediate || !got.Plans[0].FireAt.Equal(now) ||
			!slices.Equal(got.Carry, []string{"a"}) || !slices.Equal(got.Expire, []string{"b"}) {
			t.Errorf("Follow = %+v, want a firing now, b expired", got)
		}
	})
	t.Run("a correction never creates an at-once reminder", func(t *testing.T) {
		got := Follow(soon, nil, prefs, nil, now)
		if len(got.Plans) != 1 || got.Plans[0].What != ArmNothing || len(got.Carry) != 1 || got.Carry[0] != "" {
			t.Errorf("Follow = %+v, want nothing armed", got)
		}
	})
	t.Run("an at-once reminder already due keeps its instant", func(t *testing.T) {
		due := Live{ID: "a", FireAt: now.Add(-time.Minute)}
		got := Follow(soon, []Live{due}, prefs, nil, now)
		if !got.Plans[0].FireAt.Equal(due.FireAt) || got.Carry[0] != "a" {
			t.Errorf("Follow = %+v, want a kept at %v", got, due.FireAt)
		}
	})
	t.Run("a past date expires everything live and arms nothing", func(t *testing.T) {
		got := Follow(past, []Live{one, two}, prefs, nil, now)
		if got.Plans[0].What != ArmNothing || got.Carry[0] != "" || !slices.Equal(got.Expire, []string{"a", "b"}) {
			t.Errorf("Follow = %+v, want a and b expired", got)
		}
	})
	t.Run("a recurring trigger stays recurring, re-anchored", func(t *testing.T) {
		got := Follow(past, []Live{one, rec}, prefs, nil, now)
		p := got.Plans[0]
		if len(got.Plans) != 1 || got.Carry[0] != "r" || p.What != ArmRecurring || p.Rule != RuleYearly ||
			p.Anchor.Month != time.October || p.Anchor.Day != 8 || !slices.Equal(got.Expire, []string{"a"}) {
			t.Errorf("Follow = %+v, want r carried as yearly on Oct 8, a expired", got)
		}
	})
	t.Run("a new trigger takes the given interrupt level", func(t *testing.T) {
		level := 0.9
		if got := Follow(later, nil, prefs, &level, now); got.Plans[1].Interrupt != ResolveInterrupt(&level) {
			t.Errorf("Interrupt = %+v, want the resolved %v", got.Plans[1].Interrupt, level)
		}
	})
}

// TestFollow_KeepsALeadPulledToNow is I29 under ADR-0030: a lead that
// deliverAt pulled to now is a real reminder of the moved event, not
// ADR-0029's at-once reminder, so a correction with nothing live arms the
// whole set a fresh capture arms — never nothing in front of it.
func TestFollow_KeepsALeadPulledToNow(t *testing.T) {
	zone := time.FixedZone("ART", -3*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	event := classify.KindEvent
	eventAt := at(16, 5, 0)
	custom := DefaultReminderPrefs()
	custom.TimedLeads = []time.Duration{6 * time.Hour, 2 * time.Hour}
	for _, tc := range []struct {
		name  string
		prefs ReminderPrefs
		now   time.Time
		want  []time.Time
	}{
		{"leads 6h and 2h at 22:30: the 6h one pulled to now, the 2h one at 23:00", custom, at(15, 22, 30), []time.Time{at(15, 22, 30), at(15, 23, 0)}},
		{"the default leads at 23:00: the 2h one pulled to now", DefaultReminderPrefs(), at(15, 23, 0), []time.Time{at(15, 23, 0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh, _ := Arm(classify.Classification{Kind: &event, EventAt: &eventAt}, false, tc.prefs, tc.now)
			got := Follow(eventAt, nil, tc.prefs, nil, tc.now)
			if !reflect.DeepEqual(got.Plans, fresh) {
				t.Fatalf("Follow plans = %+v, want the capture's %+v", got.Plans, fresh)
			}
			fires := make([]time.Time, len(got.Plans))
			for i, p := range got.Plans {
				fires[i] = p.FireAt
			}
			if !slices.EqualFunc(fires, tc.want, time.Time.Equal) {
				t.Errorf("fires at %v, want %v", fires, tc.want)
			}
		})
	}
}

// TestFollowSent: a re-arm under new preferences does not plan a lead that
// a reminder already delivered for the same event instant served — tied to
// the lead and the instant, not to times under the new preferences. Leads
// 6 h and 2 h, an event at 05:00: the 2 h reminder went out at 22:00,
// pulled to the evening, and at 22:30 the 6 h one waits at 23:00.
func TestFollowSent(t *testing.T) {
	event := time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)
	twoHours := Sent{About: event, Leads: []time.Duration{2 * time.Hour}, At: time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC)}
	six := time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)
	live := []Live{{ID: "six", FireAt: six}}
	prefs := DefaultReminderPrefs()
	prefs.TimedLeads = []time.Duration{6 * time.Hour, 2 * time.Hour}
	only23 := func(t *testing.T, name string, f Following) {
		t.Helper()
		if len(f.Plans) != 1 || f.Carry[0] != "six" || !f.Plans[0].FireAt.Equal(six) || len(f.Expire) != 0 {
			t.Errorf("%s: %+v, want the 23:00 reminder carried alone, the 2 h lead covered", name, f)
		}
	}

	if plain := Follow(event, live, prefs, nil, now); len(plain.Plans) != 2 {
		t.Fatalf("plain Follow = %+v, want the re-send this test guards against", plain)
	}
	only23(t, "same preferences", FollowSent(event, live, []Sent{twoHours}, prefs, nil, now))

	// B1: a real change that moves the 2 h lead's slot after the send still
	// finds it delivered — the evening time later, or a quiet end that now
	// comes before the event.
	later := prefs
	later.EveningAt = TimeOfDay{Hour: 23, Minute: 30}
	only23(t, "evening at 23:30", FollowSent(event, live, []Sent{twoHours}, later, nil, now))
	earlyEnd := prefs
	earlyEnd.QuietEnd, earlyEnd.EveningAt = TimeOfDay{Hour: 4}, TimeOfDay{Hour: 22}
	only23(t, "quiet end at 04:00", FollowSent(event, live, []Sent{twoHours}, earlyEnd, nil, now))

	// B2: a send about another instant of the same unit (the date before a
	// correction) covers nothing here.
	old := twoHours
	old.About = event.Add(-6 * time.Hour)
	if f := FollowSent(event, live, []Sent{old}, prefs, nil, now); len(f.Plans) != 2 {
		t.Errorf("a send for the old date = %+v, want both leads planned", f)
	}

	// A new lead due after an earlier one went out is armed: an 18:00
	// event, its 6 h reminder sent at noon, a 1 h lead added at 13:00.
	day := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
	dayNow := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	sixFired := Sent{About: day, Leads: []time.Duration{6 * time.Hour}, At: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
	withHour := prefs
	withHour.TimedLeads = []time.Duration{6 * time.Hour, time.Hour}
	f := FollowSent(day, nil, []Sent{sixFired}, withHour, nil, dayNow)
	if len(f.Plans) != 1 || !f.Plans[0].FireAt.Equal(day.Add(-time.Hour)) || !slices.Equal(f.Plans[0].Leads, []time.Duration{time.Hour}) {
		t.Errorf("a new 1 h lead after the 6 h one went out = %+v, want it armed at 17:00", f)
	}

	// A lead whose slot is at or before a send for the same instant is
	// covered even if no reminder served it: 1 h before 05:00 is due the
	// evening before at 22:00, when the 2 h one went out.
	withOne := prefs
	withOne.TimedLeads = []time.Duration{6 * time.Hour, 2 * time.Hour, time.Hour}
	only23(t, "a 1 h lead due at the send", FollowSent(event, live, []Sent{twoHours}, withOne, nil, now))

	// Every lead covered: the live reminder of a dropped lead stops.
	onlyTwo := prefs
	onlyTwo.TimedLeads = []time.Duration{2 * time.Hour}
	f = FollowSent(event, live, []Sent{twoHours}, onlyTwo, nil, now)
	if len(f.Plans) != 1 || f.Plans[0].What != ArmNothing || f.Plans[0].Why != RefusalCovered || len(f.Expire) != 1 {
		t.Errorf("only the sent lead = %+v, want nothing armed, covered, and the 23:00 one expired", f)
	}

	// No send: FollowSent is Follow.
	if f, g := FollowSent(event, live, nil, DefaultReminderPrefs(), nil, now), Follow(event, live, DefaultReminderPrefs(), nil, now); len(f.Plans) != len(g.Plans) {
		t.Errorf("FollowSent with no send = %+v, want Follow's %+v", f, g)
	}
}

// TestFollowSent_LeadsOneReminderServed: when two leads land on one
// instant they are one reminder, and it records both, so neither is sent
// again later. Leads 6 h, 3 h and 2 h before a 05:00 event: 3 h and 2 h
// both fall in quiet hours before an event that is before quiet end, so
// both go out the evening before at 22:00, as one.
func TestFollowSent_LeadsOneReminderServed(t *testing.T) {
	event := time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
	capture := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	prefs := DefaultReminderPrefs()
	prefs.TimedLeads = []time.Duration{6 * time.Hour, 3 * time.Hour, 2 * time.Hour}

	plans, _ := eventReminders(event, prefs, capture, ResolveInterrupt(nil), nil)
	if len(plans) != 2 || !slices.Equal(plans[0].Leads, []time.Duration{3 * time.Hour, 2 * time.Hour}) {
		t.Fatalf("capture plans = %+v, want 22:00 serving 3 h and 2 h, then 23:00", plans)
	}
	sent := []Sent{{About: event, Leads: plans[0].Leads, At: plans[0].FireAt}}
	six := time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)
	live := []Live{{ID: "six", FireAt: six}}
	now := time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)
	for name, edit := range map[string]func(p *ReminderPrefs){
		"evening at 23:30":   func(p *ReminderPrefs) { p.EveningAt = TimeOfDay{Hour: 23, Minute: 30} },
		"quiet end at 04:00": func(p *ReminderPrefs) { p.QuietEnd = TimeOfDay{Hour: 4} },
	} {
		p := prefs
		edit(&p)
		if f := FollowSent(event, live, sent, p, nil, now); len(f.Plans) != 1 || !f.Plans[0].FireAt.Equal(six) {
			t.Errorf("%s: %+v, want only the 23:00 reminder: 3 h and 2 h both went out at 22:00", name, f)
		}
	}

	// Pulled to now at capture: captured at 22:40, both the 3 h and the 2 h
	// lead are behind their evening slot and go out at once, as one.
	late := time.Date(2026, 10, 14, 22, 40, 0, 0, time.UTC)
	plans, _ = eventReminders(event, prefs, late, ResolveInterrupt(nil), nil)
	if len(plans) != 2 || !plans[0].Immediate || !slices.Equal(plans[0].Leads, []time.Duration{3 * time.Hour, 2 * time.Hour}) {
		t.Fatalf("late capture plans = %+v, want one at once serving 3 h and 2 h", plans)
	}
	sent = []Sent{{About: event, Leads: plans[0].Leads, At: late}}
	p := prefs
	p.EveningAt = TimeOfDay{Hour: 23, Minute: 30}
	if f := FollowSent(event, live, sent, p, nil, late.Add(5*time.Minute)); len(f.Plans) != 1 || !f.Plans[0].FireAt.Equal(six) {
		t.Errorf("after the at-once send: %+v, want only the 23:00 reminder", f)
	}
}
