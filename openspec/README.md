# openspec/

Planning artifacts for changes that are too large to hold in a PR description.

Each change gets a directory under `changes/<change-name>/` holding, in order of production:

| File | What it holds |
|---|---|
| `proposal.md` | Intent, scope boundary, approach, PR chain, TDD ordering |
| `spec.md` | The requirements the change must satisfy, testable |
| `design.md` | Technical decisions and their rationale |
| `tasks.md` | The executable breakdown, checked off as work lands |

## Why these live in the repository

These artifacts are committed **alongside the code they govern**. A proposal that lives in a
chat window or a ticket is unreviewable six months later, when the question is not "what did we
build" but "why is it shaped like this". The `docs/` tree records decisions that are in force;
`openspec/` records how a specific change got planned and sliced.

They are subordinate to `docs/`. If an artifact here contradicts
[`docs/02-cognitive-core.md`](../docs/02-cognitive-core.md) or an `Accepted` ADR, the doc wins
and the artifact is wrong.

## Lifecycle

A change directory stays until the work merges and is verified. After that it is a historical
record: it is not edited to match what shipped — the shipped truth lives in `docs/` and in the
code.
