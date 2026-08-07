// Package consolidation holds the decision logic of the eight nightly
// phases (docs/02-cognitive-core.md §6), in the order Phase's own
// numbering fixes (I11):
//
//	expire_incomplete -> archive -> strengthen -> connect -> derive ->
//	reweight -> pattern_eval -> learn
//
// Each phase is a pure function over data the caller already holds, plus
// the pass's single `now` where an instant matters — never a repository,
// a context, a clock, or a provider. A phase's job is to compute a plan
// (a transition, a strength change, a boost, a proposed relation, a merge
// decision, a finding); persisting that plan is brain's job, not this
// package's.
//
// PhaseLearn has no corresponding function. Ruling 3 ships `learn` as a
// true no-op that still occupies slot eight, and that no-op is the
// absence of a decision function — not a vacuous func Learn() {} with
// nothing worth testing. A future runner's arm for PhaseLearn performs no
// work and writes no decision_log row (docs/02-cognitive-core.md §6.8);
// M5 is what fills the slot.
//
// This PR (feat/core-consolidation-order) ships only Phase and its
// vocabulary — no decision function, no calibrated number, and no §13
// row: the first calibrated constant (IncompleteExpiryHours) arrives in
// feat/core-consolidation-expire-archive.
//
// See docs/06-harness.md §1 for the dependency rule.
package consolidation
