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

- [x] **Judgment Day correction round** (PR #184, frozen target `2906af1`): four confirmed findings,
      fixed and re-verified.
      - **JD-4-01** (CRITICAL): `test/conformance/scheduler_boundary_scan_test.go` leg 2 checked
        `strings.Contains(text, sym)` over each file's raw bytes, so a symbol name mentioned only in
        a comment (`catchup.go:24`'s own `CatchUpDue` mention, `cron.go:9,12`'s own `NextDailyRun`
        mentions) satisfied it even with the real call deleted. Rewritten to parse each scanned file
        with `go/parser`/`go/ast` and look for a real `*ast.CallExpr` selecting the symbol — comments
        and string literals cannot satisfy it. The collective, package-union scope (task 4.11's own
        disclosed deviation from a per-file reading) is unchanged; only the detection methodology was
        defective. **Disclosed probe**: the real `consolidation.CatchUpDue(...)` call in
        `catchup.go` was temporarily removed (its line-24 comment kept) — leg 2 FAILED, correctly, on
        the missing real call site — then restored byte-identical, `git diff --exit-code` clean, leg
        2 green again.
      - **JD-4-02** (WARNING): `runCatchUp`'s `Config.Load` error early return (`catchup.go:20-27`)
        had zero tests, unlike `runPass`'s identical pattern (`TestCron_ConfigLoadError_
        FiredTickCallsConsolidateZeroTimes`, landed for JD-3a-01). Added
        `TestCatchUp_ConfigLoadError_ZeroCallsEvenOnStaleVault` (`catchup_test.go`), reusing
        `errConfigRepo` (`cron_test.go`) rather than duplicating it. The production code was already
        correct, so this was not a chronological red. **Disclosed probe**: the `if err != nil {
        return }` branch was temporarily removed — the new test FAILED (`runCatchUp did not return`)
        — then restored byte-identical, `git diff --exit-code` clean, test green again.
      - **JD-4-03** (WARNING): `design.md` §3.3's Mechanism paragraph stated the catch-up "reads it
        once at boot... there is nothing to re-read," which predates R2.4's single-entry-point
        requirement (routing the due catch-up fire through the same `runPass` the cron uses) — the
        shipped code reads config twice (once at boot to gate the delay, once inside `runPass` when
        the delay elapses). Corrected the prose to describe the two-read behavior and explain why:
        the second read is a structural consequence of routing through `runPass`, and it is the safer
        shape (the enabled flag is re-checked immediately before the pass fires). Docs-only, no code
        change — same posture §3.4's own PR-3b correction note took.
      - **JD-4-04** (WARNING): `TestScheduler_Start_JoinsBootCatchUp` (`scheduler_test.go`) could not
        distinguish the catch-up goroutine being in `s.wg` from it not being: the fake timer was
        never fed, so after `cancel()` both goroutines returned via their own `ctx.Done()` branch
        almost immediately regardless of `wg` membership. Rewritten with a new `discriminatingTimer`
        fixture (keys a distinct channel per distinct requested duration, unlike the shared-channel
        `fakeTimer`) so the catch-up's own 120s wait can be fired independently of the cron's: the
        catch-up goroutine is routed into a real, deliberately blocked `Consolidate` call, and a
        `s.Wait` call issued with a short bounded ctx while that call is still blocked must NOT
        return — it can only return early if the catch-up goroutine is missing from `s.wg`.
        **Disclosed probe**: `Start`'s `s.wg.Add(1)` for the catch-up goroutine was temporarily
        removed — the new test FAILED (`Wait returned while the catch-up goroutine was still blocked
        inside Consolidate`) — then restored byte-identical, `git diff --exit-code` clean, test green
        again.

      **Full verification**: `make check-all` green end to end; `internal/core` coverage floor
      unchanged at 750/750 (100%). `go test -race -shuffle=on ./internal/scheduler/... -count=5`:
      green, no race. `go test ./test/conformance/... -run TestSchedulerBoundaryScan -v`: all three
      legs green.
      **Rollback boundaries**: JD-4-01 touches only `test/conformance/scheduler_boundary_scan_test.go`
      (leg 2's doc comment and body); JD-4-02 touches only `internal/scheduler/catchup_test.go` (one
      new test); JD-4-03 touches only `openspec/changes/m2d-scheduler-demo/design.md` §3.3; JD-4-04
      touches only `internal/scheduler/scheduler_test.go` (`discriminatingTimer` fixture plus the one
      rewritten test) — no production behavior changed by any of the four; `internal/scheduler/
      {catchup,scheduler}.go` are byte-identical to PR 4's own committed state once each disclosed
      probe was reverted.

---

## PR 5 — `feat/scheduler-abort-logging` (~110 impl+docs / ~150 test)

Depends on PR 4. **Correction to design §13's own scope line for this link**: design's table lists
`TriggerStalenessHours`/`TimerStalenessHours` here — that line answers design §12 Q3 with the
pre-round-2 default ("define now"); round-2 owner ruling Q3 deferred both constants to M3 (task
4.15 above). This PR's real scope is R1.4's abort surfacing and the `Corrupted()` log line only.

- [x] **5.1** Commit 1 (RED): `internal/scheduler/scheduler_test.go` (extend) —
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
      **Done** (commit `336fa7f`) — observed failing: `log = "", want "scheduler: pass aborted
      (cron): unit not found\n"`, exactly the stated reason.
- [x] **5.2** Commit 2 (GREEN): `scheduler.go` (extend) — `runPass`, on a `Consolidate` error:
      `s.logf("scheduler: pass aborted (%s): %v", trigger, err)`; returns cleanly, no retry loop, no
      special-cased "retry" state.
      Verify: `go test ./internal/scheduler/...`.
      Requirement: spec R1.4; design §5.4.
      **Done** (commit `1a247ee`) — green, verbatim log line landed.
- [x] **5.3** Commit 1 (RED): `scheduler_test.go` (extend) —
      `TestRunPass_NextFireAttemptsFreshWholePass`: after an aborted fire, a second `runPass` call
      attempts a full pass again — the fake `Consolidator`'s recorded `ConsolidateRequest{}` is the
      same zero value both times, no carried state.
      **Red**: only red if `5.2`'s abort path accidentally threads state into the next call — a
      regression guard more than a fresh failure; disclosed as such.
      Requirement: spec R1.4 (second Verified-by clause).
      **Confirmed, not a genuine red** (commit `336fa7f`, same commit as 5.1) — passed on first run:
      `runPass` constructs `brain.ConsolidateRequest{}` fresh at every call and threads no state
      across calls, so there was no implementation state in which this could have failed.
- [x] **5.4** Verify/confirm: `go test ./internal/scheduler/... -run TestRunPass_NextFireAttempts`.
      Requirement: spec R1.4.
      **Done** — `PASS`.
- [x] **5.5** Commit 1 (RED): `scheduler_test.go` (extend) —
      `TestRunPass_CompletedPassLogsCorrupted`: a fake `Consolidate` returning a
      `brain.ConsolidateReport` with a non-empty `Corrupted()` (no error) logs one line naming the
      refused unit ids on success.
      **Red**: no log call exists on the success path yet — fails on the missing line.
      Requirement: design §5.2 ("`Corrupted()` is operationally meaningful for an unattended pass").
      **Done** (commit `9e0be9b`) — observed failing: `log = "", want "scheduler: cron pass
      completed, refused 1 unit(s): [u-corrupted]\n"`, exactly the stated reason.
      **Disclosed deviation from this task's own "a fake `Consolidate`" wording**: found while
      implementing — `brain.ConsolidateReport`'s fields (`corrupted` included) are unexported, and
      package `brain` exposes no constructor or setter that reaches them. No value satisfying the
      `Consolidator` interface from outside package `brain` can therefore return a report with a
      non-empty `Corrupted()` by construction alone. This PR's own two hard constraints rule out the
      two natural fixes: task 5.7 forbids touching `internal/brain/consolidate.go`, and this PR's
      own diff-scope note ("`scheduler.go` extended only — no new file") forbids adding an
      `export_test.go`-style file anywhere, including inside package `brain`. `reflect`/`unsafe`
      field-poking was considered and rejected as contrary to this codebase's own quality bar (no
      precedent for it anywhere in the tree). The fixture instead wires a real
      `*brain.ConsolidateService` (`brain.NewConsolidateService`, all-exported constructor) over
      `test/support/memrepo` in-memory repos seeded with exactly one unit whose decay state is
      non-finite (`Weight: math.NaN()`) — `partitionLiveDecayStates` (package `brain`, unexported,
      unmodified) refuses it in every phase that reads `LiveDecayStates` (archive, connect,
      reweight), and `report.reportCorrupted`'s own dedup collapses it to one entry. The refused
      unit is never a valid connect/derive source, so both phases' own `sourceIDs` come back empty
      and short-circuit before ever calling the judge or the recall service — the rest of the pass
      is a true no-op, the identical "nothing fed, nothing written" shape
      `internal/brain`'s own `TestConsolidate_NoEffects` already proves for a fully empty fixture.
      This is the same wiring pattern `internal/brain`'s own
      `TestConsolidate_WholePassReportsEachCorruptedIDOnce` already uses to produce a non-empty
      `Corrupted()`, reused here through exported symbols only (`fakeprovider.New`/
      `fakeprovider.NewEmbeddingFake`, `brain.NewRecallService`, `brain.NewIndex`,
      `memrepo.New*`) — zero lines touch `internal/brain/consolidate.go` (task 5.7's own
      confirmation below verifies this directly) and no new file is added anywhere (the fixture and
      a small local `ports.IDGen` double, `fakeIDGen`, both live inside the already-existing,
      extended `scheduler_test.go`).
- [x] **5.6** Commit 2 (GREEN): `scheduler.go` (extend) — after a successful `Consolidate` call, if
      `report.Corrupted()` is non-empty, `s.logf(...)` naming the refused ids.
      Verify: `go test ./internal/scheduler/...`.
      Requirement: design §5.2.
      **Done** (commit `098f9c3`) — green: `s.logf("scheduler: %s pass completed, refused %d
      unit(s): %v", trigger, len(corrupted), corrupted)`.
- [x] **5.7** Confirm `internal/brain/consolidate.go` is unchanged by this PR: `git diff --stat --
      internal/brain/consolidate.go` on this branch is empty — R1.4's own MUST: `persistBoosts` gets
      no retry loop, no partial-application logic, no new tolerance for `ports.ErrUnitNotFound`.
      Requirement: spec R1.4.
      **Confirmed** — `git diff --stat --exit-code main...HEAD -- internal/brain/consolidate.go`
      exits 0 with empty output. `persistBoosts` untouched.
- [x] **5.8** `golangci-lint run`; `go test -race ./internal/scheduler/...`.
      **Done** — `make lint` → `0 issues.`; `go test -race ./internal/scheduler/... -v` → all 17
      top-level tests `PASS` (the 12 from PR 4 plus `TestRunPass_AbortSurfacesOnProcessLog`,
      `TestRunPass_NextFireAttemptsFreshWholePass`, `TestRunPass_CompletedPassLogsCorrupted`
      new here, plus `TestNew_RejectsNilDeps`/`TestScheduler_Wait` subtests), no race.
