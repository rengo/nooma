# Tasks — m4c: focus hysteresis and adjacency

Derived from `spec.md` (R1-R10) and `design.md` (§1-§10, APPROVED after Judgment Day round 3;
§10 carries the round-3 corrections, applied below). Third of six slices under
`openspec/changes/m4-mirror-ui/proposal.md`. Shape follows the archived m4b tasks.

**Delivery**: `ask-on-risk`, `stacked-to-main` (each branch targets `main`, rebases after the
previous merge). Strict TDD per link: scaffold (compiles, no behaviour) -> RED (fails on an
assertion, never on "undefined") -> GREEN -> docs -> mutation probes. No tip is red. Every link
runs `make check`, then before its PR `make check-all` **and** `go vet -tags integration,e2e ./...`,
both in an isolated `git worktree` at that link's own commit. Conventional commits, no AI
trailer. Budgets are impl+docs; tests reported apart.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | Link 1 ~330 impl+docs (~230 if 1b is cut), tests ~480; 1b ~100, tests ~120 of that 480 moves with it; Link 2 ~130, tests ~320 |
| 400-line budget risk | Medium (link 1 at ~330 against a 1.3x-2.2x historical overrun; cut pre-defined) |
| Chained PRs recommended | Yes |
| Suggested split | `feat/brain-focus-incumbent` -> (optional 1b `feat/brain-focus-digest-writer`) -> `feat/brain-focus-adjacency` |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: Medium

Decision (one): take the 1b cut up front, or only if link 1 measures over 400 after GREEN.
Recommendation: tasks are already partitioned so either works; take it only on measured
overflow (decide and report).

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Today holds per-Kind incumbent; margin + relation adjacency; digest as second writer (Phase B) | PR 1, base `main` | Phase B moves to 1b on overflow |
| 1b | Digest as writer, `check.focus.unavailable`, `NewCheckService` sites | PR 1b (contingent), base `main` after PR 1 merges | Compiles and passes alone on top of PR 1 |
| 2 | Union adjacency to `Carry`, re-key, mirror reads P' | PR 2, base `main` after PR 1(/1b) | No constructor signature change |

---

## Pre-task (planning artifacts, rides in PR 1's first commit)

- [x] **0.1** `spec.md`: reword R9 to "the union of both Kinds' members of the loaded incumbent,
  re-keyed to trigger ids (design §3.3)"; fix OQ4 body (drop "Left open for design"); R3 wording
  "a freshly constructed keeper". (Correction 3.) Verify: `rg 'left open for design' spec.md` is empty.

---

## PR 1 — `feat/brain-focus-incumbent`

### Phase A: Today as the writer (always in PR 1)

- [x] **1.1** SCAFFOLD — create `internal/brain/focuskeeper.go`: `FocusKeeper{units,cfg,rels,held
  atomic.Pointer[incumbent]}`, `incumbent`, `focusRound{members,next}`, `NewFocusKeeper(units,
  cfg, rels)`, `compute` (returns empty round) and `publish` (no-op). `NewTodayService(…, keeper
  *FocusKeeper)`: parameter named `keeper`, accepted and ignored. `wireFocus(db)` in
  `cmd/nooma/wiring.go`; `wireToday(db, k)`; `serve.go:144` passes one X. Edit call sites:
  `today_test.go:23` and `:364`, `i27_viewing_is_not_delivering_test.go:71`,
  `wiring_today_test.go:35`. `configrepo.go:16` comment names `brain.FocusKeeper`.
  Verify: `make check` and `go vet -tags integration,e2e ./...` pass; no behaviour change.
- [x] **1.2** RED — fresh test files (R3, I01): `test/conformance/i01_incumbent_fresh_service_test.go`
  `TestI01_IncumbentDoesNotSurviveAFreshService` (first keeper expects A held, gets B; fails on
  assertion). `i19_hysteresis_margin_test.go`: `TestI19_TodayHoldsIncumbentInsideMargin`.
- [x] **1.3** RED — `test/conformance/brain_no_package_var_test.go`
  `TestBrain_DeclaresNoPackageLevelVar` (default-deny go/ast; two allowed shapes) with a
  self-proof table: fires on `var defaultKeeper = …`, `atomic.Pointer`, map, int, `sync.Mutex`,
  `*T`; silent on `errors.New`/`fmt.Errorf` sentinels, `var _ T = …`, grouped allowed block.
