// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/goldenset"
	"github.com/rengo/nooma/test/support/memrepo"
)

// recordedReading is the real model's recorded reading of a reminder phrasing
// case (testdata/llm/reminders/<id>.json). It is read from the recording
// rather than copied, so a re-recording that reads it otherwise is replayed
// here too.
func recordedReading(t *testing.T, id string) string {
	t.Helper()
	var ex goldenset.LLMExample
	path := filepath.Join(repoRootFromCaller(t), "testdata", "llm", "reminders", id+".json")
	if err := goldenset.Load(path, &ex); err != nil {
		t.Fatal(err)
	}
	if ex.Response == "" {
		t.Fatalf("%s holds no recorded response", path)
	}
	return ex.Response
}

// recordamelo is the recorded reading of "recordámelo" right after a task:
// the task it follows, and a request to be reminded of it.
func recordamelo(t *testing.T) string {
	t.Helper()
	return recordedReading(t, "classify-request-recordamelo")
}

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
	f := newI40(t, recordamelo(t))
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
	f := newI40(t, recordamelo(t))
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
			if want := i40Meeting.Add(-15 * time.Minute); !a.FireAt.Equal(want) {
				t.Errorf("asked reminder fires at %v, want %v, the default remind_me_lead before it", a.FireAt, want)
			}
		}
	}
	if len(armed) != 2 || asked != 1 {
		t.Errorf("armed = %+v, want the lead it had and one asked reminder", armed)
	}
	if res.Outcome != brain.OutcomeArmed || f.units.Count() != 1 {
		t.Errorf("result = %+v, units = %d: want the meeting armed and no unit stored", res, f.units.Count())
	}
	if got := channels.RenderReply(res); !strings.Contains(got, "Te aviso 15 minutos antes.") {
		t.Errorf("reply = %q, want the asked reminder's answer", got)
	}
}

// TestI40_ARemindMeTooLateForItsLeadSaysWhy: the asked reminder of an event a
// few minutes away is too late to send, while one of the event's own leads
// is still ahead. That lead is no asked reminder, so the answer is the
// refusal, named for what it is ("queda muy poco tiempo"), never a blank one.
func TestI40_ARemindMeTooLateForItsLeadSaysWhy(t *testing.T) {
	soon := i40Now.Add(5 * time.Minute)
	f := newI40With(t, `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 18:05.",`+
		`"weight":0.7,"decay_rate":0.05,"language":"es","remind_request":true,"referent":"A"}`,
		func(u *unit.Unit) { u.EventAt = &soon })
	cfg := memrepo.NewConfig()
	leads := `[2]`
	cfg.SeedConfig(t, ports.VaultConfig{EventReminderLeads: &leads})
	f.svc = f.svc.WithReminderPrefs(cfg)

	res := f.capture(t, "recordámelo")

	if res.ArmRefused == nil || res.ArmRefused.Why != prospection.RefusalTooLate {
		t.Fatalf("result = %+v, want the asked reminder refused as too late", res)
	}
	if got := channels.RenderReply(res); !strings.Contains(got, "queda muy poco tiempo") {
		t.Errorf("reply = %q, want that too little time is left", got)
	}
	if armed, _ := f.triggers.ArmedForUnit(context.Background(), "meeting"); len(armed) != 0 {
		t.Errorf("armed = %+v, want none: only an asked reminder is armed by a remind-me", armed)
	}
}

// TestI40_ARemindMeThatMovesTheItemMovesItThenReminds (ADR-0043 point 4): "es a las
// 23, recordámelo" both moves the item it follows and asks to be reminded of
// it. The move is applied first, through the correction path, and the asked
// reminder is armed at the moved instant; the answer names both. Before, the
// move was dropped and the answer confirmed a reminder of the old time.
func TestI40_ARemindMeThatMovesTheItemMovesItThenReminds(t *testing.T) {
	f := newI40(t, `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 23:00.",`+
		`"event_at":"2026-10-14T23:00:00-03:00","weight":0.7,"decay_rate":0.05,"language":"es","remind_request":true,"referent":"A"}`)

	res := f.capture(t, "es a las 23, recordámelo")

	moved := time.Date(2026, 10, 14, 23, 0, 0, 0, i40Now.Location())
	got := f.meeting(t)
	if got.EventAt == nil || !got.EventAt.Equal(moved) || !got.RemindMe {
		t.Errorf("meeting at %v, remind_me %v: want moved to %v and asked", got.EventAt, got.RemindMe, moved)
	}
	if n := f.units.Count(); n != 1 {
		t.Errorf("units = %d, want 1: a follow-up is not a new memory", n)
	}
	armed, err := f.triggers.ArmedForUnit(context.Background(), "meeting")
	if err != nil {
		t.Fatal(err)
	}
	asked := 0
	for _, a := range armed {
		if a.Payload.Asked {
			asked++
			if want := moved.Add(-15 * time.Minute); !a.FireAt.Equal(want) {
				t.Errorf("asked reminder fires at %v, want %v, before the moved time", a.FireAt, want)
			}
		}
	}
	if asked != 1 {
		t.Errorf("armed = %+v, want one asked reminder", armed)
	}
	if res.Outcome != brain.OutcomeCorrected || res.Correction == nil || res.Armed == nil {
		t.Fatalf("result = %+v, want the meeting corrected and its asked reminder armed", res)
	}
	reply := channels.RenderReply(res)
	if !strings.HasPrefix(reply, "Movido al ") || !strings.HasSuffix(reply, ". Te aviso 15 minutos antes.") {
		t.Errorf("reply = %q, want the move and the asked reminder named", reply)
	}
}

