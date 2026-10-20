// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
)

// i43Chat is the chat of the live case (ADR-0044): the oculista reminder
// pushed that morning, then the service task the user told Nooma about.
const i43Chat ports.ConversationID = "43"

var i43Pushed = time.Date(2026, 10, 14, 9, 7, 0, 0, i40Now.Location())

// newI43 seeds, beside the i40 fixture, the live chat: the oculista event (A)
// whose reminder was pushed at 09:07 and is an open check-in, then the
// service task (B) told a minute before the next message, and another open
// check-in, newer, on a unit the chat never showed.
func newI43(t *testing.T, response string) i40Fixture {
	t.Helper()
	f := newI40(t, response)
	ctx := context.Background()
	oculista := time.Date(2026, 10, 14, 10, 0, 0, 0, i40Now.Location())
	service := time.Date(2026, 10, 16, 0, 0, 0, 0, i40Now.Location())
	for _, u := range []unit.Unit{
		{ID: "oculista", Type: unit.TypeEvent, Content: "Turno con el oculista el 2026-10-14 a las 10:00.", Title: "Turno con el oculista", EventAt: &oculista},
		{ID: "service", Type: unit.TypeTask, Content: "Llevar el auto al service el 2026-10-16.", Title: "Llevar el auto al service", DueAt: &service},
		{ID: "unseen", Type: unit.TypeTask, Content: "Pagar la luz.", Title: "Pagar la luz"},
	} {
		u.Status, u.Source, u.CreatedAt, u.UpdatedAt = unit.StatusPool, "telegram", i43Pushed, i43Pushed
		if err := f.units.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	for id, at := range map[string]time.Time{"t-oculista": i43Pushed, "t-unseen": i40Now.Add(-30 * time.Minute)} {
		unitID := "oculista"
		if id == "t-unseen" {
			unitID = "unseen"
		}
		if err := f.triggers.Create(ctx, ports.Trigger{ID: id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased, FireAt: &at,
			CreatedAt: i43Pushed, Payload: ports.TriggerPayload{ActionText: unitID}}); err != nil {
			t.Fatal(err)
		}
		if err := f.triggers.Fire(ctx, id, at); err != nil {
			t.Fatal(err)
		}
		if err := f.triggers.Surface(ctx, id, at); err != nil {
			t.Fatal(err)
		}
	}
	told := i40Now.Add(-time.Minute)
	f.conv.Add(ctx, i43Chat, brain.Turn{At: i43Pushed, Speaker: classify.SpeakerPush, Text: "Recordatorio: Turno con el oculista — hoy 10:00", UnitIDs: []string{"oculista"}})
	f.conv.Add(ctx, i43Chat, brain.Turn{At: told, Speaker: classify.SpeakerUser, Text: "el viernes tengo que llevar el auto al service", UnitIDs: []string{"service"}})
	f.conv.Add(ctx, i43Chat, brain.Turn{At: told, Speaker: classify.SpeakerNooma, Text: "Anotado para el vie 16 oct.", UnitIDs: []string{"service"}})
	return f
}

func (f i40Fixture) sayI43(t *testing.T, text string) brain.CaptureResult {
	t.Helper()
	res, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: text, Channel: "telegram", Conversation: i43Chat})
	if err != nil {
		t.Fatalf("Capture(%q): %v", text, err)
	}
	return res
}

