// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/fakeprovider"
	"github.com/rengo/nooma/test/support/memrepo"
)

// i60LLM is a scripted provider that answers by the request's task, never by
// its prompt text: one canned classification for capture_processing and one
// canned answer for unit_title (ADR-0065). A test of the naming step sets what
// each answers; nothing else may be asked.
type i60LLM struct {
	t        *testing.T
	classify string
	title    string
	titleErr error
	// during runs inside the unit_title call, standing for whatever else writes
	// the vault while the model is thinking.
	during func()
	calls  []string
	// titlePrompts are the prompts the unit_title task received.
	titlePrompts []string
}

func (l *i60LLM) Complete(_ context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	l.calls = append(l.calls, req.Task)
	switch req.Task {
	case "capture_processing":
		return ports.LLMResponse{Text: l.classify}, nil
	case "unit_title":
		l.titlePrompts = append(l.titlePrompts, req.Prompt)
		if l.during != nil {
			l.during()
		}
		return ports.LLMResponse{Text: l.title}, l.titleErr
	}
	l.t.Fatalf("i60: unscripted %q call", req.Task)
	return ports.LLMResponse{}, nil
}

func (l *i60LLM) titleCalls() int {
	n := 0
	for _, c := range l.calls {
		if c == "unit_title" {
			n++
		}
	}
	return n
}

type i60Vault struct {
	svc       *brain.CaptureService
	units     *memrepo.Units
	triggers  *memrepo.Triggers
	decisions *memrepo.DecisionLog
	llm       *i60LLM
}

var i60Now = time.Date(2026, 11, 4, 10, 0, 0, 0, time.FixedZone("ART", -3*60*60))

func newI60(t *testing.T, llm *i60LLM) i60Vault {
	t.Helper()
	llm.t = t
	v := i60Vault{units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(), decisions: memrepo.NewDecisionLog(), llm: llm}
	embeddings := memrepo.NewEmbeddings()
	idx, err := embeddings.LoadIndex(context.Background(), embedFakeModel)
	if err != nil {
		t.Fatal(err)
	}
	v.svc = brain.NewCaptureService(fixedClock{now: i60Now}, &counterIDs{}, v.units, embeddings, memrepo.NewLexical(),
		memrepo.NewRelations(), v.decisions, llm, llm, llm, fakeprovider.NewEmbeddingFake(embedFakeModel),
		brain.NewIndex(idx), memrepo.NewSignals(), v.triggers, memrepo.NewTimers(), 0.5, nil)
	return v
}

func (v i60Vault) rows(t *testing.T, action ports.DecisionAction) []ports.Decision {
	t.Helper()
	all, err := v.decisions.Since(context.Background(), time.Time{}, 500)
	if err != nil {
		t.Fatal(err)
	}
	var out []ports.Decision
	for _, r := range all {
		if r.Action == action {
			out = append(out, r)
		}
	}
	return out
}

// titleRow reads a naming row's previous and next title.
func titleRow(t *testing.T, d ports.Decision) (unitID string, previous, next any) {
	t.Helper()
	var c struct {
		UnitID   string         `json:"unit_id"`
		Fields   []string       `json:"fields"`
		Previous map[string]any `json:"previous"`
		Next     map[string]any `json:"next"`
	}
	if err := json.Unmarshal(d.Context, &c); err != nil || len(c.Fields) != 1 || c.Fields[0] != "title" {
		t.Fatalf("row %s (%v), want one change of the title field", d.Context, err)
	}
	prev, ok := c.Previous["title"]
	if !ok || prev != nil {
		t.Fatalf("row %s: previous title %v (present %v), want an explicit null", d.Context, prev, ok)
	}
	return c.UnitID, prev, c.Next["title"]
}

// A dated event whose title the capture check drops (an HH:MM in it).
const (
	i60DroppedTitle = `{"type":"event","normalized_content":"Tengo reunión con Juan el 2026-11-04 a las 22:30","weight":0.8,"decay_rate":0.05,` +
		`"event_at":"2026-11-04T22:30:00-03:00","title":"Reunión 22:30","language":"es"}`
	i60GoodTitle = `{"type":"event","normalized_content":"Tengo reunión con Juan el 2026-11-04 a las 22:30","weight":0.8,"decay_rate":0.05,` +
		`"event_at":"2026-11-04T22:30:00-03:00","title":"Reunión con Juan","language":"es"}`
	i60UndatedDropped = `{"type":"task","normalized_content":"Llamar a Ana","weight":0.5,"decay_rate":0.03,"title":"Llamar 22:30","language":"es","remind_request":true}`
)

