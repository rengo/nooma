// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/channels"
	"github.com/rengo/nooma/internal/core/classify"
	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/prospection"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakechannel"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// I59 (doc 02 §5 step 2, ADR-0064): a reply that carries a fact is built by
// code, in warm fixed sentences. No model words it and none waits for one.
// This file pins the recall, push, timer ring, digest, confirmation and correction surfaces.

var (
	i59Zone = time.FixedZone("ART", -3*60*60)
	// Wednesday: the fixture every row of warm-replies design §6 is written at.
	i59Now = time.Date(2026, 11, 4, 10, 0, 0, 0, i59Zone)
)

func i59At(month time.Month, day, hour, minute int) *time.Time {
	t := time.Date(2026, month, day, hour, minute, 0, 0, i59Zone)
	return &t
}

// TestI59_RecallAnswerIsBuiltByCode: every recall answer of design §6, exact
// text, in both languages, through the channel's own renderer.
func TestI59_RecallAnswerIsBuiltByCode(t *testing.T) {
	type row struct {
		name   string
		es, en []unit.Unit
		wantES string
		wantEN string
	}
	rows := []row{
		{name: "one event ahead",
			es:     []unit.Unit{{Title: "Turno para estudios con el gastroenterólogo", Content: "Turno el 2026-11-26 a las 08:30.", EventAt: i59At(11, 26, 8, 30)}},
			en:     []unit.Unit{{Title: "Appointment for tests with the gastroenterologist", Content: "Appointment on 2026-11-26 at 08:30.", EventAt: i59At(11, 26, 8, 30)}},
			wantES: "Tenés anotado turno para estudios con el gastroenterólogo el jueves 26 de noviembre a las 8:30.",
			wantEN: "You have “Appointment for tests with the gastroenterologist” on Thursday 26 November at 8:30."},
		{name: "one event past",
			es:     []unit.Unit{{Title: "Cena con Ana", EventAt: i59At(11, 3, 21, 0)}},
			en:     []unit.Unit{{Title: "Dinner with Ana", EventAt: i59At(11, 3, 21, 0)}},
			wantES: "Tenías anotado cena con Ana ayer a las 21:00.",
			wantEN: "You had “Dinner with Ana” yesterday at 21:00."},
		{name: "one task due",
			es:     []unit.Unit{{Title: "Renovar el pasaporte", DueAt: i59At(11, 5, 0, 0)}},
			en:     []unit.Unit{{Title: "Renew the passport", DueAt: i59At(11, 5, 0, 0)}},
			wantES: "Tenés anotado renovar el pasaporte para mañana.",
			wantEN: "You have “Renew the passport” due tomorrow."},
		{name: "one undated",
			es:     []unit.Unit{{Content: "La clave del wifi es 1234."}},
			en:     []unit.Unit{{Content: "The wifi password is 1234."}},
			wantES: "Tenés anotado: la clave del wifi es 1234.",
			wantEN: "You have this noted: The wifi password is 1234."},
		{name: "several",
			es: []unit.Unit{
				{Title: "Turno con el oculista", Content: "Turno con el oculista el jueves.", EventAt: i59At(11, 5, 11, 0)},
				{Title: "Cena con Ana", EventAt: i59At(11, 13, 21, 0)},
				{Title: "Renovar el pasaporte", DueAt: i59At(11, 9, 0, 0)}},
			en: []unit.Unit{
				{Title: "Appointment with the optician", EventAt: i59At(11, 5, 11, 0)},
				{Title: "Dinner with Ana", EventAt: i59At(11, 13, 21, 0)},
				{Title: "Renew the passport", DueAt: i59At(11, 9, 0, 0)}},
			wantES: "Tenés 3 cosas anotadas:\n• Turno con el oculista, mañana a las 11:00\n• Cena con Ana, el viernes 13 de noviembre a las 21:00\n• Renovar el pasaporte, para el lunes 9 de noviembre",
			wantEN: "You have 3 things noted:\n• Appointment with the optician, tomorrow at 11:00\n• Dinner with Ana, on Friday 13 November at 21:00\n• Renew the passport, due Monday 9 November"},
		{name: "none",
			wantES: "No tengo nada anotado sobre eso.",
			wantEN: "I don't have anything noted about that."},
	}
	for _, r := range rows {
		for _, l := range []struct {
			lang  classify.Language
			units []unit.Unit
			want  string
		}{{classify.LanguageES, r.es, r.wantES}, {classify.LanguageEN, r.en, r.wantEN}} {
			res := brain.CaptureResult{Outcome: brain.OutcomeRecalled, Language: l.lang, At: i59Now, Recalled: l.units}
			if got := channels.RenderReply(res); got != l.want {
				t.Errorf("%s (%s): reply\n%q\nwant\n%q", r.name, l.lang, got, l.want)
			}
		}
	}
}

// i59Guard answers capture_processing with the scripted reading and
// relation_evaluation with an empty verdict, and records every task. Any
// other task fails the test: no model words a reply.
type i59Guard struct {
	t       *testing.T
	reading string
	mu      sync.Mutex
	tasks   []string
}

func (g *i59Guard) Complete(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tasks = append(g.tasks, req.Task)
	switch req.Task {
	case "capture_processing":
		return ports.LLMResponse{Text: g.reading}, nil
	case "relation_evaluation":
		return ports.LLMResponse{Text: `{"verdict":"unrelated","confidence":0}`}, nil
	}
	g.t.Errorf("provider called for task %q: no model words a reply (I59)", req.Task)
	return ports.LLMResponse{}, nil
}

func (g *i59Guard) answered(task string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, got := range g.tasks {
		if got == task {
			return true
		}
	}
	return false
}

