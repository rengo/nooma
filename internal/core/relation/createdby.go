package relation

import "fmt"

// CreatedBy is who proposed a relation — doc 02 §4's three-value vocabulary,
// design.md §5.3/§6.10: `connect` (feat/core-consolidation-connect-derive)
// must plan a relation with created_by='consolidation', and today the
// column's vocabulary exists only as a SQL comment
// (0001_core_tables.sql:37) — no Go package declares it, and
// brain/capture.go:485 writes the bare literal "system". Declaring it here
// puts the closed vocabulary where relation.Outcome and relation.Verdict
// already live; adopting it at capture.go:485 is m2c's one-line change
// (design.md §5.3, §8).
type CreatedBy string

// The three CreatedBy vocabulary members, doc 02 §4. Order matches
// migration 0001's relations.created_by column comment
// ("system|consolidation|user") — TestRelationCreatedByDDLMatchesAllCreatedBy
// pins the two together.
const (
	CreatedBySystem        CreatedBy = "system"
	CreatedByConsolidation CreatedBy = "consolidation"
	CreatedByUser          CreatedBy = "user"
)

// ErrUnknownCreatedBy is returned by ParseCreatedBy when s does not name a
// member of AllCreatedBy().
var ErrUnknownCreatedBy = fmt.Errorf("relation: unknown created_by")

// AllCreatedBy returns a fresh slice holding the three CreatedBy vocabulary
// members, in the order the constants above declare them —
// unit.AllStatuses's own house pattern: a function, not an exported var, so
// a caller mutating one call's result cannot affect another.
func AllCreatedBy() []CreatedBy {
	return []CreatedBy{CreatedBySystem, CreatedByConsolidation, CreatedByUser}
}

// ParseCreatedBy is the sole entry point from untrusted text into the
// CreatedBy vocabulary. It returns ErrUnknownCreatedBy, naming the rejected
// value, for anything that is not one of AllCreatedBy()'s members.
func ParseCreatedBy(s string) (CreatedBy, error) {
	for _, want := range AllCreatedBy() {
		if s == string(want) {
			return want, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownCreatedBy, s)
}
