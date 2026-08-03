# Tasks — M1c: the surface

Implementation task list for `m1c-surface`, derived from `spec.md` (revision 4, ten numbered
sections, R1–R9 plus test-level and boundary sections) and `design.md` (revision 4, eighteen
decisions, D1–D18). Chain strategy **`stacked-to-main`**, matching M0, `m1a-substrate` and
`m1b-pipeline`. Scope is the umbrella proposal's six-row Phase C table as PR #101/#102 widened it
— PR 12 `feat/corrections`, PR 13 `feat/httpapi-capture-recall`, PR 14 `feat/cli-capture-demo`,
PR 15 `feat/init-provider-paths`, PR 16 `feat/doctor-quality-gate`, PR 17 `feat/openai-embeddings`
— which `design.md` §6 splits into **seventeen** merges (verified by direct count against the
design document's own chain table: `12a,12b,12c,12d,12e,12f,13a,12g,13b,13c,13d,17,15,16a,16b,
14a,14b`).

**This document pre-splits two of those seventeen into nineteen**, per this change's own
governing instruction: *"if a slice's honest estimate exceeds 400 lines, split it now rather than
discovering it at PR time."* Four slices land at or over design's own 400-line ceiling before any
of Phase A/B's own measured 1.3×–2.6× multiplier is applied — `design.md` itself names three of
them (`12f` ~430, `13b` ~450, `16a` ~420) plus `13d` (~420) as "the rows to read through that
band." Each of the four was evaluated for a split line rather than left as a forecast risk to
discover later:

- **`12f` splits into `12f-i`/`12f-ii`** — no `MUST` in `spec.md` requires the pre-image ordering
  (R1.9) and the learning-signal write (R1.10) to land in the same PR; D5/D6's own pipeline
  diagram already treats them as sequential steps (`applyWithPreImage`, *then* `signals.Record`),
  which is a clean stack seam.
- **`16a` splits into `16a-i`/`16a-ii`** — "does the check work at all" (R5.1, R5.3, R5.4) and
  "does it handle the edge states correctly" (R5.6, R5.7, R5.8) are two different questions over
  the same one `doctorChecks` row; nothing requires them in one commit.
- **`13b` is NOT split, on purpose.** `spec.md` R2.9's own `MUST` forbids it: ADR-0017 and the
  auth middleware must land "in the same PR that mounts `POST /capture`... so that no commit in
  this change's history ever has a write route mounted without the check already in place." A
  split here recreates the exact defect the requirement exists to prevent. Carried as an
  unsplittable, flagged High-risk stop-and-report checkpoint instead — the same treatment
  `m1b-pipeline` gave PR 8a for the same reason (R2.8's own MUST forbade splitting the
  `pending-red` retirement from the code that needed it retired).
- **`13d` is NOT split, on purpose.** No natural seam exists between `serve.go`'s wiring and
  `tasksM1Consumes`'s first reader — the list only has meaning once wired to a reader, and the
  wiring is connective tissue with almost no branching (`m1b-pipeline` C8's own lesson: a cut that
  reaches 400 by separating declaration from its consumer breaks "one commit = change + tests +
  doc" to satisfy a line counter, which is not what the ceiling protects). Flagged High-risk
  stop-and-report instead.

**A fifth slice, `12c`, joined this list in flight rather than up front — not by choice.** It
looked splittable ahead of the other four: `design.md` §4's own package layout already lists
`edit.go` and `plan.go` as two files, and D3's own text reads as two decisions. `sdd-apply`
measured the unsplit PR at 501 lines (1.25× its ~400 ceiling), and a split along the `Edit`/
`PlanEdit` seam was attempted (PRs #108/#109, `feat/core-correction-edit` +
`feat/core-correction-plan`, both closed unmerged). `docs<->code sync` rejected `12c-i`
(`edit.go`/`edit_test.go` alone): it touches `internal/core/**` with no `docs/02-cognitive-core.md`
delta of its own, because `Edit` is an opaque type with three accessors and no behaviour doc 02
governs — the delta belongs to `PlanEdit`, which decides what a correction writes. The seam
looked clean and held structurally through `make check-all` (both halves passed `golangci-lint`,
`TestCoreExportedDeclsHaveTests`, and the coverage floor standing alone); it was still wrong,
caught by the one gate `make check-all` cannot run locally. See Conflicts §C2 and the Review
Workload Forecast for the full record. `12c` now joins `13b`/`13d` as unsplittable, carrying
`size:exception`.

Nineteen merge links, in order: `12a, 12b, 12c, 12d, 12e, 12f-i, 12f-ii, 13a, 12g, 13b, 13c, 13d,
17, 15, 16a-i, 16a-ii, 16b, 14a, 14b`.

**`docs-sync.yml` fires per pull request, not per umbrella PR-number** (`m1b-pipeline` C9's own
lesson). `spec.md` R1.13/R7.2 say umbrella "PR 12" needs no *new* prose because doc 02 §5 step 4
and §13 already carry Q3c's answer — but `12a`, `12b` and `12c` are three separate pull requests,
and `design.md` D13 pre-assigns each of them (and every other core- or doc-adjacent slice) its own
real delta so none reaches `docs-sync.yml` needing `no-spec-change`. Every task below that carries
a doc delta cites D13's row for its own slice.

**Strict TDD is active** (`CLAUDE.md` non-negotiable #4) and **is no longer backed by a Makefile
target** — `make pending-red` was retired in `714934e`, confirmed absent from this repository as
of this writing. Every behavioral task below states its test first and what its red looks like;
the discipline is carried by review and by the commit sequence, per `spec.md` R8.2. One task
(`12f-i.2`, the AST ordering guard) has no natural pre-implementation red — it is a structural
proxy over code that is already correct by construction the moment `applyWithPreImage` exists, the
same category `m1b-pipeline` tasks.md established for `brain_no_direct_clock_read_test.go`. Its
own sub-task states this explicitly and includes a temporary-break check in its verify step.

Every PR runs `make check-all` before opening (`nooma-pr` Hard Rule 2), not `make check`.

---

## Conflicts surfaced (do not silently resolve)

This section starts with one finding from cutting this task list and is otherwise left ready for
whatever apply, measurement, or execution surfaces next — `m1b-pipeline` found fourteen conflicts
this way and not one of them by review.

### C1 — `design.md`'s C10 ("open, escalated, must be settled before `sdd-apply` reaches 16b") is stale. `spec.md` revision 4 already settled it, in design's own recommended direction.

`design.md` §2's C10 section, its §8 risk row 0, and its §9 all describe an **open, unresolved**
disagreement: `spec.md` R6.3 asks that a permanently-unembedded Cloud vault be statable at
runtime, which needs `ports.EmbeddingRepo.CountLiveWithoutEmbedding` — a method on a Phase B file
— while `spec.md` R7.3/R7.4 (as design read them) forbid every edit to `internal/ports/**` beyond
a short enumerated list that does not name `embeddingrepo.go`. Design lays out two resolutions,
priced both ways, and states plainly: *"This design's recommendation is the first [add one clause
to R7.3/R7.4 naming `EmbeddingRepo`'s count method], and it is a recommendation... it must be
settled before `sdd-apply` reaches 16b."*

**Re-reading `spec.md`'s own current text (not design's summary of it) shows this already
happened.** R7.3 now reads, verbatim: *"`internal/ports/embeddingrepo.go`'s exception is a debt
discharged, not a scope carved"*, citing the exact `m1b-pipeline/design.md:790-793` deferral
design's own C10 analysis cites, and concluding *"Phase B's stated condition — no caller but a
test — discharges itself once `doctor` (R6.3) is that caller."* R7.4 goes further, explicitly
correcting its own prior revision's omission: *"the only three sanctioned edits to existing
interfaces"* now names `unitrepo.go`'s two methods, `decisionlog.go`'s two actions, **and**
`embeddingrepo.go`'s new consistency method, with a `MUST NOT` against reading the enumeration as
covering only the untouched files, "naming five files as untouched and two edits as sanctioned
while never mentioning `embeddingrepo.go` at all."

This is exactly design's own Option 1, adopted into the spec text at the cost design itself
priced: "one sentence in the spec." `spec.md`'s current text is what a reader following
`design.md`'s C10 section verbatim would not expect to find — the same pattern
`m1b-pipeline/tasks.md` C2 and C3 already named twice: a design document's own conflict callout
describing a wording its cited document has since corrected.

**Resolution: C10 is closed, in design's own recommended direction.** `16b` below is cut to ship
**both** D18b rows — task coverage (a pure config read) and vault coverage
(`CountLiveWithoutEmbedding`) — matching design's own "the estimate carries it" ~330-line sizing
for `16b`, which already assumed the larger branch. `design.md`'s own §2 C10 section, §8 risk row
0, and §9 are stale on this one point and need a follow-up correction from whoever next revises
that document, per `CLAUDE.md` non-negotiable #1's spirit applied to two documents disagreeing
rather than to doc-and-code.

### C2 — `12c` looked splittable along `Edit`/`PlanEdit` (design §4's own file layout suggests it); it is not. **RESOLVED — the seam does not exist; `12c` is unsplittable, `size:exception` applied.**

`sdd-apply` measured the unsplit `12c` PR at 501 changed lines against its own ~400 ceiling
(1.25×). `design.md` §4's package layout lists `edit.go` and `plan.go` as two files under
`internal/core/correction/`, and D3's own text reads as two decisions (the gate, the plan) — both
readable as evidence a clean split exists at that file boundary. The coordinator directed a split
on exactly that reading: `12c-i` (`edit.go`/`edit_test.go`, PR #108) and `12c-ii` (`plan.go` plus
its tests, the L2 conformance corpus, and the doc 02 delta, PR #109), `12c-ii` branched from `main`
directly rather than from `12c-i`, so neither PR's diff would show the other's code.

**Both PRs passed `make check-all` standing alone** — `12c-i`'s own `TestCoreExportedDeclsHaveTests`
and `internal/core` coverage floor held with only `edit.go`/`edit_test.go` added (100%, 300/300),
which reads as the seam holding structurally. It was not: `12c-i` touches `internal/core/**` and
changes no `docs/02-cognitive-core.md`, and `docs<->code sync` is the one gate `make check-all`
cannot run locally — it decides on PR base and labels, which exist only once a PR is open on
GitHub. `CLAUDE.md` non-negotiable #1 requires the doc to change in the same PR as the core change
it documents; `12c-i` has no such change to make, because **`Edit` alone changes no observable
behaviour**. An opaque type with three per-field accessors is *how* the "a correction writes
exactly one field" guarantee is made structural, not *what* the system does — doc 02 governs
behaviour, and `PlanEdit` is what decides which field a correction writes, which is the entire
content of the §5 step 4 delta. Under `work-unit-commits`, a work unit is change + tests + doc;
`12c-i` could only ever be change + tests, because its doc does not exist. A second candidate seam
— core-plus-doc in one PR, the L2 conformance corpus in a second — fails differently: it would
merge behaviour before the conformance test that proves it, which strict TDD (`CLAUDE.md`
non-negotiable #4) forbids.

**Resolution: `12c` is genuinely indivisible — `nooma-pr`'s own condition for `size:exception`
("if splitting is genuinely wrong") — and joins `13b`/`13d` as an unsplittable, flagged High-risk,
`size:exception` link.** Both split PRs (#108, #109) were closed unmerged with this reasoning in
their closing comments; the single combined PR (this `12c` block, all tasks `[x]`) supersedes them.
Recorded for whoever next eyes a design document's file layout as a split seam: a package listing
two files, or a design section reading as two decisions, is necessary but not sufficient evidence
of a valid split — the doc-delta ownership test (does each half have its own behaviour to
document?) is what actually decides it, and `make check-all` cannot check it for you.

### A note on merge mechanics, not a spec/design conflict — flagged because it changes what "the same PR" safely means for every link below

`nooma-pr`'s own Hard Rules state: *"Merging | `gh pr merge <n> --merge`. Do not delete the
branch — merged branches are kept."* This task list's merge-order section (below) requires the
opposite outcome for every one of this chain's nineteen links: the repository's
`delete_branch_on_merge` setting must be **on**, so that each merge auto-deletes its branch and
GitHub retargets any dependent open PR's base to `main`. The task that specifies this chain
(2026-08-01's incident, restated below) is explicit that the setting is now on, and that its being
off before is the entire reason two links of a three-link stack landed in the wrong place instead
of `main`.

**These two instructions are in real tension for this chain**, not a false alarm: `nooma-pr`'s
rule, read literally, is about not passing `--delete-branch` to the merge command — but a
repository-level `delete_branch_on_merge` setting deletes the branch anyway, regardless of the
flag, so the skill's stated outcome ("merged branches are kept") does not hold for any PR in this
repository while that setting stays on, chained or not. Named here rather than picked silently.
**Until the maintainer scopes `nooma-pr`'s rule (e.g., "kept, except inside a stacked-PR chain, where
`delete_branch_on_merge` governs instead"), this task list treats the repository setting as
authoritative for the nineteen links below**, because it is the one that makes the retarget
mechanism this chain depends on actually work, and because the setting is what the owner's own
post-incident fix changed.

---

## Merge order and per-link verification

**Order** (respecting every dependency in the Dependencies list further below; `12d`/`12e` may
merge in either order relative to each other and to `12a`–`12c`; `17` may merge at any point before
`15`, including in parallel with `12a`, per `design.md`'s own note — *"17 can go at any time before
15... a parallel worktree could take it while 12a is in review"*):

```
12a → 12b → 12c → (12d, 12e independent) → 12f-i → 12f-ii → 13a → 12g → 13b → 13c → 13d
  → (17, anywhere before 15) → 15 → 16a-i → 16a-ii → 16b → 14a → 14b
```

**Task 0 — before opening the first PR of this chain.** Confirm `delete_branch_on_merge` is on for
this repository — `gh api repos/rengo/nooma -q .delete_branch_on_merge` must return `true`. Do not
trust the inherited claim that it is on; confirm it fresh, the same discipline
`m1b-pipeline/tasks.md` task 8a.0 used for the `pending-red` ruleset context, because a repository
setting is not mechanically re-verifiable by any Makefile target and can be changed by anyone with
admin access between sessions.

**Before opening any one of the nineteen links' PRs:** default its base to `main` whenever every
dependency it lists is already merged — a PR based on `main` needs no retarget at all, the only
unconditionally safe state to be in when merges happen quickly. Base a link's branch on an
immediate predecessor's still-open branch only when this link's own code genuinely cannot compile
without that predecessor's uncommitted-to-main state (true git stacking) — in this chain, that is
`12a→12b→12c` (three files added to the same new `core/correction` package skeleton in immediate
sequence) and `12f-i→12f-ii` (the second link's code calls into the first's). Every other adjacent
pair in the order above lists a dependency that is expected to already be on `main` by the time the
next link starts, per the Dependencies section, and should be branched from `main`.

**After every merge in this chain, not only the first:**

1. Confirm the merged branch was actually deleted —
   `git ls-remote --heads origin <branch>` returns empty, or
   `gh api repos/rengo/nooma/branches/<branch>` 404s.
2. For every open PR in this chain whose base was the branch just merged, confirm GitHub retargeted
   it to `main` **before** doing anything else with it — `gh pr view <n> --json baseRefName -q
   .baseRefName` must read `main`. If it still reads the deleted branch's name, retarget manually
   with `gh pr edit <n> --base main`, then re-run `make check-all` — a clean textual retarget does
   not prove the combined tree compiles, the same caveat `nooma-pr`'s own Decision Gates table
   states for a manual rebase.
3. Only then start, or continue, the next link's work.

**Never merge two links of this chain within the same short window without step 2 between them.**
The 2026-08-01 incident this instruction is built to prevent: a three-link stack (the shape
`12a→12b→12c` most resembles in this chain) merged in about three minutes, and two of the three
links landed in the wrong place — `#72` merged into `feat/core-classify-salvage` and `#73` into
`feat/core-classify-vocab` instead of both reaching `main`, and `main` kept 274 of the stack's 1565
lines. The root cause was `delete_branch_on_merge` being off, so GitHub never retargeted the
dependent PRs' bases away from the branches that had just merged — task 0 above and step 1 above
exist specifically to keep that precondition from recurring silently.

---

## Package layout (from `design.md` §4, unchanged, cited per task below)

```
internal/core/recall/fuse.go              + FusedCandidate, FuseScored; Fuse a projection   12a
internal/core/correction/  (NEW)          doc.go, referent.go                               12b
                                           edit.go, plan.go                                  12c
internal/ports/unitrepo.go                + UpdateEventAt, UpdateDueAt                       12d
internal/ports/signalrepo.go  (NEW)                                                          12e
internal/ports/decisionlog.go             + ActionCorrectionApplied/Ambiguous               12f-i
internal/ports/embeddingrepo.go           + CountLiveWithoutEmbedding                         16b
internal/store/sqlite/unitrepo.go         + the two UPDATEs                                   12d
internal/store/sqlite/signalrepo.go (NEW)                                                     12e
internal/store/sqlite/embeddingrepo.go    + the LEFT JOIN count                                16b
internal/brain/recall.go                  + embed field, ScoredUnit, ScoredFor, ForText        13a
internal/brain/correction.go  (NEW)       correctionRunner, applyWithPreImage, dispatchEdits 12f-i
                                           + signals.Record call                             12f-ii
internal/brain/capture.go                 + discarded/unparseable/unclassifiable routing       13a
                                           + correction/recall Kind forks                       12g
internal/brain/result.go                  CaptureOutcome, CaptureResult reshaped, Correction    12g
internal/httpapi/server.go, auth.go       Handler(Deps), two muxes, guarded route slice         13b
internal/httpapi/capture.go               POST /capture, total status switch                    13b
internal/httpapi/recall.go                POST /recall; GET /units/{id}; GET /units?ids=        13c
internal/providers/openai/embed.go (NEW)  Embed; ports.EmbeddingProvider                          17
cmd/nooma/serve.go                        wires providers/repos/Index/services/token             13d
cmd/nooma/tasks.go (NEW)                  tasksM1Consumes, the shared list                       13d
cmd/nooma/capture.go (NEW)                HTTP-client subcommand                                14a
cmd/nooma/init.go                         EnvVarName, providerChoice, renderProviders, two paths  15
cmd/nooma/doctor.go                       + quality-gate row                                  16a-i/ii
                                           + two coverage rows                                    16b
docs/adr/0017-http-request-auth.md (NEW)                                                          13b
```

---

## PR 12a — `feat/core-recall-scored` (~320)

Depends on nothing outside this chain beyond Phase A/B (already shipped). **Goes first — the only
slice with no dependency, and it unblocks both `12b` and `13a`.**

- [x] **12a.1** Test first: `internal/core/recall/fuse_test.go` — a hand-computed magnitude table
      asserting each `FuseScored` return equals `Σ w_i/(RRFK + rank_i(d))` computed by hand (the
      only proof of magnitude that exists — the pre-existing suites prove order, never magnitude);
      a property test asserting every returned score is strictly positive, including the
      last-rank-in-one-list case, naming the constants the bound depends on so a future negative
      weight breaks it loudly. **Red**: `undefined: recall.FuseScored`, `undefined:
      recall.FusedCandidate`.
      Implement `internal/core/recall/fuse.go`: `FusedCandidate{ID string; Score float64}`,
      `FuseScored(lists ...[]string) []FusedCandidate`.
      Verify: `make test`; `golangci-lint run`.
      Requirement: R1.2; design D1.
- [x] **12a.2** In the same PR: reimplement `Fuse` as a projection of `FuseScored`, confirming the
      pre-existing `TestFuse_BreaksTiesDeterministically`, `TestFuse_ReproducesADR0010ByHand`, and
      `test/conformance/recall_corpus_test.go` all pass **unedited** — order is proven by these
      suites, magnitude only by 12a.1's new table, per D1's own stated proof split.
      Verify: `make test` — zero edits to any pre-existing test's assertions.
      Requirement: R1.2 (the MUST NOT against forbidding `Fuse`'s internal reimplementation);
      design D1; conflict C5 (`spec.md` revision 3 already closed this point in R1.2's own text).
- [x] **12a.3** doc 02 §5.2 delta (D13, this PR's own row): the scored fusion as a named output of
      the same mechanism, and why every fused score is strictly positive.
      Verify: read the section; `docs-sync.yml` not locally verifiable.
      Requirement: design D13 (`12a` row).
- [x] **12a.4** `internal/core/recall`'s purity and coverage for the new surface.
      Verify: `golangci-lint run`; `make cover`.
      Requirement: R1.14.
- [x] Verify (PR-level): `make check-all`; confirm `git diff --name-only` touches only
      `internal/core/recall/**`, its tests, and `docs/02-cognitive-core.md`.

---

## PR 12b — `feat/core-correction-referent` (~330)

Depends on `12a` (imports `recall.FusedCandidate`).

- [x] **12b.1** Test first: `internal/core/correction/referent_test.go` — zero candidates → `("",
      false)`; one candidate → `(id, true)`; ratios at `1.4999`, `1.5`, `1.5001` (the boundary,
      pinned inclusive on the pick side); a margin ≤ 1 documented as vacuous, not guarded; a third
      candidate that would flip the answer if it participated, asserting it does not. **Red**:
      `undefined: correction.Referent`, `undefined: correction.ReferentMargin`.
      Implement `internal/core/correction/` (NEW package) `doc.go` + `referent.go`: `ReferentMargin
      = 1.5`, `Referent(cands []recall.FusedCandidate, margin float64) (string, bool)`.
      Verify: `make test`; `golangci-lint run` (`depguard`'s `core-purity` allows this new package
      only stdlib + the `internal/core` prefix — confirm no other import).
      Requirement: R1.3, R1.4; design D2.
- [x] **12b.2** `docs/06-harness.md` §1's package tree gains the `correction/` line.
      Verify: read the section.
      Requirement: design D13 (`12b` row).
- [x] **12b.3** doc 02 §5 step 4 delta: the gate's boundary is inclusive, applied to the **live**
      candidates.
      Verify: read the section; `docs-sync.yml`.
      Requirement: design D13 (`12b` row).
- [x] **12b.4** Purity/coverage.
      Verify: `golangci-lint run`; `make cover`.
      Requirement: R1.14.
- [x] Verify (PR-level): `make check-all`.

---

## PR 12c — `feat/core-correction-plan` (~400 estimate, 501 measured — 1.25×; High risk, **not
split** — see forecast and Conflicts §C2; `size:exception` applied)

Depends on `12b` (same new `core/correction` package).

- [x] **12c.1** Test first: `internal/core/correction/edit_test.go` — `AllFields()` completeness:
      for each `Field`, exactly one accessor reports true. **Red**: `undefined: correction.Field`,
      `undefined: correction.Edit`.
      Implement `edit.go`: `Field`, `FieldContent`/`FieldEventAt`/`FieldDueAt`, `AllFields()`, the
      opaque `Edit` type plus its three constructors and three accessors.
      Verify: `make test`.
      Requirement: R1.8 (the edit-plan shape); design D3.
- [x] **12c.2** Test first: `internal/core/correction/plan_test.go` — every row of R1.8's table
      (event-only, due-only, content-fallback, both-dates → ask, no-date-no-content → ask); the
      explicit scenario a date-carrying correction leaves content byte-for-byte untouched. **Red**:
      `undefined: correction.PlanEdit`.
      Implement `plan.go`: `PlanEdit(c classify.Classification) ([]Edit, bool)`.
      Verify: `make test`; `golangci-lint run` (confirm `core/correction` importing `core/classify`
      stays inside `depguard`'s allowed prefix).
      Requirement: R1.8; design D3.
- [x] **12c.3** L2: `test/conformance/` — `PlanEdit` over the corpus's correction cases produces the
      field each case implies; add one new due-date correction case under
      `testdata/classify/cases/`, following `m1b-pipeline`'s own "written once, used in two places"
      discipline.
      Verify: `go test ./test/conformance/...`.
      Requirement: R1.8's own Verified-by; design §7 test matrix (`12c` row).
- [x] **12c.4** doc 02 §5 step 4 delta: which column a correction writes (C2/C6's closed gap), and
      that two dated fields ask.
      Verify: read the section; `docs-sync.yml`.
      Requirement: design D13 (`12c` row).
- [x] **12c.5** Purity/coverage — ≥ 90 % for `internal/core/correction` now that it holds real
      branching logic.
      Verify: `golangci-lint run`; `make cover`.
      Requirement: R1.14.
- [x] Verify (PR-level): `make check-all`; confirm `depguard`/`forbidigo` clean over the whole new
      `correction/` package. **A split was attempted along the `Edit`/`PlanEdit` seam (PRs #108/#109)
      and rejected — see the Review Workload Forecast note and Conflicts §C2. `size:exception`
      applied instead.**

---

## PR 12d — `feat/ports-unit-fields` (~380 — High risk: the store-adapter class historically
overran in `m1a`/`m1b`)

Depends on nothing beyond Phase A (independent of `12a`–`12c` and of `12e`).

- [ ] **12d.1** Test first: extend the shared `repocontract` shape — `UpdateEventAt`/`UpdateDueAt`
      driven with **two distinguishable instants** (the new value vs. the audit timestamp),
      asserting both columns independently and catching a swapped-argument call site; `ErrUnitNotFound`
      for an unknown id. **Red**: the interface addition fails to compile against `memrepo`'s fake
      until it implements both.
      Implement: `internal/ports/unitrepo.go` gains `UpdateEventAt(ctx, id string, eventAt
      time.Time, at time.Time) error`, `UpdateDueAt(ctx, id string, dueAt time.Time, at time.Time)
      error` — non-nullable `time.Time` parameters, per D4's no-clear-the-date reasoning; `memrepo`'s
      fake implements both.
      Verify: `go build ./...`; `make test`.
      Requirement: R1.7; design D4.
- [ ] **12d.2** L3: `internal/store/sqlite/unitrepo.go` implements both methods against a real
      migrated vault, the same two-distinguishable-instants contract case answered again in SQLite
      — C11's lesson from `m1b-pipeline` ("answered twice — `memrepo` and `internal/store/sqlite`,
      in the same PR").
      Verify: `go test ./test/integration/... -tags integration`.
      Requirement: R1.12 (this port's half); design D4.
- [ ] **12d.3** `make store-api-golden`; confirm `git diff` over `internal/store/sqlite/migrations/`
      is empty — repository methods against existing columns, no new migration.
      Verify: `TestHarness_StoreAPIUnchanged` against the regenerated golden; the empty migration
      diff.
      Requirement: R1.12.
- [ ] Verify (PR-level): `make check-all`; confirm this PR's diff is additions to
      `internal/ports/unitrepo.go` and `internal/store/sqlite/unitrepo.go` only (R7.3/R7.4).

---

## PR 12e — `feat/ports-signalrepo` (~400 — at the ceiling; High risk: a new port plus its first
L3 case)

Depends on nothing beyond Phase A/B (independent of `12a`–`12d`).

- [ ] **12e.1** Test first: a `repocontract`-shared case for `ports.SignalRepo` — `AllSignalTypes()`/
      `AllValences()` closed-vocabulary completeness; `Record`/`Since` round-trip. **Red**:
      `undefined: ports.SignalRepo`, `undefined: ports.Signal`.
      Implement `internal/ports/signalrepo.go` (NEW): `SignalType` (the eleven members migration
      0002's own comment enumerates), `Valence` (positive/negative/neutral), `TargetKind`,
      `Signal{... TargetID *string /* NO FK */ ...}`, `SignalRepo{Record, Since}`; `memrepo` gains a
      `Signals` fake.
      Verify: `make test`; `golangci-lint run` (no `Delete*`-prefixed method, CLAUDE.md
      non-negotiable #6).
      Requirement: R1.10; design D6.
- [ ] **12e.2** L3 — I13's behavioural half: `internal/store/sqlite/signalrepo.go` implements
      `Record`/`Since` against a real migrated vault with `foreign_keys=on`; a signal whose
      `TargetID` names a unit that was **never created** persists and reads back.
      Verify: `go test ./test/integration/... -tags integration`.
      Requirement: R1.10's Verified-by; design D6; `docs/06-harness.md` §4's I13 framing.
- [ ] **12e.3** `make store-api-golden`; confirm no migration touched (`learning_signals` already
      exists, migration 0002).
      Verify: `TestHarness_StoreAPIUnchanged`; empty migration diff.
      Requirement: R1.12 (the `SignalRepo` half).
- [ ] Verify (PR-level): `make check-all`; confirm `internal/ports/decisionlog.go`,
      `relationrepo.go`, `provider.go`, `clock.go`, `lexicalsearch.go` are untouched by this PR
      (R7.4).

---

## PR 12f-i — `feat/brain-correction-audit` (~260, first half of the pre-split `12f`)

Depends on (`12c`, `12d`, `12e`) all merged to `main`.

- [ ] **12f-i.1** Test first (I23, the RED-first audit-failure test ADR-0016 names): `test/conformance/`
      — a `DecisionLog` fake configured to fail `Record`, driving a correction attempt, asserting no
      `Update*` call reaches `ports.UnitRepo` and the target unit's stored content/dates are
      unchanged afterward. **Red**: compile failure against the not-yet-existing
      `internal/brain/correction.go`.
      Implement `internal/brain/correction.go`: `correctionRunner{units, log, signals, ids,
      recall}`, `applyWithPreImage` (Layer 1 — the one door; ADR-0016's ordering: `recordPreImage`
      first, `dispatchEdits` only on success), `dispatchEdits` (a total switch over
      `correction.Field`). Two new `ports.DecisionAction` members —
      `ActionCorrectionApplied`, `ActionCorrectionAmbiguous` — the sanctioned R7.4 edit to
      `internal/ports/decisionlog.go`.
      Verify: `make test`.
      Requirement: R1.9; design D5 Layers 1 and 3.
- [ ] **12f-i.2** L2 AST guard (Layer 2). **No natural pre-implementation red** — it proxies over
      code that is already correct by construction the moment `applyWithPreImage` exists, the same
      category `m1b-pipeline` task 10b.2 established. Walks every non-test file under `internal/`
      except `internal/store/**`/`internal/ports/**`, failing if (a) any call to
      `UpdateContent`/`UpdateEventAt`/`UpdateDueAt` occurs outside `dispatchEdits`, or (b)
      `applyWithPreImage`'s body calls `dispatchEdits` at a statement index lower than
      `recordPreImage`, or either call is absent. State the no-natural-red fact in the guard's own
      doc comment; its verify step must perform a **temporary-break check** — introduce the
      violation it exists to catch, confirm it reports, then revert before committing.
      Verify: `go test ./test/conformance/...`; the temporary-break check performed and reverted,
      recorded in the commit message.
      Requirement: design D5 Layer 2.
- [ ] **12f-i.3** The pre-image shape: a successful correction writes exactly one `decision_log` row
      (`correction.applied`) whose `context` carries `{unit_id, fields, previous, next, referent}`
      per D5's JSON shape, `previous`/`next` keyed by column name.
      Verify: `go test ./test/conformance/...` — `context.previous` equals what `ByID` returned
      before the edit.
      Requirement: R1.9; design D5.
- [ ] **12f-i.4** doc 02 §5 step 4 delta: ADR-0016's settled `context` JSON shape (not forced by
      `docs-sync` per R1.13, carried anyway per design's own choice — D13's row for `12f`).
      Verify: read the section; `docs-sync.yml`.
      Requirement: R1.13; design D13.
- [ ] Verify (PR-level): `make check-all`; confirm this PR's diff to
      `internal/ports/decisionlog.go` is exactly the two new `DecisionAction` members and nothing
      else in that file changes.

---

## PR 12f-ii — `feat/brain-correction-signal` (~180, second half of the pre-split `12f`)

Depends on `12f-i` (stacked — extends `applyWithPreImage`'s own call site).

- [ ] **12f-ii.1** Test first: after `12f-i`'s pre-image write and every `Update*` call have both
      succeeded, `correctionRunner` calls `signals.Record` — a `learning_signals`-shaped row via
      `ports.SignalRepo` (`12e`) with `signal_type = "correction"`, `target_kind = "unit"`,
      `target_id` = the referent unit's id, `valence = negative`, `context = {unit_id, fields,
      decision_id}` where `decision_id` names the accompanying `correction.applied` row. Written
      **after** the edits, never for a correction that did not land (D6's own reasoning: a signal for
      a failed edit would teach a future learning pass from an event that did not occur). **Red**:
      compile failure — `correctionRunner` has no `signals` call site yet beyond the field itself.
      Verify: `make test`.
      Requirement: R1.10; design D6.
- [ ] **12f-ii.2** Confirm this PR is the first in the whole `m1-capture-recall` umbrella to write
      to `learning_signals` at all (`m1b-pipeline` R8.1's own deferral).
      Verify: review; `i13_learning_signal_test.go`'s existing DDL check stays unaffected — this PR
      exercises the write path that test's schema fact protects, not a change to the schema.
      Requirement: R1.10.
- [ ] Verify (PR-level): `make check-all`.

---

## PR 13a — `feat/brain-recall-fortext` (~380)

Depends on `12a` (merged). **Lands here in merge order, before `12g`, even though its umbrella
number (13) is higher than `12g`'s (12) — the one ordering in this chain that inverts its own
numbering, called out explicitly rather than left implicit, in the same category
`m1b-pipeline`'s C1 already named.** `12g`'s correction-referent resolution needs `ScoredFor`
before it can be written at all, so `13a` must merge first.

- [ ] **13a.1** Test first (I22, a new invariant — register it in `docs/06-harness.md` §4 before
      writing this test, per `nooma-testing`'s execution step 2): `test/conformance/` — one text
      driven once through `CaptureService` (classified `recall`) and once through
      `RecallService.ForText`, asserting the two ordered candidate-id lists are equal; a second case
      where the embedding leg fails and **both** entrances degrade identically (lexical leg alone,
      `semantic_leg_available: false`). **Red**: `undefined: brain.RecallService.ScoredFor`,
      `undefined: ...ForText`.
      Implement: `internal/brain/recall.go` gains an `embed ports.EmbeddingProvider` field,
      `ScoredUnit{Unit, Score}`, `ScoredFor(ctx, text) ([]ScoredUnit, bool, error)`,
      `ForText(ctx, text) ([]unit.Unit, bool, error)` — the argument is always the **raw** text,
      never `NormalizedContent` (D9's forced design, since `/recall` never calls classify).
      `captureRunner` now holds **one** `RecallService` instance instead of constructing one per
      capture (fixing `capture.go:305`).
      Verify: `make test`.
      Requirement: I22 (new; R8.1's L2 assignment); design D9.
- [ ] **13a.2** Wire the three orphan actions that are this phase's job to give callers, the
      discard/unparseable/unclassifiable half (D8) that belongs with recall/routing rather than
      `12g`'s correction/outcome-vocabulary work: `chitchat`/`out_of_scope` classification →
      `OutcomeDiscarded`, one `capture.discarded` row; `Decode` returning `ErrNoFieldsSalvaged` →
      `capture.classify.unparseable`; `c.Kind == nil` → `capture.classify.unclassifiable`.
      (`capture.dedup.judged` stays an orphan, per `m1b-pipeline`'s own C14b.)
      Verify: `make test` — a conformance test per orphan action confirming it is now called
      outside `test/support/repocontract`.
      Requirement: design D8 (the three-orphan-actions half not owned by `12g`).
- [ ] **13a.3** `docs/06-harness.md` §4 registers I22 before its test is written — confirm task
      13a.1 followed this order.
      Verify: read the invariant table — I22 present with its doc 02 §5 step 2 reference.
      Requirement: design D14.
- [ ] Verify (PR-level): `make check-all`; confirm `git diff --name-only` touches only
      `internal/brain/recall.go`, the discarded/unparseable/unclassifiable arms of
      `internal/brain/capture.go`'s routing fork (not the correction/recall `Kind` forks — those
      are `12g`'s), and `docs/06-harness.md`.

---

## PR 12g — `feat/brain-correction-route` (~400 — at the ceiling; High risk, named explicitly by
`design.md` as one of the rows in the estimate band)

Depends on (`12f-ii`, `13a`) both merged.

- [ ] **12g.1** Test first: R1.1 — a conformance test driving a `correction`-classified capture,
      asserting `ToUnit` is never called and `Create` is never called for it.
      Implement: capture's `Kind`-based routing fork for `correction` (mirroring
      `m1b-pipeline` R4.6's own timer-refusal fork), before `ToUnit` is ever reached.
      Verify: `make test`.
      Requirement: R1.1.
- [ ] **12g.2** Test first: R1.5 — an explicit `unit_id` override: `CaptureInput` gains
      `ReferentID`; when non-empty and the classification is `correction`, capture uses
      `UnitRepo.ByID` directly and does **not** run recall at all (an instrumented index that fails
      the test if queried); an unknown explicit id returns an error and edits nothing.
      Verify: `make test`.
      Requirement: R1.5; design D7.
- [ ] **12g.3** Test first: R1.6 — chat-path referent resolution: no explicit id →
      `RecallService.ScoredFor(ctx, in.Text)` (`13a`, raw text) → `correction.Referent` (`12b`)
      gated by `ReferentMargin`, computed over the **live** candidates only (after `LiveByIDs` — a
      `superseded` top scorer is dropped and the ratio recomputed over the survivors, I02); *ask* →
      capture edits no unit, writes `correction.ambiguous`, returns `OutcomeAsked`; *pick* →
      proceeds to `PlanEdit`. Includes R1.6's own two-units-ambiguous scenario.
      Verify: `make test`.
      Requirement: R1.6; design D2, D7, D9.
- [ ] **12g.4** Test first: R1.8's orchestration half — `correctionRunner.at` calls
      `correction.PlanEdit(c)` (`12c`); a `false` result (both dates, or neither) writes
      `correction.ambiguous` and returns `OutcomeAsked`; a `true` result calls
      `applyWithPreImage(target, plan, ref, now)` (`12f-i`/`12f-ii`).
      Verify: `make test`.
      Requirement: R1.8; design D3, D7.
- [ ] **12g.5** `CaptureOutcome`/`CaptureResult` reshaped (D8): the closed vocabulary (`Stored`,
      `Deferred`, `Discarded`, `Recalled`, `Corrected`, `Asked`), `AllCaptureOutcomes()`. `Stored
      bool` is **replaced**, not joined — Phase B tests asserting `Stored: true/false` are edited in
      this PR to assert `Outcome` instead (assertion-renaming only, never a weakened conformance
      claim, per C7's own cost pricing).
      Verify: `make test` — full suite green, including the edited Phase B assertions.
      Requirement: design D8 (C7's resolution).
- [ ] **12g.6** I03's correction half: a conformance test asserting a correction is an UPDATE — the
      unit count is unchanged, the id survives, no `Create`, no `SetStatus`/`Delete*`/`Remove*`/
      `Purge*`/`Drop*`/`Destroy*`-prefixed call.
      Verify: `go test ./test/conformance/...` — the existing `i03_units_never_deleted_test.go`
      stays unchanged and green against this PR's new files.
      Requirement: R1.11.
- [ ] Verify (PR-level): `make check-all`; confirm `12g` and `13a` together are the only routing
      changes to `internal/brain/capture.go`.

---

## PR 13b — `feat/httpapi-capture` (~450 — over the ceiling; High risk; **not split, on
purpose** — R2.9's own MUST forbids it, see intro)

Depends on (`12g`, `13a`).

- [ ] **13b.1** `docs/adr/0017-http-request-auth.md` — new ADR recording the decision that every
      API route requires a bearer token whenever one is configured; ADR-0007 unedited, not
      superseded; `docs/adr/README.md`'s index gains its row.
      Verify: read the ADR; the index row present.
      Requirement: R2.9.
- [ ] **13b.2** Test first: `internal/httpapi/auth.go` — `ResolveToken(cfg, lookup) (string, bool)`,
      reading the same `server.auth_token_env` variable `DecideBinding` reads; `requireToken(token)`
      middleware; a truth-table test sweeping the same `(bind, auth_token_env, env-set?)`
      combinations `binding_test.go` already exercises, asserting the middleware is a no-op **only**
      when the effective bind is loopback, for every combination where `DecideBinding` actually
      succeeds. **Red**: `undefined: httpapi.ResolveToken`, `undefined: httpapi.requireToken`.
      Verify: `make test`.
      Requirement: R2.10; design D10.
- [ ] **13b.3** Test first: a completeness test iterating **one** declared route-table slice
      (consumed both by registration and by this test — D10's "one slice, two consumers" shape),
      asserting every entry returns 401 with no token and with a wrong token when a token is
      configured, and that both responses are **byte-identical** (R2.11's own MUST NOT against an
      oracle) — compared to each other, not to a literal. The comparison uses
      `crypto/subtle.ConstantTimeCompare`.
      Verify: `make test`.
      Requirement: R2.11.
- [ ] **13b.4** `Handler(Deps)` — the two muxes (open: `GET /{$}`, `GET /ui`; guarded: everything
      else); `POST /capture` wired to `CaptureService.Capture` unchanged, mapping every
      `AllCaptureOutcomes()` member to a status code via a total switch (`stored` → 201, every other
      outcome → 200 with a body naming what happened, provider failures → 502, store failures →
      500; `deferred` is **not** an error status). A completeness test over `AllCaptureOutcomes()`
      fails loudly if any member has no mapping.
      Test first: a handler test driving one ordinary capture (201/stored), one timer-classified
      message (the refusal's plain-words message verbatim, R2.2), and the completeness test.
      Verify: `make test`.
      Requirement: R2.1, R2.2; design D10.
- [ ] **13b.5** R2.12 review checkpoint: no cookie-setting or session code path exists in this PR;
      `GET /` and `GET /ui` stay reachable without a token regardless of whether one is configured.
      Verify: review; a route test confirming both routes succeed with no `Authorization` header
      even when a token **is** configured.
      Requirement: R2.12.
- [ ] Verify (PR-level): `make check-all`; confirm `git diff --name-only` for this PR contains
      `docs/adr/0017-http-request-auth.md` and the middleware landing together with `POST
      /capture`'s mount — never the middleware in a later PR (R2.9's own Verified-by). **Stop-and-report
      checkpoint once this PR's own cumulative diff crosses roughly 300 lines**, per the Review
      Workload Forecast below; no valid split line exists inside this PR's own scope.

---

## PR 13c — `feat/httpapi-recall-units` (~330)

Depends on `13b`.

- [ ] **13c.1** Test first: `POST /recall` — embeds the query via `ports.EmbeddingProvider` and
      calls the same `RecallService.ForText` capture already uses (`13a`); a test asserting no LLM
      completion call occurs (`fakeprovider` configured with zero scripted `capture_processing`
      cases still succeeds) — no classify call on the read path.
      Verify: `make test`.
      Requirement: R2.4.
- [ ] **13c.2** Test first (Q3b's conformance property, R2.5): seeding `memrepo`/`fakeprovider` with
      the same corpus, driving one capture classified `recall` and one standalone `POST /recall`
      over identical text, asserting the two ordered candidate-id lists are equal.
      Verify: `make test`.
      Requirement: R2.5.
- [ ] **13c.3** `GET /units/{id}` and `GET /units?ids=a,b,c` through `LiveByIDs` (never `ByID`
      exposed over HTTP); a non-`pool` unit returns the **same** 404 shape an unknown id would
      (I02); a shared unit renderer (`id`/`type`/`content`/`status`/`weight`/`event_at`/`due_at`/
      `created_at`/`updated_at` only).
      Test first: a route test seeding one `pool` unit and one `archived` unit, asserting only the
      `pool` unit's route responds successfully and the archived unit's by-id request returns the
      identical not-found shape an unknown id would.
      Verify: `make test`.
      Requirement: R2.6.
- [ ] **13c.4** R2.7: a route test asserting `DecisionLog.Record` is never called for `POST /recall`
      or `GET /units/{id}` (an instrumented fake failing the test if `Record` is invoked), asserted
      over all four routes.
      Verify: `make test`.
      Requirement: R2.7.
- [ ] **13c.5** L4: `test/e2e` — at least one test starting the compiled `nooma serve` binary
      against a real, migrated, fixture-configured vault, posting a capture, and issuing a recall
      that finds it.
      Verify: `go test ./test/e2e/... -tags e2e`.
      Requirement: R2.8.
- [ ] Verify (PR-level): `make check-all`.

---

## PR 13d — `feat/serve-wiring` (~420 — over the ceiling; High risk; **not split, on purpose** —
no natural seam exists between wiring and the shared list it introduces, see intro)

Depends on `13c`.

- [ ] **13d.1** `cmd/nooma/serve.go` wires config→providers→repos→`Index`→services→token into
      `Handler(Deps{Version, Capture, Recall, Token})` — the guarded route slice built from this
      wiring, not reconstructed per request.
      Test first: extend `13c.5`'s L4 test so it exercises `serve.go`'s real wiring rather than a
      handler test's manually-built `Deps`.
      Verify: `go test ./test/e2e/... -tags e2e`.
      Requirement: R2.8 (this PR's wiring share); design D10.
- [ ] **13d.2** `cmd/nooma/tasks.go` (NEW): `tasksM1Consumes = []string{"capture_processing",
      "relation_evaluation", "embedding"}` — the one list three readers share (D18a). This PR's own
      reader: `serve` resolves exactly these into `CaptureService`/`RecallService`'s ports.
      Test first: an L2 test asserting `serve`'s wiring reads `tasksM1Consumes` rather than a
      hardcoded list, and every member is in `config.DocumentedTaskNames`.
      Verify: `make test`.
      Requirement: design D18a (this PR's reader; `init`'s and `doctor`'s readers land in `15` and
      `16b`).
- [ ] Verify (PR-level): `make check-all`; L4 green. **Stop-and-report checkpoint once this PR's own
      cumulative diff crosses roughly 300 lines.**

---

## PR 17 — `feat/openai-embeddings` (~200)

Depends only on Phase A PR 6 (already shipped). **Can land at any point before `15`, including in
parallel with `12a`**, per design's own note; positioned here in the primary order for narrative
continuity with the chain design walks, not because anything forces this slot.

- [ ] **17.1** Test first: `internal/providers/openai/embed_test.go` against `httptest` — request
      path `/v1/embeddings`, `Authorization: Bearer <key>`, request body's `model`/`input`, response
      decode, **the echoed model is what is returned** (not the request's), empty `data` → error,
      non-200 → error carrying the body.
      Implement `internal/providers/openai/embed.go`: `embedRequest`, `embedResponse`, `(c
      *Client) Embed(ctx, req) (ports.EmbedResponse, error)`; `var _ ports.EmbeddingProvider =
      (*Client)(nil)`.
      Verify: `make test`.
      Requirement: R6.1; design D17.
- [ ] **17.2** A provider entry's `endpoint` reaches the client as its `baseURL` (the existing
      `config.Provider.Endpoint` field, no schema change); an empty one falls back to the provider's
      default (`https://api.openai.com`) — this is what makes R6.3's L4 form possible in `15`.
      Verify: `go test ./cmd/nooma/...`.
      Requirement: design D17 (the `Endpoint`→`baseURL` passthrough).
- [ ] Verify (PR-level): `make check-all`; confirm `git diff --name-only` for this PR contains no
      path under `cmd/nooma/` (R6.2's own MUST — PR 17 touches no wizard code).

---

## PR 15 — `feat/init-provider-paths` (~400 — at the ceiling; High risk)

Depends on `17` (merged) — `6 → 17 → 15`.

- [ ] **15.1** Test first: `cmd/nooma/init.go` — `NewEnvVarName(s) (EnvVarName, error)` rejecting
      real-shaped API keys (`sk-ant-api03-…`, `sk-proj-…`) and accepting real POSIX-shaped variable
      names (`^[A-Z_][A-Z0-9_]*$`).
      Verify: `make test`.
      Requirement: R4.3; design D15.
- [ ] **15.2** Test first: `renderProviders(choices []providerChoice) string` — the yml renderer
      whose declared parameters contain no field typed to hold a raw secret
      (`providerChoice{Type, Model, APIKeyEnv EnvVarName, BaseURL}`); a wizard-flow test driving the
      Cloud path with scripted input, asserting the resulting `nooma.yml` carries two `openai`
      provider entries (chat model + embedding model) and `tasks:` bindings covering
      `capture_processing`, `relation_evaluation`, `embedding` — **this PR's diff performs the R6.2
      binding**; `17` touches no `cmd/nooma` file.
      Verify: `make test`.
      Requirement: R4.1, R4.2, R6.2; design D15.
- [ ] **15.3** Test first: the Ollama path binds the same three tasks to one `ollama` entry (or two
      if the user names a distinct embedding model); no llama.cpp/embedded option offered
      (ADR-0002's "the embedded option is discarded").
      Verify: `make test`.
      Requirement: R4.1, R4.2.
- [ ] **15.4** Test first: `TestFreshVaultIsLoadable` extended to a wizard-populated vault for both
      paths — `config.Decode`/`cfg.Validate` succeed; every task the wizard binds names a provider
      present in the `providers:` map it also wrote (`checkTaskProviders`).
      Verify: `make test`.
      Requirement: R4.4.
- [ ] **15.5** R6.3's L2 half — the observable-embedding property at build time: a wired pipeline
      with `tasks.embedding` bound returns `Embedded: true`; the same pipeline without it returns
      `false` — the two are distinguishable.
      Verify: `go test ./test/conformance/...`.
      Requirement: R6.3 (L2 half, per N2 — cheap once `17.2`'s `endpoint` passthrough exists).
- [ ] **15.6** R6.3's L4 half — a wizard-written Cloud vault whose `openai` entries point at a
      loopback `httptest` server, driven through the compiled binary, leaves a `unit_embeddings`
      row. Not a duplicate of `15.5` — L2 proves the distinction is observable in the wired
      pipeline, L4 proves it survives being wired together for real (N2).
      Verify: `go test ./test/e2e/... -tags e2e`.
      Requirement: R6.3 (L4 half; R8.1's level assignment).
- [ ] **15.7** R4.3's structural no-secret guarantee, L4: a wizard run with a scripted key value
      asserts the literal key string appears nowhere in the written `nooma.yml`, while `.env`
      carries it at `0o600`.
      Verify: `go test ./test/e2e/... -tags e2e`.
      Requirement: R4.3.
- [ ] **15.8** doc 01's `nooma init` row: already accurate per D13, no delta required.
      Verify: review.
      Requirement: design D13 (`15` row).
- [ ] Verify (PR-level): `make check-all`; R4.5 confirmed by review — no test in this PR opens a
      network connection.

---

## PR 16a-i — `feat/doctor-quality-gate` (~280, first half of the pre-split `16a`: "does the check
work at all")

Depends on `13d` (merged), per `design.md`'s own dependency line.

- [ ] **16a-i.1** Test first: `cmd/nooma/doctor.go`'s `doctorChecks` gains one new `{name, run}`
      entry; a test asserting `doctorChecks` grew by exactly one entry and every existing check's
      behavior is unchanged.
      Verify: `make test`.
      Requirement: R5.1.
- [ ] **16a-i.2** Test first: a table test over scripted `fakeprovider` responses — a clean pass
      (zero degradations); a response with only an optional field absent (still a clean pass,
      Refinement 1); each I14 degradation shape (truncated, wrong-typed, unknown-enum), asserting
      the report names the field, the `Reason`, and the task (Refinement 2); one task passing and a
      different task failing reported separately (R5.2); the corpus's `prompt` field sent verbatim,
      never `response`/`expected` (R5.3); each corpus prompt sent exactly once, no retry; the
      report's `k of n` count line.
      Verify: `make test`.
      Requirement: R5.2, R5.3, R5.4.
- [ ] Verify (PR-level): `make check-all`; confirmed by review — every test for this check goes
      through `fakeprovider`, none opens a network connection (R5.5).

---

## PR 16a-ii — `feat/doctor-quality-gate-edges` (~150, second half of the pre-split `16a`: "does it
handle the edge states correctly")

Depends on `16a-i` (stacked — extends the same `doctorChecks` row's decision logic).

- [ ] **16a-ii.1** Test first: a transport-level failure for one task's provider reports
      **unreachable**, distinct in wording/category from a JSON-degradation failure; the live call
      is timeout-bounded.
      Verify: `make test`; review confirming the live call is timeout-bounded.
      Requirement: R5.7.
- [ ] **16a-ii.2** Test first: zero configured tasks (a freshly `init`ed vault) reports zero
      failures with the count line stated even at zero (`ok (0 tasks configured)`); a review
      confirming the no-op is structural — the gate iterates the vault's configured `tasks:`
      bindings, no `len(tasks) == 0` branch anywhere in the check's own control flow;
      `test/e2e/doctor_test.go`'s `TestDoctorOnAHealthyVault` stays green with no change required by
      this PR.
      Verify: `make test`; `go test ./test/e2e/... -tags e2e` (`TestDoctorOnAHealthyVault`
      unchanged); review.
      Requirement: R5.6.
- [ ] **16a-ii.3** R5.8: confirm `testdata/llm/cases/` holds at least one case tagged with each task
      this gate checks (`capture_processing`, `relation_evaluation`) as the corpus stands at this
      PR's own start; add any missing case.
      Verify: `test/support/goldenset.Load` succeeds; review of coverage before this PR's own check
      is written against it.
      Requirement: R5.8.
- [ ] **16a-ii.4** doc 01's `nooma doctor` row gains the quality gate as a named check.
      Verify: read the section.
      Requirement: design D13 (`16a` row).
- [ ] Verify (PR-level): `make check-all`.

---

## PR 16b — `feat/doctor-coverage` (~330 — carries **both** D18b rows per C1's resolution above;
Medium-High risk)

Depends on (`15`, `16a-ii`) both merged.

**C10 resolved (see Conflicts § C1): `spec.md` revision 4's R7.3/R7.4 already sanctions the
`ports.EmbeddingRepo` edit D18b row 2 needs. This slice ships both rows, matching design's own
larger-branch estimate.**

- [ ] **16b.1** D18a's third reader and the shared-list L2 guard: `doctor`'s coverage check reads
      `tasksM1Consumes` (`13d.2`) rather than restating it; an L2 test asserting all three readers
      (`serve`'s wiring, `init`'s wizard, `doctor`'s coverage check) read the same list, and every
      member is in `config.DocumentedTaskNames`.
      Verify: `make test`.
      Requirement: design D18a.
- [ ] **16b.2** D18b row 1 — task coverage (a pure configuration read, no provider call): a
      `doctorChecks` row iterating `tasksM1Consumes` against the vault's `tasks:` bindings — no
      providers configured at all → `ok (no providers configured)`; every member bound → `ok`; a
      member unbound → **FAIL**, naming the task and what degrades (for `embedding`: "capture will
      store units with no vector and recall will run on its lexical leg alone"). Design states
      this is the row that "would have caught C9 before a single capture ran."
      Verify: `make test`.
      Requirement: design D18b row 1.
- [ ] **16b.3** D18b row 2 — vault coverage: `ports.EmbeddingRepo` gains
      `CountLiveWithoutEmbedding(ctx) (int, error)` (the R7.3/R7.4-sanctioned edit); a `LEFT JOIN`
      from `units` where `status = 'pool'` and `unit_embeddings.unit_id IS NULL`. Zero → `ok`; above
      zero → **FAIL**, naming the count ("N live units have no embedding; semantic recall cannot
      reach them").
      Test first: L2 against a `repocontract`-shared fake (`memrepo`'s `EmbeddingRepo` fake gains the
      count method); L3 against a real vault holding both embedded and unembedded live units,
      confirming archived units are excluded (I02's own read-side filter).
      Verify: `go test ./internal/store/... -tags integration`; `go test
      ./test/support/repocontract/...`.
      Requirement: R6.3 (the runtime consistency-method half); design D18b row 2; R7.3/R7.4's
      sanctioned exception.
- [ ] **16b.4** `make store-api-golden`; confirm no migration touched.
      Verify: `TestHarness_StoreAPIUnchanged`; empty migration diff.
      Requirement: R1.12-shaped golden-regeneration obligation, applied to this port edit.
- [ ] **16b.5** `docs/03-data-model.md:306-307`'s promise corrected to name which half exists (units
      ↔embeddings now checked; units↔fts stays M6's).
      Verify: read the line.
      Requirement: design D13 (`16b` row).
- [ ] Verify (PR-level): `make check-all`; confirm this PR's diff to
      `internal/ports/embeddingrepo.go` is exactly the one new `CountLiveWithoutEmbedding` method —
      the third and last of R7.4's three sanctioned edits — and nothing else in that file changes.

---

## PR 14a — `feat/cli-capture` (~350)

Depends on **all five** of (`13d`, `15`, `16a-ii`, `16b`, `17`) — the proposal's own `(13,15,16,17)
→ 14` line, with `13d`/`16a-ii`/`16b` standing in for their umbrella PRs' completion.

- [ ] **14a.1** Test first: `cmd/nooma` gains a `capture` subcommand in `main.go`'s dispatch table
      (following `init`/`status`/`doctor`/`serve`'s own convention); it sends `POST /capture`
      against a running `nooma serve` instance, reading `nooma.yml` only to resolve the bind address
      (no lock taken); a wildcard bind (`0.0.0.0`, `::`) dials `127.0.0.1`, never the wildcard
      literal.
      Driven against an `httptest`-backed fake server: the request/response shape.
      Verify: `make test`.
      Requirement: R3.1 (the HTTP-client half); design D11.
- [ ] **14a.2** Test first: the three no-server-reachable diagnoses — not held (fails); held by pid
      N but nothing answers (fails, names the pid); either succeeds (ordinary path) — using
      `vaultlock.ReadHolder`, the same free read `status` already performs.
      Verify: `make test`.
      Requirement: R3.1 (the diagnosis half); design D11.
- [ ] **14a.3** Test first: `httpapi.ResolveToken`'s third reader — `nooma capture` reads
      `server.auth_token_env` the same way the server does; loopback + no `auth_token_env` → no
      header; `auth_token_env` set + variable set → `Authorization: Bearer <value>`; `auth_token_env`
      set + variable **unset** → the CLI refuses **before** sending, naming the variable — never
      sends first and discovers the 401 afterward.
      Verify: `make test`.
      Requirement: R3.1 (the auth half); design D11.
- [ ] **14a.4** MUST NOT check: `nooma capture` never opens the vault's database directly and never
      takes (or attempts) the vault's write lock.
      Verify: review; confirm no call to `vaultlock.Acquire` from `cmd/nooma/capture.go`.
      Requirement: R3.1 (the MUST NOT).
- [ ] **14a.5** L4: a compiled-binary test starting `nooma serve` against a real vault, then running
      `nooma capture "<text>"` against it, asserting a unit was persisted.
      Verify: `go test ./test/e2e/... -tags e2e`.
      Requirement: R3.1's Verified-by, L4 half.
- [ ] **14a.6** doc 01's CLI table gains the `capture` row.
      Verify: read the section.
      Requirement: design D13 (`14a` row).
- [ ] Verify (PR-level): `make check-all`.

---

## PR 14b — `feat/demo` (~180)

Depends on `14a`. **Last link in the chain — the demo is M1's exit criterion.**

- [ ] **14b.1** Test first (L4): the demo captures via `nooma capture` and finds it via recall —
      either a subsequent `nooma capture` classified `recall` (reusing R2.3's routing) or the HTTP
      `/recall` route; no `nooma recall` subcommand required.
      Verify: `go test ./test/e2e/... -tags e2e`.
      Requirement: R3.2.
- [ ] **14b.2** Review: no case in this PR's own demo script or fixture corpus asks the CLI to
      capture a `timer` or `recurring_reminder` message — Q3a's closing sentence, "the demo must not
      be shown a timer."
      Verify: review of the demo script/fixtures this PR adds.
      Requirement: R3.3.
- [ ] **14b.3** The demo walked end to end **by hand**, not only by the L4 test above: two captures
      (API + CLI), one ask, one correction — no timer. `docs/05-build-plan.md`'s M1 section closed;
      `CLAUDE.md`'s status line updated to reflect M1 closed.
      Verify: manual walkthrough, recorded in the PR description; review of the two doc updates.
      Requirement: design's own scope note — "a green suite is necessary and no longer sufficient"
      from PR 13c onward, since a compiled binary exists to point at.
- [ ] Verify (PR-level): `make check-all`; the demo run by hand and reported in the PR body.

---

## Cross-cutting verification (spec §7, §9)

- **R7.1 (no network, no real LLM/embedding provider)**: every PR above whose tests touch a
  provider goes through `test/support/fakeprovider` or `httptest`; reviewed per-PR at each
  PR-level verify line, restated here because it spans the whole chain.
- **R7.2 (only PR 12's slices touch `internal/core/**`, and only `12a`/`12b`/`12c` carry a doc 02
  delta)**: `12d`, `12e`, `12f-i`, `12f-ii`, `12g`, and every PR 13/14/15/16/17 slice touch no
  `internal/core/**` file — confirmed by each PR-level verify line's own scope check.
- **R7.3/R7.4 (Phase A/B surfaces modified by addition only)**: the three sanctioned edits across
  this whole chain are `unitrepo.go`'s two methods (`12d`), `decisionlog.go`'s two actions
  (`12f-i`), and `embeddingrepo.go`'s one method (`16b`, per C1's resolution) — nothing else under
  `internal/ports/**`/`internal/core/**` (beyond `12a`/`12b`/`12c`'s additions)/`internal/store/
  sqlite/**`/`internal/providers/**` changes.
- **R8.2 (every test observed failing first)**: each task above states its own red; verified at
  apply time by the commit sequence.
- **R9.1 (no undo surface, version table, or `corrects` edge)**: no task above persists a previous
  value anywhere beyond `12f-i`'s `decision_log.context` pre-image, or reads it back.
- **R9.2 (no non-goal work)**: no task computes `effective_weight`, implements consolidation,
  arms/fires a trigger or timer, derives a self-belief, or touches Telegram/`reindex`/perception.
- **R9.3 (PR 14 does not absorb PR 15/16/17's scope)**: `14a`/`14b` touch no `cmd/nooma/init.go`
  wizard logic, no `cmd/nooma/doctor.go` check, and no `internal/providers/openai/` embeddings
  code — confirmed by `14a`'s and `14b`'s own PR-level verify lines.

---

## Review Workload Forecast

**Chained PRs recommended: yes — nineteen links, all pre-split by this document before any code
exists**, matching the discipline `m1a-substrate`'s and `m1b-pipeline`'s own retrospectives earned
after PR 2a (2.6×) and PR 7a (2.6×, split mid-flight into three) shipped with no split line drawn
in advance for the latter's original PR.

**400-line budget risk, per link** (ceilings are the lines this document and `design.md` chose to
fit the 400-line soft rule, never predictions):

| Link | Ceiling | Risk |
|---|---|---|
| 12a | ~320 | Low–Medium |
| 12b | ~330 | Medium |
| 12c | ~400, 501 measured (1.25×) | **High, over the ceiling, not split, `size:exception`** — a split along the `Edit`/`PlanEdit` seam was attempted (PRs #108/#109) and rejected by `docs-sync`: `Edit` alone touches `internal/core/**` with no doc 02 delta of its own, since an opaque type with three accessors has no behaviour to document — the delta belongs entirely to `PlanEdit`. See Conflicts §C2 |
| 12d | ~380 | **High** — the store-adapter class (BLOB-free, but two new UPDATEs plus L3) has already overrun in Phase A/B |
| 12e | ~400 | **High** — at the ceiling; a brand-new port plus its first L3 case |
| 12f-i | ~260 | Medium — the audit-ordering half of a pre-split High-risk parent |
| 12f-ii | ~180 | Low–Medium |
| 13a | ~380 | Medium–High — the recall-embedder addition plus three orphan-action wirings |
| 12g | ~400 | **High** — named explicitly by `design.md` as one of the rows in the estimate band |
| 13b | ~450 | **High, over the ceiling, not split** — R2.9's own MUST forbids splitting ADR-0017/the middleware away from `POST /capture`'s mount; treat as a stop-and-report checkpoint at ~300 lines, the same treatment `m1b-pipeline` gave PR 8a for the identical reason |
| 13c | ~330 | Low–Medium |
| 13d | ~420 | **High, over the ceiling, not split** — no clean seam between `serve.go`'s wiring and the shared task list it introduces; stop-and-report checkpoint at ~300 lines |
| 17 | ~200 | Low |
| 15 | ~400 | **High** — at the ceiling; the wizard flow, the L4 walk, and the structural no-secret proof all land together |
| 16a-i | ~280 | Medium |
| 16a-ii | ~150 | Low |
| 16b | ~330 | Medium–High — now carries both D18b rows (C1's resolution); a new SQL count method plus its L3 case |
| 14a | ~350 | Medium |
| 14b | ~180 | Low |

**Decision needed before apply: yes, for three links — `13b`, `13d`, and, as measured rather than
forecast, `12c`.** `13b`/`13d` sit at or over their own 400-line ceiling with no valid split line
inside their own scope, stated above and in the intro before any code existed, the same shape
`m1b-pipeline` PR 8a landed in at 1.23× its ceiling. `12c` reached the same place by a different
route: its own row above did **not** originally flag it as needing this decision ("High — at the
ceiling before any measured multiplier" was a risk flag, not an action flag); `sdd-apply` measured
it at 501 lines and a split was attempted and rejected before landing here as `size:exception`,
alongside `13b`/`13d`. Report all three to the owner as stop-and-report checkpoints once their own
cumulative diff crosses roughly 300 lines — the same threshold `m1a-substrate`'s PR 5a/6a and
`m1b-pipeline`'s own forecast used — rather than pushing through to the ceiling and discovering the
overrun only at PR-open time. Every other link is either comfortably under its ceiling or was
split down to a size where the pre-split itself is the answer.

**On the estimates themselves.** This chain's raw total is **~6,140 budgeted lines across
nineteen PRs** (design's own seventeen-slice total was ~6,120 — splitting `12f` and `16a` redrew
lines, it did not add real work). Applying Phase A/B's own measured band — 1.3×–2.2×, with one
outlier at 2.6× — puts the realistic range at **roughly 8,000–13,500 review lines**, consistent
with the umbrella proposal's own framing of M1 as "two to three times M0" in total size. Every
ceiling above is a target to design against, never a prediction — the lesson `m1b-pipeline`'s own
C8 drew and this document tries not to repeat: **estimate a core PR from its invariant's proof
obligation, not its implementation.** `12c`, `12e`, `12g`, `13b`, `13d`, `15` are this chain's own
version of that band.

---

## Traceability

| Spec section | Requirements | Tasks |
|---|---|---|
| §1 Corrections (PR 12) | R1.1–R1.14 | 12a.1–12a.4, 12b.1–12b.4, 12c.1–12c.5, 12d.1–12d.3, 12e.1–12e.3, 12f-i.1–12f-i.4, 12f-ii.1–12f-ii.2, 12g.1–12g.6 |
| §2 HTTP surface (PR 13) | R2.1–R2.12 | 13a.1–13a.3, 13b.1–13b.5, 13c.1–13c.5, 13d.1–13d.2 |
| §3 CLI surface (PR 14) | R3.1–R3.3 | 14a.1–14a.6, 14b.1–14b.3 |
| §4 Provider configuration (PR 15) | R4.1–R4.5 | 15.1–15.8 |
| §5 Doctor quality gate (PR 16) | R5.1–R5.8 | 16a-i.1–16a-i.2, 16a-ii.1–16a-ii.4, 16b.1–16b.5 |
| §6 OpenAI embeddings (PR 17) | R6.1–R6.4 | 17.1–17.2; R6.2/R6.3 also 15.2/15.5/15.6/16b.3 |
| §7 Cross-cutting constraints | R7.1–R7.4 | Cross-cutting verification section; every PR-level verify line |
| §8 Test levels | R8.1–R8.2 | every "Test first" line; each task's own stated level |
| §9 Boundaries this change must not cross | R9.1–R9.3 | Cross-cutting verification section |
| §10 Open items | (design's choices) | design D2, D5, D6, D10, D15 as cited per task |
