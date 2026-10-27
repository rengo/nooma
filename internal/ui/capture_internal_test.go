package ui

import (
	"testing"
	"time"

	"github.com/rengo/nooma/internal/brain"
)

// TestTimersSummary says what a list or a cancel of the live timers did, one
// sentence per answer (ADR-0051). A cancel names the timer as chat does, by
// when it rings and what it says: QA on #402 read "Cancelled the timer t2.".
func TestTimersSummary(t *testing.T) {
	zone := time.FixedZone("ART", -3*3600)
	at := time.Date(2026, 10, 26, 10, 10, 0, 0, zone)
	text := "Sacar la pizza del horno"
	pizza := brain.ListedTimer{ID: "t2", FireAt: time.Date(2026, 10, 26, 10, 28, 0, 0, zone), Text: &text}
	one := []brain.ListedTimer{pizza}
	for _, tc := range []struct {
		answer brain.TimersAnswer
		want   string
	}{
		{brain.TimersAnswer{Cancelled: &pizza}, "Cancelled: 10:28 — Sacar la pizza del horno."},
		{brain.TimersAnswer{Gone: true}, "That timer is no longer set."},
		{brain.TimersAnswer{}, "No timers are set."},
		{brain.TimersAnswer{Listed: one, Which: true}, "Nothing was cancelled: it was not clear which timer. Name it by its time or what it says."},
		{brain.TimersAnswer{Listed: one, More: 2}, "3 timer(s) set."},
	} {
		if got := timersSummary(brain.CaptureResult{Outcome: brain.OutcomeTimers, Timers: &tc.answer, At: at}); got != tc.want {
			t.Errorf("timersSummary(%+v) = %q, want %q", tc.answer, got, tc.want)
		}
	}
}
