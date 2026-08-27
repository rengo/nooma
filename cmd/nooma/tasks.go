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
// "chat" joined the list with ADR-0021, which gave it its first caller:
// until then it was a documented task the wizard could bind and doctor
// could report, that no code ever asked a provider for. Adding it here is
// the whole wiring change — the wizard binds it and doctor reports it
// because both read this slice, which is the property D18a was built for.
//
// **A vault configured before that ADR does not bind it, and will not
// serve until it does.** That is the loud failure this list exists to
// produce: the alternative is a brain whose conversational half is
// silently absent, which is the same shape as the permanently-unembedded
// vault in the paragraph above. `nooma doctor` names the unbound task, and
// `nooma init` binds it.
var tasksM1Consumes = []string{"capture_processing", "relation_evaluation", "chat", "embedding"}

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

// tasksTheBinaryRuns is the union of the two lists above, in first-seen
// order, and it is what a vault must bind to be fully usable.
//
// Both lists are already read live by their own consumers — that is design
// D18a, and neither was wrong. What nobody owned was the gap BETWEEN them:
// `nooma init` bound M1's three, the scheduler needed belief_derivation,
// and the wizard wrote vaults whose sleep phase could not start while
// `nooma doctor` reported "ok task coverage" over them.
//
// A function rather than a var, for the reason every AllX() in this
// repository is one: a package-level slice is mutable by any caller, and a
// mutated union would defeat the coverage check that reads it. Derived
// rather than written out, so a task added to either list reaches the
// wizard and doctor with no second edit — the same property joinVocabulary
// gives the prompt.
func tasksTheBinaryRuns() []string {
	size := len(tasksM1Consumes) + len(tasksConsolidateConsumes)
	seen := make(map[string]bool, size)
	union := make([]string, 0, size)
	for _, list := range [][]string{tasksM1Consumes, tasksConsolidateConsumes} {
		for _, task := range list {
			if seen[task] {
				continue
			}
			seen[task] = true
			union = append(union, task)
		}
	}
	return union
}
