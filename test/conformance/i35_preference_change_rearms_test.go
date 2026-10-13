// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// i35Vault is a vault the preferences page saves into: units, their armed
// triggers, and the service under test.
type i35Vault struct {
	units    *memrepo.Units
	triggers *memrepo.Triggers
	log      *memrepo.DecisionLog
	cfg      *memrepo.Config
	ids      *counterIDs
	svc      *brain.PreferencesService
}

func newI35(t *testing.T, now time.Time) i35Vault {
	t.Helper()
	v := i35Vault{units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(), log: memrepo.NewDecisionLog(), cfg: memrepo.NewConfig(), ids: &counterIDs{}}
	v.svc = v.at(now)
	return v
}

// at is the same vault's service read at another instant.
func (v i35Vault) at(now time.Time) *brain.PreferencesService {
	return brain.NewPreferencesService(fixedClock{now: now}, v.ids, v.cfg, v.log, v.units, v.triggers)
}

func (v i35Vault) unit(t *testing.T, id string, typ unit.Type, eventAt time.Time, now time.Time) {
	t.Helper()
	u := unit.Unit{ID: id, Type: typ, Status: unit.StatusPool, Content: id, EventAt: &eventAt, Source: "cli", CreatedAt: now, UpdatedAt: now}
	if err := v.units.Create(context.Background(), u); err != nil {
		t.Fatalf("seeding unit %s: %v", id, err)
	}
}

func (v i35Vault) arm(t *testing.T, id, unitID string, fireAt time.Time, leadMinutes int, rule *prospection.Rule) {
	t.Helper()
	v.armAt(t, id, unitID, fireAt, leadMinutes, rule, nil)
}

func (v i35Vault) armAt(t *testing.T, id, unitID string, fireAt time.Time, leadMinutes int, rule *prospection.Rule, level *float64) {
	t.Helper()
	if err := v.triggers.Create(context.Background(), ports.Trigger{ID: id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: unitID, LeadMinutes: leadMinutes}, FireAt: &fireAt, RecurrenceRule: rule, InterruptLevel: level}); err != nil {
		t.Fatalf("seeding trigger %s: %v", id, err)
	}
}

func (v i35Vault) armedAt(t *testing.T, unitID string) []time.Time {
	t.Helper()
	got, err := v.triggers.ArmedForUnit(context.Background(), unitID)
	if err != nil {
		t.Fatalf("ArmedForUnit: %v", err)
	}
	out := make([]time.Time, len(got))
	for i, g := range got {
		out[i] = g.FireAt
	}
	return out
}

func (v i35Vault) count(t *testing.T, action ports.DecisionAction) int {
	t.Helper()
	all, err := v.log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	n := 0
	for _, d := range all {
		if d.Action == action {
			n++
		}
	}
	return n
}

func i35Defaults() brain.Preferences {
	return brain.Preferences{LeadMinutes: []int{1440, 120}, DateOnlyAt: "09:00", QuietEnd: "07:00", EveningAt: "22:00"}
}

