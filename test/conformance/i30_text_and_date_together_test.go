// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/correction"
	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/internal/ports"
	"github.com/rengo/nooma/test/support/memrepo"
)

const (
	i30tdBody  = "Tengo un vuelo a Madrid el 2026-12-20 a las 09:00."
	i30tdRome  = "Tengo un vuelo a Roma el 2026-12-20 a las 08:00."
	i30tdAsked = "Es a Roma a las 8"
)

func i30tdFlight(now time.Time) unit.Unit {
	at := time.Date(2026, 12, 20, 9, 0, 0, 0, i30Zone)
	return unit.Unit{ID: "flight", Type: unit.TypeEvent, Status: unit.StatusPool, Content: i30tdBody,
		EventAt: &at, Source: "ui", CreatedAt: now, UpdatedAt: now}
}

func i30tdResponse(content, eventAt string) string {
	return `{"type":"correction","normalized_content":"` + content + `","weight":0.7,"decay_rate":0.03,"event_at":"` + eventAt + `","language":"es"}`
}

// TestI30_ACorrectionOfTextAndTimeKeepsBoth is the patch rule when one
// message changes what the unit says and when it happens: the new text,
// which states the new instant, and the new date are both written, the
// reminder follows, and the row records both fields. Before this the date
// won and the old body's time was rewritten, dropping "Roma" silently.
func TestI30_ACorrectionOfTextAndTimeKeepsBoth(t *testing.T) {
	now := time.Date(2026, 10, 10, 18, 0, 0, 0, i30Zone)
	oldFire := time.Date(2026, 12, 13, 9, 0, 0, 0, i30Zone)

	t.Run("explicit referent", func(t *testing.T) {
		referent := "flight"
		{
			f := newCorrectionFixture(t, now, i30tdFlight(now), i30tdAsked,
				i30tdResponse(i30tdRome, "2026-12-20T08:00:00-03:00"), memrepo.NewDecisionLog())
			f.arm(t, "t1", "flight", oldFire, i30tdBody, nil, nil)
			result, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: f.message, Channel: "ui", ReferentID: referent})
			if err != nil {
				t.Fatalf("Capture: %v", err)
			}
			if result.Outcome != brain.OutcomeCorrected || result.Correction == nil ||
				len(result.Correction.Fields) != 2 || result.Correction.Fields[0] != correction.FieldEventAt || result.Correction.Fields[1] != correction.FieldContent {
				t.Fatalf("result = %+v, want corrected [event_at content]", result)
			}

			got := f.unit(t, "flight")
			if got.Content != i30tdRome {
				t.Errorf("content = %q, want %q — the text the correction spoke about", got.Content, i30tdRome)
			}
			if want := time.Date(2026, 12, 20, 8, 0, 0, 0, i30Zone); got.EventAt == nil || !got.EventAt.Equal(want) {
				t.Errorf("event_at = %v, want %v", got.EventAt, want)
			}
			armed := f.armed(t, "flight")
			wantFire := time.Date(2026, 12, 13, 8, 0, 0, 0, i30Zone)
			if len(armed) != 1 || armed[0].ID != "t1" || !armed[0].FireAt.Equal(wantFire) || armed[0].Payload.ActionText != i30tdRome {
				t.Errorf("armed = %+v, want t1 at %s saying %q", armed, wantFire, i30tdRome)
			}
			applied := f.rows(t, ports.ActionCorrectionApplied)
			if len(applied) != 1 {
				t.Fatalf("correction.applied rows = %d, want 1", len(applied))
			}
			if c := decodeI29(t, applied[0]); len(c.Fields) != 2 || c.Fields[0] != "event_at" || c.Fields[1] != "content" || c.Previous["content"] != i30tdBody || c.Next["content"] != i30tdRome {
				t.Errorf("row = %s, want [event_at content], %q -> %q", applied[0].Context, i30tdBody, i30tdRome)
			}
		}
	})

	// The chat path finds its referent after the model answered, so the
	// model never saw the unit: its text is the utterance's. The date is
	// the edit and the unit's own body follows it.
	t.Run("chat path: the date alone, the old body carried", func(t *testing.T) {
		f := newCorrectionFixture(t, now, i30tdFlight(now), i30tdAsked,
			i30tdResponse(i30tdRome, "2026-12-20T08:00:00-03:00"), memrepo.NewDecisionLog())
		f.capture(t, "")
		if got := f.unit(t, "flight").Content; got != "Tengo un vuelo a Madrid el 2026-12-20 a las 08:00." {
			t.Errorf("content = %q, want the unit's own body moved to 08:00", got)
		}
	})

	// The text was written against the unit as shown; an edit made while
	// the model was thinking is not hers to discard.
	t.Run("a unit edited during the call keeps its edit", func(t *testing.T) {
		f := newCorrectionFixture(t, now, i30tdFlight(now), i30tdAsked,
			i30tdResponse(i30tdRome, "2026-12-20T08:00:00-03:00"), memrepo.NewDecisionLog())
		f.hook.during = func() {
			if err := f.units.UpdateContent(context.Background(), "flight", i30tdBody+" Asiento 12A.", now); err != nil {
				t.Fatalf("UpdateContent: %v", err)
			}
		}
		f.capture(t, "flight")
		if got := f.unit(t, "flight").Content; got != "Tengo un vuelo a Madrid el 2026-12-20 a las 08:00. Asiento 12A." {
			t.Errorf("content = %q, want the concurrent edit kept", got)
		}
	})

	t.Run("text that does not state the new instant yields to the date, which carries the old body", func(t *testing.T) {
		const sameOldTime = "Tengo un vuelo a Roma el 2026-12-20 a las 09:00."
		f := newCorrectionFixture(t, now, i30tdFlight(now), i30tdAsked,
			i30tdResponse(sameOldTime, "2026-12-20T08:00:00-03:00"), memrepo.NewDecisionLog())
		f.capture(t, "flight")
		if got := f.unit(t, "flight").Content; got != "Tengo un vuelo a Madrid el 2026-12-20 a las 08:00." {
			t.Errorf("content = %q, want the old body carried to 08:00", got)
		}
	})
}

