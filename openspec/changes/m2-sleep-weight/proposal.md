# Proposal — M2: sleep and weight

Deliver M2 as laid out in [`docs/05-build-plan.md`](../../../docs/05-build-plan.md) §M2:
`effective_weight` and the decay model, priority and the two focuses with anti-jitter hysteresis,
the eight-phase nightly consolidation of [`docs/02-cognitive-core.md`](../../../docs/02-cognitive-core.md)
§6 with each phase individually invocable, and the in-process scheduler with
[ADR-0009](../../../docs/adr/0009-scheduler-downtime.md)'s boot catch-up.

M1 made the binary a brain that answers when spoken to. M2 is the change where the brain does
something while nobody is looking — and therefore the change where **time becomes a first-class
input** rather than a timestamp column nothing reads back.

---

## 1. Why now

M1 closed on 2026-08-03 against a real migrated vault. What it left behind is a vault that
accumulates and never settles.

| Fact (verified 2026-08-04) | Consequence |
|---|---|
| `internal/core/{weight,focus,consolidation,selfmodel,learning,prospection}` each contain only a `doc.go` with a one-line package comment | Every decision M2 owns is greenfield. Six packages, zero statements |
| `internal/scheduler/doc.go` is the whole of `internal/scheduler` | Nothing runs on a timer. The binary only acts when a request arrives |
| `units.weight`, `units.weight_decay_rate` and `units.last_touched_at` are written at capture (migration `0001_core_tables.sql:11-13`) and **read back by nothing** | The Ebbinghaus curve of doc 02 §2 exists in the schema and in prose, and nowhere else. Every unit is as important on day 90 as on day 1 |
| `ports.UnitRepo` has seven methods and **not one of them writes `weight` or `last_touched_at`** (`internal/ports/unitrepo.go:30-73`) | Revive, resurface and reweight have no way to persist their result. This is a required new port surface, not an oversight to route around |
| **No Go file anywhere references `weight_threshold`, `hysteresis_margin` or `consolidation_last_run_at`**, and **no migration contains `INSERT INTO config`** | The `config` singleton table has never been read, never been written, and **its row does not exist in any vault**. M2 is the first consumer of all six columns. See R1 — this is the single most expensive fact in this document |
| `cmd/nooma` has `status`, `tasks`, `serve`, `init`, `capture`, `doctor` — and no `consolidate` | `docs/01-architecture.md` already documents `nooma consolidate [vault]` as "a pure subcommand, also used by the scheduler". The contract is written; the subcommand is not |
| `test/conformance/` holds 44 files and **none named I05, I11 or I19** | The three invariants M2 exists to satisfy have no test yet. Nothing has started |
| `scripts/pending-red.sh` is gone, retired in `714934e` | The gate that made "test before implementation" structural no longer exists. §6 says how the chain honours the rule without it |

The schema needs almost nothing. Fourteen tables have held since the harness landed; `units`,
`relations`, `self_beliefs`, `decision_log`, `learning_signals` and `config` all carry the columns
the eight phases write. **M2 is, like M1, overwhelmingly a code change over a schema that already
exists** — with two candidate exceptions priced in R1 and R3.

What M2 is *not* mostly, and this is the scoping fact that matters more than the schema one:
**M2 is not mostly code.** Its dominant cost is that doc 02 gestures at six behaviours without
defining any of them (§4.2). The lines are few and the decisions are many, which is the exact
inversion of M1.

---

## 2. Success criteria

The change is done when:

- [ ] `effective_weight` is computed on read from `(weight, weight_decay_rate, last_touched_at,
      now)` and is never written by a read path (**I05**), while consolidation's optional bulk
      decay materialization — which doc 02 §2 and §6.6 explicitly permit — remains legal.
- [ ] A revive and a resurface each write `weight` **and** `last_touched_at` together, never one
      without the other (**I24**, new — see §6).
- [ ] Priority ranks the pool, the task focus and the load focus are two queries over one table,
      and a challenger displaces an incumbent only by beating it by more than `hysteresis_margin`
      (**I19**).
- [ ] `status='focus'` is still absent from the tree after focus is built (**I01**) — M2 is the
      first change capable of breaking that test.
- [ ] The eight phases run in the order `expire_incomplete → archive → strengthen → connect →
      derive → reweight → pattern_eval → learn`, with `learn` always last (**I11**), and each is
      individually invocable through `nooma consolidate`.
- [ ] Every phase decision with an effect writes a `decision_log` row from `internal/brain`, never
      from `internal/core` (**I12**), and a phase that decided nothing writes nothing.
- [ ] `archive` moves a unit `pool → archived` through `SetStatus`, never through a `DELETE`
      (**I03**), and a concurrent revive that loses the `from` precondition is skipped and logged,
      not treated as a failure.
- [ ] At startup, a vault whose `consolidation_last_run_at` is more than 24 h old queues a
      consolidation with a **120-second delay** (ADR-0009), and one whose value is fresher does
      not.
- [ ] Every behavioural number M2 introduces is a named constant in exactly one place **and** a row
      in `docs/02-cognitive-core.md` §13.
- [ ] `make check-all` green, and CI green, on every PR in the chain — including the
      `internal/core` coverage floor, which `make check` does not run and which m2a and m2b are
      made almost entirely of.
