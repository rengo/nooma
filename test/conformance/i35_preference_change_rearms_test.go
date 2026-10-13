// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
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
	svc      *brain.PreferencesService
}

func newI35(t *testing.T, now time.Time) i35Vault {
	t.Helper()
	v := i35Vault{units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(), log: memrepo.NewDecisionLog()}
	v.svc = brain.NewPreferencesService(fixedClock{now: now}, &counterIDs{}, memrepo.NewConfig(), v.log, v.units, v.triggers)
	return v
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
	if err := v.triggers.Create(context.Background(), ports.Trigger{ID: id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: unitID, LeadMinutes: leadMinutes}, FireAt: &fireAt, RecurrenceRule: rule}); err != nil {
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
	v.arm(t, "train-2h", "train", time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC), 60, nil)

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