// open is the ids of the check-ins still open, newest first.
func (f i40Fixture) open(t *testing.T) []string {
	t.Helper()
	open, err := f.triggers.Delivered(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, o := range open {
		ids = append(ids, o.ID)
	}
	return ids
}

func (f i40Fixture) count(t *testing.T, action ports.DecisionAction) int {
	t.Helper()
	n := 0
	for _, d := range i40Actions(t, f.decisions) {
		if d.Action == action {
			n++
		}
	}
	return n
}

// TestI43_ACheckInAnswerNamingNoItemClosesNoCheckIn is the live misreading
// (ADR-0044): "recordámelo" after the service task, read as a chitchat taking
// up a nudge and naming no item. Beside a chat, a check-in answer closes only
// the check-in of the pushed item it names, so this one closes none: before,
// it closed the newest open check-in, one the message never answered.
func TestI43_ACheckInAnswerNamingNoItemClosesNoCheckIn(t *testing.T) {
	f := newI43(t, `{"type":"chitchat","normalized_content":"Recordámelo.","weight":0.1,"decay_rate":0.3,"language":"es","nudge_outcome":"engaged"}`)

	f.sayI43(t, "recordámelo")

	if got := f.open(t); len(got) != 2 {
		t.Errorf("open check-ins = %v, want both still open: the answer named no pushed item", got)
	}
	if n := f.count(t, ports.ActionCaptureCheckInResolved); n != 0 {
		t.Errorf("%d check-in(s) resolved, want none", n)
	}
	if n := f.count(t, ports.ActionCaptureCheckInUnmatched); n != 1 {
		t.Errorf("%d unmatched row(s), want 1: an answer that matched nothing is recorded", n)
	}
}

// TestI43_ACheckInAnswerClosesThePushedItemItNames: an answer that names the
// pushed oculista item closes that item's check-in, not the newest open one,
// which the chat never showed.
func TestI43_ACheckInAnswerClosesThePushedItemItNames(t *testing.T) {
	f := newI43(t, `{"type":"chitchat","normalized_content":"Ya fui al oculista.","weight":0.1,"decay_rate":0.3,"language":"es",`+
		`"task_checkin_outcome":"done","referent":"A"}`)

	f.sayI43(t, "ya fui al oculista")

	if got := f.open(t); len(got) != 1 || got[0] != "t-unseen" {
		t.Errorf("open check-ins = %v, want only t-unseen: the answer named the oculista", got)
	}
	if n := f.count(t, ports.ActionCaptureCheckInResolved); n != 1 {
		t.Errorf("%d check-in(s) resolved, want the oculista's", n)
	}
}

// TestI43_AnAnswerNamingAnItemWithNoCheckInClosesNone: the service task was
// told, never pushed, so it has no check-in; an answer naming it closes none
// of the others.
func TestI43_AnAnswerNamingAnItemWithNoCheckInClosesNone(t *testing.T) {
	f := newI43(t, `{"type":"chitchat","normalized_content":"Dale.","weight":0.1,"decay_rate":0.3,"language":"es",`+
		`"nudge_outcome":"engaged","referent":"B"}`)

	f.sayI43(t, "dale")

	if got := f.open(t); len(got) != 2 {
		t.Errorf("open check-ins = %v, want both still open", got)
	}
}

// TestI43_ARemindMeAfterAPushRemindsTheItemItFollows: read as it should be,
// "recordámelo" names the service task it follows (B) and asks to be reminded
// of it, whatever type the model gives it. The task becomes an asked
// reminder, nothing is stored, and the oculista check-in stays open.
func TestI43_ARemindMeAfterAPushRemindsTheItemItFollows(t *testing.T) {
	f := newI43(t, `{"type":"task","normalized_content":"Llevar el auto al service el 2026-10-16.","weight":0.7,"decay_rate":0.05,`+
		`"language":"es","remind_request":true,"referent":"B"}`)
	before := f.units.Count()

	res := f.sayI43(t, "recordámelo")

	if n := f.units.Count(); n != before {
		t.Errorf("units = %d, want %d: a reminder of an item stores no unit", n, before)
	}
	u, err := f.units.ByID(context.Background(), "service")
	if err != nil {
		t.Fatal(err)
	}
	if !u.RemindMe {
		t.Error("the service task is not marked asked")
	}
	armed, err := f.triggers.ArmedForUnit(context.Background(), "service")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 16, 9, 0, 0, 0, i40Now.Location()); len(armed) != 1 || !armed[0].FireAt.Equal(want) {
		t.Errorf("armed = %+v, want one asked reminder at %v", armed, want)
	}
	if res.Outcome != brain.OutcomeArmed || res.UnitID != "service" {
		t.Errorf("result = %+v, want the service task armed", res)
	}
	if got := f.open(t); len(got) != 2 {
		t.Errorf("open check-ins = %v, want both still open", got)
	}
}
