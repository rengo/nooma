// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// i36Zone is the observed message's zone, -03:00. The recorded case was
// captured on 2026-10-16 at 10:42:25 local.
var i36Zone = time.FixedZone("ART", -3*60*60)

func i36At(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, i36Zone)
}

var i36Now = time.Date(2026, 10, 16, 10, 42, 25, 0, i36Zone)

// i36Done is the model's reading of a bare Spanish "listo" answering a check-in.
const i36Done = `{"type":"chitchat","normalized_content":"Listo.","weight":0.2,"decay_rate":0.3,"language":"es","task_checkin_outcome":"done","remind_request":false,"reminder_intent":"none"}`

// i36Reading is a classification of "Comprar café" of kind, due at due (an
// RFC 3339 instant or a bare date), asked or not, in language.
func i36Reading(kind, due string, asked bool, language string) string {
	ask := "false"
	if asked {
		ask = "true"
	}
	return `{"type":"` + kind + `","normalized_content":"Comprar café","title":"Comprar café","weight":0.7,"decay_rate":0.03,` +
		`"language":"` + language + `","due_at":"` + due + `","remind_request":` + ask + `,"reminder_intent":"none","question_answer":"none"}`
}

func i36Recordings(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRootFromCaller(t), "testdata", "llm", "reminders")
}

// i36Vault is one vault a capture and the check passes after it share, with
// one digest state so a morning's digest is sent once.
type i36Vault struct {
	*i39Vault
	state *memrepo.State
	cfg   ports.ConfigRepo
}

func newI36Vault() *i36Vault { return &i36Vault{i39Vault: newI39Vault(), state: memrepo.NewState()} }

// capture runs one capture at now, the model answering with case id in dir.
func (v *i36Vault) capture(t *testing.T, now time.Time, dir, id, text string) brain.CaptureResult {
	t.Helper()
	ctx := context.Background()
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, dir, id)
	svc := brain.NewCaptureService(fixedClock{now: now}, v.ids, v.units, embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), v.log, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx),
		memrepo.NewSignals(), v.triggers, memrepo.NewTimers(), 0.5, memrepo.NewPendingQuestions())
	if v.cfg != nil {
		svc = svc.WithReminderPrefs(v.cfg)
	}
	result, err := svc.Capture(ctx, brain.CaptureInput{Text: text, Channel: "chat"})
	if err != nil {
		t.Fatalf("Capture(%q): %v", text, err)
	}
	return result
}

// say captures the model reading response at now.
func (v *i36Vault) say(t *testing.T, now time.Time, response string) brain.CaptureResult {
	t.Helper()
	return v.capture(t, now, i28Case(t, "i36", "replayed", response), "i36", "replayed")
}

// pass runs one check pass at at and returns what it sent.
func (v *i36Vault) pass(t *testing.T, at time.Time) []string {
	t.Helper()
	before := len(v.channel.Sent(t))
	if _, err := brain.NewCheckService(fixedClock{now: at}, v.triggers, memrepo.NewTimers(), v.ids, v.log, v.channel,
		v.units, v.state, nil, "12449194", nil, nil).Check(context.Background(), brain.CheckRequest{}); err != nil {
		t.Fatalf("Check at %s: %v", at, err)
	}
	var texts []string
	for _, m := range v.channel.Sent(t)[before:] {
		texts = append(texts, m.Text)
	}
	return texts
}

func (v *i36Vault) only(t *testing.T) ports.Trigger {
	t.Helper()
	rows := v.triggers.All()
	if len(rows) != 1 {
		t.Fatalf("triggers = %d, want one: %+v", len(rows), rows)
	}
	return rows[0]
}

func (v *i36Vault) unit(t *testing.T, id string) unit.Unit {
	t.Helper()
	u, err := v.units.ByID(context.Background(), id)
	if err != nil {
		t.Fatalf("the captured unit %q: %v", id, err)
	}
	return u
}

// reminders are the sent texts that are a reminder push, not a digest.
func reminders(sent []string) []string {
	var out []string
	for _, s := range sent {
		if strings.HasPrefix(s, "Recordatorio") {
			out = append(out, s)
		}
	}
	return out
}

