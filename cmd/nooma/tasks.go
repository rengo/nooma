package main

// tasksM1Consumes names every task this binary resolves to a provider at
// startup — design D18a's own "one list, three readers" answer to a gap
// that survived M1 Phase B's planning entirely: nothing anywhere connected
// "which tasks does this binary actually ask a provider for" to "which
// tasks are configured", and a permanently-unembedded Cloud vault degraded
// silently instead of failing loudly (tasks.md's own C1/m1b-pipeline C9
// history).
//
// This is the single source for three readers, none of which restates its
// own copy: runServe's wiring (this PR, serve.go) resolves exactly these
// into CaptureService's and RecallService's ports; `nooma init`'s wizard
// (15) binds exactly these; `nooma doctor`'s coverage check (16b) reports
// exactly these. A future milestone that starts consuming, say,
// `belief_derivation` adds one string here — the wizard that must bind it
// and the check that must report it both move with it, because both read
// this same slice rather than restating their own. The gap becomes hard to
// represent rather than merely detectable, which is D18a's own strongest
// available form of this guard (the same shape D10's guarded-route slice
// already applies to HTTP routes, applied here to configuration).
//
// Every member is one of config.DocumentedTaskNames — TestTasksM1ConsumesAreAllDocumented
// (tasks_test.go) pins it.
var tasksM1Consumes = []string{"capture_processing", "relation_evaluation", "embedding"}

// tasksConsolidateConsumes are the three tasks one consolidation pass
// needs bound — design §7.2 (m2c-consolidation-runtime). capture_processing
// is deliberately absent: no consolidation phase classifies. Every member
// is one of config.DocumentedTaskNames — belief_derivation was already
// documented before this change, so no config-vocabulary edit was needed
// for it. This PR adds no dedicated membership test the way
// TestTasksM1ConsumesAreAllDocumented pins tasksM1Consumes above (that
// would be a tasks_test.go edit, outside this PR's own diff scope,
// test/e2e/consolidate_e2e_test.go covers the two required behaviors —
// resolution and refusal — end to end instead: TestConsolidate_WholePass
// and TestConsolidate_RefusesUnboundTaskBeforeTheLock).
//
// runConsolidate (consolidate.go) reads this list directly for its
// pre-lock refusal, and wiring.go's resolveConsolidateProviders reads the
// identical slice for the real provider resolution after the lock — one
// list, two readers, the same D18a shape tasksM1Consumes already
// establishes above.
var tasksConsolidateConsumes = []string{"relation_evaluation", "belief_derivation", "embedding"}