func sameInstants(a, b []time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

// TestI35_ASaveThatChangesNothingTouchesNoReminder: re-planning a set under
// the same preferences is not a no-op — Follow sees only live triggers, so a
// lead whose evening-pulled reminder already went out would be planned again
// at once. Leads 6 h and 2 h, an event at 05:00 on day 3: the 2 h reminder
// went out at 22:00, the 6 h one waits at 23:00. A save at 22:30 that
// changes nothing must leave the 23:00 one alone and arm nothing.
func TestI35_ASaveThatChangesNothingTouchesNoReminder(t *testing.T) {
	night := time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)
	event := time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
	p := i35Defaults()
	p.LeadMinutes = []int{360, 120}

	for _, history := range []string{"never changed", "a change whose re-arm completed"} {
		t.Run(history, func(t *testing.T) {
			v := newI35(t, night)
			if history != "never changed" {
				if _, err := v.at(night.Add(-36*time.Hour)).Save(context.Background(), p); err != nil {
					t.Fatalf("the earlier change: %v", err)
				}
			} else {
				leads := "[360,120]"
				v.cfg.SeedConfig(t, ports.VaultConfig{EventReminderLeads: &leads})
			}
			v.unit(t, "flight", unit.TypeEvent, event, night)
			v.arm(t, "flight-6h", "flight", time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC), 360, nil)
			before := len(v.rowsOrFail(t))

			res, err := v.svc.Save(context.Background(), p)
			if err != nil || len(res.Changed) != 0 {
				t.Fatalf("Save = %+v, %v; want nothing changed", res, err)
			}
			if got, want := v.armedAt(t, "flight"), []time.Time{time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)}; !sameInstants(got, want) {
				t.Errorf("flight reminders = %v, want only the 23:00 one, untouched", got)
			}
			if after := len(v.rowsOrFail(t)); after != before {
				t.Errorf("a save that changed nothing wrote %d decision rows", after-before)
			}
		})
	}
}

// i35ReadFails fails ArmedForUnit for one unit, so a re-arm stops part way
// after the units before it.
type i35ReadFails struct {
	*memrepo.Triggers
	unit string
}

func (f *i35ReadFails) ArmedForUnit(ctx context.Context, unitID string) ([]ports.DueTrigger, error) {
	if unitID == f.unit {
		return nil, errors.New("disk")
	}
	return f.Triggers.ArmedForUnit(ctx, unitID)
}

// TestI35_APendingReArmIsFinishedByTheNextSave is I35's pending clause: a
// change whose re-arm stopped before the "zoo" trip (a date-only event,
// reminded the day before at 09:00) is finished by the next save even
// though that one changes nothing, and the save after it writes nothing.
func TestI35_APendingReArmIsFinishedByTheNextSave(t *testing.T) {
	ctx := context.Background()
	evening := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	zoo := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	v := newI35(t, evening)
	v.unit(t, "zoo", unit.TypeEvent, zoo, evening)
	v.arm(t, "zoo-day-before", "zoo", time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC), 900, nil)
	p := i35Defaults()
	p.DateOnlyAt = "08:00"

	broken := brain.NewPreferencesService(fixedClock{now: evening}, v.ids, v.cfg, v.log, v.units, &i35ReadFails{Triggers: v.triggers, unit: "zoo"})
	if _, err := broken.Save(ctx, p); !errors.Is(err, brain.ErrRemindersNotFollowed) {
		t.Fatalf("Save at 20:00 = %v, want the re-arm to stop at zoo", err)
	}
	later := v.at(evening.Add(2 * time.Hour))
	if res, err := later.Save(ctx, p); err != nil || len(res.Changed) != 0 {
		t.Fatalf("Save at 22:00 = %+v, %v; want nothing changed and no error", res, err)
	}
	if got := v.armedAt(t, "zoo"); !sameInstants(got, []time.Time{time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)}) {
		t.Errorf("zoo reminders = %v, want the pending re-arm to move it to 08:00", got)
	}
	before := len(v.rowsOrFail(t))
	if _, err := later.Save(ctx, p); err != nil || len(v.rowsOrFail(t)) != before {
		t.Errorf("a third save = %v and wrote %d rows, want nothing: the re-arm completed", err, len(v.rowsOrFail(t))-before)
	}
}

