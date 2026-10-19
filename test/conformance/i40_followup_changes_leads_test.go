// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// i40Leads answers "avisame solo 1 hora antes" about the meeting, A, with the
// leads the model read and the item's text unchanged.
func i40Leads(leads string) string {
	return `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 22:30.",` +
		`"weight":0.7,"decay_rate":0.05,"language":"es","referent":"A","reminder_leads_minutes":` + leads + `}`
}

// armMeeting seeds the meeting's reminder as a capture arms it: the 2-hour
// lead, the 24-hour one being behind.
func (f i40Fixture) armMeeting(t *testing.T) {
	t.Helper()
	unitID, fire := "meeting", i40Meeting.Add(-2*time.Hour)
	if err := f.triggers.Create(context.Background(), ports.Trigger{ID: "r-2h", UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "Reunión con Juan", LeadMinutes: 120, UserLeadsMinutes: []int{120}, About: i40Meeting},
		FireAt:  &fire}); err != nil {
		t.Fatal(err)
	}
}

func (f i40Fixture) armedAt(t *testing.T) []time.Time {
	t.Helper()
	armed, err := f.triggers.ArmedForUnit(context.Background(), "meeting")
	if err != nil {
		t.Fatal(err)
	}
	var at []time.Time
	for _, a := range armed {
		at = append(at, a.FireAt)
	}
	return at
}

// TestI40_AFollowUpReplacesTheEventsLeads is the client's case of 2026-10-19:
// "avisame solo 1 hora antes" answered "Corregido." while nothing about the
// reminders changed. The leads the model read are the event's own now: they
// are stored on the unit with a pre-image, the armed reminder is re-planned to
// them as a fresh capture with those leads would arm, the text stays, and the
// result carries the reminders now armed for the reply to name.
func TestI40_AFollowUpReplacesTheEventsLeads(t *testing.T) {
	f := newI40(t, i40Leads(`[60]`))
	f.armMeeting(t)
	res := f.capture(t, "avisame solo 1 hora antes")

	if res.Outcome != brain.OutcomeCorrected || res.Correction == nil ||
		!slices.Equal(res.Correction.Fields, []correction.Field{correction.FieldReminderLeads}) {
		t.Fatalf("result = %+v, want the meeting's leads corrected and nothing else", res)
	}
	got := f.meeting(t)
	if !slices.Equal(got.ReminderLeads, []int{60}) || got.Content != "Tengo reunión con Juan el 2026-10-14 a las 22:30." ||
		got.Title != "Reunión con Juan" || !got.EventAt.Equal(i40Meeting) {
		t.Errorf("meeting = %+v, want only its leads set to [60]", got)
	}
	want := []time.Time{i40Meeting.Add(-time.Hour)}
	if at := f.armedAt(t); len(at) != 1 || !at[0].Equal(want[0]) {
		t.Errorf("armed at %v, want exactly one reminder at %v", at, want[0])
	}
	if r := res.Correction.Reminders; len(r) != 1 || !r[0].Equal(want[0]) || r[0].Location() != i40Now.Location() {
		t.Errorf("Reminders = %v, want %v in the user's zone", r, want)
	}
	if !res.Correction.About.Equal(i40Meeting) {
		t.Errorf("About = %v, want the meeting's instant, what the reminders are before", res.Correction.About)
	}
	rows := i40Actions(t, f.decisions)
	var pre struct {
		Fields   []string       `json:"fields"`
		Previous map[string]any `json:"previous"`
		Next     map[string]any `json:"next"`
	}
	if len(rows) == 0 || rows[0].Action != ports.ActionCorrectionApplied || json.Unmarshal(rows[0].Context, &pre) != nil ||
		pre.Previous["reminder_leads"] != nil || len(pre.Next["reminder_leads"].([]any)) != 1 {
		t.Errorf("first row = %+v (%+v), want a pre-image of reminder_leads from null to [60]", rows, pre)
	}
}

// TestI40_AnEventsOwnLeadsFollowItsNewTime: once set, the leads are the
// event's, so moving it re-plans at them, not at the user's preferences.
func TestI40_AnEventsOwnLeadsFollowItsNewTime(t *testing.T) {
	f := newI40(t, `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 23:00.",`+
		`"event_at":"2026-10-14T23:00:00-03:00","weight":0.7,"decay_rate":0.05,"language":"es","referent":"A"}`)
	f.armMeeting(t)
	u := f.meeting(t)
	if err := f.units.UpdateReminderLeads(context.Background(), u.ID, []int{60}, i40Now); err != nil {
		t.Fatal(err)
	}
	f.capture(t, "es a las 23")
	want := time.Date(2026, 10, 14, 22, 0, 0, 0, i40Now.Location())
	if at := f.armedAt(t); len(at) != 1 || !at[0].Equal(want) {
		t.Errorf("armed at %v, want one reminder an hour before the new time, %v", at, want)
	}
}

// TestI40_LeadsThatCannotApplyChangeNothing: leads for a unit with no time of
// day to count back from, or leads outside what a preference may hold, are not
// applied, and the answer never says they were: the correction asks
// (leads_not_set) and the unit and its reminders stay as they were.
func TestI40_LeadsThatCannotApplyChangeNothing(t *testing.T) {
	day := time.Date(2026, 10, 16, 0, 0, 0, 0, i40Now.Location())
	for _, tc := range []struct {
		name  string
		leads string
		edit  func(*unit.Unit)
	}{
		{"a task", `[60]`, func(u *unit.Unit) { u.Type, u.DueAt, u.EventAt = unit.TypeTask, u.EventAt, nil }},
		{"an event on a day alone", `[60]`, func(u *unit.Unit) { u.EventAt = &day }},
		{"the same lead twice", `[60, 60]`, func(*unit.Unit) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newI40With(t, i40Leads(tc.leads), tc.edit)
			u := f.meeting(t)
			res := f.capture(t, "avisame solo 1 hora antes")
			if res.Outcome != brain.OutcomeAsked || res.Correction == nil || res.Correction.Why != brain.AskLeadsNotSet {
				t.Fatalf("result = %+v, want asked with leads_not_set", res)
			}
			if got := f.meeting(t); got.ReminderLeads != nil || got.UpdatedAt != u.UpdatedAt {
				t.Errorf("meeting = %+v, want it untouched", got)
			}
			if at := f.armedAt(t); len(at) != 0 {
				t.Errorf("armed at %v, want nothing armed", at)
			}
		})
	}
}