// TestI36_TheObservedCoffeeRequestRingsOnce replays the real model's reading
// of the message observed on 2026-10-14 (recorded in the phrasing set): it
// stores a task that was asked for, arms one reminder 15 minutes before 10:00
// the next day (ADR-0031 point 2), the reply says so, a check pass pushes it
// once at 09:45 local and never again, and a bare "listo" then closes it as
// the check-in it became.
func TestI36_TheObservedCoffeeRequestRingsOnce(t *testing.T) {
	v := newI36Vault()
	result := v.capture(t, i36Now, i36Recordings(t), "classify-task-remind-me-coffee", "recordame comprar café mañana a las 10")

	if u := v.unit(t, result.UnitID); u.Type != unit.TypeTask || !u.RemindMe || u.Title != "Comprar café" {
		t.Errorf("unit = %+v, want the titled task that was asked for", u)
	}
	trg := v.only(t)
	p := trg.Payload
	if trg.FireAt == nil || !trg.FireAt.Equal(i36At(17, 9, 45)) || p.LeadMinutes != 15 || !p.About.Equal(i36At(17, 10, 0)) ||
		!p.Asked || !p.NightExempt || p.DayOnly {
		t.Fatalf("trigger = %+v, want 09:45 about 10:00 with lead 15, asked and night-exempt", trg)
	}
	if got, want := channels.RenderReply(result), "Anotado para el sáb 17 oct, 10:00. Te aviso 15 minutos antes."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}

	v.pass(t, i36At(17, prospection.DigestHour, 5))
	if got := reminders(v.pass(t, i36At(17, 9, 44))); len(got) != 0 {
		t.Fatalf("at 09:44 sent %q, want nothing yet", got)
	}
	if got := reminders(v.pass(t, i36At(17, 9, 45))); len(got) != 1 || !strings.Contains(got[0], "Comprar café") {
		t.Fatalf("at 09:45 sent %q, want the coffee reminder once", got)
	}
	for _, at := range []time.Time{i36At(17, 9, 50), i36At(17, 10, 0), i36At(17, 10, 30)} {
		if got := reminders(v.pass(t, at)); len(got) != 0 {
			t.Fatalf("at %s sent %q again", at.Format("15:04"), got)
		}
	}
	if open := v.open(t); len(open) != 1 || open[0] != trg.ID {
		t.Fatalf("open check-ins %v, want the delivered reminder %s", open, trg.ID)
	}

	v.answer(t, "listo", i36Done, i36At(17, 10, 40))
	if open := v.open(t); len(open) != 0 {
		t.Errorf("open check-ins %v after a bare listo, want it closed", open)
	}
	if got := v.rows(t, ports.ActionCaptureCheckInResolved); got != 1 {
		t.Errorf("capture.checkin.resolved rows = %d, want 1", got)
	}
}

// TestI36_APutOffAskedReminderIsAskedAgain: the delivered reminder is a
// check-in like any other, so a bare "no" puts it off and the next morning's
// digest asks it again (doc 02 §7, I39).
func TestI36_APutOffAskedReminderIsAskedAgain(t *testing.T) {
	v := newI36Vault()
	v.capture(t, i36Now, i36Recordings(t), "classify-task-remind-me-coffee", "recordame comprar café mañana a las 10")
	v.pass(t, i36At(17, prospection.DigestHour, 5))
	v.pass(t, i36At(17, 9, 45))

	v.answer(t, "no", i39NotYet, i36At(17, 10, 40))
	sent := v.pass(t, i36At(18, prospection.DigestHour, 5))
	if !strings.Contains(strings.Join(sent, "\n"), "Comprar café") {
		t.Errorf("the next morning sent %q, want the coffee asked again", sent)
	}
}

