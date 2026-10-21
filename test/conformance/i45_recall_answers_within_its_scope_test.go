// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// The vault of the live session that found the four failures I45 pins
// (ADR-0048): Wednesday 2026-10-21 18:30 -03, five units told on 10-19,
// nothing told on 10-20, and a few told earlier.
var (
	i45Zone = time.FixedZone("UTC-3", -3*60*60)
	i45Now  = time.Date(2026, 10, 21, 18, 30, 0, 0, i45Zone)
)

func i45At(month time.Month, day, hour, minute int) *time.Time {
	t := time.Date(2026, month, day, hour, minute, 0, 0, i45Zone)
	return &t
}

type i45Unit struct {
	id, title, content string
	typ                unit.Type
	told               *time.Time
	event, due         *time.Time
}

var i45Vault = []i45Unit{
	{"dentist-past", "Turno con el dentista", "Tengo una cita con el dentista el 2026-10-15 a las 11:00.", unit.TypeEvent, i45At(10, 19, 10, 0), i45At(10, 15, 11, 0), nil},
	{"dentist-friday", "Dentista", "Dentista el 2026-10-23 a las 10:00.", unit.TypeEvent, i45At(10, 19, 10, 5), i45At(10, 23, 10, 0), nil},
	{"padel", "Pádel con Leo", "Pádel con Leo el 2026-10-24 a las 19:00.", unit.TypeEvent, i45At(10, 19, 11, 0), i45At(10, 24, 19, 0), nil},
	{"passport", "Renovar el pasaporte", "Renovar el pasaporte antes del 2026-10-25.", unit.TypeTask, i45At(10, 19, 12, 0), nil, i45At(10, 25, 0, 0)},
	{"oculist", "Turno con el oculista", "Turno con el oculista el 2026-10-20 a las 10:00.", unit.TypeEvent, i45At(10, 19, 13, 0), i45At(10, 20, 10, 0), nil},
	{"birthday", "Cumpleaños de Ana", "Cumpleaños de Ana el 2026-10-22.", unit.TypeEvent, i45At(10, 12, 9, 0), i45At(10, 22, 0, 0), nil},
	{"meeting-this-morning", "Reunión de equipo", "Reunión de equipo el 2026-10-21 a las 09:00.", unit.TypeEvent, i45At(10, 18, 9, 0), i45At(10, 21, 9, 0), nil},
	{"dentist-checkup", "Control anual", "Control anual con el dentista el 2026-11-10 a las 09:00.", unit.TypeEvent, i45At(10, 10, 9, 0), i45At(11, 10, 9, 0), nil},
	{"coffee", "Café", "Me gusta el café sin azúcar.", unit.TypeKnowledge, i45At(10, 10, 8, 0), nil, nil},
}

// i45Recall seeds i45Vault into both recall legs and answers text with the
// scripted reading, as the chat does.
func i45Recall(t *testing.T, text, reading string) []string {
	t.Helper()
	ctx := context.Background()
	units, embeddings, lexical := memrepo.NewUnits(), memrepo.NewEmbeddings(), memrepo.NewLexical()
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	for _, s := range i45Vault {
		u := unit.Unit{ID: s.id, Type: s.typ, Status: unit.StatusPool, Source: "telegram", Title: s.title, Content: s.content,
			EventAt: s.event, DueAt: s.due, CreatedAt: *s.told, UpdatedAt: *s.told, LastTouchedAt: *s.told}
		if err := units.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
		v, err := embed.Embed(ctx, ports.EmbedRequest{Text: s.content})
		if err != nil {
			t.Fatal(err)
		}
		if err := embeddings.Put(ctx, ports.Embedding{UnitID: s.id, Model: embedFakeModel, Vector: v.Vector, At: *s.told}); err != nil {
			t.Fatal(err)
		}
		lexical.SeedLexical(t, s.id, s.content)
	}
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	svc := brain.NewCaptureService(fixedClock{now: i45Now}, &counterIDs{}, units, embeddings, lexical, memrepo.NewRelations(),
		memrepo.NewDecisionLog(), &scriptedLLM{response: reading}, nil, nil, embed, brain.NewIndex(idx),
		memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)
	res, err := svc.Capture(ctx, brain.CaptureInput{Text: text, Channel: "telegram"})
	if err != nil {
		t.Fatalf("Capture(%q): %v", text, err)
	}
	if res.Outcome != brain.OutcomeRecalled {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, brain.OutcomeRecalled)
	}
	if n := units.Count(); n != len(i45Vault) {
		t.Errorf("units %d, want %d: a recall stores nothing", n, len(i45Vault))
	}
	ids := make([]string, len(res.Recalled))
	for i, u := range res.Recalled {
		ids[i] = u.ID
	}
	return ids
}

