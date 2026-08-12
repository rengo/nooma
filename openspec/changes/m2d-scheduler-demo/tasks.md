# Tasks — M2 Phase D: the scheduler and the demo

Implementation task list for `m2d-scheduler-demo`, derived from `spec.md` (R0–R4.6) and
`design.md` (§1–§14), both read in full before this document. `m2d` is M2's last phase change —
its exit criterion is `docs/05-build-plan.md` §M2's own demo bullet and the umbrella proposal's
final success criterion (R4.6), discharged nowhere but PR 9b below.

Chain strategy **`stacked-to-main`**, delivery strategy chained PRs. Design §13's re-derivation is
authoritative over the proposal's original four-PR row: **9 links, 3 pre-drawn splits, up to 12
real PRs**, per the owner rulings taken 2026-08-11 (`sdd/m2d-scheduler-demo/owner-rulings`,
`sdd/m2d-scheduler-demo/owner-rulings-round-2`) — restated here so they bind, not re-opened:

1. `consolidation_enabled = false` gates **both** the cron and the boot catch-up. ADR-0009 stands
   unamended (non-negotiable #2) — its "always recovered" contrasts recovered-vs-expired
   staleness against `time_based`/timer triggers, not a user switch.
2. The cron's `03:00` is **local** time.
3. An aborted unattended pass logs to the **process log only**, never `decision_log` (`m2c` I12's
   own effect-scoping).
4. A machine asleep across 03:00 fires late on resume — **accepted, no wake-up detection**. Where
   Nooma runs 24/7 is the deployment's problem, not this change's.
5. `TriggerStalenessHours`/`TimerStalenessHours` are **deferred to M3** — no M2 reader, `unused`
   does not fire on exported identifiers, and whoever reads them writes them. Only
   `CatchUpStalenessHours` lands in `m2d`. This **overrides** design §13's own PR-5 scope line
   (a stale answer to design §12 Q3, written before this ruling) — see PR 5's own correction note.
6. The demo uses **one corpus** for `archive`, `connect` and `derive` together (spec R4.4) — three
   corpora would prove three mechanisms, not the one-vault story `docs/05-build-plan.md` tells. If
   fake-provider tuning runs long, split it into its own link rather than weakening the demo.
7. A fire skipped by the closed overlap gate (D4) goes to the **process log only**.

**A design-internal inconsistency, resolved here rather than carried forward silently.** Design
§13's PR-scope table lists PR 1 (`feat/scheduler-core-decisions`) as carrying the "doc 02 §6
sentence on `consolidation_enabled` gating both triggers" — but design §3.3 itself, in the same
document, states plainly: *"Three places carry it, **all in the catch-up PR**"* (doc 02, the gate's
own doc comment, and §3.3 itself). This document follows §3.3, the more specific mechanism
placement, matching how the reasoning is actually load-bearing where the catch-up gate lives: the
doc 02 §6 sentence is **PR 4**'s task (4.12), not PR 1's. PR 1's own diff does not touch doc 02 §6.

**Two hard rules, structurally guaranteed, stated once rather than per task**: `internal/core`
stays pure by construction — `schedule.go`'s three functions take `(lastRunAt, now, ...)` as
parameters, never call `time.Now()`, and PR 1 adds no import that could violate it; `core-purity`
and `forbidigo` (existing gates) need no new test, matching `m2c` R0.2's own posture. No test in
this chain touches the network or a real LLM — every judge/embedding call in PR 9a/9b goes through
`test/support/fakeprovider` (R4.3), asserted explicitly, not merely assumed. `m2d` adds **no
migration** — `config.consolidation_enabled`/`consolidation_last_run_at` both exist since migration
0002; this chain is their first scheduler-side reader (design §11).

**Strict TDD is active** (`CLAUDE.md` non-negotiable #4). Every behavioral task states the
two-commit shape: **commit 1** is the test plus a stub with the final signature returning zero
values (red for the right reason); **commit 2** is the implementation (green). Where a genuine red
is structurally impossible — a `.golangci.yml` config edit with no matching Go violation yet, a
scan whose assertion is vacuously true until a later PR adds the first file it could flag — this
document says so explicitly at the task, per `m2a` C9's convention against claiming a red step that
cannot occur, and forward-references the PR where the check becomes genuinely live.

Every PR runs `make check-all` before opening. `docs-sync.yml` fires on `internal/core/`; only
PR 1 touches that directory in this chain (`internal/core/consolidation/schedule.go`), so it is the
only PR here that needs a genuine `docs/02-cognitive-core.md` delta from `docs-sync`'s own
perspective — PR 4 also amends doc 02 (§6, plus two §13 prose amendments), not gated by `docs-sync`
(no `internal/core/` file in that PR), done anyway per non-negotiable #1.

**Chain-merge verification is a checklist item at every merge point**, per `nooma-pr`'s "Merging a
Chain" section, restated at every PR's Verify block below:

1. `git ls-remote --heads origin <merged-branch>` returns nothing (the branch is gone).
2. `gh pr view <next-pr> --json baseRefName` names `main`, not the branch just merged.

---

## Handoffs inherited from `m2c`, and what this document does with each

`m2c`'s own closing "Handoffs left open" section named two items for `m2d` directly — carried here,
not rediscovered:

- **"The scheduler, ADR-0009's boot catch-up, `serve` wiring, and the simulated-weeks demo golden
  set are all `m2d`"** — the whole of this document.
- **"`consolidation_enabled` gates the scheduler, never the explicit CLI invocation"** (`m2c` design
  §7.4/§12 Q3) — inherited as a closed decision, not re-opened here: `nooma consolidate` (M2c's own
  CLI path) stays ungated by the flag; only the two scheduler-triggered entry points (cron, catch-up)
  read it, per spec R1.2/R2.4.

---

## PR 1 — `feat/scheduler-core-decisions` (~130 impl+docs / ~200 test)

Depends on nothing outside this change. Ships the whole of `m2d`'s `internal/core` surface in one
PR (design §4: "two files, one of them a test" — nothing else under `internal/core/**` is touched
by any later link in this chain).

- [x] **1.1** Commit 1 (RED): `internal/core/consolidation/schedule_test.go` (new) —
      `TestCatchUpDue` table: `nil` `lastRunAt` → due at any `now`; `now - 23h59m` → not due;
      `now - 24h` exactly → not due (ADR-0009's "more than 24h", strict, mirroring §6's own
      strict-comparison convention); `now - 24h - 1s` → due; a future `lastRunAt` (`now + 1h`) →
      never due (signed comparison, no repair invented).
      **Red**: `undefined: consolidation.CatchUpDue` — package does not compile (`schedule.go`
      does not exist).
      Requirement: spec R2.1; design §4 (`CatchUpDue`'s doc comment).
- [x] **1.2** Commit 2 (GREEN): `internal/core/consolidation/schedule.go` (new) —
      `const CatchUpStalenessHours = 24` (**untyped**, not `time.Duration` — design D2/§3.2: the
      calibration gate's anchored `calibrationLeadingNumber` regex must read the literal `24`, not
      `86400000000000`) and `func CatchUpDue(lastRunAt *time.Time, now time.Time, stalenessHours
      int) bool`.
      Verify: `go test ./internal/core/consolidation/...`.
      Requirement: spec R2.1, R2.2; design §3.2 (D2), §4.
- [x] **1.3** Commit 1 (RED): `schedule_test.go` (extend) — `TestResolveConsolidationEnabled`
      table: `nil` → `true` (migration `0002:65`'s own `DEFAULT 1`), `&false` → `false`, `&true` →
      `true`.
      **Red**: `undefined: consolidation.ResolveConsolidationEnabled` — package does not compile.
      Requirement: spec R1.2; design §3.1 (D1).
- [x] **1.4** Commit 2 (GREEN): `schedule.go` (extend) — `func ResolveConsolidationEnabled
      (configured *bool) bool`.
      Verify: `go test ./internal/core/consolidation/...`.
      Requirement: spec R1.2; design §3.1, §4.
- [x] **1.5** Commit 1 (RED): `schedule_test.go` (extend) — `TestNextDailyRun` table, via
      `time.FixedZone` (no tzdata dependency): before the hour today (`02:00`, hour `3` → today
      `03:00`); after the hour today (`04:00`, hour `3` → tomorrow `03:00`); exactly on the hour
      (`03:00:00`, hour `3` → tomorrow `03:00`, "strictly after" per design §4's own comment); across
      a month/year boundary (Dec 31 23:00 → Jan 1 03:00).
      **Red**: `undefined: consolidation.NextDailyRun` — package does not compile.
      Requirement: design §3.1 (D1), §4.
- [x] **1.6** Commit 2 (GREEN): `schedule.go` (extend) — `func NextDailyRun(after time.Time, hour
      int) time.Time`.
      Verify: `go test ./internal/core/consolidation/...`.
      Requirement: design §3.1, §4.
- [x] **1.7** `schedule_test.go` (extend) — `TestNextDailyRun_DST`: spring-forward normalization (a
      non-existent local `03:00` normalizes forward, `time.Date`'s own behavior) and fall-back
      (the first `03:00` wins) over a real zone, `import _ "time/tzdata"` **in the test file only**
      — `time.LoadLocation` has no zone database on Windows otherwise, and this repo cross-compiles
      for Windows (ADR-0013); a test-only import keeps tzdata out of the shipped binary. Runs
      against `1.6`'s already-committed implementation — no separate red/green pair, the case is an
      edge of the same function.
      Verify: `go test ./internal/core/consolidation/... -run TestNextDailyRun_DST`.
      Requirement: design §9 (testing strategy, `NextDailyRun` DST row).
- [x] **1.8** `docs/02-cognitive-core.md` §13 — add the new row for `catch_up_staleness_hours`,
      text verbatim from design §3.2: `` | `catch_up_staleness_hours`
      (`internal/core/consolidation.CatchUpStalenessHours`) | 24 — ADR-0009's boot catch-up gate;
      coincides with `incomplete_expiry_hours` above by coincidence, not by relation (a startup
      staleness window versus a phase's expiry window), no test ties them | ``. **Not** the §6
      sentence — see this document's opening correction; that lands in PR 4.
      Requirement: spec R0.3, R2.2; design §3.2.
- [x] **1.9** `schedule.go` — GoDoc comments for all four exported symbols, verbatim per design §4:
      `CatchUpStalenessHours`'s doc names ADR-0009 and doc 02 §13; `CatchUpDue`'s doc states the
      `nil`-is-always-due reading and the strict-comparison/no-repair-for-future decisions;
      `ResolveConsolidationEnabled`'s doc names the `DEFAULT 1` precedent; `NextDailyRun`'s doc
      states the DST-normalization behavior explicitly, so a reader does not have to find the
      design doc to understand the signature.
      Requirement: design §4 (reasoning belongs in the comment, `nooma-core` convention).
- [x] **1.10** Verify: `go test ./test/conformance/... -run TestCalibrationDoc` (or the suite's
      actual test name) — confirms the new §13 row is picked up automatically by the existing gate
      (R0.3/R2.2's own Verified-by clause); `calibrationMinSymbols`'s floor moves by exactly one,
      no other row needs bumping.
      Requirement: spec R0.3, R2.2 (Verified by).
- [x] **1.11** `golangci-lint run`; `go test -race ./internal/core/consolidation/...`.
- [x] Verify (PR-level): `make check-all`; diff scope — `internal/core/consolidation/schedule.go`
      (new), `internal/core/consolidation/schedule_test.go` (new), `docs/02-cognitive-core.md`
      (one new §13 row only, **not** §6 — corrected above). Target ≤130 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/scheduler-core-decisions` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR2> --json baseRefName` names `main`.

      **PR 1 result** (`feat/scheduler-core-decisions`, PR #180): `make check-all` green end to
      end; `internal/core` coverage floor 749/749 (100%, up from 738/738);
      `TestHarness_CalibrationTableMatchesConstants` green (`CatchUpStalenessHours` subtest
      passes; `calibrationMinSymbols` left at 21 — it is a floor, not an equality, per its own
      doc comment, and 22 live rows already clear it; not part of this PR's declared diff scope).
      Changed lines: 71 impl+docs (`schedule.go` 70 + doc02 1) / 189 test — both under budget.
      Diff scope matched exactly (3 files, no strays). Chain-merge checks deferred to actual
      merge time (not yet merged as of this apply batch).

---

## PR 2 — `feat/scheduler-boundary-lint` (~70 impl+docs / ~90 test)

Depends on PR 1 (references the same three core symbols the boundary scan below will eventually
check for). Ships the `scheduler-boundary` depguard rule and the source-scan scaffolding design
§3.1 item 4 calls for — genuinely live only once PR 3a/PR 4 add the package's first real files.

- [x] **2.1** `.golangci.yml` — add the `scheduler-boundary` rule, verbatim from design §7: `files:
      ["**/internal/scheduler/**"]`, `deny`: `internal/store` (vault access), `database/sql`
      (redundant with `sqlite-containment`, kept for a self-contained rule statement),
      `internal/providers` (no direct model calls), `internal/httpapi` (no transport knowledge).
      **No genuine red is possible here**: `internal/scheduler` is `doc.go` alone today, so no file
      exists that could violate the rule — disclosed per `m2a` C9 rather than claimed as red, same
      posture `m2c` R0.1 took for its own three depguard rules.
      Requirement: spec R0.1; design §7.
      **Done** (commit `b1c8a82`).
- [x] **2.2** Verify: `golangci-lint run` (or `make check`'s lint step) — 0 issues, confirming the
      rule is syntactically valid and vacuously satisfied.
      Requirement: spec R0.1 (Verified by).
      **Done** — `make lint` → `0 issues.`
- [x] **2.3** `test/conformance/scheduler_boundary_scan_test.go` (new) — leg 1: no non-test file
      under `internal/scheduler` contains the literal `time.Hour` (design §3.1 item 4's realistic
      regression: someone re-deriving "24h" or "03:00" inline instead of through the three core
      symbols). **Vacuously true today** — `doc.go` has no such literal — disclosed, not claimed as
      red; becomes a live guard from PR 3a onward.
      Requirement: design §3.1 item 4.
      **Done** (commit `cbccf99`).
- [x] **2.4** Verify: `go test ./test/conformance/... -run TestSchedulerBoundaryScan` passes today
      (trivially, per 2.3's disclosure).
      Requirement: design §3.1 item 4.
      **Done** — `PASS`, leg 1 subtest green (vacuously, `doc.go` scanned, no violation).
- [x] **2.5** Forward reference, in the same test file's own doc comment: leg 2 (every non-test,
      non-`doc.go` file under `internal/scheduler` references all three of `CatchUpDue`,
      `ResolveConsolidationEnabled`, `NextDailyRun` at least once) is added as PR 4's own task
      (4.11), once `CatchUpDue` finally has a real caller — the same forward-reference pattern
      `m2c` task 1.6 used for a leg that could not exist yet.
      Requirement: design §3.1 item 4 (forward reference only).
      **Done** — recorded in `scheduler_boundary_scan_test.go`'s own doc comment, not implemented.
- [x] Verify (PR-level): `make check-all`; diff scope — `.golangci.yml`,
      `test/conformance/scheduler_boundary_scan_test.go` (new). Target ≤70 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/scheduler-boundary-lint` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR3a> --json baseRefName` names `main`.
      **Done** — `make check-all` green end to end; `internal/core` coverage 750/750 (100%,
      unchanged — this PR adds no core code); diff scope matched exactly (`.golangci.yml` +17/-0,
      `test/conformance/scheduler_boundary_scan_test.go` +84/-0, 101 changed lines total, both
      files under their ~70/~90 targets). Chain-merge checks deferred to actual merge time (PR not
      yet merged), same posture PR 1 took.

---

## PR 3a — `feat/scheduler-cron` (~200 impl+docs / ~260 test)

Depends on PR 2. Ships `internal/scheduler`'s first real code: the lifecycle skeleton, the timer
seam, and the daily cron loop calling `Consolidate` with no overlap protection yet (D4's slot is
PR 3b's own scope — this link's `runPass` calls `Consolidate` directly). **Pre-drawn split, per
design §13**: 3a (skeleton + cron loop, ~200) | 3b (the overlap guard, ~60).

- [x] **3a.1** `internal/scheduler/doc.go` (modify) — extend the package comment: what the package
      owns (goroutines, the timer seam, the process log) and what it does not (any decision — every
      `if` over a duration, an hour, or a `*bool` lives in `internal/core/consolidation`). Not a
      red/green pair — a doc comment, verified by review only.
      Requirement: design §2, §3.1.
- [x] **3a.2** Commit 1 (RED): `internal/scheduler/scheduler_test.go` (new) — `New` rejects a `nil`
      `Clock`, a `nil` `Config`, and a `nil` `Consolidate` (three cases).
      **Red**: `undefined: scheduler.New` / `scheduler.Deps` / `scheduler.Scheduler` — package does
      not compile.
      Requirement: design §5.2.
- [x] **3a.3** Commit 2 (GREEN): `internal/scheduler/scheduler.go` (new) — `Consolidator` interface,
      `Deps` struct (`Clock`, `Config`, `Consolidate`, `Log io.Writer`, `Timer timer` — the only
      optional field), `Scheduler` struct, `New(d Deps) (*Scheduler, error)` validating the three
      non-nil deps.
      Verify: `go test ./internal/scheduler/...`.
      Requirement: design §5.2.
- [x] **3a.4** `internal/scheduler/timer.go` (new) — the `timer` seam (an interface over
      `time.After`) and its real implementation calling `time.After` directly (`internal/scheduler`
      is outside `forbidigo`'s scope, design §5.2). **No meaningful red**: a bare interface satisfied
      trivially by both implementations — disclosed; proven indirectly by 3a.5's own cron test.
      Requirement: design §5.2 ("Why a package-local `timer` seam").
- [x] **3a.5** Commit 1 (RED): `internal/scheduler/cron_test.go` (new) — a fake clock/timer advanced
      past `ConsolidationHour` asserts exactly one whole-pass call (`Phase == nil`) to the fake
      `Consolidator`; a clock/timer that never reaches the hour asserts zero calls.
      **Red**: `undefined: scheduler.ConsolidationHour` / `cron.go`'s loop — package does not
      compile.
      Requirement: spec R1.1; design §5.3.
- [x] **3a.6** Commit 2 (GREEN): `scheduler.go` (extend) — `const ConsolidationHour = 3` (local
      time — owner ruling round 1 #2; design §5.1 places this constant in `scheduler.go` alongside
      `BootConsolidationDelay`, "the constants", not in `cron.go`). `internal/scheduler/cron.go`
      (new) — the daily loop: `next := NextDailyRun(Clock.Now(), ConsolidationHour)`,
      `select { timer.After(next.Sub(now)) | ctx.Done() → return }`, on fire call
      `runPass(ctx, "cron")`. `runPass` (this commit's version, no gate check, no overlap guard yet)
      calls `Consolidate(ctx, brain.ConsolidateRequest{})` unconditionally.
      Verify: `go test ./internal/scheduler/...`.
      Requirement: spec R1.1; design §5.1, §5.3.
- [x] **3a.7** Commit 1 (RED): `cron_test.go` (extend) — a fixture with `ConsolidationEnabled =
      &false` asserts the fired tick calls `Consolidate` zero times.
      **Red**: `3a.6`'s `runPass` has no gate check — fails, calling `Consolidate` once.
      Requirement: spec R1.2.
- [x] **3a.8** Commit 2 (GREEN): `scheduler.go` (extend) — `runPass` reads `Config.Load`, resolves
      `consolidation.ResolveConsolidationEnabled`; `false` → return before calling `Consolidate` —
      no pass, no `decision_log` rows, no `consolidation_last_run_at` write, no side effect beyond
      the `Config.Load` read (R1.2's own MUST).
      Verify: `go test ./internal/scheduler/...`.
      Requirement: spec R1.2; design §5.3.
- [x] **3a.9** Commit 1 (RED): `scheduler_test.go` (extend) — `Wait(ctx)` returns once the cron
      goroutine unwinds after `ctx` cancellation, or when `ctx` itself is done first.
      **Red**: `undefined: Scheduler.Wait` — package does not compile.
      Requirement: design §5.2, §3.5 (D5, mechanical join only — the shutdown-budget wiring is
      PR 7's own scope).
- [x] **3a.10** Commit 2 (GREEN): `scheduler.go` (extend) — `Start(ctx)` spawns the cron goroutine
      into a `sync.WaitGroup`; `Wait(ctx)` blocks on the group or `ctx.Done()`, whichever first. The
      catch-up goroutine's own `Add(1)`/`Done()` is PR 4's addition to the same group — documented
      as a known gap in this commit's own comment, not silently assumed complete.
      Verify: `go test ./internal/scheduler/...`.
      Requirement: design §5.2.
- [x] **3a.11** Verify: `go test ./test/conformance/... -run TestSchedulerBoundaryScan` — confirm
      PR 2's leg 1 (no `time.Hour` literal) still passes now that `scheduler.go`/`cron.go`/`timer.go`
      exist — the guard is live for the first time, not vacuous.
      Requirement: design §3.1 item 4.
      **Done** — `PASS`, and genuinely live confirmed by a disclosed temporary probe (not merely
      passing because there is nothing to check): a throwaway `time.Hour` literal was inserted into
      `scheduler.go`, the scan failed pointing at that exact line, then the file was reverted to its
      prior committed state (`git diff` empty afterward) — the same disclosed-probe convention
      `m2c` task 9.7 used, restated in this document's own PR 3a preamble note. `scanned` was 4
      non-test `.go` files (`doc.go`, `scheduler.go`, `cron.go`, `timer.go`), not the vacuous 1 of
      PR 2's own leg-1 result.
- [x] **3a.12** *(split checkpoint)*: measure `git diff --stat` for tasks 3a.1–3a.11 in isolation
      against the ~200/260 sub-estimate. The 3a/3b boundary is already pre-drawn (design §13); this
      checkpoint decides only whether 3a itself needs a further, undrawn split — flag and report if
      at risk rather than split reflexively.
      **Done, no further split** — `git diff --stat main...HEAD -- internal/scheduler/` for tasks
      3a.1–3a.11: impl+docs (`doc.go` +19, `scheduler.go` +139, `cron.go` +26, `timer.go` +28) = 212
      lines, next to the 200-line sub-estimate; test (`scheduler_test.go` +149, `cron_test.go` +177)
      = 326 lines, ~25% over the 260-line sub-estimate. Flagged rather than silently absorbed: the
      overrun is entirely in test lines, and CLAUDE.md's own 400-line PR ceiling counts
      implementation + docs only, test lines counted and reported separately
      (`docs/06-harness.md` §7) — 212 sits comfortably under that ceiling with task 3a.13 still to
      come (lint/race only, no new lines). No further, undrawn split of 3a is warranted.
- [x] **3a.13** `golangci-lint run`; `go test -race ./internal/scheduler/...`.
      **Done** — `make lint` → `0 issues.`; `go test -race ./internal/scheduler/... -v` → all 6 top
      level tests (`TestCron_FiresAfterHourElapses`, `TestCron_NeverFiresBeforeCtxCancelled`,
      `TestCron_GatedOff_FiredTickCallsConsolidateZeroTimes`, `TestNew_RejectsNilDeps` (3 subtests),
      `TestNew_AcceptsValidDeps`, `TestScheduler_Wait` (2 subtests)) `PASS`, no race reported.
- [x] Verify (PR-level): `make check-all`; diff scope — `internal/scheduler/{doc,scheduler,cron,
      timer}.go` (+ tests, new). Target ≤200 impl+docs lines. **No wake-up detection is implemented
      anywhere in this loop** — a machine asleep across 03:00 fires late on resume by construction
      (owner ruling round 2 Q2, accepted) — recorded here since this is the file that would carry
      it if it existed.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/scheduler-cron` returns nothing
      after merge.
      **Chain-merge check 2**: `gh pr view <PR3b> --json baseRefName` names `main`.

      **PR 3a result** (`feat/scheduler-cron`): `make check-all` green end to end (see PR-level
      evidence below); `internal/core` coverage floor unchanged at 750/750 (100%) — this PR adds no
      `internal/core` code, only a new consumer of the already-shipped
      `internal/core/consolidation` symbols. Diff scope matched exactly: `doc.go` (modified, +19/-1),
      `scheduler.go` (new, 139 lines), `cron.go` (new, 26 lines), `timer.go` (new, 28 lines),
      `scheduler_test.go` (new, 149 lines), `cron_test.go` (new, 177 lines) — no strays, no
      `internal/core`, `cmd/nooma`, or `docs/` file touched. Impl+docs 212 lines (~106% of the
      ~200 sub-estimate, task 3a.12's own flagged-not-split conclusion); test 326 lines (~125% of
      the ~260 sub-estimate). Both well under CLAUDE.md's 400-line impl+docs ceiling.

      **Chain-merge check 1, confirmed**: PR #182 merged to `main` at `696d4d6`;
      `git ls-remote --heads origin feat/scheduler-cron` returns nothing.

      **Judgment Day correction JD-3a-01**: `runPass` discarded the `Config.Load` error, so a read
      failure resolved `ConsolidationEnabled == nil` to `true` and opened the R1.2 gate. Fixed by
      failing closed — commits `3f1ec4a` (RED) and `2763cad` (GREEN), both on `main` via PR #182.

      **Chain-merge check 2, for PR 3a, confirmed**: `gh pr view 183 --json baseRefName` →
      `"main"` — PR 3b (`feat/scheduler-overlap-guard`, GitHub PR #183) targets `main`.

---

## PR 3b — `feat/scheduler-overlap-guard` (pre-drawn split of 3a) (~60 impl+docs / ~140 test)

Depends on PR 3a. Ships D4's non-blocking try-lock — the concurrency guard design §13 names as "the
slowest part to get right", split from the mechanism landing before it, the same seam `m2c` used.

- [x] **3b.1** Commit 1 (RED): `internal/scheduler/scheduler_test.go` (extend) —
      `TestScheduler_NoOverlap_ExactlyOneInFlight`: a slow fake `Consolidate` blocked on a channel,
      two fires landing inside that window (a cron fire plus a direct `runPass(ctx, "test")` call —
      the catch-up trigger does not exist until PR 4) assert exactly one call is ever in flight, run
      under `-race`.
      **Red**: genuinely red against PR 3a's own committed `runPass` — no slot exists, so the second
      fire proceeds concurrently, and the "exactly one in flight" counter observes two.
      Requirement: spec R1.3; design §3.4 (D4).
      **Done** (commit `1573be8`) — observed failing before the green: `max concurrent Consolidate
      calls = 2, want exactly 1`, exactly the stated reason.
- [x] **3b.2** Commit 2 (GREEN): `scheduler.go` (extend) — `slot chan struct{}` (capacity 1) on
      `Scheduler`; `runPass` wraps the `Consolidate` call in the non-blocking try-lock/skip pattern
      from design §3.4's exact shape (`select { s.slot <- struct{}{}: ... ; default: skip+log }`).
      Verify: `go test ./internal/scheduler/... -race`.
      Requirement: spec R1.3; design §3.4.
      **Done** (commit `316a4be`) — green, no race.
- [x] **3b.3** Commit 1 (RED): `scheduler_test.go` (extend) — a skipped fire logs one line naming
      the trigger it skipped and the trigger holding the slot, over a `bytes.Buffer` log fixture.
      **Red**: `3b.2`'s skip path exists but the log line's exact content is unasserted until this
      test — fails first on the message text.
      Requirement: design §3.4, §5.4.
      **Disclosed, not a genuine red** (commit `909c3dc`, combined with 3b.4 below — one commit, no
      code change between them): task 3b.2's own text already quotes design §3.4's exact shape
      (`"default: skip+log"`) as part of its own scope, so the `logf` call landed complete inside
      3b.2's commit. `TestScheduler_SkippedFire_LogsOneLine` passed on first run — there was no
      implementation state in which it could have failed. A second disclosure in the same commit:
      the test asserts a narrower claim than this task's own prose ("naming the trigger it skipped
      **and** the trigger holding the slot") — design §3.4's own code block and task 3b.4's own
      verbatim text both give a single `%s` (the skipped trigger only); no task adds state recording
      which trigger holds the slot, so that half of this task's prose has no matching implementation
      to test. Followed the verbatim design instead of the broader prose.
- [x] **3b.4** Commit 2 (GREEN): `scheduler.go` (extend) — `s.logf("scheduler: %s fire skipped, a
      pass is already running", trigger)` on the `default` branch (owner ruling round 2 Q1: process
      log only, no `decision_log` row for a skip).
      Verify: `go test ./internal/scheduler/...`.
      Requirement: design §3.4, §5.4; owner ruling round 2 Q1.
      **Done** — already implemented as part of 3b.2's own commit (see 3b.3's disclosure above); no
      separate code change, `TestScheduler_SkippedFire_LogsOneLine` (commit `909c3dc`) is its own
      Verify.
- [x] **3b.5** `golangci-lint run`; `go test -race -shuffle=on ./internal/scheduler/...`.
      **Done** — `make lint` → `0 issues.`; `go test -race -shuffle=on ./internal/scheduler/... -v
      -count=5` → all tests `PASS` across all 5 shuffled runs, no race reported; a further isolated
      `go test -race -run TestScheduler_NoOverlap_ExactlyOneInFlight -count=20` also green, no race
      — the concurrency test proven non-flaky over 25 total runs.
- [x] Verify (PR-level): `make check-all` incl. `-race`; diff scope — `internal/scheduler/
      scheduler.go` (+ tests, extended only, no new file). Target ≤60 impl+docs lines. Threat
      matrix discharge: design §10's "Concurrency" row (two triggers into one non-reentrant pass) —
      planned RED test is 3b.1.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/scheduler-overlap-guard` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR4> --json baseRefName` names `main`.

      **PR 3b result** (`feat/scheduler-overlap-guard`): `make check-all` green end to end;
      `internal/core` coverage floor unchanged at 750/750 (100%) — this PR adds no `internal/core`
      code. Diff scope matched exactly: `scheduler.go` (extended, no new file), `scheduler_test.go`
      (extended) — no strays, no `internal/core`, `cmd/nooma`, or `docs/` file touched. `git diff
      --stat main...HEAD -- internal/scheduler/`: `scheduler.go` +32/-6 (38 changed lines), impl+docs
      well under the ~60 sub-estimate; `scheduler_test.go` +181/-0 (181 test lines), ~129% of the
      ~140 sub-estimate — flagged, not split: the overrun is entirely in test lines, and CLAUDE.md's
      400-line PR ceiling counts impl+docs only. Chain-merge checks deferred to actual merge time
      (PR not yet merged as of this apply batch), same posture PR 1, PR 2, and PR 3a took.

---

## PR 4 — `feat/scheduler-boot-catchup` (~150 impl+docs / ~220 test)

Depends on PR 3b (shares the slot and `runPass`). Ships ADR-0009's boot catch-up: the cancellable
120s delay, the D3 gate (round 1 ruling 1: gates both triggers), and — per this document's opening
correction — the doc 02 §6 sentence design §3.3 itself says belongs here.

- [x] **4.1** Commit 1 (RED): `internal/scheduler/catchup_test.go` (new) — `TestCatchUp_FiresAfter
      Delay`: `CatchUpDue` evaluates true at boot (a `lastRunAt` older than `CatchUpStalenessHours`)
      → `Consolidate` not called before 120s elapses, called once after, via a fake clock/timer.
      **Red**: `undefined: scheduler.BootConsolidationDelay` / `catchup.go`'s goroutine — package
      does not compile.
      Requirement: spec R2.3; design §5.1, §5.3.
- [x] **4.2** Commit 2 (GREEN): `scheduler.go` (extend) — `const BootConsolidationDelay = 120 *
      time.Second` (alongside `ConsolidationHour`, per design §5.1's "the constants"). `internal/
      scheduler/catchup.go` (new) — at `Start`, `Config.Load` once, resolve
      `consolidation.ResolveConsolidationEnabled` (owner ruling round 1 #1: gates catch-up too —
      `false` → return, no catch-up at all), then `consolidation.CatchUpDue(cfg.
      ConsolidationLastRunAt, Clock.Now(), consolidation.CatchUpStalenessHours)` — due →
      `select { timer.After(120s) | ctx.Done() → return }` → `runPass(ctx, "catchup")`.
      Verify: `go test ./internal/scheduler/...`.
      Requirement: spec R2.3; design §3.3 (D3), §5.1, §5.3.
      **Staged deliberately without the gate check and without routing through `runPass` yet**
      (disclosed deviation from this task's own combined prose), to keep `4.3` and `4.7` genuinely
      red per strict TDD's own minimum-code law — `4.1`'s test alone does not exercise either. Both
      landed later, in `4.4`'s and `4.8`'s own GREEN commits, matching this task's final shape
      exactly by the time `4.16`'s verify runs. `Start` also wired to spawn the catch-up goroutine
      into the same `sync.WaitGroup` as the cron goroutine in this same commit, closing the gap PR
      3a's own `Start` comment disclosed — not a separately numbered task, but literally "at
      `Start`" per this task's own text, verified by a new `TestScheduler_Start_JoinsBootCatchUp`.
- [x] **4.3** Commit 1 (RED): `catchup_test.go` (extend) — `TestCatchUp_GatedOff_ZeroCallsEven
      OnStaleVault`: `ConsolidationEnabled = &false` plus a 30-day-stale `lastRunAt` asserts zero
      `Consolidate` calls.
      **Red**: genuinely red if the gate check does not precede `CatchUpDue` (owner ruling round 1
      #1 — the whole point of this task).
      Requirement: spec R2.4 ("whether `consolidation_enabled = false` also suppresses the boot
      catch-up" — resolved by owner ruling, no longer open); design §3.3 (D3).
      **Observed failing for the stated reason**: `4.2`'s own committed `runCatchUp` checked only
      `CatchUpDue`, so the disabled+stale fixture reached the delay and blocked forever instead of
      returning as a true no-op — `runCatchUp did not return`.
- [x] **4.4** Verify/confirm: `4.2`'s gate check already precedes the `CatchUpDue` evaluation per
      the code order specified — no new implementation if `4.3` passes; this task is the checkpoint.
      Verify: `go test ./internal/scheduler/... -run TestCatchUp_GatedOff`.
      Requirement: spec R2.4.
      **New implementation was needed** (per `4.2`'s own disclosed staging above):
      `consolidation.ResolveConsolidationEnabled` added ahead of `CatchUpDue` in `runCatchUp`. `4.3`
      green afterward.
- [x] **4.5** Commit 1 (RED): `catchup_test.go` (extend) — `TestCatchUp_CancelledDelayNeverFires`:
      `ctx` cancelled before the 120s delay elapses asserts `Consolidate` is never called.
      **Red**: only red if the `select` omits the `ctx.Done()` arm — `4.2`'s shape already includes
      it, so this is confirmatory rather than a genuine pre-existing bug; disclosed rather than
      claimed as a fresh red.
      Requirement: spec R2.3 (second Verified-by clause).
      **Confirmed**: passed on first run, no implementation change.
- [x] **4.6** Verify/confirm: `go test ./internal/scheduler/... -run TestCatchUp_CancelledDelay`.
      Requirement: spec R2.3.
- [x] **4.7** Commit 1 (RED): `catchup_test.go` (extend) — `TestCatchUp_IndistinguishableFromCron`:
      a due catch-up call is indistinguishable, on the mock `Consolidator`, from a cron-triggered
      call (both `Phase == nil`, both routed through `runPass`).
      **Red**: fails if `catchup.go` constructs its own `ConsolidateRequest` instead of relying on
      `runPass`'s own single construction point.
      Requirement: spec R2.4 (first Verified-by clause).
      **Observed failing for the stated reason**: `runCatchUp` still called
      `s.consolidate.Consolidate` directly, bypassing `runPass`'s slot — `max concurrent Consolidate
      calls = 2, want exactly 1`. Extended `blockingConsolidator` (`scheduler_test.go`) with a
      `lastReq`/`lastRequest()` so the `Phase` assertion could inspect the winning call.
- [x] **4.8** Verify/confirm: both triggers call only `runPass(ctx, trigger)`, `ConsolidateRequest{}`
      constructed in exactly one place (`runPass` itself) — R1.1's "unrepresentable from this
      package" property.
      Verify: `go test ./internal/scheduler/... -run TestCatchUp_Indistinguishable`.
      Requirement: spec R1.1, R2.4.
      **Done**: `runCatchUp`'s due fire now calls `s.runPass(ctx, "catchup")`; `4.7` green.
- [x] **4.9** Commit 1 (RED, structural): `test/conformance/scheduler_boundary_scan_test.go`
      (extend) — leg 3: no non-test file under `internal/scheduler` references `time_based`,
      `expired`, or `cancelled` as a status literal (R2.4's own `MUST NOT` — "no part of ADR-0009
      beyond 'always recovered'").
      **Red**: not genuinely red — nothing in this chain has ever referenced those literals;
      disclosed as a guard against future regression, not a proven-broken state, per `m2a` C9.
      Requirement: spec R2.4 (Verified by, second clause).
- [x] **4.10** Verify/confirm: `go test ./test/conformance/... -run TestSchedulerBoundaryScan`.
      Requirement: spec R2.4.
- [x] **4.11** Discharge PR 2's forward reference (task 2.5): `scheduler_boundary_scan_test.go`
      (extend) — leg 2: every non-test, non-`doc.go` file under `internal/scheduler` references all
      three of `CatchUpDue`, `ResolveConsolidationEnabled`, `NextDailyRun` at least once.
      **Genuinely red before this task's own `catchup.go` lands** — `CatchUpDue` had zero callers
      until `4.2`; first true, live check in the chain.
      Requirement: design §3.1 item 4.
      **Read collectively, not per file** (disclosed deviation): task 2.5's own "every... file...
      references all three" phrasing, read most literally, is unsatisfiable — `timer.go` is a bare
      `time.After` seam with no legitimate reason to import `internal/core/consolidation` at all.
      This leg checks that all three symbols are referenced somewhere across the non-test,
      non-`doc.go` files, matching design §3.1 item 4's own package-level prose.
      **Genuineness proven by a disclosed temporary probe** (same technique task 3a.11 used):
      `catchup.go` moved out of the package, the leg re-run, observed failing exactly on the missing
      `consolidation.CatchUpDue` reference, then restored byte-identical (`git diff --exit-code`
      clean) and re-run green — since `catchup.go` was already committed by this task's own position
      in the sequence, a plain write-then-run would not have observed the genuine failure otherwise.
- [x] **4.12** `docs/02-cognitive-core.md` §6 — one new sentence, design §3.3/owner ruling round 1
      #1: `config.consolidation_enabled = 0` suppresses the nightly pass **and** ADR-0009's boot
      catch-up — the two are one body of work behind two triggers. Corrects design §13's own PR-1
      scope line per this document's opening note; lands here, not in PR 1.
      Requirement: spec R0.3 (non-negotiable #1 — doc 02 governs behavior); design §3.3.
- [x] **4.13** `internal/scheduler/catchup.go` — a doc comment at the gate's call site naming
      ADR-0009 by section and carrying the "recovered vs. expired, not vs. a user switch" reading in
      two sentences (design §3.3 item 2).
      Requirement: design §3.3.
      **Landed in `4.4`'s own commit** (same gate-check line), ticked here for its own record.
- [x] **4.14** `docs/02-cognitive-core.md` §13 — amend the existing `boot_consolidation_delay` row
      (line ~914-917) to name `internal/scheduler.BootConsolidationDelay` in prose (manual review
      only, no gate — design §3.2's asymmetry table); amend the "03:00 daily" cadence row to name
      `internal/scheduler.ConsolidationHour` in prose, with a note stating explicitly why this does
      **not** make the row calibration-gate-checkable: the Default cell's leading text is `03:00`,
      which the gate's anchored numeric-leading regex reads as `03`, not the constant's value `3`;
      splitting the row is M3's job (the row's other half, "every 5 min", is M3's proactive check).
      Requirement: spec R0.3; design §3.2.
- [x] **4.15** Explicit non-task, recorded rather than silently dropped: no automated check exists
      for `TriggerStalenessHours`/`TimerStalenessHours` naming rows because **neither constant is
      defined in `m2d` at all** — round-2 owner ruling Q3 deferred both to M3, overriding design §12
      Q3's own default answer ("owner ruling 2 says define them now") and design §13's PR-5 scope
      line, which still lists them. This document does not carry that line into PR 5 below.
      Requirement: owner ruling round 2, Q3.
      **Nothing implemented** — recorded as an explicit non-task, per its own text.
- [x] **4.16** `golangci-lint run`; `go test -race ./internal/scheduler/...`.
      **Done** — `make lint` → `0 issues.`; `go test -race ./internal/scheduler/... -v` → all 12
      top-level tests `PASS` (`TestCatchUp_FiresAfterDelay`, `TestCatchUp_GatedOff_
      ZeroCallsEvenOnStaleVault`, `TestCatchUp_CancelledDelayNeverFires`,
      `TestCatchUp_IndistinguishableFromCron`, `TestCron_FiresAfterHourElapses`,
      `TestCron_NeverFiresBeforeCtxCancelled`, `TestCron_GatedOff_FiredTickCallsConsolidateZeroTimes`,
      `TestCron_ConfigLoadError_FiredTickCallsConsolidateZeroTimes`, `TestNew_RejectsNilDeps` (3
      subtests), `TestNew_AcceptsValidDeps`, `TestScheduler_NoOverlap_ExactlyOneInFlight`,
      `TestScheduler_SkippedFire_LogsOneLine`, `TestScheduler_Wait` (2 subtests),
      `TestScheduler_Start_JoinsBootCatchUp`), no race.
- [x] Verify (PR-level): `make check-all`; diff scope — `internal/scheduler/{catchup,scheduler}.go`
      (+ tests), `docs/02-cognitive-core.md` (§6 sentence + two §13 prose amendments). Target ≤150
      impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/scheduler-boot-catchup` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR5> --json baseRefName` names `main`.

      **PR 4 result** (`feat/scheduler-boot-catchup`): `make check-all` green end to end;
      `internal/core` coverage floor unchanged at 750/750 (100%) — this PR adds no `internal/core`
      code. `sh scripts/docs-sync.sh '[]' < changed-files`: `OK: this PR does not touch
      internal/core/** — nothing for this gate to check` (this PR touches no `internal/core/` file;
      the doc02 edits are done anyway, per non-negotiable #1, same as this document's own opening
      note). `TestHarness_CalibrationTableMatchesConstants` unaffected — the two amended §13 rows
      are `internal/scheduler` constants, outside that gate's reach by construction (spec R0.3).
      Diff scope matched exactly: `internal/scheduler/catchup.go` (new, 49 lines),
      `internal/scheduler/scheduler.go` (extended, +20/-7 = 27 changed lines), `docs/02-
      cognitive-core.md` (+5/-2 = 7 changed lines), plus `internal/scheduler/catchup_test.go` (new,
      227 lines), `internal/scheduler/scheduler_test.go` (extended, +40/-1 = 41 lines),
      `test/conformance/scheduler_boundary_scan_test.go` (extended, +134/-22 = 156 lines) — no
      strays. Impl+docs 83 lines (~55% of the ~150 estimate, comfortably under). Test 424 lines
      (~193% of the ~220 estimate) — **flagged, not split**: the overrun is entirely in test lines
      (CLAUDE.md's 400-line ceiling counts impl+docs only), but this is a notably larger overrun
      percentage than links 1/2/3a/3b saw (106–129%); most of it is `scheduler_boundary_scan_test.go`
      growing from 84 to 218 lines (legs 2/3 plus the doc-comment expansion) and `catchup_test.go`'s
      own 227 lines covering four distinct fixtures. Reported for the orchestrator's own read, not
      split unilaterally.
      `go test -race ./internal/scheduler/... -count=5`: green, all 5 runs, no race.
      `go test -race -shuffle=on ./internal/scheduler/... -count=5`: green, all 5 shuffled runs, no
      race. `go test -race -run 'TestCatchUp_IndistinguishableFromCron|
      TestScheduler_NoOverlap_ExactlyOneInFlight' ./internal/scheduler/... -count=20`: green, no
      race — 20 runs of both concurrency-sensitive tests together, zero flakes.
      Chain-merge checks deferred to actual merge time (PR not yet merged as of this apply batch),
      same posture PR 1/2/3a/3b took.

---

## PR 5 — `feat/scheduler-abort-logging` (~110 impl+docs / ~150 test)

Depends on PR 4. **Correction to design §13's own scope line for this link**: design's table lists
`TriggerStalenessHours`/`TimerStalenessHours` here — that line answers design §12 Q3 with the
pre-round-2 default ("define now"); round-2 owner ruling Q3 deferred both constants to M3 (task
4.15 above). This PR's real scope is R1.4's abort surfacing and the `Corrupted()` log line only.

- [ ] **5.1** Commit 1 (RED): `internal/scheduler/scheduler_test.go` (extend) —
      `TestRunPass_AbortSurfacesOnProcessLog`: a fake `Consolidator` returns `ports.
      ErrUnitNotFound` (simulating `persistBoosts`'s own abort) mid-pass, triggered by a scheduler
      fire (not a direct `Consolidate` call) — `runPass` returns without panicking and the
      operational log (`bytes.Buffer`) records the failure. **Scope note**: the "`consolidation_
      last_run_at` unwritten" property is `m2c` R5.4's own already-proven guarantee
      (`RecordConsolidationRun` unreachable on any `runPhase` error) — relied upon here as a fact
      the scheduler package has no write path to re-assert, not re-tested at this layer.
      **Red**: `runPass`'s error path is unasserted before this test — fails on the missing log
      line.
      Requirement: spec R1.4; design §5.4.
- [ ] **5.2** Commit 2 (GREEN): `scheduler.go` (extend) — `runPass`, on a `Consolidate` error:
      `s.logf("scheduler: pass aborted (%s): %v", trigger, err)`; returns cleanly, no retry loop, no
      special-cased "retry" state.
      Verify: `go test ./internal/scheduler/...`.
      Requirement: spec R1.4; design §5.4.
- [ ] **5.3** Commit 1 (RED): `scheduler_test.go` (extend) —
      `TestRunPass_NextFireAttemptsFreshWholePass`: after an aborted fire, a second `runPass` call
      attempts a full pass again — the fake `Consolidator`'s recorded `ConsolidateRequest{}` is the
      same zero value both times, no carried state.
      **Red**: only red if `5.2`'s abort path accidentally threads state into the next call — a
      regression guard more than a fresh failure; disclosed as such.
      Requirement: spec R1.4 (second Verified-by clause).
- [ ] **5.4** Verify/confirm: `go test ./internal/scheduler/... -run TestRunPass_NextFireAttempts`.
      Requirement: spec R1.4.
- [ ] **5.5** Commit 1 (RED): `scheduler_test.go` (extend) —
      `TestRunPass_CompletedPassLogsCorrupted`: a fake `Consolidate` returning a
      `brain.ConsolidateReport` with a non-empty `Corrupted()` (no error) logs one line naming the
      refused unit ids on success.
      **Red**: no log call exists on the success path yet — fails on the missing line.
      Requirement: design §5.2 ("`Corrupted()` is operationally meaningful for an unattended pass").
- [ ] **5.6** Commit 2 (GREEN): `scheduler.go` (extend) — after a successful `Consolidate` call, if
      `report.Corrupted()` is non-empty, `s.logf(...)` naming the refused ids.
      Verify: `go test ./internal/scheduler/...`.
      Requirement: design §5.2.
- [ ] **5.7** Confirm `internal/brain/consolidate.go` is unchanged by this PR: `git diff --stat --
      internal/brain/consolidate.go` on this branch is empty — R1.4's own MUST: `persistBoosts` gets
      no retry loop, no partial-application logic, no new tolerance for `ports.ErrUnitNotFound`.
      Requirement: spec R1.4.
- [ ] **5.8** `golangci-lint run`; `go test -race ./internal/scheduler/...`.
- [ ] Verify (PR-level): `make check-all`; diff scope — `internal/scheduler/scheduler.go` (+ tests,
      extended only, no new file). Target ≤110 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/scheduler-abort-logging` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR6> --json baseRefName` names `main`.

---

## PR 6 — `feat/serve-scheduler-wiring` (~140 impl+docs / ~120 test)

Depends on PR 5. Ships `wireScheduler` and `serve`'s own `Start` call over the vault/lock/ctx
`serve` already holds. **Pre-drawn split, per design §13**: 6 (wiring, ~140) | 7 (the shutdown
join, ~60) — the two have different failure modes (a wiring change vs. an e2e SIGTERM fixture) and
must not hold each other hostage.

- [ ] **6.1** Commit 1 (RED): `test/e2e/serve_scheduler_test.go` (new, `e2e` build tag) —
      `TestServe_ConcurrentConsolidate_StillRefused`: a `serve` process with the scheduler wired,
      running against a vault a second `nooma consolidate` invocation targets, asserts the CLI is
      refused with `m2c` R6.1's existing clean lock error.
      **Red**: `undefined: wiring.wireScheduler` — package/binary does not compile.
      Requirement: spec R3.1; design §6.
- [ ] **6.2** Commit 2 (GREEN): `cmd/nooma/wiring.go` (extend) — `wireScheduler(ctx, db, cfg,
      lookup, log) *scheduler.Scheduler`, reusing `wireConsolidate`'s own resolution over the
      `*sqlite.Vault` `runServe` already opened under its own lock (`serve.go:71-89`) — no second
      `vaultlock.Acquire` anywhere.
      Verify: `go test -tags e2e ./test/e2e/... -run TestServe_ConcurrentConsolidate`.
      Requirement: spec R3.1; design §6.
- [ ] **6.3** Commit 1 (RED): `test/e2e/serve_scheduler_test.go` (extend) —
      `TestServe_UnconfiguredVault_HTTPStillAnswers`: `serve` started against a vault with no
      `relation_evaluation`/`embedding` task binding asserts the HTTP server starts and answers
      `/capture`/`/recall` normally, no scheduled or catch-up pass ever fires.
      **Red**: `wireScheduler` (as of `6.2`) does not yet degrade gracefully — fails `runServe`
      outright or panics on the unresolved provider.
      Requirement: spec R3.2; design §6.
- [ ] **6.4** Commit 2 (GREEN): `wiring.go` (extend) — `wireScheduler` mirrors `wireBrain`'s
      degrade-to-`nil` precedent (`wiring.go:236-238`): a vault whose `resolveConsolidateProviders`
      refuses yields a `nil` scheduler, `nil` error, one log line naming why. `Start`/`Wait` are
      no-ops on a `nil *Scheduler` (design §6's own note — `runServe` needs no branch).
      Verify: `go test -tags e2e ./test/e2e/... -run TestServe_UnconfiguredVault`.
      Requirement: spec R3.2; design §6.
- [ ] **6.5** Commit 2 (GREEN, same commit as 6.4 or its own): `cmd/nooma/serve.go` (extend) — call
      `scheduler.Start(ctx)` after `wireBrain`, using the same signal-aware `ctx`
      `signal.NotifyContext` already produces (`serve.go:114`) — not a new `context.Background()`.
      Verify: `go test -tags e2e ./test/e2e/...`.
      Requirement: spec R3.3 (first clause); design §6.
- [ ] **6.6** *(split checkpoint)*: measure `git diff --stat` for tasks 6.1–6.5 in isolation against
      the ~140/120 sub-estimate. The 6/7 split is already pre-drawn (design §13); this checkpoint
      confirms PR 6 alone stays inside its own share, flagging rather than splitting reflexively if
      at risk.
- [ ] **6.7** `golangci-lint run`; confirm `Start`/`Wait` remain no-ops on a `nil *Scheduler`
      (covered by `6.4`'s own fixture).
- [ ] Verify (PR-level): `make check-all` incl. the `e2e`-tagged suite; diff scope — `cmd/nooma/
      {wiring,serve}.go`, `test/e2e/serve_scheduler_test.go` (new). Target ≤140 impl+docs lines.
      **Disclosed, temporary gap inside this stacked chain**: this PR does **not** add
      `sched.Wait(shutdownCtx)` (D5's own correction — PR 7's scope). Between this PR's merge and
      PR 7's, `main` closes the vault's `*sql.DB` in a `defer` without joining a live pass goroutine
      first — a real, if narrow, correctness gap, disclosed here rather than presented as already
      closed, the same honesty `m2c` PR 7a/7b's own split carried for its missing decision-log half.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/serve-scheduler-wiring` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR7> --json baseRefName` names `main`.

---

## PR 7 — `feat/serve-shutdown-cancel` (pre-drawn split of 6) (~60 impl+docs / ~200 test)

Depends on PR 6. Closes the gap PR 6 disclosed: `sched.Wait(shutdownCtx)` sharing the existing
`shutdownGrace` budget (D5's correction to spec R3.3, verified against the code, not merely the
spec's literal reading).

- [ ] **7.1** Commit 1 (RED): `test/e2e/serve_shutdown_test.go` (new, `e2e` tag) —
      `TestServe_SIGTERM_PassInFlight_ExitsWithinGrace`: `serve` started with a slow fake
      consolidation pass in flight (synchronized via a channel the fake signals on `ctx.Done()`, not
      relied on to race a real SQL close deterministically — design §3.5's own "cancellation is not
      instantaneous" caution), sent `SIGTERM`, asserts the process exits within `shutdownGrace` and
      the fake observed `ctx` cancellation before exit.
      **Red**: without `7.2`'s join, `runServe` exits without ever cancelling/joining the pass in a
      way this fixture can observe — fails on the missing cancellation signal.
      Requirement: spec R3.3; design §3.5 (D5).
- [ ] **7.2** Commit 2 (GREEN): `cmd/nooma/serve.go` (extend) — `shutdownCtx, cancel :=
      context.WithTimeout(context.Background(), shutdownGrace); defer cancel();
      server.Shutdown(shutdownCtx); sched.Wait(shutdownCtx)` — same budget, no second window (D5's
      exact shape, design §3.5).
      Verify: `go test -tags e2e ./test/e2e/... -run TestServe_SIGTERM_PassInFlight`.
      Requirement: spec R3.3; design §3.5.
- [ ] **7.3** Commit 1 (RED): `serve_shutdown_test.go` (extend) —
      `TestServe_SIGTERM_ConsolidationLastRunAtUnchanged`: `consolidation_last_run_at` is unchanged
      from before the pass started, after the `SIGTERM` above.
      **Red**: only meaningfully red if `7.2`'s join somehow let the pass complete far enough to
      write the column — relies on `m2c` R5.4's completion-gated write as a fact, not re-proven here.
      Requirement: spec R3.3 (second Verified-by clause).
- [ ] **7.4** Commit 1 (RED): `serve_shutdown_test.go` (extend) —
      `TestServe_SIGTERM_FollowUpRunCompletesFreshPass`: a follow-up `serve`/`consolidate` run
      against the same vault after the cancelled shutdown asserts a fresh whole pass runs to
      completion with nothing skipped because of the earlier cancellation.
      **Red**: fails only if the cancelled pass left partial state — again relies on, does not
      re-derive, `m2c` R5.4's completion-gated write.
      Requirement: spec R3.3 (third Verified-by clause).
- [ ] **7.5** Verify/confirm: no code change needed beyond `7.2` for `7.3`/`7.4` — verification
      tasks, the property holds by construction.
      Verify: `go test -tags e2e ./test/e2e/...`.
      Requirement: spec R3.3.
- [ ] **7.6** `golangci-lint run`; `go test -race ./cmd/nooma/...`.
- [ ] Verify (PR-level): `make check-all` incl. the `e2e` suite; diff scope — `cmd/nooma/serve.go`
      (+ `test/e2e/serve_shutdown_test.go`, new). Target ≤60 impl+docs lines. Threat matrix
      discharge: design §10's "Process lifecycle" row — planned RED tests are `7.1` here and `4.5`
      (cancelled-delay, PR 4).
      **Chain-merge check 1**: `git ls-remote --heads origin feat/serve-shutdown-cancel` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR8> --json baseRefName` names `main`.

---

## PR 8 — `feat/demo-golden-format` (~170 impl+docs / ~160 test)

Depends on PR 7 (no code dependency — sequenced last on the stack per design §13's own link order).
Ships the new golden-set format and registration, before any real corpus content — `testdata/
recall/format.md`'s own shape is the bar (design §8.2), and design names this link as one of the
two most likely to blow its budget (prose is not code, and `format.md` is prose).

- [ ] **8.1** Commit 1 (RED): `test/conformance/golden_sets_test.go` (extend) — require
      `"consolidation"` registered in `goldenSetDirs`, `formatToType`, and
      `casesDirMustBeEmpty`. **Red**: `assertCasesDirEmptiness`/`TestHarness_GoldenSetFormatMatches
      Type` `t.Fatalf` on the unregistered directory (or the directory itself is missing) — package
      compiles but the test fails loudly, per the existing gate's own half-registration guard.
      Requirement: spec R4.1; design §8.2.
- [ ] **8.2** `testdata/consolidation/format.md` (new) — written before or in the same commit as the
      first case file: field table (case name; capture script — offset from `t0`, text, the scripted
      fake-provider answer; injected `now`; optional `last_run_at`; expected effects — `archived`,
      `relations_created`, `beliefs`), one fenced ` ```json``` ` worked example, a "what the loader
      does and does not check" section — `testdata/recall/format.md`'s shape, not a template
      improvised past.
      Requirement: spec R4.1; design §8.2.
- [ ] **8.3** `test/support/goldenset/types.go` (extend) — `ConsolidationExample` struct decoding
      `format.md`'s fenced example via `goldenset.DecodeStrict` — makes `format.md` executable
      rather than aspirational.
      Requirement: design §8.2.
- [ ] **8.4** Commit 2 (GREEN): `golden_sets_test.go` (extend) — register `"consolidation"` in all
      three maps: `goldenSetDirs`, `formatToType`'s constructor, `casesDirMustBeEmpty["consolidation"]
      = false`.
      Verify: `go test ./test/conformance/... -run TestHarness_GoldenSetFormatMatchesType`.
      Requirement: spec R4.1; design §8.2.
- [ ] **8.5** `testdata/consolidation/format_example.json` (new) — sibling of `cases/`, never inside
      it. Verify: `assertFormatExampleIsSiblingOfCases` passes.
      Requirement: design §8.2.
- [ ] **8.6** Commit 1 (RED): a loader test proving `format.md`'s documented shape round-trips — a
      case file decoded via `goldenset.DecodeStrict`, asserting each field lands where `format.md`
      says.
      **Red**: no case file exists yet under `testdata/consolidation/cases/` — the loader has
      nothing to decode, fails on a missing/empty directory.
      Requirement: spec R4.1 (Verified by, first clause).
- [ ] **8.7** Commit 2 (GREEN): `testdata/consolidation/cases/*.json` (new) — at least one real case
      satisfying `8.6`.
      Verify: `go test ./test/conformance/... ./test/support/...`.
      Requirement: spec R4.1.
- [ ] **8.8** Verify: `golden_sets_test.go`'s widened guard fails if the directory is ever empty —
      confirmed via a throwaway local probe (temporarily empty `cases/`, observe the `Fatalf`,
      restore), the same disclosed-probe convention `m2c` task 9.7 used for a reversion check; the
      tree is never left broken by this verification.
      Requirement: spec R4.1 (Verified by, second clause).
- [ ] **8.9** `golangci-lint run`.
- [ ] **8.10** *(unplanned but flagged split checkpoint — design §13 names PR 8 among the two links
      most likely to blow their budget)*: measure `git diff --stat` in isolation. If `format.md`'s
      own prose risks the ~170 sub-estimate alone, split the prose (`format.md` + example) from the
      loader/registration wiring — an undrawn split, reported honestly rather than forced, the same
      discipline `m2c`'s own PR 5/6/9/11 checkpoints used.
- [ ] Verify (PR-level): `make check-all`; diff scope — `testdata/consolidation/**`, `test/support/
      goldenset/types.go`, `test/conformance/golden_sets_test.go`. Target ≤170 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/demo-golden-format` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR9a> --json baseRefName` names `main`.

---

## PR 9a — `feat/demo-simulated-weeks` (~130 impl+docs / ~280 test)

Depends on PR 8. Ships D6's corrected mechanism (overturning spec R4.2's own literal reading, per
design §8.1): the corpus is built by driving the real capture path under a stepping fake clock, not
hand-authored with past timestamps — hand-written rows never populate the vector index or FTS
`connect`'s `RecallService.ScoredFor` needs. **Pre-drawn split, per design §13**: 9a (the corpus and
one green pass, ~130) | 9b (the `decision_log` assertions, ~40). Design names this pair, and Q5
specifically, "the single highest-variance task in the chain" — the round-2 owner ruling on Q5
already declined the one-corpus-per-phase alternative, so an overrun here is reported, not resolved
by silently narrowing the demo's scope.

- [ ] **9a.1** Commit 1 (RED): `test/e2e/consolidation_demo_test.go` (new, `e2e` tag) —
      `TestDemo_SimulatedWeeks_PassCompletes`: construct the corpus by driving
      `brain.CaptureService.Capture` per scripted entry (offset from `t0` advances a stepping fake
      clock) through `test/support/fakeprovider` for every judge/embedding call (R4.3), against
      `internal/store/sqlite` repos constructed directly (`test/e2e/**` is **not** in `sqlite-
      containment`'s exception list — no `database/sql`, no raw SQL, per design §8.1's own note),
      then run one consolidation pass at a single injected `now`. Asserts the pass completes without
      error.
      **Red**: `undefined: goldenset.ConsolidationExample` / the corpus loader — package does not
      compile.
      Requirement: spec R4.2 (as corrected by design D6), R4.3; design §8.1.
- [ ] **9a.2** Commit 2 (GREEN): build the corpus loader reading `testdata/consolidation/cases/
      *.json` via `test/support/goldenset`, drive `CaptureService` per case, wire `fakeprovider.New`/
      `fakeprovider.NewEmbeddingFake` for every call.
      Verify: `go test -tags e2e ./test/e2e/... -run TestDemo_SimulatedWeeks_PassCompletes`.
      Requirement: spec R4.2, R4.3; design §8.1.
- [ ] **9a.3** Commit 1 (RED): `consolidation_demo_test.go` (extend) — `TestDemo_ArchiveFires`: over
      the chosen `now`, at least one unit's `effective_weight` has fallen under `weight_threshold`
      (archive fires) — asserted via `DecisionLog.Since` containing ≥1 `ActionArchiveArchived` row
      (R4.4's archive clause only; the full three-effect assertion with named rationales is PR 9b's
      own scope).
      **Red**: no corpus timestamps chosen yet to cross the threshold — zero rows.
      Requirement: spec R4.4 (archive clause).
- [ ] **9a.4** Commit 2 (GREEN, tuning): adjust the corpus's `weight`/`last_touched_at` fixture
      values until `9a.3` passes.
      Verify: `go test -tags e2e ./test/e2e/... -run TestDemo_ArchiveFires`.
      Requirement: spec R4.4.
- [ ] **9a.5** Commit 1 (RED): `consolidation_demo_test.go` (extend) —
      `TestDemo_ConnectCandidatePairExists`: at least one candidate pair is close enough by
      `connect`'s fused ranking to reach the judge, proven via a `fakeprovider` script expecting
      exactly one `Complete` call for the pair — a broken candidate search either calls zero times or
      calls for the wrong pair, both fail the scripted guard.
      **Red**: no candidate pair reaches the judge yet — the scripted call is never made,
      `fakeprovider`'s own never-called guard fails the test.
      Requirement: spec R4.4 (connect clause).
- [ ] **9a.6** Commit 2 (GREEN, tuning): script the fake embedding responses so `RecallService.
      ScoredFor`'s fusion surfaces the intended pair; seed `config.consolidation_last_run_at` via
      `ConfigRepo.RecordConsolidationRun` to a value before the corpus's own most-recent timestamps
      (R4.4's `MAY`, so `strengthen`'s `since` is non-`nil`).
      Verify: `go test -tags e2e ./test/e2e/... -run TestDemo_ConnectCandidatePairExists`.
      Requirement: spec R4.4.
- [ ] **9a.7** Commit 1 (RED): `consolidation_demo_test.go` (extend) —
      `TestDemo_DeriveBeliefExists`: at least one derivable belief exists in the corpus, proven by
      scripting the dedup-judge fake to accept exactly one derive candidate.
      **Red**: no derive candidate accepted yet — zero beliefs.
      Requirement: spec R4.4 (derive clause).
- [ ] **9a.8** Commit 2 (GREEN, tuning): adjust the corpus/fake script until `9a.7` passes.
      Verify: `go test -tags e2e ./test/e2e/... -run TestDemo_DeriveBeliefExists`.
      Requirement: spec R4.4.
- [ ] **9a.9** *(split checkpoint, pre-drawn per design §13)*: measure `git diff --stat` for tasks
      9a.1–9a.8 in isolation against the ~130/280 sub-estimate. If the fake-provider tuning proves
      unbounded, report it rather than resolve it — round-2 owner ruling Q5 already declined the
      per-phase-corpus alternative design §12 raised.
- [ ] **9a.10** `golangci-lint run`.
- [ ] Verify (PR-level): `make check-all` incl. the `e2e` suite; diff scope — `test/e2e/
      consolidation_demo_test.go` (new), `testdata/consolidation/cases/` (corpus content growth
      only — the format/schema itself is PR 8's own scope, unchanged here). Target ≤130 impl+docs
      lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/demo-simulated-weeks` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR9b> --json baseRefName` names `main`.

---

## PR 9b — `feat/demo-decision-log-assertions` (pre-drawn split of 9a) (~40 impl+docs / ~180 test)

Depends on PR 9a. Ships R4.5's own bar — `decision_log` alone tells the story — and, per R4.6,
**this is `m2d`'s own exit criterion**: no later PR owes anything toward it.

- [ ] **9b.1** Commit 1 (RED): `test/e2e/consolidation_demo_test.go` (extend) —
      `TestDemo_DecisionLogTellsTheStory`: extends `9a`'s pass fixture to assert, via `DecisionLog.
      Since` only (never re-deriving from `units`/`relations`/`self_beliefs` directly), at least one
      legible row for each of `ActionArchiveArchived`, `ActionConnectRelationPersisted`, and
      (`ActionDeriveBeliefCreated` or `ActionDeriveBeliefReinforced`) — each `Rationale` string
      present and naming the specific unit/relation/belief the fixture expects, not merely that a
      row of the right `Action` exists somewhere.
      **Red**: `9a`'s own fixtures only assert row *existence*, not rationale content naming the
      specific item — fails on the strengthened assertion.
      Requirement: spec R4.5.
- [ ] **9b.2** Commit 2 (GREEN, mostly tuning, no production code expected): if `9a`'s three phases
      already produce the right effects, no `internal/brain`/`internal/core` change is needed — only
      the corpus's content (distinctive unit text) is tuned until each `Rationale` string uniquely
      identifies the expected unit/relation/belief.
      Verify: `go test -tags e2e ./test/e2e/... -run TestDemo_DecisionLogTellsTheStory`.
      Requirement: spec R4.5.
- [ ] **9b.3** R4.6 discharge, verification not a new test: confirm this is the PR where `docs/
      05-build-plan.md` §M2's demo bullet and the umbrella proposal's own final success-criteria
      bullet (§2) may be checked off — no later PR in this chain, and none in the wider M2 chain,
      owes anything further toward either.
      Requirement: spec R4.6.
- [ ] **9b.4** `golangci-lint run`.
- [ ] Verify (PR-level): `make check-all` incl. the `e2e` suite; diff scope — `test/e2e/
      consolidation_demo_test.go` (extended only, no new file). Target ≤40 impl+docs lines. **This
      is `m2d`'s own exit criterion (R4.6)** — after this PR merges, `docs/05-build-plan.md`'s M2
      demo bullet and the umbrella proposal's final success criterion get checked off in the same PR
      or immediately after.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/demo-decision-log-assertions`
      returns nothing after merge.
      **Chain-merge check 2**: this is the chain's last link — no next PR to check a base against;
      confirm `main`'s own `git log --oneline -11` (wider if any undrawn split fired) shows every
      link merged in order instead.

---

## X — Chain-wide verifications (only make sense once every link is in)

- [ ] **X.1** Confirm `docs/06-harness.md` needed no change across all eleven links — `m2d` adds no
      new invariant number (no `I`-prefixed row), unlike `m2c`. `git diff main~11..main --
      docs/06-harness.md` (or the equivalent range once the two undrawn checkpoints, 8.10/9a.9, are
      accounted for) is empty.
      Requirement: design §1 (no new invariant claimed anywhere in spec/design).
- [ ] **X.2** Confirm `internal/core` gained exactly the one file's worth of new code the chain
      claims — `internal/core/consolidation/schedule.go` plus its own test — and no other
      `internal/core` file is touched anywhere in the chain. `git diff <base>..<tip> --
      internal/core/` names only those two files.
      Requirement: design §4.
- [ ] **X.3** Confirm `docs/02-cognitive-core.md` §13 grew by exactly one row (`catch_up_staleness_
      hours`, PR 1) and exactly two existing rows were amended in place, not replaced
      (`boot_consolidation_delay` → names `BootConsolidationDelay`; the "03:00 daily" cadence row →
      names `ConsolidationHour`, both PR 4). `calibration_doc_test.go`'s symbol floor moves by
      exactly one new `internal/core` symbol; nothing scheduler-side is gated.
      Requirement: spec R0.3; design §3.2.
- [ ] **X.4** Confirm `docs/02-cognitive-core.md` §6 gained exactly the one `consolidation_enabled`
      sentence, landing in PR 4's own commit range — not PR 1's, correcting design §13's own
      mis-scoped table entry per this document's opening note.
      Requirement: spec R0.3; design §3.3.
- [ ] **X.5** Confirm `TriggerStalenessHours`/`TimerStalenessHours` were never defined anywhere in
      the chain: `rg 'TriggerStalenessHours|TimerStalenessHours' internal/` returns nothing.
      Requirement: owner ruling round 2, Q3.
- [ ] **X.6** Confirm `.golangci.yml`'s `scheduler-boundary` rule is exercised for real by the time
      the chain closes, not merely vacuously as it was at PR 2: `golangci-lint run` passes with the
      rule active against the finished `internal/scheduler` package, which by PR 9b's own merge
      legitimately imports `internal/brain`, `internal/ports`, and `internal/core/consolidation`.
      Requirement: spec R0.1; design §7.
- [ ] **X.7** Confirm the boundary scan's leg 2 (all three core symbols referenced) is genuinely
      live by the chain's end: `CatchUpDue`, `ResolveConsolidationEnabled`, and `NextDailyRun` each
      have ≥1 non-test reference under `internal/scheduler`.
      Requirement: design §3.1 item 4.
- [ ] **X.8** `make check-all` green end to end on the tip of the chain: L1–L4, `internal/core`
      coverage floor, the seven-target cross-compile matrix (ADR-0013). The schema-golden
      regen-diff check is **N/A** for this whole chain — `m2d` adds no migration (design §11).
      Requirement: `docs/06-harness.md` §9 (the full local gate).
- [ ] **X.9** Confirm the umbrella proposal's own M2 success criteria and `docs/05-build-plan.md`'s
      M2 demo bullet are both checked off, dated, and reference PR 9b's own merge — R4.6's discharge,
      restated as the chain's own closing fact rather than assumed.
      Requirement: spec R4.6.

---

## Handoffs `m2d` leaves open (design §14, §12 — carried forward so the archive does not lose them)

Not tasks in this change — recorded here per this project's own convention (`m2c` tasks.md's own
"Handoffs left open" section), so M3's own change inherits them rather than rediscovering them.

- **`TriggerStalenessHours`/`TimerStalenessHours`** — undefined by this chain (round-2 owner ruling
  Q3). M3's own PR, the first to read either, defines and names it in §13 at that point.
- **M3's own half of `internal/scheduler`** — `time_based` trigger staleness, ephemeral-timer
  expiry, `status='expired'`/`'cancelled'` transitions, quiet-hours deferral, digest and push
  delivery — all untouched (design §14). `internal/channels` is deliberately **not** denied by the
  `scheduler-boundary` rule (design §7), precisely because M3's trigger delivery is a channel
  consumer by ADR-0009's own design.
- **§13's shared "03:00 daily / every 5 min" row** stays one cell for two knobs (design §3.2) —
  splitting it so the proactive-check cadence gets its own gate-checkable constant is M3's job.
- **Q1 (a skipped fire's visibility)** and **Q4 (the resident vector index loaded twice at `serve`
  startup, once by `wireBrain` and once by `wireScheduler`'s own `wireConsolidate` call)** — both
  accepted as-is by the design (§12 Q1, round-2 ruling Q1; §12 Q4), recorded here so a later reader
  does not mistake either for an oversight.
- **`persistBoosts`'s abort semantics** — confirmed unchanged by `m2d` (R1.4). Any future retry or
  partial-application redesign is a separate change to `internal/brain`, not owed here.
- **`m2c`'s own three named-and-deferred debts** (two `MergeDecision`s sharing one `MergeInto` not
  compounding untested; the confidence `[0,1]`/NaN bound expressed twice; `judgeAndPersistPair`'s
  `j.Type == nil` guard with no regression test) — **not `m2d`'s**, restated here only so they are
  not silently re-derived as new by this document's own review; `m2d` does not touch
  `internal/brain/consolidate.go` except via PR 5.7's own confirmed-unchanged check.

---

## Traceability

| Spec section | Requirements | Tasks |
|---|---|---|
| R0 — cross-cutting (dependency rule, clock scope, calibration scope) | R0.1, R0.2, R0.3 | 2.1–2.2 (R0.1); (R0.2, no new task — existing guards, restated in this doc's opening); 1.8, 1.10, 4.12, 4.14, X.3–X.4 (R0.3) |
| §1 `internal/scheduler` cron (PR 3a, 3b) | R1.1–R1.4 | 3a.1–3a.14, 3b.1–3b.6, 5.1–5.9 (R1.4) |
| §2 ADR-0009 boot catch-up (PR 1, 4) | R2.1–R2.4 | 1.1–1.12, 4.1–4.17 |
| §3 `nooma serve` wiring and shutdown (PR 6, 7) | R3.1–R3.3 | 6.1–6.8, 7.1–7.6 |
| §4 The demo and golden set (PR 8, 9a, 9b) | R4.1–R4.6 | 8.1–8.11, 9a.1–9a.11, 9b.1–9b.5 |
| §5 Handoffs discharged/deferred | (not spec requirements) | Inherited-handoffs section; Handoffs-left-open section |
| §6 What this spec does not require | (not tasked — M3/M4/M5) | Handoffs-left-open section |
| §7 Open questions Q1–Q3 | (resolved by owner ruling before this document) | Owner-ruling recap (this document's opening); 3b.4 (Q1); 3a.14 (Q2, accepted no-op); 5.1–5.9 (Q3, resolved "process log only") |
| Chain-merge discipline (`nooma-pr`, not a spec requirement) | — | Every PR's two chain-merge-check items |

---

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1,260 implementation + docs, ~2,000 test (design's own guess, §13 — summed from the eleven-row forecast table below) |
| 400-line budget risk | **Low across all eleven links.** Tallest is PR 3a at ~200 impl+docs (0.50× the ceiling); every other link sits at 0.40× or below |
| Chained PRs recommended | Yes — 9 links, 3 pre-drawn splits, up to 12 real PRs (`design.md` §13) |
| Delivery strategy | Chained PRs, per owner rulings taken 2026-08-11 |
| Chain strategy | `stacked-to-main`, per owner rulings taken 2026-08-11 |
| Decision needed before apply | No — chain strategy and every open design question were ruled on before this document was written |

**Per-link estimate (implementation + docs / test lines), transcribed from `design.md` §13**, with
one correction: PR 5's design-table scope line included `TriggerStalenessHours`/
`TimerStalenessHours`, deferred to M3 by round-2 owner ruling Q3 (task 4.15) — its estimate is kept
as the design's own ceiling target rather than re-guessed downward, since the two dropped constants
were a small fraction of the line count and a fresh guess would carry no more confidence than the
one already made:

| # | Branch | Impl + docs | Tests (est.) | vs. 400 ceiling |
|---|---|---|---|---|
| 1 | `feat/scheduler-core-decisions` | ~130 | ~200 | 0.33× |
| 2 | `feat/scheduler-boundary-lint` | ~70 | ~90 | 0.18× |
| 3a | `feat/scheduler-cron` | ~200 | ~260 | **0.50×** (tallest) |
| 3b | `feat/scheduler-overlap-guard` | ~60 | ~140 | 0.15× |
| 4 | `feat/scheduler-boot-catchup` | ~150 | ~220 | 0.38× |
| 5 | `feat/scheduler-abort-logging` | ~110 | ~150 | 0.28× |
| 6 | `feat/serve-scheduler-wiring` | ~140 | ~120 | 0.35× |
| 7 | `feat/serve-shutdown-cancel` | ~60 | ~200 | 0.15× |
| 8 | `feat/demo-golden-format` | ~170 | ~160 | 0.43× |
| 9a | `feat/demo-simulated-weeks` | ~130 | ~280 | 0.33× |
| 9b | `feat/demo-decision-log-assertions` | ~40 | ~180 | 0.10× |
| **Total** | | **~1,260** | **~2,000** | — |

**No link crosses the ceiling — confirmed, with the same caution `design.md` §13 states it.** These
are pre-code guesses, "of the same kind this project has measured wrong 1.3×–4.3× across M0/M1/M2a/
M2b/M2c." Three split boundaries are **pre-drawn**, not conditional on a judgment call at apply
time, with an explicit checkpoint task at each:

- PR 3 → 3a (skeleton + cron loop, ~200) | 3b (overlap guard, ~60), checkpoint at task 3a.12.
- PR 6/7 → 6 (wiring, ~140) | 7 (shutdown join, ~60), checkpoint at task 6.6.
- PR 9 → 9a (corpus + one green pass, ~130) | 9b (decision-log assertions, ~40), checkpoint at
  task 9a.9.

**The two links most likely to blow their budget** (design's own words): PR 3a (a new package's
whole skeleton at once) and PR 8 (`format.md` is prose, and `testdata/recall/format.md` is the
bar) — both carry their own unplanned-split checkpoint (3a.12, 8.10) in addition to any pre-drawn
one.

`m2d`'s exit criterion is link 9b: `docs/05-build-plan.md` §M2's demo bullet and the umbrella
proposal's final success criterion are checked off there and nowhere later (R4.6, task 9b.3).

