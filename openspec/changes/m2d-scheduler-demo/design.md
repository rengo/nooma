# Design — M2 Phase D: the scheduler and the demo

How `m2d-scheduler-demo` gets built. `spec.md` states what must be true of the repository after
this change; this document fixes the mechanisms, names what each PR touches, and records every
decision with its rejected alternatives. Where the documents genuinely do not answer something,
§12 says so rather than inventing an answer.

Authoritative over `openspec/changes/m2-sleep-weight/proposal.md` §5.1's four-PR row for `m2d`,
exactly as `m2c`'s design was for its own row (spec's Scope boundary grants this explicitly).

---

## 1. Ground truth this design was verified against

Every claim below was read in the tree at `docs/m2d-planning`, not recalled:

| Claim | Where verified |
|---|---|
| `consolidation_last_run_at` is written only after a whole pass completes | `internal/brain/consolidate.go:1032-1060` — `RecordConsolidationRun` runs after the `Order()` loop and only when `req.Phase == nil`; every `runPhase` error returns at :1044-1046, before it |
| `Consolidate` reads the clock exactly once, whole pass or phase | `internal/brain/consolidate.go:161-163` |
| `ConfigRepo` already exposes both fields the scheduler needs | `internal/ports/configrepo.go:17,20` — `ConsolidationEnabled *bool`, `ConsolidationLastRunAt *time.Time`, both annotated "m2d" |
| `internal/scheduler` is `doc.go` alone | `internal/scheduler/doc.go` — package comment, no code |
| `serve` holds one lock, one `*sqlite.Vault`, and a signal-aware ctx | `cmd/nooma/serve.go:71-89, 114` |
| `wireBrain` degrades to `nil` services rather than failing startup | `cmd/nooma/wiring.go:234-238` |
| `wireConsolidate` exists and returns a fully wired `*brain.ConsolidateService` | `cmd/nooma/wiring.go:209-232` |
| The calibration gate only sees `internal/core/<pkg>.<Symbol>` rows, and the Default cell must **lead** with the number | `test/conformance/calibration_doc_test.go:35,46`; floor `calibrationMinSymbols = 21` is a floor, not an equality (:29) |
| doc 02 §13's `boot_consolidation_delay` / `trigger_staleness_hours` / `timer_staleness_hours` / "03:00 daily" rows exist and name no constant | `docs/02-cognitive-core.md:914-917` |
| **doc 02 never mentions `consolidation_enabled` at all** — only `docs/03-data-model.md:191` does | tree-wide grep over `docs/` |
| `UnitRepo.Create(ctx, unit.Unit)` accepts a caller-supplied unit, so fixture timestamps need no new port | `internal/ports/unitrepo.go:40` |
| `sqlite-containment` denies `database/sql` and the driver everywhere except `internal/store/**` and `test/integration/**` — **`test/e2e/**` is not excepted** | `.golangci.yml:83-92` |
| Every store repo is backed by `*sql.DB` | `internal/store/sqlite/*.go` |
| `httpapi.Handler(httpapi.Deps{...})` is the repo's existing "struct of dependencies" constructor shape | `cmd/nooma/serve.go:108` |
| The clock is a port *because of this demo* | `docs/06-harness.md:122-123` — "the M2 demo is 'a vault with simulated weeks of data'. That demo is literally impossible without an injected clock" |

---

## 2. What `m2d` decides, in one paragraph

`internal/scheduler` becomes a **timer that asks and obeys**: it owns goroutines, a `time.Timer`
seam, a 120-second delay and a process log, and it owns no policy. Every question with a
right answer — *is a catch-up due? is the gate open? when does the cron next fire?* — is a pure
function in `internal/core/consolidation`, over `(lastRunAt, now, config)`, testable at L1 with no
scheduler, no real clock, and no database, which is what ADR-0009's own Consequences section says
the placement is for. `serve` wires the scheduler over the vault it already holds, degrades it to
"not started" on an unconfigured vault the way `wireBrain` already degrades capture and recall,
and cancels an in-flight pass at the same instant it starts the HTTP graceful shutdown. The demo
builds its simulated weeks by driving the **real capture path under a stepping fake clock** — the
mechanism `docs/06-harness.md` §2 says the `Clock` port exists to enable.

---

## 3. The five decisions

### 3.1 D1 — the pure/impure split, made structural

`internal/core` is pure by non-negotiable #3 and by two lint gates: `core-purity` (depguard,
`.golangci.yml:49-76`) and `forbidigo` scoped by `path-except: internal/core/` (:151-158). The
split is therefore not a preference — it is the only place a clock-free decision can live and stay
clock-free.

| Question the scheduler must answer | Where it is answered | Shape |
|---|---|---|
| Is a boot catch-up due? | `internal/core/consolidation` | `CatchUpDue(lastRunAt *time.Time, now time.Time, stalenessHours int) bool` |
| Is the gate open? | `internal/core/consolidation` | `ResolveConsolidationEnabled(configured *bool) bool` |
| When does the cron next fire? | `internal/core/consolidation` | `NextDailyRun(after time.Time, hour int) time.Time` |
| How long until then, and who waits? | `internal/scheduler` | a `timer` seam over `time.After` |
| Who holds the goroutines, the delay, the log? | `internal/scheduler` | `Scheduler` |
| Who touches the vault? | nobody in `internal/scheduler` — only `brain.ConsolidateService` | R0.1's new depguard rule (§7) |

**Choice**: the three decisions above are exported functions in `internal/core/consolidation`; the
scheduler holds no `if`-statement over a duration, an hour, or a `*bool`.