// TestI60_CaptureNamesADroppedTitleBeforeArming: a dated capture that came back
// with no usable title is asked for one, once, before its reminder is armed, so
// the reminder's text is the title from the start (ADR-0065 point 1).
func TestI60_CaptureNamesADroppedTitleBeforeArming(t *testing.T) {
	llm := &i60LLM{classify: i60DroppedTitle, title: `{"title":"Reunión con Juan"}`}
	v := newI60(t, llm)

	res, err := v.svc.Capture(context.Background(), brain.CaptureInput{Text: "Tengo reunión con Juan hoy a las 22:30", Channel: "cli"})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	u, err := v.units.ByID(context.Background(), res.UnitID)
	if err != nil || u.Title != "Reunión con Juan" {
		t.Fatalf("unit title = %q, %v; want the model's naming", u.Title, err)
	}
	if llm.titleCalls() != 1 {
		t.Errorf("%d unit_title calls (%v), want exactly one", llm.titleCalls(), llm.calls)
	}
	if len(llm.titlePrompts) == 1 && !strings.Contains(llm.titlePrompts[0], "Tengo reunión con Juan el 2026-11-04 a las 22:30") {
		t.Errorf("the naming prompt does not carry the unit's content:\n%s", llm.titlePrompts[0])
	}
	armed := v.triggers.All()
	if len(armed) == 0 {
		t.Fatalf("nothing was armed: %+v", res)
	}
	for _, tr := range armed {
		if tr.Payload.ActionText != "Reunión con Juan" {
			t.Errorf("trigger %s says %q, want the title it was named with before arming", tr.ID, tr.Payload.ActionText)
		}
	}
	rows := v.rows(t, ports.ActionCaptureTitleNamed)
	if len(rows) != 1 {
		t.Fatalf("%d capture.title.named rows, want one", len(rows))
	}
	if id, _, next := titleRow(t, rows[0]); id != res.UnitID || next != "Reunión con Juan" {
		t.Errorf("row names unit %q and title %v; want %q and the new title", id, next, res.UnitID)
	}
}

// TestI60_CaptureAsksOnlyWhereATitleIsMissing: a usable title costs no call, and
// a unit with no instant of its own is not named at capture.
func TestI60_CaptureAsksOnlyWhereATitleIsMissing(t *testing.T) {
	for name, classify := range map[string]string{"a usable title": i60GoodTitle, "an undated reminder": i60UndatedDropped} {
		t.Run(name, func(t *testing.T) {
			llm := &i60LLM{classify: classify, title: `{"title":"No debe usarse"}`}
			v := newI60(t, llm)

			if _, err := v.svc.Capture(context.Background(), brain.CaptureInput{Text: "x", Channel: "cli"}); err != nil {
				t.Fatalf("Capture: %v", err)
			}

			if llm.titleCalls() != 0 {
				t.Errorf("%d unit_title calls, want none", llm.titleCalls())
			}
			if rows := v.rows(t, ports.ActionCaptureTitleNamed); len(rows) != 0 {
				t.Errorf("%d capture.title.named rows, want none", len(rows))
			}
		})
	}
}

// TestI60_ANamingThatFailsStillCaptures: a provider error, or an answer
// ResolveTitle drops, leaves the title NULL; the unit is stored and its
// reminder armed with the words it had (the night retries, ADR-0065 point 3).
func TestI60_ANamingThatFailsStillCaptures(t *testing.T) {
	for name, llm := range map[string]*i60LLM{
		"a provider error":     {classify: i60DroppedTitle, titleErr: errors.New("boom")},
		"an unusable answer":   {classify: i60DroppedTitle, title: `{"title":"Reunión 2026-11-04"}`},
		"an answer of nothing": {classify: i60DroppedTitle, title: `{"title":null}`},
	} {
		t.Run(name, func(t *testing.T) {
			v := newI60(t, llm)

			res, err := v.svc.Capture(context.Background(), brain.CaptureInput{Text: "Tengo reunión con Juan hoy a las 22:30", Channel: "cli"})
			if err != nil {
				t.Fatalf("Capture: %v", err)
			}

			u, err := v.units.ByID(context.Background(), res.UnitID)
			if err != nil || u.Title != "" {
				t.Errorf("unit title = %q, %v; want it left NULL", u.Title, err)
			}
			if len(v.triggers.All()) == 0 {
				t.Error("the reminder was not armed")
			}
			if rows := v.rows(t, ports.ActionCaptureTitleNamed); len(rows) != 0 {
				t.Errorf("%d capture.title.named rows, want none", len(rows))
			}
		})
	}
}

// TestI60_ATitleWrittenMeanwhileSurvives: a title that lands while the model is
// thinking is never overwritten, and nothing is logged for the lost naming.
func TestI60_ATitleWrittenMeanwhileSurvives(t *testing.T) {
	llm := &i60LLM{classify: i60DroppedTitle, title: `{"title":"Reunión con Juan"}`}
	v := newI60(t, llm)
	llm.during = func() {
		if err := v.units.UpdateTitle(context.Background(), "id-1", "Cita con Juan", i60Now); err != nil {
			t.Errorf("the concurrent write: %v", err)
		}
	}

	res, err := v.svc.Capture(context.Background(), brain.CaptureInput{Text: "Tengo reunión con Juan hoy a las 22:30", Channel: "cli"})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if u, err := v.units.ByID(context.Background(), res.UnitID); err != nil || u.Title != "Cita con Juan" {
		t.Errorf("unit title = %q, %v; want the concurrent title kept", u.Title, err)
	}
	if rows := v.rows(t, ports.ActionCaptureTitleNamed); len(rows) != 0 {
		t.Errorf("%d capture.title.named rows for a lost naming, want none", len(rows))
	}
}