- [ ] No test touches the network or a real LLM, and no core test uses the real clock.
- [ ] **Demo**: a vault seeded with simulated weeks of data, run through `nooma consolidate` —
      cold things get archived, related things get connected, beliefs get derived, and
      `decision_log` tells the story end to end.

That last bullet is the build plan's own wording and it is the exit criterion. It is also
[`docs/06-harness.md`](../../../docs/06-harness.md) §2's own worked example of why the clock is a
port: *"the M2 demo is 'a vault with simulated weeks of data'. That demo is literally impossible
without an injected clock."* The harness committed to this shape a milestone in advance.

---

## 3. Scope

### 3.1 The boundary rule

> **M2 implements what the brain does to itself over time. It implements nothing that speaks to
> the user, and nothing that waits for the user to answer.**

Concretely: M2 decays, ranks, archives, connects, derives, reweights and evaluates patterns. It
does not arm a trigger, does not fire a timer, does not build a digest, does not push, does not
open Telegram, and does not consume a learning signal. Those are M3 and M5, and the build plan
says so.

### 3.2 In scope

1. **`internal/core/weight`** — `effective_weight` from the Ebbinghaus curve, the thermal-zone
   classification of doc 02 §2, the revive boost, and resurface's spreading activation along
   relation `strength`.
2. **`internal/core/focus`** — the priority formula, the task focus and the load focus as two
   selections over one ranked pool, and anti-jitter hysteresis over a previous-focus value handed
   in as data.
3. **`internal/core/consolidation`** — the eight phase decisions, each a pure function, plus the
   ordered phase sequence itself as a value the runner cannot reorder.
4. **`internal/ports`** — a weight-write method on `UnitRepo`; a live-count-by-type method on
   `UnitRepo` for `mental_load` (owner ruling 6); `SelfModelRepo`, shaped like `RelationRepo`'s
   upsert-by-unique-key (owner ruling 5); and a `ConfigRepo` over the `config` singleton, which no
   port has ever covered.
5. **`internal/store/sqlite`** — the implementations of the above. Each widens
   `testdata/schema/store_api.golden`.
6. **`internal/brain`** — `ConsolidateService`: read the clock **once** for the whole pass, run the
   eight phases in order, persist, and write `decision_log` at every effect.
7. **`internal/scheduler`** — the in-process cron at 03:00 and ADR-0009's boot catch-up, with
   `boot_consolidation_delay` as a Go constant (owner ruling 2).
8. **`cmd/nooma`** — the `consolidate` subcommand, whole-pass and per-phase, wired into `serve`.
9. **The doc 02 amendments of §4.2** — six behaviours that currently have no formula, each landing
   in the same PR as the code that implements it, per `CLAUDE.md` non-negotiable #1.
10. **A new consolidation golden set** — the simulated-weeks vault the demo needs, with its own
    `format.md`, following `testdata/`'s existing convention.

### 3.3 Explicit non-goals, each with its reason

