package consolidation

import (
	"strings"
	"testing"

	"github.com/rengo/nooma/internal/core/selfmodel"
	"github.com/rengo/nooma/internal/core/unit"
)

// TestBuildDerivePrompt_IsDeterministic proves BuildDerivePrompt is pure:
// the same arguments produce the byte-identical string across repeated
// calls, with no hidden clock, randomness, or map-iteration order leaking
// in (nooma-core's own purity gate — CLAUDE.md, docs/06-harness.md §1).
func TestBuildDerivePrompt_IsDeterministic(t *testing.T) {
	sources := []DeriveSource{
		{UnitID: "unit-1", Type: unit.TypeKnowledge, Content: "The user prefers dark mode."},
		{UnitID: "unit-2", Type: unit.TypeKnowledge, Content: "The user works remotely."},
		{UnitID: "unit-3", Type: unit.TypeKnowledge, Content: "The user's timezone is UTC-3."},
	}
	existing := []Belief{
		{ID: "belief-1", Facet: selfmodel.FacetIdentity, TopicKey: "derived/identity/role", Content: "Works as a backend engineer."},
		{ID: "belief-2", Facet: selfmodel.FacetPreference, TopicKey: "derived/preference/ui-theme", Content: "Prefers dark themes."},
		{ID: "belief-3", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/learn-go", Content: "Wants to learn Go this year."},
	}

	first := BuildDerivePrompt(sources, existing)
	second := BuildDerivePrompt(sources, existing)

	if first != second {
		t.Fatalf("BuildDerivePrompt is not deterministic:\nfirst:  %q\nsecond: %q", first, second)
	}
}

// TestBuildDerivePrompt_RendersEveryUnitAndEveryExistingBelief proves spec
// R5.6's first MUST: the prompt includes every source unit's content and
// every existing active belief's topic_key/content, so the judge can
// decide "this already exists" before proposing a new belief (dedup
// defense 1, doc 02 §6.5's named gap). At least three of each so a
// truncated or misordered render would be falsifiable.
func TestBuildDerivePrompt_RendersEveryUnitAndEveryExistingBelief(t *testing.T) {
	sources := []DeriveSource{
		{UnitID: "unit-1", Type: unit.TypeKnowledge, Content: "The user prefers dark mode."},
		{UnitID: "unit-2", Type: unit.TypeKnowledge, Content: "The user works remotely."},
		{UnitID: "unit-3", Type: unit.TypeKnowledge, Content: "The user's timezone is UTC-3."},
	}
	existing := []Belief{
		{ID: "belief-1", Facet: selfmodel.FacetIdentity, TopicKey: "derived/identity/role", Content: "Works as a backend engineer."},
		{ID: "belief-2", Facet: selfmodel.FacetPreference, TopicKey: "derived/preference/ui-theme", Content: "Prefers dark themes."},
		{ID: "belief-3", Facet: selfmodel.FacetGoal, TopicKey: "derived/goal/learn-go", Content: "Wants to learn Go this year."},
	}

	got := BuildDerivePrompt(sources, existing)

	for _, s := range sources {
		if !strings.Contains(got, s.Content) {
			t.Errorf("BuildDerivePrompt output missing source unit content %q\nfull output:\n%s", s.Content, got)
		}
	}
	for _, b := range existing {
		if !strings.Contains(got, b.TopicKey) {
			t.Errorf("BuildDerivePrompt output missing existing belief topic_key %q\nfull output:\n%s", b.TopicKey, got)
		}
		if !strings.Contains(got, b.Content) {
			t.Errorf("BuildDerivePrompt output missing existing belief content %q\nfull output:\n%s", b.Content, got)
		}
	}
}

// TestBuildDerivePrompt_EmptyExistingStillNamesTheEmptyStatePlainly proves
// spec R5.6's second MUST: when there are no active beliefs yet (a fresh
// vault), the prompt still sends every source unit's content AND states
// the absence of existing beliefs plainly, rather than omitting the
// section or sending something malformed — the empty state is itself
// informative to the judge, not a degenerate case to hide.
func TestBuildDerivePrompt_EmptyExistingStillNamesTheEmptyStatePlainly(t *testing.T) {
	sources := []DeriveSource{
		{UnitID: "unit-1", Type: unit.TypeKnowledge, Content: "The user prefers dark mode."},
		{UnitID: "unit-2", Type: unit.TypeKnowledge, Content: "The user works remotely."},
		{UnitID: "unit-3", Type: unit.TypeKnowledge, Content: "The user's timezone is UTC-3."},
	}

	got := BuildDerivePrompt(sources, nil)

	for _, s := range sources {
		if !strings.Contains(got, s.Content) {
			t.Errorf("BuildDerivePrompt output missing source unit content %q with empty existing\nfull output:\n%s", s.Content, got)
		}
	}
	if !strings.Contains(got, "no existing self-beliefs") {
		t.Errorf("BuildDerivePrompt with empty existing did not name the empty state plainly\nfull output:\n%s", got)
	}
}