const i45Head = `{"type":"recall","normalized_content":"x","weight":0.2,"decay_rate":0.3,"language":"es"`

// TestI45_ATimeWindowListsWhatWasToldThen: "¿y anteayer?" read as what was
// told on 10-19 lists the five units told that day, in the order they were
// told, and nothing else. Live, the raw text found nothing.
func TestI45_ATimeWindowListsWhatWasToldThen(t *testing.T) {
	got := i45Recall(t, "¿y anteayer?", i45Head+
		`,"recall_scope":{"window":{"on":"told","from":"2026-10-19","to":"2026-10-20"}}}`)
	if want := []string{"dentist-past", "dentist-friday", "padel", "passport", "oculist"}; !slices.Equal(got, want) {
		t.Errorf("recalled %v, want %v", got, want)
	}
}

// TestI45_WhatTheUserHasIsWhatIsAhead: "¿qué tengo esta semana?" read as
// from now to next Monday lists what is ahead this week by its own instant —
// a task's due day included — and nothing already past, this morning's
// meeting included. Live, it returned a past dentist and missed the rest.
func TestI45_WhatTheUserHasIsWhatIsAhead(t *testing.T) {
	got := i45Recall(t, "¿qué tengo esta semana?", i45Head+
		`,"recall_scope":{"window":{"on":"when","from":"2026-10-21T18:30:00-03:00","to":"2026-10-26"}}}`)
	if want := []string{"birthday", "dentist-friday", "padel", "passport"}; !slices.Equal(got, want) {
		t.Errorf("recalled %v, want %v", got, want)
	}
}

// TestI45_ATermAnswersWithWhatNamesIt: "¿cuándo era lo del oculista?" read
// with its terms answers with the oculist alone. Live, every appointment
// cleared the similarity floor and six came back.
func TestI45_ATermAnswersWithWhatNamesIt(t *testing.T) {
	got := i45Recall(t, "¿cuándo era lo del oculista?", i45Head+
		`,"recall_scope":{"terms":["oculista","oculistas","oftalmólogo"]}}`)
	if want := []string{"oculist"}; !slices.Equal(got, want) {
		t.Errorf("recalled %v, want %v", got, want)
	}
}

// TestI45_ASynonymTheModelWroteFindsTheUnit: "¿cuándo tengo el odontólogo?"
// finds the dentist, because the model wrote "dentista" among its terms (the
// client: "si digo dentista, que sepa que es odontólogo también"). The code
// expands no word.
func TestI45_ASynonymTheModelWroteFindsTheUnit(t *testing.T) {
	got := i45Recall(t, "¿cuándo tengo el odontólogo?", i45Head+
		`,"recall_scope":{"terms":["odontólogo","odontóloga","dentista","dentistas"]}}`)
	slices.Sort(got)
	if want := []string{"dentist-checkup", "dentist-friday", "dentist-past"}; !slices.Equal(got, want) {
		t.Errorf("recalled %v, want %v", got, want)
	}
}

// TestI45_TermsAndAWindowNarrowTogether: "¿qué tengo con el dentista esta
// semana?" is the dentist ahead this week, not every dentist.
func TestI45_TermsAndAWindowNarrowTogether(t *testing.T) {
	got := i45Recall(t, "¿qué tengo con el dentista esta semana?", i45Head+
		`,"recall_scope":{"terms":["dentista","odontólogo"],"window":{"on":"when","from":"2026-10-21T18:30:00-03:00","to":"2026-10-26"}}}`)
	if want := []string{"dentist-friday"}; !slices.Equal(got, want) {
		t.Errorf("recalled %v, want %v", got, want)
	}
}

// TestI45_TermsNothingNamesFallBackToTheFloor: terms no unit holds answer as
// a recall with no scope did — whatever cleared the similarity floor — rather
// than with nothing: a word the model chose is not a reason to say "nothing".
func TestI45_TermsNothingNamesFallBackToTheFloor(t *testing.T) {
	const asked = "¿qué sé del kinesiólogo?"
	scoped := i45Recall(t, asked, i45Head+`,"recall_scope":{"terms":["kinesiólogo","kinesióloga"]}}`)
	unscoped := i45Recall(t, asked, i45Head+`}`)
	if len(unscoped) == 0 || !slices.Equal(scoped, unscoped) {
		t.Errorf("terms nothing names recalled %v, want the floor's own %v (non-empty)", scoped, unscoped)
	}
}
