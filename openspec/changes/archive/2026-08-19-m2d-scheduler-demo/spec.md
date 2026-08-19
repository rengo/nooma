# Spec — M2d: scheduler and demo

Delta specification for `m2d-scheduler-demo`, the fourth and last of four chained changes
splitting `openspec/changes/m2-sleep-weight/proposal.md` (owner ruling round 2 #6). States what
MUST be true of the repository after this change is applied, in testable form — not how
(`design.md`'s job). Where the documents leave a real choice unmade, this document says so in
§7 rather than inventing an answer.

**Status: written against `m2c`'s shipped surface**, not a concurrent draft. `m2c` closed with
`brain.ConsolidateService` real, `nooma consolidate` real, and every port this change needs
already declared. `internal/scheduler` today is `doc.go` alone (package comment only) — every
file this document names is new.

Sources: `openspec/changes/m2-sleep-weight/proposal.md` §3.2 item 7, §4.1 (placement reasoning),
§4.5 (one instant per pass), §5 (m2d row), §5.1 (m2d's four PRs), §7 R9; `docs/adr/0009-scheduler-
downtime.md` (Accepted, in force, not edited — non-negotiable #2); `docs/02-cognitive-core.md` §5
("NUMBERS are calibratable defaults... see §13"), §6 (the eight phases), §7 (prospection,
out of scope here), §13 (the calibration table's existing, code-less rows for
`boot_consolidation_delay`/`trigger_staleness_hours`/`timer_staleness_hours`/the 03:00 cadence);
`docs/05-build-plan.md` §M2's own demo bullet; `docs/06-harness.md` §1 (the dependency rule) and
§2 (the injected-clock argument, naming the M2 demo by name); `openspec/changes/m2c-consolidation-
runtime/spec.md` §8 (the boundary this document picks up) and R5.4 (the completion-gated
`consolidation_last_run_at` write this document leans on twice); `internal/brain/consolidate.go`'s
own `persistBoosts` doc comment (the carried debt §7.1 resolves).

## Scope boundary (binding, from the proposal's §3.2 item 7 and §5's m2d row)

> `m2d` is `internal/scheduler`'s cron and ADR-0009's boot catch-up, `serve` wiring, the
> simulated-weeks golden set, and the L4 demo. Depends on `m2c`.

Four PRs, per proposal §5.1's m2d row: `feat/scheduler-cron` (~300), `feat/scheduler-boot-catchup`
(~300), `feat/serve-scheduler-wiring` (~200), `feat/demo-simulated-weeks` (~400). **These are
guesses** (proposal §5.1's own caveat, measured wrong 1.3×–4.3× across M0/M1) — `sdd-design`'s
re-derivation is authoritative over this row, exactly as it was for `m2a`/`m2b`/`m2c`.

`m2d` adds real code to `internal/core/consolidation` for the first time since `m2b` — the pure
catch-up decision (§2 below) — despite `m2c`'s own closing note that it added none. This is
stated up front so it is not read as a scope violation: the catch-up decision belongs in `core`
by the proposal's own §4.1 argument (ADR-0009's own claim that it is "a pure function... testable
with no scheduler, no clock, no database"), not in `internal/scheduler`.

### R0 — General requirements across all four PRs

**R0.1 — the dependency rule holds at the new boundary, mechanically, not by convention.**
`internal/core`'s `core-purity` depguard rule already denies `internal/scheduler` ("core must not
schedule; it decides" — `.golangci.yml:69-70`), so nothing in `internal/core/consolidation` can
reach the scheduler. The reverse direction has **no gate today**: `internal/scheduler` carries no
depguard entry of its own, so nothing stops it from opening `internal/store/sqlite` directly —
exactly the gap `m2c` design §10.1 found and closed for `internal/ports`/`internal/brain`
(`ports-purity`, `brain-boundary`). **MUST**: this PR adds a `scheduler-boundary` depguard rule
scoped to `**/internal/scheduler/**` denying `github.com/rengo/nooma/internal/store` and
`database/sql` — the scheduler reaches the vault only through `brain.ConsolidateService`, never
by opening a connection itself. **Verified by**: `make check`'s existing lint pass, extended by
this PR's own `.golangci.yml` edit; no new Go test is needed, the same posture `m2c` R0.1 took for
its own three new depguard rules.

**R0.2 — the clock guards' scope, restated so it is not assumed to reach further than it does.**
`brain_single_clock_read_test.go` and `brain_no_direct_clock_read_test.go` scope to
`internal/brain/**`; neither reaches `internal/scheduler`, and neither should — a cron ticker
reads the real clock every tick by definition (`forbidigo`'s own exclusion is `path-except:
internal/core/`, so `internal/scheduler` was already free to call `time.Now`). **MUST**: the one
clock read `brain.ConsolidateService` performs per pass (`m2c` R0.2) is unaffected by this
change — the scheduler decides *when* to call `Consolidate`; it does not pass its own clock
reading into the pass. **Verified by**: L2 — the existing two conformance tests keep passing with
no file-list edit, and a source-tree scan confirms no new file under `internal/scheduler` imports
`ports.Clock` into `internal/brain`'s call.

**R0.3 — calibration gate scope, stated plainly (mirrors `m2c` R0.3's own honesty).** `docs/02-
cognitive-core.md` §13 already carries code-less rows for `boot_consolidation_delay` (120 s),
`trigger_staleness_hours` (6), `timer_staleness_hours` (3), and the "03:00 daily" cadence
(§13:914-917). **MUST**: this change amends those rows to name the Go constants it adds (§1/§2
below). **MUST NOT be read as**: that doing so makes `test/conformance/calibration_doc_test.go`
check them — that gate checks only rows naming an `internal/core/<pkg>.<Symbol>`
(`docs/06-harness.md` §7's own scoping sentence, restated by `m2c` R0.3), and every constant this
section places in `internal/scheduler` is outside that gate's reach by construction. The one
exception is §2's new 24-hour staleness constant, which **does** live in `internal/core/
consolidation` and **is** checked. **Verified by**: manual review for the `internal/scheduler`
constants (no automated check exists, by design of the existing gate); L2 (`calibration_doc_test.go`)
for the one `internal/core` constant this change adds.

---

## 1. `internal/scheduler` — the in-process cron — PR `feat/scheduler-cron`

Traced to proposal §3.2 item 7, §5.1's m2d row (`feat/scheduler-cron`), doc 02 §6's "one pass per
night (default 03:00)" and §13's cadence row.

### R1.1 — the cron fires the whole pass, at a named, once-daily local instant

**MUST**: `internal/scheduler` gains a Go constant naming the consolidation hour (default 3,
local time — see Q2, §7), following `core/classify.PriorWeight`'s own precedent for a bare
constant with a §13 row (proposal §4.1's own citation). **MUST**: firing invokes
`brain.ConsolidateService.Consolidate` with no `Phase` set — the whole pass, `consolidation.
Order()`'s full sequence — never a per-phase call; a cron-triggered per-phase run has no product
requirement anywhere in the documents and would corrupt `since`'s meaning the same way `m2c`
R5.4's own `MUST NOT` already forbids for a hand-run per-phase invocation.

**Verified by**: L2 (`internal/scheduler`) — a fake clock advanced past the named hour asserts
exactly one whole-pass call with no `Phase`; a clock that never reaches it asserts zero calls.

### R1.2 — `consolidation_enabled` gates the cron, and a gated-off tick is a true no-op

**MUST**: before firing, the scheduler resolves `ConfigRepo`'s `ConsolidationEnabled` pointer
through a new pure function in `internal/core/consolidation` (`ResolveConsolidationEnabled`,
mirroring `ResolveWeightThreshold`'s own shape) — `nil` resolves to `true`, matching the column's
own `DEFAULT 1` (migration `0002:65`), never a bare Go literal duplicating that default inline at
the call site. **MUST**: when the resolved value is `false`, the tick calls `Consolidate` zero
times — no pass, no `decision_log` rows, no `consolidation_last_run_at` write, no side effect
beyond the `ConfigRepo` read itself. This is the first real consumer of the column `m2c` R2.4
shipped a nil-sentinel read for but never gated anything on.

**Verified by**: L1 (`internal/core/consolidation`) for `ResolveConsolidationEnabled`'s own two
cases (`nil` → `true`, a stored value → itself); L2 (`internal/scheduler`) — a fixture with
`ConsolidationEnabled` resolved `false` asserts the fired tick never calls `Consolidate`.

### R1.3 — no two passes run concurrently in one process

**MUST**: the scheduler serializes every entry point into a pass — the cron (R1.1) and the boot
catch-up (§2) both funnel through one in-process gate, so a fire arriving while a pass from either
source is still in flight does not start a second, concurrent call to `Consolidate`. Which of
"skip this fire" or "queue it for right after the current pass finishes" is chosen is `sdd-
design`'s call; what is testable regardless is that `brain.ConsolidateService.Consolidate` is
never entered twice at once from scheduler-triggered code. This matters because a pass "may run
for a long time" over a real vault (proposal §4.5) — long enough for the *next* scheduled fire to
arrive before the current one finishes.

**Verified by**: L2 — a fixture with a slow fake `Consolidate` (blocked on a channel) and two
fires scheduled to land inside that window asserts exactly one call is ever in flight.

### R1.4 — an unattended pass abort: the carried debt from `m2c` PR 11, decided here

`internal/brain/consolidate.go`'s `persistBoosts` aborts the whole pass on `ports.
ErrUnitNotFound` from `ApplyBoosts`, while `persistArchiveTransitions`'s analogous race
(`ports.ErrStatusConflict`) is skipped and logged. The code's own comment names this asymmetry as
deliberate and names `m2d` as "the first place to revisit."

**Decision: the scheduled pass still aborts, unchanged.** `persistArchiveTransitions`'s tolerance
exists because spec R4.3 (`m2c`) mandated it explicitly; no spec line covers `reweight`'s
analogous race, and inventing tolerance for it here would mean redesigning `ApplyBoosts`'s
all-or-nothing batch contract (`m2c` R1.1's own closed batch-vs-single decision) — a port
`m2c` already shaped and `m2d` depends on rather than reopens. Tolerating the race without
changing the port would mean silently discarding every other legitimate boost in the same batch
call, which is a worse failure mode than the loud abort it already has.

**MUST**: `persistBoosts` is unchanged by this PR — no retry loop, no partial-application logic,
no new tolerance for `ports.ErrUnitNotFound` is added.

**MUST**: an unattended pass that aborts this way is safe to retry without new machinery, because
`m2c` R5.4 already gates the `consolidation_last_run_at` write on full pass completion — an
aborted pass writes nothing to that column, so it looks, to the very next fire (tomorrow's cron
tick regardless, or the next boot's catch-up if the abort happened during a catch-up-triggered
pass), exactly like a pass that never started. This document states that property as load-bearing
rather than incidental: it is the reason "still abort" is safe to choose here instead of building
a retry mechanism.

**MUST**: an aborted unattended pass is surfaced through process-level operational logging
(the mechanism — `log`, a scheduler-owned `io.Writer`, or similar — is `sdd-design`'s choice, not
this document's). A hand-run `nooma consolidate` already returns the error to a terminal a human
is watching; a scheduler-triggered pass has no such audience, so silently discarding the error
would make the abort literally invisible until someone happens to notice a stale `decision_log`.
Whether this belongs in `decision_log` itself, given the abort has no vault effect (`m2c` I12's
own effect-scoped framing), is left open — see Q3, §7.

**Verified by**: L2 — a fixture where `ApplyBoosts` returns `ports.ErrUnitNotFound` mid-pass,
triggered by a scheduler fire (not a direct `Consolidate` call), asserts the pass returns an
error, `consolidation_last_run_at` is not written, and the operational log channel records the
failure; a second fixture asserts the next fire attempts a fresh whole pass with no special-cased
"retry" state carried over from the aborted one.

---

## 2. ADR-0009's boot catch-up — PR `feat/scheduler-boot-catchup`

Traced to proposal §3.2 item 7, §4.1 (placement), §5.1's m2d row, ADR-0009's "Consolidation —
always recovered" section (the only section of ADR-0009 owner ruling 1 leaves in M2's scope).

### R2.1 — the staleness decision is a pure function in `internal/core/consolidation`

**MUST**: `internal/core/consolidation` gains one new exported pure function deciding whether a
catch-up is due, over `(lastRunAt *time.Time, now time.Time, threshold ...)` — exact signature is
`sdd-design`'s to fix, following the package's existing `Resolve*`/phase-decision shape. **MUST**:
a `nil` `lastRunAt` (a vault that has never consolidated) is always due — ADR-0009's own text
("if... more than 24 h old") reads naturally as "unknown is at least as stale as any known age."
**MUST**: the function is testable with no scheduler, no real clock, and no database — ADR-0009's
own consequences section states this is the point of the placement, and this requirement makes it
literal.

**Verified by**: L1 (`internal/core/consolidation`) — a table of `(lastRunAt, now)` pairs
straddling the threshold, including the `nil` case, asserts the due/not-due boundary is exact and
strict on the documented side (ADR-0009 says "more than 24 h", so exactly-24h-old is not yet due,
mirroring §6's own "strictly less than" convention for `archive`'s threshold comparison).

### R2.2 — the 24-hour staleness threshold gets its own named constant and its own §13 row

ADR-0009's text ("more than 24 h old") and doc 02 §13's existing rows do not currently share one
knob for this value — `incomplete_expiry_hours` is also 24 by coincidence, and is a different
phase's window (§6 item 1), not this one. Doc 02 §5's own governing line — "NUMBERS are
calibratable defaults (see §13); the MECHANISMS are fixed" — and the umbrella proposal's own
success criterion ("every behavioural number M2 introduces is a named constant in exactly one
place and a row in §13") both apply to this number as much as to any other M2 introduces.

**MUST**: this PR adds a Go constant for the 24-hour catch-up staleness threshold in
`internal/core/consolidation`, distinct from `IncompleteExpiryHours`, and a new doc 02 §13 row
naming it — in this same PR, per non-negotiable #1. This is a **fourth** number, beyond the three
ADR-0009 explicitly enumerates ("the three thresholds (120s, 6h, 3h) are calibratable defaults" —
ADR-0009's own Decision section) and beyond owner ruling 2's "three Go constants" — stated
explicitly here so it is not silently under-delivered against doc 02 §5's own general rule.

**Verified by**: L2 — `calibration_doc_test.go` (R0.3 above) covers this constant automatically,
being the one constant this whole document places under `internal/core/`.

### R2.3 — `boot_consolidation_delay` (120 s) governs the delay from decision to execution, once

**MUST**: `internal/scheduler` gains the Go constant `boot_consolidation_delay = 120s` (owner
ruling 2's own naming) and, when R2.1's function decides a catch-up is due at process start,
delays the actual `Consolidate` call by that duration rather than firing immediately — ADR-0009's
own reasoning: startup is already busy opening the vault, running migrations, and connecting
channels, and consolidation "bothers nobody" by waiting two minutes.

**MUST**: the pending delay is itself cancellable — if `serve` is signalled to shut down before
the 120 s elapses, the delayed catch-up is cancelled and never fires (§3's shutdown-ordering
requirement, R3.3, covers the mechanism; this requirement states the outcome the delay window
must honor).

**Verified by**: L2 — a fixture with a fake clock/timer asserts `Consolidate` is not called before
the delay elapses and is called once after; a second fixture cancels the pending delay's context
before it elapses and asserts `Consolidate` is never called.

### R2.4 — catch-up shares the cron's whole-pass entry point and no-overlap guard

**MUST**: a due catch-up invokes the exact same whole-pass call R1.1 describes (no `Phase` set)
and passes through R1.3's same in-process no-overlap gate — there is one path into
`Consolidate` from `internal/scheduler`, entered from two triggers (a 03:00 tick, a due boot
catch-up), not two independently-built call sites that could drift.

**MUST NOT**: this PR implements no part of ADR-0009 beyond "Consolidation — always recovered" —
no `time_based` trigger staleness gate, no ephemeral-timer expiry, no `status='expired'`/
`'cancelled'` transition anywhere. Owner ruling 1 scoped `I15`, `I16`, `I17` to M3, and this PR
defines `trigger_staleness_hours`/`timer_staleness_hours` as Go constants (R0.3, owner ruling 2)
with **no consumer in M2** — the same "define now, no caller yet" shape `m2b`'s `PhaseLearn`
slot took (owner ruling 3), stated here so a reviewer does not go looking for the trigger gate
this PR's own constants might otherwise suggest exists.

**Whether `consolidation_enabled = false` also suppresses the boot catch-up is left open** — see
Q1, §7. This requirement states only what is certain: catch-up and cron share one entry point and
one overlap guard, whatever the gate's final scope turns out to be.

**Verified by**: L2 — a due-catch-up fixture asserts the call is indistinguishable, on the mock
`ConsolidateService`, from a cron-triggered call (same `Phase == nil`); a source-tree scan
confirms no non-test file under `internal/scheduler` references `time_based`, `expired`, or
`cancelled` as a status literal.

---

## 3. `nooma serve` wiring and shutdown ordering — PR `feat/serve-scheduler-wiring`

Traced to proposal §3.2 item 7, §5.1's m2d row (`feat/serve-scheduler-wiring`), and the task's own
naming of "a pass in flight when `serve` is signalled."

### R3.1 — the scheduler reuses `serve`'s own vault, not a second one

**MUST**: `runServe` wires the scheduler using the same `db` connection and the same vault lock
`serve` already holds (`cmd/nooma/serve.go:71-89`) — no second `vaultlock.Acquire`, which would
either deadlock against the lock `serve` itself holds or (if the lock is per-file-descriptor
reentrant) silently create a second logical holder undetectable by `nooma consolidate`'s own
`R6.1` refusal.

**Verified by**: L4 — a `serve` process with the scheduler wired, running against a vault a second
`nooma consolidate` invocation targets, asserts the CLI is refused with `m2c` R6.1's existing
clean lock error — proving the two never fight over two separate lock handles.

### R3.2 — an unconfigured vault degrades the scheduler, not `serve`'s startup

**MUST**: following `wireBrain`'s own precedent (capture/recall come back `nil`, no error, when
the vault's provider bindings cannot resolve `ConsolidateService`'s needs), a vault that cannot
wire a working `ConsolidateService` — a missing judge or embedding provider binding, the same
class of gap `resolveConsolidateProviders` already refuses on in `nooma consolidate`'s own CLI
path — leaves the scheduler unstarted (no cron, no catch-up) rather than failing `runServe`
outright. HTTP capture and recall stay available on such a vault exactly as they do today.

**Verified by**: L4 — `serve` started against a vault with no `relation_evaluation`/`embedding`
task binding asserts the HTTP server starts and answers `/capture`/`/recall` normally, with no
scheduled or catch-up pass ever firing.

### R3.3 — shutdown cancels an in-flight pass; `serve` does not wait for it to finish

**MUST**: the scheduler is wired with (or derives its own pass context from) the same
signal-aware `ctx` `signal.NotifyContext` already produces in `runServe`
(`cmd/nooma/serve.go:114`) — not a separate, unlinked `context.Background()`. When a signal
arrives, an in-flight pass and any pending boot-catch-up delay (R2.3) observe cancellation at the
same instant the HTTP server's own graceful shutdown begins, not after `shutdownGrace` (10 s)
elapses.

**MUST**: `runServe`'s own exit is bounded by `server.Shutdown`'s existing `shutdownGrace` window
— it does not additionally block waiting for a cancelled pass's goroutine to finish unwinding. A
consolidation pass over a real vault can run far longer than 10 s (proposal §4.5), and
`shutdownGrace`'s own stated reasoning — "short enough that a supervisor restarting the service
does not wait on a hung connection" — applies to a background pass with the same force it already
applies to an HTTP request.

**MUST**: a pass cancelled mid-run this way is safe by the same property R1.4 already
establishes: `consolidation_last_run_at` is written only on full completion (`m2c` R5.4), so a
cancelled pass leaves the vault exactly as stale as before it started, and the next cron tick or
boot catch-up retries the whole pass cleanly — no partial-pass state to reconcile.

**Verified by**: L4 — `serve` started with a slow fake consolidation pass in flight, sent
`SIGTERM`, asserts the process exits within `shutdownGrace` and `consolidation_last_run_at` is
unchanged from before the pass started; a follow-up run against the same vault asserts a fresh
whole pass runs to completion with nothing skipped because of the earlier cancellation.

---

## 4. The simulated-weeks golden set and the L4 demo — PR `feat/demo-simulated-weeks`

Traced to proposal §3.2 item 10, §5.1's m2d row, `docs/05-build-plan.md` §M2's own demo bullet,
and `docs/06-harness.md` §2's own citation of this exact demo as the reason the clock is a port.

### R4.1 — a new `testdata/` golden set, with `format.md` before its first case

**MUST**: a new `testdata/<domain>/` directory (naming left to `sdd-design`, following
`classify/`, `llm/`, `recall/`'s own convention) carries a `format.md` written before or in the
same commit as its first case file — `testdata/recall/format.md`'s own shape (fields table, a
worked example, "what the loader does and does not check") is the model, not a template to
improvise past. **MUST**: `golden_sets_test.go`'s existing non-empty-directory guards are
extended to cover the new directory, in this PR's own task list (proposal R10's own risk,
"`golden_sets_test.go` carries non-empty guards that may need extending").

**Verified by**: L1/L2 — a loader test over the new format proves `format.md`'s own documented
shape round-trips; `golden_sets_test.go`'s widened guard fails if the directory is ever empty.

### R4.2 — the corpus is repo-constructed with explicit past timestamps, named as a deliberate exception

**MUST**: the golden set's units carry explicit `created_at`/`last_touched_at`/`weight` values
spanning simulated weeks, authored directly rather than produced by driving the real capture path
weeks apart — an explicit, named departure from `m2c`'s own preference (R5.1, R6.4) for seeding
through real production paths, because simulating weeks of real capture traffic is not practical
inside a test. **MUST**: the demo's own *entry point* — `nooma consolidate` or the scheduler-
triggered path — remains real; only the corpus's timestamps are fixture-authored, not the pass
that reads them.

**Verified by**: this is a corpus-authoring convention documented in `format.md` (R4.1), not
independently mechanized — the same posture `testdata/recall/format.md`'s own "documented, not
checked" cross-field section already takes for a comparable rule.

### R4.3 — the demo runs against `test/support/fakeprovider` only, for every provider call

**MUST**: every judge call (`connect`'s relation judge, `derive`'s dedup judge) and every
embedding call (`derive`'s in-memory active-belief embedding, `m2c` R5.7) the demo's pass makes
goes through `fakeprovider.New`/`fakeprovider.NewEmbeddingFake` — zero network calls, zero real
LLM calls, per CLAUDE.md non-negotiable #5, restated here because this is the one PR in the whole
M2 chain most likely to be tempted to "just try it against a real model once."

**Verified by**: L4 (`test/e2e`, `e2e` build tag) — the demo test's own provider wiring is
`fakeprovider` exclusively; no `httptest.Server` standing in for Ollama, unlike M1's own
`demoProvider` (`test/e2e/demo_test.go`), since M1's demo needed HTTP-shaped realism and this
one's provider calls are all internal to `internal/brain`, not proxied through `serve`'s HTTP
surface at all for this specific pass.

### R4.4 — one pass, one instant, far enough past the fixture to cross every relevant threshold

**MUST**: the demo drives its pass with a single injected "now" (proposal §4.5's own "one pass,
one instant" semantics, unchanged by this PR) chosen far enough past the fixture's own timestamps
that at least one unit's `effective_weight` has fallen under `weight_threshold` (archive fires),
at least one candidate pair is close enough by `connect`'s fused ranking to produce a relation,
and at least one derivable belief exists in the corpus.

**MAY**: the fixture pre-seeds `config.consolidation_last_run_at` to a value before the corpus's
own most-recent timestamps, so `strengthen`'s `since` is non-`nil` and it evaluates real co-use —
doc 02 §6 item 3's own documented fact that `since == nil` (a vault that has never consolidated)
makes `strengthen` evaluate nothing is stated here as design guidance, not left for `sdd-design`
to discover by a failing assertion.

**Verified by**: L4 — the demo asserts, over the chosen `now`, that `archive`, `connect`, and
`derive` each produce at least one effect against this specific corpus (not merely that the pass
completes without error).

### R4.5 — `decision_log` alone tells the story

**MUST**: the demo reads `decision_log` after the pass (via `DecisionLog.Since`, not by
re-deriving state from `units`/`relations`/`self_beliefs` directly) and asserts it contains at
least one legible row for each of `ActionArchiveArchived`, `ActionConnectRelationPersisted`, and
(`ActionDeriveBeliefCreated` or `ActionDeriveBeliefReinforced`) — extending `m2c` R6.4's "at least
one legible row" bar to specifically these three, because the build plan's own demo bullet names
these three outcomes by name ("cold things get archived, related things get connected, beliefs
get derived") and a demo that merely completes without asserting on the log would not actually
prove the bullet.

**Verified by**: L4 — the same test as R4.4, extended to assert the three `Rationale` strings are
present and each names the unit/relation/belief the fixture expects, not merely that a row of the
right `Action` exists somewhere.

### R4.6 — this PR is M2's own exit criterion

**MUST**: this is the PR where `docs/05-build-plan.md` §M2's demo bullet and the umbrella
proposal's own final success-criteria bullet (§2) are satisfied and may be checked off — no later
PR in M2 owes anything toward either.

**Verified by**: this requirement is discharged by R4.1–R4.5 together; it names no additional
test of its own.

---

## 5. Handoffs discharged or explicitly deferred

| Handoff | Discharged by | Disposition |
|---|---|---|
| `persistBoosts`/`persistArchiveTransitions` asymmetry (`m2c` PR 11's own comment, "the first place to revisit") | R1.4 | **Discharged, decided**: still aborts, unchanged code; safe because `m2c` R5.4's completion-gated write makes it self-healing; the abort must now be surfaced through process-level logging since unattended runs have no terminal audience |
| ADR-0009's trigger/timer staleness gates (`I15`, `I16`, `I17`) | R2.4, R2.5-equivalent note | **Explicitly deferred, not `m2d`'s**: owner ruling 1 scoped these to M3; this change defines their two Go constants with no consumer, mirroring `PhaseLearn`'s own "defined now, filled later" shape |
| The 24-hour catch-up staleness threshold's missing §13 home | R2.2 | **Discharged**: a new, distinct constant and §13 row, not folded into `incomplete_expiry_hours`'s coincidentally-equal value |
| `m2c`'s own three named-and-deferred debts — two `MergeDecision`s sharing one `MergeInto` not compounding (untested); the confidence `[0,1]`/NaN bound expressed twice; `judgeAndPersistPair`'s `j.Type == nil` guard with no regression test | — | **Not `m2d`'s.** Restated here only so they are not silently re-derived as new by this document's own review — all three live entirely inside `m2c`'s own shipped surface (`internal/core/consolidation`, `internal/brain`), which this change does not touch except to add the one new pure function R2.1 names |

---

## 6. What this spec does not require

Matching the proposal's own scope boundary and `m2c` §8's own precedent: `time_based` trigger
staleness/expiry, ephemeral-timer staleness/cancellation, quiet-hours logic, any digest or push
delivery, any focus consumer, and the learning module filling `PhaseLearn`'s slot are all M3 or
M5, and no requirement above depends on any of them existing. Every requirement in this document
is provable with no network call, no real clock, and no real LLM — `test/support/fakeprovider`
and an injected `ports.Clock`/fake timer throughout.

---

## 7. Open questions

Each of these is a decision the owner makes; the recommendation is this document's own reasoning,
not a settled answer.

### Q1 — Does `consolidation_enabled = false` also suppress the boot catch-up, or only the nightly cron?

ADR-0009's "Consolidation — always recovered" text names no user-facing off-switch at all — its
whole argument is that this work "bothers nobody" and should always run. The proposal's own
§5.1 line item ties `consolidation_enabled` only to the cron PR (`feat/scheduler-cron`), separate
from the catch-up PR (`feat/scheduler-boot-catchup`), which could mean the gate was intended for
the cron alone.

**Recommendation: gate both.** A user who has explicitly disabled consolidation should reasonably
not expect it to fire behind their back at the next boot either — the two are the same underlying
work with two different triggers, and gating only one is a surprising asymmetry with no stated
reason. But ADR-0009 itself, the ADR this whole PR implements, does not say so, and an `Accepted`
ADR is never edited (non-negotiable #2) — so this needs the owner's confirmation before R2.4's
open note becomes a MUST.

### Q2 — Is the 03:00 consolidation cadence local time, or a fixed offset/UTC?

Doc 02 §6 states only "default 03:00" for consolidation, with no timezone qualifier. Doc 02 §7
states quiet hours as "`[00:00, 07:00)` **local** time" explicitly, for a neighboring but distinct
concept (trigger delivery, out of `m2d`'s own scope).

**Recommendation: local**, for consistency with the one neighboring knob doc 02 does specify a
timezone for, and because a laptop-resident binary's "night" is inherently the user's own local
night. This is inferred, not stated, and R1.1 above is written as a MUST on that inference — flag
here so the owner can confirm or correct it before `sdd-design` fixes the mechanism.

### Q3 — Does an aborted unattended pass also write a `decision_log` row for the abort itself?

R1.4 requires the abort to be *observable*; it does not require the channel to be `decision_log`
specifically. `m2c`'s own I12 framing scopes `decision_log` to decisions with a **vault effect**,
and an abort — by definition — has none (nothing was written). Process-level logging (a `log`
call, a writer the scheduler owns) satisfies "observable" without stretching `decision_log`'s own
scope.

**Recommendation: process-level logging only**, keeping `decision_log` reserved for effects as
`m2c` already framed it. Flagged as open rather than asserted because "observable" is the actual
MUST and an implementer reaching for `decision_log` by default (it is, after all, the glass box)
is a plausible enough reading that the boundary should be confirmed rather than left implicit.