- [x] Verify (PR-level): `make check-all`; diff scope — `internal/scheduler/scheduler.go` (+ tests,
      extended only, no new file). Target ≤110 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/scheduler-abort-logging` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR6> --json baseRefName` names `main`.

      **PR 5 result** (`feat/scheduler-abort-logging`): `make check-all` green end to end; `internal/
      core` coverage floor unchanged at 750/750 (100%) — this PR adds no `internal/core` code.
      Diff scope matched exactly: `internal/scheduler/scheduler.go` (extended, no new file),
      `internal/scheduler/scheduler_test.go` (extended, no new file) — no strays, no
      `internal/brain`, `cmd/nooma`, or `docs/` file touched (task 5.7's own confirmation above).
      `git diff --stat main...HEAD -- internal/scheduler/`: `scheduler.go` +35/-5 (40 changed
      lines), impl+docs well under the ≤110 target; `scheduler_test.go` +224/-0 (224 test lines),
      ~149% of the ~150 sub-estimate — flagged, not split: squarely inside the 106–193% band prior
      links in this chain already saw, and CLAUDE.md's 400-line PR ceiling counts impl+docs only.
      Most of the test overrun is the disclosed real-`ConsolidateService` fixture (task 5.5's own
      deviation note) — heavier than a plain fake, but the only construction path available under
      this PR's own hard constraints.
      `go test -race -shuffle=on ./internal/scheduler/... -count=5`: green, all 5 shuffled runs, no
      race. `go test -race -run 'TestScheduler_NoOverlap_ExactlyOneInFlight|
      TestCatchUp_IndistinguishableFromCron|TestRunPass_CompletedPassLogsCorrupted|
      TestRunPass_AbortSurfacesOnProcessLog' ./internal/scheduler/... -count=20`: green, no race —
      20 runs of all four log/concurrency-sensitive tests together, zero flakes.

      **The inherited `logf`/unsynchronized-`io.Writer` note (link 3b's Judgment Day, carried
      forward by link 4's own Judgment Day), reasoned through explicitly rather than re-inherited
      unexamined**: this PR adds two new `logf` call sites inside `runPass` — the abort line and the
      completed-pass-with-`Corrupted()` line. Both sit strictly *after* the non-blocking try-lock
      (`select { s.slot <- struct{}{}: ...; default: ... }`, design §3.4/D4) already established in
      PR 3b, on the path where the slot was actually acquired — i.e., inside the
      `defer func() { <-s.slot }()` region. Since the slot has capacity 1 and is acquired
      non-blockingly, **at most one goroutine can be inside that region, and therefore inside either
      new `logf` call, at any instant** — exactly the same guarantee that already covers PR 3b's own
      skip-path `logf` call (which sits *outside* the region, on the `default` branch, contended by
      construction, but writing to `s.log` only after failing to acquire the slot — no overlap with
      the acquired-path calls either, since a `default`-branch caller by definition never entered
      the acquired region at all). All three call sites — skip (3b), abort (5.2), completed-with-
      refusals (5.6) — are therefore mutually exclusive at runtime under the current two-trigger
      model (cron, catch-up), the same model link 3b's and link 4's judges already reasoned about.
      **This does not change the "not yet, but soon" conclusion, and here is why it still holds**:
      the try-lock serializes *pass-holding* access to `logf`, but does not by itself prove no
      *other*, non-pass-holding writer will ever call `s.log.Write` concurrently — that was true in
      PR 3b (no other writer existed) and remains true here (still no other writer exists in this
      PR's own diff), but it stops being trivially true the moment PR 6 wires `serve`'s own
      `errOut` as a shared `io.Writer` that other parts of `runServe` may also write to
      concurrently with a scheduler pass in flight. This PR's own two new call sites do not
      themselves introduce a race — no test failed under `-race` at any point in this PR's work,
      including the 20-run repeated-execution proof above — but the risk surface named in link 3b
      is now three call sites deep instead of one, all still protected by the *same* single
      assumption (the try-lock's own mutual exclusion), which was never `Deps.Log`'s or `logf`'s own
      documented contract. No mutex was added: the orchestrator's own instruction was explicit that
      this is not this link's call to make, and the try-lock's mutual exclusion is sufficient for
      every writer this PR itself introduces. Flagged for link 6's own Judgment Day to re-examine
      once a real, possibly-shared writer is actually wired.

**Judgment Day correction round, `feat/scheduler-abort-logging` (PR #185), frozen target
`f4f57c6`. Three confirmed findings, all fixed in this same branch — none deferred to link 6.**

- **JD-5-01 (CRITICAL, Judge A).** The "mutually exclusive at runtime" reasoning immediately
  above this note was wrong, and this is why. It reasoned from "a `default`-branch caller by
  definition never entered the acquired region" to "no overlap with the acquired-path calls" —
  but that only proves the two callers are in different goroutines and different code regions; it
  proves nothing about ordering between their two `logf` calls, which have no channel, mutex, or
  `WaitGroup` between them at all. Two goroutines calling an unsynchronized `fmt.Fprintf(s.log,
  ...)` — one from the `default` (skip) branch, one from inside the acquired region on abort or
  completed-with-refusals — is a genuine data race under Go's memory model regardless of whether
  the try-lock excludes them from being inside the *same region* at the *same time*; the try-lock
  never claimed to order writes to `s.log` at all, only entry into `Consolidate`. Fixed by adding
  `Scheduler.logMu sync.Mutex`, held for the duration of every `logf` call — see `logf`'s own
  updated doc comment (`internal/scheduler/scheduler.go`) and `Deps.Log`'s own updated doc comment
  for the documented multi-goroutine-writer contract this PR's original version never stated.
  **Regression test**: `TestRunPass_LogfIsRaceFree` (`internal/scheduler/scheduler_test.go`) —
  genuinely exercises the interleaving (a fire holding the slot inside a blocked `Consolidate`
  that then aborts, concurrent with a second fire taking the skip branch, both writing to the same
  `Log`), observed failing for real before the fix:
  ```
  go test -race -run TestRunPass_LogfIsRaceFree -v ./internal/scheduler/...
  WARNING: DATA RACE
  Write at 0x00c0001a27d0 by goroutine 9:
    bytes.(*Buffer).Write()
    fmt.Fprintf()
    github.com/rengo/nooma/internal/scheduler.(*Scheduler).logf()
        internal/scheduler/scheduler.go:97
    github.com/rengo/nooma/internal/scheduler.(*Scheduler).runPass()
        internal/scheduler/scheduler.go:184
  Previous write at 0x00c0001a27d0 by goroutine 8:
    (same stack, scheduler.go:97 via runPass at scheduler.go:196)
  --- FAIL: TestRunPass_LogfIsRaceFree (0.00s)
  ```
  Green after the `logMu` fix: `go test -race -run TestRunPass_LogfIsRaceFree -v
  ./internal/scheduler/...` → `PASS`.
  **Rollback boundary**: `internal/scheduler/scheduler.go`'s `logMu` field and `logf`'s two new
  lines, plus `TestRunPass_LogfIsRaceFree` and `blockingErrorConsolidator` in
  `scheduler_test.go` — revertible independently of JD-5-02's own changes below.
- **JD-5-02 (WARNING, Judge B).** `runPass`'s abort branch discarded `report` entirely, so any
  unit an earlier phase had already refused (`internal/brain/consolidate.go:1044-1045` returns
  `(report, err)` together; `report.reportCorrupted` runs from five phase sites, any of which can
  run before a later phase aborts the same pass) never reached the process log — the only place a
  refused unit is surfaced at all for an unattended pass. Fixed: the abort branch now surfaces
  `report.Corrupted()` alongside the abort, as **one combined log line**, not two separate `logf`
  calls — chosen over two lines because `logMu` (JD-5-01) only ever makes a single `logf` call
  atomic, not two consecutive ones, so two calls would let a concurrent, unrelated write from
  another goroutine land between them once `Deps.Log` becomes a writer other code shares (the
  exact risk this same PR's own note above already flagged for link 6). When `Corrupted()` is
  empty the line is byte-identical to before. `internal/brain/consolidate.go` is untouched — no
  retry loop, no partial-application logic (spec R1.4's own MUST); `git diff --stat --
  internal/brain/consolidate.go` on this branch is empty, confirmed below.
  **Regression test**: `TestRunPass_AbortSurfacesRefusedUnits` — wires a real
  `*brain.ConsolidateService` (task 5.5's own precedent: `ConsolidateReport`'s fields are
  unexported, so no fake outside package `brain` can construct a non-empty `Corrupted()`) over one
  seeded corrupted unit plus `abortingUnitsAfterNCalls{n: 2}`, a `ports.UnitRepo` wrapper that
  fails Connect's own `LiveDecayStates` read (slot 4) strictly after Archive's own `slot` 2 refusal
  is already recorded — so the returned `(report, err)` pair genuinely carries both in the same
  call. Observed failing for the stated reason before the fix:
  ```
  go test -run TestRunPass_AbortSurfacesRefusedUnits -v ./internal/scheduler/...
  scheduler_test.go:764: log = "scheduler: pass aborted (cron): consolidate: connect: read live
  decay states: boom\n", want "scheduler: pass aborted (cron): consolidate: connect: read live
  decay states: boom, refused 1 unit(s) before the abort: [u-corrupted]\n"
  --- FAIL: TestRunPass_AbortSurfacesRefusedUnits (0.00s)
  ```
  Green after the fix: `PASS`.
  **Rollback boundary**: `runPass`'s abort branch (the `if corrupted := report.Corrupted(); ...`
  clause) plus `TestRunPass_AbortSurfacesRefusedUnits` and `abortingUnitsAfterNCalls` in
  `scheduler_test.go` — revertible independently of JD-5-01's own `logMu` change.
- **JD-5-03 (docs).** `design.md`'s diagram documenting the shape that produced JD-5-02 lives in
  **§5.3 "Flow"**, not §5.2 as the correction round's own delegate prompt named it — §5.2 is "The
  dependency surface" (a Go interface/struct block with no diagram at all); the `err ─▶ log the
  abort │ ok ─▶ log Corrupted(), if any` diagram Judge B described is unambiguously §5.3's own
  ASCII flow chart. Fixed at its real location: the diagram no longer gives the abort and
  completed-with-refusals arms as mutually exclusive; a correction note below it states plainly
  what was wrong (the abort arm carried no `Corrupted()` clause, and the shipped code implemented
  that faithfully — that fidelity *was* the bug), what the corrected behavior now is (the abort arm
  surfaces `Corrupted()` too, one combined line), and why (§3.3's and §3.4's own correction notes
  already took the identical posture twice in this chain: the shipped code was the outlier's
  cause, the design was the one left to correct). Docs-only; recorded separately below.
  **Rollback boundary**: `design.md` §5.3's diagram and its new correction paragraph only.

**Verification, this correction round**: `go test -race -shuffle=on ./internal/scheduler/...
-count=5` → green, no race. `make check-all` → green end to end; `internal/core` coverage floor
unchanged at 750/750 (100%) — this round adds no `internal/core` code. `git diff --stat --
internal/brain/consolidate.go` → empty (JD-5-02's own hard constraint, task 5.7's precedent).
`make lint` → `0 issues.`

---

## PR 6 — `feat/serve-scheduler-wiring` (~140 impl+docs / ~120 test)

Depends on PR 5. Ships `wireScheduler` and `serve`'s own `Start` call over the vault/lock/ctx
`serve` already holds. **Pre-drawn split, per design §13**: 6 (wiring, ~140) | 7 (the shutdown
join, ~60) — the two have different failure modes (a wiring change vs. an e2e SIGTERM fixture) and
must not hold each other hostage.

- [x] **6.1** Commit 1 (RED): `test/e2e/serve_scheduler_test.go` (new, `e2e` build tag) —
      `TestServe_ConcurrentConsolidate_StillRefused`: a `serve` process with the scheduler wired,
      running against a vault a second `nooma consolidate` invocation targets, asserts the CLI is
      refused with `m2c` R6.1's existing clean lock error.
      **Red**: `undefined: wiring.wireScheduler` — package/binary does not compile.
      Requirement: spec R3.1; design §6.
      **Disclosed, not a genuine red** (commit `bef96d0`): `test/e2e` only drives the compiled
      binary as a subprocess and never imports `cmd/nooma` (`package main`, unimportable by any
      other package), so no reference to an unexported `wireScheduler` could ever appear in this
      file, compile-time or otherwise. Run against unmodified `main` (before `wireScheduler`
      existed at all), the test already **passes**: the refusal it asserts comes from
      `vaultlock`'s pre-existing mutual exclusion (M1), unrelated to anything this PR adds —
      confirmed by actually running it against `main` first, not merely reasoned about. Kept as a
      genuine forward regression guard for R3.1 (proving `wireScheduler`'s addition does not
      change this observable behavior), not a fabricated red.
- [x] **6.2** Commit 2 (GREEN): `cmd/nooma/wiring.go` (extend) — `wireScheduler(ctx, db, cfg,
      lookup, log) (*scheduler.Scheduler, error)`, reusing `wireConsolidate`'s own resolution over
      the `*sqlite.Vault` `runServe` already opened under its own lock (`serve.go:71-89`) — no
      second `vaultlock.Acquire` anywhere.
      Verify: `go test -tags e2e ./test/e2e/... -run TestServe_ConcurrentConsolidate`.
      Requirement: spec R3.1; design §6.
      **Done** (commit `59bba01`) — this stage's own naive shape still propagates
      `resolveConsolidateProviders`' refusal as a hard error (fixed in 6.4); `6.1`'s test verified
      green, now proving the property live over a real `wireScheduler` rather than vacuously.
- [x] **6.3** Commit 1 (RED): `test/e2e/serve_scheduler_test.go` (extend) —
      `TestServe_UnconfiguredVault_HTTPStillAnswers`: `serve` started against a vault with no
      `relation_evaluation`/`embedding` task binding asserts the HTTP server starts and answers
      `/capture`/`/recall` normally, no scheduled or catch-up pass ever fires.
      **Red**: `wireScheduler` (as of `6.2`) does not yet degrade gracefully — fails `runServe`
      outright or panics on the unresolved provider.
      Requirement: spec R3.2; design §6.
      **Genuine, observed failing for the stated reason** (commit `99c9a20`): `serve never
      answered on port <port>`, `stderr: nooma: wiring the scheduler: consolidate: task
      "relation_evaluation" has no provider bound — add it under tasks: in nooma.yml before
      running a pass` — `runServe` exited before the listener ever came up, exactly as predicted.
- [x] **6.4** Commit 2 (GREEN): `wiring.go` (extend) — `wireScheduler` mirrors `wireBrain`'s
      degrade-to-`nil` precedent (`wiring.go:236-238`): a vault whose `resolveConsolidateProviders`
      refuses yields a `nil` scheduler, `nil` error, one log line naming why. `Start`/`Wait` are
      no-ops on a `nil *Scheduler` (design §6's own note — `runServe` needs no branch).
      Verify: `go test -tags e2e ./test/e2e/... -run TestServe_UnconfiguredVault`.
      Requirement: spec R3.2; design §6.
      **Done** (commit `463b632`) — `wireScheduler` now checks `resolveConsolidateProviders`
      itself, ahead of `wireConsolidate`'s heavier call (which opens the resident vector index),
      the same two-step shape `resolveTaskProviders`/`wireBrain` already take; `6.3`'s test green
      afterward, for the stated reason.
- [x] **6.5** Commit 2 (GREEN, same commit as 6.4): `cmd/nooma/serve.go` (extend) — call
      `scheduler.Start(ctx)` after `wireBrain`, using the same signal-aware `ctx`
      `signal.NotifyContext` already produces (`serve.go:114`) — not a new `context.Background()`.
      Verify: `go test -tags e2e ./test/e2e/...`.
      Requirement: spec R3.3 (first clause); design §6.
      **Done** (commit `463b632`) — `sched.Start(ctx)` called unconditionally right after the
      signal-aware `ctx` is created; relies on `6.4`'s own nil-safe `Start`/`Wait` (task 6.7) so
      `runServe` needs no branch on the unconfigured-vault case.
- [x] **6.6** *(split checkpoint)*: measure `git diff --stat` for tasks 6.1–6.5 in isolation against
      the ~140/120 sub-estimate. The 6/7 split is already pre-drawn (design §13); this checkpoint
      confirms PR 6 alone stays inside its own share, flagging rather than splitting reflexively if
      at risk.
      **Done, no further split** — `git diff --stat main -- cmd/nooma/wiring.go cmd/nooma/serve.go
      internal/scheduler/scheduler.go test/e2e/serve_scheduler_test.go
      internal/scheduler/scheduler_test.go`: impl+docs (`serve.go` +15, `wiring.go` +47,
      `scheduler.go` +16 — the nil-guard, see `6.7`'s own disclosure) = 78 lines, well under the
      ~140 sub-estimate (56%); test (`scheduler_test.go` +18, `serve_scheduler_test.go` +162) =
      180 lines, ~150% of the ~120 sub-estimate — flagged, not split: the overrun is entirely in
      test lines, inside the 106–193% band every prior link in this chain has already seen, and
      CLAUDE.md's own 400-line PR ceiling counts impl+docs only (78, nowhere near it).
- [x] **6.7** `golangci-lint run`; confirm `Start`/`Wait` remain no-ops on a `nil *Scheduler`
      (covered by `6.4`'s own fixture).
      **Done** — `make lint` → `0 issues.`. **Disclosed diff-scope note**: making `Start`/`Wait`
      genuinely no-op on a nil receiver required a two-line guard in each method
      (`internal/scheduler/scheduler.go`), one file beyond this PR's own stated diff scope
      (`cmd/nooma/{wiring,serve}.go`, `test/e2e/serve_scheduler_test.go`) — a minimal,
      non-negotiable-mandated change, not scope creep; without it `TestServe_UnconfiguredVault_
      HTTPStillAnswers` (6.3/6.4) would panic once `Start` is called unconditionally (6.5) against
      a `nil` scheduler. Genuineness proven by a disclosed temporary probe (the same technique
      links 3a/4 used): both nil checks removed, the new `TestScheduler_NilScheduler_
      StartAndWaitAreNoOps` (`internal/scheduler/scheduler_test.go`) failed with `panic: runtime
      error: invalid memory address or nil pointer dereference` at the exact removed line
      (`scheduler.go:132`, `Start`), then the file restored byte-identical (`diff` against a
      pre-probe copy, clean) before the commit that ships the guard.
- [x] Verify (PR-level): `make check-all` incl. the `e2e`-tagged suite; diff scope — `cmd/nooma/
      {wiring,serve}.go`, `test/e2e/serve_scheduler_test.go` (new). Target ≤140 impl+docs lines.
      **Disclosed, temporary gap inside this stacked chain**: this PR does **not** add
      `sched.Wait(shutdownCtx)` (D5's own correction — PR 7's scope). Between this PR's merge and
      PR 7's, `main` closes the vault's `*sql.DB` in a `defer` without joining a live pass goroutine
      first — a real, if narrow, correctness gap, disclosed here rather than presented as already
      closed, the same honesty `m2c` PR 7a/7b's own split carried for its missing decision-log half.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/serve-scheduler-wiring` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR7> --json baseRefName` names `main`.

      **PR 6 result** (`feat/serve-scheduler-wiring`): `make check-all` green end to end;
      `internal/core` coverage floor unchanged at 750/750 (100%) — this PR adds no `internal/core`
      code. Diff scope: `cmd/nooma/wiring.go` (extended, +47/-0), `cmd/nooma/serve.go` (extended,
      +15/-1), `test/e2e/serve_scheduler_test.go` (new, 162 lines) as declared, plus
      `internal/scheduler/scheduler.go` (+16, the nil-guard) and
      `internal/scheduler/scheduler_test.go` (+18, its own direct proof) — both disclosed above
      (task 6.7) as a minimal, non-negotiable-mandated addition beyond the stated file list, not a
      stray. Impl+docs 78 lines (56% of the ~140 estimate); test 180 lines (~150% of the ~120
      estimate, flagged not split per 6.6).

      **The `Deps.Log` writer reasoning link 5's Judgment Day (JD #728) named for this link,
      carried out explicitly rather than inherited**: `wireScheduler` passes `errOut` as
      `scheduler.Deps.Log` — in production, `os.Stderr` (`cmd/nooma/main.go:20`,
      `run(os.Args[1:], os.Stdout, os.Stderr)`). `os.Stderr` is a real OS-level `io.Writer` that
      **can** block: redirected to a stalled pipe, a full disk, a slow syslog/journal consumer, or
      `docker logs` backpressure are all real deployment shapes, not fabricated ones. This makes
      link 5's round-2 WARNING ("`logf` now holds `logMu` across `fmt.Fprintf`... a pathological
      `Deps.Log`... blocks every other `logf` caller, and `wg.Done()`/`Wait` too") concrete rather
      than hypothetical for the first time — exactly what JD #728 asked this link's own review to
      re-examine. Per the orchestrator's explicit instruction, this is reported rather than
      silently redesigned: **no timeout, no mutex change, no `logf` redesign was made in this PR.**
      `internal/scheduler/scheduler.go`'s own `Deps.Log` and `logf` doc comments already carry this
      exact hazard (JD-5-01), written before this PR, and this PR does not touch either.

      **Whether `runServe` writes to the same writer independently of the scheduler, which would
      make it a second writer bypassing `logf` entirely (`logMu` cannot help there)**: verified
      directly by reading `cmd/nooma/serve.go` end to end. `runServe`'s own human-facing lines
      (`"nooma serving %s on http://%s\n"`, `"\nshutting down"`) write to `out` (`os.Stdout`), not
      `errOut` — confirmed by reading the two exact call sites design §5.4 cites
      (`serve.go:119,137` in the pre-PR6 file), which the design document's own prose describes
      inaccurately (it claims `errOut` "already carries runServe's own human lines" at those two
      lines; the code at those lines has always written to `out`). `errOut` is written to only
      during flag parsing (`fs.SetOutput(errOut)`, the `Usage` line), which completes and returns
      before the vault is even resolved — long before `wireScheduler`/`sched.Start` exist in the
      call graph. So: **no second concurrent writer to `errOut` exists *inside `runServe`* while a
      scheduler is running.**

      **Corrected here (JD-6-02, correction round after PR 6), narrower than originally
      stated.** The claim above originally read "no second concurrent writer to `errOut` exists
      inside `runServe` for the lifetime of a running scheduler" — that overreaches. This PR starts
      scheduler goroutines (`sched.Start(ctx)`, `serve.go`) and does not join them: `sched.Wait` is
      never called here (PR 7's own disclosed scope, `6`'s own Verify note above). So a scheduler
      goroutine can still be synchronously inside `logf`, writing to `os.Stderr`, at the moment
      `runServe` itself has already returned an error and unwound — `main()`
      (`cmd/nooma/main.go:19-23`) then writes `fmt.Fprintln(os.Stderr, "nooma:", err)` to that same
      file descriptor, entirely outside `logMu`, which `logf` alone can never guard against. The
      analysis above was correct as far as it went — genuinely no second writer exists *while
      `runServe` itself is still running* — but "inside `runServe`" and "for the lifetime of a
      running scheduler" are not the same claim, and the text conflated them. Impact stays bounded:
      interleaved or torn output on one line at process exit, no data loss, no crash. PR 7's own
      `sched.Wait(shutdownCtx)` addition closes this for the normal shutdown path, by joining every
      scheduler goroutine before `runServe` (and therefore `main`) can return. Docs-only; `design.md`
      §5.4's own citation is corrected in the same round (the two call sites now cited as
      `serve.go:134,152`, matching this file's own current line numbers), with the citation's
      inaccuracy noted the same way, in its own correction paragraph there.

**Judgment Day correction round, `feat/serve-scheduler-wiring` (PR #186), frozen target
`0464bb8`. Two confirmed findings, both fixed in this same branch — none deferred beyond PR 7's
own already-disclosed scope.**

- **JD-6-01 (CRITICAL, Judge A; WARNING, Judge B; both judges confirmed, orchestrator verified
  the cascade firsthand).** `runPass`'s try-lock released the slot only via `defer func() {
  <-s.slot }()`, which fires when `runPass` itself returns — but `logf` (called from inside the
  acquired region, on abort or on a completed pass with refusals) holds `logMu` for the duration
  of `fmt.Fprintf(s.log, ...)`, and this PR is the first to wire `errOut` = `os.Stderr`
  (`cmd/nooma/main.go:20`) into `Deps.Log` in production. `os.Stderr` genuinely blocks under
  ordinary deployment conditions — an unread `docker logs` consumer, a full pipe buffer, a stalled
  journald, a full disk. A goroutine blocked inside `logf` therefore holds **both** `logMu` and the
  slot; `runPass` never returns, so the slot is never released, and every later fire takes the
  `default` branch and calls `logf` itself, which also blocks on the same `logMu` — consolidation
  halts permanently, with no crash and no log line escaping to say so, because the log is the thing
  that is stuck.

  Fixed: the slot is now released explicitly right after `Consolidate` returns, **before** any
  `logf` call — not by a defer that only fires once `runPass` itself returns
  (`internal/scheduler/scheduler.go`). Panic-safety is preserved: a `released` bool plus an
  idempotent `release()` closure mean the explicit call covers the normal path and a `defer
  release()` covers a panic inside `Consolidate`, with neither able to run `<-s.slot` twice — a
  double release would otherwise block the releasing goroutine on an already-empty channel and
  permanently consume a slot token no fire legitimately holds, deadlocking the very next fire's own
  non-blocking acquire. D4's try-lock semantics are unchanged: non-blocking acquire, skip-don't-queue,
  at most one pass in flight — verified by `TestScheduler_NoOverlap_ExactlyOneInFlight` staying
  green, and confirmed still genuinely discriminating by a disclosed temporary probe (the same
  technique links 3a/4/6 used): the try-lock's `slot` channel capacity bumped from 1 to 2, the test
  observed failing for real (`max concurrent Consolidate calls = 2, want exactly 1`), then the file
  restored byte-identical (`diff` against a pre-probe copy, clean) before this commit.

  **Regression test**: `TestRunPass_SlotReleasedBeforeBlockedLog`
  (`internal/scheduler/scheduler_test.go`) — a `Deps.Log` writer (`permanentlyBlockedWriter`) whose
  `Write` blocks forever, a first fire that reaches the abort-log path and calls `logf` into it, and
  a proof that a SECOND, independent fire (`twoPhaseConsolidator`) can still acquire the slot and
  actually run its own pass. Observed failing for real against the pre-fix code:
  ```
  go test -run TestRunPass_SlotReleasedBeforeBlockedLog -v -timeout 15s ./internal/scheduler/...
  --- FAIL: TestRunPass_SlotReleasedBeforeBlockedLog (2.00s)
      scheduler_test.go:912: timed out waiting for the second fire to return — the slot (and
      logMu) are permanently wedged by the first fire's blocked logf call
  FAIL
  ```
  Green after the fix: `PASS`.

  **Deliberately deferred, not fixed here (recorded per the owner's own instruction)**: `logf`
  itself is not redesigned — no async writer, no buffered channel, no timeout. A blocked `Deps.Log`
  writer still stalls the individual goroutine that called `logf`, and still stalls `logMu` for
  every OTHER goroutine that tries to log while it is blocked (two fires, or a fire and a future
  caller, cannot both log concurrently if one is wedged) — only the *permanent, all-future-fires*
  halt this round closes. A genuine fix for the underlying blocking-writer hazard is its own future
  work unit, not in this branch's scope.

  **Rollback boundary**: `runPass`'s slot-acquire/release restructuring
  (`internal/scheduler/scheduler.go`) plus `TestRunPass_SlotReleasedBeforeBlockedLog`,
  `permanentlyBlockedWriter` and `twoPhaseConsolidator` in `scheduler_test.go` — revertible
  independently of JD-6-02's docs-only fix below.

- **JD-6-02 (WARNING, confirmed by both judges).** `cmd/nooma/main.go:19-23` writes
  `fmt.Fprintln(os.Stderr, "nooma:", err)` at process exit, entirely outside `logMu`. PR 6's own
  second-writer analysis above scoped its "no second concurrent writer" claim to *inside
  `runServe`*, but this PR starts scheduler goroutines (`sched.Start(ctx)`, `serve.go`) without
  joining them (`sched.Wait` is PR 7's own disclosed scope) — so a scheduler goroutine can still be
  synchronously inside `logf`, writing to `os.Stderr`, at the exact moment `runServe` has already
  returned and `main()` writes its own line to the same file descriptor. The PR 6 text, read
  literally, could be misread as covering the scheduler's whole lifetime rather than only the
  window while `runServe` itself is still executing — those are not the same claim.

  This is a documentation fix, not a code fix: the analysis above is corrected in place (see its own
  new correction paragraph) to state the real scope precisely (exclusive *inside `runServe`*, not
  for the scheduler's whole lifetime) and to name PR 7's `sched.Wait(shutdownCtx)` as what closes
  the normal shutdown path, by joining every scheduler goroutine before `runServe` (and therefore
  `main`) can return. Impact stays bounded either way: interleaved or torn output on one line at
  process exit, no data loss, no crash. `cmd/nooma/main.go`'s error reporting is untouched — closing
  this gap in code stays PR 7's own scope, not assumed here.

  **Rollback boundary**: this file's own correction paragraph above (PR 6's Verify note) plus
  `design.md` §5.4's own new correction paragraph — docs only, no code changed, independently
  revertible from JD-6-01.

**Also corrected (docs only, no separate ledger ID)**: `design.md` §5.4 cited
`cmd/nooma/serve.go:119,137` as carrying `runServe`'s own human lines on `errOut` — those two lines
have always written to `out`, confirmed by reading both call sites directly (now `serve.go:134,152`
as the file has grown since the pre-PR6 citation). Corrected in `design.md` §5.4 itself, in the same
style this chain has used three times already (links 3b §3.4, 4 §3.3, 5 §5.3): what was wrong, the
real shape, and why — not merely swapped line numbers.

**Verification, this correction round**: `go test -run TestRunPass_SlotReleasedBeforeBlockedLog -v
-timeout 15s ./internal/scheduler/...` → red before the fix (shown above), green after.
`go test -race -shuffle=on ./internal/scheduler/... -count=5` → green, no race.
`go test -tags e2e ./test/e2e/...` → green. `make check-all` → green end to end; `internal/core`
coverage floor unchanged at 750/750 (100%) — this round adds no `internal/core` code. `make lint` →
`0 issues.`

---

## PR 7 — `feat/serve-shutdown-cancel` (pre-drawn split of 6) (~60 impl+docs / ~200 test)

Depends on PR 6. Closes the gap PR 6 disclosed: `sched.Wait(shutdownCtx)` sharing the existing
`shutdownGrace` budget (D5's correction to spec R3.3, verified against the code, not merely the
spec's literal reading).

- [x] **7.1** Commit 1 (RED): `test/e2e/serve_shutdown_test.go` (new, `e2e` tag) —
      `TestServe_SIGTERM_PassInFlight_ExitsWithinGrace`: `serve` started with a slow fake
      consolidation pass in flight (synchronized via a channel the fake signals on `ctx.Done()`, not
      relied on to race a real SQL close deterministically — design §3.5's own "cancellation is not
      instantaneous" caution), sent `SIGTERM`, asserts the process exits within `shutdownGrace` and
      the fake observed `ctx` cancellation before exit.
      **Red**: without `7.2`'s join, `runServe` exits without ever cancelling/joining the pass in a
      way this fixture can observe — fails on the missing cancellation signal.
      Requirement: spec R3.3; design §3.5 (D5).
      **Disclosed, in three parts, none papered over.**
      1. **A real defect in the first draft of this fixture, found and fixed before any RED was
         trusted**: the original blocking handler (`select { case <-r.Context().Done(): ... }`,
         no drain) never observed cancellation at all — not "eventually", not "slowly", *never*,
         confirmed with a standalone probe outside the whole e2e harness (a ~15-line throwaway
         `httptest.Server` + client, `go run`, seconds not minutes): Go's `net/http` server only
         starts watching a connection for an early client close (the mechanism that cancels
         `r.Context()`) once the request body is fully drained. Without that drain, the probe's own
         server-side wait sat past 10s with `"server-side cancellation NOT observed within 10s"`,
         and `httptest.Server.Close()` then deadlocked entirely (`fatal error: all goroutines are
         asleep`). The real (non-throwaway) fixture's first live run against unmodified `serve.go`
         hit the exact same failure mode as a **600-second harness timeout**, not a legible
         assertion failure — a defect independent of whether `7.2` was implemented, since a hang
         proves nothing about the stated red reason. Fixed by draining and closing `r.Body` before
         blocking, and by bounding every remaining wait in the file (`waitBudget`,
         `shutdownGraceCeiling`, a `waitForExit` helper wrapping `cmd.Wait()` with a timeout +
         force-kill) so a genuine hang now fails in tens of seconds with a message naming exactly
         what did not arrive, never the harness's own multi-minute timeout.
      2. **This task's own stated red does not materialize**, verified rather than assumed — run
         twice, back to back, against unmodified `serve.go` (before `7.2` existed): **PASS, 120.96s;
         PASS, 120.92s** (not cached — `-count=1` — and not flaky across the two runs). PR 6 already
         wires the pass's ctx to the same signal-aware ctx the HTTP server uses, and every provider
         call in this codebase is ctx-aware (`http.NewRequestWithContext` in
         `internal/providers/ollama`), so once the request body is properly drained, cancellation
         propagation and unwinding are sub-millisecond — independently confirmed by the same
         standalone probe above (`"server observed cancellation after 1.0007105s"` measured from a
         1-second-delayed `cancel()`, i.e. under 1ms of observed latency). The background scheduler
         goroutine reliably finishes (including its own abort `logf` line) before the main
         goroutine's own, slightly slower `Shutdown()`-involving path returns — regardless of `7.2`.
      3. **A second, later, deeper probe — asked for explicitly, not offered — settles a stronger
         question: does ANY test in this file distinguish `sched.Wait` being present from being
         absent, at all?** Not "is `7.1`'s own red genuine" (already answered: no), but the general
         property. Answer, verified rather than argued: **no.** `sched.Wait(shutdownCtx)` was
         removed from `cmd/nooma/serve.go` entirely (not commented out — the call and its 12-line
         doc comment deleted, `git diff --stat` showing `13 deletions(-)`), and all three
         `TestServe_SIGTERM_*` tests were run together against that build:
         `TestServe_SIGTERM_PassInFlight_ExitsWithinGrace` PASS (120.92s),
         `TestServe_SIGTERM_ConsolidationLastRunAtUnchanged` PASS (120.12s),
         `TestServe_SIGTERM_FollowUpRunCompletesFreshPass` PASS (120.14s) — all three green with the
         join entirely absent. `serve.go` was then restored via `git checkout --`, confirmed
         byte-identical (`git diff --exit-code` clean). This is a genuine, disclosed gap: `7.2` is
         still correct and required by spec R3.3/design §3.5 as a structural safety net for a
         wedged or non-ctx-aware provider call (a case this project's real clients, all
         `http.NewRequestWithContext`-based, do not reach), but this test suite provides **no
         behavioral proof of `7.2`'s own necessity** — only the L2 unit-level `TestScheduler_Wait`
         (PR 3a, `internal/scheduler/scheduler_test.go`) proves `Wait` itself blocks until its
         goroutines unwind. Recorded here as a follow-up gap, not fixed by widening this PR's scope
         to build a non-ctx-aware fake provider, which would test a code path (an LLM client that
         ignores `ctx`) nothing in this codebase currently has.
- [x] **7.2** Commit 2 (GREEN): `cmd/nooma/serve.go` (extend) — `shutdownCtx, cancel :=
      context.WithTimeout(context.Background(), shutdownGrace); defer cancel();
      server.Shutdown(shutdownCtx); sched.Wait(shutdownCtx)` — same budget, no second window (D5's
      exact shape, design §3.5).
      Verify: `go test -tags e2e ./test/e2e/... -run TestServe_SIGTERM_PassInFlight`.
      Requirement: spec R3.3; design §3.5.
      **Done** — `sched.Wait(shutdownCtx)` added at `cmd/nooma/serve.go:171`, taking the exact same
      `shutdownCtx` `server.Shutdown` already used at line 156 (confirmed by reading the committed
      diff, not merely by reasoning about it). Existing error-handling around `server.Shutdown`
      preserved (a minor, disclosed deviation from the task's own literal code snippet, which drops
      the `if err != nil` check for brevity — kept the existing check rather than regressing error
      propagation). `TestServe_SIGTERM_PassInFlight_ExitsWithinGrace` stays green with the join in
      place (121.03s).

      **Corrected (JD-7-01, correction round below)**: the sentence above understated its own scope.
      Keeping the `if err != nil { return ... }` check unmodified meant that check's own `return` ran
      **before** `sched.Wait` on the `server.Shutdown`-error branch specifically — the join covered
      only the happy path, silently narrowing R3.3/D5's guarantee, which this disclosure did not say
      at the time. Fixed and re-disclosed in the correction round below; this paragraph is the
      corrected statement of that omission, not a new gap.
- [x] **7.3** Commit 1 (RED): `serve_shutdown_test.go` (extend) —
      `TestServe_SIGTERM_ConsolidationLastRunAtUnchanged`: `consolidation_last_run_at` is unchanged
      from before the pass started, after the `SIGTERM` above.
      **Red**: only meaningfully red if `7.2`'s join somehow let the pass complete far enough to
      write the column — relies on `m2c` R5.4's completion-gated write as a fact, not re-proven here.
      Requirement: spec R3.3 (second Verified-by clause).
      **Done, combined with `7.4` into one commit** (both declared non-genuine reds, no code change
      between them — same posture PR 3b tasks 3b.3+3b.4 and PR 5 tasks 5.1+5.3 took). PASS (120.93s).
      Relies on, does not re-derive, `m2c` R5.4 — confirmed the reliance is real by reading
      `internal/brain/consolidate.go:1039-1057` again for this task rather than trusting the earlier
      citation: `RecordConsolidationRun` is reached only after the full `Order()` loop returns with
      no error.
- [x] **7.4** Commit 1 (RED): `serve_shutdown_test.go` (extend) —
      `TestServe_SIGTERM_FollowUpRunCompletesFreshPass`: a follow-up `serve`/`consolidate` run
      against the same vault after the cancelled shutdown asserts a fresh whole pass runs to
      completion with nothing skipped because of the earlier cancellation.
      **Red**: fails only if the cancelled pass left partial state — again relies on, does not
      re-derive, `m2c` R5.4's completion-gated write.
      Requirement: spec R3.3 (third Verified-by clause).
      **Done** — PASS (120.15s). The follow-up reuses the same fake LLM URL already bound in the
      vault's `nooma.yml`: `blockingConsolidateLLM`'s block is one-shot (already spent by the
      fixture's own `SIGTERM`), so every request from this follow-up pass is proxied through and
      answered normally, with no separate fixture needed.
- [x] **7.5** Verify/confirm: no code change needed beyond `7.2` for `7.3`/`7.4` — verification
      tasks, the property holds by construction.
      Verify: `go test -tags e2e ./test/e2e/...`.
      Requirement: spec R3.3.
      **Confirmed** — no code change was made for `7.3`/`7.4` beyond the two new test functions
      themselves; `cmd/nooma/serve.go` was touched only once, in `7.2`'s own commit.
- [x] **7.6** `golangci-lint run`; `go test -race ./cmd/nooma/...`.
      **Done** — `make lint` → `0 issues.`; `go test -race ./cmd/nooma/...` → `ok
      github.com/rengo/nooma/cmd/nooma 1.690s`, no race.
- [x] Verify (PR-level): `make check-all` incl. the `e2e` suite; diff scope — `cmd/nooma/serve.go`
      (+ `test/e2e/serve_shutdown_test.go`, new). Target ≤60 impl+docs lines. Threat matrix
      discharge: design §10's "Process lifecycle" row — planned RED tests are `7.1` here and `4.5`
      (cancelled-delay, PR 4).
      **Chain-merge check 1**: `git ls-remote --heads origin feat/serve-shutdown-cancel` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR8> --json baseRefName` names `main`.

      **PR 7 result** (`feat/serve-shutdown-cancel`): `make check-all` green end to end;
      `internal/core` coverage floor unchanged at 750/750 (100%) — this PR adds no `internal/core`
      code. Diff scope matched exactly: `cmd/nooma/serve.go` (extended, +20/-3 = 23 changed lines),
      `test/e2e/serve_shutdown_test.go` (new, 293 lines) — no strays. Impl+docs 23 lines vs the ~60
      estimate (well under); test 293 lines vs the ~200 estimate (~147%, flagged not split — the
      overrun is entirely test lines, inside the 106–193% band this chain has seen, and driven by
      the fixture's own bounded-wait infrastructure plus the disclosure-worthy standalone-probe
      investigation, not by scope creep).

      **Threat matrix discharge — design §10's "Process lifecycle" row**: "Applicable — signal-aware
      ctx, two long-lived goroutines, a database closed by `defer`." Planned RED tests were `7.1`
      here (L4 SIGTERM-mid-pass) and `4.5` (L2 cancelled-delay, PR 4, already shipped). `4.5`
      remains a genuine, already-proven L2 guard for the *pending catch-up delay* being cancellable.
      `7.1`, as disclosed above, does not itself discriminate `7.2`'s presence for a ctx-aware
      provider — the row's design response (§3.5: "one bounded join inside the existing
      `shutdownGrace` budget") is implemented and structurally correct, but this PR's own L4 test
      cannot demonstrate its necessity against this codebase's real (ctx-aware) clients. Discharged
      as "implemented and end-to-end regression-guarded, not behaviorally proven necessary by this
      suite" — not as "proven", to avoid the exact failure mode CLAUDE.md's own three-links-running
      warning names (a passing suite is not evidence for a property nothing in it exercises).

      **Repeated-run flakiness evidence** (`TestServe_SIGTERM_PassInFlight_ExitsWithinGrace`, the
      one real-process, signal-timing-sensitive test in this file): `go test -tags e2e
      ./test/e2e/... -run TestServe_SIGTERM_PassInFlight_ExitsWithinGrace -count=5 -timeout 20m` —
      all 5 runs green, ~121s each, no flake.

      **Shutdown-path scope, stated explicitly**: `sched.Wait(shutdownCtx)` runs only on
      `runServe`'s clean-shutdown path (after `ctx.Done()`, after `server.Shutdown` returns). Every
      EARLIER `return err` in `runServe` (flag parsing, vault resolution, config load/validate,
      `DecideBinding`, lock acquisition, `sqlite.Open`, `wireBrain`, `wireScheduler`) returns before
      `sched.Start` is ever called, so there is no scheduler goroutine to join or leave unjoined on
      those paths — not a gap, since nothing was started. The one path this PR does NOT close:
      `main.go`'s own `fmt.Fprintln(os.Stderr, "nooma:", err)` (line 21) writes only when `run()`
      returns a non-nil error — i.e. only on one of those same early-return paths, all of which
      exist entirely before scheduler goroutines exist. JD-6-02's second-writer window was ONLY ever
      about the CLEAN-shutdown path racing an unjoined scheduler goroutine's own `logf` — this PR's
      `sched.Wait` closes exactly that path. An early error return was never the scenario JD-6-02
      described, and remains outside this PR's scope for the same reason it was never in it.

**Judgment Day correction round, `feat/serve-shutdown-cancel` (PR #187), frozen target `28e6652`.
Three confirmed findings, all fixed in this same branch.**

- **JD-7-01 (CRITICAL, Judge A; WARNING, Judge B; both judges confirmed, orchestrator verified).**
  `cmd/nooma/serve.go:156-158,171`: `server.Shutdown`'s own error branch returned before
  `sched.Wait(shutdownCtx)` ever ran — `if err := server.Shutdown(shutdownCtx); err != nil { return
  fmt.Errorf(...) }` returned on that branch, and `sched.Wait` sat after it, unreached. design.md's
  own D5 pseudocode (§3.5) and `7.2`'s own task snippet both call `sched.Wait(shutdownCtx)`
  UNCONDITIONALLY after that `if` block, not behind a `return` — `7.2`'s own disclosure (corrected
  above, in place) kept the `if err != nil` check to avoid regressing error propagation, which was
  the right call, but never said doing so also narrowed R3.3/D5's join guarantee to the happy path
  only. Judge B's fairness note, recorded rather than dropped: in *disposition* this was
  pre-existing, since PR 6's base never joined on any path — this PR closed the happy path and left
  the error branch exactly as unjoined as it always was. A narrowed guarantee, not a fresh
  regression, but still the gap this correction closes.

  **Fixed shape**: `server.Shutdown`'s error is now captured (`shutdownErr := server.Shutdown(shutdownCtx)`)
  rather than branched on immediately; `sched.Wait(shutdownCtx)` runs unconditionally right after,
  on the same shared `shutdownCtx` (still one budget, no second window); the captured error is
  returned only afterward (`if shutdownErr != nil { return fmt.Errorf("shutting down: %w",
  shutdownErr) }`). Chosen over restructuring into two separate blocks because it keeps the exact
  same wrapped error text and exact same non-nil/nil branching the caller already saw — error
  propagation is provably unchanged, byte-for-byte, only its position in the function moved. `Wait`
  stays a no-op on a nil `*Scheduler` (`scheduler.go:157-159`, untouched).

  **Proof, in two parts, since a black-box e2e assertion for the join's own reachability turned out
  not to exist without inventing machinery this codebase does not have (checked, not assumed):**

  1. A genuine new e2e test, `TestServe_SIGTERM_ShutdownErrorStillJoinsScheduler`
     (`test/e2e/serve_shutdown_test.go`), forces `server.Shutdown` itself into a real, non-simulated
     error: `openStuckCaptureConn` opens a raw TCP connection straight to the running `serve`
     process (bypassing the `nooma capture` CLI, which is itself only an HTTP client of this same
     server per design D11) and writes a `POST /capture` whose `Content-Length` promises far more
     body than it ever sends, then stops without closing. `captureHandler`'s `json.Decoder` blocks
     on `r.Body.Read` waiting for bytes that never arrive, so `net/http` treats the connection as
     genuinely active — `server.Shutdown` blocks the full `shutdownGrace` and returns a real
     `context.DeadlineExceeded`, not a mock. This is layered onto the existing `sigtermMidPass`
     fixture (extended with an optional `beforeSignal func(t *testing.T, port int)` hook, run right
     before `SIGTERM`; the three existing `TestServe_SIGTERM_*` tests pass `nil` and are otherwise
     unchanged) so a real pass is also genuinely in flight. The test asserts the process still exits
     with the propagated non-zero status (error propagation intact) and does not hang past
     `waitForExit`'s own bound — real regression coverage against a fix that made the join
     unconditional but introduced a deadlock between it and an already-timed-out
     `server.Shutdown`.

     **Disclosed limitation, found by actually running red-then-green, not assumed**: an earlier
     draft of this test also asserted on the scheduler's own `"scheduler: pass aborted (catchup):"`
     log line as proof the join ran. Run with `-count=1` against `serve.go` stashed back to the
     pre-fix (early-`return`) shape, that assertion **still passed** — the same non-discriminating
     outcome `7.1`'s own disclosure #3 above already recorded for the happy path, for the identical
     reason: `internal/scheduler/scheduler.go`'s `logf` call is made by the pass's own background
     goroutine the instant it observes `ctx.Done()`, synchronously and unconditionally, never gated
     by whether the main goroutine ever calls `Wait` on it. Reused as a sanity check that the
     fixture reaches its intended scenario (the same purpose the other three SIGTERM tests already
     put it to), not as a discriminator — its doc comment says so. Boundaries for this round forbid
     inventing a non-ctx-aware fake to force a genuine discriminator, matching `7.1`'s own accepted
     gap.

  2. The join's own reachability on the error branch — the actual heart of this finding — is proven
     directly against the code instead, by a disclosed temporary probe (the same technique links
     3a/4/6/JD-6-01 already used, never shipped): a one-line marker,
     `fmt.Fprintln(errOut, "PROBE: sched.Wait reached")`, added immediately after
     `sched.Wait(shutdownCtx)` in `serve.go`, with `TestServe_SIGTERM_ShutdownErrorStillJoinsScheduler`
     logging whether the marker appears in the process's stderr. Run with `-count=1` (`go test`'s
     package-level cache does not see changes under `cmd/nooma`, confirmed the hard way — a first
     run without `-count=1` silently replayed a cached result instead of re-executing):
     - Pre-fix shape (early `return` before the probe line, `serve.go` temporarily restored to
       that shape) — `PROBE marker present: false`. The statement is genuinely unreached.
     - Fixed shape (probe placed right after the now-unconditional `sched.Wait`) — `PROBE marker
       present: true`, reproduced twice.
     The probe was then fully reverted from both `serve.go` and the test file; `git diff` against
     each shows only the intended `JD-7-01` fix and test additions, no leftover probe line.

  **Rollback boundary**: `cmd/nooma/serve.go`'s `runServe` shutdown block (the `shutdownErr`
  restructuring) plus `test/e2e/serve_shutdown_test.go`'s `sigtermMidPass` `beforeSignal` parameter,
  `openStuckCaptureConn`, and `TestServe_SIGTERM_ShutdownErrorStillJoinsScheduler` — revertible
  independently of JD-7-02 and JD-7-03 below.

- **JD-7-02 (WARNING, confirmed by both judges).** `test/e2e/serve_shutdown_test.go:180-181`:
  `_ = seed.Process.Kill(); _ = seed.Wait()` — the one process wait in this file bypassing its own
  `waitForExit` helper, contradicting the file's own doc comment (lines 29-34): "every channel
  receive and every process wait below is guarded by one of these two bounds, never left
  unbounded." `SIGKILL` cannot be trapped, so both judges noted a hang here is very unlikely in
  practice — fixed anyway, so the stated invariant is true as written, not true-in-practice-only.

  **Fixed**: `_ = seed.Wait()` replaced with `_ = waitForExit(t, seed)`, with a comment naming why
  (JD-7-02, the file's own doc-comment invariant). `waitForExit`'s own fatal message says "did not
  exit within %s of SIGTERM" — technically imprecise here since this reap follows `SIGKILL`, not
  `SIGTERM`; left as is rather than forking a second near-identical helper for one call site, since
  `SIGKILL`'s untrappable nature makes this branch effectively unreachable regardless of wording.

  **Proof**: a disclosed probe, not a red-then-green test — this codebase has no way to make a
  `SIGKILL`'d process hang (that is the whole reason both judges called a real hang "very
  unlikely"), so there is no red state to construct honestly. Verified instead by reading the diff
  directly: `seed` is now reaped through the same bounded helper (`waitForExit`, `waitBudget` =
  35s) every other process wait in this file already uses, making the doc comment's "never left
  unbounded" claim true by inspection, and confirmed by the full-suite green run below (no new
  timeout, no new flake).

  **Rollback boundary**: the single `seed.Wait()` → `waitForExit(t, seed)` substitution and its
  comment in `sigtermMidPass` — a one-line, independently revertible change.

- **JD-7-03 (WARNING, Judge B; independently measured and confirmed by the orchestrator).**
  Measured directly: `go test -tags e2e -count=1 ./test/e2e/...` → **364.324s** on the frozen
  target, against Go's implicit 600s per-package default — neither `Makefile`'s `test-e2e` target
  nor either CI invocation (`.github/workflows/main.yml` lines 43, 59) passed `-timeout`, so the
  whole package ran on that implicit ceiling by accident of the language default, not by a stated
  decision. The three `TestServe_SIGTERM_*` tests each wait `scheduler.BootConsolidationDelay`
  (120s) via `sigtermMidPass`, none called `t.Parallel()`, and ran sequentially — roughly 360s of
  the 364s total.

  **Fixed, both parts the owner selected**:
  1. Explicit `-timeout 20m` added to `Makefile`'s `test-e2e` target and to both CI invocations in
     `.github/workflows/main.yml` (the `ubuntu` job at line ~43 via `make test-e2e` itself, and the
     `windows` job at line ~66, kept in sync by hand per that job's own existing comment), each with
     a comment naming why the ceiling exists (the `BootConsolidationDelay`-bound SIGTERM tests) and
     that it is a stated decision, not a default.
  2. `t.Parallel()` added to all three `TestServe_SIGTERM_*` tests, so their ~360s of sequential
     waiting overlaps into roughly one `BootConsolidationDelay` window.

  **Isolation checked before parallelizing, not assumed** — read `sigtermMidPass` and every helper
  it calls (`freePort`, `initVault`, `writeConfig`, `startServe`/`startServeCapturingStderr`,
  `mockConsolidateLLM`, `newBlockingConsolidateLLM`, `binaryPath`) directly:
  - **Vault directory**: each call gets its own `t.TempDir()` for both `home` and `work`;
    `initVault` joins the vault name under that test's own `work`, so no two tests share a path.
  - **Port**: each call gets its own `freePort(t)` (`net.Listen("tcp", "127.0.0.1:0")`, read the
    assigned port, close, hand it back) — the file's own comment already discloses the one residual
    risk honestly: a TOCTOU window between closing the probe listener and the child process binding
    it. Running three of these closer together in time (parallel) shifts that pre-existing,
    already-accepted risk's odds slightly without introducing a new failure mode; it was already
    present for any two tests in this package sharing a `go test` invocation.
  - **Fake provider server**: each call gets its own `mockConsolidateLLM(t)` (`httptest.NewServer`,
    a fresh instance) and its own `newBlockingConsolidateLLM(t, llm.URL)` wrapping it (its own
    `atomic.Bool`/channel fields) — no shared server or shared armed/triggered state between tests.
  - **Package-level state**: the only package-level shared state in this file or its helpers is
    `init_test.go`'s `buildOnce sync.Once` / `builtPath` compiling `cmd/nooma` once — `sync.Once`
    provides the happens-before guarantee that makes concurrent reads of `builtPath` after `Do`
    returns safe; no other `var` at package scope holds mutable state any of these three tests
    touch.
  - **Fixed paths**: none found — every path used (`home`, `work`, `vault`) is derived from that
    call's own `t.TempDir()`.
  All three passed this check; none were left sequential.

  **Measured after this fix (JD-7-03 round)**: `go test -tags e2e -count=1 ./test/e2e/...` →
  **256.988s**, down from the 364.324s baseline (a 107.336s / ~29.5% reduction) — smaller than a
  naive "360s → 120s" estimate because this same correction round's
  `TestServe_SIGTERM_ShutdownErrorStillJoinsScheduler` (JD-7-01) was left sequential at the time,
  adding one further ~131s run on top of the other three's shared window. All tests green, no
  failure, no flake observed across the runs in this round.

  **JD-7-04 — the rendezvous, and its measured basis**: `openStuckCaptureConn` used to dial, write
  an 11-byte partial body, and return. `conn.Write` returns as soon as the OS ACKs bytes into the
  connection's kernel receive buffer, which happens the instant the TCP handshake completes — before
  the server process has necessarily called `Accept()` or `Read()`. The caller then sent `SIGTERM`
  immediately, so `server.Shutdown` could see no active connection, return `nil`, and fail the
  test's own non-nil assertion on unlucky timing. Every other blocking wait in that file uses an
  explicit rendezvous; this one did not.

  Measured with a standalone ~15-line probe outside the e2e harness, against a listener whose
  `Accept()` was deliberately held back 2s to model exactly that window: an 11-byte write returned
  in **~79µs** (proving nothing about server-side progress), while a write sized past any plausible
  passive receive buffer completed only after **~3.9s** — i.e. only once something was actively
  draining the connection throughout. `stuckConnBodyFillerBytes` is 16MiB on that basis: Linux's
  `tcp_rmem` and Darwin's `net.inet.tcp.recvspace`/`autorcvbufmax` both cap in the low single-digit
  megabytes absent deliberate long-fat-WAN sysctl tuning, which a loopback test has no reason to
  carry.

  **That basis covers Linux and Darwin only.** Judgment Day round 2 escalated the gap (both judges):
  `.github/workflows/main.yml`'s `e2e-windows` job runs this same file on `windows-latest`, and
  Windows' auto-tuning TCP receive window can grow past those figures over a near-zero-RTT loopback
  connection without the server reading — which would restore the exact false completion the
  rendezvous exists to remove. Resolved by **skipping `TestServe_SIGTERM_ShutdownErrorStillJoins
  Scheduler` on Windows with the reason stated in the test itself**, rather than running it on an
  unmeasured premise: a test whose premise may be false either flakes or passes vacuously, and both
  are worse than not running. The other three `TestServe_SIGTERM_*` tests carry no such premise and
  still cover shutdown on Windows. Reopen once the premise is measured on a Windows runner.

  **JD-7-07 — the whole fixture never ran on Windows, and the escalation round's own claim about
  that was wrong**: the JD-7-04 write-up above stated "the other three `TestServe_SIGTERM_*` tests
  carry no such premise and still cover shutdown on Windows." They do not. `sigtermMidPass` delivers
  a real `SIGTERM` to the child process, and Go's `os.Process.Signal` supports only `os.Kill` on
  Windows — `cmd.Process.Signal(syscall.SIGTERM)` there returns `not supported by windows`
  unconditionally. The `e2e (windows)` CI job was consequently **red from the moment this link's
  tests landed**, failing all three of them after each had burnt its full ~120s
  `BootConsolidationDelay` setup:

  ```
  FAIL: TestServe_SIGTERM_ConsolidationLastRunAtUnchanged (120.50s)  not supported by windows
  FAIL: TestServe_SIGTERM_FollowUpRunCompletesFreshPass     (120.51s)  not supported by windows
  FAIL: TestServe_SIGTERM_PassInFlight_ExitsWithinGrace     (120.52s)  not supported by windows
  ```

  This was invisible to `make check-all`, which runs on the developer's own platform; the
  `e2e (windows)` job is the only gate that could see it, which is exactly why it exists. It was
  also missed once by reading the PR's `mergeStateStatus: BLOCKED` as "checks still running" rather
  than opening `gh pr checks`.

  Fixed by skipping at `sigtermMidPass`'s own root — before its ~2 minute setup rather than after,
  and so any future test built on the fixture inherits it. The platform limitation is real and not a
  defect in `serve`: the shutdown path is covered on Linux and macOS, and Windows offers no
  equivalent signal to model it with. `TestServe_SIGTERM_ShutdownErrorStillJoinsScheduler` keeps its
  own separate Windows skip as well, unreachable behind this one, so that lifting the signalling
  limitation someday cannot silently start it running on the unmeasured buffer premise JD-7-04
  identified.

  **JD-7-06 — a doc comment claimed a red that never happened**: `TestServe_SIGTERM_ShutdownError
  StillJoinsScheduler`'s comment opened "What this test genuinely proves, run red against the
  unfixed shape above and green against the fix". Against the pre-fix shape the test **passed** —
  none of its assertions discriminate, which the same file already conceded two paragraphs below and
  again for the sibling `PassInFlight` test. Corrected to state that it already passes against the
  unfixed shape and is therefore not a red-to-green regression test for the join itself, while
  keeping what it does prove: a fix that made the join unconditional but deadlocked against an
  already-timed-out `server.Shutdown` would fail it via `waitForExit`'s bound.

  **Corrected in the JD-7-05 round (this reasoning was wrong)**: the sequential placement above was
  justified as "it carries its own additional `shutdownGrace` stall on top of the shared
  `BootConsolidationDelay` wait, so it does not compress the same way" — but parallel wall clock is
  bounded by the group's **max**, not the **sum** of its members. Adding a fourth ~131s test to a
  parallel group whose longest member is otherwise ~126s only raises the group's bound to ~131s; it
  was the sequential placement itself that added the extra ~131s on top, discarding roughly half of
  this reduction. `t.Parallel()` was added to
  `TestServe_SIGTERM_ShutdownErrorStillJoinsScheduler` and both this entry and the test's own doc
  comment were corrected to state the right reasoning. Re-measured below.

  **Measured after JD-7-05** (`go test -tags e2e -count=1 ./test/e2e/...`, run twice to check the
  newly-parallelised group for flakes): **137.771s** and **135.900s** — both green, 1.9s apart, no
  flake. Against the **364.324s** original baseline that is a **62.18%** reduction for the 137.771s
  run and **62.70%** for the 135.900s one; against the **256.988s** figure the JD-7-03 round left,
  a further **119.217s** and **121.088s** respectively. (An earlier revision of this paragraph gave
  a single "62.6%" that matched neither run — corrected here rather than left to be rediscovered.) The result lands on the predicted
  bound: a parallel group's wall clock is its longest member (`TestServe_SIGTERM_
  ShutdownErrorStillJoinsScheduler`, ~131s), not the sum of its members. Measured with `-count=1`
  because `go test`'s cache does not observe `cmd/nooma` changes (recorded separately as a
  harness-level finding, out of this link's scope).

  **Rollback boundary**: `Makefile`'s `test-e2e` target and `.github/workflows/main.yml`'s two `run:`
  lines (the `-timeout 20m` additions) are independently revertible from the three `t.Parallel()`
  calls in `test/e2e/serve_shutdown_test.go`, which are in turn independently revertible from
  JD-7-01 and JD-7-02 above.

**Verification, this correction round**: `go test -tags e2e -count=1 -run
TestServe_SIGTERM_ShutdownErrorStillJoinsScheduler -v ./test/e2e/...` → red (`PROBE marker present:
false`) against the pre-fix shape, green (`PROBE marker present: true`, reproduced twice) against
the fix, temporary probe reverted, diff clean. `go test -tags e2e -count=1 ./test/e2e/...` →
green, **137.771s and 135.900s** after JD-7-05 parallelised the fourth test (was 256.988s after the
JD-7-03 round, 364.324s before it). This line previously still carried the stale 256.988s figure
while the paragraph above already reported the corrected one — flagged by Judgment Day round 2 and
propagated here. `make check-all` → green end to end; `internal/core` coverage floor
unchanged at 750/750 (100%) — this round adds no `internal/core` code. `make lint` → `0 issues.`

---

## PR 8 — `feat/demo-golden-format` (~170 impl+docs / ~160 test)

Depends on PR 7 (no code dependency — sequenced last on the stack per design §13's own link order).
Ships the new golden-set format and registration, before any real corpus content — `testdata/
recall/format.md`'s own shape is the bar (design §8.2), and design names this link as one of the
two most likely to blow its budget (prose is not code, and `format.md` is prose).

- [x] **8.1** Commit 1 (RED): `test/conformance/golden_sets_test.go` (extend) — require
      `"consolidation"` registered in `goldenSetDirs`, `formatToType`, and
      `casesDirMustBeEmpty`. **Red**: `assertCasesDirEmptiness`/`TestHarness_GoldenSetFormatMatches
      Type` `t.Fatalf` on the unregistered directory (or the directory itself is missing) — package
      compiles but the test fails loudly, per the existing gate's own half-registration guard.
      Requirement: spec R4.1; design §8.2.
      **Done** (`6ba2aff`): only `goldenSetDirs` was extended in this commit — `formatToType` and
      `casesDirMustBeEmpty` were deliberately left unregistered so `TestHarness_GoldenSetFormats
      Declared` fails on the directory count (3 of 4 found) and `TestHarness_GoldenSetFormatMatches
      Type/consolidation` fails reading the not-yet-existing `format.md`, exactly the disclosed red.
      Package compiled.
- [x] **8.2** `testdata/consolidation/format.md` (new) — written before or in the same commit as the
      first case file: field table (case name; capture script — offset from `t0`, text, the scripted
      fake-provider answer; injected `now`; optional `last_run_at`; expected effects — `archived`,
      `relations_created`, `beliefs`), one fenced ` ```json``` ` worked example, a "what the loader
      does and does not check" section — `testdata/recall/format.md`'s shape, not a template
      improvised past.
      Requirement: spec R4.1; design §8.2.
      **Done** (`ff6cd8a`): 100 lines. Adds a "Why indices, not unit IDs" section
      `testdata/recall/format.md` has no counterpart for — `expected.archived`/`.relations_created`/
      `.beliefs` name `capture_script` array positions, not unit IDs, because a captured unit's ID
      does not exist until `CaptureService.Capture` runs (design D6). Documented as a departure from
      `recall`/`classify`'s authored-row convention up front, not left implicit.
- [x] **8.3** `test/support/goldenset/types.go` (extend) — `ConsolidationExample` struct decoding
      `format.md`'s fenced example via `goldenset.DecodeStrict` — makes `format.md` executable
      rather than aspirational.
      Requirement: design §8.2.
      **Done** (`eef43be`): `ConsolidationExample`/`ConsolidationCapture`/`ConsolidationExpected`,
      72 lines, appended at file end (after `LLMExample.Validate`) rather than between `LLMExample`'s
      struct and its own `Validate` method, where an earlier draft of this edit briefly, incorrectly
      landed it — caught before commit. `ConsolidationExpected.Validate` was dropped (no required
      fields, so a no-op method served no caller).
- [x] **8.4** Commit 2 (GREEN): `golden_sets_test.go` (extend) — register `"consolidation"` in all
      three maps: `goldenSetDirs`, `formatToType`'s constructor, `casesDirMustBeEmpty["consolidation"]
      = false`.
      Verify: `go test ./test/conformance/... -run TestHarness_GoldenSetFormatMatchesType`.
      Requirement: spec R4.1; design §8.2.
      **Done** (`22dc2c0`): all four `TestHarness_GoldenSetFormatMatchesType` subtests PASS.
      `TestHarness_GoldenSetFormatsDeclared/consolidation` still failed at this point (missing
      `format_example.json`, empty `cases/`) — expected, this task's own Verify line is deliberately
      scoped to `-run TestHarness_GoldenSetFormatMatchesType` only, closed by 8.5/8.7.
- [x] **8.5** `testdata/consolidation/format_example.json` (new) — sibling of `cases/`, never inside
      it. Verify: `assertFormatExampleIsSiblingOfCases` passes.
      Requirement: design §8.2.
      **Done** (`63c5c63`): 22 lines, same content as `format.md`'s fenced example. Also bootstrapped
      `testdata/consolidation/cases/.gitkeep` here (not its own task, needed so 8.6's `os.ReadDir`
      has a directory to read). After this commit `TestHarness_GoldenSetFormatsDeclared/consolidation`
      failed only on the empty `cases/` directory (R5.4) — expected, closed by 8.7.
- [x] **8.6** Commit 1 (RED): a loader test proving `format.md`'s documented shape round-trips — a
      case file decoded via `goldenset.DecodeStrict`, asserting each field lands where `format.md`
      says.
      **Red**: no case file exists yet under `testdata/consolidation/cases/` — the loader has
      nothing to decode, fails on a missing/empty directory.
      Requirement: spec R4.1 (Verified by, first clause).
      **Done** (`8045abc`): `TestLoad_ConsolidationCaseRoundTrips` added to
      `test/support/goldenset/loader_test.go`, 57 lines. **Red confirmed**: `cases/` held only
      `.gitkeep`, so the test `t.Fatal`'d on the empty directory verbatim as stated. Package
      compiled.
- [x] **8.7** Commit 2 (GREEN): `testdata/consolidation/cases/*.json` (new) — at least one real case
      satisfying `8.6`.
      Verify: `go test ./test/conformance/... ./test/support/...`.
      Requirement: spec R4.1.
      **Done** (`b54c384`): `dry-cleaning-and-descale-reminder.json`, 19 lines — a two-entry capture
      script against real `testdata/llm/` cases (`classify-remind-me-tomorrow`,
      `classify-pick-up-dry-cleaning`). **Green confirmed**: both target test suites pass. Disclosed
      scope note: this seed case satisfies the loader round-trip only — its `expected` values are
      illustrative, not tuned against real archive/connect/derive thresholds; that tuning is
      `feat/demo-simulated-weeks`'s own job (design §13's PR 9a row), which grows this same
      directory rather than replacing it.
      **Corrected (JD-8-02, correction round below)**: the claim above was false for its first id —
      `classify-remind-me-tomorrow` never existed anywhere in the repository. The seed case was
      rewritten and renamed to `dry-cleaning-and-ambiguous-contract-request.json`, now referencing
      two real `testdata/llm/` cases: `classify-person-ref-ambiguous-ana` and
      `classify-pick-up-dry-cleaning`. See the correction round for the verification run.
- [x] **8.8** Verify: `golden_sets_test.go`'s widened guard fails if the directory is ever empty —
      confirmed via a throwaway local probe (temporarily empty `cases/`, observe the `Fatalf`,
      restore), the same disclosed-probe convention `m2c` task 9.7 used for a reversion check; the
      tree is never left broken by this verification.
      Requirement: spec R4.1 (Verified by, second clause).
      **Done**: moved `dry-cleaning-and-descale-reminder.json` out of `cases/` to `/tmp`, ran
      `TestHarness_GoldenSetFormatsDeclared` — `--- FAIL`, `golden_sets_test.go:56: .../consolidation/
      cases holds only .gitkeep — expected at least one real case beyond .gitkeep (R5.4)`, exactly
      the guard's own message. Moved the file back, `git diff --exit-code` clean (no working-tree
      residue), re-ran the same test — green again.
- [x] **8.9** `golangci-lint run`.
      **Done**: `0 issues.` — no fixes needed, no separate commit.
- [x] **8.10** *(unplanned but flagged split checkpoint — design §13 names PR 8 among the two links
      most likely to blow their budget)*: measure `git diff --stat` in isolation. If `format.md`'s
      own prose risks the ~170 sub-estimate alone, split the prose (`format.md` + example) from the
      loader/registration wiring — an undrawn split, reported honestly rather than forced, the same
      discipline `m2c`'s own PR 5/6/9/11 checkpoints used.
      **Done, flagged not split**: `git diff --numstat main...HEAD` — impl+docs
      (`format.md` 100 + `format_example.json` 22 + `types.go` 72) = **194 lines vs the ~170
      sub-estimate (114%, +24 lines, +14%)**; test (`golden_sets_test.go` 47 + `loader_test.go` 57 +
      the seed case 19 + `.gitkeep` 0) = 123 lines vs the ~160 sub-estimate (77%, comfortably under).
      194 is a modest overrun, not the kind design's own PR 3a/8 budget-risk framing treats as
      requiring a split (PR 7's own precedent treated a considerably larger, 147% test-line overrun
      as flag-not-split for the same reason: `format.md`'s prose is what design §13 itself named as
      this link's own risk, and the overrun is entirely inside that one file's own documentation
      surface, not scope creep into a second concern). Reported, not resolved unilaterally — the
      split decision belongs to the orchestrator, per this task's own instruction.
      **Corrected (correction round below, record defects #2 and #3)**: both the attribution and the
      test-line total above were wrong. (1) The +24-line overrun was NOT "entirely inside" `format.md`'s
      own documentation surface — 72 of the 194 counted impl+docs lines are `types.go`, a Go file;
      design §13 gives one combined ~170 figure for the whole link with no per-file split, so there
      was no basis for attributing the overrun to prose alone. (2) `golden_sets_test.go`'s 47 counted
      insertions-only, but this same `tasks.md` sums insertions+deletions for a modified test file
      elsewhere (e.g. PR 4's "`scheduler_test.go` (extended, +40/-1 = 41 lines)"); on that convention
      `golden_sets_test.go` is **78** lines (insertions+deletions), making the test total **154** (78 +
      57 + 19 + 0) vs the ~160 sub-estimate — **96%, not 77% "comfortably under"**. Still under budget,
      just by a much narrower margin than originally reported.
- [x] Verify (PR-level): `make check-all`; diff scope — `testdata/consolidation/**`, `test/support/
      goldenset/types.go`, `test/conformance/golden_sets_test.go`. Target ≤170 impl+docs lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/demo-golden-format` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR9a> --json baseRefName` names `main`.
      **Done**: `make check-all` green end to end — lint 0 issues, vet, `-race -shuffle=on` full
      unit+conformance suite green, `-tags integration` green, schema-golden diff-clean,
      `internal/core` coverage 750/750 (100%, unchanged — this PR adds no `internal/core` code),
      7-target cross-compile matrix all OK, `-tags e2e` suite green (133.511s). Diff scope matched
      exactly: only `testdata/consolidation/**`, `test/support/goldenset/types.go` and
      `test/support/goldenset/loader_test.go` (loader test, same package, task 8.6's own home), and
      `test/conformance/golden_sets_test.go` were touched — no file outside the declared scope.
      **Target ≤170 impl+docs lines: exceeded at 194 (+14%), flagged in 8.10 above, not split.**
      Both chain-merge checks are post-merge verifications — deferred until after this PR merges;
      not run by this apply phase, consistent with PR 7's own precedent (Judgment Day runs before
      any merge decision).

**Judgment Day correction round, `feat/demo-golden-format` (PR #188), frozen target `2cce109`.
Two CRITICAL findings (JD-8-01, JD-8-02) plus three record defects, all fixed in this same branch.**

- **JD-8-01 (CRITICAL, both judges confirmed, orchestrator verified).** `testdata/consolidation/
  format.md:60` and its own "Checked" section (~lines 84-90) claimed `ConsolidationExample.Validate`
  enforces every "Required: yes" field, including `expected` — but `Validate` never looked at
  `Expected` at all, and `ConsolidationExpected` had no `Validate` method (task 8.3's own disclosed
  drop). Because `Expected` was a plain, non-pointer `ConsolidationExpected`, an absent `expected` key
  and an explicit `"expected": {}` decoded to the identical Go zero value — a case omitting `expected`
  entirely loaded, validated, and asserted nothing, contradicting the document's own named guarantee.

  **RED, observed before the fix**: added `TestDecodeStrict_EnforcesRequiredFields`'s "consolidation:
  an absent expected key is rejected, not silently treated as {}" case
  (`test/support/goldenset/validate_test.go`), data `{"id":"x","capture_script":[{"offset":"0h",
  "text":"t","llm_case_id":"c"}],"now":"2026-01-01T00:00:00Z"}` (no `expected` key). Run against the
  pre-fix code:
  ```
  validate_test.go:77: DecodeStrict({"id":"x","capture_script":[{"offset":"0h","text":"t",
  "llm_case_id":"c"}],"now":"2026-01-01T00:00:00Z"}) = nil, want an error containing "expected is required"
  --- FAIL: TestDecodeStrict_EnforcesRequiredFields
  ```

  **Fixed shape**: `ConsolidationExample.Expected` changed to `*ConsolidationExpected`
  (`test/support/goldenset/types.go`), the same absent-vs-zero-value pattern this file already uses
  for `ConsolidationExample.LastRunAt *string` and `ClassifyExpected.Weight/DecayRate *float64` — a
  pointer, not `json.RawMessage`, because `ConsolidationExpected` is a fixed, already-declared struct
  with no schema-varies-by-type reason to defer parsing (unlike `ClassifyExpected.StructuredData`,
  whose shape genuinely varies by `expected.type`). `ConsolidationExample.Validate` now rejects a nil
  `Expected`. The owner's explicit ruling is preserved: an explicit, empty `"expected": {}` decodes to
  a non-nil pointer to a zero-value struct and stays valid — a case asserting "nothing should happen"
  is legitimate — proven by a new companion test,
  `TestDecodeStrict_ConsolidationExpectedEmptyObjectStaysValid`. `format.md` itself needed no change —
  it already documented the intended contract correctly; only the code was made to honour it.

  **Verification**: `go test ./test/support/goldenset/... ./test/conformance/... -v` — every
  `goldenset` and `golden_sets_test.go` subtest green, including
  `TestLoad_ConsolidationCaseRoundTrips` and `TestHarness_GoldenSetFormatMatchesType/consolidation`
  against the real, unpointer-touched case and fenced example.

  **Rollback boundary**: `test/support/goldenset/types.go`'s `ConsolidationExample.Expected` field and
  `Validate` method, plus the two new tests in `test/support/goldenset/validate_test.go` — revertible
  independently of JD-8-02 and the record corrections below.

- **JD-8-02 (Judge B: CRITICAL, Judge A: WARNING; both judges confirmed, orchestrator verified).**
  `testdata/consolidation/cases/dry-cleaning-and-descale-reminder.json:7`'s first `capture_script`
  entry named `llm_case_id: "classify-remind-me-tomorrow"` — no such file exists anywhere in the
  repository (`fd 'classify-remind-me-tomorrow' testdata/` returns nothing;
  `testdata/llm/cases/` holds 21 cases, none with that name). `test/support/fakeprovider`'s `Complete`
  calls `t.Fatalf` on a missing scripted case, so the seed case would fail the instant PR 9a's
  `fakeprovider` corpus actually replays it — not the "at least one real case" spec R4.1 requires, and
  task 8.7's own disclosure (corrected above) was false for this id.

  **Chosen replacement, verified by running it, not by reasoning about it**: of the four `classify-*`
  cases that return `"type":"task"` (`classify-pick-up-dry-cleaning`, already used;
  `classify-fenced-response`, same message as pick-up-dry-cleaning; `classify-person-ref-ambiguous-ana`,
  message "Ana asked me to send her the contract", `person_ref_status: "ambiguous"`;
  `classify-truncated-response`), `classify-person-ref-ambiguous-ana` was chosen over
  `classify-fenced-response` specifically to avoid capturing the identical "Pick up the dry cleaning"
  text twice in the same two-entry case. Read `internal/brain/capture.go:257-267` directly first:
  `ambiguousPersonRef` only adds an extra `decision_log` row (`recordAmbiguousPersonRefDecision`) — the
  unit is still built via `classify.ToUnit` and persisted `unit.StatusPool` on either branch, so
  `person_ref_status: "ambiguous"` does not change archivability.

  A throwaway test (`test/conformance/zzverify_seed_pair_test.go`, deleted after verification, never
  committed) drove `brain.NewCaptureService(...).Capture` for real over both messages
  (`classify-person-ref-ambiguous-ana` at `t0`, `classify-pick-up-dry-cleaning` at `t0+24h`, `t0 =
  2026-04-01T09:00:00Z`, one month before the case's own `now`), then ran the real
  `internal/core/consolidation.Archive` at `now = 2026-05-01T09:00:00Z`:
  ```
  capture_script[0]: unit=zz-id-1 weight=0.7 decay_rate=0.05 last_touched_at=2026-04-01 09:00:00 +0000 UTC effective@now=0.15619111210390085
  capture_script[1]: unit=zz-id-4 weight=0.6 decay_rate=0.1 last_touched_at=2026-04-02 09:00:00 +0000 UTC effective@now=0.033013932033844326
  --- PASS: TestZZVerifySeedPairArchivesBoth (0.00s)
  ```
  Both effective weights (0.156, 0.033) are below `DefaultWeightThreshold` (0.5) — both indices
  genuinely archive, confirming `expected.archived: [0, 1]` holds for this pair for real, not just by
  inspection.

  **Renamed** (the "dry cleaning and descale reminder" name was no longer truthful once the descale
  message left): `testdata/consolidation/cases/dry-cleaning-and-descale-reminder.json` →
  `testdata/consolidation/cases/dry-cleaning-and-ambiguous-contract-request.json`, `id` field updated
  to match verbatim (naming convention, `format.md`'s own rule). New content: `capture_script[0]` text
  "Ana asked me to send her the contract" / `llm_case_id: "classify-person-ref-ambiguous-ana"`;
  `capture_script[1]` unchanged ("Pick up the dry cleaning on Friday" /
  `classify-pick-up-dry-cleaning"`); `now` and `expected.archived: [0, 1]` unchanged.

  Task 8.7's disclosure above is corrected in place, not silently rewritten — see the `**Corrected
  (JD-8-02 ...)**` paragraph there.

  **Verification**: `go test ./test/support/goldenset/... -run TestLoad_ConsolidationCaseRoundTrips
  -v` — `dry-cleaning-and-ambiguous-contract-request.json` round-trips through `Load`/`DecodeStrict`
  cleanly.

  **Rollback boundary**: the file rename plus its content rewrite
  (`testdata/consolidation/cases/dry-cleaning-and-ambiguous-contract-request.json`) — revertible
  independently of JD-8-01 and the record corrections below.

- **Record correction #1 (Judge A).** `TestGoldenSetFormatExamples`
  (`test/support/goldenset/loader_test.go`) had no `consolidation` entry — the shared table proving,
  for `recall`/`classify`/`llm`, that a top-level and a nested unknown field are both rejected and a
  well-formed example populates every declared field never covered `ConsolidationExample`;
  `TestLoad_ConsolidationCaseRoundTrips` only exercised the happy path. Added a `consolidation` entry
  (`examplePath: testdata/consolidation/format_example.json`, `nestedPath: "expected"`, populated-check
  on `ID`/`CaptureScript`/`Now`/`Expected`) — same coverage shape as its three siblings. Verified:
  `go test ./test/support/goldenset/... -run TestGoldenSetFormatExamples -v` —
  `TestGoldenSetFormatExamples/consolidation` PASS, including the nested-unknown-field-in-`expected`
  rejection.

- **Record correction #2 (Judge A).** 8.10's budget disclosure attributed the +24-line overrun to
  `format.md`'s own prose alone ("the overrun is entirely inside that one file's own documentation
  surface"), but 72 of the 194 counted impl+docs lines are `types.go`, a Go file — `design.md` §13
  gives one combined ~170 figure for the whole link with no per-file split, so there was no basis for
  that attribution. Corrected in place in 8.10 above.

- **Record correction #3 (Judge B).** 8.10's test-line accounting counted `golden_sets_test.go` as 47
  (insertions only), inconsistent with this same `tasks.md`'s own established convention of summing
  insertions+deletions for a modified test file (e.g. PR 4's "`scheduler_test.go` (extended, +40/-1 =
  41 lines)"). On that convention `golden_sets_test.go` is 78 lines, making the test total 154 vs the
  ~160 sub-estimate — 96%, not 77% "comfortably under". Corrected in place in 8.10 above.

**Verification, this correction round**: `go build ./...`; `go test ./test/support/goldenset/...
./test/conformance/... -v` (all green, including the new/renamed consolidation coverage);
`make check-all` (below).

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

- [x] **9a.1** Commit 1 (RED): `test/e2e/consolidation_demo_test.go` (new, `e2e` tag) —
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
      **Disclosed deviation from the two-commit shape**: `goldenset.ConsolidationExample` already
      compiles (link 8 shipped it), so this task's own literal red cannot occur; the whole file
      (driver + all four `TestDemo_*` functions) was built and verified as one unit rather than
      split into a genuinely separate RED commit and GREEN commit, disclosed rather than presented
      as a real two-commit TDD cycle it is not — the same `m2a` C9 posture this document's own
      preamble states for a red that cannot occur. In its place: three **disclosed temporary
      probes**, run after the whole file was green, each proving one guard is genuine rather than
      vacuous — reported verbatim under 9a.4/9a.6/9a.8 below.
- [x] **9a.2** Commit 2 (GREEN): build the corpus loader reading `testdata/consolidation/cases/
      *.json` via `test/support/goldenset`, drive `CaptureService` per case, wire `fakeprovider.New`/
      `fakeprovider.NewEmbeddingFake` for every call.
      Verify: `go test -tags e2e ./test/e2e/... -run TestDemo_SimulatedWeeks_PassCompletes`.
      Requirement: spec R4.2, R4.3; design §8.1.
      **Done** — `driveDemoCorpus`/`runDemoPass` (`consolidation_demo_test.go`), green.
- [x] **9a.3** Commit 1 (RED): `consolidation_demo_test.go` (extend) — `TestDemo_ArchiveFires`: over
      the chosen `now`, at least one unit's `effective_weight` has fallen under `weight_threshold`
      (archive fires) — asserted via `DecisionLog.Since` containing ≥1 `ActionArchiveArchived` row
      (R4.4's archive clause only; the full three-effect assertion with named rationales is PR 9b's
      own scope).
      **Red**: no corpus timestamps chosen yet to cross the threshold — zero rows.
      Requirement: spec R4.4 (archive clause).
      **Disclosed, same posture as 9a.1**: the corpus's timing was chosen and tuned together with
      9a.1/9a.2, not as a separate genuinely-red step.
- [x] **9a.4** Commit 2 (GREEN, tuning): adjust the corpus's `weight`/`last_touched_at` fixture
      values until `9a.3` passes.
      Verify: `go test -tags e2e ./test/e2e/... -run TestDemo_ArchiveFires`.
      Requirement: spec R4.4.
      **Done** — the demo extends `testdata/consolidation/cases/dry-cleaning-and-ambiguous-contract-
      request.json` (PR 8's own seed case, `id` and offsets 0h/24h unchanged) with two more
      `capture_script` entries (`235h`, `239h`) and `t0` fixed in the driver
      (`consolidation_demo_test.go`'s own `demoT0`, 2026-02-01T09:00:00Z, 10 days before the case's
      own `now` 2026-02-11T09:00:00Z) — clearing JD-737's own ~6.73-day archival threshold for
      capture 0 (`classify-person-ref-ambiguous-ana`, weight 0.7, decay 0.05) with margin
      (effective weight ≈0.4246 at 10 days, JD-737's own table already confirms 7 days archives at
      ≈0.493). Capture 1 (`classify-pick-up-dry-cleaning`, weight 0.6, decay 0.1) archives even
      sooner (≈1.82 days). **Disclosed temporary probe** (proves this guard is genuine, not
      vacuous): both llm cases' `weight`/`decay_rate` were temporarily bumped to `0.99`/`0.001` (no
      decay), `TestDemo_ArchiveFires` re-run — failed, but for a richer reason than "0 archived
      rows": with captures 0/1 now staying live, they also became connect candidates, so connect's
      one live source (capture 3) found three live candidates instead of one and issued three judge
      calls against a two-entry script, failing on the third with "unscripted Complete call" before
      the archive assertion itself was ever reached — `runtime.Goexit()` inside `t.Fatalf` aborts the
      test goroutine immediately, so no later code in the test runs. Both llm case files were then
      restored via `git checkout --`, confirmed `git diff --exit-code` clean, and the four
      `TestDemo_*` tests re-run green.
- [x] **9a.5** Commit 1 (RED): `consolidation_demo_test.go` (extend) —
      `TestDemo_ConnectCandidatePairExists`: at least one candidate pair is close enough by
      `connect`'s fused ranking to reach the judge, proven via a `fakeprovider` script expecting
      exactly one `Complete` call for the pair — a broken candidate search either calls zero times or
      calls for the wrong pair, both fail the scripted guard.
      **Red**: no candidate pair reaches the judge yet — the scripted call is never made,
      `fakeprovider`'s own never-called guard fails the test.
      Requirement: spec R4.4 (connect clause).
      **Disclosed, same posture as 9a.1**. The connect judge's own scripted response is
      `"outcome":"new"` (`relation-no-match-for-dry-cleaning`, already committed) — deliberately no
      persist: this task's own text proves only "reach the judge", and a persisted relation needs
      the real, dynamically-generated target unit id, which a static pre-authored fixture cannot
      carry — left to PR 9b's own tuning scope rather than invented here (9b.2's own "if 9a's
      phases already produce the right effects" language is conditional, not a guarantee 9a owes).
- [x] **9a.6** Commit 2 (GREEN, tuning): script the fake embedding responses so `RecallService.
      ScoredFor`'s fusion surfaces the intended pair; seed `config.consolidation_last_run_at` via
      `ConfigRepo.RecordConsolidationRun` to a value before the corpus's own most-recent timestamps
      (R4.4's `MAY`, so `strengthen`'s `since` is non-`nil`).
      Verify: `go test -tags e2e ./test/e2e/... -run TestDemo_ConnectCandidatePairExists`.
      Requirement: spec R4.4.
      **Done, no embedding tuning needed**: `RecallService`'s vector leg is an unconditional top-K
      (`internal/core/recall.Search` has no similarity gate — verified by reading, not assumed), so
      any two live units are found as candidates regardless of their fake-embedding vectors; the
      corpus needed only ONE eligible connect source, not a specific pair, so the tuning surface was
      the corpus's own timing, not the embedding fake. `last_run_at` is seeded at
      `2026-02-11T06:00:00Z` — after capture 2 (`235h`, touched at `04:00`, excluded as a source but
      still a live candidate) and before capture 3 (`239h`, touched at `08:00`, the pass's one
      connect/derive source), so `SelectConnectSources` returns exactly one id. **Disclosed
      temporary probe**: `last_run_at` was temporarily pushed to `2026-02-11T08:30:00Z` (past every
      capture) — `TestDemo_ConnectCandidatePairExists` failed with "2 scripted case(s) never called"
      (zero sources, zero judge calls), exactly the stated "calls zero times... fails the scripted
      guard" property — then restored via `git checkout --`, confirmed clean, tests re-run green.
- [x] **9a.7** Commit 1 (RED): `consolidation_demo_test.go` (extend) —
      `TestDemo_DeriveBeliefExists`: at least one derivable belief exists in the corpus, proven by
      scripting the dedup-judge fake to accept exactly one derive candidate.
      **Red**: no derive candidate accepted yet — zero beliefs.
      Requirement: spec R4.4 (derive clause).
      **Disclosed, same posture as 9a.1**.
- [x] **9a.8** Commit 2 (GREEN, tuning): adjust the corpus/fake script until `9a.7` passes.
      Verify: `go test -tags e2e ./test/e2e/... -run TestDemo_DeriveBeliefExists`.
      Requirement: spec R4.4.
      **Done**: `derive`'s one source (capture 3, the same unit connect's own source is) feeds
      `taskBeliefDerivation`, scripted by a new `testdata/llm/cases/derive-team-meeting-
      preference.json` proposing one real belief (facet `goal`, topic key `team_meeting`) —
      persisted for real via `createDerivedBelief` (`MergeProposals` has zero active beliefs to
      merge against, so the decision is always CREATE), unlike connect's own deliberately-inert
      response: a derived belief carries no unit reference at all, so no real-id problem exists
      here. **Disclosed temporary probe**: the same `last_run_at` probe as 9a.6 (zero sources) also
      demonstrated derive's own guard directly — `TestDemo_DeriveBeliefExists` failed with
      `"decision_log gained 0 consolidate.derive.belief_created/consolidate.derive.belief_reinforced
      rows over 7 total, want >= 1"`, both the row-count assertion AND fakeprovider's own
      never-called guard firing together — then restored and re-verified green (same probe/restore
      pair as 9a.6, one probe proving both guards).
- [x] **9a.9** *(split checkpoint, pre-drawn per design §13)*: measure `git diff --stat` for tasks
      9a.1–9a.8 in isolation against the ~130/280 sub-estimate. If the fake-provider tuning proves
      unbounded, report it rather than resolve it — round-2 owner ruling Q5 already declined the
      per-phase-corpus alternative design §12 raised.
      **Done, flagged not split**: `git diff --stat main...HEAD`: impl+docs **0 lines** (no
      production code or docs touched — this link is test/fixture-only, well under the ~130
      estimate); test/fixture `test/e2e/consolidation_demo_test.go` (new, 347 lines) +
      `testdata/consolidation/cases/dry-cleaning-and-ambiguous-contract-request.json` (+14/-2) +
      three new `testdata/llm/cases/*.json` (8 lines each, 24 total) = 387 changed lines, ~138% of
      the ~280 test sub-estimate — consistent with this chain's own established 106%-193% band
      (PR1-PR8), reported per the round-2 owner ruling on Q5 rather than resolved by narrowing the
      demo. Fake-provider tuning did NOT prove unbounded: the vector leg's unconditional top-K
      (9a.6's own finding) meant no embedding-response tuning was needed at all, only corpus timing
      — the actual variance driver this link hit was the capture-time relation_evaluation calls
      every capture past the first triggers (not named by design §8.1/§12 Q5 explicitly), handled
      once via `driveDemoCorpus`'s own interleaved script rather than per-test tuning.
- [x] **9a.10** `golangci-lint run`.
      **Done** — `make lint` → `0 issues.`
- [x] Verify (PR-level): `make check-all` incl. the `e2e` suite; diff scope — `test/e2e/
      consolidation_demo_test.go` (new), `testdata/consolidation/cases/` (corpus content growth
      only — the format/schema itself is PR 8's own scope, unchanged here). Target ≤130 impl+docs
      lines.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/demo-simulated-weeks` returns
      nothing after merge.
      **Chain-merge check 2**: `gh pr view <PR9b> --json baseRefName` names `main`.

      **PR 9a result** (`feat/demo-simulated-weeks`): `make check-all` green end to end;
      `internal/core` coverage floor unchanged at 750/750 (100%) — this PR adds no `internal/core`
      code. Diff scope matched the declared set exactly, plus three new `testdata/llm/cases/*.json`
      fixtures the declared scope's own prose did not separately name but design §8.1 anticipates
      (capture-time and pass-time provider responses) — disclosed rather than silently added.
      Impl+docs 0 lines (well under ~130). Test/fixture 387 lines (~138% of ~280, task 9a.9's own
      flagged-not-split conclusion). `go test -tags e2e ./test/e2e/... -count=1 -timeout 20m`: green,
      133.371s wall-clock — essentially unchanged from link 7's own ~134s baseline (this link's four
      new tests run in well under 1s combined; no real timer, signal, or subprocess wait, unlike the
      SIGTERM suite). Chain-merge checks deferred to actual merge time (PR not yet merged as of this
      apply batch), same posture every earlier link in this chain took.

**Judgment Day correction round, `feat/demo-simulated-weeks` (PR #189), frozen target `5990ffd`.
One confirmed finding (JD-9a-01, CRITICAL by Judge A, WARNING by Judge B, both confirmed and
orchestrator-verified), fixed in this same branch.**

- **JD-9a-01.** `demoT0`'s own doc comment (`consolidation_demo_test.go`) claimed
  `TestDemo_ArchiveFires` was "MECHANICAL, not merely a comment" and protected capture 0's own
  ~6.73-day archival threshold. It did not: the assertion counted `decision_log` rows
  (`archived == 0`) instead of checking the case's own declared `expected.archived` indices
  (`[0, 1]`), so it only required "at least one" capture to archive — capture 1's own weaker
  ~2.823-day threshold, not capture 0's ~6.73-day one the comment described. Verified: at
  `demoT0 = now - 3 days`, capture 0 sits at effective weight ~0.6025 (does not archive) while
  capture 1 sits at ~0.4912 (archives), so the old assertion PASSED even though the case's own
  `expected.archived` says both indices must archive; the comment's claimed ~3.27-day margin was
  actually ~7.18 days on the assertion as shipped. Further, a close-`demoT0` perturbation does not
  always fail on this file's own archive assertion at all: `SelectConnectSources`' `since` filter
  gates only which units are considered connect sources, never the candidate search itself
  (`connectPairsForSource` -> `RecallService.ScoredFor`, whose only liveness filter is
  `LiveByIDs`), so a capture that stays live because it missed the archive threshold becomes an
  extra connect candidate and can drive more judge calls than `runDemoPass`'s fixed two-entry
  `passJudge` script allows — `fakeprovider.Complete`'s own `t.Fatalf` then fires INSIDE
  `Consolidate()` itself, and `runtime.Goexit()` aborts the test goroutine before the archive
  assertion ever runs.
  **Fix**: `TestDemo_ArchiveFires` now reads `expected.archived` from the loaded case and requires
  a matching `ActionArchiveArchived` row (its `Context`'s own `UnitID`, decoded via
  `encoding/json`) for each declared index, rather than counting rows and comparing to zero. With
  that fix, the BINDING constraint (the harder of the two thresholds to satisfy, since both
  indices must now archive) really is capture 0's own ~6.73-day math — restoring the mechanism the
  comment always intended. `demoT0`'s own doc comment was corrected in place (not silently
  rewritten) to state what was wrong, why, the real margin, and the connect-script-exhaustion
  failure mode. `demoT0`/`now` themselves are unchanged — both judges verified the shipped values
  arithmetically satisfy `expected.archived: [0, 1]` and `expected.beliefs: [3]`. No production
  code touched (`internal/core` coverage floor unchanged at 750/750, 100%); no `database/sql` or
  raw SQL added (`test/e2e/**` stays outside `sqlite-containment`'s exception list); every judge
  call stays on `test/support/fakeprovider`. This round intentionally does not widen the demo's
  assertions to a full three-effect check with named rationales — that is PR 9b's own scope; only
  9a's own archive clause was strengthened to match its own case data.
  **Disclosed probe** (proves the strengthened guard genuinely discriminates, not vacuously):
  `demoT0` was temporarily moved to `now - 3 days` (`2026-02-08T09:00:00Z`) and
  `TestDemo_ArchiveFires` re-run. It did **not** fail cleanly on the archive assertion — it aborted
  earlier, exactly as the corrected comment now documents:
  ```
  consolidate.go:473: fakeprovider: unscripted Complete call (task "relation_evaluation", ...);
  no scripted case ids remain
  --- FAIL: TestDemo_ArchiveFires (0.02s)
  ```
  `demoT0` was then restored byte-identical from an explicit pre-probe copy (not `git checkout --`,
  per this branch's own key learning #5: that command has no committed baseline for this file on
  this branch and would revert to the parent branch's state, not to the prior edit) — confirmed
  identical via `diff`, and the file's real fix content preserved. All four `TestDemo_*` tests
  re-run green.
  **Verify**: `go test -tags e2e -count=1 -v ./test/e2e/...` → all green, `test/e2e` package
  wall-clock 134.038s (baseline 133.371s — the SIGTERM suite still dominates; the four `TestDemo_*`
  tests themselves run in well under 1s combined, unchanged). `make check-all` green;
  `internal/core` coverage unchanged at 750/750 (100%).
  **Rollback boundary**: `test/e2e/consolidation_demo_test.go`'s `demoT0` doc comment and
  `TestDemo_ArchiveFires`'s body (plus the new `encoding/json` import) — revertible independently
  of any other PR 9a change; no other file touched.

---

## PR 9b — `feat/demo-decision-log-assertions` (pre-drawn split of 9a) (~40 impl+docs / ~180 test)

Depends on PR 9a. Ships R4.5's own bar — `decision_log` alone tells the story — and, per R4.6,
**this is `m2d`'s own exit criterion**: no later PR owes anything toward it.

- [x] **9b.1** Commit 1 (RED): `test/e2e/consolidation_demo_test.go` (extend) —
      `TestDemo_DecisionLogTellsTheStory`: extends `9a`'s pass fixture to assert, via `DecisionLog.
      Since` only (never re-deriving from `units`/`relations`/`self_beliefs` directly), at least one
      legible row for each of `ActionArchiveArchived`, `ActionConnectRelationPersisted`, and
      (`ActionDeriveBeliefCreated` or `ActionDeriveBeliefReinforced`) — each `Rationale` string
      present and naming the specific unit/relation/belief the fixture expects, not merely that a
      row of the right `Action` exists somewhere.
      **Red**: `9a`'s own fixtures only assert row *existence*, not rationale content naming the
      specific item — fails on the strengthened assertion.
      Requirement: spec R4.5.
      **Done** (commit `22f7730`) — observed failing for exactly the stated reason:
      ```
      decision_log has no consolidate.connect.relation_persisted row whose Rationale names both
      unit demo-id-0008 (capture_script[3]) and unit demo-id-0006 (capture_script[2]) — spec
      R4.5's connect clause
      ```
      The archive and derive clauses passed unmodified at this same commit — the red is
      specifically 9a's own disclosed connect boundary, not a defect anywhere else. Also adds
      `expected.relations_created: [[3, 2]]` to the corpus (already documented by `format.md`,
      unused until now) — fixture data, not a fix.
- [x] **9b.2** Commit 2 (GREEN, mostly tuning, no production code expected): if `9a`'s three phases
      already produce the right effects, no `internal/brain`/`internal/core` change is needed — only
      the corpus's content (distinctive unit text) is tuned until each `Rationale` string uniquely
      identifies the expected unit/relation/belief.
      Verify: `go test -tags e2e ./test/e2e/... -run TestDemo_DecisionLogTellsTheStory`.
      Requirement: spec R4.5.
      **Done** (commit `24417fb`), **zero production code touched, confirmed** — solved 9a's own
      disclosed "id problem" (a persisted `ActionConnectRelationPersisted` row needs connect's
      judge to answer with the real, run-time `target_unit_id`
      `consolidate.go`'s own `judgeAndPersistPair`/`ProposeRelation` trusts verbatim — never
      cross-checked against the candidate `RecallService` actually found) entirely inside
      `test/e2e/consolidation_demo_test.go`: a new `connectJudgeCase` helper reads the real id
      straight out of `dv.unitIDs` (deterministic — one shared, monotonic counter, no wall clock,
      no map-iteration dependency on this path, the same property `dv.unitIDs` itself already
      relies on) using the corpus's own `expected.relations_created` pair, and writes ONE fresh
      case file per test run into a `t.TempDir()` (never committed, self-correcting if a future
      corpus edit shifts which indices are involved) alongside an unchanged copy of derive's own
      static case. `fakeprovider.New` still does every bit of the actual replaying (CLAUDE.md
      non-negotiable #5) — this only supplies the one value a checked-in, pre-authored fixture
      structurally cannot carry. No change to `test/support/fakeprovider` or `test/support/
      goldenset` — both stay exactly as 9a/PR8 shipped them.
      **Disclosed temporary probe** (proves the connect assertion is genuinely reachable, not
      vacuous — a different failure mode than the natural 9b.1 red above): `connectJudgeCase`'s
      response was temporarily built with `sourceUnitID` in place of `targetUnitID` (a wrong id on
      purpose). `TestDemo_DecisionLogTellsTheStory` failed on exactly the connect clause again:
      ```
      decision_log has no consolidate.connect.relation_persisted row whose Rationale names both
      unit demo-id-0008 (capture_script[3]) and unit demo-id-0006 (capture_script[2]) — spec
      R4.5's connect clause
      ```
      Restored from an explicit pre-probe copy (not `git checkout --`, this branch's own key
      learning — see 9a's own Judgment Day record for why), confirmed `diff` clean, all five
      `TestDemo_*` tests re-run green afterward.
- [x] **9b.3** R4.6 discharge, verification not a new test: confirm this is the PR where `docs/
      05-build-plan.md` §M2's demo bullet and the umbrella proposal's own final success-criteria
      bullet (§2) may be checked off — no later PR in this chain, and none in the wider M2 chain,
      owes anything further toward either.
      Requirement: spec R4.6.
      **Confirmed.** `fd -d 1 -t d . openspec/changes | rg m2` names exactly four M2 sub-changes:
      `m2a-weight-focus`, `m2b-consolidation-core`, `m2c-consolidation-runtime`,
      `m2d-scheduler-demo` — the whole of the umbrella `m2-sleep-weight` proposal's own PR split
      (proposal.md's own "sharing this proposal and this scope" framing). `rg -c '^\s*- \[ \]'
      openspec/changes/{m2a,m2b,m2c}*/tasks.md` returns **zero** unchecked boxes in all three —
      only `m2d-scheduler-demo/tasks.md` still has open items, and after this batch they are
      exactly the "## X — Chain-wide verifications" section (verification-only, not new work owed
      toward R4.5/R4.6) plus the "Handoffs `m2d` leaves open" section (explicitly deferred to M3
      by design §14/owner ruling round 2 Q3, never owed by `m2d`). `m2d`'s own chain has no PR
      after 9b (design §13's own table ends at row 9b; this document's own opening line names 9b
      as the exit criterion). **This is therefore the PR** — `docs/05-build-plan.md` §M2's demo
      bullet and the umbrella proposal's §2 final bullet ("a vault seeded with simulated weeks of
      data, run through `nooma consolidate` — cold things get archived, related things get
      connected, beliefs get derived, and `decision_log` tells the story end to end") may both be
      checked off once this PR merges, and no later PR anywhere in M2 owes anything further toward
      either. **Doc edits themselves are NOT made in this apply batch** — landing them in this PR
      or immediately after is the orchestrator's own call, per this task's own instruction not to
      make that unilaterally.
- [x] **9b.4** `golangci-lint run`.
      **Done** — `make lint` → `0 issues.` (also re-confirmed inside `make check-all`'s own lint
      step below).
- [x] Verify (PR-level): `make check-all` incl. the `e2e` suite; diff scope — `test/e2e/
      consolidation_demo_test.go` (extended only, no new file). Target ≤40 impl+docs lines. **This
      is `m2d`'s own exit criterion (R4.6)** — after this PR merges, `docs/05-build-plan.md`'s M2
      demo bullet and the umbrella proposal's final success criterion get checked off in the same PR
      or immediately after.
      **Chain-merge check 1**: `git ls-remote --heads origin feat/demo-decision-log-assertions`
      returns nothing after merge.
      **Chain-merge check 2**: this is the chain's last link — no next PR to check a base against;
      confirm `main`'s own `git log --oneline -11` (wider if any undrawn split fired) shows every
      link merged in order instead.

      **PR 9b result** (`feat/demo-decision-log-assertions`): `make check-all` green end to end;
      `internal/core` coverage floor unchanged at 750/750 (100%) — this PR adds no `internal/core`
      code, no production code of any kind. Diff scope matched exactly: `test/e2e/
      consolidation_demo_test.go` (extended only, no new file, `git diff --numstat` +203/-9 = 212
      changed lines), `testdata/consolidation/cases/dry-cleaning-and-ambiguous-contract-request.json`
      (+1/-0, one new `expected.relations_created` field already documented by `format.md`) — no
      strays, no new file anywhere. Impl+docs **0 lines** (well under the ~40 target — this link is
      test/fixture-only, same posture 9a took). Test/fixture 213 changed lines (~118% of the ~180
      estimate), inside this chain's own established 106%-193% band (PR1-PR9a), not split.
      `go test -tags e2e ./test/e2e/... -count=1 -timeout 20m`: green, **134.045s** wall-clock —
      essentially unchanged from 9a's own 134.038s (this link's one new test runs in well under a
      second; no new timer, signal, or subprocess wait). Chain-merge checks deferred to actual
      merge time (PR not yet merged as of this apply batch), same posture every earlier link in
      this chain took.

      **Judgment Day scoped correction round** (PR #190, target `8d8c76d`, branch
      `feat/demo-decision-log-assertions`) — two owner-authorized ledger IDs, both fix-agent-
      applied, test-only, no production code:
      - **JD-9b-01** (Judge B): `TestDemo_DecisionLogTellsTheStory`'s archive clause looped over
        `ex.Expected.Archived` with no guard, unlike its connect and derive siblings, so
        `expected.archived: []` would report PASS with zero rows checked — an undisclosed
        asymmetry, not exploitable on this corpus (`[0,1]` is non-empty) but the exact
        vacuous-pass-on-empty class this chain has caught throughout. **Fix**: added the same
        `len(ex.Expected.Archived) == 0 { t.Fatalf(...) }` guard the connect and derive clauses
        already had, worded consistently. No natural red exists for it on this corpus; a disclosed
        probe was not additionally run since the guard's own logic is identical in shape to the two
        already-proven siblings.
      - **JD-9b-02** (Judge A: SUGGESTION, Judge B: WARNING, both confirmed): the connect clause's
        `anyRationaleNames(connectRows, sourceID, targetID)` could not distinguish "the real
        candidate search found the expected target" from "the fixture's own scripted judge answer
        named it" — `judgeAndPersistPair` persists `rel.ToUnitID` from the judge's own
        `TargetUnitID` verbatim (`internal/brain/consolidate.go`, `ProposeRelation`), never
        cross-checked against the candidate `RecallService` actually returned; that verbatim trust
        is pre-existing and stays out of this PR's scope, untouched. **Fix**: added a second,
        clearly separated assertion (own comment block, distinct from the `DecisionLog`-only
        checks above it) using `fakeprovider.Fake.SeenPrompts()` — already exported, unmodified —
        to verify the prompt actually sent to the connect judge names `demoConnectTargetContent`
        ("Draft the team meeting agenda", capture_script[2]'s own `normalized_content` from
        `testdata/llm/cases/classify-prepare-meeting-agenda.json`), a substring none of the
        corpus's other three units' own `normalized_content` share
        ("Send Ana the contract", "Pick up the dry cleaning", "Schedule the team meeting for
        Monday"), so it can only name capture_script[2] in this corpus. `runDemoPass` was split
        into `buildDemoPass` (construction) + `runDemoPass` (wraps it, discards `passJudge`,
        unchanged signature and behavior) so `TestDemo_DecisionLogTellsTheStory` alone can keep
        `passJudge` for this check — the other four `TestDemo_*` tests were not touched.
        **Disclosed probe**: temporarily changed `demoConnectTargetContent` to a string absent from
        every rendered prompt; `TestDemo_DecisionLogTellsTheStory` failed cleanly at the new
        assertion's own `t.Fatalf` ("no prompt sent to the connect judge names ... — the candidate
        search never presented the expected target ..."), not pre-empted by any earlier assertion.
        Restored from an explicit pre-probe copy (not `git checkout --`, this branch's own key
        learning), confirmed `git diff --exit-code` clean, full suite re-run green.
      - **Evidence**: `go test -tags e2e -count=1 ./test/e2e/...` green at **134.066s** (and
        **134.411s** on an earlier run of this same round) — both within the ~134s baseline, no
        regression. `make check-all` green end to end (lint 0 issues, L1-L3, schema-golden diff
        clean, 7-target cross-compile matrix OK, `internal/core` coverage unchanged at 750/750,
        L4 e2e **134.261s**). Diff scope: `test/e2e/consolidation_demo_test.go` only (extended, no
        new file); `openspec/changes/m2d-scheduler-demo/tasks.md` (this record).
      - **Rollback boundary**: each ledger ID is one independent, reversible edit inside
        `TestDemo_DecisionLogTellsTheStory` and its two small helpers (`buildDemoPass`,
        `demoConnectTargetContent`) — JD-9b-01's guard and JD-9b-02's `SeenPrompts` block do not
        depend on each other and can each be reverted alone without touching the other.
      - Not merged: scoped re-judgment runs first, per this chain's own two-round Judgment Day
        budget; this is the chain's last link.

  **JD-9b-03 (scoped re-judgment, one finding from each judge, same family)**: the `SeenPrompts`
  proof added for JD-9b-02 was correct on this corpus but rested on properties the assertion did not
  itself enforce. Judge A: the loop had no `req.Task` filter, and `passJudge` receives both connect's
  `relation_evaluation` and derive's `belief_derivation` calls — a match on content alone could in
  principle be satisfied by a derive prompt; it is not today only because `SelectConnectSources`'
  `since` window (capture_script[2] at `2026-02-11T04:00Z`, `last_run_at` at `06:00Z`) excludes that
  unit from derive's sources. Judge B: `demoConnectTargetContent` was a hardcoded literal tied to
  capture_script[2], while `connectJudgeCase` one function away derives its target from
  `ex.Expected.RelationsCreated[0]` — a corpus edit repointing the declared pair would decouple the
  anchor from the real target.

  **Fix (orchestrator, one change closing both)**: anchor on the composite `JudgePrompt` renders for
  a candidate — `"  " + c.ID + ": " + c.Content` (`internal/brain/capture.go:521-524`) — with the id
  half derived from `ex.Expected.RelationsCreated[0]`. Only a candidate line carries `<id>: <content>`
  (the source unit is rendered as bare content with no id prefix, and `BuildDerivePrompt` renders
  sources, never candidates), so a derive prompt can no longer satisfy it; and a future edit
  repointing the declared pair moves the id half and produces a loud failure instead of a silent
  correlation against the wrong unit.

  **Disclosed probe**: `" PROBE-ABSENT"` appended to the composite so no rendered prompt could
  contain it. `TestDemo_DecisionLogTellsTheStory` failed cleanly at the new assertion's own
  `t.Fatalf`, not pre-empted:
  ```
  no prompt sent to a judge renders "demo-id-0006: Draft the team meeting agenda PROBE-ABSENT" as a
  candidate line — connect's candidate search never presented capture_script[2] to the judge ...
  ```
  The message carries the real run-time id (`demo-id-0006`), confirming the id half is genuinely
  derived rather than literal. Restored from an explicit `cp` backup (not `git checkout --`);
  `diff` reported byte-identical.

  **Verify**: `go test -tags e2e -count=1 ./test/e2e/...` green, 134.072s (baseline ~134s);
  `make lint` 0 issues. No production code touched.
  **Rollback boundary**: the `wantCandidateLine` composite and its comment in
  `test/e2e/consolidation_demo_test.go` — one commit, independently revertible.

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

