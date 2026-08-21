package prospection

import (
	"testing"
	"time"
)

// TestLeadTime proves spec R5.2's arithmetic and its explicit refusal to
// clamp at this layer.
func TestLeadTime(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*60*60)
	at := func(day int) time.Time { return time.Date(2027, time.June, day, 9, 30, 0, 0, loc) }

	t.Run("an event nine days out fires two days out", func(t *testing.T) {
		if got, want := LeadTime(at(10)), at(3); !got.Equal(want) {
			t.Errorf("LeadTime(%v) = %v, want %v", at(10), got, want)
		}
	})

	t.Run("the wall clock and zone are carried, not reset", func(t *testing.T) {
		got := LeadTime(at(10))
		if got.Hour() != 9 || got.Minute() != 30 {
			t.Errorf("LeadTime = %v, want the event's own wall clock (09:30) — the horizon "+
				"shifts the date, not the time of day", got)
		}
		if got.Location().String() != loc.String() {
			t.Errorf("LeadTime = %v, want the event's own zone", got)
		}
	})

	t.Run("an event closer than the horizon returns a past instant, unclamped", func(t *testing.T) {
		// Spec R5.2 is explicit that this layer does not clamp. The clamp is
		// Arm's, because "do not arm in the past" is a fact about arming and
		// this function answers a fact about the event.
		eventAt := at(3)
		got := LeadTime(eventAt)
		if !got.Before(eventAt) {
			t.Fatalf("LeadTime(%v) = %v, want an earlier instant", eventAt, got)
		}
		if want := time.Date(2027, time.May, 27, 9, 30, 0, 0, loc); !got.Equal(want) {
			t.Errorf("LeadTime = %v, want %v — the horizon crosses the month boundary rather "+
				"than stopping at it", got, want)
		}
	})
}
