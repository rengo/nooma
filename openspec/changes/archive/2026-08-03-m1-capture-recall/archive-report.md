# Archive report — `m1-capture-recall` (retroactive)

**Written 2026-08-30, long after the change closed.** No archive report was written at the time;
the convention started with M3. Everything below is reconstructed from evidence present in the
repository today.

## What the change was

The **umbrella proposal** for M1 — the brain gets written: the `LLMProvider` / `EmbeddingProvider`
interfaces and their implementations, `tasks:` routing, the synchronous capture pipeline of
`docs/02-cognitive-core.md` §5, hybrid recall with RRF fusion, the dedup/relation judge with its
thresholds, and in-place correction.

The folder holds `proposal.md` and nothing else, by design. On 2026-07-30 the owner decided
(proposal §8 Q5) to plan it as **three chained changes** rather than one, and the spec, design and
tasks live in those:

- `m1a-substrate` — the substrate
- `m1b-pipeline` — the pipeline
- `m1c-surface` — the surface

Each is archived in this same pass, with its own report.

## Evidence that it closed

- First commit: `189e812` (2026-07-31), *"docs(m1): the M1 proposal, and the decision to plan it in
  three"*. Last: `db9f409` (2026-08-03), *"docs(m1): order PR 17 before PR 15 — the wizard cannot
  offer a path that does not exist"*.
- Its three children are complete: `m1a` 43/43 tasks, `m1b` 74/74, `m1c` 96/96.
- The M1 demo is live in the product: capture via the API and via `nooma capture`, a real question
  answered by a real recall, and correction of what was captured — all against a migrated vault, on
  Linux and Windows.

## Archive date

Named `2026-08-03-m1-capture-recall` after the date of the last commit that touched the folder.
That date coincides with the M1 close date `CLAUDE.md` carried until 2026-08-30.

## What this report cannot verify

- **Merge-time CI state**, for the same reason as every change in this pass.
- **A discharge against the code.** M2's umbrella proposal carries a *"Discharge — 2026-08-19, PR
  #190"* section that verified each success criterion against the code at a specific commit. **M1's
  umbrella has no equivalent section**, and one cannot be written honestly today: verifying M1's
  criteria now would measure the code as it stands after M2, M3 and the post-M3 fixes, not as it
  stood when M1 closed. The absence is the finding.