// TestI35_AReminderAlreadySentIsNeverSentAgain: a reminder that went out is
// never sent again because of a preference save — not by a change that
// does not touch the event, and not by the save that finishes a re-arm that
// stopped part way. Leads 6 h and 2 h, a flight at 05:00: its 2 h reminder
// goes out the evening before at 22:00 and its 6 h one waits at 23:00. A
// date-only event, the "zoo" trip, is reminded the day before at 09:00.
func TestI35_AReminderAlreadySentIsNeverSentAgain(t *testing.T) {
	ctx := context.Background()
	flight := time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
	zoo := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	evening := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	sent := time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC)
	late := time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)
	six := time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)

	setup := func(t *testing.T) (i35Vault, brain.Preferences) {
		v := newI35(t, evening)
		leads := "[360,120]"
		v.cfg.SeedConfig(t, ports.VaultConfig{EventReminderLeads: &leads})
		v.unit(t, "flight", unit.TypeEvent, flight, evening)
		v.arm(t, "flight-2h", "flight", sent, 420, nil)
		v.arm(t, "flight-6h", "flight", six, 360, nil)
		v.unit(t, "zoo", unit.TypeEvent, zoo, evening)
		v.arm(t, "zoo-day-before", "zoo", time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC), 900, nil)
		p := i35Defaults()
		p.LeadMinutes = []int{360, 120}
		return v, p
	}
	fire := func(t *testing.T, v i35Vault) {
		if err := v.triggers.Fire(ctx, "flight-2h", sent); err != nil {
			t.Fatalf("Fire: %v", err)
		}
	}
	assertFlight := func(t *testing.T, v i35Vault) {
		t.Helper()
		if got := v.armedAt(t, "flight"); !sameInstants(got, []time.Time{six}) {
			t.Errorf("flight reminders = %v, want only the 6 h one at 23:00: the 2 h one already went out", got)
		}
	}

	t.Run("a change that does not touch the event", func(t *testing.T) {
		v, p := setup(t)
		fire(t, v)
		p.DateOnlyAt = "08:00"
		if _, err := v.at(late).Save(ctx, p); err != nil {
			t.Fatalf("Save: %v", err)
		}
		assertFlight(t, v)
	})

	t.Run("the save that finishes a re-arm that stopped part way", func(t *testing.T) {
		v, p := setup(t)
		broken := brain.NewPreferencesService(fixedClock{now: evening}, v.ids, v.cfg, v.log, v.units, &i35ReadFails{Triggers: v.triggers, unit: "zoo"})
		p.DateOnlyAt = "08:00"
		if _, err := broken.Save(ctx, p); !errors.Is(err, brain.ErrRemindersNotFollowed) {
			t.Fatalf("Save at 20:00 = %v, want the re-arm to stop at zoo", err)
		}
		if got := v.armedAt(t, "zoo"); !sameInstants(got, []time.Time{time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}) {
			t.Fatalf("zoo reminders = %v, want them not reached yet", got)
		}
		fire(t, v)

		// The pending clause: a save that changes nothing finishes the
		// re-arm, zoo moving to the new 08:00, and sends nothing again.
		res, err := v.at(late).Save(ctx, p)
		if err != nil || len(res.Changed) != 0 {
			t.Fatalf("Save at 22:30 = %+v, %v; want nothing changed and no error", res, err)
		}
		if got := v.armedAt(t, "zoo"); !sameInstants(got, []time.Time{time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)}) {
			t.Errorf("zoo reminders = %v, want the pending re-arm to move it to 08:00", got)
		}
		assertFlight(t, v)
	})
}

// serve seeds a one-shot reminder as capture now arms one: the event
// instant it is about and every user lead it serves (none for one at once).
func (v i35Vault) serve(t *testing.T, id, unitID string, fireAt, about time.Time, leadMinutes int, userLeads ...int) {
	t.Helper()
	if err := v.triggers.Create(context.Background(), ports.Trigger{ID: id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: unitID, LeadMinutes: leadMinutes, About: about, UserLeadsMinutes: userLeads}, FireAt: &fireAt}); err != nil {
		t.Fatalf("seeding trigger %s: %v", id, err)
	}
}

