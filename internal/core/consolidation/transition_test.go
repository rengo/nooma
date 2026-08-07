package consolidation

import "testing"

// TestAllReasons_HasExactlyThreeMembers is the C14 length guard: it must
// fail on AllReasons()'s length before any content assertion runs, against
// a zero-value stub that returns nil (spec R1.4).
func TestAllReasons_HasExactlyThreeMembers(t *testing.T) {
	reasons := AllReasons()
	if len(reasons) != 3 {
		t.Fatalf("AllReasons() returned %d reasons, want 3: %v", len(reasons), reasons)
	}

	want := map[Reason]bool{
		ReasonIncompletePromoted:   true,
		ReasonIncompleteExpired:    true,
		ReasonBelowWeightThreshold: true,
	}
	for _, r := range reasons {
		if !want[r] {
			t.Errorf("AllReasons() contains unexpected reason %q", r)
		}
		delete(want, r)
	}
	if len(want) != 0 {
		t.Errorf("AllReasons() is missing reasons: %v", want)
	}
}

// TestAllReasons_ReturnsAFreshSlice guards against a shared backing array:
// mutating one call's result must not affect a later call.
func TestAllReasons_ReturnsAFreshSlice(t *testing.T) {
	a := AllReasons()
	if len(a) == 0 {
		t.Fatal("AllReasons() returned zero reasons — nothing to mutate-guard")
	}
	a[0] = Reason("mutated")

	b := AllReasons()
	if len(b) == 0 {
		t.Fatal("AllReasons() returned zero reasons on the second call — nothing to compare")
	}
	if b[0] == Reason("mutated") {
		t.Fatal("AllReasons() does not return a fresh slice — mutating one call's result affected a later call")
	}
}
