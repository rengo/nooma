// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

const pillsChat ports.ConversationID = "21"

// i40Daily is the anchor-day instant of a daily reminder at hh:mm: its unit
// keeps the day it was first told (2026-10-01) as its event_at, which is
// behind the clock, and only its trigger knows the next occurrence.
func i40Daily(hour, minute int) time.Time {
	return time.Date(2026, 10, 1, hour, minute, 0, 0, i40Now.Location())
}

// seedPills stores the daily pills reminder at hh:mm the way a capture of
// "recordame tomar las pastillas todos los días a las hh:mm" leaves it: an
// asked event unit whose event_at is the anchor day, and one recurring trigger
// armed at its next occurrence. The chat's last turn is that trigger's push,
// so the next message follows it as A.
func seedPills(t *testing.T, f i40Fixture, hour, minute int) (unit.Unit, ports.Trigger) {
	t.Helper()
	return seedPillsAs(t, f, hour, minute, true, nil)
}

// seedPillsAs is seedPills with the unit asked or not, and its row armed at
// fire instead of its next occurrence when fire is set: a row already due
// that no pass has sent yet.
func seedPillsAs(t *testing.T, f i40Fixture, hour, minute int, asked bool, fire *time.Time) (unit.Unit, ports.Trigger) {
	t.Helper()
	ctx := context.Background()
	anchorAt := i40Daily(hour, minute)
	u := unit.Unit{ID: "pills", Type: unit.TypeEvent, Status: unit.StatusPool, Source: "telegram",
		Content: "Recordarme tomar las pastillas todos los días a partir del 2026-10-01.", Title: "Tomar las pastillas",
		EventAt: &anchorAt, RemindMe: asked, CreatedAt: anchorAt.Add(-time.Hour), UpdatedAt: anchorAt.Add(-time.Hour)}
	if err := f.units.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	rule, weekday := prospection.RuleDaily, anchorAt.Weekday()
	clock := prospection.TimeOfDay{Hour: hour, Minute: minute}
	anchor := prospection.Anchor{Month: anchorAt.Month(), Day: anchorAt.Day(), Weekday: &weekday, At: &clock}
	next := prospection.NextOccurrence(rule, anchor, i40Now)
	if fire != nil {
		next = *fire
	}
	id := u.ID
	trig := ports.Trigger{ID: "pills-daily", UnitID: &id, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "Tomar las pastillas"}, FireAt: &next,
		RecurrenceRule: &rule, RecurrenceAnchor: &anchor, CreatedAt: anchorAt.Add(-time.Hour)}
	if err := f.triggers.Create(ctx, trig); err != nil {
		t.Fatal(err)
	}
	pushed := i40Now.Add(-20 * time.Hour)
	f.conv.Add(ctx, pillsChat, brain.Turn{At: pushed, Speaker: classify.SpeakerNooma, Text: "Acordate: tomar las pastillas, hoy.", UnitIDs: []string{u.ID}})
	return u, trig
}

func (f i40Fixture) sayPills(t *testing.T, text string) brain.CaptureResult {
	t.Helper()
	res, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: text, Channel: "telegram", Conversation: pillsChat})
	if err != nil {
		t.Fatalf("Capture(%q): %v", text, err)
	}
	return res
}

// remindThatOf is "recordame eso" bound to the pushed item A, in lang, with
// no instant of its own: the live reading of 2026-11-10.
func remindThatOf(lang string) string {
	return `{"type":"task","normalized_content":"Tomar las pastillas.","weight":0.6,"decay_rate":0.05,` +
		`"language":"` + lang + `","remind_request":true,"referent":"A"}`
}

// keepsItsDailyTrigger fails unless the unit's armed set is still exactly the
// one recurring trigger it had, untouched.
func (f i40Fixture) keepsItsDailyTrigger(t *testing.T, want ports.Trigger) {
	t.Helper()
	armed, err := f.triggers.ArmedForUnit(context.Background(), *want.UnitID)
	if err != nil {
		t.Fatal(err)
	}
	if len(armed) != 1 || armed[0].ID != want.ID || !armed[0].FireAt.Equal(*want.FireAt) ||
		armed[0].RecurrenceRule == nil || *armed[0].RecurrenceRule != prospection.RuleDaily {
		t.Errorf("armed = %+v, want only the daily trigger %q at %v, as it was", armed, want.ID, *want.FireAt)
	}
}

