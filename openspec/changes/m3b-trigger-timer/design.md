# Design — M3 Phase B: triggers and timers (ports, store, arming, due scan)

Technical design for `m3b-trigger-timer`, the second of the four chained changes
[`m3-mouth-telegram/proposal.md`](../m3-mouth-telegram/proposal.md) §5 splits M3 into. Scope is
that document's **m3b block only**: `TriggerRepo` and `TimerRepo` with their SQLite
implementations, the focus-candidate query M2 deferred, arming at capture, the due-scan runner,
and `nooma check`. `m3c` (channel port and Telegram adapter) and `m3d` (tick, delivery,
check-ins, demo) are out. **Delivery is a `decision_log` row and nothing else** — no channel, no
rendering, no `surfaced_at`.

`m3b` is the change where `m3a`'s pure verdicts become rows. It writes to two tables that have
existed since M0 and **adds no migration**.

> **Four things this design decides that the umbrella proposal did not anticipate**, each flagged
> for owner review rather than applied silently:
> 1. **The status transitions take no `to` parameter** (§3.1). Proposal R6 asks them to mirror
>    `UnitRepo.SetStatus`'s `from` precondition; they mirror its *semantics* and drop its
>    signature, because the `to` parameter is the only channel through which a `prospection`
>    verdict string could reach a column with no `CHECK` constraint.
> 2. **The trigger and timer status vocabularies live in `internal/ports`, not in `internal/core`**
>    (§3.2), following `ports.StateSourceConsolidation`'s own precedent — because `m3a` §3.3
>    decided core must not name them.
> 3. **`m3b` is eight PRs, not six** (§7), with a named ninth contingency. Two of the proposal's
>    six clear 400 lines on their own.
> 4. **`OutcomeDeferred` and `Deferred` are deleted from `internal/brain`'s public surface**
>    (§3.5), which reaches `internal/httpapi` and `cmd/nooma`. That is a cross-package retirement,
>    not a capture-pipeline edit.

---

## 1. Ground truth this design was verified against

Every row was read at the named file and line in this session.

| Claim | Verified at |
|---|---|
| `triggers` carries `status` `armed\|fired\|dismissed\|expired`, `kind` `time_based\|event_based\|pattern_based`, `unit_id` nullable ("NULL for pattern_based"), `interrupt_level REAL` with **no DEFAULT and no CHECK**, `payload TEXT NOT NULL DEFAULT '{}'` ("action, rationale, lead_days…"), `fire_at` **nullable**, `recurrence_anchor` "JSON `{month, day}`" | `internal/store/sqlite/migrations/0001_core_tables.sql:42-58` |
| `idx_triggers_status_fire ON triggers(status, fire_at)` already exists | `0001_core_tables.sql:59` |
| `timers` has `pending\|fired\|cancelled`, `fire_at NOT NULL`, `surfaced_at`, and **no index at all** | `0001_core_tables.sql:61-70` |
| `surfaced_at`'s own column comment is "NULL = pending delivery" | `0001_core_tables.sql:52` |
| `prospection.Verdict` is a four-member vocabulary (`pending\|defer\|stale\|deliver`) with **no `AllVerdicts()`**, and its doc comment already assigns the `expired`/`cancelled` naming to `brain` | `internal/core/prospection/staleness.go:30-48` |
| `TriggerVerdict(fireAt, now)` / `TimerVerdict(fireAt, now)` take two instants, not a struct | `staleness.go:94-105` |
| `ResolveInterrupt` degrades `nil`, NaN, ±Inf and anything outside `[0,1]` to `DefaultInterruptLevel` with `confirmed = false`, and **never clamps** | `internal/core/prospection/delivery.go:41-50` |
| `Interrupt.confirmed` (not `degraded`) is the shipped field, so the zero value and every hand-written literal are degraded by construction | `delivery.go:29-32`, `delivery.go:56-60`; reasoning at `m3a-prospection/tasks.md:672-701` (Finding F8) |
| `Arm` returns `(Plan, bool)` where `Plan.Why` is a five-member `Refusal`, and its doc comment already states *"m3b writes decision_log from this plan, so a refusal that cannot say why is a hole in the glass box"* | `internal/core/prospection/arm.go:39-64`, `:66-79` |
| A rule-bearing `recurring_reminder` is **never** refused for a past `event_at`; the anchor's year is discarded | `arm.go:112-132`; Finding F9 at `m3a-prospection/tasks.md:742-766` |
| `prospection.Rule` carries exactly `"yearly"` / `"monthly"` — the same two strings `triggers.recurrence_rule`'s column comment names | `internal/core/prospection/recurrence.go:11-16` vs `0001_core_tables.sql:55` |
| `prospection.Anchor` is `{Month time.Month; Day int}` with **no JSON tags** | `recurrence.go:18-23` |
| `timerHookRefusal` returns the "aren't wired up yet" sentence and is routed before `classify.ToUnit` | `internal/brain/capture.go:178-183`, `:296-322` |
| `ports.ActionCaptureHookDeferred` is a member of `AllDecisionActions()`'s twenty-four | `internal/ports/decisionlog.go:52`, `:106-118` |
| `DecisionLog.Since` converts `action` with a plain `ports.DecisionAction(action)` cast — **no vocabulary gate** — so retiring a member cannot break reading historical rows | `internal/store/sqlite/decisionlog.go:99` |
| `OutcomeDeferred` / `Deferred` reach `internal/httpapi/capture.go`, `cmd/nooma/capture.go` and `internal/brain/result.go` — twelve Go files in total | grep over `*.go`; `internal/brain/result.go:44`, `:86-88`, `:100-116` |
| `AllCaptureOutcomes` exists specifically so 13b's HTTP status mapping is a total switch | `internal/brain/result.go:35-59` |
| `UnitRepo.SetStatus` reads the current status, then `UPDATE … WHERE id = ? AND status = ?`, then `requireRowAffected(res, ports.ErrStatusConflict)` | `internal/store/sqlite/unitrepo.go:163-184` |
| `persistArchiveTransitions` catches `ErrStatusConflict`, records `ActionArchiveConflictSkipped` and **continues**; any other error aborts the pass | `internal/brain/consolidate.go:834-853` |
| `ConsolidateService` holds the only `ports.Clock` and hands one instant to a clockless `consolidateRunner`; `record` is the single `decision_log` call site | `consolidate.go:22-62`, `:159-163`, `:194-222` |
| `brain_single_clock_read_test.go` fails a non-test file under `internal/brain/**` on a **second** `Now()` call expression, and on any `Now()` inside a func already taking a `time.Time` | `test/conformance/brain_single_clock_read_test.go:23-38`, `:85-97` |
| `depguard`'s `core-purity` allows `internal/core/**` only `$gostd` and `internal/core` — so core **cannot import `internal/ports`** | `.golangci.yml:49-57` |
| `ports-purity` allows `internal/ports/**` to import `internal/core` | `.golangci.yml:101-107`; already exercised — `unitrepo.go` imports `consolidation`, `unit`, `weight` |
| `ports.StateSourceUser` / `MoodLoaded` are schema literals declared **in `internal/ports`**, deliberately outside §13's reach, pinned to a migration's own column comment by an L2 test | `internal/ports/staterepo.go:8-19` |
| `UnitRepo`'s doc comment forbids a `List(status)` read and requires every read method be **named for what it returns**; `LiveDecayStates` returns a narrow read shape, never a `unit.Unit` | `internal/ports/unitrepo.go:17-36`, `:141-161` |
| `UpdateEventAt` refuses a `*time.Time` parameter because it "would ship a branch with no caller" | `unitrepo.go:57-65` |
| `StateRepo` was declared one PR ahead of both its migration and its implementation, deliberately | `internal/ports/staterepo.go:31-39` |
| `focus.Candidate` is `{ID, Type, Weight, DecayRate, LastTouchedAt, CreatedAt, DueAt *time.Time}` and `Priority` is homogeneous of degree 1 in effective weight | `internal/core/focus/priority.go:158-166`, `:194-198` |
| `nooma consolidate` takes the vault lock itself, resolves providers **before** the lock, and prints silence for a clean pass | `cmd/nooma/consolidate.go:31-114`, `:120-125` |
| `i04_timer_never_a_unit_test.go:36-46` states in its own doc comment that the timers/triggers half holds *"by construction, because there is nothing here to query"* | that file, verbatim |
| `store_api.golden` is regenerated by `make store-api-golden`, and the gate renders `type`, `func`, `var` and `const` | `test/conformance/store_api_test.go:20-29`, `:158-189` |