- [x] **1.4** RED — **NEW file** `cmd/nooma/serve_gate_test.go`, `TestServe_OneFocusKeeperSharedByTodayAndDigest`:
  clause (a) one `wireFocus(` call in `serve.go`, (b) `wireToday` half, (c) `NewFocusKeeper(`
  and `wireFocus(` nowhere else outside `serve.go` and their definition. Probe: two `wireFocus`
  calls, inline `wireToday(db, wireFocus(db))`.
- [x] **1.5** RED — `internal/brain/focuskeeper_test.go` and `today_test.go` (FX-H: 6 fillers
  + A, B, seeded by a first request, identical timestamps): `TestToday_IncumbentHeldInsideMargin`
  (R1 both halves), `TestToday_ConfiguredZeroMarginDisplaces` (R2),
  `TestToday_KindsAreIndependent` (R4), `TestToday_HeldMemberShowsItsOwnRankScore` (R7; Select
  order differs from Rank order), `TestToday_FailedRequestPublishesNothing` (fail
  `questions.Open`), `TestFocusKeeper_PublishReplacesBothKinds`,
  `TestFocusKeeper_ConfigErrorPropagates`, `…_CandidatesErrorPropagates`,
  `…_RelationsErrorPropagates`, R8 fixture (arithmetic in the comment): B-over-C,
  `TestToday_AdjacencyUsesStrengthNotConfidence` (0.9/0.1 vs 0.1/0.9), direction test (A->B and
  B->A), `TestToday_AdjacencyFromSecondMember`, load-Kind adjacency test,
  `TestToday_TaskNotLiftedByLoadIncumbent` (OQ4). R10 own-ranking half: fresh keeper, adjacency 0.
- [x] **1.6** RED — `today_test.go:323-400` `TestToday_RepeatedRequestsLeaveTheMorningDigestByteIdentical`:
  rename/comment to "delivery bookkeeping (surfaced/asked/held rows) is unchanged"; comment
  states the keeper-free runner (`r.focus == nil`) is why. Optional: shared-keeper variant
  asserting only bookkeeping rows. (Correction 1.)
- [x] **1.7** RED — `i27_viewing_is_not_delivering_test.go`: add a `RelationRepo` write guard
  (`Upsert`, `Delete` writes; reads listed in design §5); **recount** the "sixteen distinct method
  names" comment (`:34`, `:112-113`) from the guard; update `:84` "TodayService is stateless" to
  design §5's wording.