**Alternatives considered**: (a) keep the staleness comparison in `internal/scheduler`, next to the
timer that acts on it — rejected: ADR-0009's Consequences section names the pure function as the
whole point of the decision, and a comparison inside a goroutine is provable only by a fake timer,
which is a slower and weaker test than a table over `(lastRunAt, now)`; (b) put the decisions in
`internal/brain` — rejected: `brain` already reads the clock once per pass, and a second reader
would be a `brain_single_clock_read_test.go` violation the moment the scheduler asked it "is a run
due at *now*"; (c) inject the decisions as function values so the scheduler cannot see the
package — rejected as ceremony: `core-purity` plus the new `scheduler-boundary` rule already fix
the arrow direction, and a `func` field buys nothing a package-level call does not.

**What makes it structural rather than conventional**, honestly bounded:

1. `core-purity` denies `internal/scheduler` from core (`.golangci.yml:69-70`) — mechanical, exists.
2. `scheduler-boundary` (§7, new) denies `internal/store` and `database/sql` from the scheduler —
   mechanical, this change.
3. `forbidigo` makes any decision that lands in core clock-free by force — mechanical, exists.
4. A `test/conformance` source scan (new, PR link 2) asserting that non-test files under
   `internal/scheduler` **reference all three core symbols** and contain **no `time.Hour` literal**
   — the scheduler's only durations are `BootConsolidationDelay`'s `120 * time.Second` and the
   `time.Duration` it derives from `NextDailyRun`'s returned instant. This catches the realistic
   regression (someone re-deriving "24 h" or "03:00" inline); it does not prove the general absence
   of duplicated policy, and is named as a scan, not a proof — the same honesty `m2c` applied to
   `record`'s "convention, not a gate" note (`internal/brain/consolidate.go:199-204`).

### 3.2 D2 — the 24-hour staleness threshold lives in `internal/core/consolidation`

**Choice**: `const CatchUpStalenessHours = 24` in the new `internal/core/consolidation/schedule.go`,
with a new doc 02 §13 row naming it.

**Rationale**: it is the one number `m2d` introduces that a pure function consumes, and putting it
in core is the difference between a gate and a wish. `test/conformance/calibration_doc_test.go`
requires the symbol to exist, resolve to a `*types.Const`, and hold exactly the number the row
documents; scheduler-side it would get manual review and nothing else. It also keeps the constant
in the same file as `CatchUpDue`, the only code that reads it, matching `StrengthenGain`/`Strengthen`
and `IncompleteExpiryHours`/`ExpireIncomplete`.

**Alternatives considered**:

| Option | Why rejected |
|---|---|
| `internal/scheduler.CatchUpStalenessHours`, next to the other three constants | Zero automated check, and it separates the number from the pure function whose entire justification is that it is testable alone |
| Reuse `consolidation.IncompleteExpiryHours` (also 24) | A different phase's window (doc 02 §6 item 1) that happens to hold the same number. §13's own precedent for coincidentally-equal knobs — `load_cooldown_days` vs `mental_load_threshold` (`docs/02-cognitive-core.md:900,907`), both 7, both annotated "no test ties them" — is to keep them separate and say so |
| A `config` column + a `ResolveCatchUpStalenessHours` companion | No such column exists in migration 0002 and adding one is a schema change `m2d` has no requirement for. A bare constant is the right shape; `Resolve*` companions exist only for knobs with a config column |

**Row text** (the Default cell must lead with the bare number — `calibrationLeadingNumber` is
anchored, `calibration_doc_test.go:46`):

```
| `catch_up_staleness_hours` (`internal/core/consolidation.CatchUpStalenessHours`) | 24 — ADR-0009's boot catch-up gate; coincides with `incomplete_expiry_hours` above by coincidence, not by relation (a startup staleness window versus a phase's expiry window), no test ties them |
```

**The asymmetry, stated plainly rather than left to be discovered.** Four of the five numbers
`m2d` introduces get **no** automated check:

| Constant | Home | Checked by |
|---|---|---|
| `CatchUpStalenessHours` = 24 | `internal/core/consolidation` | `calibration_doc_test.go` (L2) |
| `ConsolidationHour` = 3 | `internal/scheduler` | manual review only |
| `BootConsolidationDelay` = 120 s | `internal/scheduler` | manual review only |
| `TriggerStalenessHours` = 6 | `internal/scheduler` | manual review only — no consumer in M2 (spec R2.4) |
| `TimerStalenessHours` = 3 | `internal/scheduler` | manual review only — no consumer in M2 (spec R2.4) |