**`m3b` names zero calibratable constants.** Every number it touches is already a §13 row `m3a`
landed. `calibration_doc_test.go` is untouched and no §13 row is added or amended — which is why
`docs-sync` (proposal R13) fires on exactly one PR in this slice, §7's PR 5a, and that PR carries
a genuine doc 02 §7 delta.

---

## 2. What `m3b` decides, in one paragraph

`m3b` owns **the shape of a trigger and a timer at rest** (two ports, two SQLite repositories over
tables that already exist), **what a capture arms** (`prospection.Arm`'s `Plan` becomes rows, and
its `Refusal` becomes a `decision_log` row exactly where nothing else would record the refusal),
**what one pass over what is due does** (one clock read, `m3a`'s gates, a status transition, a
`decision_log` row), and **how a human runs that pass by hand** (`nooma check`). It decides no
delivery, no rendering, no recurrence re-arm, and no routing — a fired row is left with
`surfaced_at` NULL, which is exactly the state the column's own comment defines as "pending
delivery" and exactly the state `m3d`'s publisher will pick up.

---

## 3. Decisions

### 3.1 The two ports — and why a transition takes no `to`

Proposal R6 asks that "every status transition takes a `from` precondition and returns a conflict,
as `UnitRepo.SetStatus` already does". This design keeps the **semantics** — a precondition, an
optimistic-concurrency conflict — and drops the **signature**.

| Option | Verdict |
|---|---|
| **A. `SetStatus(id, from, to, at)`**, `UnitRepo`'s exact shape | Symmetric, and R6's literal wording. But `from` and `to` would each have exactly one legal value at every m3b call site, and `to` is *the one channel* through which a `prospection.Verdict` string could reach a column that has no `CHECK` constraint (§1). `unit.Status` earns its parameter because `unit.ValidateTransition` decides legality in core; prospection decides no such thing |
| **B. One method per transition, no status parameter** — chosen | The status literal exists in exactly one SQL string per method, inside `internal/store/sqlite`, and never crosses the port boundary in either direction. `StateRepo.OpenHypothesis` sets its own `source` column literal inside its SQL for the same reason (`staterepo.go:34-44`). M4's `dismissed` adds a fourth method rather than a new argument — the same trade `m2c` already took when it chose one method per field over a wide update |

```go
// internal/ports/triggerrepo.go
type TriggerRepo interface {
	// Create persists t. ErrTriggerExists when t.ID is taken.
	Create(ctx context.Context, t Trigger) error

	// Due returns every armed trigger whose fire_at is non-NULL and at or
	// before at, ordered by (fire_at, id). Named for what it returns:
	// there is no Due(status, …) parameterized read (UnitRepo's own rule).
	// fire_at IS NOT NULL is part of the predicate, not a defensive scan
	// guard — a pattern_based trigger legitimately has none (0001:44, :50).
	Due(ctx context.Context, at time.Time) ([]DueTrigger, error)

	// Fire moves id from armed to fired and writes fired_at = at in ONE
	// statement, so a fired row without a fired_at is unrepresentable
	// (I24's shape, applied to this table). surfaced_at is untouched and
	// stays NULL — "pending delivery" (0001:52) is m3d's to close.
	// ErrTriggerStatusConflict when id is no longer armed;
	// ErrTriggerNotFound when it does not exist.
	Fire(ctx context.Context, id string, at time.Time) error

	// Expire moves id from armed to expired (I15). triggers carries no
	// expired_at column, so no timestamp is written and none is invented.
	Expire(ctx context.Context, id string) error
}

// internal/ports/timerrepo.go
type TimerRepo interface {
	Create(ctx context.Context, t Timer) error
	Due(ctx context.Context, at time.Time) ([]DueTimer, error)
	Fire(ctx context.Context, id string, at time.Time) error
	// Cancel moves id from pending to cancelled — doc 02 §8's own
	// vocabulary for a timer too stale to deliver.
	Cancel(ctx context.Context, id string) error
}
```

