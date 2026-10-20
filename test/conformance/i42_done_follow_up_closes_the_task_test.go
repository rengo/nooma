// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

var (
	i42Zone    = time.FixedZone("ART", -3*3600)
	i42Digest  = time.Date(2026, 10, 20, 7, 0, 0, 0, i42Zone)
	i42Now     = i42Digest.Add(20 * time.Minute)
	i42DueDay  = time.Date(2026, 10, 20, 0, 0, 0, 0, i42Zone)
	i42Created = time.Date(2026, 10, 19, 16, 34, 0, 0, i42Zone)
)

// i42Fixture is a vault holding the tasks the morning digest just named, the
// digest as the last turn of the chat, and a capture that reads the next
// message as response says.
type i42Fixture struct {
	svc       *brain.CaptureService
	units     *memrepo.Units
	triggers  *memrepo.Triggers
	decisions *memrepo.DecisionLog
}

// newI42 seeds the open tasks titled titles (ids t1, t2, …), all due today,
// and the digest that named them.
func newI42(t *testing.T, response string, titles ...string) i42Fixture {
	t.Helper()
	ctx := context.Background()
	f := i42Fixture{units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(), decisions: memrepo.NewDecisionLog()}
	conv := brain.NewConversation(memrepo.NewConversations())
	var ids []string
	for i, title := range titles {
		id := "t" + string(rune('1'+i))
		ids = append(ids, id)
		if err := f.units.Create(ctx, unit.Unit{
			ID: id, Type: unit.TypeTask, Status: unit.StatusPool, Source: "telegram", Content: title + ".", Title: title,
			Weight: 0.8, DueAt: &i42DueDay, LastTouchedAt: i42Created, CreatedAt: i42Created, UpdatedAt: i42Created,
		}); err != nil {
			t.Fatal(err)
		}
	}
	conv.Add(context.Background(), i40Chat, brain.Turn{At: i42Digest, Speaker: classify.SpeakerPush, Text: "Hoy hay cosas", UnitIDs: ids})
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := &scriptedLLM{response: response}
	f.svc = brain.NewCaptureService(fixedClock{now: i42Now}, &counterIDs{}, f.units, embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), f.decisions, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel),
		brain.NewIndex(idx), memrepo.NewSignals(), f.triggers, memrepo.NewTimers(), 0.5, memrepo.NewPendingQuestions()).WithConversation(conv)
	return f
}

func (f i42Fixture) capture(t *testing.T, text string) brain.CaptureResult {
	t.Helper()
	res, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: text, Channel: "telegram", Conversation: i40Chat})
	if err != nil {
		t.Fatalf("Capture(%q): %v", text, err)
	}
	return res
}

func (f i42Fixture) status(t *testing.T, id string) unit.Status {
	t.Helper()
	u, err := f.units.ByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return u.Status
}

func (f i42Fixture) rows(t *testing.T, action ports.DecisionAction) []ports.Decision {
	t.Helper()
	var out []ports.Decision
	for _, d := range i40Actions(t, f.decisions) {
		if d.Action == action {
			out = append(out, d)
		}
	}
	return out
}

// A reading of "ya lo hice": it only answers Nooma, says the task is done,
// and names the item it is about.
func i42Done(referent string) string {
	return `{"type":"chitchat","normalized_content":"Ya lo hice.","weight":0.1,"decay_rate":0.3,"language":"es",` +
		`"task_checkin_outcome":"done","referent":"` + referent + `"}`
}

// TestI42_ADoneFollowUpClosesTheTaskItFollows is the client's case of
// 2026-10-19: the digest named "Renovar el pasaporte", and "ya lo hice"
// answers it. The task is archived (nothing is deleted), the trail says so
// with what it was before, no unit is stored for the answer, the reply names
// what was closed, and no later digest names it.
func TestI42_ADoneFollowUpClosesTheTaskItFollows(t *testing.T) {
	f := newI42(t, i42Done("A"), "Renovar el pasaporte")
	res := f.capture(t, "ya lo hice")

	if res.Outcome != brain.OutcomeCorrected || res.Correction == nil || res.Correction.Closed != "Renovar el pasaporte" || res.Correction.UnitID != "t1" {
		t.Fatalf("result = %+v (correction %+v), want t1 closed by name", res, res.Correction)
	}
	if got := f.status(t, "t1"); got != unit.StatusArchived {
		t.Errorf("status = %q, want archived", got)
	}
	if n := f.units.Count(); n != 1 {
		t.Errorf("units = %d, want 1: an answer is not a memory", n)
	}
	rows := f.rows(t, ports.ActionCaptureTaskClosed)
	var change struct {
		UnitID   string            `json:"unit_id"`
		Previous map[string]string `json:"previous"`
		Next     map[string]string `json:"next"`
	}
	if len(rows) != 1 || json.Unmarshal(rows[0].Context, &change) != nil || change.UnitID != "t1" ||
		change.Previous["status"] != "pool" || change.Next["status"] != "archived" {
		t.Errorf("rows = %+v, want one %s row, pool -> archived", rows, ports.ActionCaptureTaskClosed)
	}
	if got := channels.RenderReply(res); got != "Listo, saqué «Renovar el pasaporte»." {
		t.Errorf("reply = %q", got)
	}
	from, to := prospection.DueTaskWindow(i42Now.AddDate(0, 0, 1))
	if due, err := f.units.LiveTasksDue(context.Background(), from, to); err != nil || len(due) != 0 {
		t.Errorf("tasks the next digest can name = %v (%v), want none", due, err)
	}
}