`ConsolidationHour` was considered for core (it is a parameter of the pure `NextDailyRun`, so it
*could* live there and inherit the gate) and **kept in `internal/scheduler` per spec R1.1**, for a
reason found while checking, not assumed: doc 02 §13's row is `| Consolidation / proactive check |
03:00 daily / every 5 min |` (`docs/02-cognitive-core.md:914`) — one cell for two knobs, whose
Default text begins `03:00`, which the gate's anchored `^-?\d+(?:\.\d+)?` parser reads as `03`, not
as a constant value of `3`. Making that row gate-checkable means splitting it and rewriting the
proactive-check half, which belongs to M3. `m2d` amends the row to name
`internal/scheduler.ConsolidationHour` in prose (R0.3) and does not pretend that buys a check.

### 3.3 D3 — `consolidation_enabled = false` gates the cron **and** the boot catch-up

**Owner ruling. ADR-0009 stands unamended, and this is why.**

ADR-0009's "Consolidation — always recovered" section never mentions `consolidation_enabled`. It
cannot: the flag is a `config` column (`docs/03-data-model.md:191`), and the ADR is about downtime
semantics. Read the word "always" in its own document: it is the answer to *this ADR's own
question* — of the three kinds of overdue work the Context section lists, which survive a gap and
which expire. Consolidation is "always recovered" **as against `time_based` triggers, which expire
after 6 h, and ephemeral timers, which expire after 3 h**. The contrast is *recovered vs. expired
by staleness*. It is not *overrides a user's switch*; no user switch appears anywhere in the ADR's
Context, Options, Decision or Consequences.

So gating the catch-up **interprets a silence**; it does not contradict a statement. Nothing in
ADR-0009 is edited, nothing is superseded, non-negotiable #2 is untouched. And the substantive
argument runs one way: the cron and the catch-up are the *same work* behind two triggers, a user
who turned consolidation off has not asked for it to run behind their back at the next boot
instead, and an asymmetry with no stated reason is a bug report waiting to be filed.

**This reasoning must be findable, not reconstructable.** A reader who lands on a heading that says
"always recovered" and then finds catch-up suppressed will go looking. Three places carry it, all
in the catch-up PR:

1. **doc 02 gets the behavior it does not currently state.** Verified: doc 02 mentions
   `consolidation_enabled` nowhere. Suppressing both triggers *is* behavior, and doc 02 governs
   behavior (non-negotiable #1), so `m2d` adds one sentence to doc 02 §6: `config.consolidation_enabled
   = 0` suppresses the nightly pass **and** ADR-0009's boot catch-up — the two are one body of work
   behind two triggers.
2. A doc comment at the gate's call site in `internal/scheduler`, naming ADR-0009 by section and
   carrying the "recovered vs. expired, not vs. a user switch" reading in two sentences.
3. This section, linked from both.

**Alternatives considered**: gate the cron only (proposal §5.1 attaches `consolidation_enabled` to
the cron PR alone, which is weak evidence of intent — it is a PR split, not a semantics statement)
— rejected on the asymmetry argument above. Amend or supersede ADR-0009 to say so — rejected: an
ADR that is silent is not an ADR that is wrong, and a superseding ADR for an interpretation would
set a precedent that every silence needs one.

**Mechanism**: both triggers call `consolidation.ResolveConsolidationEnabled(cfg.ConsolidationEnabled)`
(`nil` → `true`, migration 0002:65's `DEFAULT 1`, never a Go literal at the call site). The cron
re-reads config **at every fire**, so flipping the switch on a running `serve` takes effect at the
next tick without a restart. The catch-up reads config **twice**, not once: once at boot, to
evaluate `CatchUpDue` and decide whether to schedule the delayed fire at all, and a second time
when the 120-second delay elapses and the due fire routes through `runPass` (§3.4, D4) — the same
single entry point the cron uses (spec R2.4). The second read is not an oversight; it is a
structural consequence of PR 4's own single-entry-point requirement: routing the catch-up's due
fire through `runPass`, rather than a `ConsolidateRequest` `catchup.go` constructs itself, means it
inherits `runPass`'s own `Config.Load` and gate re-evaluation. It is also the safer of the two
possible shapes, not merely an accepted cost: the enabled flag is re-checked immediately before the
pass actually fires, so a user who disables consolidation during the 120s delay window has that
change honored at boot's own catch-up too, not only at the cron's next tick.

**Corrected here (JD-4-03), after PR 4.** This paragraph originally read "The catch-up reads it
once at boot, before the 120-second delay — it fires once, so there is nothing to re-read," which
predates R2.4's single-entry-point requirement and was never updated once that requirement
structurally forced the second read through `runPass`. The same posture §3.4's own correction note
above took after PR 3b: the shipped code was the outlier's cause, and the prose was the one left to
correct, not the code.

### 3.4 D4 — one pass at a time: a non-blocking try-lock that skips

**Choice**: a `chan struct{}` of capacity 1 acquired non-blockingly inside a single private method
`runPass(ctx, trigger)`. Both triggers — the 03:00 tick and the due boot catch-up — call only that
method, so there is exactly one entry into `brain.ConsolidateService.Consolidate` from this package
(spec R2.4). A fire that cannot take the slot **skips**, writes one line to the process log naming
the trigger it skipped, and returns. The line does not name the trigger holding the slot: doing so
would need state on `Scheduler` recording the current holder, which the code block below does not
carry and no task asks for. Corrected here after PR 3b, where this prose and the code block below
were found to disagree — the code block, the single-`%s` line it implies, and PR 3b's own task text
were already consistent with each other, so the prose was the outlier.

```go
select {
case s.slot <- struct{}{}:            // acquired
    defer func() { <-s.slot }()
default:
    s.logf("scheduler: %s fire skipped, a pass is already running", trigger)
    return
}
```

**The concrete case this is built for**: `serve` starts at 02:58, the catch-up is due, the 120 s
delay elapses at 03:00, and the cron's own 03:00 tick lands on top of a pass that has just begun.

**Alternatives considered**:

| Option | Why rejected |
|---|---|
| Queue the fire for immediately after the current pass | Runs a second whole pass over a corpus the first one just consolidated: near-zero work, a full round of `connect`/`derive` judge calls (real money on a real provider), and a second `RecordConsolidationRun` that truncates the *next* pass's `since` window to minutes |
| `sync.Mutex` around the pass | Blocks the ticker goroutine instead of the pass, which is the queue behaviour above with a backlog attached, and hides the skip from the log entirely |
| Rely on the vault lock | `serve` holds the lock for the whole process; it does not serialize two goroutines inside that one process |

**Why skipping is safe, not lossy**: the cron fires again in 24 h regardless, and the catch-up's
own staleness test re-evaluates from the vault at the next boot. If the in-flight pass completes,
`CatchUpDue` will correctly say "not due"; if it aborted, `consolidation_last_run_at` was never
written and the next fire sees a vault that looks exactly as stale as one that never consolidated
(§3.5).

### 3.5 D5 — shutdown ordering: the spec's position is confirmed, with one omission corrected

**Confirmed as written.** `spec.md` R3.3's three claims all hold against the code:

- the pass ctx derives from `serve`'s `signal.NotifyContext` ctx (`cmd/nooma/serve.go:114`), so
  cancellation reaches the pass at the same instant `server.Shutdown` begins;
- `serve` does not wait for the pass to finish its work;
- an aborted or cancelled pass is indistinguishable to the next fire from one that never started,
  because `RecordConsolidationRun` is reached only after the full `Order()` loop returns without
  error and only when `req.Phase == nil` (`internal/brain/consolidate.go:1039-1057`), and every
  `runPhase` error returns before it (:1044-1046). Verified by reading, not assumed.

**One correction — "does not wait" must not become "closes the vault underneath a running pass".**
`runServe` releases the lock and closes the database in `defer`s (`cmd/nooma/serve.go:79,89`) that
run the moment it returns. A pass goroutine still executing when that happens is issuing statements
against a `*sql.DB` (`internal/store/sqlite/*.go`) that has just been closed. The spec does not
mention this at all.

**Choice**: `runServe` performs a **bounded join inside the existing `shutdownGrace` budget**, not
in addition to it:

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
defer cancel()
if err := server.Shutdown(shutdownCtx); err != nil { ... }
sched.Wait(shutdownCtx)   // returns when both goroutines unwind, or when the same 10 s deadline expires
```

`Wait` takes the *same* `shutdownCtx`, so the total shutdown bound remains `shutdownGrace` and
R3.3's "does not additionally block" stays literally true — no second 10-second window is
introduced. In the common case the pass observes cancellation at its next ctx-aware port call and
unwinds in milliseconds, and the vault closes cleanly. In the pathological case (a pass wedged
inside a provider call that ignores ctx) the deadline expires and `db.Close()` runs anyway; that
is an error path, not corruption: `sql.DB.Close` lets statements already in flight finish and
fails subsequent ones, each SQLite write is its own transaction, and `consolidation_last_run_at`
is unwritten, so the pass is a no-op to the next fire by the property above.

**Alternatives considered**: wait unbounded (violates R3.3 and re-introduces the hang
`shutdownGrace` exists to prevent); do not wait at all (the spec's literal reading — rejected
because it closes the database under a live goroutine on *every* shutdown, not only the
pathological one); give the pass its own longer grace (two budgets to reason about, and a
supervisor that has to know about both).

**Cancellation is not instantaneous, and the spec's "at the same instant" is about the *signal*,
not about the pass stopping.** A phase inside an LLM call or a SQLite statement returns when that
call observes ctx. Stated so nobody writes a test asserting a stop within microseconds.

---

## 4. What `m2d` adds to `internal/core`, exactly

`m2c`'s design claimed it added one file to `internal/core` and the chain-closing check found two.
Counted here rather than estimated — **two files, one of them a test**:

| Path | Action | Contents |
|---|---|---|
| `internal/core/consolidation/schedule.go` | Create | `CatchUpStalenessHours` (const), `CatchUpDue`, `ResolveConsolidationEnabled`, `NextDailyRun` |
| `internal/core/consolidation/schedule_test.go` | Create | L1 tables for all three functions |

Nothing else under `internal/core/**` is created, modified or deleted by this change. No existing
file in `internal/core/consolidation` is touched: `ResolveConsolidationEnabled` goes in
`schedule.go` beside the other two rather than in `archive.go` next to `ResolveWeightThreshold`,
because it is a pass-level knob, not `archive`'s, and `schedule.go`'s subject is precisely "whether
and when a pass runs".

```go
// schedule.go — the whole of m2d's core surface.

// CatchUpStalenessHours is ADR-0009's boot catch-up gate: a vault whose last
// completed pass is older than this is consolidated at startup. doc 02 §13.
const CatchUpStalenessHours = 24

// CatchUpDue reports whether a boot catch-up pass is due. A nil lastRunAt — a
// vault that has never completed a pass — is always due: unknown is at least
// as stale as any known age. Strictly greater, matching ADR-0009's "more than
// 24 h" and §6's own strict-comparison convention. A lastRunAt in the future
// (a vault carried across machines, a corrected clock) is never due: the
// comparison is signed and this document declines to invent a repair.
func CatchUpDue(lastRunAt *time.Time, now time.Time, stalenessHours int) bool

// ResolveConsolidationEnabled resolves config.consolidation_enabled's
// nil-sentinel. nil is true — migration 0002:65's own DEFAULT 1 — so the
// default lives here and not as a literal at a call site.
func ResolveConsolidationEnabled(configured *bool) bool

// NextDailyRun returns the next instant strictly after `after` whose local
// wall-clock hour is `hour`, minute and second zero, in after's own Location.
// DST is time.Date's own normalization: on a spring-forward day a
// non-existent 03:00 normalizes forward, and on a fall-back day the first
// 03:00 wins — at most one pass per calendar day either way.
func NextDailyRun(after time.Time, hour int) time.Time
```

`stalenessHours` is an `int` parameter rather than a `time.Duration` because the constant must hold
the literal `24` for the calibration gate to compare it against the doc's cell; `24 * time.Hour`
would hold `86400000000000` and fail. `IncompleteExpiryHours = 24` (`expire.go:13`) sets the same
precedent.

Neither `ResolveConsolidationEnabled` nor `NextDailyRun` gets a §13 row: §13 is a table of
calibratable **numbers**, a boolean switch is not one, and `NextDailyRun`'s number is
`ConsolidationHour`, which lives in the scheduler (§3.2).

---

## 5. `internal/scheduler`

### 5.1 Layout

| Path | Action | Contents |
|---|---|---|
| `internal/scheduler/doc.go` | Modify | package comment extended: what the package owns (goroutines, timers, the log) and what it does not (any decision) |
| `internal/scheduler/scheduler.go` | Create | `Deps`, `Scheduler`, `New`, `Start`, `Wait`, `runPass`, the pass slot, the constants |
| `internal/scheduler/cron.go` | Create | the daily loop |
| `internal/scheduler/catchup.go` | Create | the boot catch-up goroutine and its cancellable delay |
| `internal/scheduler/timer.go` | Create | the `timer` seam and its real implementation |
| `internal/scheduler/*_test.go` | Create | L2, in-package (white-box), the convention `internal/brain` already uses |

### 5.2 The dependency surface

```go
// Consolidator is the only thing the scheduler asks the brain to do.
// *brain.ConsolidateService satisfies it; the L2 fixtures implement it directly.
type Consolidator interface {
    Consolidate(ctx context.Context, req brain.ConsolidateRequest) (brain.ConsolidateReport, error)
}

// Deps follows httpapi.Deps' own shape (cmd/nooma/serve.go:108): a struct of
// dependencies, not a nine-argument constructor.
type Deps struct {
    Clock        ports.Clock
    Config       ports.ConfigRepo
    Consolidate  Consolidator
    Log          io.Writer   // the process log; serve passes its errOut
    Timer        timer       // nil means the real one — the test seam, and the only optional field
}

func New(d Deps) (*Scheduler, error)          // rejects a nil Clock/Config/Consolidate
func (s *Scheduler) Start(ctx context.Context) // returns immediately; spawns cron + catch-up
func (s *Scheduler) Wait(ctx context.Context)  // returns when both goroutines unwind or ctx is done
```

**Why a narrow interface over `brain`'s own types** rather than a bare `func(context.Context) error`
adapter built in `cmd/nooma`: `ConsolidateReport.Corrupted()` is operationally meaningful for an
unattended pass for exactly the reason `renderConsolidateReport` gives for the CLI
(`cmd/nooma/consolidate.go:120-125`) — a refused unit appears nowhere else, and a `func` returning
only `error` would discard it silently every night. The scheduler logs `Corrupted()` after each
completed pass.

**Why a package-local `timer` seam and not a widened `ports.Clock`**: `Clock` is `Now()` and
nothing else (`docs/06-harness.md:89`), implemented by every fake in the tree; adding `After` would
force every one of them to change for one consumer. `internal/scheduler` is outside `forbidigo`'s
scope (`.golangci.yml:151-158`), so the real implementation calls `time.After` directly. Fakes
live in the package's own tests.

### 5.3 Flow

```
serve ── ctx (signal-aware) ──▶ Scheduler.Start(ctx)
                                   │
            ┌──────────────────────┴──────────────────────┐
            ▼                                             ▼
     catch-up goroutine                             cron goroutine
     Config.Load ─▶ ResolveConsolidationEnabled     for {
       │  false ─▶ return (D3)                        next := NextDailyRun(Clock.Now(), ConsolidationHour)
       ▼                                              select { timer.After(next-now) | ctx.Done() ─▶ return }
     CatchUpDue(lastRunAt, Clock.Now(), 24)           Config.Load ─▶ ResolveConsolidationEnabled
       │  false ─▶ return                               │  false ─▶ continue   (a true no-op: R1.2)
       ▼                                                ▼
     select { timer.After(120s) | ctx.Done() ─▶ return }
            └──────────────────▶ runPass(ctx, trigger) ◀─┘
                                   │  try-lock the slot, else skip + log (D4)
                                   ▼
                     Consolidate(ctx, ConsolidateRequest{})   ← Phase nil: the whole pass, always
                                   │
                     err ─▶ log the abort (process log only)  │  ok ─▶ log Corrupted(), if any
```

The `ConsolidateRequest` is the zero value at both call sites, and it is constructed in exactly
one place — `runPass` — so a per-phase scheduled run is not merely discouraged but unrepresentable
from this package (R1.1).

### 5.4 The abort log, and why it is not `decision_log`

**Owner ruling: process log only.** `m2c`'s I12 scopes `decision_log` to decisions that had a
vault effect; an aborted pass had none — nothing was written, by construction. Writing an
"abort" row would be the first row in that table describing something that did not happen, and it
would arrive through a code path (`ConsolidateService`'s caller) that has no `DecisionLog` at all.
The `io.Writer` in `Deps` is the whole mechanism: `serve` passes its `errOut`, which already
carries `runServe`'s own human lines (`cmd/nooma/serve.go:119,137`), so no logging framework
decision is forced on this milestone. Three events are logged: an abort, a skipped fire, and a
completed pass that refused units.

`persistBoosts` is not touched (spec R1.4): no retry, no partial application, no new tolerance for
`ports.ErrUnitNotFound`.

---

## 6. `nooma serve` wiring

| File | Action | Change |
|---|---|---|
| `cmd/nooma/wiring.go` | Modify | new `wireScheduler(ctx, db, cfg, lookup, log) *scheduler.Scheduler` — reuses `wireConsolidate`'s own resolution and returns `nil, nil` (not an error) when `resolveConsolidateProviders` refuses |
| `cmd/nooma/serve.go` | Modify | start the scheduler after `wireBrain`; `sched.Wait(shutdownCtx)` after `server.Shutdown` |

**R3.1 — one vault, one lock.** `wireScheduler` takes the `*sqlite.Vault` `runServe` already
opened under the lock it already holds (`serve.go:71-89`). No second `vaultlock.Acquire` anywhere:
the L4 proof is that a concurrent `nooma consolidate` against the same vault still fails with
`m2c` R6.1's clean in-use error, which it can only do if there is exactly one lock holder.

**R3.2 — degrade the scheduler, not the startup.** `wireBrain` returns `nil` services with a `nil`
error on a vault whose task bindings do not resolve (`wiring.go:236-238`), and the handlers answer
503. `wireScheduler` mirrors it exactly: a vault that cannot produce a `ConsolidateService`
(missing `relation_evaluation`, `belief_derivation` or `embedding` binding — the same
`resolveConsolidateProviders` refusal `nooma consolidate` uses) yields a `nil` scheduler, one line
on the log saying consolidation is not scheduled and why, and an HTTP server that starts and
serves capture and recall normally. A `nil` `*Scheduler` accepts `Start` and `Wait` as no-ops, so
`runServe` needs no branch.

**Note on `wireConsolidate`'s cost at startup**: it calls `embeds.LoadIndex` (`wiring.go:224`),
which `wireBrain` also calls — `serve` will load the resident vector index twice unless the two
share. This design does **not** attempt to merge them: `wireBrain` returns `*RecallService` but not
the `*Index`, and threading it out is a refactor of `m1`'s wiring with its own review surface.
Named as accepted cost (one extra index load at startup, on a path that already opens a database
and runs migrations), not hidden. Open question Q4 if the owner wants it collapsed inside `m2d`.

---

## 7. The `scheduler-boundary` depguard rule

Added to `.golangci.yml` following `ports-purity` / `brain-boundary`'s shape exactly (:101-131):

```yaml
        # spec R0.1: internal/scheduler has no depguard entry today, so nothing
        # stopped it from opening internal/store/sqlite directly and running its
        # own SQL — the same gap m2c design §10.1 found for ports/brain. The
        # scheduler reaches the vault only through brain.ConsolidateService.
        scheduler-boundary:
          files:
            - "**/internal/scheduler/**"
          deny:
            - pkg: github.com/rengo/nooma/internal/store
              desc: "the scheduler reaches the vault only through brain.ConsolidateService — docs/06-harness.md §1"
            - pkg: database/sql
              desc: "the scheduler does not speak SQL — docs/06-harness.md §1"
            - pkg: github.com/rengo/nooma/internal/providers
              desc: "the scheduler calls no model; a pass does that through its own ports — docs/06-harness.md §1"
            - pkg: github.com/rengo/nooma/internal/httpapi
              desc: "the scheduler does not know about transport — docs/06-harness.md §1"
```

Two deliberate choices inside the rule:

- **`database/sql` is redundant with `sqlite-containment`** (:83-92 already denies it for `$all`
  minus store and `test/integration`). Spec R0.1 asks for it explicitly and it is kept, because a
  rule that reads as a complete statement of the boundary survives a future edit to
  `sqlite-containment`'s exception list. Named as redundant rather than presented as load-bearing.
- **`internal/channels` is deliberately NOT denied.** ADR-0009's `time_based` trigger delivery —
  M3's half of this same package — is a channel consumer by the ADR's own design ("passing through
  quiet hours and the digest/push logic"). Denying it now would guarantee a future PR has to
  relax this rule, and a rule that gets relaxed teaches that rules get relaxed.

No `allow` list: unlike `core-purity`, the scheduler legitimately needs `internal/brain`,
`internal/ports` and `internal/core/consolidation`, and an allow-list would have to enumerate them
plus every future one. Denial by name matches `brain-boundary`'s own posture.

Verified by `make check`'s existing lint pass; no new Go test, the same posture `m2c` R0.1 took.

---

## 8. The demo and the golden set

### 8.1 D6 — the corpus is built by driving the real capture path under a stepping fake clock

**This corrects spec R4.2**, which requires the corpus's rows to be "authored directly rather than
produced by driving the real capture path". Two pieces of evidence found while checking:

1. `docs/06-harness.md:122-123` states the reason the clock is a port is *this exact demo*: "the M2
   demo is 'a vault with simulated weeks of data'. That demo is literally impossible without an
   injected clock." Authoring rows directly makes the injected clock unnecessary for the demo,
   which contradicts the harness's own stated justification.
2. Authored rows do not populate what `connect` needs. `connect`'s candidate search goes through
   `RecallService.ScoredFor` (`m2c` design §7.1) — a fusion of the resident vector index and FTS.
   `UnitRepo.Create` writes a unit; it does not embed it or index it. A corpus of hand-authored
   units would make R4.4's "at least one candidate pair produces a relation" unreachable without
   also hand-seeding embeddings and lexical rows — three fixture surfaces instead of zero.

**Choice**: the demo constructs a `brain.CaptureService` over a fake clock whose reading advances
per capture, captures the corpus across simulated weeks through the real path, then optionally
seeds `consolidation_last_run_at` through `ConfigRepo.RecordConsolidationRun(ctx, past)` (R4.4's
`MAY`, so `strengthen`'s `since` is non-nil), then runs one pass at a single injected `now`.
Timestamps remain fixture-chosen — R4.2's actual intent — but they arrive through the production
write path, which is `m2c` R5.1/R6.4's own preference and needs no exception.

**Alternative kept in reserve, not chosen**: `sqlite.NewUnitRepo(db).Create(ctx, u)` with authored
`unit.Unit` values. It is available without any port widening (`internal/ports/unitrepo.go:40`),
and it is the fallback if scripting `fakeprovider` responses per capture proves worse than
expected. If that fallback is taken, the embedding and lexical seeding above must be solved in the
same PR or R4.4's connect assertion silently weakens — a trade to make in the open, not quietly.

Note for whoever writes it: `test/e2e/**` is **not** in `sqlite-containment`'s exception list
(`.golangci.yml:83-92`), so the demo may import `internal/store/sqlite` and call its constructors,
but may not import `database/sql` or the driver. No raw SQL in the demo, at all.

### 8.2 The golden set

`testdata/consolidation/`, chosen over `demo/` or `sleep/`: it names the pass, matching
`classify/`, `llm/` and `recall/`'s convention of naming the thing under test.

| File | Contents |
|---|---|
| `testdata/consolidation/format.md` | Written before the first case (R4.1). `testdata/recall/format.md`'s shape: field table, one fenced ```json``` example, "what the loader does and does not check" |
| `testdata/consolidation/format_example.json` | Sibling of `cases/`, never inside it — `assertFormatExampleIsSiblingOfCases` |
| `testdata/consolidation/cases/*.json` | At least one real case |
| `test/support/goldenset/types.go` | New `ConsolidationExample` type the fence decodes into |
| `test/conformance/golden_sets_test.go` | `goldenSetDirs` gains `"consolidation"`; `formatToType` gains its constructor; `casesDirMustBeEmpty["consolidation"] = false` |

All three registration maps must be extended together — `assertCasesDirEmptiness` and
`TestHarness_GoldenSetFormatMatchesType` both `t.Fatalf` on an unregistered directory
(`golden_sets_test.go:100-101, 278-280`), so a half-registration fails loudly rather than skipping.

A case carries: a name, the capture script (offset from `t0`, text, the fake provider's scripted
answer), the injected `now`, an optional `last_run_at`, and the expected effects
(`archived`, `relations_created`, `beliefs`). The exact field set is the format PR's to fix; what
this design fixes is that the fence must decode into `ConsolidationExample` under
`goldenset.DecodeStrict`, which is what makes `format.md` executable rather than aspirational.

### 8.3 What the demo asserts

Per R4.4/R4.5, and only through `DecisionLog.Since`, never by re-reading `units`/`relations`/
`self_beliefs`: at least one `ActionArchiveArchived`, at least one
`ActionConnectRelationPersisted`, and at least one of `ActionDeriveBeliefCreated` /
`ActionDeriveBeliefReinforced`, each with a `Rationale` naming the unit, relation or belief the
fixture expects. Every provider call goes through `test/support/fakeprovider` (R4.3): no
`httptest.Server`, no network, no real model.

---

## 9. Testing strategy

| Layer | What | How |
|---|---|---|
| L1 `internal/core/consolidation` | `CatchUpDue` boundary (nil, just under, exactly 24 h → not due, just over, future) | table test, no clock |
| L1 | `ResolveConsolidationEnabled` (nil → true, `&false` → false, `&true` → true) | table test |
| L1 | `NextDailyRun` (before the hour today, after it, exactly on it, across a month/year boundary) | `time.FixedZone`, no tzdata dependency |
| L1 | `NextDailyRun` DST (spring-forward normalization, fall-back picks the first 03:00) | a real zone, with `import _ "time/tzdata"` **in the test file only** — `time.LoadLocation` has no zone database on Windows otherwise, and this repo cross-compiles for Windows (ADR-0013); a test-only import keeps tzdata out of the shipped binary |
| L2 `internal/scheduler` | one whole-pass call with `Phase == nil` after the hour; zero calls before it | fake clock + fake timer |
| L2 | gated-off tick is a true no-op (zero `Consolidate` calls, zero writes) | fixture with `ConsolidationEnabled = &false` |
| L2 | gated-off boot catch-up (D3) — zero calls even on a 30-day-stale vault | same fixture |
| L2 | no overlap: a slow fake `Consolidate` blocked on a channel, two fires inside the window, exactly one in flight | counter + channel |
| L2 | catch-up fires only after the delay; a cancelled ctx before the delay elapses fires never | fake timer |
| L2 | catch-up and cron are indistinguishable at the `Consolidator` (both `Phase == nil`) | recording fake |
| L2 | an aborted pass (`ports.ErrUnitNotFound` from the fake) writes no `last_run_at` and does write the log line | recording `ConfigRepo` + `bytes.Buffer` log |
| L2 `test/conformance` | `calibration_doc_test.go` covers `CatchUpStalenessHours` automatically | existing gate, new row |
| L2 `test/conformance` | scheduler references the three core symbols and holds no `time.Hour` literal (§3.1) | source scan, new |
| L2 `test/conformance` | `golden_sets_test.go` widened guards | existing gates, new directory |
| L4 `test/e2e` | concurrent `nooma consolidate` still refused (one lock, R3.1) | e2e tag |
| L4 | unconfigured vault: HTTP up, capture/recall answer, no pass ever fires (R3.2) | e2e tag |
| L4 | SIGTERM with a pass in flight: exits within `shutdownGrace`, `last_run_at` unchanged, next run completes a fresh whole pass (R3.3) | e2e tag |
| L4 | the demo: archive + connect + derive each leave a legible `decision_log` row (R4.4/R4.5) | e2e tag, fakeprovider only |

Lint is not a test layer here but is a gate: `make check` runs `scheduler-boundary` from the
moment PR link 2 lands.

---

## 10. Threat matrix

The scheduler adds process-lifecycle integration (signals, background goroutines, shutdown
ordering). It adds no routing, no shell command, no subprocess, no VCS or PR automation, and no
executable-file classification.

| Boundary | Applicability | Design response | Planned RED tests |
|---|---|---|---|
| Documentation-like paths | **N/A** — this change classifies no file as executable or documentation; `testdata/**` is read by a strict JSON decoder only | — | — |
| Git repository selection | **N/A** — no VCS invocation anywhere in `m2d` | — | — |
| Commit state | **N/A** — no VCS invocation | — | — |
| Push state | **N/A** — no VCS invocation | — | — |
| PR commands | **N/A** — no PR automation | — | — |
| *Process lifecycle* (project-specific row) | **Applicable** — signal-aware ctx, two long-lived goroutines, a database closed by `defer` | §3.5: one bounded join inside the existing `shutdownGrace` budget; the pass is safe to kill because the completion write is the only durable marker | L4 SIGTERM-mid-pass (exit within grace, `last_run_at` unchanged, next pass clean); L2 cancelled-delay (catch-up never fires) |
| *Concurrency* (project-specific row) | **Applicable** — two triggers into one non-reentrant pass | §3.4: non-blocking try-lock, skip and log | L2 two-fires-one-window (exactly one in flight); run under `-race` |

---

## 11. Migration / rollout

No schema migration. `config.consolidation_enabled` and `config.consolidation_last_run_at` both
exist since migration 0002; `m2d` is their first scheduler-side reader. No feature flag beyond
`consolidation_enabled` itself, which is the product's own switch and not a rollout mechanism.
Rollout is the binary: a `serve` built from this change consolidates nightly and catches up at
boot; one built before it does not, and a vault is compatible with both because the only column
either writes is set on completion.

---

## 12. Open questions for the owner

None of these blocks the design; each is a place the documents do not answer and this document
declined to invent one.

- [ ] **Q1 — should a skipped fire (D4) be visible anywhere but the process log?** The ruling on
      aborts (process log only, I12's effect-scoping) settles aborts. A *skip* is arguably even
      further from `decision_log` — nothing was even attempted — so this design logs it and stops
      there. Confirm, or say whether an operator-facing counter is wanted in M3's status surface.
- [ ] **Q2 — does the cron fire on a machine that was asleep across 03:00 but never restarted?**
      ADR-0009 answers the *boot* case. Suspend/resume without a process restart is a third case
      the ADR does not name: a `time.After(20h)` timer set at 07:00 fires late on resume, so the
      pass runs at wake-up rather than not at all — which is arguably the desired ADR-0009
      behaviour arriving through the timer rather than through the catch-up. This design leaves
      it as the natural consequence and adds no separate wake-up detection. Confirm.
- [ ] **Q3 — `TriggerStalenessHours` and `TimerStalenessHours` have no consumer in M2 (spec R2.4)
      and no gate (§3.2). Should they be defined in `m2d` at all**, or deferred to the M3 PR that
      first reads them? Owner ruling 2 says define them now; this design implements that ruling
      and flags only that `unused` does not fire on exported constants, so they will sit
      unreferenced with nothing pointing at their eventual consumer except a comment.
- [ ] **Q4 — `serve` will load the resident vector index twice** once `wireScheduler` calls
      `wireConsolidate` (§6). Collapse it inside `m2d` (a refactor of M1 wiring, its own review
      surface, +1 PR link) or accept the duplicate startup load and leave it to a later change?
      This design accepts it.
- [ ] **Q5 — the demo's fake provider script** (§8.1) is the one place `m2d` could burn iterations:
      making `archive`, `connect` and `derive` each fire against a single corpus needs the fake's
      answers tuned to `connect`'s fused ranking and `derive`'s dedup cosine. If the tuning proves
      unbounded, is a corpus per phase (three cases, one assertion each) acceptable, or must one
      corpus produce all three effects as R4.4 reads today?

---

## 13. Review workload forecast and the PR chain

Strategy: **stacked-to-main**. Link 1 targets the tracker branch; each later link targets its
immediate predecessor.

`Decision needed before apply: No`
`Chained PRs recommended: Yes`
`400-line budget risk: High`

Budgets are **implementation + docs** (the lines a reviewer judges against the design), counted
separately from tests, per CLAUDE.md. `m2c` estimated 6 PRs and shipped 15; the chain below is
9 links with 3 pre-drawn splits already marked at the boundaries most likely to run hot, which is
12 real PRs if all three fire.

| # | Branch | Scope | Impl+docs | Tests |
|---|---|---|---|---|
| 1 | `feat/scheduler-core-decisions` | `internal/core/consolidation/schedule.go`; doc 02 §13 row for `CatchUpStalenessHours`; doc 02 §6 sentence on `consolidation_enabled` gating both triggers (§3.3) | ~130 | ~200 |
| 2 | `feat/scheduler-boundary-lint` | `.golangci.yml` `scheduler-boundary`; the conformance source scan (§3.1 item 4) | ~70 | ~90 |
| 3a | `feat/scheduler-cron` | `Deps`/`New`/`Start`/`Wait` skeleton, `timer` seam, cron loop, gate check, `runPass` calling `Consolidate` | ~200 | ~260 |
| 3b | `feat/scheduler-overlap-guard` | **pre-drawn split**: the pass slot and its skip+log path (D4) | ~60 | ~140 |
| 4 | `feat/scheduler-boot-catchup` | catch-up goroutine, `BootConsolidationDelay`, cancellable delay, the D3 gate and its doc comment | ~150 | ~220 |
| 5 | `feat/scheduler-abort-logging` | abort and `Corrupted()` surfacing on the process log; `TriggerStalenessHours`/`TimerStalenessHours` constants and their §13 rows | ~110 | ~150 |
| 6 | `feat/serve-scheduler-wiring` | `wireScheduler` + `serve.go` start/degrade (R3.1, R3.2) | ~140 | ~120 |
| 7 | `feat/serve-shutdown-cancel` | **pre-drawn split of 6**: `Wait(shutdownCtx)` join and the L4 SIGTERM-mid-pass test (§3.5) | ~60 | ~200 |
| 8 | `feat/demo-golden-format` | `testdata/consolidation/format.md` + example + `goldenset.ConsolidationExample` + the three conformance registrations + loader test | ~170 | ~160 |
| 9a | `feat/demo-simulated-weeks` | the stepping-clock capture corpus, one pass, green | ~130 | ~280 |
| 9b | `feat/demo-decision-log-assertions` | **pre-drawn split**: R4.5's three `decision_log` assertions with their expected rationales | ~40 | ~180 |

**Why the splits are pre-drawn where they are.** 3a/3b: `m2c` split at exactly this seam (a
mechanism landing before its concurrency guard) and the guard's test is the slowest part to get
right. 6/7: `serve` wiring and the SIGTERM e2e fixture have different failure modes, and an e2e
process test that fights its own fixture must not hold a wiring change hostage. 9a/9b: getting
three phases to fire against one corpus (Q5) is the single highest-variance task in the chain;
splitting it lets "the demo runs" merge while "the demo proves the build-plan bullet" iterates.

**The two links most likely to blow their budget**: 3a (a new package's whole skeleton at once —
if `New`/`Start`/`Wait` plus the cron loop exceeds 250 lines, split the lifecycle from the loop)
and 8 (`format.md` is prose, and `testdata/recall/format.md` is the bar).

`m2d`'s exit criterion is link 9b: `docs/05-build-plan.md` §M2's demo bullet and the umbrella
proposal's final success criterion are checked off there and nowhere later (R4.6).

---

## 14. What this design does not decide

M3's half of `internal/scheduler` — `time_based` trigger staleness, ephemeral-timer expiry,
`status='expired'`/`'cancelled'` transitions, quiet-hours deferral, digest and push delivery — is
untouched. `TriggerStalenessHours` and `TimerStalenessHours` are defined with no consumer
(spec R2.4, owner ruling 2), the same "defined now, filled later" shape `PhaseLearn` already has.
`persistBoosts`'s abort semantics are confirmed unchanged, not redesigned. The proactive-check
cadence sharing §13's row with the consolidation hour is left as it is, and the reason it cannot
be gate-checked today is recorded in §3.2 for the M3 change that will want to split that row.