// TestI35_WhatWentOutIsTiedToItsLeadAndItsDate: coverage is what was
// delivered, not a time under the new preferences. A real change that moves
// a sent lead's slot later does not send it again (B1); a send about the
// date before a correction covers nothing for the new date (B2); a new lead
// due after an earlier one went out is armed; an event with nothing armed
// is not re-armed at all.
func TestI35_WhatWentOutIsTiedToItsLeadAndItsDate(t *testing.T) {
	ctx := context.Background()
	flight := time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
	late := time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)
	sixPM, sent := time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC), time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC)
	for name, edit := range map[string]func(p *brain.Preferences){
		"the evening time later than the send": func(p *brain.Preferences) { p.EveningAt = "23:30" },
		"a quiet end before the event":         func(p *brain.Preferences) { p.QuietEnd = "04:00" },
	} {
		t.Run(name, func(t *testing.T) {
			v := newI35(t, late)
			v.unit(t, "flight", unit.TypeEvent, flight, late)
			v.serve(t, "flight-2h", "flight", sent, flight, 420, 120)
			v.serve(t, "flight-6h", "flight", sixPM, flight, 360, 360)
			if err := v.triggers.Fire(ctx, "flight-2h", sent); err != nil {
				t.Fatalf("Fire: %v", err)
			}
			p := i35Defaults()
			p.LeadMinutes = []int{360, 120}
			edit(&p)
			if _, err := v.svc.Save(ctx, p); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if got := v.armedAt(t, "flight"); !sameInstants(got, []time.Time{sixPM}) {
				t.Errorf("flight reminders = %v, want only 23:00: the 2 h reminder already went out", got)
			}
		})
	}

	t.Run("a send about the date before a correction", func(t *testing.T) {
		// Dinner was at 22:40 and its 30 min reminder went out at 22:10. At
		// 22:20 a correction moved it to 05:00 and armed the 30 min lead at
		// once, as Follow does; 20 seconds later a save changes the
		// date-only time. The reminder for the new date stays.
		at := time.Date(2026, 10, 14, 22, 20, 20, 0, time.UTC)
		v := newI35(t, at)
		old, moved := time.Date(2026, 10, 14, 22, 40, 0, 0, time.UTC), time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
		v.unit(t, "dinner", unit.TypeEvent, moved, at)
		v.serve(t, "dinner-old", "dinner", old.Add(-30*time.Minute), old, 30, 30)
		if err := v.triggers.Fire(ctx, "dinner-old", old.Add(-30*time.Minute)); err != nil {
			t.Fatalf("Fire: %v", err)
		}
		corrected := time.Date(2026, 10, 14, 22, 20, 0, 0, time.UTC)
		v.serve(t, "dinner-new", "dinner", corrected, moved, 0, 30)
		p := i35Defaults()
		p.LeadMinutes, p.DateOnlyAt = []int{30}, "08:00"
		if _, err := v.svc.Save(ctx, p); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if got := v.armedAt(t, "dinner"); !sameInstants(got, []time.Time{corrected}) {
			t.Errorf("dinner reminders = %v, want the corrected date's reminder kept", got)
		}
	})

	t.Run("a new lead after an earlier one went out, and an event with nothing armed", func(t *testing.T) {
		noon := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
		v := newI35(t, noon)
		event := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
		v.unit(t, "talk", unit.TypeEvent, event, noon)
		v.serve(t, "talk-6h", "talk", event.Add(-6*time.Hour), event, 360, 360)
		v.serve(t, "talk-2h", "talk", event.Add(-2*time.Hour), event, 120, 120)
		v.unit(t, "done", unit.TypeEvent, event, noon)
		v.serve(t, "done-6h", "done", event.Add(-6*time.Hour), event, 360, 360)
		for _, id := range []string{"talk-6h", "done-6h"} {
			if err := v.triggers.Fire(ctx, id, event.Add(-6*time.Hour)); err != nil {
				t.Fatalf("Fire: %v", err)
			}
		}
		p := i35Defaults()
		p.LeadMinutes = []int{360, 120, 60}
		if _, err := v.svc.Save(ctx, p); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if got := v.armedAt(t, "talk"); !sameInstants(got, []time.Time{event.Add(-2 * time.Hour), event.Add(-time.Hour)}) {
			t.Errorf("talk reminders = %v, want 2 h and the new 1 h, the sent 6 h not again", got)
		}
		if got := v.armedAt(t, "done"); len(got) != 0 {
			t.Errorf("an event with nothing armed got %v, want it skipped (doc 02 §7)", got)
		}
	})
}

