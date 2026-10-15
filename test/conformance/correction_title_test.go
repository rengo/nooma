// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
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

// TestCorrectionDateOnlyEditWithoutFreshTitleClearsIt: a moved date with no fresh
// title leaves one that names the old day ("Reunión del jueves — vie 16"), so it
// is cleared and the unit reads its content, which already follows the date.
func TestCorrectionDateOnlyEditWithoutFreshTitleClearsIt(t *testing.T) {
	u, applied := titledCorrection(t, `{"type":"correction","normalized_content":"no, es el viernes","weight":1,"decay_rate":0.05,"event_at":"2026-10-16T10:00:00Z"}`)
	fields, previous, next := appliedTitles(t, applied)
	if u.Title != "" || !hasField(fields, "title") || previous != "Reunión del jueves" || next != nil {
		t.Errorf("title %q, fields %v, previous %v, next %v; want it cleared", u.Title, fields, previous, next)
	}
	if u.Content != "Reunión con Juan el 2026-10-16 a las 10:00" {
		t.Errorf("content %q, want the body moved to the new date", u.Content)
	}
}

// TestCorrectionDateOnlyEditWithFreshTitleTakesIt: the same correction with a
// fresh title the model answered for the unit it was shown writes that one.
func TestCorrectionDateOnlyEditWithFreshTitleTakesIt(t *testing.T) {
	u, applied := titledCorrection(t, `{"type":"correction","normalized_content":"no, es el viernes","weight":1,"decay_rate":0.05,"event_at":"2026-10-16T10:00:00Z","title":"Reunión del viernes"}`)
	fields, previous, next := appliedTitles(t, applied)
	if u.Title != "Reunión del viernes" || !hasField(fields, "title") || previous != "Reunión del jueves" || next != "Reunión del viernes" {
		t.Errorf("title %q, fields %v, previous %v, next %v; want the fresh title", u.Title, fields, previous, next)
	}
}

// TestChatPathCorrectionNeverTakesTheModelsTitle: on the chat path the model was
// not shown the unit, so a title it returns names the utterance ("Cambio al
// viernes"). It is not stored; the moved date clears the stale title instead.
func TestChatPathCorrectionNeverTakesTheModelsTitle(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	original := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	const response = `{"type":"correction","normalized_content":"Dentista hoy a las 10:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T10:00:00Z","title":"Cambio al viernes","language":"es"}`
	u := unit.Unit{ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "Dentista el 2026-10-16 a las 10:00",
		Title: "Dentista del viernes", EventAt: &original, Source: "cli", CreatedAt: now, UpdatedAt: now}
	f := newI29(t, now, u, response)

	f.capture(t, "") // no referent: the chat path finds the unit by recall

	got := f.unit(t, "dentist")
	if got.Title == "Cambio al viernes" {
		t.Fatalf("the model's title for the utterance was stored: %q", got.Title)
	}
	if got.Title != "" {
		t.Errorf("title %q, want the stale one cleared by the moved date", got.Title)
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

// retextFixture is a dentist event, titled or not, with one armed reminder that
// says what the unit says, corrected by a wording change that moves no date.
func retextFixture(t *testing.T, title, response string) (i29Fixture, string) {
	t.Helper()
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	original := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	const body = "Dentista el 2026-10-16 a las 10:00"
	u := unit.Unit{ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool, Content: body, Title: title,
		EventAt: &original, Source: "cli", CreatedAt: now, UpdatedAt: now}
	f := newI29(t, now, u, response)
	said := body
	if title != "" {
		said = title
	}
	f.arm(t, "t1", "dentist", time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC), said, nil, nil)
	return f, said
}

// TestCorrectionOfTheTitleRefreshesTheFrozenTextWithoutAMovedDate: the title
// changes and no date does. The text an armed reminder falls back to when its
// unit cannot be read followed only a moved date, so it kept saying the old name.
func TestCorrectionOfTheTitleRefreshesTheFrozenTextWithoutAMovedDate(t *testing.T) {
	f, _ := retextFixture(t, "Dentista", `{"type":"correction","normalized_content":"Dentista el 2026-10-16 a las 10:00 con Ana","weight":0.6,"decay_rate":0.03,"title":"Dentista con Ana","language":"es"}`)

	f.capture(t, "dentist")

	got := f.armed(t, "dentist")
	if len(got) != 1 || got[0].Payload.ActionText != "Dentista con Ana" {
		t.Fatalf("armed = %+v, want one reminder now saying the new title", got)
	}
	if want := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC); !got[0].FireAt.Equal(want) {
		t.Errorf("the reminder now fires at %v, want %v: only its text changes", got[0].FireAt, want)
	}
	rows := f.rows(t, ports.ActionCorrectionReminderMoved)
	if len(rows) != 1 {
		t.Fatalf("%d correction.reminder.moved rows, want one recording the text change", len(rows))
	}
	var c struct {
		Fields   []string          `json:"fields"`
		Previous map[string]string `json:"previous"`
		Next     map[string]string `json:"next"`
	}
	if err := json.Unmarshal(rows[0].Context, &c); err != nil || len(c.Fields) != 1 || c.Fields[0] != "action_text" ||
		c.Previous["action_text"] != "Dentista" || c.Next["action_text"] != "Dentista con Ana" {
		t.Errorf("row %s (%v), want action_text Dentista -> Dentista con Ana", rows[0].Context, err)
	}
}

