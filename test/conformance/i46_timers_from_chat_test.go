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
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// I46 (doc 02 §8, ADR-0051): timers are listed and cancelled from chat
// through the numbered list the code shows. The responses are hand-written:
// the recorded phrasing set covers how a real model reads "¿qué temporizadores
// tengo?" and "cancelá el del horno"; these tests cover what the code does
// with each reading.

var (
	i46Zone  = time.FixedZone("ART", -3*3600)
	i46Now   = time.Date(2026, 10, 26, 10, 10, 0, 0, i46Zone)
	i46Pizza = time.Date(2026, 10, 26, 10, 28, 0, 0, i46Zone)
	i46Mama  = time.Date(2026, 10, 26, 11, 7, 0, 0, i46Zone)
)

type i46Fixture struct {
	svc       *brain.CaptureService
	llm       *scriptedLLM
	units     *memrepo.Units
	timers    *memrepo.Timers
	decisions *memrepo.DecisionLog
	signals   *memrepo.Signals
}

// i46Timer is one pending timer to seed.
type i46Timer struct {
	id   string
	at   time.Time
	text string
}

// newI46 seeds the pending timers given, then answers the next message with
// response. repo, when not nil, stands in front of the seeded timers.
func newI46(t *testing.T, response string, seed []i46Timer, wrap func(ports.TimerRepo) ports.TimerRepo) i46Fixture {
	t.Helper()
	ctx := context.Background()
	f := i46Fixture{llm: &scriptedLLM{response: response}, units: memrepo.NewUnits(), timers: memrepo.NewTimers(), decisions: memrepo.NewDecisionLog(), signals: memrepo.NewSignals()}
	for _, s := range seed {
		text := s.text
		if err := f.timers.Create(ctx, ports.Timer{ID: s.id, FireAt: s.at, ActionText: &text, CreatedAt: i46Now.Add(-time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	var timers ports.TimerRepo = f.timers
	if wrap != nil {
		timers = wrap(f.timers)
	}
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	f.svc = brain.NewCaptureService(fixedClock{now: i46Now}, &counterIDs{}, f.units, embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), f.decisions, f.llm, f.llm, f.llm, fakeprovider.NewEmbeddingFake(embedFakeModel),
		brain.NewIndex(idx), f.signals, memrepo.NewTriggers(), timers, 0.5, memrepo.NewPendingQuestions())
	return f
}

func (f i46Fixture) capture(t *testing.T, text string) brain.CaptureResult {
	t.Helper()
	res, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: text, Channel: "telegram"})
	if err != nil {
		t.Fatalf("Capture(%q): %v", text, err)
	}
	return res
}

func (f i46Fixture) pending(t *testing.T) []string {
	t.Helper()
	live, err := f.timers.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(live))
	for i, d := range live {
		ids[i] = d.ID
	}
	return ids
}

func (f i46Fixture) rows(t *testing.T, action ports.DecisionAction) []ports.Decision {
	t.Helper()
	var out []ports.Decision
	for _, d := range i40Actions(t, f.decisions) {
		if d.Action == action {
			out = append(out, d)
		}
	}
	return out
}

// i46Reading is a reading of a timer request: type typ, the request, and the
// target's raw JSON when not empty.
func i46Reading(typ, request, target string) string {
	r := `{"type":"` + typ + `","normalized_content":"x","weight":0.1,"decay_rate":0.3,"language":"es","timer_request":"` + request + `"`
	if target != "" {
		r += `,"timer_target":` + target
	}
	return r + `}`
}

var i46Two = []i46Timer{{"tmr-mama", i46Mama, "Llamar a mamá"}, {"tmr-pizza", i46Pizza, "Sacar la pizza del horno"}}

