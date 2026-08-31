# Archive report — `m2-sleep-weight` (retroactive)

**Written 2026-08-30, long after the change closed.** No archive report was written at the time;
the convention started with M3. This one is the shortest of the ten, because **it is the only
change in this pass that discharged itself.**

## What the change was

The **umbrella proposal** for M2 — sleep and weight: `effective_weight` and the decay model,
priority and the two focuses with anti-jitter hysteresis, the eight-phase nightly consolidation of
`docs/02-cognitive-core.md` §6 with each phase individually invocable, and the in-process scheduler
per ADR-0009.

Planned as **four chained changes**, whose spec, design and tasks live in their own folders:

- `m2a-weight-focus`, `m2b-consolidation-core`, `m2c-consolidation-runtime` — archived in this same
  pass
- `m2d-scheduler-demo` — archived at the time, at `2026-08-19-m2d-scheduler-demo`

## Evidence that it closed — and where the real record lives

**`proposal.md` §"Discharge — 2026-08-19, PR #190" is the record.** It is not superseded by this
report and should be read instead of it. It states that M2 closed with the eleventh of eleven PRs
(#180–#190, `074033a`), that every success criterion was verified *against the code at that commit
rather than inferred from the four sub-changes' own task lists*, and it then names **five things
that are true more narrowly than the proposal's own wording implies** — including that
`weight.Revive` has no production caller, that the demo runs the service in-process rather than the
`nooma consolidate` binary, and that no guard enforces the "no test touches the network or a real
LLM" rule.

That is what an honest discharge looks like, and it is why the other nine reports in this pass are
explicit about what they cannot verify.

## Still open, carried out of M2 and true today

The discharge's last paragraph records that **`consolidation.ProposeRelation` trusts the judge's
own `TargetUnitID` without cross-checking it against the candidate the search returned**. Verified
still open on 2026-08-30: `internal/core/consolidation/connect.go:161` checks only that the field is
non-`nil`. It remains its own future work unit, and `docs/05-build-plan.md` §M2 names it too.

## Archive date

Named `2026-08-19-m2-sleep-weight` after the date of the last commit that touched the folder,
`8d2a428` — *"docs(m2): discharge M2's success criteria against the code, not the task lists"* —
which is the discharge itself.

## What this report cannot verify

Nothing beyond what the discharge already declares. Its own point 5 says it plainly: CI history is
a rollup read, not a merge-time record.