// TestCorrectionOfTheContentRefreshesTheFrozenTextOfAnUntitledUnit: with no
// title the reminder says the content, and a wording change updates it too.
func TestCorrectionOfTheContentRefreshesTheFrozenTextOfAnUntitledUnit(t *testing.T) {
	f, _ := retextFixture(t, "", `{"type":"correction","normalized_content":"Dentista el 2026-10-16 a las 10:00 con Ana","weight":0.6,"decay_rate":0.03,"language":"es"}`)

	f.capture(t, "dentist")

	if got := f.armed(t, "dentist"); len(got) != 1 || got[0].Payload.ActionText != "Dentista el 2026-10-16 a las 10:00 con Ana" {
		t.Errorf("armed = %+v, want one reminder now saying the new content", got)
	}
}

// TestRetriedCorrectionRepairsAStaleFrozenText: a first attempt wrote the unit
// and failed before the reminder step, so the unit holds the new title and the
// armed reminder still says the old one. The same correction again changes
// nothing in the unit and must still repair the reminder (doc 02 §5 step 4).
func TestRetriedCorrectionRepairsAStaleFrozenText(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	original := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	const response = `{"type":"correction","normalized_content":"Dentista el 2026-10-16 a las 10:00 con Ana","weight":0.6,"decay_rate":0.03,"title":"Dentista con Ana","language":"es"}`
	u := unit.Unit{ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "Dentista el 2026-10-16 a las 10:00 con Ana",
		Title: "Dentista con Ana", EventAt: &original, Source: "cli", CreatedAt: now, UpdatedAt: now}
	f := newI29(t, now, u, response)
	f.arm(t, "t1", "dentist", time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC), "Dentista", nil, nil)
	f.arm(t, "t2", "dentist", time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC), "Dentista con Ana", nil, nil)

	f.capture(t, "dentist")

	for _, tr := range f.armed(t, "dentist") {
		if tr.Payload.ActionText != "Dentista con Ana" {
			t.Errorf("reminder %s says %q, want the unit's title", tr.ID, tr.Payload.ActionText)
		}
	}
	if rows := f.rows(t, ports.ActionCorrectionReminderMoved); len(rows) != 1 {
		t.Errorf("%d correction.reminder.moved rows, want one: only the stale row is rewritten", len(rows))
	}
	if rows := f.rows(t, ports.ActionCorrectionApplied); len(rows) != 0 {
		t.Errorf("%d correction.applied rows for a correction the unit already held, want none", len(rows))
	}
}

