// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/correction"
)

// i44Fires is when the unit's armed reminders ring, in order.
func (f i40Fixture) i44Fires(t *testing.T, unitID string) []time.Time {
	t.Helper()
	armed, err := f.triggers.ArmedForUnit(context.Background(), unitID)
	if err != nil {
		t.Fatal(err)
	}
	var at []time.Time
	for _, a := range armed {
		at = append(at, a.FireAt)
	}
	return at
}

func i44At(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, i40Now.Location())
}

// TestI44_ACaptureArmsTheLeadsItsMessageNames is the live case of 2026-10-20:
// "dentista mañana a las 10, avisame 1 hora antes" armed the preferences'
// hours-before ring and an asked one 15 minutes before, and said so. The leads
// the message named are the event's own: stored on the unit, the only rings
// armed, no asked reminder beside them (leads win, ADR-0043), and the answer
// names them as the user said them.
func TestI44_ACaptureArmsTheLeadsItsMessageNames(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		leads          []int
		want           []time.Time
	}{
		{"one hour before", `{"type":"event","normalized_content":"Dentista el 2026-10-15 a las 10:00.","event_at":"2026-10-15T10:00:00-03:00",` +
			`"weight":0.7,"decay_rate":0.05,"language":"es","title":"Dentista","remind_request":true,"reminder_leads_minutes":[60]}`,
			[]int{60}, []time.Time{i44At(15, 9, 0)}},
		{"the day before and half an hour before", `{"type":"event","normalized_content":"Cena con Ana el 2026-10-16 a las 21:00.",` +
			`"event_at":"2026-10-16T21:00:00-03:00","weight":0.7,"decay_rate":0.05,"language":"es","title":"Cena con Ana",` +
			`"remind_request":true,"reminder_leads_minutes":[1440,30]}`,
			[]int{1440, 30}, []time.Time{i44At(15, 21, 0), i44At(16, 20, 30)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newI40(t, tc.response)
			res := f.capture(t, "x, avisame antes")
			if res.Outcome != brain.OutcomeArmed || res.Armed == nil || !slices.Equal(res.Armed.OwnLeads, tc.leads) || res.UnitID == "" {
				t.Fatalf("result = %+v, want armed at the event's own leads", res)
			}
			u, err := f.units.ByID(context.Background(), res.UnitID)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(u.ReminderLeads, tc.leads) || u.RemindMe {
				t.Errorf("unit leads %v, remind_me %v; want %v and not asked", u.ReminderLeads, u.RemindMe, tc.leads)
			}
			if got := f.i44Fires(t, res.UnitID); !slices.EqualFunc(got, tc.want, time.Time.Equal) {
				t.Errorf("armed at %v, want exactly %v", got, tc.want)
			}
		})
	}
}

