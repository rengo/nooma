// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
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

// TestI44_AnAddedLeadKeepsTheAskedRingAsked (review on #383): an item the user
// asked to be reminded of keeps its asked ring through an added lead, asked and
// exempt from the night, beside the joined leads; the asked lead never becomes
// one of the event's own.
func TestI44_AnAddedLeadKeepsTheAskedRingAsked(t *testing.T) {
	f := newI40With(t, `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 22:30.",`+
		`"weight":0.7,"decay_rate":0.05,"language":"es","referent":"A","reminder_leads_minutes":[60],"reminder_leads_add":true}`,
		func(u *unit.Unit) { u.RemindMe = true })
	f.armMeeting(t)
	unitID, asked := "meeting", i40Meeting.Add(-15*time.Minute)
	if err := f.triggers.Create(context.Background(), ports.Trigger{ID: "r-asked", UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "Reunión con Juan", LeadMinutes: 15, UserLeadsMinutes: []int{15}, About: i40Meeting,
			Asked: true, NightExempt: true}, FireAt: &asked}); err != nil {
		t.Fatal(err)
	}
	f.capture(t, "también 1 hora antes")
	if got := f.meeting(t).ReminderLeads; !slices.Equal(got, []int{120, 60}) {
		t.Errorf("leads = %v, want [120 60], the asked lead kept apart", got)
	}
	armed, err := f.triggers.ArmedForUnit(context.Background(), "meeting")
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Time{i40Meeting.Add(-2 * time.Hour), i40Meeting.Add(-time.Hour), asked}
	var at []time.Time
	for _, a := range armed {
		at = append(at, a.FireAt)
		if a.FireAt.Equal(asked) && (!a.Payload.Asked || !a.Payload.NightExempt) {
			t.Errorf("the 22:15 ring = %+v, want it still asked and night-exempt", a.Payload)
		}
	}
	if !slices.EqualFunc(at, want, time.Time.Equal) {
		t.Errorf("armed at %v, want %v", at, want)
	}
}

// TestI44_AFirstMessageArmsTheLeadsItNames is the live case of the review
// round on #383: "dentista mañana a las 10, avisame 1 hora antes" as the first
// message, with no chat yet, still armed 07:00 and 09:45. Leads are read with
// no chat too.
func TestI44_AFirstMessageArmsTheLeadsItNames(t *testing.T) {
	f := newI40(t, `{"type":"event","normalized_content":"Dentista el 2026-10-15 a las 10:00.","event_at":"2026-10-15T10:00:00-03:00",`+
		`"weight":0.7,"decay_rate":0.05,"language":"es","title":"Dentista","remind_request":true,"reminder_leads_minutes":[60]}`)
	res, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: "dentista mañana a las 10, avisame 1 hora antes", Channel: "telegram"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.llm.prompts[0], "reminder_leads_minutes") || strings.Contains(f.llm.prompts[0], "Recent conversation") {
		t.Fatalf("the prompt with no chat does not read leads, or shows a chat")
	}
	if res.Outcome != brain.OutcomeArmed || res.Armed == nil || !slices.Equal(res.Armed.OwnLeads, []int{60}) {
		t.Fatalf("result = %+v, want armed at the event's own lead", res)
	}
	if got := f.i44Fires(t, res.UnitID); !slices.EqualFunc(got, []time.Time{i44At(15, 9, 0)}, time.Time.Equal) {
		t.Errorf("armed at %v, want one ring at 09:00", got)
	}
}

// TestI44_StrayLeadsArmNothingForAnUnaskedTask: leads beside a dated task
// nobody asked to be reminded of change nothing: it is stored, nothing is
// armed, and its day's digest names it, as before (the live "el viernes llevo
// el auto al service").
func TestI44_StrayLeadsArmNothingForAnUnaskedTask(t *testing.T) {
	f := newI40(t, `{"type":"task","normalized_content":"Llevar el auto al service el 2026-10-16.","due_at":"2026-10-16",`+
		`"weight":0.6,"decay_rate":0.05,"language":"es","title":"Auto al service","remind_request":false,"reminder_leads_minutes":[1440]}`)
	res := f.capture(t, "el viernes llevo el auto al service")
	if res.Outcome != brain.OutcomeStored || res.Armed != nil || res.DueInDigest == nil {
		t.Fatalf("result = %+v, want stored, nothing armed, named in its day's digest", res)
	}
	if got := f.i44Fires(t, res.UnitID); len(got) != 0 {
		t.Errorf("armed at %v, want nothing", got)
	}
}
