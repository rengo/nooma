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
	"github.com/rengo/nooma/internal/core/prospection"
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
	decisions ports.DecisionLog
	signals   *memrepo.Signals
	llm       *fakeprovider.Fake
	message   string
	// hook runs its during func inside the classify call, standing for
	// whatever else writes the vault while the model is thinking.
	hook *llmHook
}

// llmHook wraps the scripted provider so a test can act mid-call.
type llmHook struct {
	inner  ports.LLMProvider
	during func()
}

func (h *llmHook) Complete(ctx context.Context, req ports.LLMRequest) (ports.LLMResponse, error) {
	if h.during != nil {
		h.during()
	}
	return h.inner.Complete(ctx, req)
}

const i29Message = "en realidad es hoy viernes 9 a las 11"

// newI29 seeds u and, for the chat path, makes it the one strong recall
// match for i29Message.
func newI29(t *testing.T, now time.Time, u unit.Unit, response string) i29Fixture {
	t.Helper()
	return newI29Logging(t, now, u, response, memrepo.NewDecisionLog())
}

func newI29Logging(t *testing.T, now time.Time, u unit.Unit, response string, decisions ports.DecisionLog) i29Fixture {
	t.Helper()
	return newCorrectionFixture(t, now, u, i29Message, response, decisions)
}

// newCorrectionFixture seeds u, makes it the one strong recall match for
// message, and replays response for the one classify call.
func newCorrectionFixture(t *testing.T, now time.Time, u unit.Unit, message, response string, decisions ports.DecisionLog) i29Fixture {
	t.Helper()
	ctx := context.Background()
	f := i29Fixture{units: memrepo.NewUnits(), triggers: memrepo.NewTriggers(), decisions: decisions, signals: memrepo.NewSignals(), message: message}
	if err := f.units.Create(ctx, u); err != nil {
		t.Fatalf("seeding the referent: %v", err)
	}
	embeddings := memrepo.NewEmbeddings()
	lexical := memrepo.NewLexical()
	embed := fakeprovider.NewEmbeddingFake(embedFakeModel)
	match, err := embed.Embed(ctx, ports.EmbedRequest{Text: message})
	if err != nil {
		t.Fatalf("deriving the match vector: %v", err)
	}
	if err := embeddings.Put(ctx, ports.Embedding{UnitID: u.ID, Model: embedFakeModel, Vector: match.Vector, At: now}); err != nil {
		t.Fatalf("seeding the embedding: %v", err)
	}
	lexical.SeedLexical(t, u.ID, message)
	idx, err := embeddings.LoadIndex(ctx, embedFakeModel)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	f.llm = fakeprovider.New(t, i28Case(t, "i29", message, response), "i29")
	f.hook = &llmHook{inner: f.llm}
	f.svc = brain.NewCaptureService(fixedClock{now: now}, &counterIDs{}, f.units, embeddings, lexical, memrepo.NewRelations(), f.decisions, f.hook, f.hook, f.hook, fakeprovider.NewEmbeddingFake(embedFakeModel), brain.NewIndex(idx), f.signals, f.triggers, memrepo.NewTimers(), 0.5, nil)
	return f
}

