# Archive Report — m3e-pending-question

**Change**: m3e-pending-question (closes `m3d` finding J24, one of the two items M3 left
deliberately open)
**Date Archived**: 2026-09-16
**Status**: Complete — 5 PRs merged, 61 tasks checked, `make check-all` green on `main`
**Main Branch HEAD at Archive**: `af59318` (merge of PR #261), tree `0428acb`

---

## Artifacts Archived

`proposal.md` (286), `spec.md` (195), `design.md` (869), `tasks.md` (765) — moved from
`openspec/changes/m3e-pending-question/` byte-for-byte, no content edited. `spec.md`
carries an annotation block added during PR 5 naming the four MUSTs that `design.md`
superseded; the MUSTs themselves are left standing, per this repository's annotate-don't-edit
convention.

---

## What shipped

A closed loop. The nightly connect pass judges a relation into the
`[min_confidence_to_persist, min_confidence_to_surface)` band and **queues a question** about
it. The morning digest **asks** it, naming both endpoints — a digest it will send even with
no triggers due, because a pending question is not nothing to say. An inbound "yes" raises
the relation's confidence to its type's own surface floor and emits `relation_confirm`, that
signal's first emission anywhere in this tree. An inbound "no" emits `relation_reject` and
deletes the relation, signal first (I10, unchanged). A question nobody answers expires after
`MaxDigestDeferrals` digests as a state transition, never a delete.

**I09 — the `[persist, surface)` band is stored AND asked about — has its first real
conformance test**, over the band rather than over the deferral, green end to end from
`judgeAndPersistPair` through `assembleDigest`.

### The five merged PRs

| # | PR | Branch | Commits | impl+docs |
|---|---|---|---|---|
| 1 | [#257](https://github.com/rengo/nooma/pull/257) | `feat/store-pending-questions-migration` | `155c6d5..1a57603` (2) | 149 |
| 2 | [#258](https://github.com/rengo/nooma/pull/258) | `feat/core-relation-confirmed-confidence` | `3985e34..b6e46b2` (2) | 53 |
| 3 | [#259](https://github.com/rengo/nooma/pull/259) | `feat/ports-store-pending-questions` | `c15c2ef..8dfeda1` (2) | 370 |
| 4 | [#260](https://github.com/rengo/nooma/pull/260) | `feat/brain-queue-and-digest-asks` | `fdbecb7..0c0d353` (8) | 332 |
| 5 | [#261](https://github.com/rengo/nooma/pull/261) | `feat/brain-relation-checkin` | `f752a8b..2e41719` (5) | 294 |

1,190 lines implementation + docs. Every link under the 400 ceiling; test lines counted
separately per `docs/06-harness.md` §7.

---

## Verification Gate: PASS WITH WARNINGS

`sdd-verify` (fresh context, told explicitly **not** to take `tasks.md`'s own claims at face
value and to re-run at least one mutation itself): **0 CRITICAL, 4 WARNING, 2 SUGGESTION**.
All ten spec requirements compliant with runtime evidence. Strict TDD ordering verified by
reading `git log` per PR — every RED commit strictly ahead of its GREEN.

`make check-all` green on `main` after the chain landed: lint, L1/L2, build, L3, the
schema-golden regeneration diff, `internal/core` coverage 99% (990/992, floor 90%), the
seven-target cross-compile matrix, and L4 (145s). Each link also passed all 16 CI contexts
on its own PR, including `docs<->code sync`, which `make check-all` cannot run locally.

Two of the four warnings were fixed before delivery (commit `2e41719`): `tasks.md` task
2.1's wrong mutation claim, and `spec.md`'s unannotated superseded MUSTs. The other two —
PR 3 over its own ~300 budget at 370, and the AI-attribution trailers — are recorded below.

---

## The findings that matter

### Four spec/design disagreements, all resolved in design's favour

Argued in full in `tasks.md`'s own Findings section, and each independently re-verified
against running code by `sdd-verify`. Summarised here because `spec.md` still reads the old
way by convention:

- **F1** — the confirm signal is emitted **after** the raise, the opposite of I10's order.
  Same rule (never leave evidence for an effect that may not have happened) pointed at the
  direction each effect can fail in: a delete is irreversible, a raise is idempotent.
- **F2** — `ProposedRelation.Band` is computed **inside** `core/consolidation`, which spec R1
  forbids by name. `ProposeRelation` already called `relation.Decide` and threw the result
  away; the alternative is one rule written in two packages.
- **F3** — `ConfirmedConfidence` takes `relation.Thresholds`, not a bare `floor float64`.
- **F4** — the digest's queue orders by `created_at` then `id`, not by `asked_at`. Spec R3's
  wording is **unreachable**, not merely different: the digest reads queued rows, whose
  `asked_at` is NULL by construction, and ordering a set of NULLs orders nothing.

### Two defects in this change's own plan, found at delivery

**The slicing produced an unmergeable PR.** `tasks.md` planned six PRs. Planned PR 4's own
conformance test was written **deliberately red** — I09 is "stored AND asked about", and PR 4
built only the storing half — with the instruction to "disclose the red in PR 4's
description" and turn it green in PR 5. Disclosure cannot help: `main` is protected by a
ruleset with 17 required contexts and **zero bypass actors**, so a PR whose own suite is red
never reaches `mergeStateStatus: CLEAN`. PR 4 was not awkward, it was **unmergeable**.
Delivered as five links, with planned 4 and 5 merged into one (332 lines, under the ceiling,
every commit unchanged and each RED still ahead of its GREEN). **The next change that plans
a deliberately-red intermediate PR needs to know that the plan cannot end there.**

**A commit sat in the wrong link, and only a per-link check found it.** Migration `0004`
bumps `PRAGMA user_version` to 4, and three L3 files assert the old literal. The commit
fixing that fallout had been written into PR 3's range. PR 1 alone was therefore **green
under `make check`and red under `make check-all`** — 7 failing integration tests — which
would have landed `main` red mid-chain. Moved into PR 1 before anything was opened.

### One class of gap that keeps recurring

**Task 3.3 was never written.** The `pending_questions` vocabulary pin — `AllQuestionKinds()`
and `AllQuestionResolutions()` matched against migration `0004`'s own column comments — was
ticked in PR 3's plan and does not exist in PR 3's diff. Nothing caught it: nothing anywhere
in the tree read `AllQuestionKinds()`, and the only reader of `AllQuestionResolutions()` was
a test written two PRs later. Found by walking `tasks.md` box by box while closing PR 6, and
shipped there. It also landed in a different file than the task named — `m3b`'s G4 discipline
lives in `test/conformance`, not `repocontract`, because a comment-order pin does not vary by
repository implementation.

**Task 2.1's `Mutation:` line was fiction.** It claimed the NaN sweep in property (a) catches
`math.Max(current, t.Surface)`. It does not: `relation.Decide` compares with `<`, every NaN
comparison is false, so a NaN falls through to `Asserted` and property (a) holds **vacuously
for exactly the input the mutation corrupts**. A dedicated fourth test catches it. The gate
was real; the sentence describing the gate had survived a full PR review. Found by
`sdd-verify` running the mutation, because it was told to.

---

## Method notes

**Verify each link, not the tip.** The branch tip was green throughout — `make check-all`
locally, and `sdd-verify` agreed. Neither said anything about whether the chain could be
*merged*, because both measured the same point: the end. Running `make check-all` at each
link's own commit in its own worktree found two blockers (PR 1 red, PR 4 unmergeable) that
every tip-level check was structurally incapable of seeing.

**Grep for the class, not the string.** Nine commits carried AI-attribution trailers against
the project rule. The first strip pass removed `Co-Authored-By:` and reported "0 trailers" —
having grepped for the one string it was looking for. The commits also carried
`Claude-Session:`. The second pass filtered six known forms and then **probed by shape**:
every line matching `^Word:` across all 19 commits. The branch was rebuilt with the tree
verified byte-identical before and after.

**A delegated phase's output is a claim, not a result.** The `sdd-archive` sub-agent copied
the artifacts instead of moving them, and the copies were mutilated: `design.md` 869 → 189
lines, `tasks.md` 765 → 107. Its report cited commit SHAs that no longer existed, branch
names never opened, four wrong line counts, and `make check-all` timings it was never given.
It also reported two warnings as open that had been fixed in a commit it had been told
about. Discarded and redone by hand; the originals were intact in git, so nothing was lost.
The check that caught it was diffing the archived copy against the original rather than
reading the agent's summary.

**Mutation-probe the high-level tests.** The L4 demo and the threat-matrix conformance case
were both checked by breaking the code and confirming they failed — short-circuiting
`resolveRelationCheckIn` fails both demo subtests, suppressing `ConfirmRelation`'s signal
fails the "yes" branch — rather than trusted green-on-arrival.

---

## Carried forward, named rather than silently inherited

**Design §11 Q1 — the queued pool is unbounded.** One question per digest against a producer
with no bound. The *open* pool is bounded at `MaxDigestDeferrals` by arithmetic (a question
is asked exactly once), but nothing bounds the unasked queue. Not mitigated here; a
`created_at`-based queue expiry would cost a second calibration-adjacent number for a
pressure nothing has measured.

**Design §12 N4 — `ConfirmRelation` races the nightly connect pass** inside one `nooma serve`
process. Bounded by `vaultlock` across processes. Worst in-process case is one confidence
revision losing to another: no deletion, no lost question, and `ConfirmedConfidence`'s
idempotence means a repeat answer corrects it.

**PR 3 exceeded its own budget without measuring.** 370 impl+docs lines against its stated
~300, under the 400 ceiling so no split was forced. Its sibling links measured themselves and
reported; PR 3 recorded no count at all. The link most likely to need the discipline is the
one that skipped it.

**M3's other deliberately-open item is still open.** `docs/05-build-plan.md` names two.
This change closes the first (m3d finding J24 — an inbound answer now names *which* relation
it answers). The second — listing and cancelling a timer from chat — that document assigns
to M4, since `ports.TimerRepo` declares neither method and this repository does not ship a
method with no caller.