// TestI59_RecallCallsNoWordingTask: a recall over a real vault calls the
// provider for the classification only, the classification was answered (the
// recall is a recall, not a degraded capture), and the reply is the pinned
// sentence.
func TestI59_RecallCallsNoWordingTask(t *testing.T) {
	for _, tc := range []struct {
		name, text, reading, want string
	}{
		{"found, es", "¿cuándo es el turno con el oculista?",
			`{"type":"recall","normalized_content":"x","weight":0.2,"decay_rate":0.3,"language":"es","recall_scope":{"terms":["oculista"]}}`,
			"Tenés anotado turno con el oculista mañana a las 11:00."},
		{"found, en", "when is the optician appointment?",
			`{"type":"recall","normalized_content":"x","weight":0.2,"decay_rate":0.3,"language":"en","recall_scope":{"terms":["oculista"]}}`,
			"You have “Turno con el oculista” tomorrow at 11:00."},
		{"nothing, es", "¿qué anoté el 1 de octubre?",
			`{"type":"recall","normalized_content":"x","weight":0.2,"decay_rate":0.3,"language":"es","recall_scope":{"window":{"on":"told","from":"2026-10-01","to":"2026-10-02"}}}`,
			"No tengo nada anotado sobre eso."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			units, embeddings, lexical := memrepo.NewUnits(), memrepo.NewEmbeddings(), memrepo.NewLexical()
			embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
			told := *i59At(11, 1, 9, 0)
			content := "Turno con el oculista el 2026-11-05 a las 11:00."
			if err := units.Create(ctx, unit.Unit{ID: "oculist", Type: unit.TypeEvent, Status: unit.StatusPool, Source: "telegram",
				Title: "Turno con el oculista", Content: content, EventAt: i59At(11, 5, 11, 0),
				CreatedAt: told, UpdatedAt: told, LastTouchedAt: told}); err != nil {
				t.Fatal(err)
			}
			v, err := embed.Embed(ctx, ports.EmbedRequest{Text: content})
			if err != nil {
				t.Fatal(err)
			}
			if err := embeddings.Put(ctx, ports.Embedding{UnitID: "oculist", Model: embedFakeModel, Vector: v.Vector, At: told}); err != nil {
				t.Fatal(err)
			}
			lexical.SeedLexical(t, "oculist", content)
			idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
			if err != nil {
				t.Fatal(err)
			}
			guard := &i59Guard{t: t, reading: tc.reading}
			svc := brain.NewCaptureService(fixedClock{now: i59Now}, &counterIDs{}, units, embeddings, lexical, memrepo.NewRelations(),
				memrepo.NewDecisionLog(), guard, guard, guard, embed, brain.NewIndex(idx),
				memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)

			res, err := svc.Capture(ctx, brain.CaptureInput{Text: tc.text, Channel: "telegram"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != brain.OutcomeRecalled || !guard.answered("capture_processing") {
				t.Fatalf("outcome %q, classification answered %v: the recall must come from a classification the model answered", res.Outcome, guard.answered("capture_processing"))
			}
			if got := channels.RenderReply(res); got != tc.want {
				t.Errorf("reply\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestI59_CaptureAndCorrectionCallNoWordingTask (spec R1): a capture and a
// correction over a real vault call the provider for the classification only.
// The reply is built by code, so the guard fails the test on any other task,
// and both classifications were answered (neither is a degraded capture).
func TestI59_CaptureAndCorrectionCallNoWordingTask(t *testing.T) {
	for _, tc := range []struct {
		name, text, reading string
		outcome             brain.CaptureOutcome
		want                string
	}{
		{"capture", "Anotá que la clave del wifi es 1234",
			`{"type":"knowledge","normalized_content":"La clave del wifi es 1234.","weight":0.4,"decay_rate":0.1,"language":"es"}`,
			brain.OutcomeStored, "Listo, anotado."},
		{"correction", "el turno con el oculista es a las 12:00",
			`{"type":"correction","normalized_content":"Turno con el oculista el 2026-11-05 a las 12:00.","event_at":"2026-11-05T12:00:00-03:00","weight":0.6,"decay_rate":0.05,"language":"es"}`,
			brain.OutcomeCorrected, "Listo, lo pasé para mañana a las 12:00."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			units, embeddings, lexical := memrepo.NewUnits(), memrepo.NewEmbeddings(), memrepo.NewLexical()
			embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
			told := *i59At(11, 1, 9, 0)
			content := "Turno con el oculista el 2026-11-05 a las 11:00."
			if err := units.Create(ctx, unit.Unit{ID: "oculist", Type: unit.TypeEvent, Status: unit.StatusPool, Source: "telegram",
				Title: "Turno con el oculista", Content: content, EventAt: i59At(11, 5, 11, 0),
				CreatedAt: told, UpdatedAt: told, LastTouchedAt: told}); err != nil {
				t.Fatal(err)
			}
			v, err := embed.Embed(ctx, ports.EmbedRequest{Text: content})
			if err != nil {
				t.Fatal(err)
			}
			if err := embeddings.Put(ctx, ports.Embedding{UnitID: "oculist", Model: embedFakeModel, Vector: v.Vector, At: told}); err != nil {
				t.Fatal(err)
			}
			lexical.SeedLexical(t, "oculist", content)
			idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
			if err != nil {
				t.Fatal(err)
			}
			guard := &i59Guard{t: t, reading: tc.reading}
			svc := brain.NewCaptureService(fixedClock{now: i59Now}, &counterIDs{}, units, embeddings, lexical, memrepo.NewRelations(),
				memrepo.NewDecisionLog(), guard, guard, guard, embed, brain.NewIndex(idx),
				memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)

			res, err := svc.Capture(ctx, brain.CaptureInput{Text: tc.text, Channel: "telegram"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != tc.outcome || !guard.answered("capture_processing") {
				t.Fatalf("outcome %q (want %q), classification answered %v: the reply must come from a classification the model answered",
					res.Outcome, tc.outcome, guard.answered("capture_processing"))
			}
			if got := channels.RenderReply(res); got != tc.want {
				t.Errorf("reply\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// i59Sent is a channel that keeps what it was asked to send.
type i59Sent struct{ texts []string }

func (c *i59Sent) Name() string                                            { return "test" }
func (c *i59Sent) Receive(context.Context) ([]ports.ChannelMessage, error) { return nil, nil }
func (c *i59Sent) Confirm(context.Context, string) error                   { return nil }
func (c *i59Sent) Close() error                                            { return nil }
func (c *i59Sent) Send(_ context.Context, _ ports.ConversationID, text string) error {
	c.texts = append(c.texts, text)
	return nil
}

// i59Pass runs one check pass at now over the units, triggers and timers, with
// a provider that answers no task, and returns what the channel was sent.
func i59Pass(t *testing.T, now time.Time, units []unit.Unit, triggers []ports.Trigger, timers []ports.Timer) []string {
	t.Helper()
	sent, _ := i59PassRepo(t, now, units, triggers, timers)
	return sent
}

// i59PassRepo is i59Pass that also returns the timer repository, to read what a
// firing stored.
func i59PassRepo(t *testing.T, now time.Time, units []unit.Unit, triggers []ports.Trigger, timers []ports.Timer) ([]string, *memrepo.Timers) {
	t.Helper()
	ctx := context.Background()
	unitRepo, triggerRepo, timerRepo := memrepo.NewUnits(), memrepo.NewTriggers(), memrepo.NewTimers()
	for _, u := range units {
		if err := unitRepo.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	for _, tr := range triggers {
		if err := triggerRepo.Create(ctx, tr); err != nil {
			t.Fatal(err)
		}
	}
	for _, tm := range timers {
		if err := timerRepo.Create(ctx, tm); err != nil {
			t.Fatal(err)
		}
	}
	ch := &i59Sent{}
	guard := &i59Guard{t: t}
	if _, err := brain.NewCheckService(fixedClock{now: now}, triggerRepo, timerRepo, &counterIDs{}, memrepo.NewDecisionLog(), ch,
		unitRepo, memrepo.NewState(), guard, "12449194", nil, nil, nil, nil).Check(ctx, brain.CheckRequest{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(guard.tasks) != 0 {
		t.Errorf("a check pass called the provider for %v; it words nothing (I59)", guard.tasks)
	}
	return ch.texts, timerRepo
}

// i59Reminder is a unit with an event, and its reminder due a minute ago.
func i59Reminder(id, title, lang string, eventAt time.Time, rule *prospection.Rule, anchor *prospection.Anchor, lead int) (unit.Unit, ports.Trigger) {
	created := eventAt.Add(-72 * time.Hour)
	u := unit.Unit{ID: id, Type: unit.TypeEvent, Status: unit.StatusPool, Source: "telegram", Title: title, Content: title,
		Language: lang, EventAt: &eventAt, CreatedAt: created, UpdatedAt: created, LastTouchedAt: created}
	return u, ports.Trigger{ID: "trg-" + id, UnitID: &id, Kind: ports.TriggerKindTimeBased, CreatedAt: created,
		Payload: ports.TriggerPayload{ActionText: title, LeadMinutes: lead}, RecurrenceRule: rule, RecurrenceAnchor: anchor}
}

// TestI59_PushIsBuiltByCode: a reminder pass sends the pinned push of design
// §6 to the channel, in the unit's language, from a unit it read.
func TestI59_PushIsBuiltByCode(t *testing.T) {
	yearly := prospection.RuleYearly
	for _, tc := range []struct {
		name, title, lang string
		now               time.Time
		event             time.Time
		recurring         bool
		want              string
	}{
		{"event, same day, es", "Turno con el oculista", "es", time.Date(2026, 10, 20, 9, 0, 0, 0, i59Zone), time.Date(2026, 10, 20, 11, 0, 0, 0, i59Zone), false,
			"Acordate: turno con el oculista, hoy a las 11:00."},
		{"event, same day, en", "Appointment with the optician", "en", time.Date(2026, 10, 20, 9, 0, 0, 0, i59Zone), time.Date(2026, 10, 20, 11, 0, 0, 0, i59Zone), false,
			"Don't forget: Appointment with the optician, today at 11:00."},
		{"event, the day before, es", "Cena con Ana", "es", time.Date(2026, 11, 4, 10, 0, 0, 0, i59Zone), time.Date(2026, 11, 5, 21, 0, 0, 0, i59Zone), false,
			"Acordate: cena con Ana, mañana a las 21:00."},
		{"recurring, a day alone, es", "Tomar las pastillas", "es", time.Date(2026, 10, 29, 9, 0, 0, 0, i59Zone), time.Date(2026, 10, 29, 0, 0, 0, 0, i59Zone), true,
			"Acordate: tomar las pastillas, hoy."},
		{"recurring, a day alone, en", "Take the pills", "en", time.Date(2026, 10, 29, 9, 0, 0, 0, i59Zone), time.Date(2026, 10, 29, 0, 0, 0, 0, i59Zone), true,
			"Don't forget: Take the pills, today."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rule *prospection.Rule
			var anchor *prospection.Anchor
			if tc.recurring {
				rule, anchor = &yearly, &prospection.Anchor{Month: tc.event.Month(), Day: tc.event.Day()}
			}
			u, tr := i59Reminder("u1", tc.title, tc.lang, tc.event, rule, anchor, 120)
			fire := tc.now.Add(-time.Minute)
			tr.FireAt = &fire
			sent := i59Pass(t, tc.now, []unit.Unit{u}, []ports.Trigger{tr}, nil)
			if len(sent) != 1 || sent[0] != tc.want {
				t.Errorf("sent %q, want exactly %q", sent, tc.want)
			}
			if len(sent) == 1 && strings.Contains(sent[0], "Recordatorio") {
				t.Errorf("the old frame is back: %q", sent[0])
			}
		})
	}
}

// TestI59_PushKeepsANamesCapital: a first word written capitalised elsewhere in
// the unit is a name, so the push does not lower it (ADR-0064 point 3).
func TestI59_PushKeepsANamesCapital(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, i59Zone)
	u, tr := i59Reminder("u1", "Pedro cumple años", "es", time.Date(2026, 11, 5, 21, 0, 0, 0, i59Zone), nil, nil, 120)
	u.Content = "Cumpleaños de Pedro el jueves"
	fire := now.Add(-time.Minute)
	tr.FireAt = &fire
	sent := i59Pass(t, now, []unit.Unit{u}, []ports.Trigger{tr}, nil)
	if want := "Acordate: Pedro cumple años, mañana a las 21:00."; len(sent) != 1 || sent[0] != want {
		t.Errorf("sent %q, want exactly %q", sent, want)
	}
}

// TestI59_CheckPassCallsNoWordingTask: a pass with a reminder due calls the
// provider for nothing; i59Pass fails the test on any task.
func TestI59_CheckPassCallsNoWordingTask(t *testing.T) {
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, i59Zone)
	u, tr := i59Reminder("u1", "Turno con el oculista", "es", time.Date(2026, 10, 20, 11, 0, 0, 0, i59Zone), nil, nil, 120)
	fire := now.Add(-time.Minute)
	tr.FireAt = &fire
	if sent := i59Pass(t, now, []unit.Unit{u}, []ports.Trigger{tr}, nil); len(sent) != 1 {
		t.Fatalf("sent %q, want the one push the guard watched", sent)
	}
}

// TestI59_TimerRingIsBuiltByCode: a timer that rings is the fixed sentence of
// design §6, on time and late, with no provider call and no stored wording.
func TestI59_TimerRingIsBuiltByCode(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, i59Zone)
	words := "sacar la ropa"
	lateBy := time.Duration(prospection.DelayCaveatMinutes) * time.Minute
	newest := unit.Unit{ID: "n", Type: unit.TypeKnowledge, Status: unit.StatusPool, Source: "telegram", Content: "x", Language: "es",
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), LastTouchedAt: now.Add(-time.Hour)}
	for _, tc := range []struct {
		name string
		fire time.Time
		want string
	}{
		{"on time", now.Add(-time.Minute), "Es la hora: sacar la ropa."},
		{"late", now.Add(-lateBy), "Es la hora: sacar la ropa. Perdón, te llega más tarde de lo que pediste."},
	} {
		sent, timerRepo := i59PassRepo(t, now, []unit.Unit{newest}, nil, []ports.Timer{{ID: "tmr", FireAt: tc.fire, ActionText: &words, CreatedAt: now.Add(-time.Hour)}})
		if len(sent) != 1 || sent[0] != tc.want {
			t.Errorf("%s: sent %q, want exactly %q", tc.name, sent, tc.want)
		}
		if got := timerRepo.Rendered("tmr"); got != nil {
			t.Errorf("%s: rendered_text = %q, want NULL: the ring is never stored", tc.name, *got)
		}
	}
}

// TestI59_TimerWithBlankWordsRingsTheNudge: a timer whose action_text is only
// whitespace has no object, so it says the nudge.
func TestI59_TimerWithBlankWordsRingsTheNudge(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, i59Zone)
	blank := "  \t "
	newest := unit.Unit{ID: "n", Type: unit.TypeKnowledge, Status: unit.StatusPool, Source: "telegram", Content: "x", Language: "es",
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), LastTouchedAt: now.Add(-time.Hour)}
	sent := i59Pass(t, now, []unit.Unit{newest}, nil, []ports.Timer{{ID: "tmr", FireAt: now.Add(-time.Minute), ActionText: &blank, CreatedAt: now.Add(-time.Hour)}})
	if len(sent) != 1 || sent[0] != "Me pediste que te recordara algo ahora." {
		t.Errorf("sent %q, want the nudge", sent)
	}
}

// i59Morning is Wednesday 4 November 08:00 in the user's zone: the digest
// fixture of warm-replies design §6.
var i59Morning = time.Date(2026, 11, 4, 8, 0, 0, 0, i59Zone)

// i59DigestVault is one vault a morning digest runs over.
type i59DigestVault struct {
	t         *testing.T
	lang      string
	units     *memrepo.Units
	triggers  *memrepo.Triggers
	questions *memrepo.PendingQuestions
}

func newI59DigestVault(t *testing.T, lang string) *i59DigestVault {
	return &i59DigestVault{t: t, lang: lang, units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(), questions: memrepo.NewPendingQuestions()}
}

func (v *i59DigestVault) unit(u unit.Unit) {
	v.t.Helper()
	created := i59Morning.AddDate(0, 0, -7)
	u.Status, u.Source, u.Language, u.Weight = unit.StatusPool, "telegram", v.lang, 1
	u.CreatedAt, u.UpdatedAt, u.LastTouchedAt = created, created, created
	if err := v.units.Create(context.Background(), u); err != nil {
		v.t.Fatal(err)
	}
}

// task stores a task due on day November d, a day alone.
func (v *i59DigestVault) task(id, title string, d int) {
	v.unit(unit.Unit{ID: id, Type: unit.TypeTask, Title: title, Content: title, DueAt: i59At(11, d, 0, 0)})
}

// recurring stores a daily-style reminder fired for today and waiting for the
// digest, as a recurring row armed before ADR-0034 does.
func (v *i59DigestVault) recurring(id, title string) { v.recurringOf(id, title, title) }

// recurringOf is recurring for a unit whose title and content differ, or that
// has no title.
func (v *i59DigestVault) recurringOf(id, title, content string) {
	v.t.Helper()
	ctx := context.Background()
	v.unit(unit.Unit{ID: id, Type: unit.TypeEvent, Title: title, Content: content, EventAt: i59At(11, 4, 0, 0)})
	fire := time.Date(2026, 11, 4, 0, 0, 0, 0, i59Zone)
	yearly := prospection.RuleYearly
	if err := v.triggers.Create(ctx, ports.Trigger{ID: "trg-" + id, Kind: ports.TriggerKindTimeBased, FireAt: &fire, UnitID: &id,
		Payload: ports.TriggerPayload{ActionText: title, LeadDays: 7}, RecurrenceRule: &yearly,
		RecurrenceAnchor: &prospection.Anchor{Month: time.November, Day: 4}, CreatedAt: fire.Add(-time.Hour)}); err != nil {
		v.t.Fatal(err)
	}
	if err := v.triggers.Fire(ctx, "trg-"+id, fire); err != nil {
		v.t.Fatal(err)
	}
}

// question queues an uncertain relation's question about from and to.
func (v *i59DigestVault) question(from, to string) {
	v.t.Helper()
	v.questions.EnsureRelation(v.t, "rel-1", "same_topic", "a", "b", from, to)
	if err := v.questions.Create(context.Background(), ports.PendingQuestion{ID: "q-1", RelationID: "rel-1", CreatedAt: i59Morning.Add(-time.Hour)}); err != nil {
		v.t.Fatal(err)
	}
}

// digest runs the morning's pass with a provider that answers no task and
// returns the one message it sent.
func (v *i59DigestVault) digest() string {
	v.t.Helper()
	ch := fakechannel.New()
	guard := &i59Guard{t: v.t}
	if _, err := brain.NewCheckService(fixedClock{now: i59Morning}, v.triggers, memrepo.NewTimers(), &counterIDs{}, memrepo.NewDecisionLog(), ch,
		v.units, memrepo.NewState(), guard, "12449194", v.questions, nil, nil, nil).Check(context.Background(), brain.CheckRequest{}); err != nil {
		v.t.Fatalf("Check: %v", err)
	}
	if len(guard.tasks) != 0 {
		v.t.Errorf("the digest called the provider for %v; it words nothing (I59)", guard.tasks)
	}
	sent := ch.Sent(v.t)
	if len(sent) != 1 {
		v.t.Fatalf("channel received %d message(s), want exactly 1 digest: %+v", len(sent), sent)
	}
	return sent[0].Text
}

// TestI59_DigestIsBuiltByCode: the digest of design §6, byte for byte, in both
// languages: the greeting and the count, one line per thing with its day as a
// person says it, a task past its due day as "era para ayer", and the relation
// question on a line of its own after "una pregunta más". The provider answers
// nothing. The block of check-ins asked again ("Te pregunto de nuevo:") is not a
// row here: it needs an open check-in history this fixture does not build, and
// its exact bytes in both languages are pinned by
// TestRenderDigest_ReasksFollowTheDaysItems in internal/brain.
func TestI59_DigestIsBuiltByCode(t *testing.T) {
	for _, tc := range []struct {
		name, lang, want string
		fill             func(v *i59DigestVault)
	}{
		{"three kinds of line and a question, es", "es",
			"Buen día. Tenés 2 cosas:\n• Renovar el pasaporte, era para ayer\n• Tomar las pastillas, hoy\n\nUna pregunta más:\nRelacioné «Cena con Ana» con «Reservar restaurante». ¿Tienen que ver?",
			func(v *i59DigestVault) {
				v.task("passport", "Renovar el pasaporte", 3)
				v.recurring("pills", "Tomar las pastillas")
				v.question("Cena con Ana", "Reservar restaurante")
			}},
		{"three kinds of line and a question, en", "en",
			"Good morning. You have 2 things:\n• Renew the passport, was due yesterday\n• Take the pills, today\n\nOne more question:\nI linked “Dinner with Ana” with “Book a restaurant”. Are they related?",
			func(v *i59DigestVault) {
				v.task("passport", "Renew the passport", 3)
				v.recurring("pills", "Take the pills")
				v.question("Dinner with Ana", "Book a restaurant")
			}},
		{"one line, es", "es", "Buen día. Tenés una cosa:\n• Renovar el pasaporte, para hoy",
			func(v *i59DigestVault) { v.task("passport", "Renovar el pasaporte", 4) }},
		{"one line, en", "en", "Good morning. You have one thing:\n• Renew the passport, due today",
			func(v *i59DigestVault) { v.task("passport", "Renew the passport", 4) }},
		{"a question alone, es", "es", "Buen día.\n\nRelacioné «Cena con Ana» con «Reservar restaurante». ¿Tienen que ver?",
			func(v *i59DigestVault) {
				v.unit(unit.Unit{ID: "k", Type: unit.TypeKnowledge, Content: "algo"})
				v.question("Cena con Ana", "Reservar restaurante")
			}},
		{"a question alone, en", "en", "Good morning.\n\nI linked “Dinner with Ana” with “Book a restaurant”. Are they related?",
			func(v *i59DigestVault) {
				v.unit(unit.Unit{ID: "k", Type: unit.TypeKnowledge, Content: "something"})
				v.question("Dinner with Ana", "Book a restaurant")
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newI59DigestVault(t, tc.lang)
			tc.fill(v)
			if got := v.digest(); got != tc.want {
				t.Errorf("digest\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestI59_DigestOverTheBoundSaysWhatWaits: a digest that does not fit one
// message keeps the lines that do, ends saying how many wait for tomorrow, and
// stays under the bound.
func TestI59_DigestOverTheBoundSaysWhatWaits(t *testing.T) {
	for _, tc := range []struct{ lang, want string }{
		{"es", "\nY 2 más que te cuento mañana."},
		{"en", "\nAnd 2 more I'll tell you about tomorrow."},
	} {
		v := newI59DigestVault(t, tc.lang)
		for i := range 10 {
			v.task(fmt.Sprintf("t%d", i), fmt.Sprintf("Task %d %s", i, strings.Repeat("x", 480)), 4)
		}
		got := v.digest()
		if !strings.HasSuffix(got, tc.want) {
			t.Errorf("%s: digest ends %q, want it to end %q", tc.lang, got[max(0, len(got)-80):], tc.want)
		}
		if n := telegramUnits(got); n > 4096 {
			t.Errorf("%s: digest is %d UTF-16 units, want at most 4096", tc.lang, n)
		}
	}
}

// i59Armed is a capture that armed a reminder, told at i59Now.
func i59Armed(lang classify.Language, armed brain.Armed) brain.CaptureResult {
	return brain.CaptureResult{Outcome: brain.OutcomeArmed, Language: lang, At: i59Now, Armed: &armed}
}

// TestI59_ConfirmationIsBuiltByCode: every capture confirmation of design §6,
// exact text, in both languages, through the channel's own renderer.
func TestI59_ConfirmationIsBuiltByCode(t *testing.T) {
	at := func(m time.Month, d, h, min int) time.Time { return *i59At(m, d, h, min) }
	daily := prospection.RuleDaily
	rows := []struct {
		name           string
		res            brain.CaptureResult
		wantES, wantEN string
	}{
		{"plain note", brain.CaptureResult{Outcome: brain.OutcomeStored, At: i59Now},
			"Listo, anotado.", "Got it, noted."},
		{"event armed", i59Armed("", brain.Armed{What: prospection.ArmTrigger, About: at(11, 12, 10, 0), FireAt: at(11, 11, 10, 0), Later: []time.Time{at(11, 12, 7, 30)}}),
			"Listo, anotado para el jueves 12 de noviembre a las 10:00. Te aviso el día anterior y unas horas antes.",
			"Got it, noted for Thursday 12 November at 10:00. I'll remind you the day before and a few hours before."},
		{"day alone, asked", i59Armed("", brain.Armed{What: prospection.ArmTrigger, About: at(11, 13, 0, 0), DayOnly: true, FireAt: at(11, 13, 9, 0)}),
			"Listo, anotado para el viernes 13 de noviembre. Te aviso ese día a las 9:00.",
			"Got it, noted for Friday 13 November. I'll remind you that day at 9:00."},
		{"task to the digest", brain.CaptureResult{Outcome: brain.OutcomeStored, At: i59Now, DueInDigest: i59At(11, 9, 0, 0)},
			"Listo, anotado para el lunes 9 de noviembre. Te lo recuerdo en el resumen de esa mañana.",
			"Got it, noted for Monday 9 November. I'll mention it in that morning's digest."},
		{"event nothing was armed for", brain.CaptureResult{Outcome: brain.OutcomeStored, At: i59Now, NotArmedFor: i59At(11, 5, 21, 0)},
			"Listo, anotado para mañana a las 21:00.", "Got it, noted for tomorrow at 21:00."},
		{"daily", i59Armed("", brain.Armed{What: prospection.ArmRecurring, Rule: daily, About: at(11, 5, 9, 0), FireAt: at(11, 5, 9, 0)}),
			"Listo, te aviso todos los días a las 9:00.", "Sure, I'll remind you every day at 9:00."},
		{"timer, tomorrow", i59Armed("", brain.Armed{What: prospection.ArmTimer, About: at(11, 5, 0, 20), FireAt: at(11, 5, 0, 20)}),
			"Listo, te aviso mañana a las 0:20.", "Sure, I'll remind you tomorrow at 0:20."},
		{"timer, in the hour", i59Armed("", brain.Armed{What: prospection.ArmTimer, About: at(11, 4, 10, 20), FireAt: at(11, 4, 10, 20)}),
			"Listo, te aviso a las 10:20.", "Sure, I'll remind you at 10:20."},
		{"asked, past", brain.CaptureResult{Outcome: brain.OutcomeStored, At: i59Now, ArmRefused: &brain.ArmRefused{Why: prospection.RefusalAlreadyPast}},
			"Lo anoté, pero no te puse aviso: esa hora ya pasó.", "I noted it, but I didn't set a reminder: that time has already passed."},
		{"refused", brain.CaptureResult{Outcome: brain.OutcomeArmRefused, At: i59Now, ArmRefused: &brain.ArmRefused{Why: prospection.RefusalAlreadyPast}},
			"No lo programé: esa hora ya pasó", "I did not set that: that time has already passed"},
	}
	for _, r := range rows {
		for _, l := range []struct {
			lang classify.Language
			want string
		}{{classify.LanguageES, r.wantES}, {classify.LanguageEN, r.wantEN}} {
			res := r.res
			res.Language = l.lang
			if got := channels.RenderReply(res); got != l.want {
				t.Errorf("%s (%s): reply\n%q\nwant\n%q", r.name, l.lang, got, l.want)
			}
		}
	}
}

// TestI59_CorrectionIsBuiltByCode: every correction answer of design §6, exact
// text, in both languages.
func TestI59_CorrectionIsBuiltByCode(t *testing.T) {
	moved := i59At(11, 5, 11, 0)
	rows := []struct {
		name           string
		c              brain.Correction
		wantES, wantEN string
	}{
		{"moved, with leads", brain.Correction{UnitID: "u", MovedTo: moved, About: *moved,
			Fields:    []correction.Field{correction.FieldReminderLeads},
			Reminders: []time.Time{*i59At(11, 5, 7, 30), *i59At(11, 5, 10, 45)}},
			"Listo, lo pasé para mañana a las 11:00. Te aviso unas horas antes y 15 minutos antes.",
			"Done, I moved it to tomorrow at 11:00. I'll remind you a few hours before and 15 minutes before."},
		{"moved, a day alone", brain.Correction{UnitID: "u", MovedTo: i59At(11, 13, 0, 0), MovedToDayOnly: true},
			"Listo, lo pasé para el viernes 13 de noviembre.", "Done, I moved it to Friday 13 November."},
		{"corrected", brain.Correction{UnitID: "u"}, "Listo, lo corregí.", "Done, I fixed it."},
		{"unchanged", brain.Correction{UnitID: "u", Unchanged: true},
			"Ya decía eso, así que no cambié nada.", "It already said that, so I changed nothing."},
	}
	for _, r := range rows {
		for _, l := range []struct {
			lang classify.Language
			want string
		}{{classify.LanguageES, r.wantES}, {classify.LanguageEN, r.wantEN}} {
			c := r.c
			res := brain.CaptureResult{Outcome: brain.OutcomeCorrected, Language: l.lang, At: i59Now, Correction: &c}
			if got := channels.RenderReply(res); got != l.want {
				t.Errorf("%s (%s): reply\n%q\nwant\n%q", r.name, l.lang, got, l.want)
			}
		}
	}
}

// TestI59_TimersListNamesEachClockAsCodeSaysIt: the timers' list keeps its
// structure (ADR-0051) and every clock is the one Clock writes, with no leading
// zero.
func TestI59_TimersListNamesEachClockAsCodeSaysIt(t *testing.T) {
	words := "sacar la ropa"
	res := brain.CaptureResult{Outcome: brain.OutcomeTimers, Language: classify.LanguageES, At: i59Now,
		Timers: &brain.TimersAnswer{Listed: []brain.ListedTimer{{ID: "a", FireAt: *i59At(11, 4, 10, 20), Text: &words}, {ID: "b", FireAt: *i59At(11, 4, 9, 5)}}}}
	if got, want := channels.RenderReply(res), "Hay 2 temporizadores puestos:\n1) 10:20 \u2014 sacar la ropa\n2) 9:05"; got != want {
		t.Errorf("list\n%q\nwant\n%q", got, want)
	}
}

// TestI59_UntitledDatedUnitIsNeverSaidByItsContent: a unit with an instant and
// no title is said as something noted at that instant, on every surface that
// puts an instant beside an item's words (ADR-0065 point 5). Its content
// carries the date already (the capture writes it as YYYY-MM-DD), so saying
// both repeated it and could say a relative word that is no longer true.
func TestI59_UntitledDatedUnitIsNeverSaidByItsContent(t *testing.T) {
	const contentES = "tengo reunión con Juan el 2026-11-04 a las 22:30, hoy"
	const contentEN = "meeting with John on 2026-11-04 at 22:30, today"
	notTheContent := func(t *testing.T, got string) {
		t.Helper()
		for _, leak := range []string{"2026-", "Juan", "John", "reunión", "meeting"} {
			if strings.Contains(got, leak) {
				t.Errorf("%q says the unit's content (%q)", got, leak)
			}
		}
	}

	t.Run("recall", func(t *testing.T) {
		for _, tc := range []struct {
			lang    classify.Language
			content string
			event   *time.Time
			want    string
		}{
			{classify.LanguageES, contentES, i59At(11, 26, 8, 30), "Tenés algo anotado el jueves 26 de noviembre a las 8:30."},
			{classify.LanguageEN, contentEN, i59At(11, 26, 8, 30), "You have something noted on Thursday 26 November at 8:30."},
			{classify.LanguageES, contentES, i59At(11, 3, 9, 0), "Tenías algo anotado ayer a las 9:00."},
			{classify.LanguageEN, contentEN, i59At(11, 3, 9, 0), "You had something noted yesterday at 9:00."},
		} {
			res := brain.CaptureResult{Outcome: brain.OutcomeRecalled, Language: tc.lang, At: i59Now,
				Recalled: []unit.Unit{{Content: tc.content, EventAt: tc.event}}}
			got := channels.RenderReply(res)
			if got != tc.want {
				t.Errorf("recall (%s) = %q, want %q", tc.lang, got, tc.want)
			}
			notTheContent(t, got)
		}
	})

	t.Run("recall, due day only", func(t *testing.T) {
		for _, tc := range []struct {
			lang    classify.Language
			content string
			want    string
		}{
			{classify.LanguageES, "renovar el pasaporte el 2026-11-09", "Tenés algo anotado para el lunes 9 de noviembre."},
			{classify.LanguageEN, "renew the passport on 2026-11-09", "You have something noted due Monday 9 November."},
		} {
			res := brain.CaptureResult{Outcome: brain.OutcomeRecalled, Language: tc.lang, At: i59Now,
				Recalled: []unit.Unit{{Type: unit.TypeTask, Content: tc.content, DueAt: i59At(11, 9, 0, 0)}}}
			got := channels.RenderReply(res)
			if got != tc.want {
				t.Errorf("recall of a due day (%s) = %q, want %q", tc.lang, got, tc.want)
			}
			notTheContent(t, got)
			if strings.Contains(got, "pasaporte") || strings.Contains(got, "passport") {
				t.Errorf("%q says the unit's content", got)
			}
		}
	})

	t.Run("recall, several", func(t *testing.T) {
		res := brain.CaptureResult{Outcome: brain.OutcomeRecalled, Language: classify.LanguageES, At: i59Now,
			Recalled: []unit.Unit{
				{Content: contentES, EventAt: i59At(11, 4, 22, 30)},
				{Title: "Cena con Ana", EventAt: i59At(11, 13, 21, 0)}}}
		want := "Tenés 2 cosas anotadas:\n• Algo anotado, hoy a las 22:30\n• Cena con Ana, el viernes 13 de noviembre a las 21:00"
		if got := channels.RenderReply(res); got != want {
			t.Errorf("recall many =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("push", func(t *testing.T) {
		now := time.Date(2026, 11, 4, 22, 0, 0, 0, i59Zone)
		event := time.Date(2026, 11, 4, 22, 30, 0, 0, i59Zone)
		for _, tc := range []struct{ lang, content, want string }{
			{"es", contentES, "Acordate: tenés algo anotado hoy a las 22:30."},
			{"en", contentEN, "Don't forget: you have something noted today at 22:30."},
		} {
			u, tr := i59Reminder("u1", "", tc.lang, event, nil, nil, 120)
			u.Content = tc.content
			fire := now.Add(-time.Minute)
			tr.FireAt = &fire
			sent := i59Pass(t, now, []unit.Unit{u}, []ports.Trigger{tr}, nil)
			if len(sent) != 1 || sent[0] != tc.want {
				t.Errorf("push (%s) sent %q, want exactly %q", tc.lang, sent, tc.want)
			}
			for _, s := range sent {
				notTheContent(t, s)
			}
		}
	})

	t.Run("digest", func(t *testing.T) {
		for _, tc := range []struct{ lang, content, want string }{
			{"es", "renovar el pasaporte el 2026-11-03", "Buen día. Tenés una cosa:\n• Algo anotado, era para ayer"},
			{"en", "renew the passport on 2026-11-03", "Good morning. You have one thing:\n• Something noted, was due yesterday"},
		} {
			v := newI59DigestVault(t, tc.lang)
			v.unit(unit.Unit{ID: "passport", Type: unit.TypeTask, Content: tc.content, DueAt: i59At(11, 3, 0, 0)})
			got := v.digest()
			if got != tc.want {
				t.Errorf("digest (%s) =\n%q\nwant\n%q", tc.lang, got, tc.want)
			}
			if strings.Contains(got, "2026-") || strings.Contains(got, "pasaporte") || strings.Contains(got, "passport") {
				t.Errorf("digest says the unit's content: %q", got)
			}
		}
	})
}

// TestI59_UntitledRecurringUnitWithNoInstantKeepsItsWords: the unit is dated
// for the fallback only when it carries an event_at or a due_at of its own.
// A recurring reminder with no title and neither (a shape the code allows, not
// the usual one: a recurring unit is born with its event_at) is pushed with the day the pass computes, but that day is the reply's, not the
// unit's: its words are all it has, so the push says them, not "something
// noted" (ADR-0065 point 5).
func TestI59_UntitledRecurringUnitWithNoInstantKeepsItsWords(t *testing.T) {
	yearly := prospection.RuleYearly
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, i59Zone)
	for _, tc := range []struct{ lang, content, want string }{
		{"es", "tomar las pastillas", "Acordate: tomar las pastillas, hoy."},
		{"en", "take the pills", "Don't forget: take the pills, today."},
	} {
		u, tr := i59Reminder("u1", "", tc.lang, time.Date(2026, 10, 29, 0, 0, 0, 0, i59Zone),
			&yearly, &prospection.Anchor{Month: now.Month(), Day: now.Day()}, 120)
		u.Content, u.EventAt, u.DueAt = tc.content, nil, nil
		fire := now.Add(-time.Minute)
		tr.FireAt = &fire
		sent := i59Pass(t, now, []unit.Unit{u}, []ports.Trigger{tr}, nil)
		if len(sent) != 1 || sent[0] != tc.want {
			t.Errorf("push (%s) sent %q, want exactly %q", tc.lang, sent, tc.want)
		}
	}
}

// TestI59_UntitledDatedUnitIsNotSaidByItsContentInTheDigestAndToday: the lines
// a fired reminder waiting for the digest becomes, in the digest and in Today's
// preview of it, and Today's upcoming reminders, are the unit's instant and
// none of its words (ADR-0065 point 5). They go through brain's saidNow, which
// the due-task lines do not.
func TestI59_UntitledDatedUnitIsNotSaidByItsContentInTheDigestAndToday(t *testing.T) {
	const contentES = "cumpleaños de Ana el 2026-11-04"
	const contentEN = "Ana's birthday on 2026-11-04"
	for _, tc := range []struct {
		lang, content, wantLine string
	}{
		{"es", contentES, "• Algo anotado, hoy"},
		{"en", contentEN, "• Something noted, today"},
	} {
		t.Run("digest, "+tc.lang, func(t *testing.T) {
			v := newI59DigestVault(t, tc.lang)
			v.recurringOf("birthday", "", tc.content)
			got := v.digest()
			if !strings.Contains(got, tc.wantLine) {
				t.Errorf("digest\n%q\nwant it to hold %q", got, tc.wantLine)
			}
			if strings.Contains(got, "2026-") || strings.Contains(got, "Ana") {
				t.Errorf("digest says the unit's content: %q", got)
			}
		})
		t.Run("today, "+tc.lang, func(t *testing.T) {
			v := newI59DigestVault(t, tc.lang)
			v.recurringOf("birthday", "", tc.content)
			today := v.today()
			if len(today.Digest.Items) != 1 || !strings.HasSuffix(tc.wantLine, today.Digest.Items[0].Text) {
				t.Errorf("Today's digest preview = %+v, want one item that %q ends with", today.Digest.Items, tc.wantLine)
			}
		})
	}

	t.Run("today, upcoming", func(t *testing.T) {
		for _, tc := range []struct{ lang, content, want string }{
			{"es", "reunión con Juan el 2026-11-04 a las 11:00", "Algo anotado, hoy a las 11:00"},
			{"en", "meeting with John on 2026-11-04 at 11:00", "Something noted, today at 11:00"},
		} {
			v := newI59DigestVault(t, tc.lang)
			event := time.Date(2026, 11, 4, 11, 0, 0, 0, i59Zone)
			v.unit(unit.Unit{ID: "meeting", Type: unit.TypeEvent, Content: tc.content, EventAt: &event})
			fire := i59Morning.Add(time.Hour)
			id := "meeting"
			if err := v.triggers.Create(context.Background(), ports.Trigger{ID: "trg-meeting", UnitID: &id, Kind: ports.TriggerKindTimeBased,
				FireAt: &fire, CreatedAt: i59Morning.Add(-time.Hour), Payload: ports.TriggerPayload{LeadMinutes: 120}}); err != nil {
				t.Fatal(err)
			}
			up := v.today().Upcoming
			if len(up) != 1 || up[0].Text != tc.want {
				t.Errorf("Today upcoming (%s) = %+v, want one line %q", tc.lang, up, tc.want)
			}
		}
	})
}

// today runs Today over the vault at the digest's morning.
func (v *i59DigestVault) today() brain.Today {
	v.t.Helper()
	cfg := memrepo.NewConfig()
	svc := brain.NewTodayService(fixedClock{now: i59Morning}, v.units, cfg, memrepo.NewState(), v.triggers, v.questions, memrepo.NewDecisionLog(),
		brain.NewFocusKeeper(v.units, cfg, memrepo.NewRelations()), nil)
	out, err := svc.Today(context.Background())
	if err != nil {
		v.t.Fatalf("Today: %v", err)
	}
	return out
}

// TestI59_UntitledRecurringUnitWithAnEventIsSaidAsSomethingNoted: the common
// shape of a recurring reminder with no title is born with an event_at (its
// anchor), so it is dated by its own instant: it is pushed as something noted
// today, with none of its words. Until the title is named at capture (slice 3
// of ADR-0065), that is what it says.
func TestI59_UntitledRecurringUnitWithAnEventIsSaidAsSomethingNoted(t *testing.T) {
	yearly := prospection.RuleYearly
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, i59Zone)
	for _, tc := range []struct{ lang, content, want string }{
		{"es", "cumpleaños de Ana el 2026-10-29", "Acordate: tenés algo anotado hoy."},
		{"en", "Ana's birthday on 2026-10-29", "Don't forget: you have something noted today."},
	} {
		u, tr := i59Reminder("u1", "", tc.lang, time.Date(2026, 10, 29, 0, 0, 0, 0, i59Zone),
			&yearly, &prospection.Anchor{Month: now.Month(), Day: now.Day()}, 120)
		u.Content = tc.content
		fire := now.Add(-time.Minute)
		tr.FireAt = &fire
		sent := i59Pass(t, now, []unit.Unit{u}, []ports.Trigger{tr}, nil)
		if len(sent) != 1 || sent[0] != tc.want {
			t.Errorf("push (%s) sent %q, want exactly %q", tc.lang, sent, tc.want)
		}
	}
}

// TestI59_UntitledDatedUnitWithNoComputedInstantSaysItsOwnDate: a push that
// cannot compute the instant (an old-style trigger with no lead and no event,
// a recurring row with no anchor) says the unit's own date, never its content,
// which carries that date as YYYY-MM-DD (ADR-0065 point 5).
func TestI59_UntitledDatedUnitWithNoComputedInstantSaysItsOwnDate(t *testing.T) {
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, i59Zone)
	yearly := prospection.RuleYearly
	for _, tc := range []struct {
		name, lang, content string
		due                 *time.Time
		event               *time.Time
		rule                *prospection.Rule
		want                string
	}{
		{"task due, es", "es", "renovar el pasaporte el 2026-11-05", i59At(11, 5, 0, 0), nil, nil,
			"Acordate: tenés algo anotado para el jueves 5 de noviembre."},
		{"task due, en", "en", "renew the passport on 2026-11-05", i59At(11, 5, 0, 0), nil, nil,
			"Don't forget: you have something noted due Thursday 5 November."},
		{"event of a recurring row with no anchor, es", "es", "turno el 2026-11-26 a las 8:30", nil, i59At(11, 26, 8, 30), &yearly,
			"Acordate: tenés algo anotado el jueves 26 de noviembre a las 8:30."},
		{"event of a recurring row with no anchor, en", "en", "appointment on 2026-11-26 at 8:30", nil, i59At(11, 26, 8, 30), &yearly,
			"Don't forget: you have something noted on Thursday 26 November at 8:30."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, tr := i59Reminder("u1", "", tc.lang, time.Date(2026, 11, 5, 0, 0, 0, 0, i59Zone), tc.rule, nil, 0)
			u.Content, u.EventAt, u.DueAt = tc.content, tc.event, tc.due
			if tc.due != nil {
				u.Type = unit.TypeTask
			}
			fire := now.Add(-time.Minute)
			tr.FireAt = &fire
			tr.Payload.ActionText = ""
			sent := i59Pass(t, now, []unit.Unit{u}, []ports.Trigger{tr}, nil)
			if len(sent) != 1 || sent[0] != tc.want {
				t.Errorf("push sent %q, want exactly %q", sent, tc.want)
			}
		})
	}
}