// TestI40_ARemindMeThatMovesTheItemTooLateSaysWhy: the move is applied, and
// an asked reminder that no longer fits is said not to be set, and why.
func TestI40_ARemindMeThatMovesTheItemTooLateSaysWhy(t *testing.T) {
	f := newI40(t, `{"type":"correction","normalized_content":"Tengo reunión con Juan el 2026-10-14 a las 18:05.",`+
		`"event_at":"2026-10-14T18:05:00-03:00","weight":0.7,"decay_rate":0.05,"language":"es","remind_request":true,"referent":"A"}`)

	res := f.capture(t, "es a las 18:05, recordámelo")

	if got := f.meeting(t); got.EventAt == nil || !got.EventAt.Equal(i40Now.Add(5*time.Minute)) {
		t.Errorf("meeting at %v, want moved to 18:05", got.EventAt)
	}
	reply := channels.RenderReply(res)
	if !strings.HasPrefix(reply, "Movido al ") || !strings.Contains(reply, "no puse aviso: queda muy poco tiempo para esa hora") {
		t.Errorf("reply = %q, want the move named and the reminder said not set, and why", reply)
	}
}

// TestI40_LeadsWinOverAStrayRemindRequest: "avisame 2 horas antes", as
// recorded (classify-followup-dos-horas-antes), carries the event's new leads
// and a stray remind_request. Leads win: the reading takes the correction
// path, the event's own leads become [120], and no asked reminder is added,
// nor the item marked asked, nor a 15-minute reminder named.
func TestI40_LeadsWinOverAStrayRemindRequest(t *testing.T) {
	f := newI40(t, recordedReading(t, "classify-followup-dos-horas-antes"))

	res := f.capture(t, "avisame 2 horas antes")

	got := f.meeting(t)
	if !slices.Equal(got.ReminderLeads, []int{120}) || got.RemindMe {
		t.Errorf("meeting leads %v, remind_me %v: want its own leads [120] and not asked", got.ReminderLeads, got.RemindMe)
	}
	armed, err := f.triggers.ArmedForUnit(context.Background(), "meeting")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range armed {
		if a.Payload.Asked {
			t.Errorf("armed = %+v, want no asked reminder: the user asked for leads", armed)
		}
	}
	if res.Outcome != brain.OutcomeCorrected || res.Correction == nil || res.Armed != nil {
		t.Fatalf("result = %+v, want the leads corrected and nothing armed as asked", res)
	}
	if reply := channels.RenderReply(res); strings.Contains(reply, "15 minutos") || !strings.HasPrefix(reply, "Te aviso ") {
		t.Errorf("reply = %q, want the new leads named", reply)
	}
}

// TestI40_ARemindMeNamesTheItemInTheUsersZone: the store hands a unit's
// instants back in UTC (formatUnitTime/parse), while the clock and the user
// are at -03. "recordámelo" of a task due at 10:00 local is answered with
// 10:00, never the UTC hour, and the reminder rings at 09:45 local.
func TestI40_ARemindMeNamesTheItemInTheUsersZone(t *testing.T) {
	f := newI40(t, recordamelo(t))
	due := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC) // 10:00 at -03, as the store returns it
	seedFollowed(t, f, unit.Unit{ID: "gift", Type: unit.TypeTask, Status: unit.StatusPool, Source: "telegram",
		Content: "Comprar el regalo de Ana el 2026-10-15 a las 10:00.", Title: "Regalo de Ana", DueAt: &due,
		CreatedAt: i40Now, UpdatedAt: i40Now})

	res := f.say(t, "recordámelo")

	if got := channels.RenderReply(res); !strings.Contains(got, "10:00") || strings.Contains(got, "13:00") {
		t.Errorf("reply = %q, want the task's local 10:00 named", got)
	}
	armed, err := f.triggers.ArmedForUnit(context.Background(), "gift")
	if err != nil {
		t.Fatal(err)
	}
	if want := due.Add(-15 * time.Minute); len(armed) != 1 || !armed[0].FireAt.Equal(want) {
		t.Errorf("armed = %+v, want one reminder at %v", armed, want.In(i40Now.Location()))
	}
}
