// Package unit defines the atom of memory: types, status and transitions.
//
// See docs/06-harness.md §1 for the dependency rule.
//
// Pending conformance anchor (design.md §8.5, openspec/changes/complete-harness/
// design.md — residual risk R9): test/conformance/i01_focus_never_persisted_test.go
// (build tag pendingimpl) anchors invariant I01 to the not-yet-existing
// unit.Status / unit.AllStatuses. Whoever adds either symbol here MUST, in
// the SAME PR, promote that test into the untagged L2 suite and remove its
// two lines from test/conformance/pending_symbols.txt — otherwise
// scripts/pending-red.sh (`make pending-red`) fails, naming the symbol that
// no longer reports as undefined. This comment mitigates, but does not
// eliminate, R9: a symbol added under a DIFFERENT name (e.g. unit.Kind)
// leaves the pending-red gate silently green and the test never promoted.
package unit
