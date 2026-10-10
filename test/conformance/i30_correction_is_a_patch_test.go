// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

// The client's report on main 249e950, reproduced as written: a flight
// captured at 09:00, corrected from its unit page with "Es a las 8am".
const (
	i30Message = "Es a las 8am"
	i30Body    = "Tengo un vuelo el 2026-10-11 a las 09:00."
	i30Fixed   = "Tengo un vuelo el 2026-10-11 a las 08:00."
)

// i30Zone is the client's own frame: the body was written at UTC-3.
var i30Zone = time.FixedZone("ART", -3*60*60)

func i30Flight(now time.Time) unit.Unit {
	at := time.Date(2026, 10, 11, 9, 0, 0, 0, i30Zone)
	return unit.Unit{ID: "flight", Type: unit.TypeEvent, Status: unit.StatusPool, Content: i30Body,
		EventAt: &at, Source: "ui", CreatedAt: now, UpdatedAt: now}
}

// TestI30_ACorrectionIsAPatch is doc 02 §5 step 4's patch rule, I30: a
// correction changes only what it speaks about and keeps the rest of the
// unit. The model is shown the unit it corrects, so a time said alone can
// come back as the whole instant; and a content fallback that would no
// longer state the date and time the unit still holds asks instead of
// writing — "Es a las 08:00." replaced the flight's whole body on main
// while its event stayed at 09:00.
func TestI30_ACorrectionIsAPatch(t *testing.T) {
	now := time.Date(2026, 10, 10, 18, 0, 0, 0, i30Zone)

	t.Run("the classification is shown the unit it corrects", func(t *testing.T) {
		f := newCorrectionFixture(t, now, i30Flight(now), i30Message,
			`{"type":"correction","normalized_content":"`+i30Fixed+`","weight":0.7,"decay_rate":0.03,"event_at":"2026-10-11T08:00:00-03:00","language":"es"}`,
			memrepo.NewDecisionLog())
		f.capture(t, "flight")

		prompts := f.llm.SeenPrompts()
		if len(prompts) != 1 {
			t.Fatalf("classify calls = %d, want 1", len(prompts))
		}
		for _, want := range []string{i30Body, "2026-10-11T09:00:00-03:00", i30Message} {
			if !strings.Contains(prompts[0], want) {
				t.Errorf("classify prompt does not carry %q — a time said alone cannot be resolved against a unit the model never saw", want)
			}
		}

		got := f.unit(t, "flight")
		if got.Content != i30Fixed {
			t.Errorf("content = %q, want %q", got.Content, i30Fixed)
		}
		want := time.Date(2026, 10, 11, 8, 0, 0, 0, i30Zone)
		if got.EventAt == nil || !got.EventAt.Equal(want) {
			t.Errorf("event_at = %v, want %v", got.EventAt, want)
		}
	})

	// assertAsked is the outcome when the model answers with content
	// alone: nothing written, the reason named, the unit as it was.
	assertAsked := func(t *testing.T, f i29Fixture, referent string) {
		t.Helper()
		result, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: f.message, Channel: "ui", ReferentID: referent})
		if err != nil {
			t.Fatalf("Capture: %v", err)
		}
		if result.Outcome != brain.OutcomeAsked || result.Correction == nil || result.Correction.Why != brain.AskContentLosesDate {
			t.Fatalf("result = %+v, want OutcomeAsked because %q", result, brain.AskContentLosesDate)
		}
		got := f.unit(t, "flight")
		if got.Content != i30Body || got.EventAt == nil || !got.EventAt.Equal(*i30Flight(now).EventAt) {
			t.Errorf("unit = %q at %v, want it untouched", got.Content, got.EventAt)
		}
		if n := len(f.rows(t, ports.ActionCorrectionApplied)); n != 0 {
			t.Errorf("correction.applied rows = %d, want 0", n)
		}
		asks := f.rows(t, ports.ActionCorrectionAmbiguous)
		if len(asks) != 1 {
			t.Fatalf("correction.ambiguous rows = %d, want 1", len(asks))
		}
		var why struct {
			Reason string `json:"reason"`
			UnitID string `json:"unit_id"`
		}
		if err := json.Unmarshal(asks[0].Context, &why); err != nil {
			t.Fatalf("decoding %s: %v", asks[0].Context, err)
		}
		if why.Reason != string(brain.AskContentLosesDate) || why.UnitID != "flight" {
			t.Errorf("ask context = %s, want reason %q for unit flight", asks[0].Context, brain.AskContentLosesDate)
		}
	}

	// The response gpt-6-luna gave on main: the utterance, normalized, as
	// if it were the whole unit, and no event_at.
	const observed = `{"type":"correction","normalized_content":"Es a las 08:00.","weight":0.7,"decay_rate":0.03,"language":"es"}`

	t.Run("content that drops the unit's date asks, explicit referent", func(t *testing.T) {
		assertAsked(t, newCorrectionFixture(t, now, i30Flight(now), i30Message, observed, memrepo.NewDecisionLog()), "flight")
	})

	t.Run("content that drops the unit's date asks, chat path", func(t *testing.T) {
		assertAsked(t, newCorrectionFixture(t, now, i30Flight(now), i30Message, observed, memrepo.NewDecisionLog()), "")
	})

	t.Run("content that restates another time than the unit holds asks", func(t *testing.T) {
		// Right text, no event_at: writing it would leave the body at
		// 08:00 and the event, and its reminder, at 09:00.
		response := `{"type":"correction","normalized_content":"` + i30Fixed + `","weight":0.7,"decay_rate":0.03,"language":"es"}`
		assertAsked(t, newCorrectionFixture(t, now, i30Flight(now), i30Message, response, memrepo.NewDecisionLog()), "flight")
	})

	// The prompt shows the model the current event_at, so a text-only
	// correction can come back with that instant echoed beside the new
	// text. An echo moves nothing: it is no date edit, and the text is
	// what the correction speaks about.
	t.Run("an echoed event_at is no edit, and the text it came with is written", func(t *testing.T) {
		const rome = "Tengo un vuelo a Roma el 2026-10-11 a las 09:00."
		f := newCorrectionFixture(t, now, i30Flight(now), "es a Roma",
			`{"type":"correction","normalized_content":"`+rome+`","weight":0.7,"decay_rate":0.03,"event_at":"2026-10-11T09:00:00-03:00","language":"es"}`,
			memrepo.NewDecisionLog())
		f.capture(t, "flight")
		got := f.unit(t, "flight")
		if got.Content != rome {
			t.Errorf("content = %q, want %q", got.Content, rome)
		}
		if got.EventAt == nil || !got.EventAt.Equal(*i30Flight(now).EventAt) {
			t.Errorf("event_at = %v, want it unchanged", got.EventAt)
		}
		applied := f.rows(t, ports.ActionCorrectionApplied)
		if len(applied) != 1 {
			t.Fatalf("correction.applied rows = %d, want 1", len(applied))
		}
		if c := decodeI29(t, applied[0]); len(c.Fields) != 1 || c.Fields[0] != "content" {
			t.Errorf("fields = %v, want [content] — an echoed date is not an edit", c.Fields)
		}
	})

	// An echo beside content that would lose the instant does not hand
	// the body to that content: it stands as a same-date correction, the
	// shape I29's repair path repeats, and the body stays as it was.
	t.Run("an echoed event_at beside content that drops the date leaves the body", func(t *testing.T) {
		f := newCorrectionFixture(t, now, i30Flight(now), "es a Roma",
			`{"type":"correction","normalized_content":"Es a Roma.","weight":0.7,"decay_rate":0.03,"event_at":"2026-10-11T09:00:00-03:00","language":"es"}`,
			memrepo.NewDecisionLog())
		f.capture(t, "flight")
		got := f.unit(t, "flight")
		if got.Content != i30Body || got.EventAt == nil || !got.EventAt.Equal(*i30Flight(now).EventAt) {
			t.Errorf("unit = %q at %v, want it as it was", got.Content, got.EventAt)
		}
	})

	// The unit is read before the model call, to show it to the model, and
	// the call can be long. What gets recorded and rewritten is the unit as
	// it is when the correction lands, not as it was shown.
	t.Run("the edit lands on the unit as it is after the call", func(t *testing.T) {
		const edited = "Tengo un vuelo el 2026-10-11 a las 09:00. Asiento 12A."
		f := newCorrectionFixture(t, now, i30Flight(now), i30Message,
			`{"type":"correction","normalized_content":"`+i30Fixed+`","weight":0.7,"decay_rate":0.03,"event_at":"2026-10-11T08:00:00-03:00","language":"es"}`,
			memrepo.NewDecisionLog())
		f.hook.during = func() {
			if err := f.units.UpdateContent(context.Background(), "flight", edited, now); err != nil {
				t.Fatalf("UpdateContent: %v", err)
			}
		}
		f.capture(t, "flight")
		if got := f.unit(t, "flight").Content; got != "Tengo un vuelo el 2026-10-11 a las 08:00. Asiento 12A." {
			t.Errorf("content = %q, want the edit made during the call kept and the time moved", got)
		}
		c := decodeI29(t, f.rows(t, ports.ActionCorrectionApplied)[0])
		if c.Previous["content"] != edited {
			t.Errorf("pre-image content = %v, want %q — the body actually overwritten", c.Previous["content"], edited)
		}
	})

	t.Run("control: content that keeps the instant is written", func(t *testing.T) {
		const rome = "Tengo un vuelo a Roma el 2026-10-11 a las 09:00."
		f := newCorrectionFixture(t, now, i30Flight(now), "es a Roma",
			`{"type":"correction","normalized_content":"`+rome+`","weight":0.7,"decay_rate":0.03,"language":"es"}`,
			memrepo.NewDecisionLog())
		f.capture(t, "flight")
		if got := f.unit(t, "flight").Content; got != rome {
			t.Errorf("content = %q, want %q", got, rome)
		}
	})
}
