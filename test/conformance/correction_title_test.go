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

// titledCorrection corrects a titled meeting through an explicit referent with
// the synthetic response given, and returns the unit afterwards and the
// correction.applied row's context.
func titledCorrection(t *testing.T, response string) (unit.Unit, map[string]json.RawMessage) {
	t.Helper()
	const message = "no, es el viernes"
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	original := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	units, decisions := memrepo.NewUnits(), memrepo.NewDecisionLog()
	if err := units.Create(ctx, unit.Unit{
		ID: "meeting", Type: unit.TypeEvent, Status: unit.StatusPool, Language: "es",
		Content: "Reunión con Juan el 2026-10-15 a las 10:00", Title: "Reunión del jueves", EventAt: &original,
		Source: "cli", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	llm := fakeprovider.New(t, i28Case(t, "titled-correction", message, response), "titled-correction")
	svc := brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, units, embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), decisions, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel),
		brain.NewIndex(idx), memrepo.NewSignals(), memrepo.NewTriggers(), memrepo.NewTimers(), 0.5, nil)
	if _, err := svc.Capture(ctx, brain.CaptureInput{Text: message, Channel: "ui", ReferentID: "meeting"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	u, err := units.ByID(ctx, "meeting")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := decisions.Since(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Action == ports.ActionCorrectionApplied {
			var c map[string]json.RawMessage
			if err := json.Unmarshal(r.Context, &c); err != nil {
				t.Fatal(err)
			}
			return u, c
		}
	}
	t.Fatalf("no correction.applied row among %d", len(rows))
	return u, nil
}

func appliedTitles(t *testing.T, c map[string]json.RawMessage) (fields []string, previous, next any) {
	t.Helper()
	var prev, nxt map[string]any
	if err := json.Unmarshal(c["fields"], &fields); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(c["previous"], &prev)
	_ = json.Unmarshal(c["next"], &nxt)
	return fields, prev["title"], nxt["title"]
}

func hasField(fields []string, want string) bool {
	for _, f := range fields {
		if f == want {
			return true
		}
	}
	return false
}

// TestCorrectionRewritesTheTitle: a correction the model re-classified, with a
// fresh title, writes it, and the pre-image keeps the old one.
func TestCorrectionRewritesTheTitle(t *testing.T) {
	u, applied := titledCorrection(t, `{"type":"correction","normalized_content":"Reunión con Juan el 2026-10-16 a las 10:00","weight":1,"decay_rate":0.05,"event_at":"2026-10-16T10:00:00Z","title":"Reunión del viernes"}`)
	fields, previous, next := appliedTitles(t, applied)
	if u.Title != "Reunión del viernes" || !hasField(fields, "title") || previous != "Reunión del jueves" || next != "Reunión del viernes" {
		t.Errorf("title %q, fields %v, previous %v, next %v; want the new title with the old as pre-image", u.Title, fields, previous, next)
	}
}

// TestCorrectionContentEditWithoutTitleClearsIt: new wording with no valid title
// leaves the old title describing a body that is gone, so it is cleared.
func TestCorrectionContentEditWithoutTitleClearsIt(t *testing.T) {
	u, applied := titledCorrection(t, `{"type":"correction","normalized_content":"Reunión con Juan y Ana el 2026-10-15 a las 10:00","weight":1,"decay_rate":0.05}`)
	fields, previous, next := appliedTitles(t, applied)
	if u.Title != "" || !hasField(fields, "title") || previous != "Reunión del jueves" || next != nil {
		t.Errorf("title %q, fields %v, previous %v, next %v; want it cleared (next null)", u.Title, fields, previous, next)
	}
}

// TestCorrectionDateOnlyEditKeepsTitle: a date moved with no new title touches
// the title not at all.
func TestCorrectionDateOnlyEditKeepsTitle(t *testing.T) {
	u, applied := titledCorrection(t, `{"type":"correction","normalized_content":"no, es el viernes","weight":1,"decay_rate":0.05,"event_at":"2026-10-16T10:00:00Z"}`)
	fields, _, _ := appliedTitles(t, applied)
	if u.Title != "Reunión del jueves" || hasField(fields, "title") {
		t.Errorf("title %q, fields %v; want the title kept and not listed", u.Title, fields)
	}
}

// TestCorrectionTitleMovesTheFrozenText: the text an armed reminder falls back to
// when its unit cannot be read follows the title, as it followed the body.
func TestCorrectionTitleMovesTheFrozenText(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	original := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	const response = `{"type":"event","normalized_content":"Dentista hoy a las 10:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T10:00:00Z","title":"Dentista de hoy","language":"es"}`
	u := unit.Unit{ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "Dentista el 2026-10-16 a las 10:00",
		Title: "Dentista del viernes", EventAt: &original, Source: "cli", CreatedAt: now, UpdatedAt: now}
	f := newI29(t, now, u, response)
	f.arm(t, "t1", "dentist", time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC), "Dentista del viernes", nil, nil)

	f.capture(t, "dentist")

	if got := f.armed(t, "dentist"); len(got) != 1 || got[0].Payload.ActionText != "Dentista de hoy" {
		t.Errorf("armed = %+v, want one reminder now saying the new title", got)
	}
}