// TestI36_ARequestComesFromTheField: the same classification with
// remind_request false arms nothing for a task, and the unit does not claim
// it was asked for (ADR-0033: the model reads the words, the code acts).
func TestI36_ARequestComesFromTheField(t *testing.T) {
	v := newI36Vault()
	result := v.say(t, i36Now, i36Reading("task", "2026-10-17T10:00:00-03:00", false, "es"))

	if v.triggers.Count() != 0 || v.unit(t, result.UnitID).RemindMe {
		t.Fatalf("armed %d triggers for a message that did not ask, unit %+v", v.triggers.Count(), v.unit(t, result.UnitID))
	}
	if got := channels.RenderReply(result); got != "Anotado." {
		t.Errorf("reply = %q, want Anotado.", got)
	}
}

// TestI36_ADayAloneRemindsThatDayAtTheDateOnlyHour: "pagar el gas mañana"
// rings that day at the date-only hour, its row says it is a whole day, and
// the reply names the day and the hour armed, not a lead.
func TestI36_ADayAloneRemindsThatDayAtTheDateOnlyHour(t *testing.T) {
	for name, tc := range map[string]struct {
		at   *string
		fire time.Time
		want string
	}{
		"default": {nil, i36At(17, 9, 0), "Anotado para el sáb 17 oct. Te aviso ese día a las 09:00."},
		"08:30":   {ptr("08:30"), i36At(17, 8, 30), "Anotado para el sáb 17 oct. Te aviso ese día a las 08:30."},
	} {
		t.Run(name, func(t *testing.T) {
			v := newI36Vault()
			if tc.at != nil {
				cfg := memrepo.NewConfig()
				cfg.SeedConfig(t, ports.VaultConfig{DateOnlyReminderAt: tc.at})
				v.cfg = cfg
			}
			result := v.say(t, i36Now, i36Reading("task", "2026-10-17", true, "es"))

			trg := v.only(t)
			p := trg.Payload
			if !trg.FireAt.Equal(tc.fire) || p.LeadMinutes != 0 || !p.About.Equal(i36At(17, 0, 0)) || !p.DayOnly || !p.Asked || p.NightExempt {
				t.Errorf("trigger = %+v, want %s about that day's midnight, day-only, asked, not night-exempt", trg, tc.fire)
			}
			if got := channels.RenderReply(result); got != tc.want {
				t.Errorf("reply = %q, want %q", got, tc.want)
			}
			// The rationale says what happened: a ring on that day, not one
			// that "has already arrived" because the day began at midnight.
			if got := i36ArmedRationales(t, v.log); len(got) != 1 || got[0] != "armed a trigger for 2026-10-17T03:00:00Z, firing at "+
				tc.fire.UTC().Format(time.RFC3339)+", on that day itself" {
				t.Errorf("capture.armed.trigger rationales = %q", got)
			}
			// The gate reads the day from day_only: the row about that day's
			// midnight is not passed at the hour it rings.
			if got := reminders(v.pass(t, tc.fire)); len(got) != 1 {
				t.Errorf("at %s sent %q, want the reminder once", tc.fire.Format("15:04"), got)
			}
		})
	}
}