- **No trigger or timer staleness gate. No quiet hours. No `expired`, no `cancelled`.**
  **Owner ruling 1**, and it is a scope boundary rather than a footnote: ADR-0009's header says
  *"Enables: M2"*, and half of ADR-0009 is therefore **deliberately not implemented by M2**.
  The reason is the one ruling 1 gives — nothing in the product arms a trigger or a timer today
  (M1's Q3a: *"M1 arms nothing"*), so the gate would be code that no real behaviour exercises until
  M3 ships its callers. ADR-0009's own consequences section says the gate is *"a pure function over
  `(fire_at, now, kind, threshold)`"*, so it loses nothing by travelling with the code that arms
  its inputs. **I15, I16 and I17 are M3.** M2 implements ADR-0009's "Consolidation — always
  recovered" section and nothing else from that ADR.
- **No producer of `incomplete` units.** M2 ships the `expire_incomplete` phase — I11 requires all
  eight phases in order, so seven is not an option — but M2 does not turn on the capture path that
  would create its input. M1's Q3a decided capture creates none, and that decision stands until a
  surface exists that can resolve an ambiguity conversationally, which is M3. **I06 stays honestly
  out of scope rather than vacuously green**, exactly as M1 recorded it. See Q3.
- **No learning.** The `learn` phase ships as a **true no-op that still occupies slot eight**
  (owner ruling 3): the ordering test can and should be written now, and doc 05 gives the real
  logic to M5. A no-op with a name in an ordered sequence is not dead code — it is the sequence's
  eighth element, and I11 is about the sequence.
- **No digest, no push, no `interrupt_level`.** `pattern_eval` produces the *hypothesis* — a
  `current_state` row for load accumulation, a stagnation finding for goals — and nothing delivers
  it. M3 owns delivery. This split is honest and worth stating: the watchers of doc 02 §7 are
  half-satisfied at the end of M2 and cannot be more than half-satisfied.
- **No UI.** The focus is computed and has **no surface in M2** — see §4.3, which argues this is
  acceptable here and was not acceptable for the trigger gate.
- **No `reindex`, no export/import, no perception.** M6 and v2.

### 3.4 Invariants in scope, traced

| # | Invariant | Doc 02 | M2 status |
|---|---|---|---|
| I05 | `effective_weight` computed on read; decay not written on every read | §2 | **In scope, new test.** M2 is the first change with a read path that could write decay |
| I11 | The 8 phases run in order; `learn` is always last | §6 | **In scope, new test.** The heart of the milestone |
| I19 | A challenger must beat the incumbent by more than `hysteresis_margin` | §3 | **In scope, new test** |
| I24 | A weight write moves `weight` and `last_touched_at` together | §2 | **New invariant — see §6.** Requires a `docs/06-harness.md` §4 table row before its test |
| I01 | `status='focus'` does not exist | §3 | **Test exists and is load-bearing for the first time.** It has passed vacuously since M0 because no focus existed. M2 builds one |
| I12 | Every effectful automatic decision writes `decision_log` | §11 | **In scope, and load-bearing.** M1 covered it on the capture path; M2 adds eight phases of effects and has no dedicated test file yet |
| I02 | Live reads exclude `superseded` and `incomplete` | §1 | In scope — every phase read filters positively, and `expire_incomplete` is the one deliberate exception, which must be named rather than discovered |
| I03 | Nothing deleted; archiving is a transition | §1 | In scope — `archive` is the first path whose whole purpose is a status change. The new ports must keep the no-`Delete`-prefix property `i03_units_never_deleted_test.go` enforces |
| I07 | A relation is unique per `(from, to, type)` | §4 | In scope — `connect` and `strengthen` both write relations, through the same `RelationRepo.Upsert` the judge already uses |
| I08 | Below `min_confidence_to_persist` → not stored | §4 | In scope — `connect`'s judge reuses `core/relation.Resolve` unchanged |
| I06 | An `incomplete` unit has no embedding until promoted | §1 | **Out, honestly.** No producer exists — see §3.3 and Q3 |
| I15, I16, I17 | — | §7, ADR-0009 | Out. Owner ruling 1 moves them to M3 |
| I09, I10, I13, I20 | — | §4, §9, §12 | Out. Digest is M3, signal consumption is M5, insights are v2 |

---

## 4. Approach

### 4.1 Where the boundary falls, decision by decision

| Decision | Package | Why there |
|---|---|---|
| Given `(weight, λ, last_touched_at, now)`, what is the effective weight? | `core/weight` | Data in, data out. The clock arrives as a parameter |
| Given a unit and a boost reason, what is its new `(weight, last_touched_at)`? | `core/weight` | Pure. The write is the caller's problem |
| Given a set of units and an instant, what is the ranked pool and which N are in focus? | `core/focus` | Pure over a slice. No repository, no query |
| Given the previous focus and a challenger, does it displace? | `core/focus` | The whole of I19, and a pure function of two values and a margin |
| For each of the eight phases: given this input, what should change? | `core/consolidation` | `docs/06-harness.md` §1 states it verbatim: *"each phase's logic in `core/consolidation`, the pass that runs them in order and persists in `brain/`"* |
| Given `(consolidation_last_run_at, now, threshold)`, is a catch-up due? | `core/consolidation` | ADR-0009's own claim: a pure function, testable with no scheduler, no clock and no database |
| Read the clock once, load the vault, run the eight phases, persist, log | `brain/consolidate.go` | Orchestration |
| Fire at 03:00; delay the boot catch-up 120 s | `internal/scheduler` | Adapter over real time |
| Speak SQL for the new repositories | `store/sqlite` | Adapter |
| `nooma consolidate`, and wiring the scheduler into `serve` | `cmd/nooma` | Wiring |

Two placements deserve their reasoning stated.

**The catch-up decision lives in `core/consolidation`, not in `internal/scheduler`.** The scheduler
is a ticker; what it must not own is the *judgement* about whether 25 hours is stale. ADR-0009
already argued this ("the staleness gate is a pure function… the three questions in the Context
section become three tests"), and even though ruling 1 removes two of its three kinds, the third
keeps the shape.

**`internal/scheduler` is outside `internal/brain`, and the clock guards do not reach it.**
`test/conformance/brain_single_clock_read_test.go` fails a non-test file under `internal/brain/**`
with more than one `Now()` call, and `brain_no_direct_clock_read_test.go` forbids a direct
`time.Now`. Neither applies to `internal/scheduler`, and neither should: a cron ticker reads the
clock every tick by definition. What has **no gate** is the boot catch-up comparing two different
instants. See R9 — a review property, stated so it is known to be one.

### 4.2 The six formulas doc 02 does not have

This is the milestone's real content, and the proposal states it plainly rather than letting
`sdd-apply` improvise it.

| Behaviour | What doc 02 says today | What M2 must supply |
|---|---|---|
| **priority** | §3: `priority = f(effective_weight, temporal_urgency(due_at), type, age, relation_to_active_focus)` | `f`. Five terms, no weighting, no shape for `temporal_urgency`, and §13 has no knob for any of it |
| **revive boost** | §2: "writes a new boosted weight and resets `last_touched_at`" | How much boost, and whether it is capped |
| **resurface propagation** | §2: "propagates a boost along the graph edges, proportional to each relation's `strength`" | Hop limit, attenuation per hop, and whether cycles terminate |
| **strengthen** | §6.3: "re-evaluates relation strength with accumulated evidence" | What counts as evidence, and the update rule. **Owner ruling 4** assigns this to M2 |
| **reweight** | §6.6: "post-connection weight adjustments (and optional decay materialization)" | Which adjustments, and when materialization runs. **Owner ruling 4** |
| **incomplete resolution** | §1 gives two outcomes (promoted, or archived if still unresolved); **§6.1 gives only one** ("promoted with what they have") | The predicate that chooses, and an amended §6.1 that stops contradicting §1 |

The last row is a **contradiction inside doc 02**, found while writing this proposal: §1:17-18 and
§1:28-30 both describe an `incomplete → archived` expiry branch that §6.1:437 omits entirely. Per
`CLAUDE.md` non-negotiable #1 it gets resolved in the PR that implements the phase, not silently.

**Owner ruling 4 set the pattern and this section extends it to all six**: each formula is an
`sdd-design` output subject to owner review, lands with its doc 02 amendment and its §13 rows in
one PR, and is never improvised at apply time. Implementing against prose that cannot produce a
testable invariant is how a conformance suite becomes decoration.

### 4.3 The focus has no consumer in M2, and ships anyway

Verified: `GET /units` takes `?ids=` and maps 1:1 onto `RecallService.LiveByIDs`
(`internal/httpapi/units.go:52-87`) — no ranking. `nooma status` reports vault and schema facts,
not units. The build plan gives the today/focus view to **M4**. Nothing in M2's eight phases reads
a focus.

That is the same shape ruling 1 rejected for the trigger staleness gate, so it needs an argument
rather than an appeal to the build plan. The distinction that makes it hold:

**The trigger gate has no producer; the focus has no consumer, and those are not symmetric.** The
gate's input is a `triggers` row, and nothing in the product writes one — so its test would have to
fabricate a row against a table with no producer, proving only that the function computes. The
focus's input is `units`, which M1 already produces in quantity and which the demo vault will hold
weeks of. A focus computed over a real vault is exercised by real data the moment it exists; the
gate is not.

Second, weaker but real: doc 02 §2's thermal-zone vocabulary is defined *in terms of* the focus —
Hot is "in the focus", Warm is "pool but not in the focus". A milestone that ships `archive`
(warm→cold) without a focus can implement only two thirds of a three-valued vocabulary the demo's
own narrative uses.

### 4.4 The eight phases, and what each one actually touches

```
expire_incomplete  → units (status)                              no producer yet (§3.3)
archive            → units (status)      needs config.weight_threshold, core/weight   I03, I05
strengthen         → relations           formula owed (§4.2)
connect            → relations           reuses core/recall fusion + core/relation.Resolve  I07, I08
derive             → self_beliefs        needs SelfModelRepo, and belief embeddings (R3)
reweight           → units (weight)      needs the new UnitRepo write method (R2), formula owed
pattern_eval       → current_state       needs the live-count-by-type method (ruling 6)
learn              → nothing             true no-op (ruling 3)
```

Two of these consume machinery M1 already built and should not rebuild: `connect`'s candidate
search is `core/recall`'s fusion — ADR-0010's own warning is that a bias here propagates into the
entire relation graph, and two implementations are two biases — and `connect`'s persist decision is
`core/relation.Resolve`, unchanged, with `created_by='consolidation'` the only difference from
capture's judge.

### 4.5 What the pass sees as "now"

`brain/consolidate.go` reads `ports.Clock` **once** for the whole pass, and every phase receives
that one `time.Time`. This is required by the gate and correct on the merits, but it has a
consequence worth writing down before it is discovered: a pass over weeks of simulated data may run
for a long time, and `archive`'s Δt and `expire_incomplete`'s 24-hour window will both be measured
from the pass's **start** instant, not from the moment each phase happens to reach a unit. That is
the intended semantics — one pass, one instant, per `docs/06-harness.md` §2 — and it makes the pass
reproducible, which is what the demo needs.

---

## 5. The chain

M1 split into an umbrella proposal plus three phase changes (`m1a-substrate`, `m1b-pipeline`,
`m1c-surface`). **M2 follows that shape with four phase changes**, and the argument is dependency
order, not symmetry with M1.

The exploration proposed five slices — pure weight/focus, pure consolidation, ports+store,
brain+scheduler+CLI, demo. Four of the five survive; the fifth does not, and one boundary moves:

- **The demo is not its own change.** It is a fixture and an L4 test whose only purpose is to prove
  the scheduler and the pass work. A planning cycle — spec, design, tasks — for a golden set and one
  test is overhead with no decision in it. It merges into the last change, as its exit criterion.
- **`nooma consolidate` moves out of the scheduler slice and into the runtime slice.** The
  exploration put the CLI with the scheduler because both are "entrances". But a change that ships
  `brain.ConsolidateService` with no caller can only be verified by its own tests, whereas the same
  change plus a thin subcommand can be verified by *running the pass on a vault and reading
  `decision_log`*. The CLI is ~200 lines and it converts an unobservable change into an observable
  one. That is worth more than the tidiness of grouping entrances together.

The dependency graph the split follows:

```
core/weight ──────┬──> core/consolidation ──> ports+store ──> brain/consolidate ──> scheduler
                  │         (archive,              (m2c)          (m2c)               (m2d)
                  │          reweight)
core/focus ───────┘  (leaf: nothing in M2 depends on it)
```

**`m2a-weight-focus`** — pure read side. `core/weight` and `core/focus`. Zero ports, zero store,
zero brain, zero I/O. Depends on nothing. Owns I05's pure half, I19, I24's definition, and I01's
first real exercise. Carries the priority, revive-boost and resurface amendments to doc 02 §2/§3
and their §13 rows.

**`m2b-consolidation-core`** — pure sleep side. `core/consolidation`: the ordered phase sequence as
a value, and the eight decisions. Depends on `m2a` (archive and reweight both need
`weight.Effective`). Owns I11's pure half and the `strengthen`/`reweight`/`incomplete-resolution`
amendments to doc 02 §6.

**`m2c-consolidation-runtime`** — ports, store, `brain/consolidate.go`, and `nooma consolidate`.
Depends on `m2b`. Owns I12 across eight phases, I03 at the `archive` write, I24's structural test,
and R1's config-row decision. Exit criterion: run the pass by hand on a vault and read the
`decision_log`.

**`m2d-scheduler-demo`** — `internal/scheduler`'s cron and ADR-0009 boot catch-up, `serve` wiring,
the simulated-weeks golden set, and the L4 demo. Depends on `m2c`. Exit criterion: the build plan's
own demo bullet.

Why four and not two (pure / impure): `m2a` and `m2b` each carry three owner-reviewable formulas
(§4.2), and merging them makes one change whose design phase asks the owner six formula questions
at once. Ruling 4 already established that these are design outputs subject to review; batching six
of them into one review is how a review becomes a rubber stamp. Why four and not five: see the demo
argument above.

Chain strategy `stacked-to-main`, delivery `auto-chain` — M1's own, recorded in observation #622.

### 5.1 Per-PR budgets

**These are guesses.** Every number below is a budget chosen to respect the 400-line soft ceiling,
not a prediction, and this project has measured its predictions wrong six times in M0 (1.3x–2.2x)
and again in M1 Phase B (worst case 4.3x). The only claim made with confidence is the *shape* of
the chain, not its size.

| Change | PRs | Content | Est. |
|---|---|---|---|
| **m2a** | `feat/core-weight-decay` | `weight.Effective`, thermal zones; doc 02 §2 | ~250 |
| | `feat/core-weight-boost` | revive boost, resurface spreading activation with its hop rule; §2 + §13 | ~300 |
| | `feat/core-focus-priority` | the priority formula and `temporal_urgency`; §3 + §13 | ~350 |
| | `feat/core-focus-selection` | task focus, load focus, hysteresis (**I19**); §3 | ~350 |
| **m2b** | `feat/core-consolidation-order` | the phase sequence as a value, `learn` as slot-eight no-op (**I11** pure half) | ~250 |
| | `feat/core-consolidation-expire-archive` | the 24 h predicate and its §6.1 amendment; the `weight_threshold` decision | ~300 |
| | `feat/core-consolidation-strengthen-reweight` | ruling 4's two formulas; §6 + §13 | ~350 |
| | `feat/core-consolidation-connect-derive` | candidate selection over `core/recall`; belief merge at cosine ≥ 0.85 | ~350 |
| | `feat/core-consolidation-pattern-eval` | goal stagnation and load accumulation watchers | ~300 |
| **m2c** | `feat/ports-unit-weight-count` | the weight-write method (**I24**), the live-count-by-type method; memrepo fakes | ~300 |
| | `feat/ports-selfmodel-config` | `SelfModelRepo` (ruling 5), `ConfigRepo` and R1's missing-row decision | ~300 |
| | `feat/store-consolidation-repos` | the SQLite implementations; `store_api.golden` regenerated | ~400 |
| | `feat/brain-consolidate-runner` | one clock read, eight phases in order, `decision_log` (**I11**, **I12**) | ~400 |
| | `feat/brain-consolidate-phase-io` | each phase's reads and writes wired; likely splits further | ~400 |
| | `feat/cli-consolidate` | `nooma consolidate [vault]`, whole-pass and per-phase | ~250 |
| **m2d** | `feat/scheduler-cron` | in-process cron at 03:00, `consolidation_enabled` gate | ~300 |
| | `feat/scheduler-boot-catchup` | ADR-0009's catch-up, the three Go constants (ruling 2) | ~300 |
| | `feat/serve-scheduler-wiring` | wire into `nooma serve`, shutdown ordering | ~200 |
| | `feat/demo-simulated-weeks` | the golden set with its `format.md`, and the L4 demo | ~400 |

Nineteen PRs, ~6,050 budgeted lines. Read against M1's measured multipliers that is realistically
**8,000–13,000 lines across 25–35 PRs**, and the phase changes exist so that no single tasks
artifact has to cover all of them.

Two savings worth naming, because they are real and they are the reason M2's code is smaller than
M1's despite covering more of doc 02:

- **`docs/06-harness.md` §1's tree already lists `weight/`, `focus/`, `consolidation/` and
  `scheduler/`** (lines 28-44). M2 creates no new package directory, so no preflight doc PR is
  needed for the tree — unlike M1, whose `core/classify` cost one.
- **Owner ruling 2 removes migration 0003 from the scheduler half.** The three ADR-0009 thresholds
  are Go constants following `core/classify`'s `PriorWeight` precedent. Whether the *consolidation*
  half still needs a migration is R1, and it is open.

---

## 6. Strict TDD ordering

Strict TDD is active. What has changed since M1 is that **the gate that enforced it is gone**:
`scripts/pending-red.sh` and `test/conformance/pending_symbols.txt` were retired in `714934e`,
correctly — M1's own §4.7 predicted that the gate fails when its last symbol is promoted, and
retiring it was the fix. M2 therefore has to honour "a conformance test is written before its
implementation" as **discipline plus review**, and this section says exactly how, because a rule
with no mechanism is a rule that decays.

Four mechanisms, in descending order of strength:

1. **Commit ordering inside the PR.** In every PR that satisfies an invariant, the conformance test
   is its own commit, ahead of the implementation commit, so the PR's own history shows red then
   green. `work-unit-commits` reads a work unit as change + tests + doc together; a conformance test
   that encodes a doc 02 invariant **is** a reviewable unit of work on its own, and this is the one
   place where the split is right rather than sloppy.
2. **`sdd-tasks` orders the test task strictly before the implementation task**, per PR, and never
   as a single "implement X with tests" item. The tasks artifact becomes the schedule the gate used
   to be.
3. **`sdd-verify` reads the PR's `git log` and reports an inversion as CRITICAL.** This is the only
   automated check M2 has on the ordering, and it is a phase check, not a CI gate. Stated so it is
   known to be one.
4. **`test/conformance/core_exported_decls_have_tests_test.go` stays as the standing presence
   guard.** It fails an exported top-level declaration under `internal/core/**` whose own name
   appears in no `*_test.go` in its package, and it runs in `make check`. It says nothing about
   ordering — its own doc comment announces that honestly — but it makes "shipped with no test at
   all" impossible, which is the failure mode the retired gate mostly caught.

If the owner wants the ordering structural again, the cheap form is a per-PR commit-order check in
`scripts/`. That is **not** proposed as M2 scope; it is named here so the option is on the record
rather than rediscovered.

### 6.1 The order the tests get written

The three load-bearing invariants named in the task brief are load-bearing for different reasons,
and the difference decides where each one lands.

| Order | Invariant | Change | Why here |
|---|---|---|---|
| 1 | **I01** — `status='focus'` never persisted | m2a, `feat/core-focus-selection` | The test already exists as a tree scan and **has passed vacuously since M0** because no focus existed. M2 is the first change that can break it. It is not written; it is *inherited*, and the PR that builds focus is the one that finally puts it under load |
| 2 | **I19** — hysteresis margin | m2a, `feat/core-focus-selection` | New. Pure over `(incumbent, challenger, margin)`. L1 next to the code, plus L2 naming the invariant |
| 3 | **I05** — decay computed on read | m2a (pure half), m2c (structural half) | New, and it **splits**. The pure half — `Effective` is a function, not a mutation — is L1 in m2a. The structural half — no read path writes `weight` — needs the store layer, so it is L2 in m2c, and it must be scoped to read paths or it will forbid `reweight`'s bulk materialization, which doc 02 §2 and §6.6 explicitly permit. See R13 |
| 4 | **I24** — weight and `last_touched_at` move together | m2a (definition), m2c (test) | **New invariant.** `docs/06-harness.md` §4's table gains its row **first** — the `nooma-testing` skill's own step 2 — then the test, then the port method. Structural: a `SetWeight` that leaves `last_touched_at` alone must not be expressible |
| 5 | **I11** — eight phases in order, `learn` last | m2b (pure), m2c (behavioural) | New, and the milestone's centre. The pure half asserts the sequence value; the behavioural half asserts the runner cannot reorder it. Ruling 3's no-op `learn` exists precisely so the ordering test can be written now |
| 6 | **I12** — every effectful decision logs | m2c, `feat/brain-consolidate-runner` | **Load-bearing and has no dedicated test file today** — M1 satisfied it through capture-path tests (`capture_orphan_actions_test.go`, `recall_writes_no_decision_test.go`). M2 adds eight phases of effects. It needs both directions: an effect always logs, and a phase that decided nothing writes nothing |
| 7 | **I03** — nothing deleted | m2c, `feat/store-consolidation-repos` | Existing structural test. `archive` is the first path whose entire purpose is a status change, and the new ports must keep the no-`Delete`-prefix property |
| 8 | **I07, I08** | m2c | Existing. `connect` reuses the judge's own path; these are regression coverage, not new proof obligations |
| 9 | **ADR-0009 catch-up** | m2d, `feat/scheduler-boot-catchup` | Not an I-number. ADR-0009's Context section names three questions; ruling 1 leaves M2 the first one, and it becomes one pure test over `(consolidation_last_run_at, now)` |

Every one of these except I01, I03, I07 and I08 is a test file that does not exist today.

---

## 7. Risks

| # | Risk | Rank | Mitigation |
|---|---|---|---|
| R1 | **The `config` singleton row does not exist and nothing has ever read the table.** Verified: no Go file references `weight_threshold`, `hysteresis_margin` or `consolidation_last_run_at`; no migration contains `INSERT INTO config`. `archive` needs `weight_threshold`, the boot catch-up needs `consolidation_last_run_at`, `pattern_eval` needs `goal_stagnation_days` and `mental_load_threshold`, the cron needs `consolidation_enabled`. **On a fresh vault every one of those reads returns no rows.** Ruling 2 removed migration 0003 from the *scheduler* half; it did not touch this | **1** | Three options, priced in Q1. The recommendation is `ConfigRepo` returning named Go constants when the row is absent, pinned to the SQL column `DEFAULT`s by an L2 test — the exact pattern M1's Q1 established for `relation_thresholds`, precedent already in `i13_learning_signal_test.go` reading migration text off disk |
| R2 | **The missing `UnitRepo` weight-write method is a design decision, not an addition.** `UpdateContent`/`UpdateEventAt`/`UpdateDueAt` established M1's Q3c-iii rule: one method per field, *no signature capable of writing the wrong thing*. A weight write must move `weight` and `last_touched_at` together (I24), so the two cannot be separate methods — and `reweight`'s bulk decay materialization wants a batch write, which the per-field discipline has no shape for | **2** | Named as an `sdd-design` obligation for `m2c`, with I24 as the constraint the signature must make structural. The batch-vs-single tension is called out now rather than found at apply time |
| R3 | **`derive`'s semantic merge has nowhere to store belief embeddings.** Doc 02 §6.5 requires dedup by cosine ≥ 0.85 over beliefs. `unit_embeddings.unit_id` is `TEXT PRIMARY KEY REFERENCES units(id) ON DELETE CASCADE` (migration `0002:74-80`) — a belief is not a unit, so its vector cannot live there. Options are embedding every belief on every pass (N provider calls per night) or a new table. **This is the highest-probability forcer of migration 0003**, and neither the exploration nor the rulings priced it | **3** | Q2. Decided before `m2b` designs `derive`, not during `m2c` |
| R4 | **Six undefined formulas (§4.2), each needing an owner review and a doc 02 amendment.** M2's schedule risk is decision latency, not implementation time | **4** | The four-way chain split exists for this: each change asks the owner about three formulas at most, at the point where they are needed |
| R5 | **Estimates run low** — 1.3x–2.2x measured six times in M0, up to 4.3x in M1 Phase B | 5 | §5.1 states the multiplier up front and the split decision is made here, before apply, never discovered during it |
| R6 | **The `internal/core` coverage floor bites hardest here.** `m2a` and `m2b` are almost entirely `internal/core/**`, and `make check` never runs `scripts/core-coverage.sh` | 6 | Every core PR ships its L1 tests in the same commit as its code; `make check-all` is the pre-PR command, structurally — the fast loop cannot catch this |
| R7 | **`docs-sync` fires only once a PR is open on GitHub**, and every `m2a`/`m2b` PR touches `internal/core/**` | 7 | §4.2 gives every one of them a genuine doc 02 delta up front. The `no-spec-change` label should be needed by no M2 core PR; if one needs it, that PR is not implementing a behaviour doc 02 describes |
| R8 | **A concurrent capture can revive a unit between `archive`'s read and its write.** `SetStatus` takes a `from` precondition and returns `ErrStatusConflict` (`internal/ports/unitrepo.go:87`) | 8 | The pass skips and logs; it does not fail. Stated in §2's criteria so the runner is built that way rather than patched into it |
| R9 | **No gate reaches `internal/scheduler`.** The two clock guards are scoped to `internal/brain/**`, and a cron ticker legitimately reads the clock repeatedly — but the boot catch-up comparing two different instants is the bug class those guards exist for, and nothing catches it there | 9 | One clock read at the catch-up decision, passed into the pure staleness function. A review property, stated so it is known to be one — the same shape M1's R9 took |
| R10 | **The demo golden set has no precedent.** `testdata/` holds `classify/`, `recall/`, `llm/` and `schema/`; a simulated-weeks *vault* is a different kind of fixture, and `golden_sets_test.go` carries non-empty guards that may need extending | 10 | `format.md` first, following the existing convention, in the same PR as the first case |
| R11 | **`expire_incomplete` ships with no producer**, proven by a repo-constructed input rather than a user path | 11 | Accepted and named (Q3). I11 requires all eight phases, so the alternative is not shipping the milestone |
| R12 | **`store_api.golden` fires in the fast loop** and needs `make store-api-golden`, a different target from `make schema-golden` | 12 | Named explicitly in `feat/store-consolidation-repos`'s task list, as M1 named it for its PRs 4 and 9 |
| R13 | **I05's test can forbid something doc 02 permits.** "Decay is not written on every read" must not be written so broadly that `reweight`'s optional bulk materialization (§2, §6.6) violates it | 13 | I05's structural half is scoped to read paths only, and the scoping is stated in the test's own doc comment |

---

## 8. Open questions

Each of these is a decision the owner makes. The recommendation is the proposal's reasoning, not a
settled answer. They are separate from the six rulings already taken on 2026-08-04, which this
document treats as closed.

### Q1 — Where do the `config` columns' values come from on a vault whose row does not exist?

The blocking one. Verified: the table is written by nothing, read by nothing, and seeded by nothing.

| Option | Pro | Con |
|---|---|---|
| **A. Migration 0003 seeds the singleton row** | The value lives where doc 03 already documents it; one source of truth | A migration for M2 after ruling 2 removed one; needs `schema_doc_test.go`'s hand-written anchor list and doc 03 updated; existing vaults get the row, new ones get it too, and neither path is exercised today |
| **B. `INSERT OR IGNORE` on vault open** | No migration; self-healing for existing vaults | A write on the open path, and the values still have to come from somewhere — this defers the question rather than answering it |
| **C. `ConfigRepo` returns named Go constants when no row exists, pinned to the SQL `DEFAULT`s by an L2 test** | No migration; handles a fresh vault and a legacy one identically; satisfies harness §7's "one place" rule; **the exact pattern M1's Q1 already chose for `relation_thresholds`** | Two sources for one default — the Go constant and the SQL column `DEFAULT` — which can drift |

**Recommendation: C.** The drift objection is the only real one and this repository has already
closed it once, the same way: `i13_learning_signal_test.go` reads migration SQL text off disk, so
an L2 test pinning constants to column defaults is a pattern, not an invention. C also keeps
ruling 2's "no migration 0003" true for the whole of M2 rather than half of it — and a milestone
that needs no migration is a milestone whose `schema_doc_test.go` anchor list and doc 03 stay
untouched.

### Q2 — Where do belief embeddings live, for `derive`'s cosine ≥ 0.85 merge?

| Option | Pro | Con |
|---|---|---|
| **A. Embed every active belief at the start of each `derive` phase, in memory, discard after** | No schema change; no stale-vector problem when a belief's text is edited | N provider calls every night, growing with the belief count. A nightly cost that scales with the thing the product is trying to grow |
| **B. A `belief_embeddings` table (migration 0003), mirroring `unit_embeddings`** | Paid once per belief; the same `model`-filter discipline I21 already established | Migration 0003, doc 03, `schema_doc_test.go`'s anchor list, the schema golden regenerated — and an invalidation rule when a belief is edited |
| **C. Defer the semantic merge; ship `derive` with only the prompt-side defense** | No cost at all in M2 | Doc 02 §6.5 names **two** defenses. Shipping one and calling the phase done is the divergence non-negotiable #1 forbids — it would need a doc 02 amendment saying so |

**Recommendation: A for M2, with the cost written into doc 02 §6.5.** Belief counts are small by
construction — the self-model is a handful of facets, not a corpus — so the nightly cost is bounded
in the regime v1 actually runs in, and A is the only option that needs neither a migration nor a
doc 02 retreat. If the owner expects belief counts in the hundreds, B is right and the migration
cost belongs in `m2c`'s estimate. C is listed because it is what an implementation will silently
choose under time pressure; naming it here is how that stops being an accident.

### Q3 — Does M2 turn on the producer of `incomplete` units?

M1's Q3a decided capture creates none, because one would be invisible to every read surface (I02)
and immortal until M2 shipped `expire_incomplete`. M2 ships it. Does the producer follow?

- *No (recommended)*: M2 ships the phase because I11 requires all eight, and proves it against a
  repo-constructed `incomplete` unit — legitimate, since the status is in the schema and
  `unit.ValidateTransition` already permits `incomplete → pool` and `incomplete → archived`. **I06
  stays honestly out of scope**, exactly as M1 recorded it.
- *Yes*: capture creates an `incomplete` unit for an ambiguous person reference, which makes I06
  testable and `expire_incomplete` non-vacuous. Cost: a capture-path change inside a milestone named
  "sleep and weight", reopening a decision M1 took deliberately.

**Recommendation: no.** The reason is not the scope-creep argument, which is weak on its own — it is
that promoting an `incomplete` unit "with what it has" is a *degradation*, and the product's actual
answer to an ambiguous person reference is to **ask**. That surface is M3's. Turning on the producer
in M2 would commit the product to silently guessing for a whole milestone, which is the same failure
shape M1's Q3a rejected for timers: a system that promises quietly rather than refusing plainly.

### Q4 — Does the previous focus live in process, or in a row?

Doc 02 §3 says hysteresis *"requires remembering the previous focus (in-process state or a state
row — not a flag on units)"* and leaves the choice open. No table holds one today.

- *In-process*: no schema change, no migration. After a restart there is no incumbent, so the first
  focus computation has no hysteresis and can jitter once. For a single-process binary that a user
  restarts occasionally, one un-damped transition per restart is invisible.
- *A state row*: survives restarts, at the cost of migration 0003 (see Q1's own reasoning) and a
  write on a read path — the focus is a *view*, and giving it persistent state is one step from the
  persisted flag §3 exists to forbid.

**Recommendation: in-process.** §3's whole argument is that a persisted focus creates two sources of
truth that desynchronize on their own; the hysteresis memory is not the focus itself, but it is
close enough that the cheaper answer is also the safer one. Doc 02 §3 gains one sentence naming the
choice and its restart consequence.

### Q5 — Four chained SDD changes, or fewer?

See §5. **Recommendation: four** — `m2a-weight-focus`, `m2b-consolidation-core`,
`m2c-consolidation-runtime`, `m2d-scheduler-demo` — sharing this proposal and this scope, each with
its own spec, design and tasks.

What that buys immediately: **`m2a` is blocked by nothing in this section.** Q1 shapes `archive`'s
threshold read (m2b/m2c), Q2 shapes `derive` (m2b), Q3 shapes `expire_incomplete`'s honesty (m2b).
Q4 is the only question touching `m2a`, and it lands inside `feat/core-focus-selection` rather than
gating the change — hysteresis is a pure function over a previous-focus value either way; the
question is only who holds that value between calls. So `m2a` can be specified, designed and started
while Q1, Q2 and Q3 are still open.

The cost is real and accepted: four planning cycles rather than one. This document stays the single
umbrella — the four changes do not restate its scope, they reference it.

---

## 9. Next step

**`m2a-weight-focus`**: `sdd-spec` and `sdd-design` run in parallel over this proposal, scoped to
the four PRs of §5.1's first block. Design owes three formulas — priority, revive boost, resurface
propagation — each with its §13 rows, for owner review before apply.

**`m2b`, `m2c` and `m2d`** wait on the owner's answers here: Q1 blocks a clean design for every
phase that reads a threshold, Q2 blocks `derive`, Q3 decides whether `expire_incomplete` has a
producer. Those questions are not asked again from scratch when their change begins — they are asked
here, with options and a recommendation, and answered before that change's spec.
