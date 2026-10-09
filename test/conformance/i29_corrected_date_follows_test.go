// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// i29Fixture is one vault holding one referent unit, wired to a capture
// service that replays one classify recording.
type i29Fixture struct {
	svc       *brain.CaptureService
	units     *memrepo.Units
	triggers  *memrepo.Triggers
	decisions *memrepo.DecisionLog
}

const i29Message = "en realidad es hoy viernes 9 a las 11"

// newI29 seeds u and, for the chat path, makes it the one strong recall
// match for i29Message.
func newI29(t *testing.T, now time.Time, u unit.Unit, response string) i29Fixture {
	t.Helper()
	ctx := context.Background()
	f := i29Fixture{units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(), decisions: memrepo.NewDecisionLog()}
	if err := f.units.Create(ctx, u); err != nil {
		t.Fatalf("seeding the referent: %v", err)
	}
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	match, err := embed.Embed(ctx, ports.EmbedRequest{Text: i29Message})
	if err != nil {
		t.Fatalf("deriving the match vector: %v", err)
	}
	if err := embeddings.Put(ctx, ports.Embedding{UnitID: u.ID, Model: embedFakeModel, Vector: match.Vector, At: now}); err != nil {
		t.Fatalf("seeding the embedding: %v", err)
	}
	lexical.SeedLexical(t, u.ID, i29Message)
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	llm := fakeprovider.New(t, i28Case(t, "i29", i29Message, response), "i29")
	f.svc = brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, f.units, embeddings, lexical, memrepo.NewRelations(), f.decisions, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx), memrepo.NewSignals(), f.triggers, memrepo.NewTimers(), 0.5, nil)
	return f
}

func (f i29Fixture) capture(t *testing.T, referent string) {
	t.Helper()
	result, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: i29Message, Channel: "ui", ReferentID: referent})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if result.Outcome != brain.OutcomeCorrected {
		t.Fatalf("result = %+v, want %q", result, brain.OutcomeCorrected)
	}
}

func (f i29Fixture) unit(t *testing.T, id string) unit.Unit {
	t.Helper()
	u, err := f.units.ByID(context.Background(), id)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	return u
}

// rows returns the decision rows whose action is one of actions, in order.
func (f i29Fixture) rows(t *testing.T, actions ...ports.DecisionAction) []ports.Decision {
	t.Helper()
	all, err := f.decisions.Since(context.Background(), time.Time{}, 100)
	if err != nil {
		t.Fatalf("decisions.Since: %v", err)
	}
	var out []ports.Decision
	for _, d := range all {
		for _, a := range actions {
			if d.Action == a {
				out = append(out, d)
			}
		}
	}
	return out
}

// i29Change is the change-shaped context every row this invariant writes
// carries, so /ui/activity renders it as previous -> next.
type i29Change struct {
	UnitID    string         `json:"unit_id"`
	TriggerID string         `json:"trigger_id"`
	Fields    []string       `json:"fields"`
	Previous  map[string]any `json:"previous"`
	Next      map[string]any `json:"next"`
}

func decodeI29(t *testing.T, d ports.Decision) i29Change {
	t.Helper()
	var c i29Change
	if err := json.Unmarshal(d.Context, &c); err != nil {
		t.Fatalf("decoding %s: %v", d.Context, err)
	}
	return c
}

// TestI29_CorrectedDateCarriesTheText is doc 02 §5 step 4's text half of
// I29: a correction that moves a date rewrites the date and time the body
// states in capture's own form, inside the same correction.applied row,
// on both correction paths.
func TestI29_CorrectedDateCarriesTheText(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	original := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	const before = "Dentista el 2026-10-16 a las 10:00"
	const after = "Dentista el 2026-10-09 a las 11:00"
	event := unit.Unit{ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool, Content: before,
		EventAt: &original, Source: "cli", CreatedAt: now, UpdatedAt: now}

	assertCarried := func(t *testing.T, f i29Fixture) {
		t.Helper()
		if got := f.unit(t, "dentist").Content; got != after {
			t.Errorf("content = %q, want %q", got, after)
		}
		applied := f.rows(t, ports.ActionCorrectionApplied)
		if len(applied) != 1 {
			t.Fatalf("correction.applied rows = %d, want 1", len(applied))
		}
		c := decodeI29(t, applied[0])
		if len(c.Fields) != 2 || c.Fields[0] != "event_at" || c.Fields[1] != "content" {
			t.Errorf("fields = %v, want [event_at content]", c.Fields)
		}
		if c.Previous["content"] != before || c.Next["content"] != after {
			t.Errorf("content previous/next = %v -> %v, want %q -> %q", c.Previous["content"], c.Next["content"], before, after)
		}
	}

	t.Run("explicit referent", func(t *testing.T) {
		f := newI29(t, now, event, `{"type":"event","normalized_content":"Dentista hoy a las 11:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T11:00:00Z","language":"es"}`)
		f.capture(t, "dentist")
		assertCarried(t, f)
	})

	t.Run("chat path, referent found by recall", func(t *testing.T) {
		f := newI29(t, now, event, `{"type":"correction","normalized_content":"Es hoy a las 11:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T11:00:00Z","language":"es"}`)
		f.capture(t, "")
		assertCarried(t, f)
	})

	t.Run("a due_at correction carries the text too", func(t *testing.T) {
		due := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
		task := unit.Unit{ID: "bill", Type: unit.TypeTask, Status: unit.StatusPool, Content: "Pagar la luz antes del 2026-10-20",
			DueAt: &due, Source: "cli", CreatedAt: now, UpdatedAt: now}
		f := newI29(t, now, task, `{"type":"correction","normalized_content":"Vence el 2026-10-22","weight":0.6,"decay_rate":0.03,"due_at":"2026-10-22T12:00:00Z","language":"es"}`)
		f.capture(t, "bill")
		if got := f.unit(t, "bill").Content; got != "Pagar la luz antes del 2026-10-22" {
			t.Errorf("content = %q, want the due date rewritten", got)
		}
	})

	t.Run("a body naming the date another way is left as written", func(t *testing.T) {
		v := event
		v.Content = "Dentista el viernes a las 10"
		f := newI29(t, now, v, `{"type":"event","normalized_content":"Dentista hoy a las 11:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T11:00:00Z","language":"es"}`)
		f.capture(t, "dentist")
		if got := f.unit(t, "dentist").Content; got != v.Content {
			t.Errorf("content = %q, want it unchanged", got)
		}
		if c := decodeI29(t, f.rows(t, ports.ActionCorrectionApplied)[0]); len(c.Fields) != 1 {
			t.Errorf("fields = %v, want [event_at] alone", c.Fields)
		}
	})
}