// TestI35_OneReminderServingTwoLeadsCoversBoth: leads 6 h, 3 h and 2 h
// before a 05:00 flight. 3 h and 2 h land on one instant, so one reminder
// serves both, and a change after it went out sends neither again — when
// it went out the evening before at 22:00, or at once because the flight
// was captured at 22:40. A row from before leads were recorded, pulled to
// now (lead_minutes 0), is not sent again either.
func TestI35_OneReminderServingTwoLeadsCoversBoth(t *testing.T) {
	ctx := context.Background()
	flight := time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
	six := time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)
	run := func(t *testing.T, now, sentAt time.Time, seed func(v i35Vault), edit func(p *brain.Preferences)) {
		t.Helper()
		v := newI35(t, now)
		v.unit(t, "flight", unit.TypeEvent, flight, now)
		seed(v)
		v.serve(t, "flight-6h", "flight", six, flight, 360, 360)
		if err := v.triggers.Fire(ctx, "flight-sent", sentAt); err != nil {
			t.Fatalf("Fire: %v", err)
		}
		p := i35Defaults()
		p.LeadMinutes = []int{360, 180, 120}
		edit(&p)
		if _, err := v.svc.Save(ctx, p); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if got := v.armedAt(t, "flight"); !sameInstants(got, []time.Time{six}) {
			t.Errorf("flight reminders = %v, want only 23:00: what went out is not sent again", got)
		}
	}
	evening := time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC)
	half := time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)
	pulled := time.Date(2026, 10, 14, 22, 40, 0, 0, time.UTC)
	t.Run("evening collapse, evening time later", func(t *testing.T) {
		run(t, half, evening, func(v i35Vault) { v.serve(t, "flight-sent", "flight", evening, flight, 420, 180, 120) },
			func(p *brain.Preferences) { p.EveningAt = "23:30" })
	})
	t.Run("evening collapse, quiet end before the event", func(t *testing.T) {
		run(t, half, evening, func(v i35Vault) { v.serve(t, "flight-sent", "flight", evening, flight, 420, 180, 120) },
			func(p *brain.Preferences) { p.QuietEnd = "04:00" })
	})
	t.Run("pulled to now at capture", func(t *testing.T) {
		run(t, pulled.Add(5*time.Minute), pulled, func(v i35Vault) { v.serve(t, "flight-sent", "flight", pulled, flight, 0, 180, 120) },
			func(p *brain.Preferences) { p.EveningAt = "23:30" })
	})
	t.Run("a row from before leads were recorded, pulled to now", func(t *testing.T) {
		run(t, pulled.Add(5*time.Minute), pulled, func(v i35Vault) { v.arm(t, "flight-sent", "flight", pulled, 0, nil) },
			func(p *brain.Preferences) { p.DateOnlyAt = "08:00" })
	})
}