func (f i29Fixture) capture(t *testing.T, referent string) {
	t.Helper()
	result, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: f.message, Channel: "ui", ReferentID: referent})
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

	t.Run("the body is read in the clock's own zone first", func(t *testing.T) {
		// A clock that is not UTC: the body says 07:00 because the user is
		// at UTC-3, and only reading it in that zone finds the time to move.
		art := time.FixedZone("ART", -3*60*60)
		local := now.In(art)
		v := event
		v.Content = "Dentista el 2026-10-16 a las 07:00"
		f := newI29(t, local, v, `{"type":"event","normalized_content":"Dentista hoy a las 11:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T11:00:00-03:00","language":"es"}`)
		f.capture(t, "dentist")
		if got := f.unit(t, "dentist").Content; got != "Dentista el 2026-10-09 a las 11:00" {
			t.Errorf("content = %q, want the date and the local time moved", got)
		}
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

// arm seeds an armed trigger on unitID, saying text.
func (f i29Fixture) arm(t *testing.T, id, unitID string, fireAt time.Time, text string, rule *prospection.Rule, anchor *prospection.Anchor) {
	t.Helper()
	if err := f.triggers.Create(context.Background(), ports.Trigger{ID: id, UnitID: &unitID, Kind: ports.TriggerKindTimeBased,
		Payload: ports.TriggerPayload{ActionText: text, LeadDays: prospection.EventLeadDays}, FireAt: &fireAt,
		RecurrenceRule: rule, RecurrenceAnchor: anchor}); err != nil {
		t.Fatalf("seeding trigger %s: %v", id, err)
	}
}

func (f i29Fixture) armed(t *testing.T, unitID string) []ports.DueTrigger {
	t.Helper()
	got, err := f.triggers.ArmedForUnit(context.Background(), unitID)
	if err != nil {
		t.Fatalf("ArmedForUnit: %v", err)
	}
	return got
}

// TestI29_CorrectedDateMovesTheReminder is doc 02 §5 step 4's reminder half
// of I29: after a correction moves an event unit's date, the unit holds
// exactly one armed trigger at what a fresh capture of that date arms, or
// none when the date is past, and each move, creation or expiry wrote its
// change-shaped row first.
func TestI29_CorrectedDateMovesTheReminder(t *testing.T) {
	original := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	oldFire := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	const before = "Dentista el 2026-10-16 a las 10:00"
	const after = "Dentista el 2026-10-09 a las 10:00"
	const toToday = `{"type":"event","normalized_content":"Dentista hoy a las 10:00","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-09T10:00:00Z","language":"es"}`
	const toLater = `{"type":"event","normalized_content":"Dentista el 2026-10-30","weight":0.6,"decay_rate":0.03,"event_at":"2026-10-30T10:00:00Z","interrupt_level":0.9,"language":"es"}`
	dentist := func(now time.Time) unit.Unit {
		return unit.Unit{ID: "dentist", Type: unit.TypeEvent, Status: unit.StatusPool, Content: before,
			EventAt: &original, Source: "cli", CreatedAt: now, UpdatedAt: now}
	}
	reminderRows := func(t *testing.T, f i29Fixture) []ports.Decision {
		return f.rows(t, ports.ActionCorrectionReminderMoved, ports.ActionCorrectionReminderArmed, ports.ActionCorrectionReminderCancelled)
	}

	t.Run("the client's case: the armed reminder moves, firing at once", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		f := newI29(t, now, dentist(now), toToday)
		f.arm(t, "t1", "dentist", oldFire, before, nil, nil)
		f.capture(t, "dentist")

		got := f.armed(t, "dentist")
		if len(got) != 1 || got[0].ID != "t1" || !got[0].FireAt.Equal(now) || got[0].Payload.ActionText != after {
			t.Fatalf("armed = %+v, want t1 firing at %s saying %q", got, now, after)
		}
		if n := f.triggers.Count(); n != 1 {
			t.Errorf("triggers = %d, want 1 — a moved reminder is the same row", n)
		}
		rows := reminderRows(t, f)
		if len(rows) != 1 || rows[0].Action != ports.ActionCorrectionReminderMoved {
			t.Fatalf("reminder rows = %v, want one %s", rows, ports.ActionCorrectionReminderMoved)
		}
		c := decodeI29(t, rows[0])
		if c.UnitID != "dentist" || c.TriggerID != "t1" || c.Previous["fire_at"] != "2026-10-09T10:00:00Z" || c.Next["fire_at"] != "2026-10-09T08:00:00Z" ||
			c.Previous["action_text"] != before || c.Next["action_text"] != after {
			t.Errorf("moved row = %s", rows[0].Context)
		}
	})

	t.Run("a date already past cancels the reminder and arms none", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC)
		f := newI29(t, now, dentist(now), toToday)
		f.arm(t, "t1", "dentist", oldFire, before, nil, nil)
		f.capture(t, "dentist")

		if got := f.armed(t, "dentist"); len(got) != 0 {
			t.Fatalf("armed = %+v, want none", got)
		}
		if n := f.triggers.Count(); n != 1 {
			t.Errorf("triggers = %d, want 1 — cancelling is a transition, nothing is created or deleted", n)
		}
		rows := reminderRows(t, f)
		if len(rows) != 1 || rows[0].Action != ports.ActionCorrectionReminderCancelled {
			t.Fatalf("reminder rows = %v, want one %s", rows, ports.ActionCorrectionReminderCancelled)
		}
		if c := decodeI29(t, rows[0]); c.TriggerID != "t1" || c.Previous["status"] != "armed" || c.Next["status"] != "expired" {
			t.Errorf("cancelled row = %s", rows[0].Context)
		}
	})

	t.Run("with no reminder armed, one is armed as a fresh capture would", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		f := newI29(t, now, dentist(now), toLater)
		f.capture(t, "dentist")

		got := f.armed(t, "dentist")
		wantFire := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
		if len(got) != 1 || !got[0].FireAt.Equal(wantFire) || got[0].InterruptLevel == nil || *got[0].InterruptLevel != 0.9 {
			t.Fatalf("armed = %+v, want one firing at %s with level 0.9", got, wantFire)
		}
		rows := reminderRows(t, f)
		if len(rows) != 1 || rows[0].Action != ports.ActionCorrectionReminderArmed {
			t.Fatalf("reminder rows = %v, want one %s", rows, ports.ActionCorrectionReminderArmed)
		}
		if c := decodeI29(t, rows[0]); c.TriggerID != got[0].ID || c.Previous["fire_at"] != nil || c.Next["fire_at"] != "2026-10-23T10:00:00Z" {
			t.Errorf("armed row = %s", rows[0].Context)
		}
	})

	t.Run("a recurring reminder stays recurring, re-anchored", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		yearly := prospection.RuleYearly
		f := newI29(t, now, dentist(now), toLater)
		f.arm(t, "r1", "dentist", oldFire, before, &yearly, &prospection.Anchor{Month: time.October, Day: 16})
		f.capture(t, "dentist")

		got := f.armed(t, "dentist")
		if len(got) != 1 || got[0].ID != "r1" || got[0].RecurrenceRule == nil || got[0].RecurrenceAnchor == nil ||
			got[0].RecurrenceAnchor.Day != 30 || got[0].FireAt.Month() != time.October || got[0].FireAt.Day() != 23 {
			t.Fatalf("armed = %+v, want r1 still yearly, anchored on Oct 30", got)
		}
	})

	t.Run("two armed reminders end as one", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		f := newI29(t, now, dentist(now), toLater)
		f.arm(t, "t1", "dentist", oldFire, before, nil, nil)
		f.arm(t, "t2", "dentist", oldFire.Add(time.Hour), before, nil, nil)
		f.capture(t, "dentist")

		if got := f.armed(t, "dentist"); len(got) != 1 || got[0].ID != "t1" {
			t.Fatalf("armed = %+v, want t1 alone", got)
		}
		rows := reminderRows(t, f)
		if len(rows) != 2 || rows[0].Action != ports.ActionCorrectionReminderMoved || rows[1].Action != ports.ActionCorrectionReminderCancelled {
			t.Errorf("reminder rows = %v, want moved then cancelled", rows)
		}
	})

	t.Run("a due_at correction leaves the reminder alone", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		f := newI29(t, now, dentist(now), `{"type":"correction","normalized_content":"x","weight":0.6,"decay_rate":0.03,"due_at":"2026-10-30T10:00:00Z"}`)
		f.arm(t, "t1", "dentist", oldFire, before, nil, nil)
		f.capture(t, "dentist")

		if got := f.armed(t, "dentist"); len(got) != 1 || !got[0].FireAt.Equal(oldFire) {
			t.Errorf("armed = %+v, want t1 untouched", got)
		}
		if rows := reminderRows(t, f); len(rows) != 0 {
			t.Errorf("reminder rows = %v, want none", rows)
		}
	})

	t.Run("a unit that is not an event gains no reminder", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		task := dentist(now)
		task.Type = unit.TypeTask
		f := newI29(t, now, task, toLater)
		f.capture(t, "dentist")
		if n := f.triggers.Count(); n != 0 {
			t.Errorf("triggers = %d, want 0 — a fresh capture of a task arms nothing", n)
		}
	})

	t.Run("a failed pre-image write leaves the reminder untouched", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		f := newI29Logging(t, now, dentist(now), toLater, memrepo.NewFailingDecisionLog(errors.New("disk full")))
		f.arm(t, "t1", "dentist", oldFire, before, nil, nil)
		f.captureFails(t)
		if got := f.armed(t, "dentist"); len(got) != 1 || !got[0].FireAt.Equal(oldFire) || got[0].Payload.ActionText != before {
			t.Errorf("armed = %+v, want t1 untouched", got)
		}
	})

	// The reminder's own rows come before their writes: a log that refuses
	// only correction.reminder.* lets the unit edit land and must stop each
	// trigger write.
	t.Run("a failed reminder row leaves a moved reminder unmoved", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		f := newI29Logging(t, now, dentist(now), toToday, reminderRowsFail{memrepo.NewDecisionLog()})
		f.arm(t, "t1", "dentist", oldFire, before, nil, nil)
		f.captureFails(t)
		f.assertEditLandedUnsignalled(t, time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC))
		if got := f.armed(t, "dentist"); len(got) != 1 || !got[0].FireAt.Equal(oldFire) || got[0].Payload.ActionText != before {
			t.Errorf("armed = %+v, want t1 unmoved", got)
		}
	})
	t.Run("a failed reminder row arms nothing", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		f := newI29Logging(t, now, dentist(now), toLater, reminderRowsFail{memrepo.NewDecisionLog()})
		f.captureFails(t)
		f.assertEditLandedUnsignalled(t, time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC))
		if n := f.triggers.Count(); n != 0 {
			t.Errorf("triggers = %d, want none created", n)
		}
	})
	t.Run("a failed reminder row expires nothing", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC)
		f := newI29Logging(t, now, dentist(now), toToday, reminderRowsFail{memrepo.NewDecisionLog()})
		f.arm(t, "t1", "dentist", oldFire, before, nil, nil)
		f.captureFails(t)
		f.assertEditLandedUnsignalled(t, time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC))
		if got := f.armed(t, "dentist"); len(got) != 1 {
			t.Errorf("armed = %+v, want t1 still armed", got)
		}
	})

	t.Run("a retry repairs a reminder the failed step left behind", func(t *testing.T) {
		// The unit already holds the corrected date; its reminder still
		// watches the old one. The text is rewritten from the trigger's own
		// instant, fire_at plus its lead days.
		now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
		corrected := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
		u := dentist(now)
		u.EventAt, u.Content = &corrected, "Dentista el 2026-10-30 a las 10:00"
		f := newI29(t, now, u, toLater)
		f.arm(t, "t1", "dentist", oldFire, before, nil, nil)
		f.capture(t, "dentist")
		got := f.armed(t, "dentist")
		if len(got) != 1 || !got[0].FireAt.Equal(time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)) || got[0].Payload.ActionText != "Dentista el 2026-10-30 a las 10:00" {
			t.Errorf("armed = %+v, want t1 at the new lead time saying the new date", got)
		}
	})

	t.Run("a correction to the same date moves nothing", func(t *testing.T) {
		// The reminder already fires at once, stored at second precision;
		// the clock reads half a second later. Nothing visible changes, so
		// no row is written and the trigger is not rewritten.
		now := time.Date(2026, 10, 9, 8, 0, 0, 500_000_000, time.UTC)
		today := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
		u := dentist(now)
		u.EventAt, u.Content = &today, after
		f := newI29(t, now, u, toToday)
		f.arm(t, "t1", "dentist", now.Truncate(time.Second), after, nil, nil)
		f.capture(t, "dentist")
		if rows := reminderRows(t, f); len(rows) != 0 {
			t.Errorf("reminder rows = %v, want none", rows)
		}
		if got := f.armed(t, "dentist"); len(got) != 1 || got[0].Payload.Rationale != "" {
			t.Errorf("armed = %+v, want t1 not rewritten", got)
		}
	})

	t.Run("a moved reminder carries the payload of its new arming", func(t *testing.T) {
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		f := newI29(t, now, dentist(now), toLater)
		f.arm(t, "t1", "dentist", oldFire, before, nil, nil)
		f.capture(t, "dentist")
		got := f.armed(t, "dentist")
		if len(got) != 1 || !strings.Contains(got[0].Payload.Rationale, "2026-10-30T10:00:00Z") || got[0].Payload.LeadDays != prospection.EventLeadDays {
			t.Errorf("payload = %+v, want the rationale of an arming for 2026-10-30", got[0].Payload)
		}
	})

	t.Run("an anchor-only move shows the anchor", func(t *testing.T) {
		// The firing and the text already match the new date; only the
		// anchor moves, 10-29 -> 10-30, and the row must still show it.
		now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
		yearly := prospection.RuleYearly
		f := newI29(t, now, dentist(now), toLater)
		f.arm(t, "r1", "dentist", time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC), "Dentista el 2026-10-30", &yearly, &prospection.Anchor{Month: time.October, Day: 29})
		f.capture(t, "dentist")
		rows := reminderRows(t, f)
		if len(rows) != 1 {
			t.Fatalf("reminder rows = %d, want 1", len(rows))
		}
		if c := decodeI29(t, rows[0]); len(c.Fields) != 1 || c.Previous["recurrence_anchor"] != "10-29" || c.Next["recurrence_anchor"] != "10-30" {
			t.Errorf("moved row = %s, want the anchor previous -> next", rows[0].Context)
		}
	})
}

