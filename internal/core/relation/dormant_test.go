package relation

import (
	"math"
	"testing"
)

// TestDormant_BelowItsTypesPersistBar: a relation below its type's current
// persist bar is dormant (doc 02 §4, I49), at it or above it is awake, so
// the boundary agrees with Decide's: confidence == Persist is stored.
func TestDormant_BelowItsTypesPersistBar(t *testing.T) {
	bar := Thresholds{Persist: 0.35, Surface: 0.5}
	for _, tc := range []struct {
		confidence float64
		want       bool
	}{
		{0.34, true}, {0.35, false}, {0.36, false}, {0.9, false}, {0, true},
	} {
		if got := Dormant(tc.confidence, bar); got != tc.want {
			t.Errorf("Dormant(%v, %+v) = %v, want %v", tc.confidence, bar, got, tc.want)
		}
		if got, discard := Dormant(tc.confidence, bar), Decide(tc.confidence, bar) == Discard; got != discard {
			t.Errorf("confidence %v: Dormant = %v but Decide discards = %v; the two must agree", tc.confidence, got, discard)
		}
	}
}

// TestDormant_ANaNIsAwake: a confidence that is not a number is not hidden
// on its account; Confirmable already offers it the confirm that repairs it.
func TestDormant_ANaNIsAwake(t *testing.T) {
	if Dormant(math.NaN(), Thresholds{Persist: 0.3, Surface: 0.5}) {
		t.Error("a NaN confidence was hidden")
	}
}
