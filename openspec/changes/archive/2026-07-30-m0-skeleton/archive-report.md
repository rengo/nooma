# Archive report — `m0-skeleton` (retroactive)

**Written 2026-08-30, long after the change closed.** No archive report was written at the time;
the convention started with M3. Everything below is reconstructed from evidence present in the
repository today.

## What the change was

`docs/06-harness.md` §9 step 5 — M0 as `docs/05-build-plan.md` lays it out: the configuration
loader (`nooma.yml` plus `.env`), vault resolution, the single-writer lockfile, and the CLI
commands `init`, `serve`, `status` and `doctor`.

## The discrepancy this archive refuses to bury

**`tasks.md` has 75 of its 77 boxes unchecked — and the work landed anyway.** The checkboxes were
abandoned, not the change. Spot-checked against the repository on 2026-08-30:

| Task | Claim | Verified today |
|---|---|---|
| 2.1 | Write `docs/adr/0013-cross-compile-targets.md` | The file exists, and `CLAUDE.md` names ADR-0013 as in force |
| 1.1 | Remove "next to the executable" from `docs/01-architecture.md` | Zero matches for that phrase anywhere under `docs/` |
| 1.3 | Add `server.bind` to the `nooma.yml` documentation | `docs/01-architecture.md:187` documents `bind`, and `internal/config/config.go:34` decodes it |

The M0 surface itself is live: `nooma init`, `serve`, `status` and `doctor` are all shipped
commands, exercised by the e2e suite.

**This is a bookkeeping gap, not undone work, and it is recorded here rather than silently fixed.**
The boxes are deliberately left as they are: ticking 75 of them today would fabricate a
contemporaneous record that never existed. The honest artifact is this paragraph.

## Evidence that it closed

- First commit touching the folder: `0af5aa6` (2026-07-30), *"docs(sdd): plan m0-skeleton —
  proposal, spec and design"*. Last: `8b5ac6a` (2026-07-30), *"fix(test): the five defects the
  Windows jobs found, and none of them were the store"*.
- Every milestone after it (M1, M2, M3) closed on top of the surface it delivered.

## Archive date

Named `2026-07-30-m0-skeleton` after the date of the last commit that touched the folder.

## What this report cannot verify

Merge-time CI state, for the same reason as every other change archived in this pass: a rollup read
today cannot prove that no required check was waived at merge time.
