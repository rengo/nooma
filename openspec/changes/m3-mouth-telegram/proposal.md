# Proposal — M3: the mouth (Telegram + prospection)

Deliver M3 as laid out in [`docs/05-build-plan.md`](../../../docs/05-build-plan.md) §M3
(lines 163-177): the Telegram adapter over long polling
([ADR-0014](../../../docs/adr/0014-telegram-transport.md)), triggers armed at capture with a due
scan, digest and interruptive push, ephemeral timers end to end, and the conversational check-ins
driven by [`docs/02-cognitive-core.md`](../../../docs/02-cognitive-core.md) §5's orthogonal
classify fields.

M1 made the binary answer when spoken to. M2 made it act while nobody is looking. **M3 is the
change where it speaks first** — and therefore the first change whose failure mode is not a wrong
answer but an unwanted interruption. ADR-0009 already named the stake: *"nudges that arrive late
and out of context… teaches the user to ignore notifications. And once they ignore them, the entire
proactive lobe stops being worth anything."*

---

## 1. Why now

M2 closed on 2026-08-19 at `074033a`. What it left is a brain that decides things it cannot say.

| Fact (verified 2026-08-19 at `e1a20aa`) | Consequence |
|---|---|
| `triggers` and `timers` are created by `internal/store/sqlite/migrations/0001_core_tables.sql:42-70`, unchanged since M0 | **M3 needs no migration for its own storage.** A published migration is never modified; these two tables already carry every column doc 02 §7 and §8 name |
| `internal/core/prospection/doc.go:1` is the whole of the package — *"Package prospection decides staleness, quiet hours, digest vs push and recurrence"* | Every decision M3 owns is greenfield, and the package comment already states its charter |
| `internal/channels/` holds a `doc.go` and nothing else. No `telegram/` subpackage exists | The channel is greenfield too |
| `internal/config/config.go:73-81` declares `Telegram{Enabled, BotTokenEnv, AllowedChatIDs}`, and `internal/config/validate.go:78-91` already refuses `enabled: true` with an empty allow-list or an unset token | **ADR-0006's structural safe default is already built**, ahead of schedule, in M0. M3 wires an adapter that honours it; it does not build the gate |
| `internal/config/config.go:88` decodes `schedules.proactive_check` (default `*/5 * * * *`), and nothing reads it | The tick is configured and does not exist |
| `internal/brain/capture.go:309-322` (`timerHookRefusal`) answers every `timer` and `recurring_reminder` capture with *"timers and recurring reminders aren't wired up yet — nothing was scheduled, and this capture was not stored"* | M3's first user-visible act is deleting that sentence |
| `internal/core/classify` decodes all six orthogonal outcome fields; no file under `internal/brain/` references any of them | The check-ins are classified and then dropped on the floor |
| `interrupt_level` appears in exactly one place in the tree — the migration comment at `0001_core_tables.sql:48` | Every delivery decision in doc 02 §7 keys off a number nothing produces. See **R1** |
| `internal/ports/staterepo.go:40-52` exposes `OpenHypothesis` and `LastHypothesisAt` — and no read of `current_state.energy`, and no way to resolve a hypothesis | The digest's energy care gate and the `state_outcome` check-in both need this port widened. See **R3** |
| `internal/core/focus` has `Priority`, `Rank`, `Select`, `Hysteresis` and **no importer outside its own package** | M2's discharge note (`m2-sleep-weight/proposal.md:59-65`) deferred the query and its caller here. §3.3 decides what M3 does with it |
| `test/conformance/` has no `i15`, `i16` or `i17` file | The three invariants M3 exists to satisfy have no test yet |

The schema needs nothing. The config schema needs nothing. **M3 is code and decisions over a
substrate that is already complete** — which makes its real cost, as in M2, the decisions doc 02
gestures at without defining (§4.2).

---

## 2. Success criteria

The change is done when:

- [ ] A `timer` capture arms a real timer and says so. `timerHookRefusal` is gone, and a timer is
      still **never a unit** (**I04**) — asserted, not inherited: today's test proves that half
      structurally because no `TimerRepo` exists (`i04_timer_never_a_unit_test.go:36-46`), and m3b
      destroys that argument.
- [ ] Timers are armed, listed, cancelled and fired, with the LLM rephrasing `rendered_text` at
      fire time and the request stored verbatim (doc 02 §8).
- [ ] A dated `event` capture arms a `time_based` trigger at its lead time; a `recurring_reminder`
      arms one with `recurrence_rule` + `recurrence_anchor`.
- [ ] A trigger overdue by more than `trigger_staleness_hours` becomes `expired`, **never** `fired`,
      with a `decision_log` row explaining the age (**I15**).
- [ ] A timer overdue by more than `timer_staleness_hours` becomes `cancelled`; one inside the
      window is delivered and its text **mentions the delay explicitly** (ADR-0009).
- [ ] Nothing is delivered during quiet hours `[00:00, 07:00)` except the defined push exception,
      and deferred items resurface on waking (**I16**).
- [ ] `interrupt_level >= 0.7` pushes immediately, skipping cadence and gates; below it the item
      goes to the digest; the two are mutually exclusive — no double delivery (doc 02 §7).
- [ ] Firing a recurring trigger creates the next one pointing at the **same** source unit
      (**I17**); memory is not duplicated, only the nudge is re-armed.