// TestI35_ALeadMergedByAReArmIsRecorded goes through real re-arms, not a
// seeded payload: leads 6 h and 3 h before a 05:00 flight; at 20:00 a save
// adds 2 h, which lands on the 3 h reminder at 22:00 (the evening before).
// That reminder must now record both leads, so after it goes out a later
// evening time sends neither again. A row from before leads were recorded,
// carried at an unchanged time, gains its event instant the same way.
func TestI35_ALeadMergedByAReArmIsRecorded(t *testing.T) {
	ctx := context.Background()
	flight := time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
	ten, eleven := time.Date(2026, 10, 14, 22, 0, 0, 0, time.UTC), time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)
	evening := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	v := newI35(t, evening)
	leads := "[360,180]"
	v.cfg.SeedConfig(t, ports.VaultConfig{EventReminderLeads: &leads})
	v.unit(t, "flight", unit.TypeEvent, flight, evening)
	v.serve(t, "flight-3h", "flight", ten, flight, 420, 180)
	v.serve(t, "flight-6h", "flight", eleven, flight, 360, 360)

	p := i35Defaults()
	p.LeadMinutes = []int{360, 180, 120}
	if _, err := v.svc.Save(ctx, p); err != nil {
		t.Fatalf("Save adding 2 h: %v", err)
	}
	armed, _ := v.triggers.ArmedForUnit(ctx, "flight")
	if len(armed) != 2 || armed[0].ID != "flight-3h" || !slices.Equal(armed[0].Payload.UserLeadsMinutes, []int{180, 120}) {
		t.Fatalf("after adding 2 h = %+v, want flight-3h at 22:00 serving 3 h and 2 h", armed)
	}
	if err := v.triggers.Fire(ctx, "flight-3h", ten); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	p.EveningAt = "23:30"
	if _, err := v.at(time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)).Save(ctx, p); err != nil {
		t.Fatalf("Save at 22:30: %v", err)
	}
	if got := v.armedAt(t, "flight"); !sameInstants(got, []time.Time{eleven}) {
		t.Errorf("flight reminders = %v, want only 23:00: 3 h and 2 h both went out at 22:00", got)
	}

	t.Run("a row from before leads were recorded gains them", func(t *testing.T) {
		v := newI35(t, evening)
		v.unit(t, "flight", unit.TypeEvent, flight, evening)
		v.arm(t, "flight-6h", "flight", eleven, 360, nil)
		p := i35Defaults()
		p.LeadMinutes = []int{360}
		if _, err := v.svc.Save(ctx, p); err != nil {
			t.Fatalf("Save: %v", err)
		}
		armed, _ := v.triggers.ArmedForUnit(ctx, "flight")
		if len(armed) != 1 || !armed[0].FireAt.Equal(eleven) || !armed[0].Payload.About.Equal(flight) || !slices.Equal(armed[0].Payload.UserLeadsMinutes, []int{360}) {
			t.Errorf("carried legacy row = %+v, want it at 23:00, about 05:00, serving 6 h", armed)
		}
	})
}

