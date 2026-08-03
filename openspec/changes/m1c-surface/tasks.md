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

### C3 — `12e` measured 626 lines against its own ~400 ceiling (1.57×); a layer seam was evaluated and rejected, on different grounds than `12c`'s. **RESOLVED — unsplittable, `size:exception` applied.**

`sdd-apply` measured the complete, green `12e` PR at 626 changed lines: `internal/ports/signalrepo.go`
(133), `test/support/repocontract/signalrepo.go` (153), `test/support/memrepo/signals.go` (66),
`test/conformance/signalrepo_memrepo_test.go` (22), `internal/store/sqlite/signalrepo.go` (186),
`internal/store/sqlite/signalrepo_integration_test.go` (62), `testdata/schema/store_api.golden`
(4). Per this document's own instruction ("if the diff passes ~350 changed lines and you are not
nearly done, STOP and report"), a candidate seam was evaluated before opening the PR, not after.

**The candidate seam**: PR A — `internal/ports/signalrepo.go`, the shared `repocontract.RunSignalRepo`
suite, and `memrepo.Signals` (the L2 answer), 374 lines. PR B — `internal/store/sqlite/signalrepo.go`,
its L3 tests, and the regenerated golden (the L3 answer), 252 lines, based on PR A merged to `main`.

**Why it is not `12c`'s failure mode.** `12c`'s split failed `docs-sync.yml`, which fires only on
`internal/core/**` changes (confirmed directly off `.github/workflows/docs-sync.yml`'s own header
comment). Neither candidate half here touches `internal/core/**` — `12e` is a `ports`/`store` PR,
and `design.md`'s own D13 table assigns it no documentation row (confirmed by reading the table
directly, the same discipline `12d` used) — so the doc-delta ownership test that killed `12c`'s
seam does not apply, and does not settle this one either way.

**What actually kills this seam is C11.** `test/support/repocontract`'s own established convention
(`decisionlog.go`'s and `12d`'s doc comments both state it: "answered twice ... in the same PR") is
that a shared contract's fake and real-store answers land together, not sequentially — merging PR A
alone would leave `main` with `ports.SignalRepo`'s contract answered only by `memrepo.Signals` for
however long PR B took to follow, which is exactly the state C11 named as "that implementation's
opinion," the same defect this design's own D6 section warns `12e` specifically to avoid ("a
contract case whose entire justification is catching a specific mistake" applies here to the whole
port, not one case of it — the L3 case is the one thing an in-memory fake can never prove on its
own). A `size:exception` link that ships both halves together is not a convenience; it is what C11
requires for a brand-new port's very first PR.

**Resolution: `12e` is unsplittable for a reason distinct from `12c`'s, and joins `12c`/`13b`/`13d`
as a flagged High-risk, `size:exception` link.** No PR was opened and closed to arrive at this
finding, unlike `12c` — the seam was evaluated and rejected before any PR existed, per this
document's own stop-and-report instruction. Recorded for whoever next eyes a brand-new port's
"fake here, store there" layer boundary as a split seam: `docs-sync` is not the only gate that can
reject an otherwise clean git split — C11's own same-PR requirement is a second, independent one,
and it applies precisely to the shape (a new port, answered twice) this task list already flags as
High risk for exactly that reason.

### C4 — `spec.md` R1.10 and R1.12 cite `learning_signals` as living in "migration 0001"; it lives in migration 0002. A citation defect, not a scope disagreement.

R1.10's own `MUST NOT` clause reads: *"already structural in the schema (migration 0001, verified:
'NO FK: the signal outlives the target's deletion')."* R1.12's `MUST NOT` clause repeats the same
citation: *"learning_signals and its no-FK target_id column already exist (migration 0001, verified
above)."* Both are wrong: `learning_signals` is declared in
`internal/store/sqlite/migrations/0002_learning_and_search.sql:8-19` — confirmed directly off the
file, not assumed — and is absent from `0001_core_tables.sql` entirely (`rg -n "learning_signals"`
against that file returns nothing). `docs/03-data-model.md`'s own "Learning" section places the same
DDL under no migration number at all, so it does not disambiguate. `design.md`'s D6 section gets
this right — it cites `"the eleven members 0002:10 enumerates"` — and this task list's own `12e.3`
already said "migration 0002" independently, before this citation defect was noticed, which is how
it surfaced: the two documents' migration numbers for the same table disagreed with each other.

**Resolution: not acted on beyond this record.** The defect changes no code and no test — `12e.2`'s
L3 case and `12e.3`'s "no migration touched" check both already targeted the correct file
(`0002_learning_and_search.sql`) regardless of what R1.10/R1.12's prose said, because the
implementation was written against the real DDL, not against the spec's citation of it. Flagged
here per this document's instruction not to resolve a spec/schema disagreement silently, and left
for whoever next revises `spec.md` to correct the two citations from "migration 0001" to "migration
0002."

### C5 — `test/conformance/` cannot reach an unexported symbol it does not itself declare; `12f-i`'s behavioral tests are placed as in-package white-box tests instead

Design D5/D7 name `correctionRunner`, `applyWithPreImage`, `recordPreImage` and `dispatchEdits` in
lower case throughout — a deliberate encapsulation choice (D7: "no `CorrectionService` with its own
`ports.Clock` — it would have no caller" outside `internal/brain`), the same shape `captureRunner`
already has. Task `12f-i.1`/`12f-i.3` both say the behavioral proof (I23's RED-first audit-failure
test, and the pre-image-shape test) lives in `test/conformance/`.

**The two do not fit together.** `test/conformance` is a separate importing package
(`package conformance`), and Go's own visibility rule — not a project convention a design document
could relax — makes an unexported symbol in `internal/brain` unreachable from any package that does
not declare it itself. `captureRunner`'s own precedent survives this because every test that touches
it goes through the exported `CaptureService.Capture`; `correctionRunner` has no equivalent exported
entrance in this link — building one would either export `applyWithPreImage` (defeating the
encapsulation D7 states the reason for) or require the full `.at` routing method (referent
resolution via `RecallService.ScoredFor`, `12g`'s own job, itself blocked on `13a`, which merges
*after* `12f-i` in this chain's own order).

**Resolution: the two behavioral tests (`12f-i.1`, `12f-i.3`) are white-box, in
`internal/brain/correction_test.go` (`package brain`), not `test/conformance/`.** This is not a
convention this project lacks precedent for — `internal/core/**`'s own L1 suites and
`internal/httpapi/{server,binding}_test.go` are both in-package for the identical reason (a suite
needs to reach something the package does not export). The AST guard (`12f-i.2`) is unaffected and
stays in `test/conformance/`: it parses source text via `go/ast`, so it needs no import of
`internal/brain`'s unexported symbols at all — the same reason
`brain_single_clock_read_test.go` already lives there. Both tests still satisfy L2's functional
definition (`docs/06-harness.md` §3: "touches `core` + `brain` with fakes", no I/O, always runs) —
only their literal directory differs from the table's typical example.

### C6 — `12f-i` measured 616 lines against its own ~260 ceiling (2.37×); a Layer-2/Layer-1+3 seam was evaluated and rejected — both halves stay over budget, or strict TDD forbids the split outright

`sdd-apply` measured the complete, green `12f-i` PR at 616 changed lines:
`internal/brain/correction.go` (166), `internal/brain/correction_test.go` (189),
`test/conformance/brain_correction_audit_before_edit_ast_test.go` (224),
`internal/ports/decisionlog.go` (26), `test/support/memrepo/decisionlog.go` (16),
`test/support/repocontract/decisionlog.go` (4). Two contributors this document's own ~260 estimate
did not price: the L2 AST guard's closure-detection and statement-order logic (design's own text
estimated `~140 lines` for a comparable guard; this one's honest-limitation requirements —
refuse-loudly on a closure, refuse-loudly on same-statement ambiguity — measure at 224), and C5's
own in-package test file, whose existence this document's `test/conformance/` instruction did not
anticipate.

**The candidate seam**: PR A — `internal/brain/correction.go` plus `correction_test.go` (Layers 1
and 3, the actual functional guarantee and its behavioral proof), 355 lines. PR B — the AST guard
alone (Layer 2), 224 lines, landing second.

**Why it still fails, on two independent grounds.** First, size: PR A alone is 355 lines, 1.37× the
same ~260 ceiling this whole link is trying to fit under — splitting off the smaller half (PR B)
does not solve the size problem for the larger one. Second, and decisive on its own even if PR A
were small enough: `correction.go` and `correction_test.go` cannot be separated from each other at
all — `12c`'s own "second candidate seam" already ruled this shape out ("core-plus-doc in one PR,
the L2 conformance corpus in a second... would merge behaviour before the conformance test that
proves it, which strict TDD forbids"), and the same reasoning applies symmetrically to implementation
and its own RED-first test. No smaller valid cut exists inside PR A's own scope.

**Resolution: `12f-i` is unsplittable for a reason distinct from `12c`'s and `12e`'s — a size-only
seam candidate exists (the guard is genuinely decoupled from Layers 1/3 at the code level) but does
not clear the ceiling on either side, and joins `12c`/`12e`/`13b`/`13d` as a flagged High-risk,
`size:exception` link.** Recorded for whoever next eyes an AST-guard PR as a size-reduction seam: a
structurally-decoupled second file is necessary but not sufficient evidence a split is worth taking
— check whether the *remaining* half clears the ceiling too, not only whether the two halves compile
apart.

### C7 — `correctionRunner`'s struct shipped in `12f-i` carries only the fields `applyWithPreImage`/`dispatchEdits` use, not design D7's full five-field literal

Design D7's own struct literal declares `correctionRunner{units, log, signals, ids, recall}` as the
type's converged shape across the whole `12f-i`/`12f-ii`/`12g` sequence, and task `12f-i.1`'s own
implementation line quotes it in full. Shipping all five fields in `12f-i` fails `golangci-lint`'s
`unused` check: `recall *RecallService` is never read anywhere in this link's scope (12g's referent
routing is what reads it), which is a lint error, not a style note.

