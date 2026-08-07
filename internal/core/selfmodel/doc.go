// Package selfmodel holds belief and current-state decision logic.
//
// Facet is the self-model's closed vocabulary axis (docs/02-cognitive-core.md
// §10): identity, value, goal, social, preference. AllFacets/ParseFacet
// follow this repository's house pattern — the same shape unit.Status and
// relation.CreatedBy already use: a function returning a fresh slice, never
// a mutable exported var, plus a single ParseFacet entry point from
// untrusted text that returns ErrUnknownFacet naming the rejected value.
//
// The stagnation watcher (feat/core-consolidation-pattern-eval) selects
// FacetGoal beliefs specifically — a caller passes the enum value, not a
// free-form string, so "is this belief a goal" is a comparison, never a
// remembered convention (design.md §5.3).
//
// See docs/06-harness.md §1 for the dependency rule.
package selfmodel
