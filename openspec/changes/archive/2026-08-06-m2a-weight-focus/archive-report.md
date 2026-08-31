# Archive report — `m2a-weight-focus` (retroactive)

**Written 2026-08-30, long after the change closed.** No archive report was written at the time;
the convention started with M3. Everything below is reconstructed from evidence present in the
repository today.

## What the change was

The first of the four chained changes splitting `m2-sleep-weight` (owner ruling round 2 #6,
2026-08-04). `internal/core/weight` — effective weight and the Ebbinghaus decay curve exactly as
doc 02 states it — plus priority and the two focuses with their anti-jitter hysteresis. Pure
functions, heavily tested, as the build plan demanded of M2.

## Evidence that it closed

- `tasks.md`: **55 of 55** task boxes checked.
- First commit: `2bd5735` (2026-08-04), *"plan(m2): track M2's planning artifacts, as m1a/m1b/m1c
  already are"*. Last: `676485b` (2026-08-06), *"docs(archive): annotate design.md's stale PR cell
  as m2a closes"* — the change annotating its own staleness on the way out.
- Its umbrella's discharge (2026-08-19, PR #190) verified M2's criteria against the code, which
  covers what this phase delivered.

## What its own record already carries

`tasks.md` holds a run of Judgment Day findings closed in place — **C22** (`Priority`'s `adjacency`
entered the envelope unvalidated, found by both judges in round 1), **C23**, **C24** (`clamp` was
not total under `NaN`, so the C22 fix did not close its own class — both judges again, round 2),
**C25** and **C26** (closed by owner ruling), **C30** (self-caught by the executor, not by Judgment
Day), and **C33** (round 3: the check's *method* was the defect, not one more missing shape).

They are listed here as a pointer, not a summary. The findings' own wording in `tasks.md` is the
record.

## Archive date

Named `2026-08-06-m2a-weight-focus` after the date of the last commit that touched the folder.

## What this report cannot verify

Merge-time CI state, for the same reason as every change in this pass.
