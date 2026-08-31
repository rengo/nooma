# Archive report — `m1c-surface` (retroactive)

**Written 2026-08-30, long after the change closed.** No archive report was written at the time;
the convention started with M3. Everything below is reconstructed from evidence present in the
repository today.

## What the change was

The third and last of the chained changes splitting `m1-capture-recall`. The surface M1 promised:
corrections (routing a `correction` classification away from `classify.ToUnit`), and the rest of
the user-facing half that made M1 demonstrable rather than merely implemented.

## Evidence that it closed

- `tasks.md`: **96 of 96** task boxes checked — the largest of the three M1 phases.
- First commit: `44bbcd0` (2026-08-03), *"docs(m1c): commit Phase C's planning artifacts"*. Last:
  `858b25a` (2026-08-04), *"docs(m1c): annotate the stale degrade wording C21.1 recorded"*.
- Closing it closed M1: correction in place is one of the four things `CLAUDE.md`'s status
  paragraph has claimed as delivered since 2026-08-03.

## One finding worth carrying forward

`tasks.md:877` records **C20**, and one of its four parts is a measurement rather than a bug:
PR `16a-ii` *"measured 297 changed lines against its own ~150 ceiling (1.98×)"*. The 400-line PR
ceiling this repository now enforces, and the `chained-pr` split it triggers, are downstream of
findings shaped like that one.

## Archive date

Named `2026-08-04-m1c-surface` after the date of the last commit that touched the folder. It is one
day after the M1 close date `CLAUDE.md` carried; the trailing commit is a documentation
annotation, not delivery.

## What this report cannot verify

Merge-time CI state. The rest of C20 and its sibling findings live in the change's own `tasks.md`
and are not restated here.