// TestI36_TodayAfterTheDateOnlyHourRingsAtOnce: "hoy" asked after 09:00 rings
// at once, and the reply says so rather than an hour already gone.
func TestI36_TodayAfterTheDateOnlyHourRingsAtOnce(t *testing.T) {
	v := newI36Vault()
	result := v.say(t, i36Now, i36Reading("task", "2026-10-16", true, "es"))

	if trg := v.only(t); !trg.FireAt.Equal(i36Now) {
		t.Errorf("trigger = %+v, want it at once", trg)
	}
	if got, want := channels.RenderReply(result), "Anotado para el vie 16 oct. Te aviso ahora mismo."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// TestI36_TheReplyIsInTheMessagesLanguage: the asked reply in English.
func TestI36_TheReplyIsInTheMessagesLanguage(t *testing.T) {
	v := newI36Vault()
	result := v.say(t, i36Now, i36Reading("task", "2026-10-17T10:00:00-03:00", true, "en"))
	if got, want := channels.RenderReply(result), "Noted for Sat 17 Oct, 10:00. I will remind you 15 minutes before."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// TestI36_TheLeadIsTheUsersPreference: the stored remind-me lead moves the
// reminder and the reply (spec R3).
func TestI36_TheLeadIsTheUsersPreference(t *testing.T) {
	v := newI36Vault()
	cfg := memrepo.NewConfig()
	cfg.SeedConfig(t, ports.VaultConfig{RemindMeLeadMinutes: ptr(30)})
	v.cfg = cfg
	result := v.say(t, i36Now, i36Reading("task", "2026-10-17T10:00:00-03:00", true, "es"))

	if trg := v.only(t); !trg.FireAt.Equal(i36At(17, 9, 30)) || trg.Payload.LeadMinutes != 30 {
		t.Errorf("trigger = %+v, want 09:30 with lead 30", trg)
	}
	if got, want := channels.RenderReply(result), "Anotado para el sáb 17 oct, 10:00. Te aviso 30 minutos antes."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// TestI36_AnAskedRepetitionKeepsRecurring replays the real model's reading of
// "recordame tomar las pastillas todos los días a las 9pm": a recurring
// reminder with a rule, asked, that wrote its time as due_at. It arms the
// ADR-0034 recurring row at 21:00 rather than a one-shot that would drop the
// repetition the message asked for.
func TestI36_AnAskedRepetitionKeepsRecurring(t *testing.T) {
	v := newI36Vault()
	result := v.capture(t, i36Now, i36Recordings(t), "classify-task-remind-me-daily-pills",
		"recordame tomar las pastillas todos los días a las 9pm")

	trg := v.only(t)
	if trg.RecurrenceRule == nil || *trg.RecurrenceRule != prospection.RuleDaily || !trg.FireAt.Equal(i36At(16, 21, 0)) {
		t.Errorf("trigger = %+v, want the daily recurring row at 21:00 today", trg)
	}
	if got, want := channels.RenderReply(result), "Recordatorio recurrente puesto para el vie 16 oct, 21:00."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// TestI36_AskedButNotArmedSaysWhy: the user asked and nothing could be armed.
// The unit is kept and still says it was asked for, one capture.arm.refused
// row names the reason, and the reply says it rather than a bare "Anotado."
// (ADR-0031 point 7: when nothing was set, it says why).
func TestI36_AskedButNotArmedSaysWhy(t *testing.T) {
	cases := []struct {
		name, reading string
		now           time.Time
		why, want     string
	}{
		{"the time has passed", i36Reading("task", "2026-10-16T09:00:00-03:00", true, "es"), i36Now, "already_past",
			"Anotado, pero no puse aviso: la hora indicada ya pasó."},
		{"too little of the day is left", i36Reading("task", "2026-10-16", true, "es"), i36At(16, 23, 54), "too_late",
			"Anotado, pero no puse aviso: queda muy poco tiempo para esa hora."},
		{"no time was given", `{"type":"task","normalized_content":"Comprar café","weight":0.7,"decay_rate":0.03,"language":"en","remind_request":true}`,
			i36Now, "no_date", "Noted, but I did not set a reminder: no time was given, and guessing one is worse than not setting a reminder at all."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := newI36Vault()
			result := v.say(t, tc.now, tc.reading)

			if v.triggers.Count() != 0 || result.Outcome != brain.OutcomeStored || !v.unit(t, result.UnitID).RemindMe {
				t.Fatalf("result = %+v, triggers %d; want the asked task kept and nothing armed", result, v.triggers.Count())
			}
			rows := i36Refusals(t, v.log)
			if len(rows) != 1 || rows[0]["why"] != tc.why || rows[0]["unit_id"] != result.UnitID || rows[0]["asked"] != true {
				t.Errorf("capture.arm.refused rows = %v, want one naming %s, the unit and asked", rows, tc.why)
			}
			if got := channels.RenderReply(result); got != tc.want {
				t.Errorf("reply = %q, want %q", got, tc.want)
			}
		})
	}
}

// i36Refusals is the capture.arm.refused rows' contexts.
func i36Refusals(t *testing.T, log *memrepo.DecisionLog) []map[string]any {
	t.Helper()
	all, err := log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, d := range all {
		if d.Action != ports.ActionCaptureArmRefused {
			continue
		}
		var c map[string]any
		if err := json.Unmarshal(d.Context, &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// i36Night is the passes a night of the scheduler runs, every five minutes
// from from to to, and the reminder pushes each one sent, by local time.
func (v *i36Vault) night(t *testing.T, from, to time.Time) map[string][]string {
	t.Helper()
	sent := map[string][]string{}
	for at := from; !at.After(to); at = at.Add(5 * time.Minute) {
		if got := reminders(v.pass(t, at)); len(got) > 0 {
			sent[at.Format("01-02 15:04")] = got
		}
	}
	return sent
}

// TestI36_ANamedTimeRingsThroughTheNight drives capture and then the check
// passes over the night: "a las 3" asked at 01:00 is armed at 02:45 and pushed
// then, once, never held for quiet end nor judged past after it (ADR-0031
// point 6). Its twin, "a las 6:30" said at 06:20, is armed at once inside
// quiet hours and pushed at once.
func TestI36_ANamedTimeRingsThroughTheNight(t *testing.T) {
	for name, tc := range map[string]struct {
		now        time.Time
		due, fired string
	}{
		"03:00 asked at 01:00": {i36At(17, 1, 0), "2026-10-17T03:00:00-03:00", "10-17 02:45"},
		"06:30 said at 06:20":  {i36At(17, 6, 20), "2026-10-17T06:30:00-03:00", "10-17 06:20"},
	} {
		t.Run(name, func(t *testing.T) {
			v := newI36Vault()
			v.say(t, tc.now, i36Reading("task", tc.due, true, "es"))
			if trg := v.only(t); !trg.Payload.NightExempt {
				t.Fatalf("trigger = %+v, want it night-exempt", trg)
			}
			sent := v.night(t, tc.now, i36At(17, 9, 0))
			if len(sent) != 1 || len(sent[tc.fired]) != 1 {
				t.Errorf("pushes by pass = %v, want exactly one at %s", sent, tc.fired)
			}
		})
	}
}

// TestI36_AnAskedNamedMidnightPassesAtMidnight: an asked reminder about 00:00
// is a moment, not a day (doc 02 §7: the gate reads a day only from the row's
// day_only key). Armed 23:45, a pass that first runs at 00:00 finds it passed
// and sends nothing; read as a day, as an unasked midnight is, it would go out
// all of the next day.
func TestI36_AnAskedNamedMidnightPassesAtMidnight(t *testing.T) {
	v := newI36Vault()
	v.say(t, i36At(17, 22, 0), i36Reading("task", "2026-10-18T00:00:00-03:00", true, "es"))
	if trg := v.only(t); !trg.FireAt.Equal(i36At(17, 23, 45)) || trg.Payload.DayOnly {
		t.Fatalf("trigger = %+v, want 23:45, not day-only", trg)
	}
	if sent := reminders(v.pass(t, i36At(18, 0, 0))); len(sent) != 0 {
		t.Errorf("at 00:00 sent %q, want nothing: the midnight asked about has passed", sent)
	}
	if got := i36Expired(t, v.log); len(got) != 1 || got[0] != "passed" {
		t.Errorf("check.trigger.expired reasons at 00:00 = %v, want one passed", got)
	}
}

// i36Expired is the reasons of the check.trigger.expired rows.
func i36Expired(t *testing.T, log *memrepo.DecisionLog) []string {
	t.Helper()
	all, err := log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range all {
		if d.Action != ports.ActionCheckTriggerExpired {
			continue
		}
		var c struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(d.Context, &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c.Reason)
	}
	return out
}

// i36ArmedRationales is the rationales of the capture.armed.trigger rows.
func i36ArmedRationales(t *testing.T, log *memrepo.DecisionLog) []string {
	t.Helper()
	all, err := log.Since(context.Background(), time.Time{}, -1)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range all {
		if d.Action == ports.ActionCaptureArmedTrigger {
			out = append(out, d.Rationale)
		}
	}
	return out
}
