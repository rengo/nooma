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
