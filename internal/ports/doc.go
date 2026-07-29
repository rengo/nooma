// Package ports declares the interfaces the cognitive core and the brain depend on.
//
// Everything the core cannot do for itself — reading time, generating identifiers,
// reaching storage, calling a model, speaking to a channel — enters through a port
// declared here and is implemented by an adapter outside internal/core.
//
// See docs/06-harness.md §1 for the dependency rule and §2 for the clock.
//
// Pending conformance anchor (design.md §8.5, openspec/changes/complete-harness/
// design.md — residual risk R9): test/conformance/i03_units_never_deleted_test.go
// (build tag pendingimpl) anchors invariant I03 to the not-yet-existing
// ports.UnitRepo. Whoever adds that symbol here MUST, in the SAME PR,
// promote that test into the untagged L2 suite and remove its line from
// test/conformance/pending_symbols.txt — otherwise scripts/pending-red.sh
// (`make pending-red`) fails, naming the symbol that no longer reports as
// undefined. This comment mitigates, but does not eliminate, R9: a symbol
// added under a DIFFERENT name leaves the pending-red gate silently green
// and the test never promoted.
package ports
