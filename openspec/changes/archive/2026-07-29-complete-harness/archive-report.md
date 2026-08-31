# Archive report — `complete-harness` (retroactive)

**Written 2026-08-30, long after the change closed.** No archive report was written at the time.
The convention of writing one started with M3: `2026-08-22-m3a-prospection` is the first archived
change that carries its own report. Everything below is reconstructed from evidence present in the
repository today, and every claim that could not be verified after the fact is named as such.

## What the change was

Completing [`docs/06-harness.md`](../../../../docs/06-harness.md) §9 step 4: making every rule the
harness declares executable by a machine rather than stated in prose. Embedded migrations producing
exactly the schema of `docs/03-data-model.md`, a schema golden that stops drift, the four test
levels wired with their build tags, the golden-set formats defined, and the first conformance
tests.

## Evidence that it closed

- `tasks.md`: **49 of 49** task boxes checked.
- First commit touching the folder: `ba1a864` (2026-07-28), *"docs(sdd): plan the complete-harness
  change"*. Last: `47422fc` (2026-07-29), *"docs(sdd): record PR 7's four-lens review remediation
  and correct its claims"*.
- The harness it built is still load-bearing today: `make check` and `make check-all` are the two
  documented loops in `CLAUDE.md`, the schema-golden regeneration-diff check and the four test
  levels are all gates `check-all` runs, and `test/conformance/` is populated.

## Archive date

Named `2026-07-29-complete-harness` after the date of the last commit that touched the folder. That
rule is applied uniformly across the ten changes archived retroactively in this pass.

## What this report cannot verify

- **Merge-time CI state.** Whether every PR in this change merged with all required checks green is
  a rollup read today, not a record from merge time. No check can prove after the fact that none
  was made non-required or overridden.
- **Findings and rulings.** Whatever review findings this change produced live in its own
  `tasks.md` annotations, not here. This report does not summarise them, because summarising a
  record written by someone else a month ago would be invention, not archiving.
