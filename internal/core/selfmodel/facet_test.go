package selfmodel

import (
	"errors"
	"reflect"
	"testing"
)

// TestAllFacets_HasExactlyTheDoc02Members proves R4.7: AllFacets returns
// exactly the five doc 02 §10 members as a fresh slice.
func TestAllFacets_HasExactlyTheDoc02Members(t *testing.T) {
	want := map[Facet]bool{
		FacetIdentity:   true,
		FacetValue:      true,
		FacetGoal:       true,
		FacetSocial:     true,
		FacetPreference: true,
	}

	got := AllFacets()
	if len(got) != len(want) {
		t.Fatalf("AllFacets() has %d members, want %d: %v", len(got), len(want), got)
	}

	seen := make(map[Facet]bool, len(got))
	for _, f := range got {
		if !want[f] {
			t.Errorf("AllFacets() includes %q, which is not a doc 02 §10 member", f)
		}
		if seen[f] {
			t.Errorf("AllFacets() lists %q more than once", f)
		}
		seen[f] = true
	}
}

// TestAllFacets_ReturnsAFreshSliceEachCall proves the house pattern:
// mutating one call's result must not affect the next.
func TestAllFacets_ReturnsAFreshSliceEachCall(t *testing.T) {
	first := AllFacets()
	if len(first) == 0 {
		t.Fatal("AllFacets() returned zero members — nothing to mutate, and the length assertion above should already have failed")
	}
	first[0] = Facet("mutated")

	second := AllFacets()
	for _, f := range second {
		if f == Facet("mutated") {
			t.Fatal("AllFacets() shares backing storage across calls — mutating one call's result changed another's")
		}
	}
}

// TestParseFacet_RoundTripsEveryMember proves ParseFacet(string(f)) == f
// for every f in AllFacets() — the round-trip R4.7 requires.
func TestParseFacet_RoundTripsEveryMember(t *testing.T) {
	for _, want := range AllFacets() {
		got, err := ParseFacet(string(want))
		if err != nil {
			t.Errorf("ParseFacet(%q) returned error %v, want nil", want, err)
			continue
		}
		if got != want {
			t.Errorf("ParseFacet(%q) = %q, want %q", want, got, want)
		}
	}
}

// TestParseFacet_RejectsUnknownText proves ParseFacet returns
// ErrUnknownFacet, naming the rejected value, for text that names no
// AllFacets() member.
func TestParseFacet_RejectsUnknownText(t *testing.T) {
	_, err := ParseFacet("not-a-facet")
	if !errors.Is(err, ErrUnknownFacet) {
		t.Fatalf("ParseFacet(%q) error = %v, want errors.Is(_, ErrUnknownFacet)", "not-a-facet", err)
	}
}

// TestFacet_IsStringKind proves Facet is a defined string type, not an int
// enum — unit.Status's own reasoning: it prints as itself and binds to the
// self_beliefs.facet TEXT column with no conversion table.
func TestFacet_IsStringKind(t *testing.T) {
	var zero Facet
	if reflect.TypeOf(zero).Kind() != reflect.String {
		t.Errorf("Facet has kind %s, want %s", reflect.TypeOf(zero).Kind(), reflect.String)
	}
}
