package relation

import (
	"math"
	"testing"
)

// Confirmable offers a confirm for every stored relation below its type's
// surface threshold, the band a confirm raises it out of, and for nothing
// at or above it, where the relation is already in force.
func TestConfirmable_OnlyBelowTheSurfaceThreshold(t *testing.T) {
	th := Thresholds{Persist: 0.30, Surface: 0.50}
	cases := []struct {
		confidence float64
		want       bool
	}{
		{0.40, true},  // the uncertain band
		{0.30, true},  // its lower edge
		{0.20, true},  // stored before the persist threshold rose
		{0.50, false}, // the surface threshold itself is asserted (Decide)
		{0.99, false},
		{math.NaN(), true}, // ConfirmedConfidence lands a NaN on the floor
	}
	for _, c := range cases {
		if got := Confirmable(c.confidence, th); got != c.want {
			t.Errorf("Confirmable(%v) = %v, want %v", c.confidence, got, c.want)
		}
	}
}