// TestI40_ARemindMeOfARecurringReminderSaysItAlreadyDoes is the live report
// of 2026-11-10: "recordame eso" after the nightly pills push bound the daily
// reminder, and the referent path armed its stored event_at, the anchor day
// three weeks behind, as a one-shot: "Lo anoté, pero no te puse aviso: esa
// hora ya pasó." A recurring reminder already reminds of itself. The answer
// says so, naming its rule and its next occurrence (today when its hour is
// still ahead, else tomorrow), and its trigger stays the one it was: nothing
// armed, nothing refused, no unit stored, no decision_log row. An item not yet
// asked becomes asked.
//
// The next occurrence is when the row will actually ring (the PM's ruling on
// #450): a row held by the night rings at quiet end, and one already due that
// no pass has sent yet goes out now; neither is named at a past instant or at
// one it will not ring at. The rule keeps its own clock.
func TestI40_ARemindMeOfARecurringReminderSaysItAlreadyDoes(t *testing.T) {
	due := time.Date(2026, 10, 14, 17, 55, 0, 0, i40Now.Location()) // five minutes before i40Now
	for _, tc := range []struct {
		name, lang   string
		hour, minute int
		asked        bool
		fire         *time.Time
		want         string
	}{
		{"es, later today", "es", 21, 30, true, nil, "Eso ya te lo recuerdo todos los días a las 21:30; el próximo aviso es hoy a las 21:30."},
		{"es, passed today", "es", 9, 0, true, nil, "Eso ya te lo recuerdo todos los días a las 9:00; el próximo aviso es mañana a las 9:00."},
		{"en, later today", "en", 21, 30, true, nil, "I already remind you of that every day at 21:30; the next one is today at 21:30."},
		{"en, passed today", "en", 9, 0, true, nil, "I already remind you of that every day at 9:00; the next one is tomorrow at 9:00."},
		{"es, not yet asked", "es", 21, 30, false, nil, "Eso ya te lo recuerdo todos los días a las 21:30; el próximo aviso es hoy a las 21:30."},
		{"es, held by the night", "es", 6, 0, true, nil, "Eso ya te lo recuerdo todos los días a las 6:00; el próximo aviso es mañana a las 7:00."},
		{"en, held by the night", "en", 6, 0, true, nil, "I already remind you of that every day at 6:00; the next one is tomorrow at 7:00."},
		{"es, due and not yet sent", "es", 17, 55, true, &due, "Eso ya te lo recuerdo todos los días a las 17:55; el próximo aviso es hoy a las 18:00."},
		{"en, due and not yet sent", "en", 17, 55, true, &due, "I already remind you of that every day at 17:55; the next one is today at 18:00."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newI40(t, remindThatOf(tc.lang))
			_, trig := seedPillsAs(t, f, tc.hour, tc.minute, tc.asked, tc.fire)
			before, rows := f.units.Count(), len(i40Actions(t, f.decisions))

			res := f.sayPills(t, "recordame eso por favor")

			if got := channels.RenderReply(res); got != tc.want {
				t.Errorf("reply = %q, want %q", got, tc.want)
			}
			if res.Outcome != brain.OutcomeArmed || res.UnitID != "pills" || res.ArmRefused != nil {
				t.Errorf("result = %+v, want the pills reminder named as armed, nothing refused", res)
			}
			f.keepsItsDailyTrigger(t, trig)
			if n := len(i40Actions(t, f.decisions)); n != rows {
				t.Errorf("decision_log rows %d -> %d, want none written: nothing was armed or refused", rows, n)
			}
			if n := f.units.Count(); n != before {
				t.Errorf("units %d -> %d: a reminder of an item stores nothing", before, n)
			}
			if u, err := f.units.ByID(context.Background(), "pills"); err != nil || !u.RemindMe {
				t.Errorf("pills = remind_me %v, %v: want it marked asked", u.RemindMe, err)
			}

			// Asked again, nothing doubles.
			f.sayPills(t, "recordame eso")
			f.keepsItsDailyTrigger(t, trig)
		})
	}
}