- [ ] The four check-ins resolve from an ordinary message: `nudge_outcome`, `relation_outcome`,
      `state_outcome`, `task_checkin_outcome` — and one message can resolve a check-in *and* be a
      capture at the same time (doc 02 §5).
- [ ] Rejecting a relation emits `relation_reject` **before** deleting it (**I10**).
- [ ] Every delivery, expiry, cancellation and re-arm writes a `decision_log` row from
      `internal/brain`, never from `internal/core` (**I12**).
- [ ] The Telegram adapter opens **no inbound port** and refuses any chat id outside
      `allowed_chat_ids` at receipt time (ADR-0014, ADR-0006).
- [ ] A late delivery after downtime is handled as a normal case, not an error (ADR-0014).
- [ ] Every number M3 introduces is a named constant under `internal/core/prospection` **and** a row
      in doc 02 §13 — including the rows §13 currently leaves unnamed (§4.3).
- [ ] `make check-all` green and CI green on every PR in the chain.
- [ ] No test touches the network or a real LLM; every Telegram test runs against an
      `httptest.Server` through an injectable `baseURL`, mirroring
      `internal/providers/anthropic/client.go`.
- [ ] **Demo**: "remind me in 15 min about X" over Telegram, and a real morning digest.

The last bullet is the build plan's own wording (`docs/05-build-plan.md:177`) and it is the exit
criterion. §7 states the one part of it that cannot be automated.

---

## 3. Scope

### 3.1 The boundary rule

> **M3 implements everything that reaches the user and everything the user's reply resolves. It
> implements no new way of *storing* what the user says, and it consumes no learning signal.**

M3 arms, scans, expires, defers, delivers, re-arms and resolves. It emits learning signals where an
invariant demands it (I10), and it **tunes nothing from them** — that is M5.

### 3.2 In scope

1. **`internal/core/prospection`** — the trigger and timer staleness gates, the quiet-hours gate,
   the digest-vs-push split, the digest cadence and care gates, the recurrence re-arm, the lead-time
   rule, and the arming decision. All pure; the instant arrives as a parameter.
2. **`internal/ports`** — `TriggerRepo` and `TimerRepo`; a channel port (outbound publish, inbound
   receipt) that names no vendor; a widened `StateRepo` (read `energy`, resolve a hypothesis); the
   focus-candidate query M2 deferred (§3.3).
3. **`internal/store/sqlite`** — those repositories, over tables that already exist. Widens
   `testdata/schema/store_api.golden`; **no migration**.
4. **`internal/brain`** — arming at capture (deleting `timerHookRefusal`), the due-scan /
   proactive-check service in `ConsolidateService`'s shape (one clock read, pure decisions, persist,
   `decision_log`, publish), digest assembly, the fire-time rephrasing call, and the four check-in
   resolutions.
5. **`internal/channels/telegram`** — the long-polling client (`getUpdates` / `sendMessage`),
   `allowed_chat_ids` enforcement, backoff and offset handling, all behind an injectable `baseURL`.
6. **`internal/scheduler`** — the `proactive_check` tick, reusing the existing non-blocking
   try-lock and shutdown-join shape (`scheduler.go`, `cron.go`).
7. **`cmd/nooma`** — `nooma check [vault]` (the CLI twin of the tick, as `nooma consolidate` is the
   twin of the 03:00 cron) plus channel and tick wiring into `serve`. Adds a row to
   `docs/01-architecture.md:157-161`'s command table.
8. **The doc 02 amendments of §4.2** — seven behaviours doc 02 names without defining, each landing
   in the PR that implements it, per `CLAUDE.md` non-negotiable #1.
9. **The §13 calibration work of §4.3** — including two rows that cannot be checked as written
   today.

### 3.3 The focus query — M2's carry-over, decided here

M2's discharge recorded that `internal/core/focus`'s query and its production caller "land in M3".
Verified still true: no `ports.UnitRepo` method returns focus candidates, no `ORDER BY` over `units`
exists in `internal/store/sqlite`, and nothing imports the package.

