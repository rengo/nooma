// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"strings"
	"sync"
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

// I59 (doc 02 §5 step 2, ADR-0064): a reply that carries a fact is built by
// code, in warm fixed sentences. No model words it and none waits for one.
// This file pins the recall and push surfaces; each later surface of the
// warm-replies change adds its own rows here.

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
	return ch.texts
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
		sent := i59Pass(t, now, []unit.Unit{newest}, nil, []ports.Timer{{ID: "tmr", FireAt: tc.fire, ActionText: &words, CreatedAt: now.Add(-time.Hour)}})
		if len(sent) != 1 || sent[0] != tc.want {
			t.Errorf("%s: sent %q, want exactly %q", tc.name, sent, tc.want)
		}
	}
}
