# Archive report — `m1a-substrate` (retroactive)

**Written 2026-08-30, long after the change closed.** No archive report was written at the time;
the convention started with M3. Everything below is reconstructed from evidence present in the
repository today.

## What the change was

The first of the three chained changes splitting `m1-capture-recall` (owner decision, 2026-07-30,
umbrella proposal §8 Q5). The substrate M1's pipeline would later stand on — beginning with a
documentation preflight that made `docs/01-architecture.md` describe `openai` as a provider type
before any code assumed it.

## Evidence that it closed

- `tasks.md`: **43 of 43** task boxes checked.
- First commit: `3cc6b3c` (2026-07-31), *"docs(m1a): the substrate spec — 11 sections of testable
  requirements"*. Last: `8b529ef` (2026-07-31), *"docs(m1a): R9.3 claimed a verification it had not
  run"* — a spec correcting its own overclaim, which is the culture this repository keeps.
- Its two successors, `m1b-pipeline` and `m1c-surface`, both completed on top of it and are
  archived in this same pass.

## Archive date

Named `2026-07-31-m1a-substrate` after the date of the last commit that touched the folder.

## What this report cannot verify

Merge-time CI state. Whatever review findings this change produced live in its own `tasks.md`
annotations; this report does not restate them, because summarising a record written a month ago by
a different pass would be invention rather than archiving.