- [x] **1.8** GREEN — `focuskeeper.go`: `compute` (load once; `cfg.Load` -> `ResolveMargin`; one
  `ByUnit` sweep over P's members -> `weight.Edge{Strength}`; per Kind `LiveFocusCandidatesByType`
  -> `Rank` with `AdjacencyStrengths(prev[k], edges)` -> `Select`; `scoreByID[id]` indexed
  directly, Select's order), `publish` (`held.Store(r.next)`). `today.go`: compute first, members
  via `LiveByIDs`, publish only on success; fix doc comments saying "no Select".
- [x] **1.9** DOCS — `docs/02-cognitive-core.md` §3: delete lines ~336-339 ("Until `m4c` …");
  keep ~330-335 and ~266-270; add "written by `/ui`'s Today view; the digest becomes the second
  writer in 1b" (full two-writer sentence only if 1b is not cut), the per-focus adjacency
  sentence, the interim sentence ("Until `m4c`'s second link …"). §7 `:1116`: "writes nothing to
  the vault" + in-memory incumbent sentence. `docs/06-harness.md` §4: I27 row (`:269`), I01 row
  names the new test, I19 row names its production caller. Verify: `scripts/docs-sync.sh`.
- [x] **1.10** PROBES — enumerate from `git diff` of production files (every conditional, loop,
  field, switch arm, error branch); reconcile with design §6 and add a row for any not listed.
  Apply, watch red, revert: L1-1 ignore `held.Load`; L1-2 margin `0`/default; L1-3, L1-4, L1-18
  swallow errors; L1-5 wrong Kind's prev; L1-6 `DefaultSize-1`; L1-7 merge/no-op publish;
  L1-8 publish before a later error; L1-9 join by index; L1-10 re-sort; L1-15
  `Confidence`; L1-16 swap (equivalent); L1-17 first member only; L1-19 union; L1-20 empty map.
  Record probe in each GREEN commit body; a probe that never applied is a finding.

### Phase B: digest as second writer (ships in PR 1; **moves whole to 1b on overflow**)

- [x] **1.11** SCAFFOLD — `NewCheckService(…, keeper *FocusKeeper)` appended last, ignored;
  `wireProactive(clock ports.Clock, …, k)` and `wireScheduler(…, k)`; `serve.go:130`;
  `wireCheck` appends `nil`. Append `nil` at 10 sites: `check_effect_completeness_test.go:139`,
  `i15_…:168`, `i16_…:148`, `i09_…:173`; **integration tag** `due_scan_status_vocabulary_test.go:81`,
  `due_scan_concurrent_test.go:89`, `:93`, `pending_question_vocabulary_test.go:118`; **e2e tag**
  `m3_demo_test.go:103`, `m3e_relation_answer_demo_test.go:114` (6 build-tagged edits in 5 files).
  `ports/decisionlog.go`: `ActionCheckFocusUnavailable`, `AllDecisionActions` entry, count
  "forty-seven" -> "forty-eight". `test/support/repocontract/decisionlog.go:133-198`: hand-add
  to the `want` map, retitle the subtest. Verify `go vet -tags integration,e2e ./...`.
- [x] **1.12** RED — digest tests (FX-H seeded; failing `ConfigRepo` toggleable): `TestDigest_FailedSendPublishesNothing`,
  `…_DryRunPublishesNothing`, `…_EmptyDigestPublishesNothing`, `…_NoConversationPublishesNothing`,
  `…_QuestionOnlyDigestPublishesWhenSent`, `…_EmptyDigestDoesNotCompute`,
  `…_NoConversationDoesNotCompute`, `…_FocusErrorStillSendsDigest`,
  `…_FocusErrorRepeatsPerRetryScan`, `…_DryRunFocusErrorWritesNothing` (count equals wet run),
  `…_TodaySeesDigestIncumbent` (R9b), `…_SecondDigestSeesFirst` (R9c). Nil keeper:
  existing i09/i15/i16 must keep passing.
- [x] **1.13** RED — **L1-13b killer**: SEED the keeper (Today holds `{F1…F6, A}`), then toggle a
  failing `ConfigRepo` that fails only the digest's compute; after the digest, assert A is still
  held (Today with flipped weights inside margin still shows A). (Correction 2.)
- [x] **1.14** RED — `serve_gate_test.go` clauses (b) `wireScheduler` half, (c) remainder, (d)
  `wireScheduler` -> `wireProactive` -> `NewCheckService` pass their own parameter, never `nil`
  or a call; `TestWireProactive_DigestSharesTodaysKeeper` (real migrated vault, fake channel,
  `wireProactive` clock seam, FX-H date-insensitive: digest holds A, Today shows A);
  `TestFocusKeeper_ConcurrentTodayAndDigestAreRaceFree` (`-race`, whole snapshot, never a mix).
- [x] **1.15** GREEN — `digest.go`/`check.go`: `if r.focus != nil`; `compute` after the
  conversation check and the `!commit` return, before `Send`; error -> one `decision_log`
  `check.focus.unavailable` (commit only), send the empty-adjacency split, publish nothing;
  publish immediately after `Send` succeeds. Wiring threads one keeper.
- [x] **1.16** DOCS — doc 02 §3 full two-writer sentence (headless process holds one once a digest
  ran); §7 "the next digest then reads"; spec R9/R10 digest wording confirmed; `rg 'check\.digest\.held' docs/`
  for `check.*` enumerations (edit may be empty). Under the cut these three move here from 1.9.
- [x] **1.17** PROBES — L1-11 remove guard; L1-12 publish before/after-failed/dry/removed;
  L1-12b, 12c publish moved above their returns; L1-12d gate on `len(carry)>0`; L1-12e and 12f
  move `compute` up; L1-13 return error; L1-13a record in dry run; L1-13b publish zero round;
  L1-14, 14b, 14c two keepers / `nil`. Each with its killing test from 1.12-1.14.

---

## PR 2 — `feat/brain-focus-adjacency` (after PR 1 / 1b merges)

- [ ] **2.1** SCAFFOLD — `focusRound` gains `adjacent`, `nextAdjacent` (unset); `carryAdjacency(byUnit,
  pending)` returns `nil`, in `digest.go`. `make check` and tagged vet green.
- [ ] **2.2** RED — FX-L fixtures (low energy, >=4 pending, fillers above and below, **trigger
  ids != unit ids**, one nil-`UnitID` trigger, **trigger units typed in neither Kind, e.g.
  knowledge** (Correction 5)): `TestDigest_AdjacentItemCarriedAhead` (asserts the rendered
  digest too), `TestDigest_ItemAdjacentToLoadFocusCarried`, `TestDigest_FreshKeeperHasNoAdjacency`.
- [ ] **2.3** RED — `TestToday_PendingDigestMatchesDigestOrder`, two table cases (task, load;
  P != P'), each asserting (1) mirror order `Y, X`, (2) shared-keeper `CheckService` equals the
  mirror, (3) fresh-keeper control carries `X, Y`; plus
  `TestToday_FirstRequestMirrorUsesNextAdjacencyOnFreshKeeper` (no seed request).
- [ ] **2.4** RED — wiring: Today -> digest direction in `TestWireProactive_DigestSharesTodaysKeeper`;
  I27 guard still passes with the mirror's new relation reads.
- [ ] **2.5** GREEN — `focuskeeper.go`: `adjacent` = union vs P, `nextAdjacent` = union vs P'
  (memoized `ByUnit` covers members of P and P'); `carryAdjacency` re-key, skip nil `UnitID`;
  `today.go` mirror `Carry` gets `carryAdjacency(round.nextAdjacent, …)`; `digest.go` second
  `Carry` gets `round.adjacent` and its result is sent; delete the "Adjacency is M4's" comment.
- [ ] **2.6** DOCS — delete the interim sentence; add the union/P' sentence; append the restart
  clause after "Two effects from one restart, not one."; §7 consequence ("viewing `/ui` can
  change a later low-energy digest's order"); `docs/06-harness.md` I27 row P' note. Verify
  `scripts/docs-sync.sh`.
- [ ] **2.7** PROBES from the production diff: L2-7 task only / vs `next`; L2-8 un-re-keyed;
  L2-9 dereference nil `UnitID`; L2-10 empty map / vs P; L2-11 vs P; L2-11b `P'.task`/`P'.load`
  alone; L2-12 swap `adjacent`/`nextAdjacent`; L2-12b mirror reads P; L2-13 memo only P;
  L2-14; L2-15 send step-2 split. Record each in its commit body.

### PR 1 apply notes (applied whole; 1b cut not taken)

Measured after GREEN: implementation + docs = 299 changed lines (impl 271, docs 28), under
the 400 ceiling, so Phase B stayed in PR 1. Tests: ~1900 changed lines, reported apart.
Deviations from the plan, none behavioural:

- **1.1** the scaffold's `compute` returns an empty round and `publish` stores it (instead of
  a no-op), because `golangci-lint`'s `unused` rejects a keeper whose fields nothing touches.
  Today already calls both, so the scaffold compiles, lints and changes nothing.
- **1.4/1.14** the serve gate is one checker (`serveGateViolations`) proved against probe
  sources; clause (d) also pins `wireCheck` to `nil`.
- **1.10/1.17** probes ran from the production diff and added mutants the design's table did
  not list (relations loop, `LiveByIDs` error, publish placed before a later read, audit-row
  write failure); each got a killing test. L1-16 is equivalent, as designed.
- Guard tests with no natural RED (publish already existed in the scaffold):
  `TestFocusKeeper_ConcurrentTodayAndDigestAreRaceFree`, killed by a plain-pointer mutant
  under `-race`.

## Closing (each link)

- [ ] **C.1** `make check-all`, `go vet -tags integration,e2e ./...`, `scripts/docs-sync.sh`, all in
  an isolated worktree at the link's commit; PR per `nooma-pr`; PR body lists impl+docs vs test lines.