// reminderRowsFail refuses every correction.reminder.* row and records
// everything else.
type reminderRowsFail struct{ *memrepo.DecisionLog }

func (l reminderRowsFail) Record(ctx context.Context, d ports.Decision) error {
	if strings.HasPrefix(string(d.Action), "correction.reminder.") {
		return errors.New("disk full")
	}
	return l.DecisionLog.Record(ctx, d)
}

// assertEditLandedUnsignalled is the partial state a failed reminder step
// leaves: the unit edit landed, and no learning signal was written (D6).
func (f i29Fixture) assertEditLandedUnsignalled(t *testing.T, want time.Time) {
	t.Helper()
	if got := f.unit(t, "dentist").EventAt; got == nil || !got.Equal(want) {
		t.Errorf("EventAt = %v, want the corrected %s — the edit lands before the reminder step", got, want)
	}
	signals, err := f.signals.Since(context.Background(), time.Time{}, 100)
	if err != nil {
		t.Fatalf("signals.Since: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("signals = %d, want none — a correction whose reminder step failed did not land whole", len(signals))
	}
}

func (f i29Fixture) captureFails(t *testing.T) {
	t.Helper()
	if _, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: i29Message, Channel: "ui", ReferentID: "dentist"}); err == nil {
		t.Fatal("Capture = nil error, want the audit failure")
	}
}
