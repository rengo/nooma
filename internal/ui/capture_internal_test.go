package ui

import (
	"testing"

	"github.com/rengo/nooma/internal/brain"
)

// TestTimersSummary says what a list or a cancel of the live timers did, one
// sentence per answer (ADR-0051).
func TestTimersSummary(t *testing.T) {
	one := []brain.ListedTimer{{ID: "tmr-1"}}
	for _, tc := range []struct {
		answer brain.TimersAnswer
		want   string
	}{
		{brain.TimersAnswer{Cancelled: &one[0]}, "Cancelled the timer tmr-1."},
		{brain.TimersAnswer{Gone: true}, "That timer is no longer set."},
		{brain.TimersAnswer{}, "No timers are set."},
		{brain.TimersAnswer{Listed: one, Which: true}, "Nothing was cancelled: it was not clear which timer. Name it by its time or what it says."},
		{brain.TimersAnswer{Listed: one, More: 2}, "3 timer(s) set."},
	} {
		if got := timersSummary(tc.answer); got != tc.want {
			t.Errorf("timersSummary(%+v) = %q, want %q", tc.answer, got, tc.want)
		}
	}
}
