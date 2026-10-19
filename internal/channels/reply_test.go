package channels

import (
	"strings"
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
	"github.com/rengo/nooma/internal/core/prospection"
)

// TestRenderReply_NamesTheAppointmentNotTheNudge is the defect a reader hit
// twice before anyone looked at the code.
//
//	Pablo:  tengo dentista el viernes a las 9am
//	Nooma:  Reminder set for Wed 26 Aug, 15:26.
//
// The reading was perfect and the reply reported fire_at — the instant the
// nudge goes out, which a lead time and a clamp had moved to the capture
// instant. Twice read as a misparse by the person who wrote the message.
//
// Mutation: render FireAt again and the first assertion fails.
func TestRenderReply_NamesTheAppointmentNotTheNudge(t *testing.T) {
	loc := time.FixedZone("UTC-3", -3*60*60)
	capture := time.Date(2026, 8, 26, 15, 26, 0, 0, loc)
	appointment := time.Date(2026, 8, 28, 9, 0, 0, 0, loc)

	got := RenderReply(brain.CaptureResult{
		Outcome: brain.OutcomeArmed,
		Armed: &brain.Armed{
			What: prospection.ArmTrigger, FireAt: capture, About: appointment, Immediate: true,
		},
	})

	if !strings.Contains(got, "Fri 28 Aug, 09:00") {
		t.Errorf("reply %q does not name the appointment — a reader cannot tell a correct "+
			"reading from a misparse", got)
	}
	if strings.Contains(got, "15:26") {
		t.Errorf("reply %q names the capture instant, which is when the NUDGE goes out and "+
			"not what the reader asked about", got)
	}
}

// TestRenderReply_ImmediateIsNotADayBefore: the gap between the appointment
// and the firing is 41 hours here, and describing that as "the day before"
// is a true duration and a false promise — the reminder arrives at once.
//
// The plan carries the clamp as its own fact for exactly this reason, and
// this test is what stops a future reader from "simplifying" it back into a
// subtraction of the two instants.
//
// Mutation: compute the phrase from About minus FireAt and this fails.
func TestRenderReply_ImmediateIsNotADayBefore(t *testing.T) {
	loc := time.FixedZone("UTC-3", -3*60*60)
	armed := brain.Armed{
		What:      prospection.ArmTrigger,
		FireAt:    time.Date(2026, 8, 26, 15, 26, 0, 0, loc),
		About:     time.Date(2026, 8, 28, 9, 0, 0, 0, loc),
		Immediate: true,
	}

	got := RenderReply(brain.CaptureResult{Outcome: brain.OutcomeArmed, Armed: &armed})
	if !strings.Contains(got, "right away") {
		t.Errorf("reply %q does not say the reminder arrives at once; the 41-hour gap between "+
			"the appointment and the firing describes a wait that never happens", got)
	}

	// The same two instants without the clamp DO mean a wait, and must not
	// be flattened into the same sentence.
	armed.Immediate = false
	if got := RenderReply(brain.CaptureResult{Outcome: brain.OutcomeArmed, Armed: &armed}); strings.Contains(got, "right away") {
		t.Errorf("reply %q says the reminder is immediate for a firing that is not", got)
	}
}

// TestRenderReply_NamesEveryReminder: a timed event is reminded the day
// before and two hours before (ADR-0029), and the reply promises both.
//
// Mutation: render only FireAt's lead and this fails.
func TestRenderReply_NamesEveryReminder(t *testing.T) {
	about := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	armed := brain.Armed{What: prospection.ArmTrigger, FireAt: about.Add(-24 * time.Hour), About: about,
		Later: []time.Time{about.Add(-2 * time.Hour)}}
	got := RenderReply(brain.CaptureResult{Outcome: brain.OutcomeArmed, Armed: &armed})
	if !strings.Contains(got, "the day before and a few hours before") {
		t.Errorf("reply %q does not name both reminders", got)
	}
}

// TestRenderReply_ARingOnTheCaptureDayIsToday (ADR-0037): the reply reads the
// capture instant (result.At), not FireAt minus About, so a date-only event
// tomorrow told at 08:00 is "today at 09:00", not "the day before".
func TestRenderReply_ARingOnTheCaptureDayIsToday(t *testing.T) {
	zone := time.FixedZone("UTC-3", -3*60*60)
	day := time.Date(2026, 10, 20, 0, 0, 0, 0, zone)
	captured := time.Date(2026, 10, 19, 8, 0, 0, 0, zone)
	armed := brain.Armed{What: prospection.ArmTrigger, FireAt: time.Date(2026, 10, 19, 9, 0, 0, 0, zone), About: day, DayOnly: true}
	if got := RenderReply(brain.CaptureResult{Outcome: brain.OutcomeArmed, Armed: &armed, At: captured}); !strings.Contains(got, "I will remind you today at 09:00.") {
		t.Errorf("date-only reply %q does not name today at 09:00", got)
	}
	// An unasked date-only event (a local midnight, ADR-0029) names its date
	// alone, not "00:00".
	armed.DayOnly = false
	if got := RenderReply(brain.CaptureResult{Outcome: brain.OutcomeArmed, Armed: &armed, At: captured}); got != "Noted for Tue 20 Oct. I will remind you today at 09:00." {
		t.Errorf("unasked date-only reply = %q", got)
	}
	timed := brain.Armed{What: prospection.ArmTrigger, FireAt: time.Date(2026, 10, 19, 19, 0, 0, 0, zone), About: time.Date(2026, 10, 20, 1, 30, 0, 0, zone)}
	if got := RenderReply(brain.CaptureResult{Outcome: brain.OutcomeArmed, Armed: &timed, At: captured}); !strings.Contains(got, "I will remind you today at 19:00.") {
		t.Errorf("timed reply %q does not name today at 19:00", got)
	}
}

// A correction that found the unit already saying it is not "Corrected."
func TestRenderReply_AnUnchangedCorrectionSaysNothingChanged(t *testing.T) {
	changed := RenderReply(brain.CaptureResult{Outcome: brain.OutcomeCorrected, Correction: &brain.Correction{UnitID: "u"}})
	same := RenderReply(brain.CaptureResult{Outcome: brain.OutcomeCorrected, Correction: &brain.Correction{UnitID: "u", Unchanged: true}})
	if changed != "Corrected." {
		t.Errorf("changed = %q, want %q", changed, "Corrected.")
	}
	if same == changed || !strings.Contains(same, "Nothing") {
		t.Errorf("unchanged = %q, want a reply saying nothing changed", same)
	}
}