// TestCorrectionRefreshesTheFrozenTextOfNonEventUnits: the rewrite does not depend
// on the unit being an event. A task's reminder (the asked one, in link 2) and a
// recurring reminder (an event with a rule and an anchor) both say what their
// unit says, and neither changes when it fires.
func TestCorrectionRefreshesTheFrozenTextOfNonEventUnits(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	due := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	yearly := prospection.RuleYearly
	anchor := prospection.Anchor{Month: time.October, Day: 16}
	fire := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	const response = `{"type":"correction","normalized_content":"Pagar la luz el 2026-10-16 a las 10:00 y el gas","weight":0.6,"decay_rate":0.03,"language":"es"}`

	for name, tc := range map[string]struct {
		u      unit.Unit
		rule   *prospection.Rule
		anchor *prospection.Anchor
	}{
		"a task": {unit.Unit{ID: "bill", Type: unit.TypeTask, Status: unit.StatusPool, Content: "Pagar la luz el 2026-10-16 a las 10:00",
			DueAt: &due, Source: "cli", CreatedAt: now, UpdatedAt: now}, nil, nil},
		"a recurring reminder": {unit.Unit{ID: "bill", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "Pagar la luz el 2026-10-16 a las 10:00",
			EventAt: &due, Source: "cli", CreatedAt: now, UpdatedAt: now}, &yearly, &anchor},
	} {
		t.Run(name, func(t *testing.T) {
			f := newI29(t, now, tc.u, response)
			f.arm(t, "t1", "bill", fire, "Pagar la luz el 2026-10-16 a las 10:00", tc.rule, tc.anchor)

			f.capture(t, "bill")

			got := f.armed(t, "bill")
			if len(got) != 1 || got[0].Payload.ActionText != "Pagar la luz el 2026-10-16 a las 10:00 y el gas" {
				t.Fatalf("armed = %+v, want the reminder now saying the new text", got)
			}
			if !got[0].FireAt.Equal(fire) || (tc.rule != nil && (got[0].RecurrenceRule == nil || *got[0].RecurrenceRule != *tc.rule ||
				got[0].RecurrenceAnchor == nil || *got[0].RecurrenceAnchor != *tc.anchor)) {
				t.Errorf("reminder %+v: it must keep firing at %v with its rule and anchor", got[0], fire)
			}
		})
	}
}

// TestRetriedDateMoveRenamesTheStaleRow: a first attempt moved the date and
// renamed the unit, then failed before the reminder step. The unit holds both, the
// armed row still says "Dentista del viernes", and the same correction again moves
// the row and must also give it the unit's current name, not leave the old one.
func TestRetriedDateMoveRenamesTheStaleRow(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	today := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	const response = `{"type":"event","normalized_content":"Dentista el 2026-10-09 a las 10:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T10:00:00Z","title":"Dentista de hoy","language":"es"}`
	u := unit.Unit{ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "Dentista el 2026-10-09 a las 10:00",
		Title: "Dentista de hoy", EventAt: &today, Source: "cli", CreatedAt: now, UpdatedAt: now}
	f := newI29(t, now, u, response)
	f.arm(t, "t1", "dentist", today, "Dentista del viernes", nil, nil)

	f.capture(t, "dentist")

	got := f.armed(t, "dentist")
	if len(got) == 0 {
		t.Fatal("the retry left no armed reminder")
	}
	for _, tr := range got {
		if tr.Payload.ActionText != "Dentista de hoy" {
			t.Errorf("reminder %s says %q after the retry, want the unit's current title", tr.ID, tr.Payload.ActionText)
		}
	}
}

// TestRetextKeepsTheRestOfThePayload: rewriting what a reminder says must not
// change its lead, the event it is about, the leads it serves or the catch-up flag:
// the due scan and the re-plan read them back.
func TestRetextKeepsTheRestOfThePayload(t *testing.T) {
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	original := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	const response = `{"type":"correction","normalized_content":"Dentista el 2026-10-16 a las 10:00 con Ana","weight":0.6,"decay_rate":0.03,"title":"Dentista con Ana","language":"es"}`
	u := unit.Unit{ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool, Content: "Dentista el 2026-10-16 a las 10:00",
		Title: "Dentista", EventAt: &original, Source: "cli", CreatedAt: now, UpdatedAt: now}
	f := newI29(t, now, u, response)
	fire := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	level := 0.9
	unitID := "dentist"
	payload := ports.TriggerPayload{ActionText: "Dentista", Rationale: "why", LeadDays: 2, LeadMinutes: 120, About: original,
		UserLeadsMinutes: []int{120, 30}, CatchUp: true}
	if err := f.triggers.Create(context.Background(), ports.Trigger{ID: "t1", UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		Payload: payload, FireAt: &fire, InterruptLevel: &level}); err != nil {
		t.Fatal(err)
	}

	f.capture(t, "dentist")

	got := f.armed(t, "dentist")
	if len(got) != 1 {
		t.Fatalf("armed = %+v, want the one reminder", got)
	}
	want := payload
	want.ActionText = "Dentista con Ana"
	if !reflect.DeepEqual(got[0].Payload, want) || !got[0].FireAt.Equal(fire) || got[0].InterruptLevel == nil || *got[0].InterruptLevel != level {
		t.Errorf("payload %+v at %v, level %v; want only the text changed: %+v", got[0].Payload, got[0].FireAt, got[0].InterruptLevel, want)
	}
}
