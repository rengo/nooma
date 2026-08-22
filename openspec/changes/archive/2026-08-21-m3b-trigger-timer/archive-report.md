# Archive Report — m3b-trigger-timer

**Change**: m3b-trigger-timer (second of M3's chained changes: m3a → **m3b** → m3c → m3d)
**Date Archived**: 2026-08-21
**Mode**: hybrid (openspec files + Engram observations)
**Status**: Complete — all 9 implementation PRs merged, every task checked, the one open owner
question ruled on and the spec amended
**Main Branch HEAD at Archive**: 49910d9 (merge of PR #212, the verification PR)

---

## Artifacts Archived

Moved to `openspec/changes/archive/2026-08-21-m3b-trigger-timer/`:

- `spec.md` — Requirements R0–R6.1, **R4.2 amended 2026-08-21** by owner ruling on finding G21
- `design.md` — §1–§12 plus the F1 and G1 reconciliation notes and the 2026-08-21 owner decisions
- `tasks.md` — the eight-PR task list (shipped as nine), every task checked, findings G1–G22, and
  the verification pass
- `archive-report.md` — this report

## What shipped

`triggers` and `timers` stopped being tables M0 created and nothing used. The binary now arms a
timer or a trigger at capture, scans for what has come due, expires what is past its window, fires
what is not, and records every one of those decisions where a person can read them.

| # | PR | Branch | Merge |
|---|---|---|---|
| 1 | [#203](https://github.com/rengo/nooma/pull/203) | `feat/ports-trigger-timer` | `13f9c0c` |
| 2a | [#204](https://github.com/rengo/nooma/pull/204) | `feat/store-trigger` | `1a66454` |
| 2b | [#205](https://github.com/rengo/nooma/pull/205) | `feat/store-timer` | `6cf40af` |
| 3 | [#206](https://github.com/rengo/nooma/pull/206) | `feat/ports-store-focus-candidates` | `26f343c` |
| 4a | [#207](https://github.com/rengo/nooma/pull/207) | `feat/brain-arm-at-capture` | `ab9ca3f` |
| 4b | [#208](https://github.com/rengo/nooma/pull/208) | `feat/brain-arm-refusal-audit` | `40bce21` |
| 5a | [#209](https://github.com/rengo/nooma/pull/209) | `feat/brain-due-scan` | `7f879e2` |
| 5b | [#210](https://github.com/rengo/nooma/pull/210) | `feat/brain-due-scan-conflict` | `ad63017` |
| 6 | [#211](https://github.com/rengo/nooma/pull/211) | `feat/cli-check` | `3d6e404` |
| — | [#213](https://github.com/rengo/nooma/pull/213) | `fix/check-tests-quiet-hours` | `b6df754` |
| — | [#212](https://github.com/rengo/nooma/pull/212) | `docs/m3b-verification` | `49910d9` |

Design §7 planned eight. The ninth is PR 2's own measured split (**G8**), not the trigger/timer cut
design had pre-drawn as a contingency for PR 5a — that one was measured and **not** taken (**G15**).

**No migration.** `triggers` and `timers` are unchanged since `0001_core_tables.sql`, and
`idx_triggers_status_fire` was already the index `Due` needs. `store_api.golden` widened by thirteen
reviewed lines; `docs/03-data-model.md` is untouched.

## Verification Gate: PASS

`make check-all` green on `main` end to end — lint, `go vet`, L1/L2 under `-race -shuffle=on`, build,
L3, the schema-golden regeneration diff, `internal/core` coverage at **100%** against a 90% floor,
the seven-target cross-compile matrix, and L4 at ~133s on ubuntu-latest and windows-latest.

Two claims were verified by **mutation** rather than by assertion, because assertion alone could not
have distinguished a working guard from an absent one:

- **R5.1's single-clock-read gate** — a second `Now()` inserted into `check.go` fails
  `brain_single_clock_read_test.go` with the file named.
- **5a's status-vocabulary constraint** — mapping `VerdictStale` to `"stale"` leaves every L2 sweep
  green and fails only `test/integration/due_scan_status_vocabulary_test.go`, exactly as design
  predicted. That test is the `CHECK` constraint the schema does not carry.

## Task Completion Gate: PASS

Every task in `tasks.md` checked. Strict TDD held across all nine PRs: in each, the conformance/L1
test commit is strictly ahead of the implementation commit. Two tests were folded into an
implementation commit rather than given a red of their own (`interrupt_test.go` and I18's
persistence half), and both are **disclosed in the commit message** rather than passed off as
red-first.

---

## Findings — G1 through G22

Twenty-two disagreements, gaps and defects were recorded rather than papered over. `tasks.md` carries
all of them in full. What follows is what a future reader needs.

### The three that were owner-facing

**G12 — an action a vault may already hold was renamed.** Design §3.5 said to delete
`ports.ActionCaptureHookDeferred` because "a vocabulary member with no producer is a bucket the glass
box can never show". It had **two** producers, not one: besides the retired timer refusal,
`recordAmbiguousPersonRefDecision` wrote the same action for an unrelated fact, the two told apart
only by `context.kind` — which that function's own doc comment already confessed to. Owner ruling R3
(delete, not keep read-only) was honoured on a better reason: once the timer refusal was gone, the
name described nothing left standing. The surviving producer got
`ports.ActionCapturePersonRefAmbiguous` (`"capture.person_ref.ambiguous"`). Historical rows still
read back — `DecisionLog.Since` casts with a plain `ports.DecisionAction` and applies no vocabulary
gate.

**G14 — PR 4a shipped a defect to `main`, and PR 4b's own sweep found it.** Retiring
`timerHookRefusal` removed the fork that caught every timer regardless of date; the arming fork only
intercepts when `Arm` returns a plan. An **undated or already-past timer fell through to
`classify.ToUnit`**, which refuses to build a unit out of one (I04), and the whole capture failed
with `capture: build unit: classify: this classification persists no unit`. A user typing "remind me
to call the dentist" with no time got a 500. Neither spec nor design named an outcome for that case,
and the `OutcomeDeferred` that used to answer it had been deleted by the same PR. Fixed with
`brain.OutcomeArmRefused` + `brain.ArmRefused{Why, Message}` — a distinct outcome, not a discard,
because "nothing worth keeping" and "you asked for a reminder and I could not set it" are different
facts.

**G21 — spec R4.2's first MUST was knowingly unmet, and the owner ruled.** R4.2 asked for a
`decision_log` row on every refused arming, all four `Refusal` members, `chitchat` included. Design
§3.5 rejected that for two of the four with reasons (audit noise; already recorded by
`ActionCaptureUnclassifiable`), task 4b.1 encoded the design's version, and the design's version
shipped. **Owner ruling 2026-08-21: amend R4.2** to the derived rule `docs/02-cognitive-core.md` §11
now carries — *a refusal is recorded exactly when the capture would otherwise leave no trace at all*.
The amended requirement carries a note naming what it originally said and why it changed. No code
moved.

### The defect this change introduced and fixed itself

**G22 — two time-of-day-dependent tests, found by CI at 01:45 UTC.** PR 6's CLI dry-run test and both
L4 demos seed a stale trigger against the **real system clock** — the shipped binary reads it and no
test can inject one, which is the point of running at that layer — then asserted "expired 1
trigger(s)" unconditionally. False for seven hours a day: it is **G16 from the other side**. Written
in the afternoon, green; run by CI overnight, red. Fixed in PR #213 by **inverting rather than
skipping** — inside the window the tests now assert the trigger must *not* have expired — with both
branches exercised against real timezones (`TZ=UTC` at 01h, `TZ=Asia/Tokyo` at 10h).

**The lesson, and it is the report's most useful line: G16 was recorded as a finding and still bit
twice.** A documented interaction is not a guarded one. The second bite landed in the one place the
fixtures read a wall clock instead of a fake, where no sweep is possible because the binary owns the
clock.

### Behaviour no artifact had stated

**G16 — an overdue trigger inside quiet hours is deferred, not expired.** `verdict` evaluates quiet
hours *before* staleness deliberately ("an item is never declared stale during a window in which it
was refused delivery"), so a trigger six hours overdue at 06:00 returns `VerdictDefer`, writes
nothing, stays `armed`, and expires on the first pass after the window ends. Neither spec R5.3,
design §3.3 nor doc 02 §7 stated the interaction — each described I15 and I16 separately. Found by
**sweeping the whole overdue window rather than sampling it**; task 5a.4's own text would have been
false as literally written. Doc 02 §7 now states it.

**G6 — the contract PR 1 shipped could not run at L3.** `triggers.unit_id REFERENCES units(id)` and
the vault opens `foreign_keys=on`, so every contract case failed the moment the SQLite repository
stopped being a no-op. L2 was green and silent because the fake enforces no FK. Neither spec nor
design names the FK. Resolved with the house pattern that already existed for exactly this —
`repocontract.TriggerHarness`, copied from `EmbeddingHarness`, **whose own doc comment names this
failure mode in advance**: *"without this hook the suite would pass at L2 and be impossible to run at
L3 — which is not a contract, it is a fake's opinion."*

**G17 — the L3 race test passed for the wrong reason.** Written as design §3.6 describes it — two
goroutines, one vault, `-race` — the first pass finished before the second read, no conflict
occurred, and every assertion held. **Green, and proving nothing.** A race test that depends on the
scheduler is flaky; this one was flaky *in the direction of always passing*, which is the kind nobody
notices. Fixed with a barrier that orders the **reads only**, so both `UPDATE`s still race genuinely.
The barrier carries a timeout for a second-order reason: before the conflict arm existed, the losing
pass aborted and never reached the timer read, so an untimed barrier would have **hung** the red step
instead of failing it.

### Storage facts pinned by observation, not assumption

**G2 / §3.4 — what SQLite does with a non-finite REAL.** **NaN bound as a parameter is stored as SQL
NULL** (`typeof` = `null`), so a NaN `interrupt_level` and a degraded one are indistinguishable once
written — NaN can never reach `ResolveInterrupt` as a number, while **±Inf can**: both survive as
REAL and read back verbatim. Non-numeric TEXT stays TEXT (REAL affinity does not coerce it), which is
why `interruptLevelFrom` type-switches on the raw driver value: a `sql.NullFloat64` scan fails with
`database/sql`'s conversion message, naming neither the column nor the row.

**G7 — `triggers.payload`'s keys** were unspecified beyond `lead_days`; pinned to migration
`0001:48`'s own comment — `action`, `rationale`, `lead_days`. Asserted against the **stored bytes**,
as is `recurrence_anchor`'s `{"month":9,"day":4}`, because `json.Unmarshal` is case-insensitive on
read and a struct round trip would pass against either encoding and prove nothing.

### Artifact disagreements resolved on their merits

| # | Disagreement | Resolution |
|---|---|---|
| **G1** | Design §3.6's pipeline diagram stale after its own F1 correction | F1's favour — four due-scan actions, not five |
| **G2** | Spec R1.2 wants `rendered_text`/`surfaced_at` parameters; design ships `Fire(ctx, id, at)` | Design's favour — no parameter with no caller |
| **G3** | Spec R0 forbids `internal/core` changes; design ships `AllVerdicts()` there | Design's favour — a completeness accessor states no new decision |
| **G4** | Task 1.3 puts vocabulary tests in `repocontract`; design §8 puts them in `test/conformance` | Design's favour — they are properties of `internal/ports`, not of an implementation |
| **G5** | Design says the I03 scan "stays scoped to `ports.UnitRepo`"; it has swept seven interfaces since m2c | Both new ports added — the list's own claim is "every ports repository interface" |
| **G9** | Spec R3.1 wants `LiveFocusCandidates(ctx, ids)`; design §3.7's snippet drops the parameter | **Spec's favour, and not by precedence** — the unparameterised form opens a new *unbounded* read; the id-set form mirrors the already-reviewed `LiveByIDs` |
| **G10** | "Five fields" in spec R3.1 and task 3.1; `focus.Candidate` has seven | All seven asserted |
| **G18** | Task 5b.3 says the race fixture is a trigger; design §3.6 says a timer | **Both raced** — resolves it by covering rather than picking, and the arm lives in two separate loops |
| **G19** | Task 6.4 changes a signature PR 5a shipped | Done as the task asks, recorded as PR 6's own scope addition |
| **G20** | Task 6.5 asks for a scan over "this PR's diff" | A Go test reads the tree, not a diff — the strongest true version ships, with its limit in its own doc comment |

### Sizing decisions, both measured rather than felt

**G8 — PR 2 was split.** It measured 463 implementation-and-docs lines against a ~250 budget and a
400 ceiling. Design §7 had already set that posture for PR 5a ("apply that cut if its own forecast
exceeds 400"); the same rule against the same number gives the same answer. Both halves stood alone.

**G13 — PR 4a took `size:exception` instead.** It measured 431, and design §3.5's own rule ("the
retirement becomes its own PR if it exceeds ~150 implementation lines") fired at a measured 153. The
cut was still wrong, for a reason the artifact could not see: **the refusal was the only thing routing
a `timer` away from `classify.ToUnit`**, so a retirement-only PR on `main` leaves every timer capture
failing until the arming PR lands. Under `stacked-to-main` that is a broken binary on `main` for the
length of a review. The mandated three-commit order was preserved inside the one PR.

**G15 — the pre-drawn ninth PR was not taken**, and that was reported rather than skipped. PR 5a
measured 254 against its ~340 budget: the runner is smaller than budgeted precisely because it
delegates every decision — no threshold, window or boundary lives in `internal/brain/check.go`.

---

## Method notes worth carrying to m3c

**Sweeps over `AllX()` vocabularies beat hand-written tables, twice over.** The refusal-audit sweep
(78 cells: `classify.AllKinds()` × date shapes × rule shapes) found G14, which a four-row table — the
shape design §3.5 explicitly warned against — would have missed, because every cell someone would
have thought to write armed successfully. The I15 window sweep found G16 for the same reason. Where a
vocabulary can be iterated, iterate it: a fourteenth member then fails the test instead of slipping
past it.

**Assertions that cannot enumerate what they check are stronger than ones that can.** The refusal
sweep never calls `prospection.Arm` and never lists which refusals qualify: it collects all 78 cells,
derives *which Kinds can arm* from whether they armed anywhere, and asserts a biconditional.

**Name what a test cannot prove, in the test.** Three limits are written into doc comments rather
than left to be discovered: the port contracts cannot observe "the row is unchanged" after a refused
transition (no any-status read exists), no fixture built from today's four-status vocabulary can
distinguish I02's positive filter from an exclusion list, and the L4 scope scan reads the tree rather
than a diff.

**Prove falsifiability by mutation when the claim is about a guard.** Done for the single-clock-read
gate and the status-vocabulary constraint; not done for G22's fixtures, which is how they shipped
broken.

---

## Final State

- **`internal/ports`**: `TriggerRepo`, `TimerRepo`, three vocabularies with `AllX()`,
  `UnitRepo.LiveFocusCandidates`. `AllDecisionActions()` = **32**.
- **`internal/brain`**: arming at capture, `CheckService`/`checkRunner`, `interruptColumn`.
  `AllCaptureOutcomes()` = **7** (`stored | armed | arm_refused | discarded | recalled | corrected |
  asked`).
- **`cmd/nooma`**: `nooma check [vault]` with `--dry-run`.
- **Docs**: `docs/02-cognitive-core.md` §5, §7, §8 and §11 amended; `docs/01-architecture.md` gains
  one command-table row.

**Exit criterion discharged**: `test/e2e/check_demo_test.go` runs the compiled binary against a real
migrated vault, fires a due timer, expires an overdue trigger outside quiet hours, and reads
`decision_log` back to confirm each transition carries a sentence a person can read.

**Next**: `m3c` — the Telegram transport. `m3b` opened no channel and speaks to no network, which is
asserted structurally rather than promised: no file under `internal/**` mentions the Telegram API
host or the two methods ADR-0014 names, and `internal/channels` still holds only its package doc.
