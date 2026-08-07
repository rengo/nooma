package relation

import (
	"errors"
	"reflect"
	"testing"
)

// TestAllCreatedBy_HasExactlyTheDoc02Members proves R4.7: AllCreatedBy
// returns exactly {system, consolidation, user} as a fresh 3-slice.
func TestAllCreatedBy_HasExactlyTheDoc02Members(t *testing.T) {
	want := map[CreatedBy]bool{
		CreatedBySystem:        true,
		CreatedByConsolidation: true,
		CreatedByUser:          true,
	}

	got := AllCreatedBy()
	if len(got) != len(want) {
		t.Fatalf("AllCreatedBy() has %d members, want %d: %v", len(got), len(want), got)
	}

	seen := make(map[CreatedBy]bool, len(got))
	for _, c := range got {
		if !want[c] {
			t.Errorf("AllCreatedBy() includes %q, which is not a doc 02 §4 member", c)
		}
		if seen[c] {
			t.Errorf("AllCreatedBy() lists %q more than once", c)
		}
		seen[c] = true
	}
}

// TestAllCreatedBy_ReturnsAFreshSliceEachCall proves the house pattern:
// mutating one call's result must not affect the next.
func TestAllCreatedBy_ReturnsAFreshSliceEachCall(t *testing.T) {
	first := AllCreatedBy()
	if len(first) == 0 {
		t.Fatal("AllCreatedBy() returned zero members — nothing to mutate, and the length assertion above should already have failed")
	}
	first[0] = CreatedBy("mutated")

	second := AllCreatedBy()
	for _, c := range second {
		if c == CreatedBy("mutated") {
			t.Fatal("AllCreatedBy() shares backing storage across calls — mutating one call's result changed another's")
		}
	}
}

// TestParseCreatedBy_RoundTripsEveryMember proves ParseCreatedBy(string(c))
// == c for every c in AllCreatedBy() — the round-trip R4.7 requires.
func TestParseCreatedBy_RoundTripsEveryMember(t *testing.T) {
	for _, want := range AllCreatedBy() {
		got, err := ParseCreatedBy(string(want))
		if err != nil {
			t.Errorf("ParseCreatedBy(%q) returned error %v, want nil", want, err)
			continue
		}
		if got != want {
			t.Errorf("ParseCreatedBy(%q) = %q, want %q", want, got, want)
		}
	}
}

// TestParseCreatedBy_RejectsUnknownText proves ParseCreatedBy returns
// ErrUnknownCreatedBy, naming the rejected value, for text that names no
// AllCreatedBy() member.
func TestParseCreatedBy_RejectsUnknownText(t *testing.T) {
	_, err := ParseCreatedBy("not-a-created-by")
	if !errors.Is(err, ErrUnknownCreatedBy) {
		t.Fatalf("ParseCreatedBy(%q) error = %v, want errors.Is(_, ErrUnknownCreatedBy)", "not-a-created-by", err)
	}
}

// TestCreatedBy_IsStringKind proves CreatedBy is a defined string type, not
// an int enum — unit.Status's own reasoning: it prints as itself and binds
// to the relations.created_by TEXT column with no conversion table.
func TestCreatedBy_IsStringKind(t *testing.T) {
	var zero CreatedBy
	if reflect.TypeOf(zero).Kind() != reflect.String {
		t.Errorf("CreatedBy has kind %s, want %s", reflect.TypeOf(zero).Kind(), reflect.String)
	}
}
