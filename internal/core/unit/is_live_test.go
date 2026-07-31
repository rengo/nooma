package unit

import "testing"

// TestStatus_IsLive proves R2.2: the live predicate filters positively —
// true for exactly pool, false for every other AllStatuses() member and
// for a deliberately unknown Status value (design D2).
func TestStatus_IsLive(t *testing.T) {
	cases := map[Status]bool{
		StatusPool:       true,
		StatusArchived:   false,
		StatusSuperseded: false,
		StatusIncomplete: false,
		Status("bogus"):  false,
	}

	if len(cases)-1 != len(AllStatuses()) {
		t.Fatalf("test table covers %d known statuses, AllStatuses() has %d — table is out of sync", len(cases)-1, len(AllStatuses()))
	}

	for s, want := range cases {
		if got := s.IsLive(); got != want {
			t.Errorf("Status(%q).IsLive() = %v, want %v", s, got, want)
		}
	}
}
