// Package conformance — see test/conformance/doc.go for the package contract.
package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/core/unit"
)

// TestCapture_AnAppointmentOnABareWeekdayIsAnArmedEvent replays the answer
// doc 02 §5 step 1 asks for to the message a client captured on Friday
// 2026-10-09 at 11:46 in Buenos Aires: "dentista el viernes a las 10".
// A bare weekday named on that same weekday is next week's, and an
// appointment is an event — so event_at is Friday 2026-10-16 10:00 local,
// and the unit carries a reminder.
//
// The live capture came back as a task due Tuesday the 13th with nothing
// armed. That was the model's answer, which no test here can reach
// (non-negotiable #5): what this pins is that the answer the prompt now asks
// for is stored as the event it names and arms its reminder. The case it
// replays is also what `nooma doctor` sends a real provider, so the
// maintainer can read what a real model answers to the same message.
func TestCapture_AnAppointmentOnABareWeekdayIsAnArmedEvent(t *testing.T) {
	art := time.FixedZone("America/Argentina/Buenos_Aires", -3*60*60)
	now := time.Date(2026, 10, 9, 11, 46, 0, 0, art)
	nextFriday := time.Date(2026, 10, 16, 10, 0, 0, 0, art)

	if nextFriday.Weekday() != time.Friday || nextFriday.Sub(now) < 6*24*time.Hour {
		t.Fatalf("fixture: %v is not next week's Friday from %v", nextFriday, now)
	}

	units, triggers, _, result := captureAndArmWithUnits(t, now, "classify-event-dentist-next-friday")

	u, err := units.ByID(context.Background(), result.UnitID)
	if err != nil {
		t.Fatalf("units.ByID(%q): %v", result.UnitID, err)
	}
	if u.Type != unit.TypeEvent {
		t.Errorf("Type = %q, want %q — an appointment is an event", u.Type, unit.TypeEvent)
	}
	if u.EventAt == nil || !u.EventAt.Equal(nextFriday) {
		t.Errorf("EventAt = %v, want %v — the following Friday at 10:00 local", u.EventAt, nextFriday)
	}
	if result.Armed == nil {
		t.Fatalf("Armed = nil — the appointment got no reminder: %+v", result)
	}
	if got := triggers.Count(); got != 1 {
		t.Errorf("triggers.Count() = %d, want 1 armed reminder", got)
	}
}