**No `Delete`-prefixed method on either**, and no `List`. The I03 scan stays scoped to
`ports.UnitRepo` (proposal R9 is `m3d`'s), but these two ports must not be the reason it needs
widening. **No `Cancel`-by-user, no `List` for chat or UI**: doc 02 §8 promises both and `m3b`
ships neither — chat is `m3c`/`m3d`, the UI is M4, and a method with no caller is what
`UpdateEventAt`'s doc comment refuses.

**Write shape and read shape are different types**, following `LiveDecayStates`' own rule
(`unitrepo.go:141-161`): `Create` takes a `Trigger` mirroring every column `m3b` writes;
`Due` returns a narrow `DueTrigger{ID, UnitID *string, FireAt, InterruptLevel *float64,
RecurrenceRule *prospection.Rule, RecurrenceAnchor *prospection.Anchor}`. `m2a` D9's rule says the
narrow shape should be the one a core decision consumes — here the core decision consumes
`(fireAt, now)`, so there is no core struct to reach for and inventing one would be a type with no
core reader. `DueTrigger` carries **no `Status` field**: `Due` returns armed rows only, so the
field would have no reader.

**`payload` is typed, `context` is not, and the difference is which one is read back.**
`ports.Decision.Context` is a `json.RawMessage` because nothing reads it structurally
(`decisionlog.go:128-135`). `payload.lead_days` **is** read back — doc 02 §7 says "the re-arm
propagates it" — so it is a declared `TriggerPayload{ActionText, Rationale string; LeadDays int}`
marshalled by the repository, not an opaque blob whose keys nothing guarantees.

**`condition` is never written.** `Arm` produces only time-based armaments, so `Trigger` carries
no `Condition` field; `event_based` and `pattern_based` production are proposal §3.4 non-goals.

### 3.2 Where the status vocabularies live

`m3a` design §3.3 decided that core "states only this neutral vocabulary — never a schema status"
(`staleness.go:30-34`). So `prospection` cannot hold `TriggerStatus`. Three homes were considered:

| Option | Verdict |
|---|---|
| `internal/core/prospection` | **Rejected.** Directly reverses `m3a`'s decision |
| A new `internal/core/trigger`, mirroring `internal/core/unit` | **Rejected.** `unit.Status` lives in core because `unit.ValidateTransition` and `consolidation.Archive` read it. No core function reads a trigger status, so this is a type with no core consumer |
| `internal/ports` — chosen | Exactly `ports.StateSourceConsolidation`/`MoodLoaded`'s situation (`staterepo.go:8-19`): schema literals that are deliberately outside §13's reach, "pinned instead to migration 0003's own column comment by an L2 test". `ports.DecisionAction` gives the shape: a defined string type, a closed vocabulary, and an `AllX()` **function**, never an exported var, so a mutated slice cannot defeat a completeness check run from outside (`decisionlog.go:98-106`) |

So `internal/ports` declares `TriggerStatus` (`armed|fired|dismissed|expired`), `TriggerKind`
(`time_based|event_based|pattern_based`) and `TimerStatus` (`pending|fired|cancelled`), each with
its `AllX()`, each pinned by an L2 test to migration `0001`'s own column comment — the shape
`relation.AllCreatedBy` already uses against `0001:37`. `dismissed`, `event_based` and
`pattern_based` are declared and never written by `m3b`, precisely as `StateSourceUser` sits
beside a `StateSourceConsolidation` that is the only one anything writes.

`prospection.Rule` **does** cross the boundary, and the asymmetry is the point: doc 02 §7 and
`triggers.recurrence_rule`'s column comment name the same two strings by design, so there is one
vocabulary and no mapping. `Verdict` and `TriggerStatus` name different things on purpose, so
there is a mapping, and it is §3.3.

### 3.3 The `Verdict` → schema-status mapping, and the proof it holds

The mapping lives in **`internal/brain/check.go`**, as two total functions:

```go
// triggerTransition names the schema transition m3a's neutral verdict
// implies for a trigger; ok is false when the verdict implies no write at
// all. Its counterpart timerTransition differs in exactly one arm — the
// one doc 02 §8 names cancelled where §7 names expired.
func triggerTransition(v prospection.Verdict) (ports.TriggerStatus, bool)
func timerTransition(v prospection.Verdict) (ports.TimerStatus, bool)
```

| `Verdict` | Trigger | Timer | Why |
|---|---|---|---|
| `VerdictPending` | no write | no write | `Due`'s own bound makes this unreachable in a pass; handled rather than assumed |
| `VerdictDefer` | no write | **unreachable** — `TimerVerdict` never returns it (`staleness.go:104`) | Quiet-hours deferral is recomputed every pass and persists nothing (`m3a` §3.1) |
| `VerdictStale` | `expired` (**I15**) | `cancelled` | doc 02 §7 and §8's two different words for one verdict |
| `VerdictDeliver` | **no write** — stays `armed` | `fired` | Finding F1: firing a trigger this change cannot deliver strands it. See below |

**Why a deliverable trigger is not fired here (Finding F1).** This table originally read `fired`
for both columns, and `spec.md` said the trigger stays `armed`. The spec is right, and the
decisive reason is neither document's own: it is liveness.

§3.6 puts the `armed` precondition in the `UPDATE`'s `WHERE` clause and nowhere else. A trigger
moved to `fired` therefore never matches the due scan again — and `m3b` has no channel, so nothing
can surface it either. It would sit `fired` with `surfaced_at` NULL, outside the staleness gate's
jurisdiction, unable to be delivered or expired by anything this change ships. A zombie.

Left `armed`, it stays under the gate: every pass re-evaluates it, and if `m3c` and `m3d` are
delayed it expires on its own, which is the honest outcome for a nudge nobody could deliver.
`fired_at` and `surfaced_at` are genuinely two moments (doc 02:785), so firing ahead of delivery
is representable — it is just not *recoverable* until the thing that surfaces exists.

A timer is different and does fire: it is the one push exception with no routing at all, and the
umbrella's own exit criterion names the asymmetry — *"fires an armed timer, expires a stale
trigger."*

**Three layers prove core's vocabulary never reaches the database, and each is assigned to a layer
verified able to run it.**

1. **Free, and lint-enforced.** `internal/core/**` cannot import `internal/ports`
   (`.golangci.yml:49-57`), so no core function can *return* a `ports.TriggerStatus`. This is the
   identical argument `ports.DecisionLog`'s own doc comment already makes for I12's core half
   (`decisionlog.go:138-145`) — recorded, not tested.
2. **Structural, by §3.1's decision.** With no `to` parameter, there is no argument through which a
   `Verdict` could be handed to the store. A `string(v)` conversion has nowhere to go.
3. **L2, exhaustive over the vocabulary — and this is the one that can fail on its own violation.**
   `m3b` PR 5a adds **`prospection.AllVerdicts()`** (the `AllKinds`/`AllDecisionActions`/
   `AllCaptureOutcomes` pattern, twelve lines) and the conformance test iterates it: every verdict
   has a defined answer for both tables, and a fifth verdict added later with no arm fails the
   test rather than silently falling through a `default`.
4. **L3, reading the database rather than the code.** After a scan over a real migrated vault,
   `SELECT DISTINCT status FROM triggers` and `FROM timers` return only members of
   `AllTriggerStatuses()` / `AllTimerStatuses()`. Neither column has a `CHECK` constraint (§1) and
   a published migration is never modified, so **this test is the constraint**. Its RED is genuine
   and must be watched: map `VerdictStale` to the literal `"stale"` and this is the only test in
   the suite that fails.

A `CHECK` constraint would make layer 4 structural. It would also mean a migration `0004` for a
change that has advertised needing none, on two tables whose vocabularies M4 will extend. The
trade is stated, and the test is taken.

### 3.4 The `interrupt_level` round trip — NULL ↔ degraded, exact

**Write.** `brain` persists NULL exactly when `Interrupt.Degraded()`, and the resolved level
otherwise. The conversion is one unexported helper in `internal/brain`:

```go
// interruptColumn is doc 02 §7's NULL <-> degraded contract in one place.
// It is not a method on prospection.Interrupt: "absent means no claim was
// made" is a persistence decision, and m3a deliberately left core unaware
// of the column (design §3.4's own "that contract is m3a's to state and
// m3b's to implement").
func interruptColumn(i prospection.Interrupt) *float64
```

**Read.** The store returns `*float64` verbatim. `ResolveInterrupt` is the inverse
(`delivery.go:41-50`), so the round trip is `ResolveInterrupt(interruptColumn(i)) == i` for every
`i` obtainable from `ResolveInterrupt` — an **L1 white-box test in `internal/brain`**, over
`nil`, `0.0`, `PushThreshold`, `1.0` and a degraded resolution, with no SQLite in the picture.

**What the store does with a value it reads back that is out of range: nothing.**

| Option | Verdict |
|---|---|
| Clamp to `[0,1]` in the repository | **Rejected.** Clamping 1.7 to 1.0 manufactures a push out of a corrupt number — `ResolveInterrupt`'s own recorded reason for degrading rather than clamping (`delivery.go:34-40`) |
| Refuse the row, `corrupted`-style | **Rejected.** `ConsolidateReport.corrupted` exists for a value that makes a *decision* impossible (a NaN weight breaks the ranking). An out-of-range interrupt level makes no decision impossible: `ResolveInterrupt` already has a total, safe answer for it, and refusing the row would suppress a nudge the user asked for over a field that only chooses a lane |
| **Return it verbatim, degrade in core, record the fact** — chosen | The repository returns what the column holds, so an auditor can still tell a corrupt row from a clean one; `ResolveInterrupt` degrades it; and the fired row's `decision_log` context carries `interrupt_level` **and** `interrupt_degraded` so the glass box shows both what was stored and what was made of it |

Two properties follow and both are asserted:

- **One corrupt row does not kill the pass.** A 1.7 is read, degraded, and its trigger fires
  normally. (The contrast: a **scan error** on that column — a REAL column holding a non-numeric
  TEXT, which SQLite's dynamic typing permits — is *not* tolerated and aborts with a named error.
  `persistBoosts`' own ruling applies verbatim: no spec line covers it, so inventing tolerance
  here would be deciding design from an implementation seat, and the failure is loud, not silent.)
- **What SQLite actually stores for NaN and ±Inf in a REAL column is pinned, not assumed.** The
  L3 fixture writes both through raw SQL and asserts whatever comes back, so the answer is
  recorded in a test rather than inferred from a doc nobody re-reads.

**The round trip's cheapest honest layer is L3 in PR 2**, not PR 5a: the insert and the read both
exist inside `internal/store/sqlite` before any `brain` code does, and the JSON-key detail below
lives there too. Assigning it to `brain` would be the layer error Finding F10 records.

**One storage detail a review would miss.** `prospection.Anchor` carries no JSON tags
(`recurrence.go:20-23`) while the column comment says `{month, day}` lowercase
(`0001_core_tables.sql:56`). Go's default marshalling would write `{"Month":9,"Day":4}`. The
SQLite repository therefore declares a private `anchorJSON` with explicit lowercase tags and
converts — adding tags to a core type for a storage concern is the wrong direction. The
mismatch is invisible to any test that does not read the stored bytes, so the L3 fixture asserts
the **stored TEXT**, not just the round-tripped struct.

### 3.5 Arming at capture, and what `decision_log` records

`timerHookRefusal` (`capture.go:296-322`) is deleted, and with it the whole refusal surface it
fed. This reaches further than the capture pipeline:

| Symbol | Fate |
|---|---|
| `timerHookRefusal` | deleted |
| `brain.OutcomeDeferred`, `brain.Deferred`, `CaptureResult.Deferred` | deleted; `OutcomeArmed` and `CaptureResult.Armed *Armed{What prospection.Armament; ID string; FireAt time.Time}` replace them |
| `ports.ActionCaptureHookDeferred` and its `AllDecisionActions()` entry | **deleted.** `DecisionLog.Since` casts the column with a plain `ports.DecisionAction(action)` and applies no vocabulary gate (`decisionlog.go:99`), so historical rows carrying the literal still read back. A vocabulary member with no producer is a bucket the glass box can never show |
| `internal/httpapi/capture.go`, `cmd/nooma/capture.go` | both switch on `CaptureOutcome`; `AllCaptureOutcomes` exists so that switch is total (`result.go:35-39`), which is what makes this retirement a compile-time-visible change rather than a silent one |

**This retirement is its own commit** inside PR 4a, ahead of the arming commit. If its measured
diff exceeds ~150 implementation lines it becomes its own PR (§7's contingency).

**Where arming runs.** Inside `captureRunner.at`, replacing the `timerHookRefusal` fork at
`capture.go:178-183` — the same position, so the "route before `ToUnit`" shape survives and
`classify.ToUnit` is still never reached for a timer. `Arm(c, now)` takes the instant `Capture`
already read; `captureRunner` gains `triggers ports.TriggerRepo` and `timers ports.TimerRepo` and
no clock, so `brain_single_clock_read_test.go` is satisfied by construction.

**What is written, per `Plan`:**

| `Plan.What` | Vault effect | `decision_log` |
|---|---|---|
| `ArmTimer` | one `timers` row, `status = pending`, `action_text` = the request **verbatim** (doc 02 §8: "the request is stored verbatim and only worded at delivery time"), `rendered_text` NULL | `capture.armed.timer` |
| `ArmTrigger` | one `triggers` row, `kind = time_based`, `status = armed`, `payload.lead_days = Plan.LeadDays`, `interrupt_level` per §3.4 | `capture.armed.trigger` |
| `ArmRecurring` | the same row plus `recurrence_rule` and `recurrence_anchor` | `capture.armed.recurring_trigger` |

Three actions rather than one with the armament in context, following `m2c` §7.5's own rule that
effects split when their **Context shapes differ** — a timer row carries no lead days, no rule, no
anchor and no interrupt level — and because `m3d`'s re-arm needs its own action, which would read
oddly beside a merged `capture.armed`.

**And for a `Plan` that armed nothing — the decision the `Refusal` exists for.**

| `Refusal` | Row? | Why |
|---|---|---|
| `RefusalKindNotArming` | **no** | Ten of thirteen kinds land here. A row per capture saying "this was not a timer" is noise that defeats the glass box, and doc 02 §11 records every decision *with an effect*; this is the system working. `TestConsolidate_NoEffects`' own MUST — "a phase fed nothing must write nothing" — applied at capture |
| `RefusalNoKind` | **no** | Already recorded, by `ActionCaptureUnclassifiable` (`capture.go:235-243`). A second row would double-count one fact |
| `RefusalNoDate` | **yes** — `capture.arm.refused` | The user asked for a nudge and did not get one. doc 02 §5.1's "arming a trigger on a guessed date is worse than not arming one" is a decision with a user-visible consequence |
| `RefusalAlreadyPast` | **yes** — `capture.arm.refused` | The same refusal pointing the other way (`arm.go:144-151`) |

**The rule underneath the table, and it is derived rather than chosen:** a refusal writes a row
exactly when the capture would otherwise leave **no trace at all**. A `timer` or
`recurring_reminder` capture never becomes a unit (I04) and never reaches
`recordClassifyDecision`, so `RefusalNoDate` and `RefusalAlreadyPast` are silent otherwise; every
other refusal already has a record. That rule is what the test asserts, not the four rows.

**And the test is exhaustive over inputs, not over the table.** A table listing four `Refusal`
values is exactly the m3a defect shape — a sixth refusal added later would simply not appear in
it. The conformance test is instead table-driven over `classify.AllKinds()` (thirteen members,
already pinned complete by `classify/kind_test.go:12`) × {dated future, dated past, undated} ×
{recurrence rule present, absent}, asserting the exact `decision_log` action set and the exact
row counts in each cell. A fourteenth kind, or a refusal reachable from a new input combination,
fails it.

**I18 at arming** is a property of `Arm`'s own body, already asserted in `m3a` PR 7. What `m3b`
adds is the persistence half: the `timers.fire_at` written for a `timer` capture is the value that
arrived in `due_at`, and the `triggers.fire_at` written for an `event` capture is derived from
`event_at` — asserted by writing a classification whose `due_at`, `event_at` and `created_at` are
**three distinct instants** and reading all three back, so a swapped assignment cannot pass.

**I04, strengthened rather than inherited (R4).** `i04_timer_never_a_unit_test.go:36-46` currently
argues its own vacuity. That paragraph must be **deleted, not amended**, in the test commit ahead
of the port commit — a PR that leaves it standing is defective on its face, because it would be a
comment claiming the opposite of what the file now does. The strengthened test asserts, over the
same two fixtures: zero `units` rows, **exactly one `timers` row**, zero `triggers` rows for the
timer case, and `CaptureResult.Outcome == OutcomeArmed`. The structural half that cannot be
entered from underneath is separate: **no method on `ports.TimerRepo` takes or returns a
`unit.Unit`**, asserted by reflection over the interface, so a timer cannot become a unit through
the port at all.

### 3.6 The due-scan runner

`CheckService` is `ConsolidateService`'s shape verbatim (`consolidate.go:22-62`): a thin
clock-owning shell over a clockless `checkRunner`, with a single `record` helper as the only
`decision_log` call site.

```
now := s.clock.Now()                      ← the file's one Now() call expression
  ├─ triggers.Due(ctx, now)   → for each: prospection.TriggerVerdict(fireAt, now)
  │        Pending / Defer → no write, no row
  │        Stale           → Expire(id)          + check.trigger.expired     (I15)
  │        Deliver         → no write, no row   (F1: stays armed for m3d)
  └─ timers.Due(ctx, now)     → for each: prospection.TimerVerdict(fireAt, now)
           Stale           → Cancel(id)          + check.timer.cancelled
           Deliver         → Fire(id, now)       + check.timer.fired
```

**Eight `DecisionAction` members are added and one is removed**, taking `AllDecisionActions()`
from twenty-four to thirty-one: four for arming (§3.5) and four here —
`check.trigger.expired`, `check.timer.fired`, `check.timer.cancelled`,
`check.conflict_skipped`. There is deliberately no `check.trigger.fired`: after F1 a trigger has
no transition to record in this change, and an action with no producer is a vocabulary member that
looks implemented and is not. The `check.` prefix mirrors `consolidate.`'s, and the segment after it
names the table rather than a phase, because this pass has none.

**`VerdictDefer` writes nothing, and that is load-bearing.** `m3a` §3.1 established that
quiet-hours deferral is recomputed every pass from `fire_at` and `now` and needs no persisted
state. So a deferred trigger stays `armed`, is returned by `Due` again next pass, and resurfaces
by arithmetic. Writing a row per pass per deferred trigger would put up to 84 rows a night per
item into the audit trail for a decision that had no effect.

**Double-firing (R6, proposal R7) — and the shape of the guard matters more than its existence.**

> **The `armed` precondition lives in the `UPDATE`'s `WHERE` clause and nowhere else.** There is
> no `if t.Status == armed` check in Go against the value `Due` returned.

A Go-side check re-reads the stale value the code already holds and cannot fail on the violation
it exists to prevent — the exact defect shape `m3a`'s Judgment Day found five times. With the
predicate in SQL, two concurrent passes race on one statement, exactly one wins, and the loser
gets zero rows affected → `ErrTriggerStatusConflict`. `DueTrigger` carrying no `Status` field
(§3.1) makes the Go-side check unwritable rather than merely discouraged.

The conflict arm is `persistArchiveTransitions`' verbatim (`consolidate.go:834-853`): record
`check.conflict_skipped`, continue with the remaining rows, never fail the pass. Any other error
aborts. `m2c` already ruled that a race-prevented effect is itself worth logging (spec R4.3);
this is that ruling applied, not re-litigated.

**Verification, at a layer checked to be able to run it:** two goroutines running `checkRunner.at`
against one real migrated vault under `-race`, asserting exactly one `fired` row, exactly one
`check.timer.fired` row and exactly one `check.conflict_skipped` row. **The fixture is a timer,
not a trigger** — F1 leaves a trigger with no transition in this change, so a trigger fixture
would race two passes that both correctly write nothing and prove the guard is reached by
proving nothing at all. That is **L3**
(`test/integration/`, build tag `integration`) — the store's own `_txlock=immediate` DSN is the
thing under test, and a memrepo fake's locking would prove nothing about it. L2 with fakes proves
the arm is reached when the port returns the sentinel; it cannot and must not claim to prove the
race.

**`Due`'s cost, stated.** The trigger query's `WHERE status = ? AND fire_at IS NOT NULL AND
fire_at <= ?` matches `idx_triggers_status_fire(status, fire_at)` left to right — the index that
already exists (`0001:59`), one more piece of evidence M3 needs no migration. `timers` has **no
index at all** (`0001:61-70`), so its `Due` is a full scan. On a personal vault that is the same
posture and the same named risk `LiveDecayStates` already carries (`unitrepo.go:157-160`); adding
an index would be a migration `0004` for no measured need.

### 3.7 The focus-candidate query (owner ruling 4, M2's carry-over)

```go
// LiveFocusCandidates returns every status = pool unit as focus.Candidate,
// ordered by id. Named for what it returns (UnitRepo's own rule): Live
// carries the status, FocusCandidates carries the shape.
LiveFocusCandidates(ctx context.Context) ([]focus.Candidate, error)
```

**The proposal's "and its `ORDER BY`" resolves to a deterministic tie-break, not a ranking, and
that correction is worth recording.** `focus.Priority` is a multiplicative envelope over
`weight.Effective` at an instant (`priority.go:168-198`); no SQL expression can order by it. Nor
can raw `weight DESC LIMIT k` approximate it — `Priority` is not monotone in raw weight once
urgency and age enter, so a `LIMIT` would drop a high-urgency low-weight item. So: no `LIMIT`, no
ranking `ORDER BY`, `ORDER BY id` for reproducibility, and `focus.Rank` does the ordering in core
where it is already reviewed and covered. Ordering in SQL would be the drift
`IncompleteOlderThan`'s own doc comment warns about — one rule in two languages.

**It ships with no production caller**, and that is stated rather than hidden: `Carry` is `m3d`'s
digest. Declaring a port ahead of its consumer inside one chain is house practice —
`ports.StateRepo` was declared before both its migration and its implementation, and said so
(`staterepo.go:31-39`). It is not untested: `repocontract` runs it against the fake and the SQLite
implementation alike. The alternative, moving it to `m3d`, pushes the largest slice toward the
nine-PR split threshold proposal §5 already set.

**I02 applies and is new here.** This is a new live read surface, so the SQL filters **positively**
on `status = 'pool'` (`LiveByIDs`' own discipline, `unitrepo.go:80-83`), never by excluding
`superseded` and `incomplete`.

### 3.8 `nooma check [vault]`

`cmd/nooma/check.go`, registered into `main.go`'s dispatch table by a package-level `init()` —
`consolidate.go:19-29`'s exact pattern. It is the CLI twin of `m3d`'s `proactive_check` tick, as
`nooma consolidate` is of the 03:00 cron.

It follows `runConsolidate` (`consolidate.go:37-114`) step for step — flag set, at most one vault
argument, `config.ResolveVault`, `loadVaultConfig`, `vaultlock.Acquire` with a clean
`InUseError`, `sqlite.Open`, `wireCheck`, run, render — with **two deliberate differences, named
so they are not copy-pasted in by reflex**:

1. **No pre-lock provider resolution.** `consolidate` refuses before the lock when a task it needs
   is unbound (`consolidate.go:72-81`), because a pass that silently skipped a phase would still
   write `consolidation_last_run_at`. `m3b`'s scan calls no LLM at all — fire-time rephrasing is
   `m3d` — so it has no task to resolve, writes no last-run marker, and copying that block would
   be a guard protecting nothing while looking like it protects something.
2. **No `--phase`-style flag**, so there is no untrusted vocabulary string entering the binary and
   no `ParsePhase` analogue to route it through.

Output mirrors `renderConsolidateReport`'s posture (`consolidate.go:120-141`): one unconditional
line naming what was scanned — `check: scanned N armed trigger(s) and M pending timer(s)` — then
one line per non-empty outcome. Silence for outcomes that did not happen, because "naming an empty
set would train the eye to skip the line that matters" is that function's own recorded reason.

**doc 01's command table gains one row**, after `nooma consolidate` (`docs/01-architecture.md:161`):

| `nooma check [vault]` | Runs one proactive check over a vault and exits: fires what is due, expires what is too late (a pure subcommand, also used by the scheduler) |

The name sits next to `nooma doctor`, which "checks config". They are different verbs on different
objects and the proposal fixed this name (§3.2 item 7); the collision is recorded as a low-rank
risk rather than reopened.

---

## 4. Package layout and dependency map

```
internal/ports/
├── triggerrepo.go   TriggerStatus, TriggerKind, AllX(), Trigger, TriggerPayload,
│                    DueTrigger, TriggerRepo, ErrTrigger*                        PR 1
├── timerrepo.go     TimerStatus, AllTimerStatuses, Timer, DueTimer, TimerRepo,
│                    ErrTimer*                                                   PR 1
├── unitrepo.go      + LiveFocusCandidates                                       PR 3
└── decisionlog.go   + 9 members, − ActionCaptureHookDeferred              PR 4a/4b/5a

internal/store/sqlite/
├── triggerrepo.go   PR 2      timerrepo.go   PR 2      unitrepo.go  + PR 3

internal/brain/
├── capture.go       − timerHookRefusal, + arming                            PR 4a/4b
├── result.go        − Deferred/OutcomeDeferred, + Armed/OutcomeArmed           PR 4a
└── check.go         CheckService, checkRunner, triggerTransition,
                     timerTransition, interruptColumn, record               PR 5a/5b

internal/core/prospection/staleness.go  + AllVerdicts()                         PR 5a
cmd/nooma/check.go, wiring.go                                                    PR 6
```

**Edges.** `ports` imports `internal/core/prospection` (for `Rule`, `Anchor`) and
`internal/core/focus` (for `Candidate`) — legal under `ports-purity` (`.golangci.yml:101-107`) and
already exercised by `unitrepo.go`'s three core imports. `brain` imports `ports` and `prospection`.
`internal/core` imports neither, and cannot: `core-purity` is what makes §3.3's layer 1 free.

**`store_api.golden` widens.** Two new files under `internal/store/sqlite` add exported `type`,
`func` and `var` lines. Regenerate with `make store-api-golden`, never by hand
(`store_api_test.go:20-29`). The diff is the review artifact.

---

## 5. Data flow

```
  capture text ──► classify.Decode ──► prospection.Arm(c, now) ──► (Plan, ok)
                                                    │
        ┌───────────────────────────────────────────┴──────────────┐
        │ ok                                                 !ok   │
        ▼                                                          ▼
  ArmTimer   ArmTrigger / ArmRecurring                     Plan.Why (Refusal)
     │              │                                              │
     │              │  interruptColumn(Plan.Interrupt)             │ NoDate / AlreadyPast only
     ▼              ▼        (nil ⇔ degraded)                      ▼
 TimerRepo.Create  TriggerRepo.Create ──► triggers row      capture.arm.refused
     │                     │
     └──── decision_log: capture.armed.{timer,trigger,recurring_trigger}
 ═══════════════════════════ a later pass ═══════════════════════════════════
  clock.Now() ──► one instant ──► TriggerRepo.Due(now) / TimerRepo.Due(now)
                       │                     │
                       └──► prospection.{Trigger,Timer}Verdict(fireAt, now)
                                             │
                       ┌────────┬────────────┼────────────┐
                    Pending   Defer        Stale       Deliver
                       │        │            │            │
                    (no write, no row)  triggerTransition / timerTransition
                                            │            │
                                        Expire/Cancel   Fire(id, now)  ──► surfaced_at
                                            │            │                  stays NULL
                                            └──── decision_log ────┘         (m3d's)
                                          ErrStatusConflict ──► check.conflict_skipped
```

`prospection.ResolveInterrupt(DueTrigger.InterruptLevel)` closes the round trip on the read side.
`Interrupt.Route()` is **not** called here: routing is `m3d`'s decision, and recording a route
`m3b` does not act on would be a row claiming more than the code delivers.

---

## 6. File changes

| File | Action | What |
|---|---|---|
| `internal/ports/triggerrepo.go` | Create | PR 1 — §3.1, §3.2 |
| `internal/ports/timerrepo.go` | Create | PR 1 — §3.1, §3.2 |
| `test/support/memrepo/{triggers,timers}.go` | Create | PR 1 — fakes |
| `test/support/repocontract/{triggerrepo,timerrepo}.go` | Create | PR 1 — the shared contract both implementations run |
| `internal/store/sqlite/{triggerrepo,timerrepo}.go` | Create | PR 2 — §3.4's `anchorJSON` lives here |
| `testdata/schema/store_api.golden` | Modify | PR 2, PR 3 — regenerated |
| `internal/ports/unitrepo.go` | Modify | PR 3 — `LiveFocusCandidates` |
| `internal/store/sqlite/unitrepo.go` | Modify | PR 3 — its SQL |
| `internal/brain/result.go` | Modify | PR 4a — `Deferred` → `Armed` |
| `internal/brain/capture.go` | Modify | PR 4a/4b — `timerHookRefusal` deleted, arming added |
| `internal/httpapi/capture.go`, `cmd/nooma/capture.go` | Modify | PR 4a — the outcome switch |
| `internal/ports/decisionlog.go` | Modify | PR 4a/4b/5a — nine added, one removed |
| `internal/core/prospection/staleness.go` | Modify | PR 5a — `AllVerdicts()` |
| `internal/brain/check.go` | Create | PR 5a/5b |
| `cmd/nooma/check.go` | Create | PR 6 |
| `cmd/nooma/wiring.go` | Modify | PR 4a (capture), PR 6 (`wireCheck`) |
| `docs/02-cognitive-core.md` | Modify | §5 step 5 and §8 (PR 4a), §7 (PR 5a) |
| `docs/01-architecture.md` | Modify | PR 6 — one command-table row |
| `test/conformance/i04_timer_never_a_unit_test.go` | Modify | PR 4a — lines 36-46 **deleted** |
| `test/conformance/i15_trigger_expires_not_fires_test.go` | Modify | PR 5a — the behavioural half beside `m3a`'s pure one |

**No migration. No `docs/03-data-model.md` change. No schema golden change. No §13 row.**

---

## 7. The eight PRs

Chain `stacked-to-main`, delivery `auto-chain`. Every PR: **the conformance test is its own commit
ahead of the implementation commit** (proposal §6; `sdd-verify` reads the PR's `git log` and
reports an inversion as CRITICAL). Estimates are budgets against the 400-line
implementation-plus-docs ceiling, **excluding test lines** — `test/support/**` counts as test
lines, and the ports' fakes and contracts are substantial.

| # | Branch | Content | Impl+docs |
|---|---|---|---|
| 1 | `feat/ports-trigger-timer` | Both ports, three vocabularies + `AllX()`, write and read shapes, sentinels. Fakes and `repocontract` (tests) | ~250 |
| 2 | `feat/store-trigger-timer` | Both SQLite repos, `anchorJSON`, golden regenerated. **L3 owns §3.4's round trip** | ~250 |
| 3 | `feat/ports-store-focus-candidates` | `LiveFocusCandidates` + its positive-`pool` SQL (I02) | ~120 |
| 4a | `feat/brain-arm-at-capture` | Retirement commit (`timerHookRefusal`, `Deferred`, `ActionCaptureHookDeferred`), then arming, three effect actions, doc 02 §5/§8. **I04 strengthened, I18 persisted** | ~280 |
| 4b | `feat/brain-arm-refusal-audit` | `capture.arm.refused`, the two logging refusals, the `AllKinds()`×date×rule table | ~110 |
| 5a | `feat/brain-due-scan` | `CheckService`, one clock read, `AllVerdicts()`, both transition mappings, four actions, doc 02 §7. **I15 behavioural, I12** | ~340 |
| 5b | `feat/brain-due-scan-conflict` | The `ErrStatusConflict` arm, `check.conflict_skipped`, the L3 concurrent-tick proof. **R6/R7** | ~90 |
| 6 | `feat/cli-check` | `nooma check [vault]`, `wireCheck`, doc 01's row, the L4 exit demo | ~180 |

**Eight PRs, ~1,620 budgeted implementation-and-docs lines.** Proposal §5.1 planned six at ~2,100.
Read against this project's own measured 1.3×–4.3× multipliers (proposal §5.1, R11), realistically
**2,100–7,000 lines across 8–12 PRs**.

**Why eight and not the proposal's six**, stated as two measurements rather than as a preference:

- **PR 4 splits** because the retirement of `OutcomeDeferred` reaches four packages before one line
  of arming is written, and arming's own effect rows and refusal rows are two reviewable units
  with different invariants (I04/I18 versus the glass-box refusal rule).
- **PR 5 splits** because concurrency safety is an autonomous property with its own verification
  layer (L3) and its own rollback, and folding it into the runner PR would mean a reviewer judging
  a race and a state machine in one diff.

**The one PR at genuine risk is 5a**, budgeted at 340: at this project's median multiplier it
clears 400. Its natural further cut is named now so it is a measurement and not a mood — the
trigger half (I15, `expired`) and the timer half (`cancelled`) — giving a ninth PR
`feat/brain-due-scan-timers`. `sdd-tasks` should apply that cut if its own forecast for 5a exceeds
**400** implementation-and-docs lines.

Order: 1 → 2 → 3 (store slice), 4a → 4b (capture slice, needs 1 and 2), 5a → 5b (scan slice, needs
1 and 2), 6 (needs 5). 3 is independent of everything after it. Stacked in the order tabled.

---

## 8. Testing strategy

| Layer | What | Where |
|---|---|---|
| **L1** | `interruptColumn` ∘ `ResolveInterrupt` is the identity over `{nil, 0.0, PushThreshold, 1.0, degraded}` — white-box in `internal/brain`, no SQLite | `internal/brain/check_test.go` (PR 5a) |
| **L2** | Both transition mappings, **iterated over `prospection.AllVerdicts()`**, so a fifth verdict fails rather than falls through | `test/conformance/` (PR 5a) |
| **L2** | The three port vocabularies pinned to migration `0001`'s own column comments — `relation.AllCreatedBy`'s shape against `0001:37` | `test/conformance/` (PR 1) |
| **L2** | **I04 strengthened**: zero `units` rows, exactly one `timers` row, `OutcomeArmed`; plus the reflection check that no `TimerRepo` method touches `unit.Unit` | `i04_…_test.go` (PR 4a) |
| **L2** | **I18 persisted**: a classification with three distinct instants in `due_at`, `event_at`, `created_at`, all three read back from the written row | `test/conformance/` (PR 4a) |
| **L2** | The refusal rule, table-driven over `classify.AllKinds()` × {future, past, undated} × {rule, no rule} — exhaustive over inputs, not over the four-row table | `test/conformance/` (PR 4b) |
| **L2** | **I15 behavioural**: a trigger past its window becomes `expired` and **never** `fired`, swept across the window rather than sampled at three points | `i15_…_test.go` (PR 5a) |
| **L2** | **I12 both directions**: every effect writes exactly one row, and a scan that decided nothing writes zero | `test/conformance/` (PR 5a) |
| **L3** | Both repositories against `repocontract`, over a real migrated vault | `internal/store/sqlite/*_integration_test.go` (PR 2, PR 3) |
| **L3** | **`interrupt_level` round trip**: NULL ↔ degraded exactly; 1.7 reads back as 1.7 and degrades; NaN and ±Inf written through raw SQL, whatever comes back **pinned**; a non-numeric TEXT aborts with a named error | PR 2 |
| **L3** | `recurrence_anchor`'s **stored TEXT** is `{"month":…,"day":…}` lowercase, not Go's default field names | PR 2 |
| **L3** | Concurrent double-fire: two `checkRunner.at` goroutines, `-race`, exactly one `fired` row and exactly one `check.conflict_skipped` row | `test/integration/` (PR 5b) |
| **L3** | `SELECT DISTINCT status` on both tables after a scan yields only vocabulary members — **the constraint the schema does not carry** | `test/integration/` (PR 5a) |
| **L4** | `nooma check` on a built vault fires an armed timer, expires a stale trigger, and `decision_log` tells the story — the slice's own exit criterion | `test/e2e/` (PR 6) |

**Fixtures express boundaries as multiples of the `m3a` constants, never as literals**
(`focus/priority.go:137-140`'s own discipline), so a recalibration needs no fixture edit.

**Two things this design deliberately does not let a test assume.** Whether SQLite can hold NaN in
a REAL column, and what Go's default marshalling does to `prospection.Anchor` — both are *pinned by
reading the stored bytes*, not inferred. The `clampedDate` lesson from `m3a` is that a defence one
line away from a probe does not cover the probe.

**No new `internal/core` coverage exposure.** `AllVerdicts()` is twelve lines with its own test;
the floor (R12) is not stressed by this slice as it was by `m3a`. `make check-all` before every PR
remains mandatory, because the store golden and L3 only run there.

---

## 9. Threat matrix

**Effectively N/A, with the one arguably-applicable row named and answered rather than omitted.**

| Boundary | Status |
|---|---|
| Routing | N/A — `m3b` opens no port and adds no route |
| Shell / subprocess | N/A — no `os/exec`; `brain-boundary` and `scheduler-boundary` depguard rules already deny the neighbourhood |
| VCS/PR automation | N/A |
| Executable-file classification | N/A |
| Process integration | N/A — `nooma check` is a subcommand in the same binary, dispatched by the existing table |
| **Untrusted CLI input → filesystem path** | **Applicable, unchanged.** `nooma check [vault]` takes a user-supplied path through `config.ResolveVault` and `vaultlock.Acquire`, the identical, already-reviewed path `nooma consolidate` takes (`consolidate.go:63-91`). `m3b` adds no new parsing of untrusted text: unlike `--phase`, `check` takes no vocabulary flag, so there is no second entry point into any closed vocabulary. Expected failure behaviour: a nonexistent vault fails with `ResolveVault`'s own error; a held lock fails with `vaultlock.InUseError` naming the holder; more than one positional argument is refused before anything is opened. All three are asserted in PR 6's `cmd/nooma/check_test.go` |

Telegram's boundaries (`getUpdates`, `allowed_chat_ids`, `bot_token_env`) are `m3c`'s, and
proposal §9 R5/R7/R8 already names them there.

---

## 10. Migration / rollout

**No migration.** `triggers` and `timers` are unchanged since `0001_core_tables.sql` (M0),
`idx_triggers_status_fire` is already the index `Due` needs, and `docs/03-data-model.md` and the
schema golden are untouched. `store_api.golden` widens and that is a reviewed diff, not a schema
change.

**No feature flag.** Two behaviour changes are user-visible the moment they merge: a `timer`
capture stops answering "not wired up yet" and starts scheduling (PR 4a), and `nooma check` starts
transitioning rows (PR 6). Neither can be staged behind a flag without shipping a branch nothing
takes.

**Rollback** is per-PR and clean: reverting PR 4a restores the refusal (the retired symbols come
back with it, since they are deleted in one commit); reverting PR 5a/6 leaves armed rows sitting
in the vault untouched, because nothing else reads them. Rows armed before a revert are inert, not
corrupt — which is the honest property of a design where arming and scanning are separate PRs.

---

## 11. Owner-review items and open questions

Numbered so `sdd-tasks` and the owner can answer by reference. None blocks `sdd-tasks`; each has a
decided default that ships if the owner is silent.

| # | Item | Decided default | What a different answer costs |
|---|---|---|---|
| **R1** | **Transitions take no `to` parameter** (§3.1), deviating from proposal R6's literal "mirror `UnitRepo.SetStatus`" | One method per transition; the `from` precondition lives in the SQL `WHERE` | Option A restores the signature at the cost of §3.3's structural layer 2 — the mapping would then be provable only by the L2 and L3 tests, not by the absence of a channel |
| **R2** | **The status vocabularies live in `internal/ports`** (§3.2), not in a new core package | `ports.TriggerStatus` / `TriggerKind` / `TimerStatus`, `ports.StateSource*`'s precedent | A core `internal/core/trigger` package is ~80 lines and a §13-adjacent surface with no core reader; it also reopens `m3a` §3.3 |
| **R3** | **`ports.ActionCaptureHookDeferred` is deleted** (§3.5) rather than kept as read-only history | Deleted; `Since` applies no vocabulary gate (`decisionlog.go:99`), so historical rows still read | Keeping it costs one doc comment and leaves a bucket the glass box can never produce |
| **R4** | **Four `decision_log` actions for arming** (three effects plus one refusal), not one with the armament in context (§3.5) | Four | One merged `capture.armed` is ~30 lines lighter and makes `m3d`'s re-arm action read oddly beside it |
| **R5** | **`LiveFocusCandidates` ships with no production caller** (§3.7) | Ships in PR 3, proven by `repocontract` | Moving it to `m3d` pushes the largest slice toward proposal §5's own nine-PR split threshold |
| **R6** | **Eight PRs, not the proposal's six** (§7), with a ninth contingency on 5a | Eight | Six means two PRs land over the 400-line ceiling as `size:exception` |
| **Q1** | Should `nooma check` accept `--dry-run` (scan and report, transition nothing)? Genuinely useful for the manual exit gate; genuinely a second code path through the runner | **Not decided.** Recorded so it is not improvised at apply time | — |
| **Q2** | `Due` is unbounded on both tables (§3.6). At what vault size does that stop being acceptable, and does the answer belong to `m3d`'s tick rather than to this port? | One constant and one `LIMIT`, deferred; `LiveDecayStates` carries the identical unmitigated risk today | — |

---

## 12. Risks this design adds or sharpens

| # | Risk | Mitigation |
|---|---|---|
| A | **The status columns have no `CHECK` constraint** (§1), so nothing in the schema stops a core verdict string from being stored. A published migration is never modified, so this cannot be closed structurally in `m3b` | Three layers (§3.3), of which layer 4's `SELECT DISTINCT` is the only one that reads the database rather than the code. Its RED must be watched |
| B | **The `OutcomeDeferred` retirement reaches four packages** (§3.5) — twelve Go files carry the symbol. A partial retirement compiles only because `AllCaptureOutcomes` makes the switch total | One retirement commit ahead of the arming commit; PR 4a's own contingency at ~150 lines |
| C | **`i04_timer_never_a_unit_test.go:36-46` asserts its own vacuity** and stops being true the moment PR 1 lands. A PR that leaves that paragraph is a comment claiming the opposite of its own file | The deletion is in the test commit, before the port commit (proposal §6 order 4). Named as a defect condition, not a review preference |
| D | **`prospection.Anchor` has no JSON tags** while the column comment specifies lowercase keys (§3.4). Every test that round-trips the struct passes; only a test reading the stored TEXT fails | The L3 fixture asserts the stored bytes. This is `m3a`'s `clampedDate` lesson applied: a defence adjacent to the probe is not a defence of the probe |
| E | **`timers` has no index** (§3.6), so its `Due` is a full scan on every tick — five minutes apart, forever, once `m3d` lands | Named, not mitigated. Same posture as `LiveDecayStates`; Q2 records the question rather than inventing a bound |
| F | **`prospection.AllVerdicts()` is a core addition whose only consumer is a test** | The `AllKinds`/`AllDecisionActions`/`AllCaptureOutcomes` precedent is exactly this, and each states the reason. It lands in PR 5a, which carries a genuine doc 02 §7 delta, so `docs-sync` (proposal R13) has something real to see |
| G | **Estimates run low** — 1.3×–4.3× measured across M0 and M1 | §7 states the multiplier and the band up front, and names 5a's cut before apply rather than during it |

---

## Reconciliation note — 2026-08-21 (F1)

**F1 — a deliverable trigger's transition.** `design.md`'s mapping table read `VerdictDeliver` →
`fired` for both a trigger and a timer; `spec.md` required the trigger to stay `armed`. The two
were written concurrently and never read each other, which is how the disagreement surfaced at all.

**Resolved in the spec's favour**, corrected in §3.3, for a reason neither document stated. The
spec argued ownership — real delivery needs `Interrupt.Route()`'s push/digest split, which is
`m3d`'s. That is true but not decisive, since `fired_at` and `surfaced_at` are two separate
moments in doc 02's own lifecycle (`02:785`) and firing ahead of surfacing is representable.

The decisive argument is **liveness**. §3.6 puts the `armed` precondition in the `UPDATE`'s `WHERE`
clause, so a trigger moved to `fired` never matches the due scan again. With no channel in `m3b`,
nothing can surface it either. It would sit `fired`, `surfaced_at` NULL, outside the staleness
gate's jurisdiction and unreachable by anything this change ships. Left `armed`, it stays under the
gate and expires on its own if `m3c`/`m3d` are delayed — the honest outcome for a nudge nobody
could deliver.

## Owner decisions — 2026-08-21

- **Risk A — no `CHECK` on the two status columns: accepted, no migration.** The L3
  `SELECT DISTINCT status` test is the constraint. It is a smaller gap than it reads as, because
  §3.1 already removed the channel: with no `to` parameter on a transition method, a
  `prospection.Verdict` has nowhere to travel to a column. The L3 test is the net for a *future*
  method that reintroduces one, not the only thing standing between core and the schema today.
  Migration `0004` was considered and declined: SQLite cannot add a `CHECK` to an existing table
  without recreating it and copying the rows, and M4 extends both tables — so the cost lands twice
  and the benefit duplicates a guarantee the port shape already gives.
- **Q1 — `nooma check --dry-run`: yes, in scope.** It reports what the pass would fire and expire
  and writes nothing, which is what makes a real vault inspectable before the first live run at a
  point in M3 where no UI exists to look at state afterwards. It is also the natural shape for the
  scan: the decision half is already pure (`m3a`), so the flag suppresses the effect rather than
  branching the logic — which `sdd-tasks` should hold it to, since a dry run that takes a different
  path is a dry run that proves nothing about the wet one.
- **R1/R2 — the missing `to` parameter, and status vocabularies in `internal/ports`: accepted as
  designed.** Both deviate from the proposal's literal wording, and both are argued in §3.1 and
  §3.2 on grounds the proposal did not have. R1 in particular converts a review obligation into a
  structural impossibility, which is the direction this chain has repeatedly paid to learn.
- **Q2 — unbounded `Due` scans, no index on `timers`: accepted for v1.** A single-user local vault
  has no scale at which it matters, and adding an index now would be a published-migration change
  for a problem no measurement has shown. Revisit if M4's activity view makes the scan hot.

## Reconciliation note — 2026-08-21 (G1, and what F1 did not propagate)

**G1 — §3.6 still carried `check.trigger.fired` after §3.3's F1 correction.** Found by `sdd-tasks`
reading §3.3, §3.6, §12 and spec R5.3 against each other. The F1 edit changed the mapping table and
did not follow through into the pipeline sketch, the action list, or the action count. Corrected:
eight members added rather than nine, `AllDecisionActions()` reaching thirty-one rather than
thirty-two, and no `check.trigger.fired` at all — an action with no producer is a vocabulary member
that looks implemented and is not.

**A third site F1 missed, and neither `sdd-tasks` nor the F1 edit itself named it.** §3.6's
concurrency proof was written as two passes racing to fire the same **trigger**. After F1 a trigger
has no transition in this change, so both passes would correctly write nothing, the conflict arm
would never be reached, and the test would pass while proving the opposite of what it claims — the
"guard that cannot fail on its own violation" shape, arrived at not by writing a weak test but by
correcting one document and leaving a test designed around the old behaviour. The fixture is now a
timer, which is the only thing that transitions here.

**G2** — spec R1.2 wanted `TimerRepo.Fire` to take `rendered_text`/`surfaced_at`; resolved in the
design's favour (no parameter with no caller, `UpdateEventAt`'s precedent). **G3** — spec R0's "no
`internal/core` touch" versus `prospection.AllVerdicts()`; resolved in the design's favour, since
it states no new decision and is what lets the Risk-A guard fail on its own violation.
