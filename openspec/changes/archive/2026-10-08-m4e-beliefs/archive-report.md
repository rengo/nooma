# Archive report — `m4e-beliefs-activity-admin` (scoped to beliefs)

**Closed**: 2026-10-08. **Verify verdict**: PASS — 0 CRITICAL (engram obs #777, filed as
"PASS WITH WARNINGS": 3 warnings, 3 suggestions; all resolved or corrected below). **`main` at
archive**: `eed46b9`, the merge of PR #296.
**Milestone**: the fourth M4 slice to close (after `m4a`, `m4b`, `m4c`), beliefs only. The change started as beliefs, activity
and admin together and was split twice; the archived directory is named `m4e-beliefs`.
`m4e-activity` and `m4e2-admin` remain active under `openspec/changes/`, and
`openspec/changes/m4-mirror-ui/proposal.md` stays active for `m4d` and `m4f`.

## What shipped

The user can open `/ui/beliefs`, see the active beliefs by facet, edit one, claim a derived one by
saving it, and retire one. The nightly derive honours both: it never brings back what the user
retired and never overwrites what the user edited.

| PR | Branch | What |
|---|---|---|
| #288 | `plan/m4e-beliefs-activity` | spec, design, tasks for the original three-way change; umbrella Q4/Q5 rows ruled |
| #289 | `feat/ports-store-belief-status` | PR 1: belief `status`, content bound, store guards (impl+docs 318, 1.2x) |
| #291 | `feat/brain-derive-shield` | PR 2a: the pure retired-key shield, `no-spec-change` label |
| #292 | `feat/brain-derive-retired-wiring` | PR 2b: the shield wired into nightly derive, fail closed, `size:exception` |
| #294 | `plan/m4e-activity-split` | split plan: activity moved out to `m4e-activity` |
| #295 | `feat/brain-belief-edit-retire` | PR 3: `ByFacet`, `Edit` (with claim), `Retire`, `WriteLandedError`, `size:exception` |
| #296 | `feat/ui-beliefs` | PR 4: `/ui/beliefs` list, edit, retire; gates G6, G7, G12; `size:exception` |

Two fix PRs are not part of the change but landed during it and are cited because they unblocked
its gates: #290 (e2e: wait on the serve child and retry a lost port) and #293 (serve announces
the bound address so e2e readiness is verifiable).

## Owner rulings

| Ruling | Date | Effect |
|---|---|---|
| Q4: `m4e` runs ahead of `m4d` | 2026-10-07 | ADR-0019 is `Proposed`; `m4d` idles |
| Q5: admin writes the five hand-set fields only | 2026-10-07 | owned by `m4e2-admin` |
| OQ3: the user's word wins | 2026-10-07 | derive never re-creates a retired belief nor overwrites an edited one (R12, R13) |
| OQ1, OQ2 defaults adopted | 2026-10-07 | only `content` is editable; the status is named `retired` |
| OQ5, OQ6 defaults adopted | 2026-10-07 | now in `m4e2-admin` (R7) |
| Admin splits off as `m4e2-admin` (OQ7) | 2026-10-08 | |
| Activity splits off as `m4e-activity`, after PR 2 measured 2.06x | 2026-10-08 | the pre-agreed more-than-seven-PRs rule |
| Unusable proposal vectors fail closed when a retired belief exists | 2026-10-08 | `belief_skipped`, reason `unusable_vector_retired_unchecked` (#292) |
| Saving unchanged claims a belief | 2026-10-08 | derived or seed becomes `user_stated`; text kept byte for byte (#295) |
| A claim's signal is positive | 2026-10-08 | a real edit and a retire stay negative (#295) |

## What it leaves behind, beyond the feature

| Gate | What it makes unexpressible |
|---|---|
| SQL upsert guard (`TestSelfModelRepo_WriteGuards`) | an upsert that overwrites an edited, seed-protected or retired belief |
| I03 over `self_beliefs` | a `DELETE FROM self_beliefs` anywhere under `internal/` or `cmd/` |
| depguard `sqlitetest-tests-only` | a production import of the test-only helpers |
| G6 body coverage | a cross-origin POST leaf without a body-bearing refusal test; each leaf driven with its own method |
| G7 read-views-write-nothing, with reflection | a GET view whose port, signal or log decorator gains an unclassified writing method |
| G12 I22 whitelist | a UI dependency exposing a brain method outside `ByFacet`, `Edit`, `Retire` |
| `no-spec-change` label | a docs-sync failure on a structural-only `internal/core` PR (#291) |

## Cost, stated plainly

- **Judgment Day rounds.** The counts supplied for this archive were design 3, PR 1 2, PR 2a 2,
  PR 2b 2, PR 3 2, PR 4 2. They are **not confirmed**: PR bodies do not state a round count. What
  the record shows is one post-judgment fix round on each of PR 1 (`ccc06a9`, `ac6d2db`), #291,
  #292, #295 and #296 (apply-progress, engram #770), plus a second owner-approved fix batch on
  #295 (claim signal made positive). The design rounds are not recorded in anything checked here.
- **Multipliers (impl+docs actual over forecast).** PR 1 1.2x. PR 2 2.06x (557 against ~270), which
  forced the cut and both splits. PR 3 1.51x before fixes (393 against ~260). PR 4 1.09x before
  fixes (301 against ~275).
- **Size exceptions** on #292 (468), #295 (465) and #296.
- **#296 final size, measured.** `git diff --numstat eed46b9^1 eed46b9`, excluding tests and
  generated `*_templ.go`: 346 changed lines of impl+docs, 386 with the 40 `tasks.md` lines. The PR
  body states about 364 and 402; those came from adding the review-fix delta to the opening
  measurement. By the merged range the PR sits under 400; the label stays as applied.
- Tests ran far over forecast on every PR (PR 4: 1,340 against ~450).

## Verification

`sdd-verify` at `eed46b9`: R1-R4, R9, R11-R13 each mapped to code and a passing test; about 75
gate mutants killed in a detached worktree; `make check-all` exit 0; `go vet -tags integration,e2e`
clean. The real binary was served, a belief seeded through the sqlite CLI (no LLM-free path
creates one), and listed, claimed, edited, and retired over HTTP, with 404, 409 and cross-origin
403 observed.

Resolved report items:

- **WARNING 1** (`docs/07-functional.md` still said "nothing built") is fixed in this archive PR.
- **WARNING 2** (`tasks.md` C.1 and C.2 unticked, PR 4 measurement stale) is fixed in this
  archive PR.
- **WARNING 3** said #296 lacked a `size:exception` label. **That was wrong: #296 carries the
  label** (checked with `gh pr view 296`). Correction recorded; nothing to fix.

## Open follow-ups (not blocking)

- A pre-existing duplicate-reinforce `byID` log row in `persistRoutes`.
- An empty facet `<ul>` remains after retiring its last belief (cosmetic).
- Un-retire has a store path (`SetStatus` retired to active) but no caller.
- `m4e-activity` and `m4e2-admin` remain active changes.
- Doc 02 section 6 item 5 has one overlong line (formatting only, verify SUGGESTION 1).

## Next

`m4e-activity`, then `m4e2-admin`; then `m4f`. `m4d` stays blocked on ADR-0019 being `Accepted`.
