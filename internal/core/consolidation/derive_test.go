package consolidation

import (
	"testing"

	"github.com/rengo/nooma/internal/core/selfmodel"
)

// TestDeriveTopicKey_RendersDerivedFormatForEveryFacet proves R4.6:
// DeriveTopicKey renders doc 02 §10's derived key format,
// "derived/{facet}/{key}", for every member of selfmodel.AllFacets() —
// driven by the vocabulary itself, so a sixth facet added later is
// exercised automatically rather than requiring a new case here.
func TestDeriveTopicKey_RendersDerivedFormatForEveryFacet(t *testing.T) {
	facets := selfmodel.AllFacets()
	if len(facets) == 0 {
		t.Fatal("selfmodel.AllFacets() returned zero members — nothing to drive this test")
	}

	for _, f := range facets {
		got := DeriveTopicKey(f, "example-key")
		want := "derived/" + string(f) + "/example-key"
		if got != want {
			t.Errorf("DeriveTopicKey(%q, %q) = %q, want %q", f, "example-key", got, want)
		}
	}
}