// TestI44_ANamedLeadAlreadyBehindRingsOnTheEventsDay (ADR-0045 point 3, the
// client's ruling of 2026-10-20): "dentista mañana a las 10, avisame el día
// antes", told after the day before's 10:00 has passed, never rings at once.
// The one reminder rings on the event's own day at 09:00, and the answer names
// that ring, not a lead it does not serve.
func TestI44_ANamedLeadAlreadyBehindRingsOnTheEventsDay(t *testing.T) {
	f := newI40(t, `{"type":"event","normalized_content":"Dentista el 2026-10-15 a las 10:00.","event_at":"2026-10-15T10:00:00-03:00",`+
		`"weight":0.7,"decay_rate":0.05,"language":"es","title":"Dentista","remind_request":true,"reminder_leads_minutes":[1440]}`)
	res := f.capture(t, "dentista mañana a las 10, avisame el día antes")
	if res.Outcome != brain.OutcomeArmed || res.Armed == nil || res.Armed.Immediate {
		t.Fatalf("result = %+v, want a ring armed, not at once", res)
	}
	if got := f.i44Fires(t, res.UnitID); !slices.EqualFunc(got, []time.Time{i44At(15, 9, 0)}, time.Time.Equal) {
		t.Errorf("armed at %v, want one ring on the event's day at 09:00", got)
	}
	if got, want := channels.RenderReply(res), "Anotado para el jue 15 oct, 10:00. Te aviso ese día a las 09:00."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// TestI44_LeadsACaptureCannotApplyChangeNothing: leads beside a task have no
// time of day an event lead counts back from (ADR-0038 point 3), so the
// capture is what it was: the asked task, its asked reminder, no own leads.
func TestI44_LeadsACaptureCannotApplyChangeNothing(t *testing.T) {
	f := newI40(t, `{"type":"task","normalized_content":"Llamar a Ana el 2026-10-15 a las 10:00.","due_at":"2026-10-15T10:00:00-03:00",`+
		`"weight":0.7,"decay_rate":0.05,"language":"es","title":"Llamar a Ana","remind_request":true,"reminder_leads_minutes":[60]}`)
	res := f.capture(t, "llamar a Ana mañana a las 10, avisame 1 hora antes")
	if res.Outcome != brain.OutcomeArmed || res.Armed == nil || res.Armed.OwnLeads != nil {
		t.Fatalf("result = %+v, want the asked reminder armed as before", res)
	}
	u, err := f.units.ByID(context.Background(), res.UnitID)
	if err != nil {
		t.Fatal(err)
	}
	if u.ReminderLeads != nil || !u.RemindMe {
		t.Errorf("unit leads %v, remind_me %v; want none and asked", u.ReminderLeads, u.RemindMe)
	}
	if got := f.i44Fires(t, res.UnitID); !slices.EqualFunc(got, []time.Time{i44At(15, 9, 45)}, time.Time.Equal) {
		t.Errorf("armed at %v, want the asked reminder at 09:45", got)
	}
}

// TestI44_AnAddedLeadKeepsTheItemsReminders is the live case of 2026-10-20:
// "también 1 hora antes" after "mañana tengo dentista a las 10" replaced the
// hours-before ring with the hour-before one. A reading that adds its leads
// joins them to every reminder the item has: its own leads, else the leads its
// armed reminders serve.
func TestI44_AnAddedLeadKeepsTheItemsReminders(t *testing.T) {
	added := `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 22:30.",` +
		`"weight":0.7,"decay_rate":0.05,"language":"es","referent":"A","reminder_leads_minutes":[60],"reminder_leads_add":true}`
	t.Run("to the leads its armed reminders serve", func(t *testing.T) {
		f := newI40(t, added)
		f.armMeeting(t)
		res := f.capture(t, "también 1 hora antes")
		if res.Outcome != brain.OutcomeCorrected || res.Correction == nil ||
			!slices.Equal(res.Correction.Fields, []correction.Field{correction.FieldReminderLeads}) {
			t.Fatalf("result = %+v, want the meeting's leads corrected", res)
		}
		if got := f.meeting(t).ReminderLeads; !slices.Equal(got, []int{120, 60}) {
			t.Errorf("leads = %v, want [120 60]", got)
		}
		want := []time.Time{i40Meeting.Add(-2 * time.Hour), i40Meeting.Add(-time.Hour)}
		if got := f.i44Fires(t, "meeting"); !slices.EqualFunc(got, want, time.Time.Equal) {
			t.Errorf("armed at %v, want %v", got, want)
		}
		if got := res.Correction.Reminders; !slices.EqualFunc(got, want, time.Time.Equal) {
			t.Errorf("Reminders = %v, want both named, %v", got, want)
		}
	})
	t.Run("to its own leads", func(t *testing.T) {
		f := newI40(t, added)
		if err := f.units.UpdateReminderLeads(context.Background(), "meeting", []int{30}, i40Now); err != nil {
			t.Fatal(err)
		}
		f.capture(t, "también 1 hora antes")
		if got := f.meeting(t).ReminderLeads; !slices.Equal(got, []int{60, 30}) {
			t.Errorf("leads = %v, want [60 30]", got)
		}
		want := []time.Time{i40Meeting.Add(-time.Hour), i40Meeting.Add(-30 * time.Minute)}
		if got := f.i44Fires(t, "meeting"); !slices.EqualFunc(got, want, time.Time.Equal) {
			t.Errorf("armed at %v, want %v", got, want)
		}
	})
}
