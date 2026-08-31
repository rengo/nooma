# Archive report — `m1b-pipeline` (retroactive)

**Written 2026-08-30, long after the change closed.** No archive report was written at the time;
the convention started with M3. Everything below is reconstructed from evidence present in the
repository today.

## What the change was

The second of the three chained changes splitting `m1-capture-recall`. The capture pipeline:
`internal/core/classify` and the thirteen-value taxonomy doc 02 §5 names, the dedup and relation
judges, and the path from a captured sentence to a stored unit.

Its spec opens by recording **two contradictions it surfaced** while being written, and a
deliberate narrowing *"recorded rather than silently applied"* (§0). That is the artifact of a spec
that was allowed to disagree with its own umbrella.

## Evidence that it closed

- `tasks.md`: **74 of 74** task boxes checked.
- First commit: `e3e85f6` (2026-07-31), *"docs(m1b): the Phase B spec, and two contradictions it
  surfaced"*. Last: `1e5fe71` (2026-08-02), *"fix(brain): a judge outage degrades the capture
  instead of refusing it"*.
- What it built is still the live path: `internal/core/classify` is the package the entire post-M3
  run of fixes (ADR-0021 through ADR-0024) has been correcting against real model behaviour.

## Archive date

Named `2026-08-02-m1b-pipeline` after the date of the last commit that touched the folder.

## What this report cannot verify

Merge-time CI state. Its review findings live in its own `tasks.md` annotations and are not
restated here.
