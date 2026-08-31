# Archive report — `m2b-consolidation-core` (retroactive)

**Written 2026-08-30, long after the change closed.** No archive report was written at the time;
the convention started with M3. Everything below is reconstructed from evidence present in the
repository today.

## What the change was

The second of the four chained changes splitting `m2-sleep-weight`. The **pure half** of the
consolidation: the eight-phase sequence of doc 02 §6 in order (invariant I11), and the decision
logic each phase runs, with the purity and calibration requirements of `internal/core` binding
across all five of its PRs. Its `design.md` ran *before* the spec on this change and is
authoritative for the identifiers the spec names.

## Evidence that it closed

- `tasks.md`: **76 of 76** task boxes checked.
- First commit: `b67a7f6` (2026-08-06), *"docs(m2b): technical design for the consolidation
  core"*. Last: `a735da3` (2026-08-07), **`docs(archive): close m2b-consolidation-core`** — the
  change was explicitly closed at the time. Only the folder move never happened, which is precisely
  the gap this pass repairs.
- The umbrella's discharge (2026-08-19, PR #190) verified M2's success criteria against the code at
  `074033a`, which covers what this phase delivered.

## Archive date

Named `2026-08-07-m2b-consolidation-core` after the date of the last commit that touched the
folder — the commit that says "close" in its own subject.

## What this report cannot verify

Merge-time CI state. Its review findings live in its own `tasks.md` annotations and are not
restated here.
