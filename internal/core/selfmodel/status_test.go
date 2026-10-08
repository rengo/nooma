package selfmodel

import (
	"reflect"
	"testing"
)

// TestAllStatuses_HasExactlyTheDoc03Members pins the closed two-member
// vocabulary, in declaration order. Doc 03's column comment is pinned to
// the same list by TestBeliefStatusDocMatchesAllStatuses (conformance).
func TestAllStatuses_HasExactlyTheDoc03Members(t *testing.T) {
	want := []Status{StatusActive, StatusRetired}
	if got := AllStatuses(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllStatuses() = %v, want %v", got, want)
	}
	if string(StatusActive) != "active" || string(StatusRetired) != "retired" {
		t.Errorf("status spellings = %q, %q, want active, retired", StatusActive, StatusRetired)
	}
}

// TestAllOrigins_HasExactlyTheDoc03Members pins doc 03's
// seed|derived|user_stated, in that order.
func TestAllOrigins_HasExactlyTheDoc03Members(t *testing.T) {
	want := []Origin{OriginSeed, OriginDerived, OriginUserStated}
	if got := AllOrigins(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllOrigins() = %v, want %v", got, want)
	}
	if string(OriginSeed) != "seed" || string(OriginDerived) != "derived" || string(OriginUserStated) != "user_stated" {
		t.Errorf("origin spellings = %q, %q, %q, want seed, derived, user_stated",
			OriginSeed, OriginDerived, OriginUserStated)
	}
}

// TestAllStatusesAndOrigins_ReturnFreshSlices proves the house pattern
// (AllFacets's own): mutating one call's result must not affect the next.
func TestAllStatusesAndOrigins_ReturnFreshSlices(t *testing.T) {
	statuses := AllStatuses()
	statuses[0] = Status("mutated")
	if got := AllStatuses()[0]; got != StatusActive {
		t.Errorf("AllStatuses()[0] = %q after mutating a previous result, want %q", got, StatusActive)
	}

	origins := AllOrigins()
	origins[0] = Origin("mutated")
	if got := AllOrigins()[0]; got != OriginSeed {
		t.Errorf("AllOrigins()[0] = %q after mutating a previous result, want %q", got, OriginSeed)
	}
}
