# ADR-0026 — The judge answers about what it was shown

- **Status**: Accepted
- **Date**: 2026-08-31
- **Supersedes**: —
- **Superseded by**: —
- **Enables**: M3e

## Context

Two places in this codebase render a candidate list into a relation-judge prompt and then read a
target unit ID back out of the answer. **Nothing made those two agree.**

| Call site | Candidates rendered | What it did with `target_unit_id` |
|---|---|---|
| `brain.judgeRelation` (capture, every message) | at most `relation.DedupCandidateK` | wrote it into `relations.to_unit_id` |
| `brain.judgeAndPersistPair` (nightly `connect`) | **exactly one** | passed it to `consolidation.ProposeRelation`, which validated only that it was non-`nil` |

This is `m2d`'s own recorded finding, carried out of M2 and named in
[`05-build-plan.md`](../05-build-plan.md) §M2: *"`consolidation.ProposeRelation` trusts the
judge's own `TargetUnitID` without cross-checking it against the candidate the search returned."*
Following it into the code found the same defect on the capture path, which the finding did not
name.

**The database catches less than it looks like it does.** `relations.to_unit_id REFERENCES
units(id)` and the vault opens with `foreign_keys=on`, so an invented ID is refused on write. That
covers exactly one of the three shapes a wrong answer takes:

| The judge answers | Foreign key | What happened |
|---|---|---|
| An ID no unit has | rejects | The error propagates out of `judgeRelation` and **fails the whole capture** — for a message whose unit is already stored. The user is told the capture failed; the vault says otherwise |
| A real ID that was never a candidate | accepts | An edge appears between two units the judge never compared. Silent |
| The source unit itself | accepts | A unit related to itself. `ConnectPairs` excludes the source from its own candidate list; the answer was never filtered the same way. Silent |

The two silent cases are the reason this is not merely a robustness nicety. A false edge is not
inert: `connect`'s own quality metric is *"how often does the user delete relations from the
nightly job?"* (§4), and an edge nobody can explain is one the user deletes, which teaches the
learning module the wrong thing about a judgment that was never made.

## Options evaluated

| Option | Real tradeoff |
|---|---|
| Rely on the foreign key | Free, and already there. Catches only the invented ID — the one case that was already loud — and misses both silent ones |
| Validate in each caller | Fixes both paths today. The defect got here by two callers doing the same thing slightly differently; a third would repeat it |
| **A pure predicate in `internal/core`, used by both the core decision and the callers** ✅ | One rule, one place. `ProposeRelation` refuses structurally so a future caller cannot persist one by forgetting; the callers ask the same question separately to record the row. Costs one parameter on `ProposeRelation` and one guard on the capture path |

## Decision

**A judgment whose target is not among the candidates the judge was shown stores no relation.**

`relation.TargetOffered(target string, offered []string) bool` is the rule, and it lives in
`internal/core/relation` — pure, total, exact. The empty target is not offered by an empty
candidate: a judge that answered nothing and a candidate list that carried nothing are two
separate faults, and matching them against each other would turn both into a persisted edge.
Comparison is exact, because unit IDs are opaque and a predicate that folded case or matched
prefixes would accept `unit-1` for `unit-10`.

It is applied twice, deliberately:

- `consolidation.ProposeRelation` takes the offered IDs and refuses. This is the **structural**
  guarantee, and it is what makes a third call site safe by default.
- Both brain callers ask the same predicate before the threshold read, and **record** the
  refusal. `ProposeRelation` is pure and cannot; refusing is all it can do.

**The refusal is recorded, and this diverges from the rule beside it.** §4 already says a judgment
that decided nothing writes nothing — no relation, no `decision_log` row. This one writes a row:
`relation.target_unknown` on the capture path, `consolidate.connect.target_unknown` in the nightly
pass. The `Context` carries both halves, what was offered and what came back.

Connect having a second action is not a reversal of `m2d` design §7.1's *"no second action for a
discard, unlike capture's own `ActionRelationDiscarded`"*. That divergence is about judgments the
thresholds rejected. This is not one.

## Consequences

### What it enables

- The two silent shapes stop being silent. A model naming a unit it was never given now appears in
  the vault where every defect this project has fixed since M3 was found — by reading the
  `decision_log` of a real vault, not by a test.
- A capture is no longer failed by a judge's bad ID. The unit is stored, the judgment is refused
  and explained, and the person gets their reply.
- The rule is one function. A third judge call site inherits the structural half by calling
  `ProposeRelation` at all.

### What it costs

- One more parameter on `ProposeRelation`, threaded from each caller — a caller that passes the
  wrong list silently rejects good judgments. `TestProposeRelation_OfferedTargetStillWrites` is
  the control against the version of this that refuses everything.
- A new `decision_log` action in each path, and the vocabulary to maintain with them.
- The predicate is called twice per judgment on both paths. It is a linear scan over at most
  `DedupCandidateK` strings, and the alternative — returning a typed refusal reason from
  `ProposeRelation` — would have reshaped a function with fourteen existing call sites in its
  tests to carry information only one caller uses.

### Reversal criteria

Evidence that the offered list is the wrong thing to check against: a judge call site that
legitimately needs to name a unit outside its own prompt — a pass that reasons over the whole
graph rather than a rendered candidate list would be the honest example. The ADR that supersedes
this one should move the check to whatever that pass's real admissible set is, not delete it.
