// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"sync"
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

// I59 (doc 02 §5 step 2, ADR-0064): a reply that carries a fact is built by
// code, in warm fixed sentences. No model words it and none waits for one.
// This file pins the recall surface; each later surface of the warm-replies
// change adds its own rows here.

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
