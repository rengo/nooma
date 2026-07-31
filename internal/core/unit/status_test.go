package unit

import (
	"reflect"
	"testing"
)

// TestStatus_IsStringKind proves R2.1's kind requirement directly, not only
// through AllStatuses' return type — the same anchor
// i01_focus_never_persisted_test.go pins from outside the package.
func TestStatus_IsStringKind(t *testing.T) {
	var zero Status
	if reflect.TypeOf(zero).Kind() != reflect.String {
		t.Errorf("Status has kind %s, want %s", reflect.TypeOf(zero).Kind(), reflect.String)
	}
}

// TestAllStatuses_HasExactlyTheDoc02Members proves R2.1: AllStatuses returns
// exactly {pool, archived, superseded, incomplete} as a set, and "focus" is
// not among them (I01).
func TestAllStatuses_HasExactlyTheDoc02Members(t *testing.T) {
	want := map[Status]bool{
		StatusPool:       true,
		StatusArchived:   true,
		StatusSuperseded: true,
		StatusIncomplete: true,
	}

	got := AllStatuses()
	if len(got) != len(want) {
		t.Fatalf("AllStatuses() has %d members, want %d: %v", len(got), len(want), got)
	}

	seen := make(map[Status]bool, len(got))
	for _, s := range got {
		if !want[s] {
			t.Errorf("AllStatuses() includes %q, which is not a doc 02 §1 member", s)
		}
		if seen[s] {
			t.Errorf("AllStatuses() lists %q more than once", s)
		}
		seen[s] = true
	}

	if seen[Status("focus")] {
		t.Error(`AllStatuses() includes "focus" — focus is a computed view (docs/02-cognitive-core.md §3), never a persisted status`)
	}
}

// TestAllStatuses_ReturnsAFreshSliceEachCall proves design D1: AllStatuses
// is a function returning a fresh slice, not an exported var — mutating one
// call's result must not affect the next.
func TestAllStatuses_ReturnsAFreshSliceEachCall(t *testing.T) {
	first := AllStatuses()
	first[0] = Status("mutated")

	second := AllStatuses()
	for _, s := range second {
		if s == Status("mutated") {
			t.Fatal("AllStatuses() shares backing storage across calls — mutating one call's result changed another's")
		}
	}
}