// i60Meeting is a dated, titled meeting a correction moves to Friday.
func i60Meeting() (unit.Unit, time.Time) {
	original := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	return unit.Unit{
		ID: "meeting", Type: unit.TypeEvent, Status: unit.StatusPool, Language: "es",
		Content: "Reunión con Juan el 2026-11-05 a las 10:00", Title: "Reunión del jueves", EventAt: &original,
		Source: "cli", CreatedAt: i60Now, UpdatedAt: i60Now,
	}, original
}

const i60DateOnlyCorrection = `{"type":"correction","normalized_content":"no, es el viernes","weight":1,"decay_rate":0.05,"event_at":"2026-11-06T10:00:00Z"}`

// TestI60_ADateOnlyCorrectionEndsTitled: a correction that moves the date with
// no fresh title clears the stale one (doc 02 §5), and the unit is then named
// against its new body; the reminder it carries says the new name.
func TestI60_ADateOnlyCorrectionEndsTitled(t *testing.T) {
	llm := &i60LLM{classify: i60DateOnlyCorrection, title: `{"title":"Reunión con Juan"}`}
	v := newI60(t, llm)
	u, original := i60Meeting()
	if err := v.units.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	unitID := u.ID
	if err := v.triggers.Create(context.Background(), ports.Trigger{ID: "t1", UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: "Reunión del jueves", About: original}, FireAt: &original}); err != nil {
		t.Fatal(err)
	}

	if _, err := v.svc.Capture(context.Background(), brain.CaptureInput{Text: "no, es el viernes", Channel: "ui", ReferentID: "meeting"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	got, err := v.units.ByID(context.Background(), "meeting")
	if err != nil || got.Title != "Reunión con Juan" || got.Content != "Reunión con Juan el 2026-11-06 a las 10:00" {
		t.Fatalf("unit = %q / %q, %v; want the new name over the moved body", got.Title, got.Content, err)
	}
	if llm.titleCalls() != 1 || !strings.Contains(llm.titlePrompts[0], "2026-11-06") {
		t.Errorf("unit_title calls %d, prompts %v; want one, shown the moved body", llm.titleCalls(), llm.titlePrompts)
	}
	rows := v.rows(t, ports.ActionCorrectionTitleNamed)
	if len(rows) != 1 {
		t.Fatalf("%d correction.title.named rows, want one", len(rows))
	}
	if id, _, next := titleRow(t, rows[0]); id != "meeting" || next != "Reunión con Juan" {
		t.Errorf("row names unit %q and title %v", id, next)
	}
	armed, err := v.triggers.ArmedForUnit(context.Background(), "meeting")
	if err != nil || len(armed) == 0 {
		t.Fatalf("armed = %+v, %v; want the unit's reminder", armed, err)
	}
	for _, tr := range armed {
		if tr.Payload.ActionText != "Reunión con Juan" {
			t.Errorf("reminder %s says %q after the unit was named", tr.ID, tr.Payload.ActionText)
		}
	}
}

// TestI60_ACorrectionThatKeepsItsTitleAsksNothing: the control. A fresh title
// the model gave is written as before and costs no naming call.
func TestI60_ACorrectionThatKeepsItsTitleAsksNothing(t *testing.T) {
	llm := &i60LLM{classify: `{"type":"correction","normalized_content":"no, es el viernes","weight":1,"decay_rate":0.05,"event_at":"2026-11-06T10:00:00Z","title":"Reunión del viernes"}`}
	v := newI60(t, llm)
	u, _ := i60Meeting()
	if err := v.units.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}

	if _, err := v.svc.Capture(context.Background(), brain.CaptureInput{Text: "no, es el viernes", Channel: "ui", ReferentID: "meeting"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if llm.titleCalls() != 0 {
		t.Errorf("%d unit_title calls, want none", llm.titleCalls())
	}
	if rows := v.rows(t, ports.ActionCorrectionTitleNamed); len(rows) != 0 {
		t.Errorf("%d correction.title.named rows, want none", len(rows))
	}
}

// TestI60_ACorrectionThatFailsToNameStillCorrects: the title stays cleared (the
// stale one is gone) and the correction is applied.
func TestI60_ACorrectionThatFailsToNameStillCorrects(t *testing.T) {
	llm := &i60LLM{classify: i60DateOnlyCorrection, titleErr: errors.New("boom")}
	v := newI60(t, llm)
	u, _ := i60Meeting()
	if err := v.units.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}

	if _, err := v.svc.Capture(context.Background(), brain.CaptureInput{Text: "no, es el viernes", Channel: "ui", ReferentID: "meeting"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	got, err := v.units.ByID(context.Background(), "meeting")
	if err != nil || got.Title != "" || got.EventAt == nil || got.EventAt.Day() != 6 {
		t.Errorf("unit title %q, event_at %v, %v; want it moved with the title still cleared", got.Title, got.EventAt, err)
	}
}
