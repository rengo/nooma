package selfmodel

import "fmt"

// Facet is the self-model's closed vocabulary axis — doc 02 §10 names five
// values. It is a defined string type, not an int enum: relation.CreatedBy
// and unit.Status's own reasoning applies here too — it prints as itself
// in an error, a log line and a decision_log row, and binds to the
// self_beliefs.facet TEXT column with no conversion table.
type Facet string

// The five Facet vocabulary members, doc 02 §10.
const (
	FacetIdentity   Facet = "identity"
	FacetValue      Facet = "value"
	FacetGoal       Facet = "goal"
	FacetSocial     Facet = "social"
	FacetPreference Facet = "preference"
)

// ErrUnknownFacet is returned by ParseFacet when s does not name a member
// of AllFacets().
var ErrUnknownFacet = fmt.Errorf("selfmodel: unknown facet")

// AllFacets returns a fresh slice holding the five Facet vocabulary
// members, in the order the constants above declare them —
// relation.AllCreatedBy's own house pattern: a function, not an exported
// var, so a caller mutating one call's result cannot affect another.
func AllFacets() []Facet {
	return []Facet{FacetIdentity, FacetValue, FacetGoal, FacetSocial, FacetPreference}
}

// ParseFacet is the sole entry point from untrusted text into the Facet
// vocabulary. It returns ErrUnknownFacet, naming the rejected value, for
// anything that is not one of AllFacets()'s members.
func ParseFacet(s string) (Facet, error) {
	for _, want := range AllFacets() {
		if s == string(want) {
			return want, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownFacet, s)
}