// TestI40_ARemindMeOfAOneOffWhoseTimePassedStillSaysSo: what the recurring
// fix leaves alone. A one-off item whose instant is behind is kept, marked
// asked, armed nothing, and the answer says its time has passed, in either
// language, with one capture.arm.refused row.
func TestI40_ARemindMeOfAOneOffWhoseTimePassedStillSaysSo(t *testing.T) {
	for _, tc := range []struct{ lang, want string }{
		{"es", "Lo anoté, pero no te puse aviso: esa hora ya pasó."},
		{"en", "I noted it, but I didn't set a reminder: that time has already passed."},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			f := newI40(t, remindThatOf(tc.lang))
			ctx := context.Background()
			past := i40Daily(21, 30)
			u := unit.Unit{ID: "pills", Type: unit.TypeEvent, Status: unit.StatusPool, Source: "telegram",
				Content: "Tomar las pastillas el 2026-10-01 a las 21:30.", Title: "Tomar las pastillas",
				EventAt: &past, CreatedAt: past.Add(-time.Hour), UpdatedAt: past.Add(-time.Hour)}
			if err := f.units.Create(ctx, u); err != nil {
				t.Fatal(err)
			}
			f.conv.Add(ctx, pillsChat, brain.Turn{At: i40Now.Add(-time.Minute), Speaker: classify.SpeakerNooma, Text: "Acordate: tomar las pastillas.", UnitIDs: []string{u.ID}})

			res := f.sayPills(t, "recordame eso")

			if got := channels.RenderReply(res); got != tc.want {
				t.Errorf("reply = %q, want %q", got, tc.want)
			}
			if armed, _ := f.triggers.ArmedForUnit(ctx, u.ID); len(armed) != 0 {
				t.Errorf("armed = %+v, want none: its time passed", armed)
			}
			if got, _ := f.units.ByID(ctx, u.ID); !got.RemindMe {
				t.Error("the item is not marked asked")
			}
			if n := f.count(t, ports.ActionCaptureArmRefused); n != 1 {
				t.Errorf("capture.arm.refused rows = %d, want 1", n)
			}
		})
	}
}

// TestI40_AMoveOfARecurringReminderReanchorsIt: "es a las 22, recordámelo"
// after the pills push moves the daily reminder's hour, and so does "es a las
// 22" alone. The move re-anchors its one recurring trigger by its rule, as a
// capture arms one (prospection.Arm's recurringTrigger): still daily, now at
// 22:00, its next occurrence today. The unit being asked, the move was planned
// as a one-shot's: the daily row went to a 2-hour lead at 20:00, its anchor
// left at 21:30, and a second, asked row was armed. The answer names the move,
// and with a remind-me the rule too.
func TestI40_AMoveOfARecurringReminderReanchorsIt(t *testing.T) {
	for _, tc := range []struct{ name, lang, remind, want string }{
		{"es, remind-me", "es", `"remind_request":true,`, "Listo, lo pasé para hoy a las 22:00. Eso ya te lo recuerdo todos los días a las 22:00; el próximo aviso es hoy a las 22:00."},
		{"en, remind-me", "en", `"remind_request":true,`, "Done, I moved it to today at 22:00. I already remind you of that every day at 22:00; the next one is today at 22:00."},
		{"es, move alone", "es", "", "Listo, lo pasé para hoy a las 22:00."},
		{"en, move alone", "en", "", "Done, I moved it to today at 22:00."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newI40(t, `{"type":"correction","normalized_content":"Recordarme tomar las pastillas todos los días a partir del 2026-10-01.",`+
				`"event_at":"2026-10-14T22:00:00-03:00","weight":0.6,"decay_rate":0.05,"language":"`+tc.lang+`",`+tc.remind+`"referent":"A"}`)
			_, trig := seedPills(t, f, 21, 30)

			res := f.sayPills(t, "es a las 22")

			armed, err := f.triggers.ArmedForUnit(context.Background(), "pills")
			if err != nil {
				t.Fatal(err)
			}
			want := time.Date(2026, 10, 14, 22, 0, 0, 0, i40Now.Location())
			if len(armed) != 1 || armed[0].ID != trig.ID || armed[0].RecurrenceRule == nil || *armed[0].RecurrenceRule != prospection.RuleDaily ||
				!armed[0].FireAt.Equal(want) || armed[0].RecurrenceAnchor == nil || armed[0].RecurrenceAnchor.At == nil ||
				*armed[0].RecurrenceAnchor.At != (prospection.TimeOfDay{Hour: 22}) {
				t.Errorf("armed = %+v, want the one daily trigger re-anchored at 22:00, next at %v", armed, want)
			}
			if got := channels.RenderReply(res); got != tc.want {
				t.Errorf("reply = %q, want %q", got, tc.want)
			}
		})
	}
}