**Resolution: `correctionRunner` grows incrementally, one field per PR that first uses it** — the
same shape design D9 already documents for `captureRunner` ("`rels` and `judge` are this PR's own
two additions, landing where D4's diagram places them"). `12f-i` declares `units`, `log`, `ids` only;
`12f-ii` adds `signals` where its `signals.Record` call is written; `12g` adds `recall` where its
referent-resolution routing reads it. No behavior changes; this is a struct-shape note for whoever
implements `12f-ii` next, so its own diff does not need to discover this pattern from scratch.

### C8 — `12f-ii` measured 235 changed lines against its own ~180 ceiling (1.3×); no seam evaluated separately from `12f-i`'s own C6 finding, because the same reasoning already applies verbatim. **RESOLVED — unsplittable, `size:exception` applied.**

`internal/brain/correction.go` (+79/-22) and `internal/brain/correction_test.go` (+131/-3) — 235
lines, 2 files. Best of the chain's split ratios so far (`12c` 1.25×, `12e` 1.57×, `12f-i` 2.37×),
but still over the ~180 estimate — `12f-ii`'s own ceiling was already the tightest of the nineteen
links, so a small absolute overrun reads as a larger ratio than the same overrun would on a bigger
slice.

**A code/test split was considered and rejected on the identical grounds `12f-i`'s own C6 already
established for this exact file pair**: `correction.go` (the `signals` field, `recordPreImage`'s new
return value, `recordCorrectionSignal`, and `applyWithPreImage`'s new third statement) has no
behavior without `correction_test.go`'s RED-first proof of it, and strict TDD (`CLAUDE.md`
non-negotiable #4) forbids shipping the implementation and the test that proves it in separate
commits regardless of line count — the same rule that closed `12c`'s attempted `Edit`/`PlanEdit`
split and `12f-i`'s attempted Layer-2/Layer-1+3 split. No other seam exists inside `correction.go`
itself: `recordCorrectionSignal` is new code with no caller until `applyWithPreImage`'s own edited
call site reaches it, so the two cannot land as separate reviewable units either.

`size:exception` applied via `gh pr edit <n> --add-label "size:exception"`, verified stuck (see the
PR record below).

### C9 — `13a.1`/`13a.2`'s own prose names things `13a`'s file-ownership table and PR-verify checklist both assign to `12g`. Resolved in the file table's favor; both tasks read narrower than their literal words.

Two separate spots inside link `13a`'s own task block disagree with the rest of the same document.

**First, `13a.2`'s own words**: "chitchat/out_of_scope classification → `OutcomeDiscarded`, one
`capture.discarded` row." `OutcomeDiscarded` is a member of the `CaptureOutcome` closed vocabulary
design D8 defines — and the package-layout table (line ~413) assigns `internal/brain/result.go`
("`CaptureOutcome`, `CaptureResult` reshaped, `Correction`") to `12g` alone, never `13a`. `13a`'s own
PR-level verify line confirms this by omission: the files it requires the diff to touch are
`recall.go`, capture.go's discarded/unparseable/unclassifiable arms, and `docs/06-harness.md` —
`result.go` is not among them. `12g` depends on `13a` (not the reverse), so `13a` cannot reference a
type `12g` has not created yet.

**Second, `13a.1`'s own words**: "one text driven once through `CaptureService` (classified
`recall`) and once through `RecallService.ForText`." `13a`'s own PR-verify line says the diff touches
"the discarded/unparseable/unclassifiable arms of `internal/brain/capture.go`'s routing fork (not the
correction/recall `Kind` forks — those are `12g`'s)" — so the routing fork that would let a
`recall`-classified capture reach `RecallService` through `CaptureService.Capture` itself does not
exist until `12g` merges. The task's own stated RED — `undefined: brain.RecallService.ScoredFor`,
`undefined: ...ForText` — is compile-only, consistent with a test that never depends on that fork.

**Resolution, in the file table's favor both times, because it is the more mechanically binding
statement** (an exhaustive file list a PR-level `Verify` step checks, versus descriptive prose):

- The discard path returns `CaptureResult{Stored: false}` — the existing struct, no new field —
  distinguishable from the timer refusal by its nil `Deferred` (that path already sets a non-nil
  one). `12g.5`'s own task text already prices this exactly: "Phase B tests asserting `Stored:
  true/false` are edited in this PR to assert `Outcome` instead (assertion-renaming only...)" — this
  PR's own new discard test is one more such assertion for `12g.5` to rename, not a scope change for
  either link.
- I22's conformance test proves the "one mechanism" property directly against
  `RecallService.ForText`/`ScoredFor`, through two call sites shaped exactly like each future caller
  (one reading a `CaptureInput.Text` field, one taking a bare query string) rather than through
  `CaptureService.Capture` itself, which cannot yet route a `recall` classification anywhere. Once
  `12g` and `13c` wire their own real entrances, both are constrained to call this same method with
  this same raw-text argument — the property this test pins does not change shape when they do.

Recorded so whoever reads `13a.1`/`13a.2` next does not spend the same time re-deriving that the
package-layout table and the PR-verify checklist govern over the task's own descriptive sentence.

### C10 — `13a` measured 471 changed lines against its own ~380 ceiling (1.24×); a mechanism/orphan-actions seam exists but is not taken, following this chain's own `size:exception` precedent

`internal/brain/recall.go` (+93/-2), `internal/brain/capture.go` (+74/-9),
`test/conformance/i22_recall_one_mechanism_two_entrances_test.go` (168, new),
`test/conformance/capture_orphan_actions_test.go` (93, new),
`test/conformance/recall_writes_no_decision_test.go` (+6/-1), three `testdata/llm/cases/*.json`
fixtures (8 each), `docs/06-harness.md` (+1) — 471 lines across nine files, comment trimming already
applied (the `recordDiscardedDecision`/`recordUnparseableDecision`/`recordUnclassifiableDecision`
trio was collapsed into one shared `recordOrphanDecision` helper specifically to bring this number
down before it was recorded here).

**The candidate seam, along `13a.1`/`13a.2`'s own task boundary**: PR A — the recall mechanism
(`recall.go`'s `ScoredFor`/`ForText`, `captureRunner`'s new `recall` field and the
`NewCaptureService`/`recallCandidates` wiring it requires, the `recall_writes_no_decision_test.go`
signature fix, the I22 test, the `docs/06-harness.md` row), roughly 275 lines. PR B — the three
orphan actions (`capture.go`'s discard/unparseable/unclassifiable routing forks and
`recordOrphanDecision`, the table test, the three fixtures), roughly 196 lines. Both halves would
clear the ~380 ceiling individually (unlike `12f-i`'s C6 finding, where the smaller half alone still
exceeded that link's own ceiling).

**Not taken.** Splitting `13a` into two links would restructure a merge chain design.md and
`tasks.md` both already fix at nineteen ordered links — `12g` is written to depend on "`12f-ii`,
`13a`" as one unit, and this prompt's own framing names this link "eighth of nineteen." Renumbering
the chain to insert a tenth-and-a-half link is a bigger decision than a single link should make on
its own judgment (per this link's own instruction: "propose a seam; do not open two PRs on your own
judgment"). At 1.24× the ceiling, this link's overrun is inside the range this chain has already
absorbed with `size:exception` alone — tighter than `12e` (1.57×) and `12f-i` (2.37×), looser than
`12c` (1.25×) and `12f-ii` (1.3×) only by a hair. `size:exception` applied via `gh pr edit <n>
--add-label "size:exception"`, verified stuck.

**Recorded as an available-but-declined seam**, not a rejected one, for whoever plans the next
revision of this chain: if `13a` is ever re-cut, the mechanism half and the orphan-actions half are
genuinely independent code paths (neither's tests import the other's production symbols) and both
clear budget alone — the only cost of splitting here is chain restructuring, not a size or
strict-TDD obstruction the way `12c`/`12f-i`/`12f-ii` each hit.

### C11 — `spec.md` R2.3/R2.4/R2.5 cite `RecallService.Candidates` for the recall Kind fork; `design.md` D9 (and `13a`'s own shipped code, and I22's own test) say `ScoredFor`/`ForText`. Resolved in design's favor. Also: `12g`'s own numbered tasks never list R2.3 at all, despite the package-layout table, `13a`'s PR-verify line, and C9's resolution all assigning it here.

`spec.md` R2.3's own MUST reads: capture "instead runs the same hybrid-recall mechanism R2.4
exposes… over the classification's text/embedding." R2.4 names the mechanism explicitly:
`brain.RecallService.Candidates`. R2.5 repeats it: "Both paths are the same call into
`brain.RecallService.Candidates` over the same `(content, vector, model)` inputs."

**`Candidates`'s own signature makes this reading impossible for either entrance it is asked to
serve.** `Candidates(ctx, content string, vector []float32, model string, excludeID string)
([]unit.Unit, error)` takes an **already-embedded** vector — its caller already has one (the
ordinary capture path's own `embedAndStore` step). Neither a `type: recall`-classified capture nor
the standalone `/recall` route (`13c`) has a vector to hand it: a correction/recall-classified
capture never reaches `embedAndStore` at all (R1.1/R2.3's own routing, before `ToUnit`), and
`/recall` is standalone by design (Q3b: "no classify call on the read path," so nothing embeds a
query before this route runs). Calling `Candidates` from either entrance would require each one to
embed the text itself first — which is precisely `RecallService.ScoredFor`/`ForText`'s own job,
design D9's later mechanism, built for exactly this reason: *"the argument is `in.Text`… `/recall`
is standalone (Q3b): it never calls classify, so it has only the raw query."* `13a` already shipped
`ScoredFor`/`ForText` against this reading, and I22's own conformance test (`13a`, revised by this
PR per the apply prompt's own instruction) pins the "one mechanism" property against exactly these
two methods, never `Candidates`.

**Resolution: implemented per design D9/I22 — `RecallService.ForText`, not `Candidates`.** This is
the same stale-citation pattern C1, C9 and C10 already found in this document and its siblings: an
earlier requirement text citing a mechanism a later design decision superseded, never corrected in
the spec itself. Flagged here per this document's own instruction not to resolve a spec/design
disagreement silently, and left for whoever next revises `spec.md` to correct R2.3/R2.4/R2.5's own
citations from `Candidates` to `ScoredFor`/`ForText`.

**Second, smaller finding in the same area**: `12g`'s own numbered task list (`12g.1`–`12g.6`)
never states a task for R2.3 at all — every one of its six tasks is R1.x (the correction path).
Yet the package-layout table (`internal/brain/capture.go … + correction/recall Kind forks   12g`),
`13a`'s own PR-verify line ("not the correction/recall `Kind` forks — those are `12g`'s"), and C9's
own resolution text all assign the recall Kind fork to this link, not `13a`. Confirmed a genuine
gap, not a misreading: none of `12g.1`–`12g.6`'s own MUST/Implement text mentions
`classify.KindRecall`, `RecallService.ForText`, or R2.3 anywhere. Added as `12g.7`, implemented per
design D9/I22 and this same conflict's resolution above — a small addition (one Kind-fork branch,
one dedicated negative test, one I22 stub-replacement) next to `12g.1`–`12g.6`'s much larger
correction-path surface, not a scope change to this link (the package-layout table already priced
it here, tasks.md's own per-task breakdown simply never wrote it down).

### C12 — `12g` measured 1073 changed lines against its own ~400 ceiling (2.68×, this chain's largest ratio yet); a correction/recall seam was evaluated and is not taken, for the same chain-structural reason C10 already declined one

`internal/brain/correction.go` (152), `internal/brain/capture.go` (74, two hunks — the correction
Kind fork and the recall Kind fork, plus `Stored`→`Outcome` renames both forks need),
`internal/brain/result.go` (111, the full six-member `CaptureOutcome` reshape — needed once,
whole, since it is one closed `iota`-shaped vocabulary, not severable per outcome),
`test/conformance/capture_correction_referent_test.go` (365, new),
`test/conformance/capture_correction_plan_test.go` (177, new),
`test/conformance/capture_recall_route_test.go` (59, new),
`test/conformance/i22_recall_one_mechanism_two_entrances_test.go` (63, stub replacement), ten
`NewCaptureService` call-site updates for the new `signals` parameter (~20, mechanical), three
`Stored`→`Outcome` assertion renames (~14, C7's own priced cost), four `testdata/llm/cases/*.json`
fixtures (32) — 1073 lines across 20 files.

**The candidate seam, along the same Kind-based boundary C11 already separates**: PR A — the
correction path in full (`correction.go`, `capture.go`'s correction-Kind hunk, `result.go`'s full
`CaptureOutcome` reshape since `OutcomeCorrected`/`OutcomeAsked` are correction-only members but the
enum ships as one closed vocabulary, the `NewCaptureService` signal-param churn, both correction
test files, the three `Stored`→`Outcome` renames), roughly 889 lines. PR B — the recall path alone
(`capture.go`'s recall-Kind hunk, `capture_recall_route_test.go`, the I22 stub replacement, one
fixture), roughly 154 lines, landing on top of PR A's already-shipped `OutcomeRecalled` member.

**Why it is not taken.** PR B alone clears the ~400 ceiling easily — but PR A, the half carrying
the actual per-task surface `12g.1`–`12g.6` describe, does not: 889 lines is still 2.2× this link's
own ceiling, dominated by three things splitting cannot shrink: (a) four independent, TDD-paired L2
behavioral guarantees (explicit-referent resolution, chat-path referent resolution including 12b's
own live-filter recompute debt, edit-plan resolution, the at-most-one-`Update*` proof) each needing
its own scenario per strict TDD (`correction.go` and its own proving tests cannot separate, the same
rule `12c`'s and `12f-i`'s own C2/C6 findings already established for this exact file shape); (b)
`result.go`'s `CaptureOutcome` reshape, which is one closed vocabulary that cannot ship partially
without breaking `AllCaptureOutcomes()`'s own completeness contract for whichever half lands second;
(c) the `NewCaptureService` signature growing a new port, rippling through every existing capture
test regardless of which Kind branch a given test happens to exercise — the same mechanical,
unavoidable churn `12g`'s own struct-growth (Conflicts §C7) already priced. Splitting off PR B does
not solve PR A's own size problem, the identical shape `12f-i`'s own C6 finding already named ("size:
PR A alone is 1.37× the same ~260 ceiling — splitting off the smaller half does not solve the size
problem for the larger one").

**Second, independent of size: this link's own numbering is fixed.** Per this link's own governing
instruction ("propose a seam; do not open two PRs on your own judgment") and `13a`'s own C10
precedent ("a seam that would reorder the chain's fixed 19-link sequence is not a link's call to
make"), splitting `12g` into two links would insert a twentieth link into a chain `tasks.md` and
`design.md` both already fix at nineteen, ordered — a bigger decision than a single link should make
on its own judgment, the same class of obstruction C10 named for `13a`, not a size or strict-TDD wall
the way `12c`/`12f-i`/`12f-ii` each hit.

**Resolution: `12g` is unsplittable for a size reason plus a chain-structural reason together, and
carries the chain's largest ratio yet.** `size:exception` applied via `gh pr edit <n> --add-label
"size:exception"`, verified stuck (see the PR record above). Recorded as an *available-but-partial*
seam for whoever next plans a revision of this chain, following `13a`'s own C10 precedent: the
smaller half (recall) genuinely clears budget on its own, but the larger half (correction) does not,
which is `12f-i`'s C6 shape, not `13a`'s C10 shape — two different reasons this document has now
named separately for declining an evaluated seam, worth keeping distinct rather than merging into one
generic "not split" note.

### C13 — `13b`: `design.md` §5.1's wire-shape illustration predates `12g`'s shipped `Correction`/`CaptureResult.Recalled` shape, `internal/httpapi`'s own sanctioned import list has no way to distinguish a provider failure from a store failure, and a real, live denial-of-service gap in `13b`'s own first submission — a nil `Deps.Capture` reaching an authenticated request panicked the process. The first three findings are resolved by rendering what is actually available and documenting both gaps rather than silently inventing data or widening scope; the fourth is a real defect, closed in this PR rather than deferred.

Four small, related findings from implementing `13b`'s own response rendering, three resolved by
rendering what the shipped Go types actually carry rather than the design document's own
illustrative JSON, following this chain's established C1/C9/C10/C11/C12 pattern (a document's own
worked example can predate a later decision that superseded it, and the later decision wins); the
fourth is not a documentation drift at all, and is recorded separately below.

1. **`design.md:1491-1495`'s own `corrected`/`asked` JSON examples show fields
   (`referent`, `question`, `candidates`) that `12g`'s shipped `brain.Correction`
   (`internal/brain/result.go`) does not carry at all** — only `UnitID`, `Fields
   []correction.Field`, and `Ambiguous`. `spec.md` R2.1 itself does not mandate field names
   ("This spec does not mandate the Go type, field names, or HTTP status codes"), so this is not a
   MUST violation — `13b`'s `correctionResponse` renders exactly the three fields `Correction`
   carries, and no more. Reading the design's illustration as binding would have meant inventing
   response data (a fabricated `question` string, a `candidates` list) that no part of the
   pipeline actually produces.
2. **`design.md:1499`'s own `outcome: recalled` example shows `semantic_leg_available`; `12g`'s
   shipped `capture.go` discards it.** `RecallService.ForText`'s second return value (design D9's
   own degradation flag) is thrown away with `_` at the capture-time recall fork
   (`internal/brain/capture.go`, the `classify.KindRecall` branch) — `CaptureResult.Recalled` has
   nowhere to carry it, unlike `13c`'s own standalone `/recall` route, which calls `ForText`
   directly and can render the flag. `13b`'s own `outcome: recalled` response therefore omits
   `semantic_leg_available`; fixing this would mean editing `12g`'s already-merged
   `internal/brain/capture.go` and `result.go`, which is out of `13b`'s own declared file
   ownership (`server.go`, `auth.go`, `capture.go` under `internal/httpapi/` — design's own
   package-layout table). Left as a named, deliberate gap for whoever next revises the capture
   pipeline's own `CaptureResult` shape, not silently worked around here.
3. **`design.md:948`'s own "provider failures → 502, store failures → 500" cannot be honored as
   written.** `brain.CaptureService.Capture` returns a plain `error` with no exported way to tell
   a provider failure (e.g. the LLM completion call itself failing) from a store failure (e.g.
   `decision_log` or a unit write failing) at `internal/httpapi`'s own boundary — and design's own
   dependency-rule check (`design.md:1472-1473`) sanctions exactly three imports for this package
   this PR (`internal/brain`, `internal/core/unit`, `crypto/subtle`), not `internal/core/classify`,
   which is the only package carrying the two sentinel errors (`ErrNoFieldsSalvaged`,
   `ErrNoUnitType`) design's own worked examples (`design.md:886-887`) name. Two options were
   evaluated: (a) import `classify` anyway to catch those two sentinels via `errors.Is`, defaulting
   everything else — including a raw LLM completion failure, which neither sentinel covers — to
   500; (b) map every `Capture` error to 500 uniformly. Chosen: **(b)**, because (a) only partially
   honors the aspiration (the single clearest "provider failure" case, a raw completion error, still
   falls through to 500 under either option) while adding an import design's own audit trail does
   not list, for a security-focused PR whose central obligation (R2.9-R2.12) is unrelated to this
   error-status nuance. 500 is also the conservative direction: it reveals less about which internal
   layer failed than a caller-visible 502 would. Recorded as an open, named gap — `brain` exposing a
   typed distinction between a provider outage and a store failure at its own package boundary —
   for whoever next revises `CaptureService.Capture`'s error contract, not solved unilaterally by
   widening `internal/httpapi`'s import list in this PR.
4. **A real defect, not a documentation drift: `13b`'s own first submission left an authenticated
   request one nil pointer away from a process crash.** `cmd/nooma/serve.go` leaves `Deps.Capture`
   nil until `13d`'s own full wiring (the same transitional state finding 3 above already names),
   and `captureHandler`'s first submission called `d.Capture.Capture(...)` with no nil check —
   caught in review, not by any test this PR had written, because every existing handler test
   supplies a real `*brain.CaptureService`. On `main`, in the window between `13b` merging and
   `13d` merging, a request that **passed** `requireToken`'s own check (this PR's entire security
   contribution) would nil-panic the server: a live, trivial denial of service against anyone who
   builds `main` in that window, worse than an ordinary bug because the request that triggers it is
   the *correct* one, on a public repository. Non-negotiable #7 — safe defaults are structural, not
   warnings — applies here exactly as it does to the auth guard itself: a code comment saying
   "stays nil until 13d" is a warning, not a guard.
   **Resolution, closed in this PR, not deferred to `13d`:** `captureHandler` now checks
   `d.Capture == nil` before decoding the request body at all, and answers `503 Service
   Unavailable` with a detail-free body (`{"error":"capture is not wired in this build"}`) — 503,
   not 500 (nothing went wrong; this build simply does not carry the dependency yet) and not 404
   (the route does exist). `TestCaptureHandler_UnwiredCaptureServiceReturns503` pins it; validated
   by breaking (the check removed, the test reproduced the exact same nil-pointer panic
   `capture.go:106` originally threw, reverted, `git diff --stat` confirmed empty). **The more
   structural fix — `Handler` refusing to build at all over an incomplete `Deps` — was considered
   and rejected on purpose**: it would churn `Handler`'s own signature and the guarded-mux tests
   this PR already built and proved (`TestGuardedRoutesRequireToken`,
   `TestOpenRoutesStayOpenRegardlessOfToken`), for a benefit small next to an honest per-request
   503; recorded in `capture.go`'s own comment so the next reader knows the cheaper option was
   chosen deliberately, not overlooked. The nil branch is not scheduled for removal once `13d`
   lands — once every `Deps` a caller builds is complete it simply becomes unreachable, and it
   stays as the structural answer to a future refactor leaving some other dependency unwired again.

### C14 — `13c`: the design leaves the read-only unit routes' access mechanism unstated; `Deps.Recall` needed the same nil-guard `13b` established for `Deps.Capture`; `13c` measured 742 changed lines against its own ~330 ceiling (2.25×), a recall/units-read seam was evaluated and not taken, for the same chain-structural reason C10/C12 already declined one; and the task text's own "asserted over all four routes" undercounts this link's three read routes.

**First finding, a design elaboration this link had to make, not a conflict.** Design D10's own `Deps` struct literal (design.md's D10 section) lists `Version`, `Capture`, `Recall`, `Token` — no field through which `GET /units/{id}`/`GET /units?ids=` could resolve a `ports.UnitRepo` read, and design's own dependency-rule check (design.md §4, "Dependency-rule check") sanctions only `internal/brain`, `internal/core/unit` and `crypto/subtle` for `internal/httpapi` — not `internal/ports`, so `Deps` cannot grow a `ports.UnitRepo`-typed field without violating that list. **Resolution**: `internal/brain/recall.go` gains `RecallService.LiveByIDs(ctx, ids) ([]unit.Unit, error)`, a thin passthrough to the `units ports.UnitRepo` `RecallService` already holds (D9's own field) — no new import, no new `Deps` field, and I02's positive live filter stays exactly where it already lived (`LiveByIDs`'s own contract), unchanged by the indirection. Recorded so whoever next reads design D10's struct literal does not read it as the read routes' complete access surface — it names the write-time struct, not every read the surface needs.

**Second finding, applying the sibling-dependency pattern this chain's own apply-progress record (13b's own C13 finding 4) named as a template, not a one-off.** `13b`'s own post-review fix closed a nil-pointer denial of service for `d.Capture == nil` and its own record explicitly flagged: *"`13c`'s own routes, if they also depend on `Recall *brain.RecallService` being non-nil, need the SAME nil-check-and-503 treatment `13b` just established for `Capture`... this is now a proven, reusable pattern... not a one-off."* All three of this link's routes (`POST /recall`, `GET /units/{id}`, `GET /units`) check `d.Recall == nil` before every call and answer `503` with `{"error":"recall is not wired in this build"}` — matching `capture.go`'s own detail-free shape exactly. Validated by breaking: the check removed from `recallHandler`, the resulting request reproduced a nil-pointer panic inside `RecallService.ScoredFor` (called via `ForText`), reverted, `git diff --stat` empty.

**Third finding, a size measurement and a declined seam, in this chain's own established idiom (C10/C12).** The complete, green `13c` PR measures 742 changed lines against its own ~330 ceiling (2.25×): `internal/brain/recall.go` (+14, the `LiveByIDs` passthrough), `internal/httpapi/recall.go` (77, new), `internal/httpapi/recall_test.go` (171, new), `internal/httpapi/units.go` (87, new), `internal/httpapi/units_test.go` (164, new), `internal/httpapi/read_routes_no_decision_test.go` (70, new), `internal/httpapi/server.go` (+20/-6, the three new route registrations and `Deps.Recall`'s doc comment), `test/conformance/i22_recall_one_mechanism_two_entrances_test.go` (+66/-20, the stub-to-real-handler replacement), `test/e2e/capture_recall_test.go` (73, new).

The candidate seam, along the same recall/units-read boundary the code itself keeps independent (`units.go` imports nothing `recall.go` declares, and vice versa): PR A — `POST /recall` in full (`internal/httpapi/recall.go`, `recall_test.go`, the I22 stub replacement, the recall half of the e2e test), roughly 360 lines. PR B — the read-only unit routes (`internal/brain/recall.go`'s `LiveByIDs`, `internal/httpapi/units.go`, `units_test.go`, the units half of the e2e test), roughly 310 lines. `read_routes_no_decision_test.go` (R2.7, spanning all three routes) would need splitting along the same boundary, roughly 35 lines each.

**Not taken, for the same reason C10 and C12 both already named for this exact chain.** Both candidate halves clear the ~330 ceiling roughly on their own (PR A at ~1.1×, tighter than plenty of this chain's already-shipped `size:exception` links), so unlike `12f-i`'s C6 finding this is not a "the smaller half doesn't solve the larger half's problem" case — the seam is genuinely available on size grounds alone. It is declined anyway because this link's own numbering is fixed at nineteen ordered links (`13c` sits between `13b` and `13d` in a chain both `tasks.md` and `design.md` already commit to), and per this link's own governing instruction — *"propose a seam; do not open two PRs on your own judgment"* — splitting it would insert a twentieth link into that fixed sequence, the same class of obstruction C10 and C12 both named, not a size or strict-TDD wall the way `12c`/`12f-i`/`12f-ii` each hit. `size:exception` applied via `gh pr edit <n> --add-label "size:exception"`, to be verified stuck once the PR opens.

**Fourth, small finding: this link's own task text ("13c.4") says its R2.7 route test is "asserted over all four routes"; this link registers three read routes, not four.** `apiRoutes` after this PR holds `POST /capture` (a write route, correctly excluded from R2.7's own scope), `POST /recall`, `GET /units/{id}` and `GET /units` — three reads. `TestReadRoutesWriteNoDecisionLog` drives exactly those three. Not acted on beyond this record — the implementation follows spec R2.7's own "Verified by" clause (which names two routes explicitly and covers the read surface generally), not the task text's imprecise count; flagged per this document's own instruction not to resolve a documentation drift silently.

**Fifth, a doc-delta confirmation, not a gap.** `design.md` D13's own table (§3, "Each PR's documentation delta is assigned up front") has no row for `13c` at all — every other core-touching or ADR-adding slice has one, `13c` has neither. Read directly: `docs/01-architecture.md:101` describes the HTTP surface only as "Exposes an HTTP API on `localhost:7777`" with no route-level promise (design.md's own §1.3 table already marks this ✎ for exactly this reason, cited by `14a`'s D13 row for the CLI table's analogous case). Confirmed by direct reading for this link specifically: no doc 01 edit is needed for `13c`, matching D13's own silence on this row rather than being an omission it missed.

### C15 — `13d`: neither `design.md` nor `spec.md` names a production `ports.Clock`/`ports.IDGen` adapter anywhere in the repository — this link is the first PR that actually constructs a real `brain.CaptureService`, and had to add one; wiring is deliberately all-or-nothing across `tasksM1Consumes`'s three tasks, not a per-task degradation; and this link's own ~420 ceiling was crossed, reported per its own stop-and-report instruction, and continued as instructed.

**First finding, a real gap this link had to close, not a documentation drift.** `NewCaptureService`'s first two parameters are `ports.Clock` and `ports.IDGen` (`internal/brain/capture.go`) — every test in this repository satisfies them with an inline fake (`c.now` closures in `*_test.go` files), and neither `design.md` nor `spec.md` names, anywhere, what production type serves either port. `rg` for `time.Now()`, `uuid.`, and `crypto/rand` outside `internal/core`/tests confirmed no implementation exists on `main` before this PR. This is not surprising in hindsight — M0 built no code that constructs a real `brain.CaptureService` (`internal/core/` was empty until M1, per this repository's own `CLAUDE.md` header), so nothing needed one — but it means `13d` is the first PR in this repository's history to need a real clock and a real id generator, and the design/spec silence on this point is a genuine gap in the planning documents, not a citation error like C1/C4/C9/C11 above.

**Resolution: two minimal, unexported adapters in `cmd/nooma/wiring.go`, not a new package.** `systemClock` wraps `time.Now()` directly — `forbidigo`'s ban on that call applies only inside `internal/core/` (`.golangci.yml:96-119`), and `cmd/nooma` is exactly where nooma-core's own decision-gate table assigns "wiring." `uuidGen` builds a version-4 UUID over `crypto/rand` (sixteen random bytes, two bit-level fixups per RFC 4122) rather than adding a new module dependency (`go.mod` carries none today) for what `docs/03-data-model.md` already specifies as the id shape. Both are five-to-ten-line types with one method each; recorded here so whoever next revises `design.md` knows this pairing needs its own row, the same way D18a's own table names three readers for `tasksM1Consumes` but nothing in this design document ever named a fourth thing this link would have to invent outright.

**Second finding, a design decision this link had to make: resolution is all-or-nothing across the three `tasksM1Consumes` tasks, never a partial wiring.** Neither `design.md` nor `spec.md` states what `serve` should do when only some of `tasksM1Consumes`' tasks are bound — an omission distinct from D18a's own stated scope ("proves the three readers agree, not that a bound provider works"). Read directly: `RecallService.ScoredFor` (`internal/brain/recall.go:120-124`) calls `s.embed.Embed(...)` with no nil guard — unlike `Candidates`, which only reads its already-supplied `vector` argument and never touches `s.embed` at all. A nil `ports.EmbeddingProvider` reaching `ScoredFor` is therefore not a degraded leg (the way an embedding-provider *outage* already degrades gracefully, `m1b` D8's own product rule) — it is a nil-interface panic on the first `/recall` request or `recall`-classified capture. `resolveTaskProviders` (`cmd/nooma/wiring.go`) therefore resolves every one of `tasksM1Consumes`' three tasks or none: an unbound task, a provider type this binary has no client for, or a provider whose type does not implement the port a task needs (an anthropic- or openai-typed `embedding` binding — `openai.Client` gains an `Embed` method only once `17` lands, confirmed directly: `rg -n "func.*Embed" internal/providers/*/*.go` shows only `ollama.Client` today) all fail the whole resolution, and `wireBrain` returns `nil, nil` rather than a partially-built pair — the existing nil-`Deps` guards (`13b`, `13c`) then answer honestly over HTTP, the identical transitional state this binary was already in before this PR, now also reachable by a genuinely unconfigured or half-configured vault rather than only by "13d has not landed." Validated by breaking (see below): a config binding only two of the three tasks was confirmed to resolve `ok=false`, not a silently partial `*brain.RecallService`.

**Third, a size measurement, following this chain's own established idiom.** The complete, green `13d` PR measures 453 changed lines against its own ~420 ceiling (1.08×, the tightest overrun ratio this chain has recorded since `12f-ii`'s 1.3×): `cmd/nooma/serve.go` (+16/-9), `cmd/nooma/tasks.go` (25, new), `cmd/nooma/tasks_test.go` (98, new), `cmd/nooma/wiring.go` (171, new), `test/e2e/capture_recall_test.go` (+134/-14). No seam was evaluated separately from this document's own intro, which already priced this link as unsplittable in advance ("no natural seam exists between `serve.go`'s wiring and `tasksM1Consumes`'s first reader — the list only has meaning once wired to a reader") — confirmed rather than merely assumed once implementation was complete: `golangci-lint`'s own `unused` check rejects `cmd/nooma/tasks.go`/`wiring.go` landing in a commit before `serve.go` actually calls `wireBrain` (`systemClock`, `uuidGen` and `wireBrain` itself all report `unused` standing alone), which is the same shape `12f-i`'s C6 finding already named for a strict-TDD-forbidden split, applied here to a lint gate instead of a test-first one. **Stop-and-report checkpoint**: this link's own cumulative diff crossed roughly 300 lines partway through implementing the L4 round-trip test (`test/e2e/capture_recall_test.go`'s own size, the single largest contributor); reported here per this document's own instruction, and continued, per the same instruction, rather than paused.

**Validated by breaking, both reverted, confirmed via `git diff --stat` empty afterward each time:**
1. `serve.go`: forced `recall = nil` immediately after a successful `wireBrain` call, over a fully-configured vault — `TestServeCaptureAndRecallRoundTripThroughRealWiring`'s own `POST /recall` assertion failed with `503`, not `200`, proving the L4 test genuinely exercises real wiring rather than passing regardless of it.
2. `wiring.go`: replaced `resolveTaskProviders`'s own `for _, task := range tasksM1Consumes` with a hardcoded literal of the same three strings — `TestResolveTaskProvidersReadsTheSharedListNotACopy` failed exactly as its own doc comment predicts, proving this link's own D18a reader test genuinely distinguishes "reads the shared list" from "happens to enumerate the same three names."

**Fourth, a doc-delta confirmation, not a gap — the same finding C14's own fifth point already made for `13c`, re-confirmed directly for this link rather than assumed to carry over.** `design.md` D13's own table (§3) has no row for `13d`, matching every other link this table is silent on that touches no `internal/core/**` file (`13a`, `13c`, `12d`, `12e`, `12f-ii` — none of them core-touching in isolation, none tabulated). `docs-sync.yml`'s own header (`.github/workflows/docs-sync.yml:8`) fires only on `internal/core/**` changes, and `13d` touches none. `docs/01-architecture.md:101`'s own HTTP line — "Exposes an HTTP API on `localhost:7777`" — makes no route-level promise before this PR and needs none after it: this link wires already-mounted routes (`13b`, `13c`) to real dependencies, it adds no new route, so the one doc-01 line C14 already confirmed correct for `13c` stays correct, unwidened, for `13d` too.

### C16 — `17`: `recall.Normalize` is idempotent by construction, not merely "untested against double normalization" — a measurement corrected mid-review, and D17's own prose overstates the risk

`17` shipped exactly as D17 designs it: `internal/providers/openai/embed.go`, a fifth method-on-`Client`
shape after `ollama/embed.go`'s precedent, `var _ ports.EmbeddingProvider = (*Client)(nil)`, no new
`testdata/llm/` case (D17's own ruling — the corpus is JSON text, not vectors, and D16's quality gate
would have nothing to judge). `17.2`'s `Endpoint`→`baseURL` passthrough needed no new code: `13d`
already wired `p.Endpoint` straight into `openai.NewClient`.

**Validated by breaking, all three reverted, confirmed via `git diff --stat` empty afterward each
time** (files were `git add`ed first so untracked new files would show in the diff):

1. Returned `c.model` (the requested model) instead of `parsed.Model` (the echoed one) —
   `TestClient_EmbedSendsAuthHeaderAndReturnsEchoedModel` failed exactly as its own doc comment
   predicts: `Model = "text-embedding-3-small-requested", want the response body's model...`.
2. Let an empty `data` array flow through as a zero-vector `EmbedResponse` instead of erroring —
   `TestClient_EmbedFailsWhenDataIsEmpty` failed exactly as predicted: `Embed returned a nil error
   for an empty data array; got {Vector:[] Model:...}`.
3. **Called `recall.Normalize` inside the client, simulating a double-normalization** (the store's
   own `internal/store/sqlite/embeddingrepo.go` already normalizes every embedding on write,
   unconditionally). This PR's own client-level test caught the change (`Vector[0] = 0.03331409,
   want 0.01` — the fixture vector `[0.01, -0.002, 0.3]` is not itself unit-norm, so a normalizing
   pass visibly moves it). The first version of this record stopped there and drew the wrong
   conclusion: that the catch was "an accident of fixture choice" and that nothing could ever
   distinguish a correctly-normalized-once vector from a double-normalized one. **That was an
   unverified generalization from one fixture, and it was corrected during review before merge.**

   **What is actually true, verified two ways.** First, algebraically: `recall.Normalize(v)` divides
   `v` by its own L2 norm, so the result has norm 1 (up to floating-point rounding); calling
   `Normalize` again divides that result by a norm of ~1, which changes nothing beyond rounding
   noise — idempotence is a property of the operation itself, not of any particular input. Second,
   empirically, because "up to rounding" needed to be measured rather than assumed given
   `Normalize`'s float64-accumulate/float32-store mix (`vector.go:125-141`): a standalone check (Go,
   run via `go run`, not committed) applied `Normalize` twice to 20,000 random 1536-component
   vectors (`text-embedding-3-small`'s own dimension) with mixed sign and magnitude spanning six
   orders of ten, plus one pathological vector spanning thirty orders of magnitude between its
   largest and smallest component — **zero bit-level differences in every case.** `ErrZeroVector` is
   idempotent too, by inspection: a zero vector fails on the first call and never reaches a second.

   **Conclusion, corrected from the original entry: double-normalizing here is silent, but it is not
   wrong — the stored vector is unaffected, because the operation is idempotent. There is no
   correctness defect for any test to catch, tested or untested, because there is nothing to catch.**
   The no-normalize rule in `embed.go` is therefore a **redundancy and single-ownership rule, not a
   correctness guard**: its value is that a reader has exactly one place to look to know a vector is
   unit-normalized (`internal/store/sqlite`), not that a second call anywhere else would corrupt
   data. **`design.md` D17's own wording — "normalizing an already-normalized vector is a no-op
   within tolerance... a truncation knob... Stated so nobody 'optimizes' the store's call away on the
   grounds that OpenAI already returns unit vectors" reads correctly, but this PR's own first attempt
   at a conflict-log entry pushed past D17's actual claim into "silent and wrong," which overstates
   it. Flagging that phrase as a design-doc imprecision this measurement resolves, for whoever next
   revises `design.md`** — not fixed here, since `17` touches no `design.md` content and the accepted
   document is not edited mid-chain except by its own revision process. `embed.go`'s comment is
   reworded in this PR to state the redundancy/single-ownership rationale rather than imply
   corruption risk. This remains a genuinely new finding this link surfaced (the idempotence property
   was not stated as such in `design.md` or `spec.md`), but the finding is "the operation cannot
   corrupt data," not "no test can catch a corruption that does not exist."

**Size**: 208 changed code lines (`internal/providers/openai/embed.go` 76 new, `embed_test.go` 132
new, both insertions only) against the ~200 ceiling (1.04×) — the tightest overrun ratio this chain
has recorded, inside `13d`'s own 1.08× precedent for "no `size:exception` needed." 266 total
including this `tasks.md` delta (58 lines). No `size:exception` applied.

**Doc delta**: none, per `design.md` D13's own table (§3, `17` row) — "doc 01's provider list
already carries `openai` (Phase A PR 1); no delta." Confirmed directly: `docs/01-architecture.md`
names provider *types* generically ("Each `type` in the yml implements the matching interface"), not
a per-type capability table, so `openai` gaining an `Embed` method changes no line there.

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

- [x] **12d.1** Test first: extend the shared `repocontract` shape — `UpdateEventAt`/`UpdateDueAt`
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
- [x] **12d.2** L3: `internal/store/sqlite/unitrepo.go` implements both methods against a real
      migrated vault, the same two-distinguishable-instants contract case answered again in SQLite
      — C11's lesson from `m1b-pipeline` ("answered twice — `memrepo` and `internal/store/sqlite`,
      in the same PR").
      Verify: `go test ./test/integration/... -tags integration`.
      Requirement: R1.12 (this port's half); design D4.
- [x] **12d.3** `make store-api-golden`; confirm `git diff` over `internal/store/sqlite/migrations/`
      is empty — repository methods against existing columns, no new migration.
      Verify: `TestHarness_StoreAPIUnchanged` against the regenerated golden; the empty migration
      diff.
      Requirement: R1.12.
- [x] Verify (PR-level): `make check-all`; confirm this PR's diff is additions to
      `internal/ports/unitrepo.go` and `internal/store/sqlite/unitrepo.go` only (R7.3/R7.4).
      **Done — PR #111 (https://github.com/rengo/nooma/pull/111, branch
      `feat/ports-unit-fields`, base `main`, NOT yet merged): 153 insertions / 1 deletion across 5
      files (well under the ~380 ceiling), `make check-all` green (lint 0 issues, `internal/core`
      coverage 100%/308/308 unaffected, seven-target cross-compile, e2e). The transposition risk
      D4 names was verified caught, not just asserted: `UpdateEventAt`'s column write was
      temporarily swapped to `DueAt`, the new contract case failed with the expected diff, then
      reverted before committing — the same temporary-break discipline `12f-i.2`'s AST guard
      uses. No `docs/02-cognitive-core.md` delta: this PR touches no `internal/core/**` file, and
      `design.md`'s D13 table assigns `12d` no documentation row.**

---

## PR 12e — `feat/ports-signalrepo` (~400 — at the ceiling; High risk: a new port plus its first
L3 case)

Depends on nothing beyond Phase A/B (independent of `12a`–`12d`).

- [x] **12e.1** Test first: a `repocontract`-shared case for `ports.SignalRepo` — `AllSignalTypes()`/
      `AllValences()` closed-vocabulary completeness; `Record`/`Since` round-trip. **Red**:
      `undefined: ports.SignalRepo`, `undefined: ports.Signal`.
      Implement `internal/ports/signalrepo.go` (NEW): `SignalType` (the eleven members migration
      0002's own comment enumerates), `Valence` (positive/negative/neutral), `TargetKind`,
      `Signal{... TargetID *string /* NO FK */ ...}`, `SignalRepo{Record, Since}`; `memrepo` gains a
      `Signals` fake.
      Verify: `make test`; `golangci-lint run` (no `Delete*`-prefixed method, CLAUDE.md
      non-negotiable #6).
      Requirement: R1.10; design D6.
      **Done: `undefined: ports.SignalRepo` confirmed red for the right reason before
      `internal/ports/signalrepo.go` existed; `undefined: memrepo.NewSignals` confirmed red for
      the right reason before the fake existed. `TestSignalRepo_MemRepo` green (4 subtests:
      `AllSignalTypes`, `AllValences`, round-trip, ordering+limit).**
- [x] **12e.2** L3 — I13's behavioural half: `internal/store/sqlite/signalrepo.go` implements
      `Record`/`Since` against a real migrated vault with `foreign_keys=on`; a signal whose
      `TargetID` names a unit that was **never created** persists and reads back.
      Verify: `go test ./test/integration/... -tags integration`.
      Requirement: R1.10's Verified-by; design D6; `docs/06-harness.md` §4's I13 framing.
      **Done: `TestSignalRepo_Contract` (the same shared suite, at L3) and
      `TestSignalRepo_RecordSurvivesATargetThatNeverExisted` both green. The latter's own claim was
      verified caught, not just asserted: `Record` was temporarily given an application-side check
      rejecting an unknown `TargetID` (simulating code that would defeat I13), the test failed with
      the expected error ("target unit ... does not exist"), then the check was reverted —
      `git diff --stat` against the new (untracked) file showed no leftover diff, the same
      temporary-break discipline `12d`'s transposition check used.**
- [x] **12e.3** `make store-api-golden`; confirm no migration touched (`learning_signals` already
      exists, migration 0002).
      Verify: `TestHarness_StoreAPIUnchanged`; empty migration diff.
      Requirement: R1.12 (the `SignalRepo` half).
      **Done: golden regenerated, four additions only (`SignalRepo.Record`, `SignalRepo.Since`,
      `NewSignalRepo`, `type SignalRepo`); `git diff --stat` over
      `internal/store/sqlite/migrations/` empty. See Conflicts §C4: `spec.md` R1.10/R1.12 cite
      "migration 0001" for this table; the real DDL is in migration 0002, confirmed off the file.**
- [x] Verify (PR-level): `make check-all`; confirm `internal/ports/decisionlog.go`,
      `relationrepo.go`, `provider.go`, `clock.go`, `lexicalsearch.go` are untouched by this PR
      (R7.4).
      **Done — PR #112 (https://github.com/rengo/nooma/pull/112, branch `feat/ports-signalrepo`,
      base `main`, NOT yet merged): 626 insertions across 7 files (1.57× the ~400 ceiling — see
      Conflicts §C3 for the seam evaluated and rejected before opening this PR, and why it is
      unsplittable for a reason distinct from `12c`'s), `make check-all` green (lint 0 issues,
      race+shuffle unit+integration tests, build, `TestSchemaGolden` empty diff, `internal/core`
      coverage 100%/308/308 unaffected, seven-target cross-compile, e2e). Confirmed untouched:
      `internal/ports/decisionlog.go`, `relationrepo.go`, `provider.go`, `clock.go`,
      `lexicalsearch.go`. No `docs/02-cognitive-core.md` delta: this PR touches no
      `internal/core/**` file, and `design.md`'s D13 table assigns `12e` no documentation row
      (confirmed by reading D13 directly).**

---

## PR 12f-i — `feat/brain-correction-audit` (~260 estimate, 616 measured — 2.37×; High risk, **not
split** — see Conflicts §C6; `size:exception` applied)

Depends on (`12c`, `12d`, `12e`) all merged to `main`.

- [x] **12f-i.1** Test first (I23, the RED-first audit-failure test ADR-0016 names): a `DecisionLog`
      fake configured to fail `Record`, driving a correction attempt, asserting no
      `Update*` call reaches `ports.UnitRepo` and the target unit's stored content/dates are
      unchanged afterward. **Red**: compile failure against the not-yet-existing
      `internal/brain/correction.go`.
      Implement `internal/brain/correction.go`: `correctionRunner{units, log, ids}` (Conflicts §C7 —
      `signals`/`recall` join in `12f-ii`/`12g`, where each is first read), `applyWithPreImage`
      (Layer 1 — the one door; ADR-0016's ordering: `recordPreImage` first, `dispatchEdits` only on
      success), `dispatchEdits` (a total switch over `correction.Field`). Two new
      `ports.DecisionAction` members — `ActionCorrectionApplied`, `ActionCorrectionAmbiguous` — the
      sanctioned R7.4 edit to `internal/ports/decisionlog.go`.
      Verify: `make test`.
      Requirement: R1.9; design D5 Layers 1 and 3.
      **Done: `go vet`/`go build` confirmed red with `undefined: correctionRunner` before
      `internal/brain/correction.go` existed. Test placed as white-box
      `internal/brain/correction_test.go` (`package brain`), not `test/conformance/` — Conflicts
      §C5: `correctionRunner`/`applyWithPreImage` are unexported by design, and Go's own visibility
      rule makes them unreachable from a separate importing package.**
- [x] **12f-i.2** L2 AST guard (Layer 2). **No natural pre-implementation red** — it proxies over
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
      **Done: `test/conformance/brain_correction_audit_before_edit_ast_test.go`
      (`TestCorrectionAuditPrecedesEveryUpdate`). Two temporary-break experiments run and reverted:
      (1) a rogue `r.units.UpdateContent(...)` call inserted inside `recordPreImage` — guard failed,
      naming `recordPreImage calls UpdateContent outside dispatchEdits`; (2) `applyWithPreImage`'s
      two calls swapped — guard failed, naming `dispatchEdits (statement 0) runs before
      recordPreImage (statement 1)`. `git diff` against the reverted file was empty both times.**
- [x] **12f-i.3** The pre-image shape: a successful correction writes exactly one `decision_log` row
      (`correction.applied`) whose `context` carries `{unit_id, fields, previous, next, referent}`
      per D5's JSON shape, `previous`/`next` keyed by column name.
      Verify: `go test ./test/conformance/...` — `context.previous` equals what `ByID` returned
      before the edit.
      Requirement: R1.9; design D5.
      **Done: `TestApplyWithPreImage_PreImageShape` (table-driven, explicit and recall referent
      paths), same white-box file as 12f-i.1 — see Conflicts §C5. Asserts `previous.event_at` is
      JSON `null` for an unedited column, `next.event_at` round-trips the new value, and the
      `referent` object's three score keys are present only on the recall path.**
- [x] **12f-i.4** doc 02 §5 step 4 delta: ADR-0016's settled `context` JSON shape (not forced by
      `docs-sync` per R1.13, carried anyway per design's own choice — D13's row for `12f`).
      Verify: read the section; `docs-sync.yml`.
      Requirement: R1.13; design D13.
      **Done: `docs/02-cognitive-core.md` §5 step 4 gains the settled pre-image JSON shape, a fenced
      example, and the null/omitted-key rules — the wording gap ADR-0016 itself left open. This PR
      touches no `internal/core/**` file, so `docs-sync.yml` does not require this edit; it is
      carried per R1.13's own licensed choice.**
- [x] Verify (PR-level): `make check-all`; confirm this PR's diff to
      `internal/ports/decisionlog.go` is exactly the two new `DecisionAction` members and nothing
      else in that file changes.
      **Done: 616 insertions / 9 deletions across 7 files (`internal/brain/correction.go` 166,
      `internal/brain/correction_test.go` 189,
      `test/conformance/brain_correction_audit_before_edit_ast_test.go` 224,
      `internal/ports/decisionlog.go` +26/-9, `test/support/memrepo/decisionlog.go` +16,
      `test/support/repocontract/decisionlog.go` +4, `docs/02-cognitive-core.md` +19 — 2.37× the
      ~260 ceiling; see Conflicts §C6 for the seam evaluated and rejected). `make check-all` green:
      lint 0 issues (including the `unused`-field fix in Conflicts §C7), vet, race+shuffle
      unit+integration tests, build, `TestSchemaGolden` empty diff, `internal/core` coverage
      100%/308/308 unaffected, seven-target cross-compile, e2e. Confirmed `internal/ports/decisionlog.go`'s
      only change is the two new `DecisionAction` members and their doc comments/`AllDecisionActions`
      entries.**

---

## PR 12f-ii — `feat/brain-correction-signal` (~180, second half of the pre-split `12f`)

Depends on `12f-i` (stacked — extends `applyWithPreImage`'s own call site).

- [x] **12f-ii.1** Test first: after `12f-i`'s pre-image write and every `Update*` call have both
      succeeded, `correctionRunner` calls `signals.Record` — a `learning_signals`-shaped row via
      `ports.SignalRepo` (`12e`) with `signal_type = "correction"`, `target_kind = "unit"`,
      `target_id` = the referent unit's id, `valence = negative`, `context = {unit_id, fields,
      decision_id}` where `decision_id` names the accompanying `correction.applied` row. Written
      **after** the edits, never for a correction that did not land (D6's own reasoning: a signal for
      a failed edit would teach a future learning pass from an event that did not occur). **Red**:
      compile failure — `correctionRunner` has no `signals` call site yet beyond the field itself.
      Verify: `make test`.
      Requirement: R1.10; design D6.
- [x] **12f-ii.2** Confirm this PR is the first in the whole `m1-capture-recall` umbrella to write
      to `learning_signals` at all (`m1b-pipeline` R8.1's own deferral).
      Verify: review; `i13_learning_signal_test.go`'s existing DDL check stays unaffected — this PR
      exercises the write path that test's schema fact protects, not a change to the schema.
      Requirement: R1.10.
- [x] Verify (PR-level): `make check-all`.

---

## PR 13a — `feat/brain-recall-fortext` (~380)

Depends on `12a` (merged). **Lands here in merge order, before `12g`, even though its umbrella
number (13) is higher than `12g`'s (12) — the one ordering in this chain that inverts its own
numbering, called out explicitly rather than left implicit, in the same category
`m1b-pipeline`'s C1 already named.** `12g`'s correction-referent resolution needs `ScoredFor`
before it can be written at all, so `13a` must merge first.

- [x] **13a.1** Test first (I22, a new invariant — register it in `docs/06-harness.md` §4 before
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
- [x] **13a.2** Wire the three orphan actions that are this phase's job to give callers, the
      discard/unparseable/unclassifiable half (D8) that belongs with recall/routing rather than
      `12g`'s correction/outcome-vocabulary work: `chitchat`/`out_of_scope` classification →
      `OutcomeDiscarded`, one `capture.discarded` row; `Decode` returning `ErrNoFieldsSalvaged` →
      `capture.classify.unparseable`; `c.Kind == nil` → `capture.classify.unclassifiable`.
      (`capture.dedup.judged` stays an orphan, per `m1b-pipeline`'s own C14b.)
      Verify: `make test` — a conformance test per orphan action confirming it is now called
      outside `test/support/repocontract`.
      Requirement: design D8 (the three-orphan-actions half not owned by `12g`).
- [x] **13a.3** `docs/06-harness.md` §4 registers I22 before its test is written — confirm task
      13a.1 followed this order.
      Verify: read the invariant table — I22 present with its doc 02 §5 step 2 reference.
      Requirement: design D14.
- [x] Verify (PR-level): `make check-all`; confirm `git diff --name-only` touches only
      `internal/brain/recall.go`, the discarded/unparseable/unclassifiable arms of
      `internal/brain/capture.go`'s routing fork (not the correction/recall `Kind` forks — those
      are `12g`'s), and `docs/06-harness.md`. **As measured (C10): the production surfaces match
      exactly; the diff also touches three test files and three JSON fixtures this sentence did not
      enumerate, which is expected — tests travel with the behavior they prove
      (`work-unit-commits`), not a second routing surface.**

---

## PR 12g — `feat/brain-correction-route` (~400 estimate, 1073 measured — 2.68×; High risk, named
explicitly by `design.md` as one of the rows in the estimate band; **not split** — see Conflicts
§C12; `size:exception` applied)

Depends on (`12f-ii`, `13a`) both merged.

- [x] **12g.1** Test first: R1.1 — a conformance test driving a `correction`-classified capture,
      asserting `ToUnit` is never called and `Create` is never called for it.
      Implement: capture's `Kind`-based routing fork for `correction` (mirroring
      `m1b-pipeline` R4.6's own timer-refusal fork), before `ToUnit` is ever reached.
      Verify: `make test`.
      Requirement: R1.1.
      **Done: `TestCapture_CorrectionChatPathReferentResolution`'s "single strong match" subtest
      asserts `units.Count() == 1` after the correction — the same one unit seeded before it, never
      grown by a `Create` call. `correctionRunner.at` is the routing fork's only callee, and it holds
      no path to `classify.ToUnit` at all.**
- [x] **12g.2** Test first: R1.5 — an explicit `unit_id` override: `CaptureInput` gains
      `ReferentID`; when non-empty and the classification is `correction`, capture uses
      `UnitRepo.ByID` directly and does **not** run recall at all (an instrumented index that fails
      the test if queried); an unknown explicit id returns an error and edits nothing.
      Verify: `make test`.
      Requirement: R1.5; design D7.
      **Done: `TestCapture_CorrectionExplicitReferentWinsWithoutRecall`, two subtests. "Recall does
      not run" is proven by `embed.EmbedCalls() == 0` — `RecallService.ScoredFor`/`ForText` is the
      only path left that would call `ports.EmbeddingProvider.Embed` for a correction, so an
      instrumented count is equivalent to an instrumented index here and needs no new fake.**
- [x] **12g.3** Test first: R1.6 — chat-path referent resolution: no explicit id →
      `RecallService.ScoredFor(ctx, in.Text)` (`13a`, raw text) → `correction.Referent` (`12b`)
      gated by `ReferentMargin`, computed over the **live** candidates only (after `LiveByIDs` — a
      `superseded` top scorer is dropped and the ratio recomputed over the survivors, I02); *ask* →
      capture edits no unit, writes `correction.ambiguous`, returns `OutcomeAsked`; *pick* →
      proceeds to `PlanEdit`. Includes R1.6's own two-units-ambiguous scenario.
      Verify: `make test`.
      Requirement: R1.6; design D2, D7, D9.
      **Done: `TestCapture_CorrectionChatPathReferentResolution`, three subtests — single strong
      match (pick), R1.6's own two-units-within-margin Scenario (ask, both units byte-identical
      after), and 12b's own ordering debt: "dropping the archived top scorer changes the pick" —
      an archived unit that would outscore a pool unit before the live filter is dropped by
      `ScoredFor`'s own `LiveByIDs` join (13a), and the sole surviving live candidate is picked
      instead. Validated by breaking: `RecallService.ScoredFor`'s `LiveByIDs` join was temporarily
      replaced with an unfiltered `ByID` walk (bypassing the live filter entirely) — the subtest
      failed exactly as expected, picking the archived unit (`Correction.UnitID = "archived-A"`,
      want `"live-B"`); reverted, `git diff --stat` empty. The recomputation is real: `ScoredFor`
      builds its `id -> score` map from the unfiltered fused ranking and only then walks
      `LiveByIDs`' own survivors, so the ratio `correction.Referent` sees is already over the
      survivors — this PR's `resolveReferent` never re-filters, it only converts `ScoredUnit` to
      `recall.FusedCandidate` and calls `Referent` directly.**
- [x] **12g.4** Test first: R1.8's orchestration half — `correctionRunner.at` calls
      `correction.PlanEdit(c)` (`12c`); a `false` result (both dates, or neither) writes
      `correction.ambiguous` and returns `OutcomeAsked`; a `true` result calls
      `applyWithPreImage(target, plan, ref, now)` (`12f-i`/`12f-ii`).
      Verify: `make test`.
      Requirement: R1.8; design D3, D7.
      **Done: `TestCapture_CorrectionPlanWritesExactlyOneField`, three subtests — content-only
      leaves both dates unchanged, two resolved dates ask (target byte-identical), and at most one
      `Update*` call reaches `ports.UnitRepo` per correction (a call-counting `ports.UnitRepo`
      wrapper, since a value-only assertion cannot distinguish one correct call from two calls that
      happen to agree). The date-only row (dates win, content stays stale) is `12g.3`'s own "single
      strong match" subtest — not re-proven here.**
- [x] **12g.5** `CaptureOutcome`/`CaptureResult` reshaped (D8): the closed vocabulary (`Stored`,
      `Deferred`, `Discarded`, `Recalled`, `Corrected`, `Asked`), `AllCaptureOutcomes()`. `Stored
      bool` is **replaced**, not joined — Phase B tests asserting `Stored: true/false` are edited in
      this PR to assert `Outcome` instead (assertion-renaming only, never a weakened conformance
      claim, per C7's own cost pricing).
      Verify: `make test` — full suite green, including the edited Phase B assertions.
      Requirement: design D8 (C7's resolution).
      **Done: `internal/brain/result.go`'s `CaptureResult` carries `Outcome CaptureOutcome` in place
      of `Stored bool`; `AllCaptureOutcomes()` returns the six-member vocabulary. Three pre-existing
      assertions renamed, no conformance claim weakened: `i04_timer_never_a_unit_test.go`
      (`Stored == false` → `Outcome == OutcomeDeferred`), `capture_ambiguous_person_ref_test.go`
      (`Stored == true` → `Outcome == OutcomeStored`), `capture_orphan_actions_test.go`
      (`Stored == true` → `Outcome == OutcomeDiscarded`).**
- [x] **12g.6** I03's correction half: a conformance test asserting a correction is an UPDATE — the
      unit count is unchanged, the id survives, no `Create`, no `SetStatus`/`Delete*`/`Remove*`/
      `Purge*`/`Drop*`/`Destroy*`-prefixed call.
      Verify: `go test ./test/conformance/...` — the existing `i03_units_never_deleted_test.go`
      stays unchanged and green against this PR's new files.
      Requirement: R1.11.
      **Done: folded into `TestCapture_CorrectionChatPathReferentResolution`'s "single strong match"
      subtest (`units.Count() == 1`, the id `dentist-real` survives and is read back directly) rather
      than a separate file — `i03_units_never_deleted_test.go`'s own reflection/tree-scan checks are
      structural and need no change to cover this PR's new files, confirmed green unmodified.**
- [x] **12g.7** (not in this link's original numbered tasks — see Conflicts §C11) R2.3's own half:
      capture's `type: recall` Kind fork, and I22's own stub-to-production replacement named by the
      apply prompt. `RecallService.ForText(ctx, in.Text)` (never `c.NormalizedContent`, D9's forced
      argument), returning `CaptureOutcome.Recalled` with the found units; never persists a unit.
      Verify: `make test`.
      Requirement: R2.3 (per Conflicts §C11, in design D9's favor over R2.3/R2.4/R2.5's own
      `RecallService.Candidates` citation); design D9; I22.
      **Done: `TestCapture_RecallClassificationNeverPersistsAUnit` (units.Count() == 0,
      decision_log has 0 rows — recall is a read, I12) and `TestI22_RecallOneMechanismTwoEntrances`'s
      own `entranceCapture`, now the real `CaptureService.Capture` pipeline rather than a stub
      closure. Validated by breaking: the fork's `in.Text` argument was temporarily replaced with a
      literal wrong string — I22 failed exactly as expected (`got [vector-match], want ... [vector-match
      lexical-match]`); reverted, `git diff --stat` empty.**
- [x] Verify (PR-level): `make check-all`; confirm `12g` and `13a` together are the only routing
      changes to `internal/brain/capture.go`.
      **Done — PR #116 (https://github.com/rengo/nooma/pull/116, branch
      `feat/brain-correction-route`, base `main`, NOT yet merged, `size:exception` labeled and
      verified stuck): 1073 changed
      lines across 20 files (976 insertions, 97 deletions; 2.68× the ~400 ceiling — see Conflicts
      §C12 for the seam evaluated and not taken), `make check-all` green (lint 0 issues,
      race+shuffle unit+integration tests, build, `internal/core` coverage unaffected — no core file
      touched, seven-target cross-compile, e2e). No `docs/02-cognitive-core.md` delta: this PR
      touches no `internal/core/**` file, and `design.md`'s D13 table assigns `12g` no row (a "no
      core file" slice, the same class as `12f`/`13b`/`17`/`15`/`16a`/`16b`).**

---

## PR 13b — `feat/httpapi-capture` (~450 — over the ceiling; High risk; **not split, on
purpose** — R2.9's own MUST forbids it, see intro)

Depends on (`12g`, `13a`).

- [x] **13b.1** `docs/adr/0017-http-request-auth.md` — new ADR recording the decision that every
      API route requires a bearer token whenever one is configured; ADR-0007 unedited, not
      superseded; `docs/adr/README.md`'s index gains its row.
      Verify: read the ADR; the index row present.
      Requirement: R2.9.
      **Done: `docs/adr/0017-http-request-auth.md` (99 lines) plus its `docs/adr/README.md` index
      row. ADR-0007 confirmed unedited (`git diff origin/main -- docs/adr/0007-http-auth.md` empty
      throughout this PR).**
- [x] **13b.2** Test first: `internal/httpapi/auth.go` — `ResolveToken(cfg, lookup) (string, bool)`,
      reading the same `server.auth_token_env` variable `DecideBinding` reads; `requireToken(token)`
      middleware; a truth-table test sweeping the same `(bind, auth_token_env, env-set?)`
      combinations `binding_test.go` already exercises, asserting the middleware is a no-op **only**
      when the effective bind is loopback, for every combination where `DecideBinding` actually
      succeeds. **Red**: `undefined: httpapi.ResolveToken`, `undefined: httpapi.requireToken`.
      Verify: `make test`.
      Requirement: R2.10; design D10.
      **Done: red confirmed (`go vet` reported both undefined symbols) before `auth.go` existed.
      `binding_test.go`'s own case table promoted to a package-level `bindTokenTruthTable` so
      `TestRequireTokenNoOpOnlyOnLoopback` sweeps the exact combinations `TestDecideBinding`
      already proves against — one table, two consumers, matching D10's own "one slice, two
      consumers" shape applied to the tests themselves.**
- [x] **13b.3** Test first: a completeness test iterating **one** declared route-table slice
      (consumed both by registration and by this test — D10's "one slice, two consumers" shape),
      asserting every entry returns 401 with no token and with a wrong token when a token is
      configured, and that both responses are **byte-identical** (R2.11's own MUST NOT against an
      oracle) — compared to each other, not to a literal. The comparison uses
      `crypto/subtle.ConstantTimeCompare`.
      Verify: `make test`.
      Requirement: R2.11.
      **Done: `TestGuardedRoutesRequireToken` iterates `apiRoutes(d)` — the same slice `Handler`
      registers the guarded mux from — asserting a 401 with an identical status and body for a
      missing token and a wrong token.**
- [x] **13b.4** `Handler(Deps)` — the two muxes (open: `GET /{$}`, `GET /ui`; guarded: everything
      else); `POST /capture` wired to `CaptureService.Capture` unchanged, mapping every
      `AllCaptureOutcomes()` member to a status code via a total switch (`stored` → 201, every other
      outcome → 200 with a body naming what happened, provider failures → 502, store failures →
      500; `deferred` is **not** an error status). A completeness test over `AllCaptureOutcomes()`
      fails loudly if any member has no mapping.
      Test first: a handler test driving one ordinary capture (201/stored), one timer-classified
      message (the refusal's plain-words message verbatim, R2.2), and the completeness test.
      Verify: `make test`.
      Requirement: R2.1, R2.2; design D10.
      **Done: red confirmed (`undefined: httpapi.Deps`) before `Handler(Deps)`/`capture.go` existed.
      `TestCaptureHandler_StoresAnOrdinaryCapture` (201/stored),
      `TestCaptureHandler_TimerRefusalSurfacesPlainWordsVerbatim` (200/deferred, the exact refusal
      message), `TestAllCaptureOutcomesHaveAStatusMapping` (the total-switch completeness test).
      The `provider failures → 502` half of this task's own text is not fully honored — see
      Conflicts §C13's third finding: `brain.CaptureService.Capture` exposes no typed distinction
      this package can read without an import design's own dependency-rule check does not list;
      every `Capture` error renders 500.**
- [x] **13b.5** R2.12 review checkpoint: no cookie-setting or session code path exists in this PR;
      `GET /` and `GET /ui` stay reachable without a token regardless of whether one is configured.
      Verify: review; a route test confirming both routes succeed with no `Authorization` header
      even when a token **is** configured.
      Requirement: R2.12.
      **Done: reviewed — `git grep -n "Set-Cookie\|http.Cookie\|session"` in this PR's own diff
      finds nothing. `TestOpenRoutesStayOpenRegardlessOfToken` confirms `GET /` and `GET /ui` both
      succeed with no `Authorization` header while `Deps.Token` is set, and that neither sets a
      `Set-Cookie` header.**
- [x] Verify (PR-level): `make check-all`; confirm `git diff --name-only` for this PR contains
      `docs/adr/0017-http-request-auth.md` and the middleware landing together with `POST
      /capture`'s mount — never the middleware in a later PR (R2.9's own Verified-by). **Stop-and-report
      checkpoint once this PR's own cumulative diff crosses roughly 300 lines**, per the Review
      Workload Forecast below; no valid split line exists inside this PR's own scope.
      **Done — PR #118 (https://github.com/rengo/nooma/pull/118, branch `feat/httpapi-capture`,
      base `main`, NOT yet merged, `size:exception` labeled and verified stuck) — four commits, in
      order:
      `docs(adr): ADR-0017` (99 lines, no code), `feat(httpapi): ResolveToken and the constant-time
      requireToken middleware` (auth.go/auth_test.go, the middleware unwired from any route),
      `feat(httpapi): guarded route slice, POST /capture, and the total status switch` (Handler(Deps)
      rewrite, apiRoutes, capture.go — POST /capture is mounted for the first time in this same
      commit that builds the guarded mux and wraps it in requireToken; no commit in this PR's
      history has the route reachable without the middleware already applied). `git diff
      --name-only origin/main..HEAD` confirms `docs/adr/0017-http-request-auth.md` and
      `internal/httpapi/{server,auth,capture}.go` all present. Measured: 937 insertions/51
      deletions = 988 changed lines across 10 files, 2.20× the ~450 ceiling — this PR's own
      stop-and-report checkpoint fired at roughly 300 lines and is recorded in the apply-progress
      artifact rather than repeated here. `make check-all` green end to end: lint 0 issues, vet,
      race+shuffle unit+integration tests, build, `TestSchemaGolden` empty diff, `internal/core`
      coverage 100% (308/308, unaffected — no core file touched), seven-target cross-compile, e2e.
      Validated by breaking, each reverted and confirmed via `git diff --stat` (empty after every
      revert): (1) mounted `POST /capture` directly on the open mux, bypassing `apiRoutes`/
      `requireToken` — `TestGuardedRoutesRequireToken` failed with `status = 400, want 401` (the
      bypassed route reached `captureHandler` directly, hitting its JSON-decode-error path instead
      of being intercepted); (2) made `requireToken` an unconditional no-op — three tests failed:
      `TestRequireTokenConstantTimeAndNoDetail` (`status = 200, want 401`),
      `TestGuardedRoutesRequireToken` (`status = 400, want 401`), and
      `TestRequireTokenNoOpOnlyOnLoopback`'s own non-loopback row (`an unauthenticated request
      reached the handler = true, want false`); (3) removed `OutcomeDiscarded`'s case from the
      status switch — `TestAllCaptureOutcomesHaveAStatusMapping` failed with `has no status mapping
      (renderCaptureResult returned status 0)` and `checked 5 outcomes, want 6`.
      **Post-review fix (Conflicts §C13 finding 4): the transitional `Deps.Capture == nil` state
      this PR's own first submission left as a code comment was a live nil-pointer-panic denial of
      service reachable by an authenticated request — closed in this PR rather than deferred to
      `13d`. `captureHandler` now returns `503 Service Unavailable` for a nil `Capture`, test-first
      (`TestCaptureHandler_UnwiredCaptureServiceReturns503`, red confirmed as the exact same panic
      `capture.go:106` originally threw); validated by breaking (the check removed, the identical
      panic reproduced, reverted, `git diff --stat` empty). Updated measurement: 1046 changed lines
      (995 insertions, 51 deletions) code-only, 2.32× the ~450 ceiling; `make check-all` re-run
      green end to end after the fix.**

---

## PR 13c — `feat/httpapi-recall-units` (~330)

Depends on `13b`.

- [x] **13c.1** Test first: `POST /recall` — embeds the query via `ports.EmbeddingProvider` and
      calls the same `RecallService.ForText` capture already uses (`13a`); a test asserting no LLM
      completion call occurs (`fakeprovider` configured with zero scripted `capture_processing`
      cases still succeeds) — no classify call on the read path.
      Verify: `make test`.
      Requirement: R2.4.
- [x] **13c.2** Test first (Q3b's conformance property, R2.5): seeding `memrepo`/`fakeprovider` with
      the same corpus, driving one capture classified `recall` and one standalone `POST /recall`
      over identical text, asserting the two ordered candidate-id lists are equal.
      Verify: `make test`.
      Requirement: R2.5.
- [x] **13c.3** `GET /units/{id}` and `GET /units?ids=a,b,c` through `LiveByIDs` (never `ByID`
      exposed over HTTP); a non-`pool` unit returns the **same** 404 shape an unknown id would
      (I02); a shared unit renderer (`id`/`type`/`content`/`status`/`weight`/`event_at`/`due_at`/
      `created_at`/`updated_at` only).
      Test first: a route test seeding one `pool` unit and one `archived` unit, asserting only the
      `pool` unit's route responds successfully and the archived unit's by-id request returns the
      identical not-found shape an unknown id would.
      Verify: `make test`.
      Requirement: R2.6.
- [x] **13c.4** R2.7: a route test asserting `DecisionLog.Record` is never called for `POST /recall`
      or `GET /units/{id}` (an instrumented fake failing the test if `Record` is invoked), asserted
      over all four routes. **See Conflicts §C14's fourth finding: this link registers three read
      routes, not four — implemented over the three.**
      Verify: `make test`.
      Requirement: R2.7.
- [x] **13c.5** L4: `test/e2e` — at least one test starting the compiled `nooma serve` binary
      against a real, migrated, fixture-configured vault, posting a capture, and issuing a recall
      that finds it. **See Conflicts §C14: `Deps.Capture`/`Deps.Recall` are still nil until `13d`'s
      own wiring lands, so this link's own L4 test pins the honest 503 both dependencies answer
      with in this transitional state, over the real compiled binary and a real socket — the same
      test `13d.1`'s own task text already names as what it extends into the full round trip.**
      Verify: `go test ./test/e2e/... -tags e2e`.
      Requirement: R2.8.
- [x] Verify (PR-level): `make check-all`.

---

## PR 13d — `feat/serve-wiring` (~420 — over the ceiling; High risk; **not split, on purpose** —
no natural seam exists between wiring and the shared list it introduces, see intro)

Depends on `13c`.

- [x] **13d.1** `cmd/nooma/serve.go` wires config→providers→repos→`Index`→services→token into
      `Handler(Deps{Version, Capture, Recall, Token})` — the guarded route slice built from this
      wiring, not reconstructed per request.
      Test first: extend `13c.5`'s L4 test so it exercises `serve.go`'s real wiring rather than a
      handler test's manually-built `Deps`.
      Verify: `go test ./test/e2e/... -tags e2e`.
      Requirement: R2.8 (this PR's wiring share); design D10.
- [x] **13d.2** `cmd/nooma/tasks.go` (NEW): `tasksM1Consumes = []string{"capture_processing",
      "relation_evaluation", "embedding"}` — the one list three readers share (D18a). This PR's own
      reader: `serve` resolves exactly these into `CaptureService`/`RecallService`'s ports.
      Test first: an L2 test asserting `serve`'s wiring reads `tasksM1Consumes` rather than a
      hardcoded list, and every member is in `config.DocumentedTaskNames`.
      Verify: `make test`.
      Requirement: design D18a (this PR's reader; `init`'s and `doctor`'s readers land in `15` and
      `16b`).
- [x] Verify (PR-level): `make check-all`; L4 green. **Stop-and-report checkpoint once this PR's own
      cumulative diff crosses roughly 300 lines.** **See Conflicts §C15: crossed at ~453 lines,
      reported per this instruction, continued as instructed.**

---

## PR 17 — `feat/openai-embeddings` (~200)

Depends only on Phase A PR 6 (already shipped). **Can land at any point before `15`, including in
parallel with `12a`**, per design's own note; positioned here in the primary order for narrative
continuity with the chain design walks, not because anything forces this slot.

- [x] **17.1** Test first: `internal/providers/openai/embed_test.go` against `httptest` — request
      path `/v1/embeddings`, `Authorization: Bearer <key>`, request body's `model`/`input`, response
      decode, **the echoed model is what is returned** (not the request's), empty `data` → error,
      non-200 → error carrying the body.
      Implement `internal/providers/openai/embed.go`: `embedRequest`, `embedResponse`, `(c
      *Client) Embed(ctx, req) (ports.EmbedResponse, error)`; `var _ ports.EmbeddingProvider =
      (*Client)(nil)`.
      Verify: `make test`.
      Requirement: R6.1; design D17.
- [x] **17.2** A provider entry's `endpoint` reaches the client as its `baseURL` (the existing
      `config.Provider.Endpoint` field, no schema change); an empty one falls back to the provider's
      default (`https://api.openai.com`) — this is what makes R6.3's L4 form possible in `15`.
      Verify: `go test ./cmd/nooma/...`.
      Requirement: design D17 (the `Endpoint`→`baseURL` passthrough).
      **Already satisfied by `13d`**: `cmd/nooma/wiring.go`'s `buildProvider` already calls
      `openai.NewClient(p.Endpoint, apiKey, p.Model, http.DefaultClient)` — this task required no
      new code, only re-confirming the passthrough exists and `go test ./cmd/nooma/...` stays green.
- [x] Verify (PR-level): `make check-all`; confirm `git diff --name-only` for this PR contains no
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
| 12d | ~380, 153 measured (0.4×) | **High forecast, did not materialize** — two single-column `UPDATE`s reusing `UpdateContent`'s and `requireRowAffected`'s existing shape, and one contract case shared by both `memrepo` and `internal/store/sqlite` rather than one case per implementation, kept this well under its own ceiling. Recorded so a future High-risk store-adapter link is not read as "this class always overruns" — `12c`'s overrun and `12d`'s undershoot are both true at once. PR #111 |
| 12e | ~400, 626 measured (1.57×) | **High, over the ceiling, not split, `size:exception`** — a layer seam (`ports`+L2 fake in one PR, `sqlite`+L3 in a second) was evaluated and rejected: unlike `12c`, `docs-sync` does not apply (no `internal/core/**` file touched, no D13 doc row), but C11's "answered twice, same PR" rule does — splitting would leave `main`'s `SignalRepo` contract answered only by the fake between the two PRs. See Conflicts §C3 |
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
