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

// The three places that arm a reminder from a unit that already exists must
// agree on what it says: its title. A new row is only written by a re-plan, a
// catch-up or a preference re-arm, and each of them reads the unit itself.

// TestReplanArmsTheTitle: a unit corrected to a new date is re-planned by the
// due scan, and the reminders it arms say the title.
func TestReplanArmsTheTitle(t *testing.T) {
	v := newI37Vault(t)
	corrected := i37Day(16, 11, 0)
	v.titled("u", "Reunión con Juan", "Reunión con Juan el 2026-10-16 a las 11:00", &corrected)
	v.fired("old", "u", "dentist", i37Day(14, 8, 0), modern(i37Day(15, 11, 0), 27*60))

	v.check(false)

	got := v.armed("u")
	if len(got) == 0 {
		t.Fatal("nothing was armed for the corrected date")
	}
	for _, tr := range got {
		if tr.Payload.ActionText != "Reunión con Juan" {
			t.Errorf("re-planned reminder %s says %q, want the title", tr.ID, tr.Payload.ActionText)
		}
	}
}

// TestCatchUpArmsTheTitle: the catch-up an event is owed says the title.
func TestCatchUpArmsTheTitle(t *testing.T) {
	v := newI37Vault(t)
	event := i37Day(14, 11, 0)
	v.titled("u", "Reunión con Juan", "Reunión con Juan el 2026-10-14 a las 11:00", &event)
	v.fired("late-a", "u", "dentist", event.Add(-24*time.Hour), modern(event, 24*60))

	v.check(false)

	got := v.catchUps("u")
	if len(got) != 1 || got[0].Payload.ActionText != "Reunión con Juan" {
		t.Errorf("catch-ups = %+v, want one saying the title", got)
	}
}

// TestPreferenceReArmArmsTheTitle: a lead added by a preference save arms a
// reminder that says the title.
func TestPreferenceReArmArmsTheTitle(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	flight := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	v := newI35(t, now)
	v.unit(t, "flight", unit.TypeEvent, flight, now)
	if err := v.units.UpdateTitle(context.Background(), "flight", "Vuelo a Lisboa", now); err != nil {
		t.Fatal(err)
	}
	v.arm(t, "flight-3h", "flight", flight.Add(-3*time.Hour), 180, nil)

	p := i35Defaults()
	p.LeadMinutes = []int{180, 30}
	if _, err := v.svc.Save(context.Background(), p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := v.triggers.ArmedForUnit(context.Background(), "flight")
	if err != nil || len(got) != 2 {
		t.Fatalf("armed = %+v, %v; want the 3 h reminder and the new 30 min one", got, err)
	}
	for _, tr := range got {
		if tr.ID != "flight-3h" && tr.Payload.ActionText != "Vuelo a Lisboa" {
			t.Errorf("the new reminder %s says %q, want the title", tr.ID, tr.Payload.ActionText)
		}
	}
}
