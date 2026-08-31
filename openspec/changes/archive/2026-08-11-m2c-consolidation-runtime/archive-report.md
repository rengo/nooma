# Archive report — `m2c-consolidation-runtime` (retroactive)

**Written 2026-08-30, long after the change closed.** No archive report was written at the time;
the convention started with M3. Everything below is reconstructed from evidence present in the
repository today.

## What the change was

The third of the four chained changes splitting `m2-sleep-weight`. The **runtime half** of the
consolidation: wiring the pure phase logic `m2b` had built into something that actually runs
against a vault, writes its `decision_log` rows, and can be invoked one phase at a time.

At **175 task boxes** it is the largest single change in the ten archived in this pass — larger
than any M1 phase.

## Evidence that it closed

- `tasks.md`: **175 of 175** task boxes checked.
- First commit: `1515959` (2026-08-09), *"docs(m2c): delta spec for the consolidation runtime"*.
  Last: `c99cb34` (2026-08-11), *"docs(tasks): run m2c's four chain-closing checks, record what
  they found"* — the chain-closing checks were run and their findings written down, which is the
  closest thing this change has to a discharge.
- The umbrella's discharge (2026-08-19, PR #190) verified M2's success criteria against the code at
  `074033a`, after this phase and `m2d` had both landed.

## Archive date

Named `2026-08-11-m2c-consolidation-runtime` after the date of the last commit that touched the
folder.

## What this report cannot verify

- **Merge-time CI state**, for the same reason as every change in this pass.
- **What the four chain-closing checks found.** `c99cb34` records them inside the change's own
  `tasks.md`. They are not restated here: paraphrasing a finding written three weeks ago, by a pass
  with context this one does not have, would degrade the record rather than preserve it.