// TestI42_ADoneDropsTheTasksRemindersAndAnswersItsCheckIn: an asked task has
// an armed reminder and a delivered one nobody answered. Closing it expires
// the armed one and answers the open one as engaged, as a check-in done does.
func TestI42_ADoneDropsTheTasksRemindersAndAnswersItsCheckIn(t *testing.T) {
	f := newI42(t, i42Done("A"), "Pagar la tarjeta")
	ctx := context.Background()
	unitID := "t1"
	for _, id := range []string{"pushed", "later"} {
		at := i42Digest.Add(-time.Hour)
		if id == "later" {
			at = i42Now.Add(3 * time.Hour)
		}
		if err := f.triggers.Create(ctx, ports.Trigger{ID: id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased, FireAt: &at,
			CreatedAt: i42Created, Payload: ports.TriggerPayload{ActionText: "Pagar la tarjeta", Asked: true}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.triggers.Fire(ctx, "pushed", i42Digest.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.triggers.Surface(ctx, "pushed", i42Digest.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	if res := f.capture(t, "listo, pagué la tarjeta"); res.Correction == nil || res.Correction.Closed == "" {
		t.Fatalf("result = %+v, want the task closed", res)
	}
	if armed, err := f.triggers.ArmedForUnit(ctx, unitID); err != nil || len(armed) != 0 {
		t.Errorf("armed = %v (%v), want none left", armed, err)
	}
	if open, err := f.triggers.Delivered(ctx); err != nil || len(open) != 0 {
		t.Errorf("open check-ins = %v (%v), want none", open, err)
	}
	if got := f.rows(t, ports.ActionCaptureCheckInResolved); len(got) != 0 {
		t.Errorf("rows %+v: the close is one row, not a check-in answer of its own", got)
	}
}

// TestI42_AnUnclearDoneAsksWhichAndClosesNothing: the digest named two tasks
// and "ya está" could be either; the model asks, as an unclear correction
// does, and both stay open.
func TestI42_AnUnclearDoneAsksWhichAndClosesNothing(t *testing.T) {
	const q = "¿Te referís a Renovar el pasaporte?"
	f := newI42(t, `{"type":"chitchat","normalized_content":"Ya está.","weight":0.1,"decay_rate":0.3,"language":"es",`+
		`"task_checkin_outcome":"done","referent":"unclear","clarifying_question":"`+q+`"}`, "Renovar el pasaporte", "Pagar la tarjeta")
	res := f.capture(t, "ya está")
	if res.Outcome != brain.OutcomeAsked || res.Reply != q {
		t.Fatalf("result = %+v, want the model's question asked", res)
	}
	for _, id := range []string{"t1", "t2"} {
		if got := f.status(t, id); got != unit.StatusPool {
			t.Errorf("%s status = %q while Nooma asks, want pool", id, got)
		}
	}
}

// TestI42_NotYetChangesNothing: "no, todavía no" puts nothing off (no check-in
// is open) and closes nothing.
func TestI42_NotYetChangesNothing(t *testing.T) {
	f := newI42(t, `{"type":"chitchat","normalized_content":"Todavía no.","weight":0.1,"decay_rate":0.3,"language":"es",`+
		`"task_checkin_outcome":"snooze"}`, "Renovar el pasaporte")
	f.capture(t, "no, todavía no")
	if got := f.status(t, "t1"); got != unit.StatusPool {
		t.Errorf("status = %q, want pool", got)
	}
	if got := f.rows(t, ports.ActionCaptureTaskClosed); len(got) != 0 {
		t.Errorf("rows = %+v, want no close", got)
	}
}

// TestI42_ADoneThatClosesNothingSaysSo: never a false confirmation. A done
// whose item is not a task, or that names no item while nothing is open,
// closes nothing, and the reply says nothing was closed.
func TestI42_ADoneThatClosesNothingSaysSo(t *testing.T) {
	for name, response := range map[string]string{
		"no referent": `{"type":"chitchat","normalized_content":"Ya lo hice.","weight":0.1,"decay_rate":0.3,"language":"es","task_checkin_outcome":"done"}`,
		"not a task":  i42Done("A"),
	} {
		t.Run(name, func(t *testing.T) {
			f := newI42(t, response, "Renovar el pasaporte")
			if name == "not a task" {
				f = newI42(t, response)
				f.seedEvent(t)
			}
			res := f.capture(t, "ya lo hice")
			if got := channels.RenderReply(res); got != "No encontré nada pendiente para cerrar." {
				t.Errorf("reply = %q (result %+v), want that nothing was closed", got, res)
			}
			if got := f.rows(t, ports.ActionCaptureTaskClosed); len(got) != 0 {
				t.Errorf("rows = %+v, want no close", got)
			}
		})
	}
}

// seedEvent replaces the chat with a digest naming one event.
func (f *i42Fixture) seedEvent(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	at := i42Now.Add(2 * time.Hour)
	if err := f.units.Create(ctx, unit.Unit{ID: "e1", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "Dentista.", Title: "Dentista",
		Weight: 0.8, EventAt: &at, LastTouchedAt: i42Created, CreatedAt: i42Created, UpdatedAt: i42Created}); err != nil {
		t.Fatal(err)
	}
	conv := brain.NewConversation(memrepo.NewConversations())
	conv.Add(context.Background(), i40Chat, brain.Turn{At: i42Digest, Speaker: classify.SpeakerPush, Text: "Hoy hay 1 cosa", UnitIDs: []string{"e1"}})
	f.svc.WithConversation(conv)
	if got := f.status(t, "e1"); got != unit.StatusPool {
		t.Fatalf("seed: %q", got)
	}
}

// TestI42_ARecurringTaskIsNotEnded: a done said of one occurrence of a
// recurring reminder answers its check-in and keeps the series.
func TestI42_ARecurringTaskIsNotEnded(t *testing.T) {
	f := newI42(t, i42Done("A"), "Pagar la tarjeta")
	ctx := context.Background()
	unitID, next := "t1", i42Now.AddDate(0, 1, 0)
	rule, anchor := prospection.RuleMonthly, prospection.Anchor{Month: 11, Day: 20}
	if err := f.triggers.Create(ctx, ports.Trigger{ID: "series", UnitID: &unitID, Kind: ports.TriggerKindTimeBased, FireAt: &next,
		RecurrenceRule: &rule, RecurrenceAnchor: &anchor, CreatedAt: i42Created, Payload: ports.TriggerPayload{ActionText: "Pagar la tarjeta"}}); err != nil {
		t.Fatal(err)
	}
	f.capture(t, "listo, pagué la tarjeta")
	if got := f.status(t, "t1"); got != unit.StatusPool {
		t.Errorf("status = %q, want pool: a series outlives one occurrence", got)
	}
	if armed, err := f.triggers.ArmedForUnit(ctx, unitID); err != nil || len(armed) != 1 {
		t.Errorf("armed = %v (%v), want the series kept", armed, err)
	}
}

// TestI42_ADoneReadAloneClosesNothingAndSaysSo: in a chat with no turns no
// digest is shown, so a done names no item. It closes nothing, and
// the reply says so instead of the chat model's own words, which could
// confirm what Nooma did not do.
func TestI42_ADoneReadAloneClosesNothingAndSaysSo(t *testing.T) {
	f := newI42(t, i42Done("A"), "Renovar el pasaporte")
	// A chat with no recent turns: the message is read alone.
	res, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: "ya lo hice", Channel: "telegram", Conversation: "another-chat"})
	if err != nil {
		t.Fatal(err)
	}
	if got := channels.RenderReply(res); got != "No encontré nada pendiente para cerrar." {
		t.Errorf("reply = %q, want that nothing was closed", got)
	}
	if got := f.status(t, "t1"); got != unit.StatusPool {
		t.Errorf("status = %q, want pool: nothing was shown to close", got)
	}
}

// TestI42_ADoneOutsideAChatIsNoChatAnswer: a "listo" typed into the UI's
// capture form or sent to the API is no answer to the chat, so it is answered
// as a chitchat, never as a correction of no unit, which the UI would render
// as "Corrected" with a link to nothing.
func TestI42_ADoneOutsideAChatIsNoChatAnswer(t *testing.T) {
	f := newI42(t, i42Done("A"), "Renovar el pasaporte")
	res, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: "listo", Channel: "ui"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != brain.OutcomeConversed || res.Correction != nil {
		t.Errorf("result = %+v, want a chitchat's reply", res)
	}
}