// TestI35_ACreatedReminderTakesTheEarliestArmedLevel: a reminder a change
// creates for an event takes the interrupt level of the event's earliest
// armed reminder (doc 02 §7), not the default and not another reminder's.
func TestI35_ACreatedReminderTakesTheEarliestArmedLevel(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	event := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	v := newI35(t, now)
	v.unit(t, "flight", unit.TypeEvent, event, now)
	early, late := 0.9, 0.3
	v.armAt(t, "flight-24h", "flight", event.Add(-24*time.Hour), 1440, nil, &early)
	v.armAt(t, "flight-2h", "flight", event.Add(-2*time.Hour), 120, nil, &late)

	p := i35Defaults()
	p.LeadMinutes = []int{1440, 120, 30}
	if _, err := v.svc.Save(context.Background(), p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	armed, err := v.triggers.ArmedForUnit(context.Background(), "flight")
	if err != nil || len(armed) != 3 {
		t.Fatalf("armed = %+v, %v; want three reminders", armed, err)
	}
	created := armed[2]
	if !created.FireAt.Equal(event.Add(-30*time.Minute)) || created.InterruptLevel == nil || *created.InterruptLevel != early {
		t.Errorf("created reminder = %+v (level %v), want 30 minutes before at the earliest reminder's level %v", created, created.InterruptLevel, early)
	}
}

// TestI35_APreferenceChangeReArmsEventsAhead is ADR-0029 point 6: a change
// of the reminder preferences re-arms every armed one-shot reminder of every
// event still ahead, through the decision a corrected date uses, each move
// recorded; an event already past, a recurring reminder and a non-event
// keep theirs; saving the same values again writes nothing.
func TestI35_APreferenceChangeReArmsEventsAhead(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()

	flight := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	birthday := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	task := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	v := newI35(t, now)
	v.unit(t, "flight", unit.TypeEvent, flight, now)
	v.arm(t, "flight-24h", "flight", flight.Add(-24*time.Hour), 1440, nil)
	v.arm(t, "flight-2h", "flight", flight.Add(-2*time.Hour), 120, nil)
	v.unit(t, "gone", unit.TypeEvent, past, now)
	v.arm(t, "gone-2h", "gone", past.Add(-2*time.Hour+30*time.Minute), 120, nil)
	v.unit(t, "birthday", unit.TypeEvent, birthday, now)
	yearly := prospection.RuleYearly
	v.arm(t, "birthday-7d", "birthday", birthday.AddDate(0, 0, -7), 0, &yearly)
	v.unit(t, "bill", unit.TypeTask, task, now)
	v.arm(t, "bill-due", "bill", task.Add(-time.Hour), 0, nil)
	untouched := map[string][]time.Time{}
	for _, id := range []string{"gone", "birthday", "bill"} {
		untouched[id] = v.armedAt(t, id)
	}

	p := i35Defaults()
	p.LeadMinutes = []int{180}
	if _, err := v.svc.Save(ctx, p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got, want := v.armedAt(t, "flight"), []time.Time{flight.Add(-3 * time.Hour)}; !sameInstants(got, want) {
		t.Errorf("flight reminders = %v, want only 3 hours before, %v", got, want)
	}
	if v.count(t, ports.ActionConfigReminderMoved) != 1 || v.count(t, ports.ActionConfigReminderCancelled) != 1 {
		t.Errorf("rows: %d moved, %d cancelled; want one of each", v.count(t, ports.ActionConfigReminderMoved), v.count(t, ports.ActionConfigReminderCancelled))
	}
	for id, want := range untouched {
		if got := v.armedAt(t, id); !sameInstants(got, want) {
			t.Errorf("%s reminders = %v, want %v untouched", id, got, want)
		}
	}

	// Two leads where there was one: the missing one is armed.
	p.LeadMinutes = []int{180, 30}
	if _, err := v.svc.Save(ctx, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got, want := v.armedAt(t, "flight"), []time.Time{flight.Add(-3 * time.Hour), flight.Add(-30 * time.Minute)}; !sameInstants(got, want) {
		t.Errorf("flight reminders = %v, want %v", got, want)
	}
	if v.count(t, ports.ActionConfigReminderArmed) != 1 {
		t.Errorf("armed rows = %d, want 1", v.count(t, ports.ActionConfigReminderArmed))
	}

	// The same values again move nothing and record nothing.
	before := len(v.rowsOrFail(t))
	if _, err := v.svc.Save(ctx, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if after := len(v.rowsOrFail(t)); after != before {
		t.Errorf("saving the same values again wrote %d rows", after-before)
	}
}

// TestI35_AQuietEndChangeMovesTheReminderItShifted: the night is a
// preference too. An 08:00 event's 2-hour reminder falls at 06:00 and is
// armed at the 07:00 quiet end; moving the quiet end to 07:30 moves it.
func TestI35_AQuietEndChangeMovesTheReminderItShifted(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	event := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	v := newI35(t, now)
	v.unit(t, "train", unit.TypeEvent, event, now)
	v.arm(t, "train-24h", "train", event.Add(-24*time.Hour), 1440, nil)
	v.arm(t, "train-2h", "train", time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC), 120, nil)

	p := i35Defaults()
	p.QuietEnd = "07:30"
	if _, err := v.svc.Save(context.Background(), p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	want := []time.Time{event.Add(-24 * time.Hour), time.Date(2026, 10, 15, 7, 30, 0, 0, time.UTC)}
	if got := v.armedAt(t, "train"); !sameInstants(got, want) {
		t.Errorf("train reminders = %v, want %v", got, want)
	}
}

func (v i35Vault) rowsOrFail(t *testing.T) []ports.Decision {
	t.Helper()
	all, err := v.log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	return all
}
