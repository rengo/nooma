// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
	"github.com/rengo/nooma/test/support/goldenset"
)

// titled seeds a Spanish unit that has both its dated content and the title a
// reminder should say (spec R12).
func (v *i37Vault) titled(id, title, content string, at *time.Time) {
	v.t.Helper()
	if err := v.units.Create(context.Background(), unit.Unit{
		ID: id, Type: unit.TypeEvent, Status: unit.StatusPool, Content: content, Title: title, Weight: 1, Language: "es",
		EventAt: at, LastTouchedAt: i37Now, CreatedAt: i37Now, UpdatedAt: i37Now,
	}); err != nil {
		v.t.Fatalf("seed unit %s: %v", id, err)
	}
}

// TestRenderUsesTheTitleWhenPresent: the client's message "Tengo reunión con Juan
// hoy a las 22:30" is pushed as a title and a date, not as the sentence with its
// own date in it.
func TestRenderUsesTheTitleWhenPresent(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 22, 30)
	v.titled("meeting", "Reunión con Juan", "Tengo reunión con Juan hoy a las 22:30", &event)
	v.row("r", "meeting", "frozen", i37Now.Add(-time.Minute), modern(event, 751))

	v.check(false)

	want := "Recordatorio: Reunión con Juan — mié 14 oct, 22:30"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Errorf("sent %q, want exactly [%q]", v.ch.texts, want)
	}
}

// TestRenderFallsBackToContentWithoutTitle: a unit from before titles, or whose
// title the check dropped, reads its content.
func TestRenderFallsBackToContentWithoutTitle(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 22, 30)
	v.titled("meeting", "", "Tengo reunión con Juan hoy a las 22:30", &event)
	v.row("r", "meeting", "frozen", i37Now.Add(-time.Minute), modern(event, 751))

	v.check(false)

	want := "Recordatorio: Tengo reunión con Juan hoy a las 22:30 — mié 14 oct, 22:30"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Errorf("sent %q, want exactly [%q]", v.ch.texts, want)
	}
}

// TestDigestLineHasNoRecordatorioPrefix: a digest line is the title and when,
// with the framing of a push left to the push.
func TestDigestLineHasNoRecordatorioPrefix(t *testing.T) {
	v := newI37Vault(t)
	v.titled("bd", "Cumpleaños de Leo", "Cumpleaños de Leo el 21 de octubre", i37At(1, 12))
	v.fired("b", "bd", "frozen", i37Day(13, 16, 0), yearlyOn21)

	v.check(false)

	want := "Hoy hay 1 cosa:\n• Cumpleaños de Leo — mié 21 oct"
	if len(v.ch.texts) != 1 || v.ch.texts[0] != want {
		t.Errorf("digest sent %q, want exactly [%q]", v.ch.texts, want)
	}
}

// TestFrozenTextUsesTheTitle: what an armed reminder says when its unit cannot be
// read later is the title the capture classified, not the dated sentence. The
// response is the test's own synthetic one (non-negotiable 5): what it pins is
// the wiring from a decoded title to the trigger.
func TestFrozenTextUsesTheTitle(t *testing.T) {
	dir := t.TempDir()
	response, err := json.Marshal(map[string]any{
		"type": "event", "normalized_content": "Tengo reunión con Juan el 2026-10-16 a las 22:30",
		"weight": 1.0, "decay_rate": 0.05, "event_at": "2026-10-16T22:30:00-03:00",
		"title": "Reunión con Juan", "language": "es",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(goldenset.LLMExample{
		ID: "synthetic-titled-event", Provider: "fake", Model: "fake", Task: "capture_processing",
		Message: "Tengo reunión con Juan el viernes a las 22:30", Response: string(response),
	})
	if err := os.WriteFile(filepath.Join(dir, "synthetic-titled-event.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.FixedZone("ART", -3*60*60))

	units, triggers, _, result := captureAndArmFrom(t, dir, now, "synthetic-titled-event")

	u, err := units.ByID(context.Background(), result.UnitID)
	if err != nil || u.Title != "Reunión con Juan" {
		t.Fatalf("stored unit title = %q, %v; want the classified title", u.Title, err)
	}
	armed := triggers.All()
	if len(armed) == 0 {
		t.Fatalf("nothing was armed: %+v", result)
	}
	for _, tr := range armed {
		if tr.Payload.ActionText != "Reunión con Juan" {
			t.Errorf("trigger %s says %q, want the title", tr.ID, tr.Payload.ActionText)
		}
	}
}
