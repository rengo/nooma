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

This freezes per requirement, not per change directory. A change can ship across several PRs; a
requirement freezes when **the PR that implements it** merges and is verified, not when the whole
change directory finally closes. While the implementing branch is still under review, that
requirement's text is mutable and gets corrected in place. Once its PR has merged, further
corrections are recorded as annotations — not rewrites — alongside the frozen text.

## Archive

A closed change moves to `changes/archive/<YYYY-MM-DD>-<change-name>/`, dated by the day it
closed, and gains one file it did not have while open:

| File | What it holds |
|---|---|
| `archive-report.md` | What the change was, the evidence that it closed, and **what that evidence cannot prove** |

`changes/` therefore holds only what is in flight. An empty `changes/` means nothing is open, and
that is a legible state rather than an ambiguous one.

The report's last section is not a formality. Merge-time CI state, for instance, cannot be
recovered after the fact: `gh pr view` reports a rollup today, and no read proves that no required
check was waived at merge time. A report that omits that limit reads as a stronger guarantee than
it is.

**Moving a directory changes what its relative links mean.** An artifact one level deeper needs
`../../../../docs/…` where it used `../../../docs/…`. Adjusting that depth is a mechanical
consequence of the move and is not the editing this document forbids above — the link's target is
preserved, not rewritten. Leaving it is how twenty-nine links in this tree came to point at
nothing.
