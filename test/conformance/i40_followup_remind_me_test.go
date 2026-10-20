// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// recordamelo is the recorded reading of "recordámelo" right after a task
// (testdata/llm/reminders/classify-request-recordamelo.json): the task it
// follows, and a request to be reminded of it.
const recordamelo = `{"type":"correction","normalized_content":"Comprar el regalo de Ana el 2026-10-17.","weight":0.8,` +
	`"decay_rate":0.03,"language":"es","title":"Regalo de Ana","remind_request":true,"reminder_intent":"none",` +
	`"question_answer":"none","referent":"A"}`

const remindChat ports.ConversationID = "9"

// seedFollowed stores u and the exchange that told it, so the next message in
// remindChat follows it as A.
func seedFollowed(t *testing.T, f i40Fixture, u unit.Unit) {
	t.Helper()
	ctx := context.Background()
	if err := f.units.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	said := i40Now.Add(-time.Minute)
	f.conv.Add(ctx, remindChat, brain.Turn{At: said, Speaker: classify.SpeakerUser, Text: "tengo que comprar el regalo de Ana mañana", UnitIDs: []string{u.ID}})
	f.conv.Add(ctx, remindChat, brain.Turn{At: said, Speaker: classify.SpeakerNooma, Text: "Anotado.", UnitIDs: []string{u.ID}})
}

func (f i40Fixture) say(t *testing.T, text string) brain.CaptureResult {
	t.Helper()
	res, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: text, Channel: "telegram", Conversation: remindChat})
	if err != nil {
		t.Fatalf("Capture(%q): %v", text, err)
	}
	return res
}

// TestI40_ARemindMeFollowUpRemindsTheItemItFollows (ADR-0043): "recordámelo"
// right after a task names that task and asks to be reminded of it. The task
// becomes an asked reminder, armed as a capture that asked arms it, on the
// unit it is: no unit is stored, its text is not rewritten, and the answer is
// the asked reminder's.
func TestI40_ARemindMeFollowUpRemindsTheItemItFollows(t *testing.T) {
	f := newI40(t, recordamelo)
	due := time.Date(2026, 10, 17, 0, 0, 0, 0, i40Now.Location())
	seedFollowed(t, f, unit.Unit{ID: "gift", Type: unit.TypeTask, Status: unit.StatusPool, Source: "telegram",
		Content: "Comprar el regalo de Ana mañana.", Title: "Regalo de Ana", DueAt: &due, CreatedAt: i40Now, UpdatedAt: i40Now})

	res := f.say(t, "recordámelo")

	if n := f.units.Count(); n != 2 {
		t.Errorf("units = %d, want 2 (the meeting and the gift): a reminder of an item stores no unit", n)
	}
	u, err := f.units.ByID(context.Background(), "gift")
	if err != nil {
		t.Fatal(err)
	}
	if !u.RemindMe || u.Content != "Comprar el regalo de Ana mañana." {
		t.Errorf("gift = remind_me %v, %q: want asked, its text as it was", u.RemindMe, u.Content)
	}
	armed, err := f.triggers.ArmedForUnit(context.Background(), "gift")
	if err != nil {
		t.Fatal(err)
	}
	if len(armed) != 1 || !armed[0].Payload.Asked || !armed[0].Payload.DayOnly {
		t.Fatalf("armed = %+v, want one asked reminder of the day", armed)
	}
	if want := time.Date(2026, 10, 17, 9, 0, 0, 0, i40Now.Location()); !armed[0].FireAt.Equal(want) {
		t.Errorf("fires at %v, want %v, the asked day's reminder time", armed[0].FireAt, want)
	}
	if res.Outcome != brain.OutcomeArmed || res.UnitID != "gift" {
		t.Errorf("result = %+v, want the gift armed", res)
	}
	if got := channels.RenderReply(res); !strings.Contains(got, "Te aviso ese día a las 09:00.") {
		t.Errorf("reply = %q, want the asked reminder's answer", got)
	}

	// Asked again, nothing doubles.
	f.say(t, "recordámelo")
	if again, _ := f.triggers.ArmedForUnit(context.Background(), "gift"); len(again) != 1 {
		t.Errorf("asked twice, armed = %d, want 1", len(again))
	}
}

// TestI40_ARemindMeOfAnUndatedItemSaysNoReminderWasSet: an item with no date
// cannot be reminded of. It is still marked asked, nothing is armed or stored,
// and the answer says why, as an asked capture's does.
func TestI40_ARemindMeOfAnUndatedItemSaysNoReminderWasSet(t *testing.T) {
	f := newI40(t, recordamelo)
	seedFollowed(t, f, unit.Unit{ID: "gift", Type: unit.TypeTask, Status: unit.StatusPool, Source: "telegram",
		Content: "Comprar el regalo de Ana.", Title: "Regalo de Ana", CreatedAt: i40Now, UpdatedAt: i40Now})

	res := f.say(t, "recordámelo")

	if n := f.units.Count(); n != 2 {
		t.Errorf("units = %d, want 2", n)
	}
	if armed, _ := f.triggers.ArmedForUnit(context.Background(), "gift"); len(armed) != 0 {
		t.Errorf("armed = %+v, want none: there is no date", armed)
	}
	if u, _ := f.units.ByID(context.Background(), "gift"); !u.RemindMe {
		t.Error("gift is not marked asked: a date given later would arm nothing")
	}
	if got := channels.RenderReply(res); !strings.Contains(got, "no puse aviso") {
		t.Errorf("reply = %q, want that no reminder was set, and why", got)
	}
}

// TestI40_ARemindMeOfAnEventAddsOnlyTheAskedReminder: an event already carries
// its leads. Asked to be reminded, it gains the asked one, and its leads are
// not armed a second time.
func TestI40_ARemindMeOfAnEventAddsOnlyTheAskedReminder(t *testing.T) {
	f := newI40(t, `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 22:30.",`+
		`"weight":0.7,"decay_rate":0.05,"language":"es","remind_request":true,"referent":"A"}`)
	ctx := context.Background()
	lead := i40Meeting.Add(-3 * time.Hour)
	meeting := "meeting"
	if err := f.triggers.Create(ctx, ports.Trigger{ID: "lead", UnitID: &meeting, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "Reunión con Juan"}, FireAt: &lead, CreatedAt: i40Now}); err != nil {
		t.Fatal(err)
	}

	res := f.capture(t, "recordámelo")

	armed, err := f.triggers.ArmedForUnit(ctx, meeting)
	if err != nil {
		t.Fatal(err)
	}
	asked := 0
	for _, a := range armed {
		if a.Payload.Asked {
			asked++
		}
	}
	if len(armed) != 2 || asked != 1 {
		t.Errorf("armed = %+v, want the lead it had and one asked reminder", armed)
	}
	if res.Outcome != brain.OutcomeArmed || f.units.Count() != 1 {
		t.Errorf("result = %+v, units = %d: want the meeting armed and no unit stored", res, f.units.Count())
	}
}