// TestI46_AListNamesTheLiveTimers: "¿qué temporizadores tengo?" was answered
// with a recall over notes. A list answers with the live timers, soonest
// first, whatever type the model gave the question, and stores nothing. The
// prompt carried the same list, so a later "el 2" means the timer shown second.
func TestI46_AListNamesTheLiveTimers(t *testing.T) {
	f := newI46(t, i46Reading("recall", "list", ""), append(i46Two, i46Timer{"tmr-gone", i46Mama.Add(time.Hour), "Regar"}), nil)
	if err := f.timers.Cancel(context.Background(), "tmr-gone"); err != nil {
		t.Fatal(err)
	}

	res := f.capture(t, "¿qué temporizadores tengo?")

	if res.Outcome != brain.OutcomeTimers || res.Timers == nil {
		t.Fatalf("Outcome = %q, Timers = %+v; want the timers answered", res.Outcome, res.Timers)
	}
	if got := len(res.Timers.Listed); got != 2 || res.Timers.Listed[0].ID != "tmr-pizza" || res.Timers.Listed[1].ID != "tmr-mama" {
		t.Fatalf("Listed = %+v, want the two pending timers, soonest first", res.Timers.Listed)
	}
	if f.units.Count() != 0 {
		t.Errorf("a list stored %d unit(s), want none", f.units.Count())
	}
	pizza := "Sacar la pizza del horno"
	mama := "Llamar a mamá"
	if want := classify.RenderTimerList([]classify.TimerEntry{{At: i46Pizza, Text: &pizza}, {At: i46Mama, Text: &mama}}); !strings.Contains(f.llm.prompts[0], want) {
		t.Errorf("the prompt does not list the live timers as\n%s\nprompt:\n%s", want, f.llm.prompts[0])
	}
	if got, want := channels.RenderReply(res), "Hay 2 temporizadores puestos:\n1) 10:28 — Sacar la pizza del horno\n2) 11:07 — Llamar a mamá"; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// TestI46_AListWithNoTimerSaysNoneIsSet: "no results" is an answer.
func TestI46_AListWithNoTimerSaysNoneIsSet(t *testing.T) {
	f := newI46(t, i46Reading("chitchat", "list", ""), nil, nil)
	res := f.capture(t, "¿qué alarmas tengo puestas?")
	if res.Outcome != brain.OutcomeTimers || res.Timers == nil || len(res.Timers.Listed) != 0 {
		t.Fatalf("Outcome = %q, Timers = %+v; want the timers answered with none", res.Outcome, res.Timers)
	}
	if !strings.Contains(f.llm.prompts[0], "  No timer is set.\n") {
		t.Errorf("with no live timer the prompt does not say none is set")
	}
	if got, want := channels.RenderReply(res), "No hay temporizadores puestos."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// TestI46_AListPastTheShownOnesSaysHowManyMore: the list a position counts
// holds at most MaxReminderChoices; the rest are counted, not dropped.
func TestI46_AListPastTheShownOnesSaysHowManyMore(t *testing.T) {
	var seed []i46Timer
	for i := range classify.MaxReminderChoices + 2 {
		seed = append(seed, i46Timer{"tmr-" + string(rune('a'+i)), i46Pizza.Add(time.Duration(i) * time.Minute), "Timer"})
	}
	f := newI46(t, i46Reading("recall", "list", ""), seed, nil)
	res := f.capture(t, "¿qué temporizadores tengo?")
	if res.Timers == nil || len(res.Timers.Listed) != classify.MaxReminderChoices || res.Timers.More != 2 {
		t.Fatalf("Timers = %+v, want %d listed and 2 more", res.Timers, classify.MaxReminderChoices)
	}
	if got := channels.RenderReply(res); !strings.HasSuffix(got, "\ny 2 más.") || !strings.HasPrefix(got, "Hay 7 temporizadores puestos:\n") {
		t.Errorf("reply = %q, want the count of all seven and \"y 2 más.\" last", got)
	}
}

// TestI46_ACancelCancelsTheListedTimerItNames: "cancelá el del horno" read as
// the timer listed first cancels that timer, as a state transition, and the
// trail says which.
func TestI46_ACancelCancelsTheListedTimerItNames(t *testing.T) {
	f := newI46(t, i46Reading("correction", "cancel", "1"), i46Two, nil)

	res := f.capture(t, "cancelá el del horno")

	if res.Outcome != brain.OutcomeTimers || res.Timers == nil || res.Timers.Cancelled == nil || res.Timers.Cancelled.ID != "tmr-pizza" {
		t.Fatalf("Outcome = %q, Timers = %+v; want tmr-pizza cancelled", res.Outcome, res.Timers)
	}
	if got := f.pending(t); len(got) != 1 || got[0] != "tmr-mama" {
		t.Errorf("pending after the cancel = %v, want only tmr-mama", got)
	}
	if got := f.timers.Count(); got != 2 {
		t.Errorf("timers.Count() = %d after a cancel, want 2: a cancel is a state transition, never a delete", got)
	}
	rows := f.rows(t, ports.ActionCaptureTimerCancelled)
	if len(rows) != 1 || !strings.Contains(string(rows[0].Context), `"timer_id":"tmr-pizza"`) {
		t.Fatalf("capture.timer.cancelled rows = %+v, want one naming tmr-pizza", rows)
	}
	if got, want := channels.RenderReply(res), "Cancelado: 10:28 — Sacar la pizza del horno."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// TestI46_AnUnclearCancelAsksWhichAndCancelsNothing: a cancel naming no
// listed position asks, with the list, and touches nothing. The answer is read
// with that list shown again.
func TestI46_AnUnclearCancelAsksWhichAndCancelsNothing(t *testing.T) {
	for name, target := range map[string]string{"unclear": `"unclear"`, "no target": "", "past the list": "4"} {
		t.Run(name, func(t *testing.T) {
			f := newI46(t, i46Reading("chitchat", "cancel", target), i46Two, nil)
			res := f.capture(t, "cancelá la alarma")
			if res.Timers == nil || !res.Timers.Which || len(res.Timers.Listed) != 2 || res.Timers.Cancelled != nil {
				t.Fatalf("Timers = %+v, want a which question over both timers", res.Timers)
			}
			if got := f.pending(t); len(got) != 2 {
				t.Errorf("pending = %v, want both still set", got)
			}
			if rows := f.rows(t, ports.ActionCaptureTimerCancelled); len(rows) != 0 {
				t.Errorf("an unclear cancel wrote %d cancel row(s)", len(rows))
			}
			if got, want := channels.RenderReply(res), "¿Cuál cancelo?\n1) 10:28 — Sacar la pizza del horno\n2) 11:07 — Llamar a mamá"; got != want {
				t.Errorf("reply = %q, want %q", got, want)
			}
		})
	}
}

// TestI46_ACancelWithNoTimerSaysNoneIsSet.
func TestI46_ACancelWithNoTimerSaysNoneIsSet(t *testing.T) {
	f := newI46(t, i46Reading("chitchat", "cancel", `"unclear"`), nil, nil)
	res := f.capture(t, "cancelá el del horno")
	if res.Timers == nil || res.Timers.Which || res.Timers.Cancelled != nil {
		t.Fatalf("Timers = %+v, want none set", res.Timers)
	}
	if got, want := channels.RenderReply(res), "No hay temporizadores puestos."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// goneTimers refuses every cancel, as a timer the due scan fired between the
// prompt and the cancel is refused.
type goneTimers struct{ ports.TimerRepo }

func (goneTimers) Cancel(context.Context, string) error { return ports.ErrTimerStatusConflict }

// TestI46_ACancelOfATimerNoLongerPendingSaysSo: the timer named rang, or was
// cancelled, after the list was read. Nothing is logged as cancelled.
func TestI46_ACancelOfATimerNoLongerPendingSaysSo(t *testing.T) {
	f := newI46(t, i46Reading("chitchat", "cancel", "1"), i46Two, func(r ports.TimerRepo) ports.TimerRepo { return goneTimers{r} })
	res := f.capture(t, "cancelá el del horno")
	if res.Timers == nil || !res.Timers.Gone || res.Timers.Cancelled != nil {
		t.Fatalf("Timers = %+v, want the named timer gone", res.Timers)
	}
	if rows := f.rows(t, ports.ActionCaptureTimerCancelled); len(rows) != 0 {
		t.Errorf("a refused cancel wrote %d cancel row(s)", len(rows))
	}
	if got, want := channels.RenderReply(res), "Ese temporizador ya no está puesto."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// TestI46_ATimerIsNamedByItsTitle: a timer stored the reading's
// normalized_content, which carries ISO dates, and was listed and cancelled
// as "12:20 — Recordatorio para sacar la pizza del horno a las 12:20 del
// 2026-10-26.". It stores the reading's title, as a trigger rings with its
// unit's, and the content only when the model gave no title.
func TestI46_ATimerIsNamedByItsTitle(t *testing.T) {
	const head = `{"type":"timer","normalized_content":"Recordarme sacar la pizza del horno a las 10:28 del 2026-10-26.","weight":0.3,"decay_rate":0.3,"language":"es","due_at":"2026-10-26T10:28:00-03:00","remind_request":true`
	for name, tc := range map[string]struct {
		extra, want string
	}{
		"with a title":    {`,"title":"Sacar la pizza del horno"`, "Sacar la pizza del horno"},
		"with none given": {``, "Recordarme sacar la pizza del horno a las 10:28 del 2026-10-26."},
	} {
		t.Run(name, func(t *testing.T) {
			f := newI46(t, head+tc.extra+`}`, nil, nil)
			f.capture(t, "avisame en 18 minutos que saque la pizza del horno")
			rows := f.timers.All()
			if len(rows) != 1 || rows[0].ActionText == nil || *rows[0].ActionText != tc.want {
				t.Fatalf("timers = %+v, want one saying %q", rows, tc.want)
			}
		})
	}
}

// TestI46_ACancelledTimerEndingInAPeriodGetsNoSecondOne: the cancel reply
// closes its sentence, and a text that already closed it must not read "..".
func TestI46_ACancelledTimerEndingInAPeriodGetsNoSecondOne(t *testing.T) {
	f := newI46(t, i46Reading("chitchat", "cancel", "1"), []i46Timer{{"tmr-pizza", i46Pizza, "Sacar la pizza del horno."}}, nil)
	if got, want := channels.RenderReply(f.capture(t, "cancelá el del horno")), "Cancelado: 10:28 — Sacar la pizza del horno."; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

// TestI46_ACorrectionNamingItsUnitIsNoTimerRequest: a correction from a unit's
// page names its unit, and that names what the message does (I28). A reading
// that also carries a timer request still corrects the unit, and lists or
// cancels no timer.
func TestI46_ACorrectionNamingItsUnitIsNoTimerRequest(t *testing.T) {
	ctx := context.Background()
	f := newI46(t, `{"type":"correction","normalized_content":"Dentista el 2026-10-27 a las 11:00.","weight":0.8,"decay_rate":0.05,"language":"es","event_at":"2026-10-27T11:00:00-03:00","timer_request":"cancel","timer_target":1}`, i46Two, nil)
	was := time.Date(2026, 10, 27, 10, 0, 0, 0, i46Zone)
	if err := f.units.Create(ctx, unit.Unit{ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool, Source: "ui",
		Content: "Dentista el 2026-10-27 a las 10:00.", Title: "Dentista", EventAt: &was, CreatedAt: i46Now, UpdatedAt: i46Now}); err != nil {
		t.Fatal(err)
	}

	res, err := f.svc.Capture(ctx, brain.CaptureInput{Text: "es a las 11", Channel: "ui", ReferentID: "dentist"})
	if err != nil {
		t.Fatal(err)
	}

	if res.Outcome != brain.OutcomeCorrected || res.Timers != nil {
		t.Fatalf("Outcome = %q, Timers = %+v; want the unit corrected and no timer answer", res.Outcome, res.Timers)
	}
	u, err := f.units.ByID(ctx, "dentist")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 27, 11, 0, 0, 0, i46Zone); u.EventAt == nil || !u.EventAt.Equal(want) {
		t.Errorf("dentist event_at = %v, want %v", u.EventAt, want)
	}
	if got := f.pending(t); len(got) != 2 {
		t.Errorf("pending = %v, want both timers still set", got)
	}
}
