# Spec — M3b: trigger/timer store, arming, due scan

Delta specification for `m3b-trigger-timer`, the second of four chained changes splitting
`openspec/changes/m3-mouth-telegram/proposal.md` (the umbrella; m3b has no proposal.md of its
own, per the umbrella §5's chain). States what MUST be true of the repository after this change
is applied, in testable form. It does not prescribe how (`sdd-design`'s job).

Sources: umbrella §3.2 items 2–4, §5's `m3b` paragraph, §5.1's six `m3b` rows, §6 order 4–6,
§9 R4/R6/R9/R12; `docs/02-cognitive-core.md` §7, §8, §11; `docs/06-harness.md` §4 (I04, I12,
I15, I18); `internal/core/prospection` as shipped by `m3a-prospection` (`Plan`, `Refusal`,
`Verdict`, `TriggerVerdict`, `TimerVerdict`, `Interrupt`, `Arm`).

## Scope boundary (binding)

> `m3b` is `TriggerRepo` and `TimerRepo` with their SQLite implementations and `memrepo` fakes;
> the focus-candidate query M2 deferred (umbrella §3.3); arming at capture
> (`internal/brain/capture.go:309-322`'s `timerHookRefusal` deleted); the due-scan runner in
> `ConsolidateService`'s own shape (`internal/brain/consolidate.go:30-62`); and `nooma check`.
> **Delivery is a log line — no channel.** Telegram is `m3c`'s and appears nowhere here.

Six PRs, per umbrella §5.1: `feat/ports-trigger-timer`, `feat/store-trigger-timer`,
`feat/ports-store-focus-candidates`, `feat/brain-arm-at-capture`, `feat/brain-due-scan`,
`feat/cli-check`. `internal/core/prospection` ships nothing new here — every gate this change
calls (`TriggerVerdict`, `TimerVerdict`, `Arm`, `Interrupt.Degraded/Route`) is `m3a`'s, consumed
unchanged.

**Not this change**: real push/digest delivery and quiet-hours-aware routing for a
`VerdictDeliver` trigger (`m3d`), recurrence re-arm on fire (I17's behavioural half, `m3d`),
fire-time LLM rephrasing (`m3d`), the four check-ins (`m3d`), the Telegram adapter (`m3c`), the
scheduler tick (`m3d`). A `VerdictDeliver` **timer** is the one exception this change does own —
see R5.2 — because a timer is never routed (m3a design §3.2: it is the push exception itself, not
a candidate for digest/quiet-hours logic).

### R0 — no `internal/core` change; ports, store, brain and `cmd/nooma` only

**MUST**: no file under `internal/core/**` is created or modified by this change. Every decision
this change persists was already a pure function `m3a` shipped; `m3b`'s own work is I/O around it.

**Verified by**: L1 not applicable (no core surface); `nooma-core`'s tree-scan conventions stay
green with zero diff under `internal/core`.

---

## 1. `TriggerRepo` and `TimerRepo` — PR `feat/ports-trigger-timer`

Traced to `internal/store/sqlite/migrations/0001_core_tables.sql:42-70` (both tables exist,
unchanged since M0) and `internal/ports/unitrepo.go:73-79` (`SetStatus`'s `from`-precondition
pattern, the house convention this change reuses rather than reinvents).

### R1.1 — `TriggerRepo` arms, reads what is due, and transitions status under a `from` precondition

**MUST**: `internal/ports` declares a `TriggerRepo` interface with: a method that persists a new
`armed` trigger from an `m3a`-shaped `prospection.Plan` (kind, `fire_at`, `payload`,
`recurrence_rule`/`recurrence_anchor` when `Plan.What == ArmRecurring`, and `interrupt_level` per
R1.3 below); a method that returns every trigger whose status is `armed`, in the shape a due scan
needs (id, `unit_id`, kind, `fire_at`, `interrupt_level`); and a method that moves one trigger from
one status to another, taking `from` as an optimistic-concurrency precondition and returning a
named conflict error when the row's current status is not `from` — `UnitRepo.SetStatus`'s own
contract (`unitrepo.go:73-79`), restated for `triggers.status`'s own vocabulary
(`armed|fired|dismissed|expired`, `0001_core_tables.sql:46`).

**MUST**: no method on this interface is named with a `Delete`/`Remove`/`Purge`/`Drop`/`Destroy`
prefix — `triggers` rows are never removed, only transitioned (`CLAUDE.md` non-negotiable #6, the
same structural rule `UnitRepo`'s own doc comment states at `unitrepo.go:17-22`).

**Scenario: an armed trigger past its `from` precondition refuses the transition**
- GIVEN a trigger whose stored status is `expired`
- WHEN the transition method is called with `from = armed, to = fired`
- THEN it returns the named conflict error and the row is left unchanged

**Verified by**: L2, `test/support/repocontract`'s own shared-suite convention
(`test/support/repocontract/unitrepo.go` is the precedent) — one contract exercised against
`test/support/memrepo`'s fake here and against the SQLite implementation at L3 (§2).

### R1.2 — `TimerRepo` mirrors R1.1 over `timers`, and carries no `interrupt_level`

**MUST**: `internal/ports` declares a `TimerRepo` interface with the same three-method shape as
R1.1 — create, read-pending, transition-under-`from` — over `timers.status`'s own vocabulary
(`pending|fired|cancelled`, `0001_core_tables.sql:66`). The create method takes no interrupt
level: `timers` has no `interrupt_level` column (`0001_core_tables.sql:61-70`), and `m3a`'s design
names the timer itself as the one push exception with no threshold to persist.

**MUST**: the transition method, on a move to `fired`, also accepts `rendered_text` and
`surfaced_at` as optional write parameters — `rendered_text` stays `NULL` in this change (`m3d`'s
LLM rephrasing owns it) but the column exists (`0001_core_tables.sql:65`) and the port must not
foreclose it.

**Verified by**: L2, the same `repocontract` shape as R1.1, over `TimerRepo`.

### R1.3 — a resolved `Interrupt`'s degradation round-trips through `NULL` exactly

**MUST**: `TriggerRepo`'s create method persists `triggers.interrupt_level` as SQL `NULL` when the
`prospection.Interrupt` it is given reports `Degraded() == true`, and as the exact `Level()` float
otherwise — never a sentinel number for either case. The read-due method reconstructs a
`*float64` such that `prospection.ResolveInterrupt` applied to it reproduces the same
`Degraded()`/`Level()` pair that was written, in both directions: a written non-degraded 0.0
reads back non-degraded, and a written degraded value reads back degraded regardless of what
number `DefaultInterruptLevel` happens to equal (`m3a` design §3.4's own stated contract, which
names this "`m3a`'s to state and `m3b`'s to implement").

**Scenario: a degraded interrupt level persists as NULL, not as a claimed number**
- GIVEN `prospection.ResolveInterrupt(nil)`, which reports `Degraded() == true` and
  `Level() == DefaultInterruptLevel`
- WHEN a trigger is created carrying it
- THEN `triggers.interrupt_level` is `NULL` in storage, not `DefaultInterruptLevel`'s numeric
  value

**Verified by**: L2/L3 — a degraded and a non-degraded `Interrupt` each written and read back,
asserting the column's actual stored value (`NULL` vs. a float) and the round-tripped
`Degraded()`/`Level()` pair, never only the Go-level equality of the two `Interrupt` values.

---

## 2. SQLite implementations — PR `feat/store-trigger-timer`

### R2.1 — `TriggerRepo` and `TimerRepo` implement over the existing tables, with no migration

**MUST**: `internal/store/sqlite.TriggerRepo` and `.TimerRepo` implement R1.1/R1.2 over
`triggers`/`timers` as they stand in `0001_core_tables.sql` today. Neither adds a column, an
index, or a new migration file — both tables have carried every field these ports need since M0
(umbrella "Fact" table, row 1).

**MUST**: `testdata/schema/store_api.golden` gains the two new repositories' method sets; no row
in the golden's existing table content changes.

**Verified by**: L3, `repocontract`'s same suite run against a real migrated vault
(`internal/store/sqlite/unitrepo_integration_test.go`'s own pattern); `make check-all`'s
schema-golden regeneration-diff check.

---

## 3. The focus-candidate query — PR `feat/ports-store-focus-candidates`

Traced to umbrella §3.3 (M2's carry-over) and owner ruling 4 (`focus.Priority` is the digest's
importance predicate, decided at the umbrella level; this change supplies its one remaining
missing piece, the store-side read).

### R3.1 — a query returns `focus.Candidate` rows for a set of unit ids, in a defined order

**MUST**: `internal/ports` gains a method (on `UnitRepo` or a new port — `sdd-design`'s choice, not
fixed here) that, given a set of unit ids, returns `focus.Candidate` (`ID, Type, Weight,
DecayRate, LastTouchedAt, CreatedAt, DueAt` — `internal/core/focus/priority.go:158-166`) for
every id among them that is currently live (`status = pool`), in a fully defined, deterministic
order — mirroring `LiveByIDs`'s own "an id that is absent or not live is omitted, not an error"
posture (`unitrepo.go:47-50`).

**MUST**: a `triggers.unit_id` that is `NULL` (a `pattern_based` trigger, `0001_core_tables.sql:44`)
never reaches this query as an id to resolve — the caller (this change's own due-scan reader, or
`m3d`'s digest assembly) is the one that must not pass a `NULL` unit id in, since a `focus.Candidate`
has no representation for "no source unit" and `m3a`'s `Carry` already expects a `nil
*focus.Candidate` for that case (`digest.go`'s `DigestItem.Candidate *focus.Candidate`).

**Verified by**: L2/L3 — an id list mixing live, archived, and absent ids returns only the live
subset, each row matching the unit's own five fields exactly; an empty id list returns an empty
result, never an error.

---

## 4. Arming at capture — PR `feat/brain-arm-at-capture`

Traced to `internal/brain/capture.go:296-322` (`timerHookRefusal`, deleted by this change) and
`internal/core/prospection/arm.go` (`Arm`, `Plan`, `Refusal` — `m3a` PR 7, shipped).

### R4.1 — a `timer`/`recurring_reminder`/dated-`event` classification arms through `prospection.Arm`, never through the deleted refusal

**MUST**: `internal/brain/capture.go`'s `timerHookRefusal` function is deleted. `CaptureService`'s
capture path calls `prospection.Arm(classification, now)` for every classification, using the
single clock read the capture pipeline already takes (`docs/06-harness.md`'s single-clock-read
rule, mirrored from `consolidate.go:159-163`).

**MUST**: when `Arm` returns `(_, true)` (something was armed), the plan is persisted through
`TriggerRepo`/`TimerRepo` (R1.1/R1.2) according to `Plan.What` — `ArmTimer` → `TimerRepo`;
`ArmTrigger`/`ArmRecurring` → `TriggerRepo` — and **no `units` row is created for it** (I04,
restated at the arming boundary: `Arm` already never returns a unit-shaped value, per `m3a`'s own
`test/conformance/i04_arming_never_produces_a_unit_test.go`; this MUST is that no caller undoes
that guarantee by wrapping the plan in a unit anyway).

**MUST**: `CaptureResult`'s outcome for a classification that armed something is distinguishable
from `OutcomeStored` (I04: no `UnitID` exists to report) and from `OutcomeDeferred` as it exists
today (`result.go:44`'s own comment, "timer / recurring_reminder — Q3a", is no longer true after
this change — a timer/recurring_reminder classification is armed, not refused).
**sdd-design owes**: the exact `CaptureOutcome` value/shape naming an armed timer or trigger (a
new member, or a repurposing of `OutcomeDeferred` with a changed meaning — either is a
`docs/02-cognitive-core.md` amendment to Q3a's own answer, which said "not yet wired up"; this
change is the PR where that sentence becomes false and must be corrected in the same PR,
`CLAUDE.md` non-negotiable #1).

**Scenario: a `timer` classification arms a timer, never persists a unit**
- GIVEN a classification with `Kind = timer` and a resolved `DueAt` after `now`
- WHEN `CaptureService.Capture` processes it
- THEN exactly one `timers` row exists afterward, zero `units` rows exist, and the result is
  distinguishable from a stored unit and from today's refusal message

**Verified by**: L2, extending `test/support/memrepo`'s new `TriggerRepo`/`TimerRepo` fakes into
`CaptureService`'s own test wiring (`consolidate_test.go`'s constructor-widening precedent).

### R4.2 — `Plan.Why` reaches `decision_log`, so an undated event is told apart from a chitchat capture

> **Amended 2026-08-21, owner ruling on finding G21.** This requirement originally read: *every*
> `Arm` call returning `(_, false)` writes a `decision_log` row carrying `Plan.Why`, for all four
> `Refusal` members, and its scenario had a `chitchat` capture writing one. Design §3.5 rejected that
> for two of the four with reasons, `tasks.md` task 4b.1 encoded the design's version, and the
> design's version shipped. The owner ratified the derived rule; the requirement below is that rule,
> and the obligation it discharges is unchanged.

**MUST**: a refused arming writes a `decision_log` row **exactly when the capture would otherwise
leave no trace at all** — the rule `docs/02-cognitive-core.md` §11 states, derived rather than
chosen. It follows from doc 02 §11's own scope ("every automatic decision **with an effect**"):

- `RefusalNoDate` and `RefusalAlreadyPast` on a classification that persists no unit — a `timer` or
  a `recurring_reminder` — write one `capture.arm.refused` row. **The user asked for a nudge and did
  not get one**, and nothing else about that capture leaves a record: it never becomes a unit (I04)
  and never reaches `recordClassifyDecision`, so this row is the only trace there will be.
- The same two refusals on a classification that DOES persist a unit — a dated `event` with no date —
  write no row. The unit is the trace, and a second record would double-count one fact.
- `RefusalNoKind` writes no row. It is already recorded, by `ActionCaptureUnclassifiable`.
- `RefusalKindNotArming` writes no row. Ten of the thirteen kinds land here; a row per capture saying
  "this was not a timer" is noise that defeats the glass box, and it is the system working rather
  than a decision with an effect.

**MUST**: the row's `Context` carries `Plan.Why` verbatim, and its `Rationale` is a distinct
human-readable sentence per `Refusal` value — never one shared sentence. This is `m3a`'s own stated
obligation (`arm.go:39-44`'s doc comment: *"an undated event is a decoding failure worth surfacing;
a chitchat capture arming nothing is the system working correctly. They must not read alike"*), and
it is discharged here in full: the two read differently because they are **different actions**, not
merely different sentences under one.

**MUST**: a classification that arms something (`Arm` returns `true`) writes exactly one
`decision_log` row — the arming itself is an effect (I12). Its `Context` carries what was armed
(`armed_id`, `what`, `fire_at`, and where applicable `lead_days`, `recurrence_rule`,
`interrupt_level` and `interrupt_degraded`) and no `why` key: there is no refusal to name, and a
`"why": "none"` field would be a key that means nothing on every row that carries it.

**Scenario: an undated event and a chitchat capture are told apart**
- GIVEN two classifications: one `Kind = event` with `EventAt = nil` (decoding failed), one
  `Kind = chitchat`
- WHEN each is captured
- THEN the event persists its unit and writes `capture.unit.created`, the chitchat writes
  `capture.discarded`, and the two records read differently — which is the distinction this
  requirement exists for
- AND GIVEN a `Kind = timer` with no date, WHEN it is captured, THEN it writes exactly one
  `capture.arm.refused` row naming `RefusalNoDate`, because that capture would otherwise leave no
  trace at all

**Verified by**: L2 — a sweep over `classify.AllKinds()` × {undated, dated future, dated past} ×
{recurrence rule, none}, asserting the biconditional (a refusal row is present if and only if the
kind can arm and the capture wrote nothing else) without enumerating which refusals qualify, so a
fourteenth `Kind` is covered with no test edit.

### R4.3 — arming reads `EventAt`/`DueAt` only through `Arm`; `brain` computes no date itself (I18)

**MUST**: no file under `internal/brain/**` reads `classify.Classification.EventAt`,
`.DueAt`, or `.CreatedAt` to compute a `fire_at`, a lead time, or a recurrence anchor. Every
date used to arm something is `Plan.FireAt`, as `Arm` returned it — I18's guarantee ("never
interchanged") is `m3a`'s own pure-function property; this MUST is that `m3b` does not
reintroduce a second, brain-side computation that could disagree with it.

**Verified by**: L2 — a fixture asserting the persisted `fire_at` equals `Arm`'s own returned
`Plan.FireAt` bit-for-bit, never a brain-recomputed value.

### R4.4 — I04 is strengthened: `test/conformance/i04_timer_never_a_unit_test.go` is rewritten against a real query

**MUST**: `test/conformance/i04_timer_never_a_unit_test.go`'s own doc comment
(`i04_timer_never_a_unit_test.go:36-46`) — which states the timers/triggers half of I04 holds
"by construction… there is nothing here to query" because no `TimerRepo`/`TriggerRepo` exists —
is corrected in this change, in a commit that lands **before** the port commit (umbrella §6 order
4; `strict TDD` — the test asserting the strengthened property is red first, because the port
it queries does not exist yet). The rewritten test asserts, using the real `TriggerRepo`/
`TimerRepo` fakes: zero `units` rows (unchanged), and — new — the arming path this change ships
produces exactly the expected `timers`/`triggers` row. The old assertion (`OutcomeDeferred`,
`Deferred.Kind`, the M1-era refusal message) is replaced per R4.1's new outcome shape.

**MUST**: the test's own file-level doc comment is rewritten to no longer claim the
timers/triggers half is structural — it is now a real, queried assertion, same as the `units`
half always was.

**Verified by**: L2, the rewritten `TestI04_…` itself; `sdd-verify` reads the PR's `git log` and
flags a test-after-implementation ordering as CRITICAL (proposal §6).

---

## 5. The due-scan runner — PR `feat/brain-due-scan`

Traced to `internal/brain/consolidate.go:30-62` (`ConsolidateService`'s shape: one `ports.Clock`
field, a clockless runner, `Consolidate`/`at` split) and umbrella §6 order 4–6 (I15 behavioural,
I12, I18 already covered above).

### R5.1 — the due-scan service reads the clock exactly once per pass and delegates every decision to `m3a`'s verdicts

**MUST**: a new service (mirroring `ConsolidateService`'s split — one `ports.Clock`-holding type,
one clockless runner struct, `docs/06-harness.md`'s single-clock-read rule enforced the same way
`brain_single_clock_read_test.go` enforces it for consolidation) reads `ports.Clock.Now()` exactly
once per invocation and passes that one instant to every `TriggerVerdict`/`TimerVerdict` call in
the pass.

**MUST**: the runner reads pending rows through R1.1/R1.2's read methods, evaluates each through
`m3a`'s `TriggerVerdict`/`TimerVerdict`, and persists only for the outcomes named in R5.2/R5.3
below — never for `VerdictPending`.

### R5.2 — a due timer fires; a stale timer cancels; both write `decision_log`

**MUST**: for each pending timer whose `TimerVerdict(fireAt, now) == VerdictDeliver`, the runner
transitions it `pending → fired` (R1.2's transition method, `from = pending`), setting
`surfaced_at = now` — the log line **is** the delivery this change owns, since a timer is `m3a`'s
one push exception and needs no quiet-hours or digest routing decision before it can be
considered delivered. It writes one `decision_log` row whose context includes whether
`prospection.DelayCaveat(overdue)` is true, for `m3d`'s later rendering to consume.

**MUST**: for each pending timer whose verdict is `VerdictStale`, the runner transitions it
`pending → cancelled` and writes one `decision_log` row (I15's own vocabulary mapping, mirrored
for timers: `VerdictStale` → `cancelled`, per `m3a` design §3.3's stated split).

**Scenario: `nooma check` fires an armed timer that is due and not stale**
- GIVEN a timer armed to fire five minutes ago, well inside `TimerStalenessHours`
- WHEN the due scan runs
- THEN the timer's status is `fired`, `surfaced_at` is set to the pass's instant, and exactly one
  `decision_log` row records it

**Verified by**: L2 — one fixture per verdict outcome (`Deliver`→fired, `Stale`→cancelled), each
asserting the persisted status, the decision_log row count (exactly one), and its rationale.

### R5.3 — a stale trigger expires and writes `decision_log`; a deliverable or deferred trigger is left untouched by this change (I15 behavioural half)

**MUST**: for each armed trigger whose `TriggerVerdict(fireAt, now) == VerdictStale`, the runner
transitions it `armed → expired` and writes one `decision_log` row — I15's own behavioural proof:
"never `fired`" (`docs/06-harness.md:255`) holds because this change's runner has no code path
that ever writes `fired` for a trigger at all.

**MUST**: an armed trigger whose verdict is `VerdictDeliver` is **not** transitioned by this
change. Real trigger delivery needs `Interrupt.Route()`'s push-vs-digest split and quiet-hours-
aware routing — both `m3d`'s (umbrella §5: "Owns I16's and I17's behavioural halves"). Writing
`fired` here, ahead of that routing, would fabricate a delivery this change cannot perform (no
channel exists yet) and would leave `m3d` unable to tell a real delivery apart from a log-only
one without a second persisted marker this change does not invent.

**MUST**: an armed trigger whose verdict is `VerdictDefer` is left untouched and **no**
`decision_log` row is written for it — quiet-hours deferral needs no persisted state (`m3a` design
§3.1: "recomputed every pass from `fire_at` and `now`"), and I12's own discipline is symmetric: "a
scan that decided nothing writes nothing" (umbrella §6 order 6).

**Scenario: `nooma check` expires a trigger overdue past the threshold, never marks it fired**
- GIVEN a trigger armed to fire eight hours ago, outside `TriggerStalenessHours` and outside
  quiet hours
- WHEN the due scan runs
- THEN the trigger's status is `expired`, never `fired`, and exactly one `decision_log` row
  records it

**Scenario: a deliverable trigger stays armed after this change's own due scan**
- GIVEN a trigger whose verdict this pass is `VerdictDeliver`
- WHEN the due scan runs
- THEN the trigger's status is still `armed` afterward and no `decision_log` row was written for
  it this pass

**Verified by**: L2 — the three trigger outcomes (`Stale`→expired+log, `Defer`→untouched+no-log,
`Deliver`→untouched+no-log), each a separate fixture; `i15_trigger_expires_not_fires_test.go`
(shipped by `m3a`) is extended with this change's own behavioural half rather than replaced.

### R5.4 — a concurrent transition conflict is skipped and logged, never fatal to the pass

**MUST**: when R1.1/R1.2's transition method returns its named conflict error (the row's status
no longer matches `from` — a second pass, or an inbound cancellation, raced this one), the runner
records a distinct `decision_log` row naming the skip and continues with the remaining due rows,
rather than aborting the whole scan — `persistArchiveTransitions`'s own precedent
(`consolidate.go:834-853`, catching `ports.ErrStatusConflict` and continuing) applied to triggers
and timers (umbrella R6).

**Verified by**: L2 — a fixture that induces the conflict (pre-seed a row already transitioned)
and asserts the scan completes, logs the skip, and still processes the remaining due rows.

---

## 6. `nooma check` — PR `feat/cli-check`

### R6.1 — `nooma check [vault]` is the due scan's CLI twin, as `nooma consolidate` is the 03:00 pass's

**MUST**: `cmd/nooma` registers a `check` command taking an optional vault path, following
`runConsolidate`'s own shape (`cmd/nooma/consolidate.go:37-49`: parse flags, take the vault's
write lock before opening the store, fail with a clean error naming the lock holder rather than
hanging). It runs one due-scan pass (R5.1–R5.4) and exits 0 on success.

**MUST**: `docs/01-architecture.md:157-161`'s command table gains a `check` row, in the same PR.

**Verified by**: L2/L4 — a real migrated vault seeded with one due timer and one stale trigger;
running the compiled `nooma check` binary against it and asserting, from `decision_log` alone,
that the timer fired and the trigger expired — the exit criterion below, made executable.

---

## 7. What this spec does not require

Matching the umbrella §5's `m3b` row: real channel delivery, push-vs-digest routing for a
deliverable trigger, quiet-hours-aware trigger delivery (I16's behavioural half), recurrence
re-arm on fire (I17's behavioural half), fire-time LLM rephrasing, the four check-ins,
`RelationRepo.Delete`/I10, and the Telegram adapter are all `m3c`/`m3d`. No requirement above
depends on any of them existing.

## Exit criterion (this change's own success condition)

`nooma check` run against a real, migrated vault: fires a timer that is due and not stale
(R5.2), expires a trigger that is overdue past `TriggerStalenessHours` (R5.3), and every one of
those transitions has exactly one corresponding `decision_log` row whose `Rationale` is a
sentence a person can read — with no file under `internal/channels/**` or `internal/scheduler/**`
touched, and no Telegram credential, token, or chat id referenced anywhere in this change's PRs.