**M3 takes it, and the reason is not tidiness.** Doc 02 §7 says the low-energy care gate "holds back
non-urgent items and only lets important ones through". *Important* is undefined. Without
`focus.Priority` the implementation invents an importance predicate at apply time — the exact
failure M2's §4.2 was written to prevent — while `focus.Priority` is already specified, reviewed and
100%-covered. The alternative (leave it to M4's today/focus view) is real and cheaper, and it is
**Q4**.

One limit, stated now: `triggers.unit_id` is nullable for `pattern_based` triggers
(`0001_core_tables.sql:44`), so a pattern trigger has no source unit and therefore no priority. The
care gate needs a defined answer for that case rather than a nil dereference.

### 3.4 Explicit non-goals, each with its reason

- **No webhook, no inbound port, no second listener.** ADR-0014 is not an option flag; reversing it
  needs a superseding ADR. `nooma doctor`'s exposure report stays complete because there is nothing
  else listening.
- **No second channel.** ADR-0006 makes Telegram the v1 channel. The port names no vendor precisely
  so WhatsApp needs no core change later — but M3 ships one adapter.
- **No learning.** M3 *emits* `relation_reject` because I10 pairs emission with deletion and M3
  builds the deletion. It consumes no signal, tunes no threshold, and writes nothing to
  `relation_thresholds` or `calibration`. Doc 05 gives that to M5.
- **No producer of `incomplete` units.** M2's Q3 said the ambiguity-asking surface "is M3's". M3
  declines it, with an argument rather than a deferral — see **Q3**. **I06 stays honestly out of
  scope rather than vacuously green**, for the third milestone running.
- **No UI.** Timers are "listable and cancellable from chat and from the UI" (doc 02 §8); M3 ships
  the chat half and `nooma check`. The UI is M4.
- **No `list_op`, no `person_ref_status` consumption.** Both are §5 orthogonal fields, and neither
  is one of the four check-ins `docs/05-build-plan.md:175-176` names. `list_op` needs a list surface
  (M4); `person_ref_status` is Q3.
- **No `pattern_based` trigger *production*.** M2's `pattern_eval` writes a `current_state` row and a
  stagnation finding; M3 delivers what exists. Turning findings into armed `pattern_based` trigger
  rows is a consolidation change, not a delivery one — named because "the watchers are
  half-satisfied at the end of M2" (`m2-sleep-weight/proposal.md:189-192`) invites the assumption
  that M3 completes them. It completes their *delivery*.
- **No `reindex`, no export/import, no perception.** M6 and v2.

### 3.5 Invariants in scope, traced

| # | Invariant | Doc 02 | M3 status |
|---|---|---|---|
| I15 | Overdue past the threshold → `expired`, never `fired` | ADR-0009 | **In scope, new test.** No file exists |
| I16 | Nothing delivered in quiet hours except the push exception | §7 | **In scope, new test** |
| I17 | A recurring trigger's next one points at the **same** unit | §7 | **In scope, new test** |
| I04 | A timer is never a unit | §8 | **Test exists and stops being structural.** `i04_…_test.go:36-46` says its timers/triggers half holds "by construction, because there is nothing here to query". m3b adds the thing to query |
| I10 | Rejecting a relation emits `relation_reject` **before** deleting | §4, §9 | **In scope, new test.** M3 builds the first rejection surface |
| I09 | The `[persist, surface)` band is stored **and** asked about in the digest | §4 | **In scope.** M3 builds the first digest; until now the band had nowhere to be asked |
| I12 | Every effectful automatic decision logs | §11 | In scope and load-bearing — arming, expiry, deferral, delivery, re-arm and resolution are all effects |
| I18 | `event_at`, `created_at`, `due_at` are never interchanged | §1 | In scope — arming from a dated event is the first path that reads one to compute a `fire_at` |
| I02 | Live reads exclude `superseded` and `incomplete` | §1 | In scope — the digest is a new live read surface |
| I03 | Nothing deleted; archiving is a transition | §1 | In scope with a **named exception**: I10 requires a relation *deletion*. I03 is scoped to `units`; the new ports must keep `i03_units_never_deleted_test.go`'s no-`Delete`-prefix property honest while `RelationRepo` gains one |
| I11 | The 8 phases in order | §6 | Untouched — M3 adds a second scheduled pass, not a ninth phase |
| I06 | An `incomplete` unit has no embedding until promoted | §1 | **Out, honestly.** No producer — Q3 |
| I13, I20 | — | §9, §12 | Out. Signal consumption is M5, insights are v2 |

---

## 4. Approach

### 4.1 Where the boundary falls

| Decision | Package | Why there |
|---|---|---|
| Given `(fire_at, now, kind, threshold)`, fire / expire / cancel? | `core/prospection` | ADR-0009's own claim, verbatim: *"a pure function… tested entirely without a scheduler, without a real clock, and without a database"* |
| Given an instant, is it inside quiet hours? | `core/prospection` | Pure over the instant's own zone (§4.4) |
| Given `interrupt_level`, push or digest? | `core/prospection` | One comparison, one threshold, one §13 row |
| Given pending items, an energy reading and a cadence, what does this digest contain? | `core/prospection` | Data in, data out |
| Given a fired recurring trigger, what is the next one? | `core/prospection` | Calendar arithmetic over `(rule, anchor, now)` |
| Given a classification, what gets armed? | `core/prospection` | The same shape `classify.Kind.UnitType()` already has: a decision about a value |
| Read the clock once, run the gates, persist, log, publish | `brain/` | Orchestration, exactly `ConsolidateService`'s shape |
| Rephrase a fired timer's text | `brain/` | An LLM call. The *decision* to add a delay caveat is the staleness gate's pure output |
| `getUpdates` loops, offsets, backoff, chat ids | `channels/telegram` | Adapter. Invisible to core and brain, as `providers/anthropic` is to `core/classify` |
| Tick every 5 minutes | `internal/scheduler` | Adapter over real time |
| `nooma check`, `serve` wiring | `cmd/nooma` | Wiring |

**Long polling versus webhook does not reach the port.** The brain-facing contract is symmetric
either way — *publish this text to this chat* and *here is text from this chat*. ADR-0014 decides
only where the transport loop lives. That is what makes `internal/channels/telegram` replaceable
without touching a decision.

One existing gate helps here and should not be broken:
`test/conformance/scheduler_boundary_scan_test.go` fails any non-test file under
`internal/scheduler` containing the literal `time.Hour`, and requires every named
`internal/core/consolidation` decision to have a call site there. M3's tick must extend the second
leg with its prospection symbols rather than route around the first.

### 4.2 The seven things doc 02 does not define

This is the milestone's real content, stated here so `sdd-apply` does not improvise it.

| Behaviour | What doc 02 says today | What M3 must supply |
|---|---|---|
| **`interrupt_level` at arming** | §7: "persisted on the trigger so the glass box can audit it" | **Where the number comes from.** Nothing produces it (**R1**): classify does not emit it, and §13 gives only the 0.70 comparison threshold |
| **Digest cadence** | §7: "with a cadence" | The cadence. §13 has **no row** for it, and the demo asks for a *morning* digest, so an hour must be chosen too |
| **"Important ones"** | §7: the low-energy gate "only lets important ones through" | The predicate. §3.3 proposes `focus.Priority`; Q4 decides |
| **Anti-starvation** | §7: "deferred items resurface on recovery" | What "recovery" is, and how many deferrals an item survives before it is delivered regardless |
| **Quiet-hours resurfacing** | §7 / ADR-0009: "deferred and resurfacing on waking" | 07:00 exactly, or the first tick after 07:00. A 5-minute tick makes these different by up to five minutes, every day |
| **Recurrence arithmetic** | §7: `yearly \| monthly` + `{month, day}` | The next occurrence, including 29 February and day 31 in a 30-day month. Both are ordinary anniversaries, not exotic input |
| **The delay caveat's wording rule** | ADR-0009: the text "mentions the delay explicitly" | At what overdue amount the caveat appears. A timer three minutes late that announces "three minutes ago you asked me…" is noise of a different kind |

Each is an `sdd-design` output subject to owner review, landing with its doc 02 amendment and its
§13 rows in one PR. Implementing against prose that cannot produce a testable invariant is how a
conformance suite becomes decoration — M2's §4.2, unchanged.

### 4.3 The calibration consequence, and two rows that cannot be checked as written

`test/conformance/calibration_doc_test.go:35` matches only `internal/core/([a-z0-9/]+)\.([A-Z]…)`.
A §13 row starts being checked the day it names a core constant. M3 names five:

| §13 row | Today | After M3 | PR |
|---|---|---|---|
| `trigger_staleness_hours` (line 920) | `6`, no constant | `internal/core/prospection.TriggerStalenessHours` | m3a #1 |
| `timer_staleness_hours` (line 921) | `3`, no constant | `internal/core/prospection.TimerStalenessHours` | m3a #1 |
| Push threshold (line 912) | `0.70`, no constant | `internal/core/prospection.PushThreshold` | m3a #3 |
| Event lead time (line 914) | `7 days`, no constant | `internal/core/prospection.EventLeadDays` | m3a #5 |
| Quiet hours (line 913) | `[00:00, 07:00) local` | **splits into two rows** — see below | m3a #2 |

Two rows need more than a constant name, and both are found by reading the gate rather than the doc:

- **`Quiet hours` must become two rows.** Its Default cell is `[00:00, 07:00) local`, and the gate
  parses a row's value with `^-?\d+(?:\.\d+)?` anchored at the start (`calibration_doc_test.go:46`).
  A cell starting with `[` yields no number. One row per bound —
  `prospection.QuietHoursStartHour` = 0 and `prospection.QuietHoursEndHour` = 7 — is the shape the
  gate can check, and it is also the shape the code has.
- **§13 line 918's own instruction is not achievable as written.** The row says splitting it "so the
  consolidation half can be checked is M3's job". But the consolidation half names
  `internal/scheduler.ConsolidationHour`, and the gate's regex never matches
  `internal/scheduler/…` — M2's own discharge note recorded this (`m2-sleep-weight/proposal.md:114-117`),
  and the row's prose blames only the `03:00` parse. **Splitting the row does not make it
  checkable.** M3 splits it *and* corrects the prose, in `feat/scheduler-proactive-tick`. Making it
  genuinely checkable would mean moving the hour into `internal/core` — proposed as **Q5**, not
  assumed.

### 4.4 Quiet hours, and the timezone question that is not open

Doc 02 already decided this and it is not reopened here.
`docs/02-cognitive-core.md:600-610`: *"The user's timezone reaches the model inside the instant,
never from the environment… This is why there is no timezone setting anywhere in Nooma's
configuration. Adding one would create a second source of truth."* The known limit — a vault hosted
for a user in a zone other than the process's — is named there as multi-tenancy, deliberately out of
v1.

So the quiet-hours gate evaluates `[00:00, 07:00)` against the zone carried by the instant
`ports.Clock` supplies, exactly as `internal/core/classify/prompt.go:28-34` already documents for
capture: *"The zone travels inside the instant rather than arriving from configuration or the
environment… A test Clock fixing a Location is what makes these assertions stable."*
`ports.Clock.Now()` returns a `time.Time` (`internal/ports/clock.go:18-20`), and a `time.Time`
carries a `Location`. **No config key, no ADR, no open question.** A scheduler-triggered decision is
not a special case: the brain reads its clock once per pass and passes the instant down, which is
the same discipline `ConsolidateService` already runs under.

### 4.5 What the pass sees as "now"

`brain`'s proactive-check reads `ports.Clock` **once** per pass and every gate receives that one
instant — required by `brain_single_clock_read_test.go`, and correct on the merits. Consequence
worth writing down: an item that crosses the 07:00 boundary *during* a pass is judged by the pass's
start instant. At a 5-minute cadence the worst case is one extra tick of deferral, which is the
intended semantics and is what makes the demo reproducible.

---

## 5. The chain

Four phase changes, sharing this proposal. **The dependency graph is not linear**, and that is the
one structural difference from M2:

```
                 ┌──> m3b-trigger-timer ──┐
m3a-prospection ─┤   (ports, store,       ├──> m3d-delivery-demo
  (pure)         │    arming, due scan)   │    (tick, digest, push,
                 └──> m3c-telegram ───────┘     check-ins, wiring, demo)
                     (port + adapter)
```

`m3b` and `m3c` are a genuine two-track split: **neither depends on the other.** `m3b` touches
`internal/ports/{triggerrepo,timerrepo}.go`, `internal/store/sqlite`, `internal/brain` and
`store_api.golden`; `m3c` touches `internal/ports/channel.go` and `internal/channels/telegram`, and
its inbound half needs only M1's existing `CaptureService`. They converge in `m3d`. `m3c` depends on
`m3a` at *design* level only — it needs to know what shape of thing it will be asked to send — so it
can start as soon as `m3a`'s design is reviewed, not when `m3a`'s last PR merges.

**`m3a-prospection`** — pure. `internal/core/prospection` in full: both staleness gates, quiet hours,
the delivery split, the digest gates, recurrence, lead time, arming. Zero ports, zero I/O. Owns
I15's, I16's and I17's pure halves and five of §4.3's §13 rows. Depends on nothing.
*Exit*: every gate in §4.2 has a named constant, a §13 row and a conformance test, with no adapter
in the tree.

**`m3b-trigger-timer`** — `TriggerRepo` and `TimerRepo` with their SQLite implementations, the
focus-candidate query (§3.3), arming at capture (`timerHookRefusal` deleted), the due-scan runner in
`ConsolidateService`'s shape, and `nooma check`. Delivery is a log line — no channel. Depends on
`m3a`. Owns I15's behavioural half, I04's strengthening, I12 across the new effects, I18 at arming.
*Exit*: `nooma check` on a vault fires an armed timer, expires a stale trigger, and `decision_log`
tells the story — with Telegram nowhere in the picture.

**`m3c-telegram`** — the channel port and `internal/channels/telegram`: long polling,
`allowed_chat_ids` enforcement at receipt, token from `bot_token_env`, backoff, offset and shutdown,
and inbound messages reaching `brain.CaptureService`. Every test runs against `httptest`. Depends on
`m3a` (design). Owns ADR-0014's and ADR-0006's structural claims.
*Exit*: a message posted to a fake Telegram server becomes a capture and gets a reply; a message from
a chat id outside the allow-list becomes nothing, audibly.

**`m3d-delivery-demo`** — the `proactive_check` tick, push with quiet-hours deferral, digest assembly
with its care gates, fire-time rephrasing, the four check-in resolutions, `serve` wiring, and the L4
demo. Depends on `m3a` + `m3b` + `m3c`. Owns I16's and I17's behavioural halves, I09, I10, and
§4.3's row-918 split.
*Exit*: the build plan's own demo bullet, against a fake Telegram — plus §7's manual pass.

**Why four and not five.** `m3d` is the largest slice (§5.1: eight PRs), and the obvious fifth cut is
the check-ins. They stay in `m3d` because a check-in is the *answer half* of a delivery: splitting
them puts the question in one change and the reply in another, and neither is reviewable as a loop.
If `sdd-tasks`'s forecast for `m3d` exceeds **nine** PRs or **3,200** budgeted lines, the check-in
pair splits off as `m3e-checkins` between `m3b` and `m3d`. That threshold is named now so the split
is a measurement, not a mood.

Chain strategy `stacked-to-main`, delivery `auto-chain` — M1's and M2's own.

### 5.1 Per-PR budgets

**These are guesses.** Every number is a budget chosen to respect the repository's 400-line soft
ceiling (implementation + docs, excluding tests — `CLAUDE.md`, `docs/06-harness.md` §7), not a
prediction. This project has measured its predictions low six times in M0 (1.3x–2.2x) and once at
4.3x in M1 Phase B.

| Change | PR | Content | Est. |
|---|---|---|---|
| **m3a** | `feat/core-prospection-staleness` | both gates (**I15**), delay-caveat predicate; two §13 rows | ~300 |
| | `feat/core-prospection-quiet-hours` | **I16**'s gate; §13's `Quiet hours` row split in two | ~300 |
| | `feat/core-prospection-delivery-split` | push vs digest at 0.70; §13 push-threshold row | ~300 |
| | `feat/core-prospection-digest-gates` | cadence, energy care gate, anti-starvation; doc 02 §7 amendment | ~400 |
| | `feat/core-prospection-recurrence` | **I17**'s next occurrence, calendar edges; event lead time | ~350 |
| | `feat/core-prospection-arming` | classification → what gets armed, incl. R1's answer | ~350 |
| **m3b** | `feat/ports-trigger-timer` | both ports + memrepo fakes | ~350 |
| | `feat/store-trigger-timer` | SQLite over the existing tables; `store_api.golden` | ~400 |
| | `feat/ports-store-focus-candidates` | M2's carry-over query and its `ORDER BY` | ~300 |
| | `feat/brain-arm-at-capture` | `timerHookRefusal` deleted; **I04** strengthened; **I18** | ~400 |
| | `feat/brain-due-scan` | one clock read, gates, transitions, `decision_log` (**I15**) | ~400 |
| | `feat/cli-check` | `nooma check [vault]` + doc 01's command table row | ~250 |
| **m3c** | `feat/ports-channel` | the vendor-free channel contract + its fake | ~250 |
| | `feat/channels-telegram-client` | `getUpdates`/`sendMessage` over an injectable `baseURL` | ~400 |
| | `feat/channels-telegram-allowlist` | `allowed_chat_ids` at receipt; token from `bot_token_env` | ~300 |
| | `feat/channels-telegram-resilience` | backoff, offset, shutdown; ADR-0014's "late is normal" | ~350 |
| | `feat/channels-telegram-inbound` | inbound → `CaptureService` → reply | ~350 |
| **m3d** | `feat/scheduler-proactive-tick` | the 5-min tick; §13 row 918 split **and** its prose corrected | ~350 |
| | `feat/brain-push-delivery` | push ≥ 0.7 with quiet-hours deferral (**I16**) | ~350 |
| | `feat/brain-digest-assembly` | cadence + care gates + anti-starvation (**I09**) | ~400 |
| | `feat/brain-timer-fire-rephrase` | the LLM rephrasing call and the delay caveat | ~300 |
| | `feat/brain-checkin-nudge-task` | `nudge_outcome`, `task_checkin_outcome` | ~350 |
| | `feat/brain-checkin-relation-state` | `relation_outcome` (**I10**), `state_outcome`, `StateRepo` widened | ~400 |
| | `feat/serve-channel-scheduler-wiring` | wire both into `runServe`; shutdown ordering | ~250 |
| | `feat/demo-timer-and-digest` | the L4 demo against a fake Telegram | ~400 |

Twenty-five PRs, ~8,500 budgeted lines. Read against the measured multipliers, realistically
**11,000–18,000 lines across 32–45 PRs**.

One saving worth naming: **M3 needs no migration.** Both tables have existed since M0, so
`schema_doc_test.go`'s anchor list, `docs/03-data-model.md` and the schema golden stay untouched —
the one cost M2 spent a whole open question (its Q1) avoiding.

---

## 6. Strict TDD ordering

Strict TDD is active, and the mechanism is M2's §6 unchanged: the conformance test is its own
commit ahead of the implementation commit inside each PR; `sdd-tasks` orders the test task strictly
before the implementation task; `sdd-verify` reads the PR's `git log` and reports an inversion as
CRITICAL; and `core_exported_decls_have_tests_test.go` stays as the standing presence guard over
`internal/core/**`.

| Order | Invariant | Change | Why here |
|---|---|---|---|
| 1 | **I15** | m3a #1 (pure), m3b #5 (behavioural) | ADR-0009's three Context questions become three tests, then the runner proves the transition is `expired` and never `fired` |
| 2 | **I16** | m3a #2 (pure), m3d #2 (behavioural) | The pure gate first; then that no delivery path skips it except the defined push exception |
| 3 | **I17** | m3a #5 (pure), m3d #3 (behavioural) | The next occurrence as arithmetic; then that the created row carries the **same** `unit_id` |
| 4 | **I04** | m3b #4 | **Strengthened, not written.** Its timers/triggers half is true today only because no port exists to query (`i04_…_test.go:36-46`). The PR that adds `TimerRepo` is the PR that must make it a real assertion — in the test commit, before the port |
| 5 | **I18** | m3b #4 | Arming from a dated event is the first path that reads `event_at` to compute a `fire_at`. Existing invariant, first real load |
| 6 | **I12** | m3b #5, m3d #2–#6 | Both directions: an effect always logs, and a scan that decided nothing writes nothing |
| 7 | **I09** | m3d #3 | The first digest is the first place the `[persist, surface)` band can be asked about |
| 8 | **I10** | m3d #6 | New test. Ordering is the whole invariant: `relation_reject` is recorded **before** the delete, so a failed signal write leaves the relation standing (I23's shape, applied to relations) |
| 9 | **I03** | m3d #6 | Existing structural test, and the one place M3 could break it by accident: I10 adds a `Delete` to `RelationRepo`, and `i03_units_never_deleted_test.go`'s scope must stay `units` deliberately rather than by luck |

---

## 7. The demo boundary, stated honestly

`CLAUDE.md` non-negotiable #5 — *no test touches the network or a real LLM* — means **every**
automated proof in M3 fakes Telegram at the transport seam: an `httptest.Server` behind an
injectable `baseURL`, mirroring `internal/providers/anthropic/client.go`'s pattern verbatim. The L4
demo drives the compiled binary against that fake, as `test/e2e/demo_test.go` already does for the
provider.

That covers everything except the one thing the milestone is named for. **`m3d` closes with one
manual pass**: a real BotFather token in `bot_token_env`, a real chat id in `allowed_chat_ids`,
"remind me in 15 min about X" typed into a real Telegram client, and a real morning digest arriving.
This is the same shape that actually closed M1 (a live-provider walkthrough, PRs #129–#132) and it is
not a PR. It is the milestone's own exit gate, run by the maintainer, recorded in `m3d`'s archive
report.

Two properties only that pass can prove, and they are the two that matter: that Telegram's real
`getUpdates` semantics match the fake, and that a message from a chat id outside the allow-list is
refused by the real bot rather than by our own mock of it.

---

## 8. Open questions

Each is a decision the owner makes. The recommendation is this proposal's reasoning, not a settled
answer. **This section is also the proposal question round**: answering Q1 and Q2 before `m3a`'s
spec is what keeps the first slice unblocked.

### Q1 — Where does `interrupt_level` come from? *(blocking for `m3a` #6)*

The number every delivery decision in doc 02 §7 keys off exists in exactly one place in the tree:
the migration's column comment (`0001_core_tables.sql:48`). Classify does not emit it; §13 gives only
the 0.70 comparison.

| Option | Pro | Con |
|---|---|---|
| **A. Classify emits it** per message, as it already emits weight and λ | The model sees the words ("urgent", "don't forget") and §5's own precedent is that the model supplies the judgement | Widens the classify contract, the prompt, the decoder and the golden corpus. A new degradable field, so §5.1's table gains a row |
| **B. A table of per-`type` defaults in `core/prospection`** | No LLM change, no new degradation path, one place to calibrate | Invents nine hand-tuned numbers — the exact thing doc 02:585-589 refuses for weight and λ |
| **C. One constant for everything armed at capture; only the user's explicit words raise it** | Cheapest; nothing is a push until the product learns what should be | Everything is a digest item in v1, which makes the 0.70 branch shipped-but-unexercised |

**Recommendation: A, degrading to a single named constant.** Doc 02's own answer for weight and λ
(§5.1, lines 585-593) is *the model supplies it, and a named default fills a degraded value* — this
is the same question with a different field, and answering it differently would be two philosophies
in one prompt. The default keeps C's safety: a degraded classification never produces a push.

### Q2 — What is the digest's cadence, and when is it delivered? *(blocking for `m3a` #4)*

Doc 02 §7 says "with a cadence" and §13 has no row. The demo says *a real morning digest*.

- *Once daily at a fixed morning hour* (recommended): matches the demo's own wording, matches quiet
  hours ending at 07:00, and gives the anti-starvation rule a natural unit (one deferral = one day).
  Needs one new §13 row and one constant.
- *Every N hours*: more responsive, but a digest that arrives three times a day is a push with extra
  steps, and doc 02 built the whole digest/push split to avoid exactly that.

**Recommendation: once daily, at a morning hour named as a §13 row**, with the hour distinct from
consolidation's 03:00 so the two passes never argue over the same instant.

### Q3 — Does M3 turn on the ambiguous-person-reference producer?

M2's Q3 recommended "no" and said *"That surface is M3's."* M3 should either take it or argue.

The argument for declining: the four check-ins the build plan names
(`docs/05-build-plan.md:175-176`) are all **outbound** questions the brain initiates and the user
answers later — and M3 builds their store, because a fired-but-unanswered trigger (`surfaced_at` set,
`responded_at` NULL) *is* doc 02 §5's "open check-ins". An ambiguous person reference is a different
shape: an **inbound, synchronous** ambiguity that blocks a capture, whose existing answer is already
built (`capture_ambiguous_person_ref_test.go`: the unit persists as `pool`, two `decision_log` rows,
no `incomplete` unit). Turning it into a question means deciding what the unit *is* while
unresolved, which reopens M1's Q3a rather than extending M3's mechanism.

**Recommendation: no, and say so in doc 02 rather than leaving it implied.** I06 stays honestly out
of scope, and the sentence "M3 owns this surface" gets corrected in the same PR that would otherwise
inherit it silently. If the owner wants it, it is `m3e` and it costs a capture-path change plus a
pending-question store neither M3 nor M2 built.

### Q4 — Does the digest's "important ones" gate use `focus.Priority`?

See §3.3. *Yes* discharges M2's carry-over and gives the gate a reviewed definition, at the cost of a
new `UnitRepo` query and an answer for `pattern_based` triggers with no source unit. *No* leaves the
query to M4's today/focus view and makes M3 invent an importance predicate — or use
`interrupt_level` itself as the ordering, which is defensible and much cheaper.

**Recommendation: yes.** The cheap alternative (order by `interrupt_level`) collapses the digest's
care gate into the same number the push/digest split already used, so a low-energy day would hold
back precisely the items that were closest to being pushed — which inverts the intent.

### Q5 — Does `ConsolidationHour` move into `internal/core`?

§4.3 shows that splitting §13's row 918 does not make its consolidation half checkable, because the
gate's regex never reaches `internal/scheduler`. Two honest endings: correct the row's prose to say
*why* it is unchecked (cheap, `m3d` #1), or move the hour into `internal/core/consolidation` so the
gate reaches it (a small M2 refactor inside M3).

**Recommendation: correct the prose in `m3d` #1, and record the move as a separate work unit.**
M3 has enough new surface without refactoring a green milestone's constants, and a row that states
its own blind spot is better than a row that quietly implies coverage — which is the state it is in
today.

### Owner rulings — 2026-08-19

The owner answered the whole round at once, taking every recommendation. These are the rulings the
later phases implement; §8's options above are kept as the reasoning that produced them, not as
choices still open.

| # | Question | Ruling |
|---|---|---|
| **1** | Q1 — where `interrupt_level` comes from | **Option A.** Classify emits it per message, degrading to a single named constant when the field is absent or unparseable. §5.1's degradable-field table gains a row; the classify prompt, decoder and golden corpus widen with it. A degraded classification never produces a push |
| **2** | Q2 — the digest's cadence | **Once daily, at a fixed morning hour** named as its own §13 row, distinct from consolidation's 03:00 so the two passes never contend for one instant. One deferral = one day is the anti-starvation unit |
| **3** | Q3 — the ambiguous-person-reference producer | **No.** I06 stays out of scope, and doc 02's "M3 owns this surface" sentence is corrected in the PR that would otherwise inherit it silently, rather than left implied. If it is wanted later it is `m3e`, costing a capture-path change plus a pending-question store |
| **4** | Q4 — the digest's importance gate | **Yes, `focus.Priority`.** This discharges M2's carry-over and buys the new `UnitRepo` query plus an answer for `pattern_based` triggers with no source unit. Ordering by `interrupt_level` is rejected: it would collapse the care gate into the same number the push/digest split already used |
| **5** | Q5 — `ConsolidationHour` | **Correct the prose in `m3d` #1**, so §13's row states its own blind spot instead of implying coverage. Moving the hour into `internal/core/consolidation` is recorded as a separate work unit, outside M3 |

Q1 and Q2 no longer block `m3a`. Q4 no longer blocks `m3b`. Q5 no longer blocks `m3d`.

---

## 9. Risks

| # | Risk | Rank | Mitigation |
|---|---|---|---|
| R1 | **`interrupt_level` has no producer.** Present only at `0001_core_tables.sql:48`; classify emits nothing. Every §7 delivery decision reads it | **1** | Q1, answered before `m3a` #6 |
| R2 | **Seven undefined behaviours (§4.2), each needing an owner review and a doc 02 amendment.** M3's schedule risk is decision latency, as M2's was | **2** | The four-way split: each change asks about at most a few, at the point of need |
| R3 | **`StateRepo` can neither read `energy` nor resolve a hypothesis** (`internal/ports/staterepo.go:40-52`). The digest's care gate and `state_outcome` both need it | **3** | Named as an `sdd-design` obligation for `m3d` #6, with `m2c`'s one-method-per-field discipline as the constraint |
| R4 | **I04 quietly weakens the moment `TimerRepo` exists.** Its own comment says the timers/triggers half holds "by construction… there is nothing here to query" (`i04_…_test.go:36-46`) | **4** | The strengthening is a test commit inside `m3b` #4, ahead of the port commit — §6 order 4 |
| R5 | **Nothing prevents a test dialling `api.telegram.org`.** M2's discharge #4 recorded that the network half of non-negotiable #5 has no guard; `m3c` is the first slice where a copy-pasted real URL would pass CI | 5 | Every Telegram test constructs its client with an `httptest` base URL, and `m3c` #2 adds a source scan for the literal host under `internal/channels/**` — the cheap structural form of a rule that is otherwise discipline |
| R6 | **A concurrent tick could double-fire.** Two passes, or a pass and an inbound cancellation, can race on one `armed` row | 6 | Every status transition takes a `from` precondition and returns a conflict, as `UnitRepo.SetStatus` already does (`internal/ports/unitrepo.go:87`); the scan skips and logs rather than failing |
| R7 | **`getUpdates`'s offset is process state.** After a restart, a confirmed-but-unprocessed update is gone and an unconfirmed one redelivers | 7 | Confirm the offset only after the capture is persisted; inbound handling must tolerate one redelivery. An `m3c` design obligation, stated before it is discovered |
| R8 | **No clock gate reaches `internal/channels` or `internal/scheduler`.** The two guards are scoped to `internal/brain/**`, and a polling loop legitimately reads the clock | 8 | `scheduler_boundary_scan_test.go`'s `time.Hour` leg already covers the realistic regression in the scheduler; `m3c` keeps every temporal decision in `core/prospection` and the adapter holds only its own backoff. A review property, named as one |
| R9 | **I10 makes M3 the first change to delete a row.** `RelationRepo` gains a `Delete`, inside a repository whose I03 test forbids the prefix on `units` | 9 | I03's scan stays scoped to `units` deliberately, with its own doc comment saying so, in the same PR |
| R10 | **Recurrence arithmetic has two ordinary inputs that break naive code**: 29 February yearly, and day 31 monthly | 10 | Both are conformance cases in `m3a` #5, written before the function |
| R11 | **Estimates run low** — 1.3x–2.2x six times in M0, 4.3x once in M1 Phase B | 11 | §5.1 states the multiplier up front; the split is decided here, not discovered during apply |
| R12 | **The `internal/core` coverage floor bites `m3a` hardest**, and `make check` never runs `scripts/core-coverage.sh` | 12 | `make check-all` before every PR, structurally |
| R13 | **`docs-sync` only fires once a PR is open**, and every `m3a` PR touches `internal/core/**` | 13 | §4.2 gives each one a genuine doc 02 delta. No M3 core PR should need `no-spec-change` |
| R14 | **The milestone cannot close inside CI.** §7's manual real-bot pass is unautomatable by non-negotiable #5 | 14 | Named as an exit gate rather than a PR, recorded in `m3d`'s archive report, as M1 did |

---

## 10. Next step

**`m3a-prospection`**: `sdd-spec` and `sdd-design` run in parallel over this proposal. Design owes
§4.2's seven behaviours with their §13 rows, for owner review before apply. **Q1 and Q2 block a
clean `m3a` design** — Q1 shapes arming, Q2 shapes the digest gates — and both are asked here, with
options, rather than improvised at apply time.

**`m3c-telegram`** can be specified and designed as soon as `m3a`'s design is reviewed; it needs no
code from `m3a` and none from `m3b`. **`m3b`** waits on Q4 (the focus-candidate query). **`m3d`**
waits on all four slices and on Q5.