// TestI30_ACorrectionThatChangesNothingSaysSo is doc 02 §5 step 4's
// same-date correction: repeating a correction finds the unit already
// holding the value, so nothing is a change: no correction.applied row
// claiming one, no learning signal for an edit that did not happen, and
// the answer says nothing changed.
func TestI30_ACorrectionThatChangesNothingSaysSo(t *testing.T) {
	now := time.Date(2026, 10, 10, 18, 0, 0, 0, i30Zone)
	done := i30tdFlight(now)
	done.Content = i30tdRome
	at := time.Date(2026, 12, 20, 8, 0, 0, 0, i30Zone)
	done.EventAt = &at

	for _, response := range []string{
		i30tdResponse(i30tdRome, "2026-12-20T08:00:00-03:00"),    // text and date both repeated
		i30tdResponse("Es a Roma.", "2026-12-20T08:00:00-03:00"), // date alone repeated
	} {
		f := newCorrectionFixture(t, now, done, i30tdAsked, response, memrepo.NewDecisionLog())
		result, err := f.svc.Capture(context.Background(), brain.CaptureInput{Text: f.message, Channel: "ui", ReferentID: "flight"})
		if err != nil {
			t.Fatalf("Capture: %v", err)
		}
		if result.Outcome != brain.OutcomeCorrected || result.Correction == nil || !result.Correction.Unchanged || len(result.Correction.Fields) != 0 {
			t.Fatalf("result = %+v, want corrected with Unchanged and no fields", result)
		}
		if n := len(f.rows(t, ports.ActionCorrectionApplied)); n != 0 {
			t.Errorf("correction.applied rows = %d, want 0 — nothing changed", n)
		}
		signals, err := f.signals.Since(context.Background(), time.Time{}, 10)
		if err != nil || len(signals) != 0 {
			t.Errorf("signals = %v, %v; want none", signals, err)
		}
		if got := f.unit(t, "flight"); got.Content != i30tdRome {
			t.Errorf("content = %q, want it untouched", got.Content)
		}
	}
}
