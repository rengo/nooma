# Spec — M2a: weight and focus

Delta specification for `m2a-weight-focus`, the first of four chained SDD changes splitting
`openspec/changes/m2-sleep-weight/proposal.md` (owner ruling round 2 #6, 2026-08-04). States what
MUST be true of the repository after this change is applied, in testable form. It does not
prescribe how (that is `design.md`).

Sources: `openspec/changes/m2-sleep-weight/proposal.md` §3.2 items 1–2, §4.2, §5, §5.1's m2a
row; `docs/02-cognitive-core.md` §2, §3, §13; `docs/06-harness.md` §4's invariant table (I01,
I05, I19); owner rulings round 1 (`sdd/m2-sleep-weight/owner-rulings`, #644) and round 2
(`sdd/m2-sleep-weight/owner-rulings-2`, #647), specifically ruling 1 (formula ownership: `m2a`
owns priority, revive boost, resurface hop/attenuation) and Q4 (previous focus lives in-process).

> **Status: reconciled.** This document and `design.md` were written **concurrently**, never saw
> each other, and disagreed on four substantive points. A fresh-context adversarial reviewer
> adjudicated all of them and the owner ruled on two; the outcome is
> `sdd/m2a-weight-focus/adjudication` (**#650**), which is binding. This revision applies all nine
> rulings, **plus owner ruling 10**, taken afterwards in response to the boundary analysis those
> nine produced (R3.5 / §9): `age_horizon_days` moves from 30 to 15. Every place where this
> document previously asserted something now overruled carries an inline **Reconciled (ruling N)**
> note recording what it used to say — the text is corrected, the disagreement is not erased. §10
> lists every ruling and where it landed.
>
> Where this document and `design.md` state the same fact, they now state it in the same words and
> with the same identifiers. `design.md` owns the signatures and the arguments; this document owns
> the testable assertions over them.

## Scope boundary (binding, from the proposal's §3.2 and §5.1's m2a row)

> `m2a` is pure read side. `internal/core/weight` and `internal/core/focus`. Zero ports, zero
> store, zero brain, zero I/O. Depends on nothing.

Four PRs, per proposal §5.1: `feat/core-weight-decay` (~250 lines), `feat/core-weight-boost`
(~300), `feat/core-focus-priority` (~350), `feat/core-focus-selection` (~350). Both packages
already exist as bare `doc.go` stubs (`sdd/m2-sleep-weight/explore`, #643) and are already listed
in `docs/06-harness.md` §1's tree — this change adds no new package directory and needs no
preflight doc PR.

**Those four estimates no longer hold.** `design.md` §8 R10 re-derives them after the rulings and
finds three of the four over the 400-line ceiling, with `m2a` landing at seven PRs rather than
four. This document does not repeat that arithmetic; the split lines are `design.md` §8 R10's and
`sdd-tasks` should plan against them, not against §5.1's row.

Every requirement below is bounded to `internal/core/weight/**` and `internal/core/focus/**`.
Nothing in this change touches `internal/ports`, `internal/store`, `internal/brain`,
`internal/scheduler`, `cmd/nooma`, or any migration. `strengthen`, `reweight`, and the
incomplete-resolution predicate are `m2b`. The eight consolidation phases, the scheduler, and
`nooma consolidate` are `m2b`–`m2d`. ADR-0009's trigger/timer staleness gate is M3.

**`weight_threshold` is not `m2a`'s** (ruling 4). Its Go home is `m2b`'s
`feat/core-consolidation-expire-archive`, per proposal §5.1:348. `m2a` still asserts two
arithmetic relations against it (R2.4, R2.7), but does so against the **migration DDL text**, not
against a Go constant this change declares.

---

## 1. `internal/core/weight` — effective weight and decay (PR1 — `feat/core-weight-decay`)

Traced to `docs/02-cognitive-core.md` §2.

### R1.1 — `Effective` computes the Ebbinghaus curve exactly as doc 02 states it

**MUST**: `internal/core/weight` exposes a pure function `Effective(weight, decayRate float64,
lastTouchedAt, now time.Time) float64` implementing `effective_weight = weight *
exp(-decay_rate * Δt)`, where Δt is `now.Sub(lastTouchedAt)` expressed in whole days (fractional,
not truncated — a unit touched 12 hours ago has Δt = 0.5), computed as
`now.Sub(lastTouchedAt).Hours() / 24` and never as a calendar-day count.

**MUST**: `now` and `lastTouchedAt` are both plain `time.Time` parameters — the function calls
neither `time.Now` nor any other clock source.

**Verified by**: L1 — a table test asserting `Effective(1.0, 0.01, t, t) == 1.0` (Δt=0), that the
result strictly decreases as `now` moves later for any `decayRate > 0`, and that `decayRate ==
0` returns `weight` unchanged regardless of Δt (matching §2's "λ of 0 never decays").

**Scenario**:
- GIVEN `weight = 1.0`, `decayRate = 0.01`, `lastTouchedAt` 100 days before `now`
- WHEN `Effective` is called
- THEN it returns `1.0 * exp(-0.01 * 100)` (≈ 0.368), computed with no side effect and no state
  retained between calls

### R1.2 — `Effective` clamps a negative Δt at zero and never exceeds the stored weight

> **Reconciled (ruling 3).** This requirement is **new**. The previous revision of this document
> never constrained the case `now < lastTouchedAt`, and so silently permitted
> `exp(-λ · negative) > 1` — an effective weight larger than the persisted weight, a value the
> schema never contained. `design.md` D1 had the clamp; this document did not. The adjudication
> calls it "a real correctness gap" and adopts the clamp. It is stated as its own requirement
> rather than folded into R1.1 so it has a test row of its own.

**MUST**: `Effective` clamps Δt at zero. When `now` is before `lastTouchedAt` — clock skew across
a restart, a backdated import, a fake clock wound backwards in a test — the function behaves as
though Δt were 0 and returns `weight` undecayed.

**MUST**: `Effective(w, λ, lt, now) ≤ w` holds for **every** input, including `λ = 0` and every
ordering of `lt` and `now`. This is a postcondition, not a comment.

**Verified by**: L1 — a table row with `now` one hour before `lastTouchedAt` asserting the result
equals `weight` exactly; a property test over random `(w, λ, lt, now)` asserting the result never
exceeds `w`.

The same clamp-a-negative-elapsed-time-at-zero rule applies to `AgeRamp` (R3.4). It is one rule
applied twice, not two rules: `core` receives instants it cannot vouch for, and every elapsed-time
computation in `m2a` therefore saturates rather than inverting.

### R1.3 — `Effective` is a read-only computation, never a mutation (I05, pure half)

**MUST**: `Effective` takes `weight` and `lastTouchedAt` by value and returns a `float64`; it has
no pointer or interface parameter capable of writing back to a caller's unit.

**MUST**: no exported function anywhere in `internal/core/weight` returns `unit.Unit`,
`*unit.Unit`, or `[]unit.Unit`. A read path therefore holds no unit-shaped value it could hand to
a repository, and "accidentally persist a decayed weight" has no syntax.

**MUST**: the only persistable value the package produces is a `weight.Boost` (R2.1), and `Boost`
values have exactly **two** producers — `Revive` and `Resurface`, the two discrete events doc 02
§2 names. No third producer exists.

**MUST NOT**: any function in `internal/core/weight` that computes an effective weight also
persist one — persistence is a store-layer concern (`m2c`), and `m2a` ships no port, no store, no
caller of a repository.

**Verified by**: L2, structural — a conformance test reflecting over `core/weight`'s exported
surface, failing if any function result type is `unit.Unit`/`*unit.Unit`/`[]unit.Unit` or if a
producer of `Boost` exists beyond the two named. Plus L1 — a call-twice test asserting identical
inputs return identical outputs (no hidden state).

**Note on scope**: I05's full guarantee — "no *read path* writes decay" — needs a store to prove
a read path exists at all. This requirement proves the pure half only: the computation itself has
no write capability, and the package cannot express the sentence. The structural half (an L2 test
scoped to read paths only, per proposal Risk R13) is `m2c`'s, once `ports.UnitRepo` exists.

### R1.4 — Thermal-zone classification is a total pure function of status and focus membership

> **Reconciled (beyond the nine rulings — divergence C-a).** This requirement previously named the
> function `ThermalZone` and required that it **never be called with `superseded` or `incomplete`
> in any test**, declaring doc 02 silent and the case unresolved. `design.md` D2 named it `ZoneOf`,
> made it **total** — both statuses map to `ZoneCold` — and tested the full 4 × 2 matrix driven by
> `unit.AllStatuses()`. The adjudication did not rule on this because it is a naming and totality
> conflict, not a formula conflict. Resolved here in `design.md`'s favour on two grounds a reader
> can check: a deliberately untested arm is an uncovered statement, and `internal/core`'s coverage
> floor is ≥ 90 % (`nooma-testing` hard rule 5); and a partial function whose contract says "do not
> call it this way" is a rule somebody remembers rather than a property the type system holds.

**MUST**: `internal/core/weight` exposes `ZoneOf(status unit.Status, inFocus bool) Zone` returning
`ZoneHot` when `status == pool && inFocus`, `ZoneWarm` when `status == pool && !inFocus`, and
`ZoneCold` when `status == archived`, matching doc 02 §2's table exactly.

**MUST**: `ZoneOf` is **total** over `unit.AllStatuses() × {true, false}`. `superseded` and
`incomplete` — the two statuses doc 02 §2's table does not name — map to `ZoneCold`. The zone
vocabulary is about attention and neither status is a candidate for attention. This is a choice,
not a derivation; it is recorded in the doc 02 §2 amendment (§7) so a later reader is not left
inferring it.

**MUST**: `ZoneOf` takes no `now`. Temperature is a function of two decisions already made, not of
time. Cold's parenthetical in doc 02 §2 — "its effective weight crossed the threshold during a
consolidation" — is causal history, not a determination.

**MUST**: `internal/core/weight` exposes `Zone`, `AllZones() []Zone` and `Zone.String()`.

**Verified by**: L1 — a table test over the **complete** `unit.AllStatuses() × {inFocus}` matrix,
driven by `unit.AllStatuses()` and asserting its own completeness against it, so a fifth status
added later fails this test rather than silently falling through.

---

## 2. `internal/core/weight` — revive boost and resurface propagation (PR2 — `feat/core-weight-boost`)

Traced to `docs/02-cognitive-core.md` §2 ("Revive… writes a new boosted weight and resets
`last_touched_at`"; "Resurface… propagates a boost along the graph edges, proportional to each
relation's `strength`"). Doc 02 names both mechanisms but supplies neither formula — this section
is `m2a`'s invention, per owner ruling round 2 #1, stated explicitly as such at each requirement.

> **Reconciled (ruling 2).** This whole section previously specified **two independent formulas**:
> a revive that was `clamp(e + revive_boost_amount, revive_weight_floor, revive_weight_cap)` and a
> resurface application that was `min(e + boost, revive_weight_cap)`. `design.md` §3.2–§3.3
> specified **one asymptotic mechanism used at two strengths**. The adjudication took
> `design.md`'s, on a numeric argument this document cannot answer: under an additive clamp, two
> distinct hot units at effective weight 1.5 and 1.8 both land on exactly 2.0 after a +0.5 boost,
> so a revive **destroys the ordering among the units the user touches most** — which is the
> jitter hysteresis exists to suppress, manufactured by the boost itself. The asymptotic form
> `e + g·(Ceiling − e)` is bounded by construction, needs no clamp, and is strictly increasing in
> `e`, so it preserves the ordering it acts on. `revive_boost_amount`, `revive_weight_floor`,
> `revive_weight_cap` and `resurface_min_boost` are gone (ruling 7); `revive_gain`,
> `weight_ceiling`, `resurface_max_hops` and `resurface_attenuation` replace them.
>
> **And the ruling amends `design.md` too**: at `e ≥ WeightCeiling` that document wrote *nothing at
> all*, not even `last_touched_at`. R2.3 fixes that edge.

### R2.1 — Every weight write is a `Boost`, and a `Boost` cannot omit `last_touched_at` (I24)

**Invention** — doc 02 names the mechanism, not the shape.

**MUST**: `internal/core/weight` exposes

```go
type Boost struct {
    UnitID        string
    Weight        float64
    LastTouchedAt time.Time
}
```

and there is **no way to obtain a new weight from this package without a corresponding new
`last_touched_at`** — no exported function returns a bare `float64` weight intended for
persistence, and `Boost` has no constructor that leaves `LastTouchedAt` at its zero value.

**MUST**: this is I24's structural guarantee at the pure-function level. `m2c`'s `UnitRepo`
weight-write method takes a `weight.Boost` (or its three fields), so a `SetWeight` that leaves
`last_touched_at` alone is not expressible at the port. `m2a` does not add the port — it fixes the
shape the port must have.

**MUST**: `docs/06-harness.md` §4's invariant table gains I24's row **in this change**, before any
test names it (`nooma-testing` execution step 2). The row: *"A weight write moves `weight` and
`last_touched_at` together; neither is written alone — §2."* The store-level test is `m2c`'s.

**Verified by**: L2, structural — the same reflection test as R1.3 asserts `Boost`'s two
producers; the shape itself is a compile-time property.

### R2.2 — `Revive` boosts asymptotically toward the ceiling, from the **decayed** value

**Invention** — doc 02 names the mechanism, not the formula.

**MUST**: `internal/core/weight` exposes `Revive(c Current, now time.Time) Boost` computing

```
e  = Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
w' = e + ReviveGain × max(0, WeightCeiling - e)
Boost{UnitID: c.UnitID, Weight: w', LastTouchedAt: now}
```

**MUST**: the boost is applied to the **effective** weight at `now`, never to the persisted
`c.Weight`. Applied to the persisted value, reviving a unit that had decayed for ninety days would
restore its full undecayed weight plus a bonus: decay would be freely reversible, `weight` would
become a monotone ratchet, and doc 02 §2's entire model would be decorative.

**MUST**: `ReviveGain` and `WeightCeiling` are named constants in exactly one place in
`internal/core/weight`, with defaults `0.35` and `2.0`.

**MUST**: `Revive` leaves `c.DecayRate` untouched. Doc 02 §2 assigns λ at classify and
personalizes it from the self-model; nothing there says use makes a thing decay more slowly.

**Verified by**: L1 — a table asserting `Revive` is strictly increasing under repetition at a
fixed instant, converges on `WeightCeiling` and **never reaches or exceeds it** for `e <
WeightCeiling`, never lowers a weight for any input, and always returns `LastTouchedAt == now`.

**Scenario**:
- GIVEN a unit whose effective weight has decayed to 0.0
- WHEN `Revive` is called with `now`
- THEN the returned weight is `0.35 × 2.0 = 0.70` and `LastTouchedAt` equals `now`, never the
  unit's prior `last_touched_at`

### R2.3 — At or above the ceiling, a revive still moves `last_touched_at`

> **Reconciled (ruling 2, the edge `design.md` got wrong).** `design.md` §3.2's boundary table
> returned **"no write at all"** when `e ≥ WeightCeiling`, and returned `(Boost, bool)` with
> `ok == false` to express it. The adjudication overturns that edge: *"a direct use must still move
> the clock."* The consequence is a **signature change** — `Revive` returns a bare `Boost`, not
> `(Boost, bool)`.

**MUST**: `Revive` always returns a write. When `e ≥ WeightCeiling` the `max(0, …)` term in R2.2
is zero, so the returned pair is `(e, now)` — the weight is not raised (a boost never lowers one
either), and `last_touched_at` moves to `now`.

**MUST**: `Revive`'s signature is `func Revive(c Current, now time.Time) Boost` — no `bool`, no
`*Boost`, no "maybe". A direct use is always an event to record.

> **Superseded during PR #137 — divergence C-d below. This MUST is kept as written; the shipped
> signature is `func Revive(c Current, now time.Time) (Boost, bool)`.** The clause above is
> correct about the case it was reasoning about (the ceiling edge, ruling 2's own subject): at or
> above `WeightCeiling` a direct use still writes, so the `bool` had no false case there and
> carrying it would have been a lie in the type. Judgment Day then found a case ruling 2 never
> considered — a **non-finite** computed weight. `Revive` is the write path, and `Boost` is the
> only shape this package lets a caller persist, so a `NaN` reaching it becomes durable in the
> `weight` column, where nothing is ever deleted. The `bool` is now `false` for exactly that
> input and nothing else, verified by two independent fuzzers (2,000,000 and 200,000 samples,
> zero false refusals). See C-d.

**Stated so it is not mistaken for a contradiction**: the `(e, now)` write is
**effective-weight-neutral by construction**, and knowingly so. Since `e = w · exp(-λ·(now - lt))`,
the pairs `(w, lt)` and `(e, now)` denote the *same* curve — `Effective` returns the same value at
every future instant either way. The write is nevertheless not null, because `last_touched_at` is
the vault's record of **direct use**, and it is read as one: doc 02 §2 distinguishes "direct use"
from "related signal", and R3.4's age term exists precisely because `last_touched_at` is reset by
use while `created_at` never is. Leaving it stale for the hottest units in the vault would break
that meaning exactly where it matters most. So doc 02 §11's "a decision with no material effect
writes nothing" does not apply: the decision here — *this was directly used* — is real, and
recording it is the effect.

**Verified by**: L1 — a case with `c.Weight` above `WeightCeiling` and `Δt = 0`, asserting the
returned `Weight` equals `e` exactly (not raised, not lowered) and `LastTouchedAt == now`; plus an
assertion that `Effective` over the returned pair equals `Effective` over the input pair at an
arbitrary later instant, which is the neutrality claim made testable.

### R2.4 — One direct revive always clears the archive band, at the default calibration

**MUST**: the constant relation `ReviveGain × WeightCeiling > weight_threshold`'s DDL default holds
— `0.35 × 2.0 = 0.70 > 0.5`. Read out: **one direct revive is always enough to lift a fully-decayed
unit back above the archive threshold.** That is doc 02 §2's own "cold → warm/hot by a strong
resurface" made arithmetic, and it pins two otherwise-free constants to a third the schema already
fixes.

> **Reconciled (ruling 4).** `design.md` D4 asserted this against a Go constant
> `weight.DefaultWeightThreshold` that it declared in `m2a`. Ruling 4 sends `weight_threshold`'s Go
> home back to `m2b`'s `feat/core-consolidation-expire-archive`, where proposal §5.1:348 assigns
> it. The relation survives; only what it is asserted **against** changes.

**MUST**: the assertion is made at **L2**, against the `DEFAULT` literal parsed out of
`internal/store/sqlite/migrations/0002_learning_and_search.sql:63` via the existing
`migrationSQLText` helper (`test/conformance/i13_learning_signal_test.go:24`) — not against a Go
constant, because `m2a` declares none for this column.

**MUST**: the assertion is an **inequality**, not an equality, and its doc comment says why:
`weight_threshold` is marked ⚙ recalibratable per user in doc 02 §13, so a user who raises it to
0.8 breaks the *relation* without breaking the *code*. The test therefore constrains the
**defaults**, and says so.

**Verified by**: L2 — `test/conformance/weight_constant_relations_ddl_test.go` (or equivalent),
reading the migration text off disk.

### R2.5 — `Resurface` is the same mechanism at an attenuated **target**

**Invention** — doc 02 names "spreading activation… proportional to strength" but not the hop
limit, the attenuation curve, or cycle termination.

**MUST**: `internal/core/weight` exposes `Resurface(n Neighbourhood, now time.Time) []Boost`
computing, for every unit `v` in the neighbourhood other than the origin:

```
gain(v)   = max over paths p from origin to v with |p| ≤ ResurfaceMaxHops of
              ( Π strength(e) for e in p ) × ResurfaceAttenuation^|p|
target(v) = gain(v) × WeightCeiling
e_v       = Effective(v.Weight, v.DecayRate, v.LastTouchedAt, now)

w'_v = e_v + ReviveGain × (target(v) - e_v)   when e_v < target(v)   → emit Boost{v, w'_v, now}
     = (no Boost emitted)                     when e_v ≥ target(v)
```

**MUST**: the gain scales the **target**, never the **step**. Scaling the step —
`e + ReviveGain·gain·(Ceiling − e)` — would let a unit merely adjacent to something used daily
converge on the full ceiling: each pass takes a fraction of the remaining gap while one day of
decay at λ = 0.01 removes about 1 % of it, so the neighbourhood of anything hot becomes permanently
hot and decay never bites. Scaling the target caps *where propagation can hold a unit*, which is
the property that makes spreading activation safe.

**MUST**: a unit reachable by more than one path within `ResurfaceMaxHops` takes the **maximum**
gain among them, never the sum. A sum makes a unit's boost depend on how many redundant edges the
judge happened to create — topology noise, and unbounded. `max` is bounded in `[0,1]` by
construction and stable under adding a weak redundant path. It is the same rule R3.5 uses for
adjacency: **one rule for combining graph evidence, used twice.**

**MUST**: traversal is **undirected**. Doc 02 §4 states a relation's direction is "what the judge
said, not a canonical form", and two units related both ways hold two rows — so an edge must
conduct activation regardless of which way it was stored, or propagation would depend on which of
two units happened to be captured second. Where two units are joined by several edges (different
relation types) the **strongest** is used, by the same `max` rule.

**MUST**: the origin is **never a recipient**. It already received its direct revive; a 2-cycle
back to it would double-count.

**MUST**: propagation stops expanding past `ResurfaceMaxHops` hops from the origin, and terminates
on a cyclic graph **by the hop bound alone** — not by a runtime timeout. Gain is strictly
decreasing along a path (attenuation < 1, strength ≤ 1) and depth is capped, so a cycle can only
produce a strictly worse path that `max` discards.

**MUST**: `ResurfaceMaxHops` and `ResurfaceAttenuation` are named constants in exactly one place in
`internal/core/weight`, with defaults `2` and `0.5`.

**MUST**: the returned slice is **sorted by `UnitID`** and contains only units the pass actually
raises. The sort is not cosmetic: the suite runs `-shuffle=on` with `-race` (`Makefile:48`), any
implementation will use a visited map, and `m2c` needs a reproducible `decision_log` order for the
demo.

**Verified by**: L1 — a cyclic fixture (A↔B↔C↔A) asserting termination and `max`-not-sum
aggregation; a chain longer than `ResurfaceMaxHops` asserting units beyond the limit are absent
from the result; a fixture with the edge stored in the opposite direction asserting the same
result; a fixture with two edges between the same pair asserting the strongest is used; an
assertion that the origin never appears in the output; an assertion the output is sorted.

**Scenario**:
- GIVEN a source unit connected to neighbour N by two distinct paths within `ResurfaceMaxHops`,
  one yielding gain 0.20 and the other 0.12
- WHEN `Resurface` computes N's target
- THEN N's gain is 0.20 (the maximum), not 0.32 (the sum)

### R2.6 — A resurfaced unit already warmer than its target is not written at all

**MUST**: when `e_v ≥ target(v)`, `Resurface` emits **no `Boost` for `v`** — a shorter slice, not a
zero-delta entry. No weight write, no `last_touched_at` reset, no `decision_log` row downstream.
Doc 02 §11: a decision with no material effect writes nothing.

**MUST**: this asymmetry with R2.3 is deliberate and is stated in the doc 02 §2 amendment. **A
direct use always records itself; an indirect propagation records itself only when it genuinely
lifts something.** Resurface's own no-op branch is therefore the thing that stops propagation from
making a unit *look* directly used: the majority of the neighbours of a hot unit are already warmer
than propagation could make them, and their clocks are never touched.

**MUST**: where `Resurface` **does** write, it resets `last_touched_at`. `weight` is *defined* as
the value at `last_touched_at` (doc 02 §2: "Persisted: `weight` (value at the last event)"), and
the on-read formula reads them as a pair — write a boosted `weight` while leaving `last_touched_at`
alone and the very next read re-applies the entire old Δt to the new value, eating the boost and
stamping a value that was never true at its own timestamp. **This is I24's mechanical origin**, and
it is why `m2a` is the change that adds I24's harness row (R2.1).

**MUST**: the worry that a reset makes a resurfaced unit look directly used is answered by the
**target cap**, not by the timestamp: a resurfaced unit converges on `gain × WeightCeiling`, not on
`WeightCeiling`, so its decay clock restarting is harmless because the level it restarts from is
bounded by graph distance. Both halves go into the doc 02 §2 amendment, because the first half
alone reads as a bug.

**Verified by**: L1 — a neighbour whose effective weight already exceeds its target, asserting it
is **absent** from the returned slice; a neighbour below its target, asserting its `LastTouchedAt`
equals `now`.

### R2.7 — Spreading activation alone cannot lift a unit above the archive threshold at maximum hop distance

**MUST**: the constant relation `ResurfaceAttenuation^ResurfaceMaxHops × WeightCeiling ≤
weight_threshold`'s DDL default holds — `0.5² × 2.0 = 0.5 ≤ 0.5`. Read out: **at the maximum hop
distance, propagation alone can never hold a unit above the archive band.** Only direct use, or a
strong immediate neighbourhood, keeps something out of the cold. That is the guarantee that makes
it safe to run resurface on every capture.

**MUST**: like R2.4, this is asserted at **L2** against the migration DDL default (ruling 4), as an
**inequality** rather than an equality, with the ⚙ caveat in its doc comment. The exact equality at
the defaults is a coincidence and the test says so.

**Verified by**: L2 — the same `weight_constant_relations_ddl_test.go` file as R2.4.

**Boundary table this change commits to** (all at the defaults):

| Situation | gain | target |
|---|---|---|
| 1 hop, `strength = 1.0` | 0.50 | 1.00 — classify's base weight, and no higher |
| 2 hops, `strength = 1.0` each | 0.25 | 0.50 — exactly the archive threshold |
| 1 hop, `strength = 0.1` (doc 02 §4's "a passing mention") | 0.05 | 0.10 — a passing mention cannot keep anything alive |
| 3+ hops | unreachable | — |

---

## 3. `internal/core/focus` — priority (PR3 — `feat/core-focus-priority`)

Traced to `docs/02-cognitive-core.md` §3: `priority = f(effective_weight,
temporal_urgency(due_at), type, age, relation_to_active_focus)`. Doc 02 names the terms and
explicitly supplies no weighting. This entire section is `m2a`'s invention, per owner ruling
round 2 #1, and every requirement below is labeled as such.

### R3.1 — `Priority` is a multiplicative envelope over `effective_weight`, not a weighted sum

> **Reconciled (ruling 1 — the largest correction in this document).** This section previously
> specified a **linear weighted sum of five normalized terms**, with five `priority_weight_*`
> constants summing to 1.0 and the result bounded to `[0, 1]`. That form is **defectively ordered**,
> and the adjudication showed it numerically: under it, an item at `effective_weight = 0` that is
> overdue, old and adjacent scores `0.30 + 0.10 + 0.10 + 0.15 = 0.65`, while a healthy item at
> `effective_weight = 1.0` with no context scores `0.35 × 0.5 = 0.175`. **The forgotten item beats
> the live one by roughly 4× on context alone**, which collapses doc 02 §3:79's whole "weight is
> intrinsic, priority is contextual" distinction. `design.md` §3.1's multiplicative form is
> adopted: it proves `priority ≥ effective_weight` and monotonicity in `e` by construction. The
> five `priority_weight_*` constants and `priority_weight_cap` are cut (ruling 7).

**Invention.**

**MUST**: `internal/core/focus` exposes `Priority(c Candidate, adjacency float64, now time.Time)
float64` computing

```
e = weight.Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
u = UrgencyRamp(c.DueAt, now)     // [0,1], exactly 0 when DueAt is nil
g = AgeRamp(c.CreatedAt, now)     // [0,1], anti-starvation
a = adjacency                     // [0,1], 0 when absent

priority = e
         × (1 + (UrgencyMax-1)·u)                 // deadline: multiplicative
         × (1 + AgeWeight·g + AdjacencyWeight·a)  // nudges: additive, bounded
```

**MUST**: `priority ≥ e` for every input. Every factor is ≥ 1, so context can promote a unit and
can **never demote one**. Demotion is what decay is for; a modifier that could demote would make
the ranking depend on the *absence* of a signal, the hardest kind of ordering to explain to a user
asking "why is this not in my focus" (doc 02 §11's glass box).

**MUST**: `Priority` is monotone non-decreasing in `e` at fixed context. Two units in identical
context rank by weight. The formula never inverts doc 02 §2.

**MUST**: `Priority` is **homogeneous of degree 1 in `e`** — scaling every candidate's effective
weight by the same positive factor leaves the ranking unchanged. This is what makes the score
comparable across the two focuses (R4.1) and is the reason the hysteresis margin is relative
(R4.3).

**MUST**: the maximum context amplification is `UrgencyMax × (1 + AgeWeight + AdjacencyWeight)` =
`3.0 × 1.45` = **4.35×**. That number is not a knob; it is the product of the knobs, and it is
asserted so the dynamic range of the ranking is known rather than discovered.

**MUST**: `Priority` is **unbounded above** — it inherits `e`'s range, which R2.2's
`WeightCeiling` caps at 2.0 for revived units but which classify may exceed. Nothing in this
change assumes `priority ∈ [0, 1]`.

**Verified by**: L1 — a property test over random valid inputs asserting `priority ≥ e` and
monotonicity in `e`; a test asserting the maximum-amplification identity at the extremes of all
three ramps; a homogeneity test scaling every weight by 0.5 and asserting `Rank`'s output order is
identical.

### R3.2 — `type` leaves the priority arithmetic entirely

> **Reconciled (ruling 8 — OWNER).** This section previously specified a `typeTerm` reading three
> named per-type priors (`priority_type_prior_task` 1.0, `priority_type_prior_event` 0.85,
> `priority_type_prior_mental_load` 1.0). The owner ruled `type` **out of the arithmetic**: it is a
> focus-membership predicate and nothing more, and `docs/02-cognitive-core.md` §3:76 is amended
> from five terms to four. Recorded for the record, from the adjudication: `design.md`'s
> rejected-alternatives table attacked a nine-constant strawman, and this document actually
> proposed three; and within the task focus `type` was **not** redundant with membership filtering,
> which does not separate `task` from `event`. The owner chose removal with that correction on the
> table. Reinstating a numeric type term later is **additive**, not a rewrite.

**MUST**: no term of `Priority` reads `c.Type`. `Priority` is type-independent, which also makes
the two focuses' scores directly comparable — a property M4's "today" view will want.

**MUST**: `type` enters only as `Types(Kind)` (R4.1), the criterion selecting which contest a unit
is in.

**MUST**: a `task` and an `event` with equal effective weight, equal urgency, equal age and equal
adjacency **tie on score**, and the tie is broken by `DueAt` (R3.6), never by type.

**MUST**: the doc 02 §3 amendment rewrites line 76 as

```
priority = f(effective_weight, temporal_urgency(due_at), age, relation_to_active_focus)
           over the units the focus's type criterion already selected
```

**Verified by**: L1 — a test constructing a `task` and an `event` identical in every field but
`Type`, asserting `Priority` returns exactly the same value for both; plus the structural fact that
`Priority`'s body contains no `c.Type` reference.

### R3.3 — `UrgencyRamp` is a linear ramp inside a lead window, clamped past the deadline

**Invention.**

**MUST**: `internal/core/focus` exposes `UrgencyRamp(dueAt *time.Time, now time.Time) float64`:

```
d = dueAt.Sub(now).Hours() / 24                              // days until due; negative when overdue
UrgencyRamp = clamp((UrgencyLeadDays - d) / UrgencyLeadDays, 0, 1)
```

**MUST**: `dueAt == nil` returns **exactly 0**, by definition — not the `d → ∞` limit. Units with
no deadline are the majority and the formula must reduce to `e × (1 + nudges)` for them with no
floating-point residue. `nil` is not the zero `time.Time` (I18).

**MUST**: an overdue unit clamps at 1 and grows no further. Without the clamp a task overdue by
three years dominates the focus permanently and the focus stops being a view of the present. With
it, "overdue" is a single state rather than a growing one, and what removes an overdue task from
the focus is decay or the user, not arithmetic.

**MUST**: `UrgencyLeadDays` (7) and `UrgencyMax` (3.0) are named constants in exactly one place in
`internal/core/focus`.

**MUST**: `UrgencyLeadDays` is a **separate** doc 02 §13 row from the existing `Event lead time`
(also 7). One is prospection's notification horizon and this is the ranking's; collapsing two knobs
because they happen to start equal is how a calibration table becomes un-tunable.

**Verified by**: L1 — a table: `nil` → exactly 0; `d ≥ UrgencyLeadDays` → 0; `d = 3.5` → 0.5;
`d = 0` → 1; overdue by 1 day → 1; overdue by 1000 days → 1 (does not grow).

| Situation | Ramp | Priority factor |
|---|---|---|
| `DueAt` is nil | 0, by definition | 1.0 (exactly neutral) |
| Due in ≥ 7 d | 0 | 1.0 |
| Due in 3.5 d | 0.5 | 2.0 |
| Due exactly now | 1 | 3.0 |
| Overdue by any amount | 1 (clamped) | 3.0 — and no more |

### R3.4 — `AgeRamp` is ANTI-STARVATION: it rises 0 → 1 over `AgeHorizonDays`, and older ranks higher

> **Reconciled (ruling 9 — OWNER).** This is the conflict neither artifact knew it had. Both
> documents read the same undefined doc 02 word `age` and gave it **opposite signs**: this document
> specified an anti-starvation term rising with age, `design.md` §3.1 specified a `NoveltyRamp`
> falling 1 → 0 over three days and argued at length that the anti-starvation reading "resurrects
> exactly the stale items decay is designed to sink". The owner ruled for **anti-starvation**.
> `NoveltyRamp`, `novelty_weight` and `novelty_window_days` are **removed** from `design.md`;
> `AgeRamp` and `age_horizon_days` stand. `age_weight` (0.20) inherits `novelty_weight`'s
> value and its argument for the magnitude — see R3.5.
>
> **Reconciled (ruling 10 — OWNER, taken *because of* the R3.5 analysis).** `age_horizon_days` was
> **30** when ruling 9 was taken; it is now **15**. `age_weight` stays 0.20, and nothing else about
> ruling 9 changes — `age` still means anti-starvation, the ramp still rises 0 → 1, older still
> ranks higher.
>
> The 30-day value is rejected because R3.5's own numbers disproved the framing it was chosen
> under. Ruling 9 was taken on the promise that "an old, ignored task does not stay buried
> forever"; at doc 02 §13's base λ = 0.01/day and a 30-day horizon, an untouched unit's priority
> was **strictly decreasing across the entire horizon** (`exp(-0.3) × 1.20 = 0.8890 < 1`), so the
> promise was false as specified. At 15 days the break-even λ becomes `0.20/15` = **0.01333/day**,
> above the base, and the rise is real. **This is the lever R3.5 named, pulled** — which is what
> the boundary analysis existed for.
>
> Note what the lever did *not* buy, because overselling it is the exact failure this ruling
> corrects: the overturnable deficit is **unchanged at 16.7 %** (R3.5 P2 has no `AgeHorizonDays`
> term), and past day 15 the ramp is saturated, so day 30 and day 60 are **numerically identical**
> to what the rejected 30-day horizon gave. Age gained no power — only earlier arrival. That is
> precisely why this lever was chosen over raising `age_weight`, which would have changed P2.

**Invention** in shape; **owner-ruled** in direction.

**MUST**: `internal/core/focus` exposes `AgeRamp(createdAt, now time.Time) float64`:

```
ageDays = now.Sub(createdAt).Hours() / 24
AgeRamp = clamp(ageDays / AgeHorizonDays, 0, 1)
```

**MUST**: the ramp rises from 0 at creation to 1 at `AgeHorizonDays` days old and stays at 1
beyond. **Older ranks higher.**

**MUST**: `createdAt` after `now` — clock skew, a backdated import — clamps at **0**, the same
negative-elapsed-time rule as R1.2. A unit that does not yet exist has waited no time.

**MUST**: `AgeHorizonDays` is a named constant in exactly one place, default `15` (owner ruling 10;
it was 30 under ruling 9).

**MUST**: the term reads `created_at`, never `last_touched_at`. That is the whole disambiguation
from decay: `last_touched_at` is reset by use and `created_at` never is, so their difference is
exactly "has this been revisited since capture". A term reading `last_touched_at` would count
decay's own signal a second time.

**Verified by**: L1 — a table at `createdAt == now` (0), half the horizon (7.5 d → 0.5), exactly the
horizon (15 d → 1), twice the horizon (30 d → still 1, not > 1), and `createdAt` one hour after
`now` (0). **Every fixture is expressed as a multiple of `AgeHorizonDays`, never as a literal day
count**, so ruling 10's change of 30 → 15 does not require editing this test at all — which is the
`nooma-core` hard rule 4 discipline paying off in a test rather than in production code.

### R3.5 — Anti-starvation is BOUNDED, and the bound is a stated, testable property

> **This requirement is new and belongs to neither original artifact.** Ruling 1 (multiplicative
> envelope) and ruling 9 (age means anti-starvation) were adjudicated separately and interact in a
> way neither document could analyse, because neither held both. The adjudication flags it
> explicitly and requires the reconciliation pass to state the resulting behaviour at the boundary
> "and surface it for owner objection rather than burying it". This requirement and §9's
> limitation passage are that surfacing.

**Invention.**

**MUST**: `AgeWeight` is a named constant in exactly one place, default `0.20`, and the age term
enters `Priority` **multiplicatively** as a factor `(1 + AgeWeight·g + AdjacencyWeight·a)` — it
multiplies `e` rather than adding to it. The consequences below follow from that and are asserted,
not assumed.

**MUST (P1 — bounded leverage)**: for any unit, `priority ≤ e × UrgencyMax × (1 + AgeWeight +
AdjacencyWeight)`, and with no deadline and no adjacency, `priority ≤ e × (1 + AgeWeight)` =
`e × 1.20`. **The age term's entire lifetime leverage is 20 %.** It does not grow past
`AgeHorizonDays` and it never operates on anything but `e`.

**MUST (P2 — the overturnable deficit)**: given two units in identical context differing only in
age, the older outranks the younger **iff** its effective weight is at least
`1/(1 + AgeWeight·Δg)` of the younger's. At the extreme (`Δg = 1`, one unit at or past the horizon
and the other brand new) that ratio is `1/1.20 = 0.833`, so **age can overturn an effective-weight
deficit of at most `AgeWeight/(1 + AgeWeight)` = 16.7 %, and no more, ever.**

**MUST (P3 — the floor cannot climb)**: a unit sitting at the archive threshold (`e = 0.5`, the
lowest effective weight the live pool holds as of the last consolidation) reaches at most
`0.5 × 1.20 = 0.60` on age alone. A healthy unit at classify's base weight of 1.0 with no context
at all scores 1.0. **The starved unit at the floor loses to it by 1.67×, at any age.** Anti-
starvation re-ranks among units that still hold weight; it does not rescue units that have lost it.

**MUST (P4 — the rise is genuine, and it happens at the default λ)**: for an untouched unit,
priority over time is `w · exp(-λt) · (1 + AgeWeight · min(t/AgeHorizonDays, 1))`. Two thresholds
follow, both closed forms in the two constants:

- it is **rising at `t = 0`** iff `λ < AgeWeight / AgeHorizonDays` = `0.20/15` = **0.01333/day**;
- its maximum falls **at the horizon itself** iff
  `λ ≤ AgeWeight / (AgeHorizonDays · (1 + AgeWeight))` = **0.01111/day**. Between the two thresholds
  the unit rises briefly, peaks *before* the horizon, and declines from there.

At doc 02 §13's base λ = 0.01/day **both** hold. `internal/core/classify/prior.go:25`'s
`PriorDecayRate = 0.01` is the λ every unit receives unless the model overrides it, so the base
case is the common case, not a hypothetical.

> **Reconciled (ruling 10).** This property previously asserted the **opposite**: *"at the default
> λ = 0.01/day this is strictly decreasing for all `t ∈ [0, AgeHorizonDays]`"*, with a break-even
> of 0.00667/day that sat *below* the default. That was true, and it was the finding that caused
> ruling 10. At `AgeHorizonDays = 15` the break-even moves above the default and the sign flips.

**MUST (P5 — the rise is transient and small)**: at the default λ, priority rises to a peak at
**day 15** — exactly the horizon — of `exp(-0.15) × 1.20` = **1.0328×** the unit's own day-0
priority. **A 3.3 % lift, for about two weeks.** Past the horizon the ramp is saturated at 1.20 and
priority is `1.20 · w · exp(-λt)`: pure decay, scaled by a constant, declining monotonically
forever after.

**MUST (P6 — the horizon buys a window, not a floor)**: because the bonus saturates at 1.20 either
way, a 15-day horizon and the rejected 30-day horizon are **numerically identical from day 30
onward**: 0.8890 of the day-0 priority at day 30, 0.6586 at day 60. Shortening the horizon bought
an earlier and briefly-positive arrival; **it did not raise any floor, and there is no floor.** P2
is likewise untouched — the overturnable deficit is `AgeWeight/(1 + AgeWeight)` and carries no
`AgeHorizonDays` term, so it stays at **16.7 %**. **Age gained no power, only earlier arrival.**
This is why ruling 10 moved the horizon rather than raising `age_weight`, which would have changed
P2 and P1 together.

**Verified by**: L1 —
- P1: a property test over random inputs asserting `priority ≤ e × 4.35`, and with `DueAt == nil`
  and `adjacency == 0`, `priority ≤ e × (1 + AgeWeight)`.
- P2: a table over `Δg ∈ {0, 0.5, 1}` asserting the exact crossover ratio, including the
  `0.833` boundary at `Δg = 1` from both sides. **Unchanged by ruling 10** — no fixture in it
  references a day count.
- P3: a fixture with one unit at `e = 0.5` and full age and one at `e = 1.0` and zero age,
  asserting `Rank` puts the second first. **Unchanged by ruling 10** — "full age" is expressed as
  `AgeHorizonDays`, not as 30.
- P4/P5: **this fixture changes shape under ruling 10.** It walks one untouched unit's priority at
  `t ∈ {0, ½·horizon, horizon, 2·horizon, 4·horizon}` at λ = `PriorDecayRate`, asserting the
  sequence **rises to its maximum at exactly `t = horizon`** and declines strictly thereafter — the
  opposite of the assertion this test carried before ruling 10, and the reason it is called out
  here rather than left for apply to discover.
- P4 break-even, pinned from both sides: the same walk at λ = 0.0111 (peak still at the horizon)
  and at λ = 0.0134 (above break-even — the sequence is decreasing from `t = 0`), so **both**
  closed forms are asserted rather than only the one the default satisfies.
- P6: the day-30 and day-60 values are asserted to be independent of `AgeHorizonDays`, by computing
  them at two different horizon constants and asserting equality.

**MUST**: the doc 02 §3 amendment states P1–P6's *meaning* in prose — a genuine but **transient**
lift peaking around two weeks, over a re-ranking among live units, and not a resurrection
mechanism — rather than only the formula. §9 below is the plain-language version, written for owner
objection.

### R3.6 — `Rank` produces a total order with an explicit three-level tie-break

**MUST**: `internal/core/focus` exposes

```go
type Candidate struct {
    ID            string
    Type          unit.Type
    Weight        float64
    DecayRate     float64
    LastTouchedAt time.Time
    CreatedAt     time.Time
    DueAt         *time.Time   // nil is not the zero time — I18
}

type Ranked struct {
    Candidate Candidate
    Score     float64
}

func Rank(cs []Candidate, adjacency map[string]float64, now time.Time) []Ranked
```

**MUST**: the tie-break is, in order: (1) higher `Score` first; (2) earlier `DueAt` first, with a
non-nil `DueAt` always before a nil one; (3) lexicographic by `ID`. Level 2 is where `type`'s
ordering job went (R3.2): with `priority` type-independent, a `task` and an `event` at equal score
are separated by which is due sooner, which is the answer a user would give. This mirrors
`recall.FuseScored`'s three-level precedent (`internal/core/recall/fuse.go:66-97`) for the same
stated reason — `-shuffle=on` and exact float ties in symmetric cases.

**MUST**: `adjacency` is a `map[string]float64` that **may be `nil`**; a nil map or an absent entry
is 0, so the term vanishes. This is what lets `m2a` ship a formula whose fourth term has no
producer in M2 (proposal §4.3): the caller with no relation graph loaded passes `nil` and gets a
well-defined ranking, and M4's today view fills it in without a signature change.

**Verified by**: L1 — a total-order test; a tie-break table exercising all three levels including
non-nil-before-nil; determinism under `-shuffle=on`; a `nil` adjacency map behaving as all-zero.

### R3.7 — `relation_to_active_focus` reads the PREVIOUS focus, never the one being computed

**Invention, and a deliberate break of an apparent circularity**: priority depends on the focus and
the focus is computed from priority. Left alone that is a fixpoint, and a fixpoint iteration inside
a ranking is both expensive and unstable (a unit's priority would depend on whether it is winning).
It is resolved by the same value hysteresis already needs.

**MUST**: `internal/core/focus` exposes `AdjacencyStrengths(previous Selection, edges []weight.Edge)
map[string]float64` computing

```
adjacency[v] = max over edges joining v to any member of previous.Members of strength(edge)
             = 0 when previous is empty, or v touches no member
```

**MUST**: `max`, not sum — the same rule as R2.5, for the same reason. A sum lets a hub unit weakly
connected to five focus members outrank a unit strongly connected to one, which measures graph
topology rather than relevance, and needs a normalization (by what — the focus size?) to stay
bounded.

**MUST**: `AdjacencyWeight` is a named constant in exactly one place, default `0.25` — slightly
above `AgeWeight`, because "this is about the thing you are already working on" is a stronger
signal than "this has been waiting", and well below the urgency ceiling, because it must never
override a deadline.

**MUST**: traversal is undirected, per R2.5's argument, and `focus` reuses `weight.Edge` rather
than declaring a second edge type.

**MUST**: `previous` and `edges` are ordinary parameters — `AdjacencyStrengths` and `Priority` read
no package-level or global state.

**MUST**: on the first ranking after a process restart, `previous` is empty, so adjacency is 0 for
every unit and the term vanishes entirely. Together with R4.4's empty-incumbent case this makes the
first ranking after a restart differ from the second in **two** ways, not one. This is owner ruling
round 2 #5's accepted cost, and the doc 02 §3 sentence that ruling owes gains **both** halves — the
proposal priced only the first.

**Verified by**: L1 — a unit joined to a previous-focus member at `strength = 0.7` asserting
`adjacency == 0.7`; a unit joined by two edges at 0.7 and 0.4 asserting 0.7 not 1.1; an edge stored
in the opposite direction asserting the same result; an empty `previous` asserting an empty map.

### R3.8 — `Priority` calls no clock, no I/O, and reads no OS state

**MUST**: `Priority`, `UrgencyRamp`, `AgeRamp`, `AdjacencyStrengths` and `Rank` take `now` as a
`time.Time` **named parameter** — never a struct field, never a `Clock` — and call none of
`time.Now`, `time.Since`, `time.Until`, `rand.*`, `uuid.*`, `os.Getenv`.

**MUST**: `now` never appears inside an input struct. `Candidate` carries `LastTouchedAt`,
`CreatedAt` and `DueAt` — data *about the unit* — and never the instant the decision is made, so
that `rg 'now time\.Time' internal/core/weight internal/core/focus` enumerates every time-dependent
decision `m2a` ships, exhaustively.

**Verified by**: `golangci-lint run` (`forbidigo`, `depguard`'s `core-purity` rule, per
`.golangci.yml:47-77` and `:96-119`). `forbidigo` bans those symbols **by call pattern**, so a
`time.Time` value or field is legal.

---

## 4. `internal/core/focus` — the two focuses and hysteresis (PR4 — `feat/core-focus-selection`)

Traced to `docs/02-cognitive-core.md` §3 ("Two focuses, one table"; "Anti-jitter hysteresis").

### R4.1 — Task focus and load focus are two selections over one ranking, not two schemas

> **Reconciled (rulings 7 and the naming alignment).** This requirement previously specified
> `ActionableTypes() []unit.Type` and an unnamed top-N selector taking `n` as a bare caller
> parameter with **no named default anywhere**, which violates `nooma-core` hard rule 4 (every
> behavioural number is a named constant in exactly one place and appears in doc 02 §13). Ruling 7
> adds `focus_size` (default 7) — `design.md` had it. The vocabulary function is `design.md`'s
> `Types(Kind)`, so the two focuses are one enumeration rather than one named helper plus an
> implicit second case.

**MUST**: `internal/core/focus` exposes

```go
type Kind string
const (
    KindTask Kind = "task"
    KindLoad Kind = "load"
)
func AllKinds() []Kind
func Types(k Kind) []unit.Type      // a fresh slice, never an exported var

const DefaultSize = 7

type Selection struct {
    Kind    Kind
    Members []string   // unit ids, in rank order
}

func Select(k Kind, ranked []Ranked, previous Selection, margin float64, size int) Selection
```

**MUST**: `Types(KindLoad)` is exactly `{mental_load}` — doc 02 §3 says so outright.

**MUST**: `Types(KindTask)` is exactly `{task, event}`. Doc 02 §3 says "top-N of actionable types"
and never enumerates "actionable types"; this is `m2a`'s scoping choice and **an owner-review
item**, stated explicitly rather than left implicit. The argument: the task focus answers "what
should I be doing", and a meeting in two hours is the strongest possible answer to that question,
so excluding `event` forces a third focus for it on day one; while a `list` is a container, and
putting a container in a focus of atoms means the focus sometimes holds a thing you cannot do.
`knowledge`, `procedural`, `emotional`, `structured_ref`, `insight` and `list` are in neither
focus.

**MUST**: `DefaultSize = 7`, **one** constant for both focuses. A focus is a human attention bound
and 7±2 is the least-invented number available for one; one constant rather than two follows
`fuse.go`'s `WeightVector = WeightLexical = 1.0` precedent — split it when data says the two
focuses want different sizes, not before. It coincides with doc 02 §13's `mental_load_threshold`
(7) and that is a **coincidence, not a relation** (that knob counts live `mental_load` units, not
focus members); no test ties them.

**MUST**: there is no second data shape and no second struct — the same `Select` called twice with
a different `Kind`.

**Verified by**: L1 — a mixed-type fixture asserting `Select(KindTask, …)` returns only
`task`/`event` members even when a `mental_load` unit outranks them by priority, and
`Select(KindLoad, …)` returns only `mental_load` members; `Types` returns a fresh slice on each
call (mutating the result does not affect the next); `AllKinds()` is exhaustive over the `Kind`
constants.

### R4.2 — `status='focus'` is never introduced, and cannot be expressed (I01, now load-bearing)

**MUST**: `Selection.Members` is `[]string` — unit ids, not units. A `[]unit.Unit` would be a
persistable shape and would put I01 one careless repository call away.

**MUST**: no exported function in `internal/core/focus` returns or embeds a `unit.Status`.

**MUST**: `internal/core/focus` declares **no package-level `var`**. A package with no mutable
state has no place to keep a focus between calls, and a package that returns ids has nothing to
write. That is the real proof, and it is what makes R4.5 structural rather than aspirational.

**MUST**: **no file under `internal/core/focus` contains the double-quoted literal `"focus"`** —
in code or in comments, unconditionally. I01's tree scan flags any Go line carrying both `"focus"`
and the substring `Status` (`test/conformance/i01_focus_never_persisted_test.go:93-95`), and this
package is the one place in the tree where both are natural. The rule is absolute rather than
conditional ("only when `Status` is on the same line") because a conditional rule is one refactor
away from tripping. `Kind`'s values are deliberately `"task"` and `"load"`.

**MUST NOT**: `unit.AllStatuses()` (owned by `internal/core/unit`, `m1a`) be modified by this
change to add a fifth member.

**Verified by**: L2 — `test/conformance/i01_focus_never_persisted_test.go` (existing, `m1a`) is
**not rewritten**; it gains a third check beside its two. Check 1 (`focus` not in
`unit.AllStatuses()`) is unchanged and still passing. Check 2 (the tree scan) is unchanged but now
non-trivial, since it scans a package literally named `focus` for the first time. Check 3 is new:
the structural assertions above — no `unit.Status` in the exported surface, `Members` is
`[]string`, no package-level `var`.

### R4.3 — Hysteresis is RELATIVE: a challenger displaces by a ratio, not by an absolute band

> **Reconciled (ruling 6).** This requirement previously specified an **absolute** margin —
> `challengerPriority > incumbentPriority + hysteresis_margin` — and read doc 02 §13's default of
> 0.05 as an absolute quantity. That reading is not an independent preference: it is *entailed by
> ruling 1*. Under the multiplicative envelope `priority` has **no fixed scale** — it is
> `effective_weight` times up to 4.35 — so an absolute 0.05 is a 5 % band at priority 1.0 and a
> 1.25 % band at priority 4.0, damping weakest exactly where the contested values are largest.
> `design.md` D8's relative form is adopted, and doc 02 §13's row gains "(relative)" so the reading
> is recorded rather than inferred. §13 already carries a ratio-shaped margin and labels it in its
> own row (`correction_referent_margin (ratio of the top two fused scores)`, `02:597`), so this is
> a pattern in the repo, not an invention. **The argument order also changes**: `Displaces` takes
> `(challenger, incumbent, margin)`, `design.md` D8's order.

**MUST**: `internal/core/focus` exposes
`Displaces(challenger, incumbent, margin float64) bool` returning
`challenger > incumbent × (1 + margin)` — strict inequality, matching doc 02 §3's "beat… by more
than".

**MUST**: equality does not displace. The incumbent wins ties, which is the entire point of
hysteresis.

**MUST**: `Displaces` takes no `now`. **Hysteresis is time-independent** — it compares two scores
that `Rank` already produced with `now` — which is why it can be tested without a fake clock at
all.

**MUST**: doc 02 §13's `hysteresis_margin` row is amended in place to `hysteresis_margin (focus,
relative)`. Its default (0.05) does not change.

**Verified by**: L1 — a boundary table: `challenger == incumbent` → false;
`challenger == incumbent × (1 + margin)` → false; `challenger == incumbent × (1 + margin) + ε` →
true; `challenger < incumbent` → false. Plus **L2** — `test/conformance/i19_hysteresis_margin_test.go`
naming the invariant in its identifier (`TestI19_ChallengerMustExceedRelativeMargin` or
equivalent), registered in `docs/06-harness.md` §4's table first, per `nooma-testing` step 2.

**Scenario**:
- GIVEN an incumbent at priority 0.60 and a challenger at 0.62, `margin = 0.05`
- WHEN `Displaces` is called
- THEN it returns `false` — 0.62 is not greater than 0.63

### R4.4 — `hysteresis_margin` resolves from config with a named Go fallback, pinned to the DDL

> **Reconciled (ruling 5).** This requirement is **new**. The previous revision said only that
> `hysteresis_margin` "is read from a single named constant with default 0.05 — not a new constant,
> reused as-is", and specified no resolution path. That omits owner ruling round 2 #2's mandated
> pattern, which `design.md` D4 had: the `config` singleton row **has never existed in any vault**
> (proposal R1), so every read of it returns nothing, and the resolution must be explicit. Ruling 5
> adopts `design.md`'s. Note that `weight_threshold`'s equivalent goes to `m2b` under ruling 4 —
> only the focus half lands here.

**MUST**: `internal/core/focus` exposes

```go
const DefaultHysteresisMargin = 0.05
func ResolveMargin(configured *float64) float64
```

returning `DefaultHysteresisMargin` when `configured` is `nil` and `*configured` otherwise. This is
`relation.Resolve`'s shape verbatim (`internal/core/relation/thresholds.go:26-38`), including the
`nil` sentinel meaning "no row". `m2c` supplies the `*float64`; `m2a` supplies the meaning of
`nil`.

**MUST**: `DefaultHysteresisMargin` equals the column `DEFAULT` at
`internal/store/sqlite/migrations/0002_learning_and_search.sql:64`, asserted by an **L2** test that
reads the SQL text off disk via the existing `migrationSQLText` helper
(`test/conformance/i13_learning_signal_test.go:24`) — the same mechanism
`test/conformance/relation_thresholds_ddl_test.go` already uses.

**MUST NOT**: this change declare a Go home for `weight_threshold`. That is `m2b`'s
`feat/core-consolidation-expire-archive` (proposal §5.1:348, ruling 4). `m2a`'s two constant
relations (R2.4, R2.7) assert against the migration text directly.

**Verified by**: L1 — `ResolveMargin(nil)` returns `DefaultHysteresisMargin`, a non-nil pointer
passes through. **L2** — the DDL pin.

### R4.5 — An empty incumbent set reduces `Select` to a plain top-N

**MUST**: when `previous.Members` is empty — the first computation after a process start — `Select`
reduces to taking the top `size` of `ranked`, with no hysteresis comparison performed.

**MUST**: an incumbent that is no longer present in `ranked` — archived since, or now the wrong
type for this `Kind` — is simply absent and is dropped with no contest. Stated because "the
incumbent always wins ties" invites the opposite reading.

**Verified by**: L1 — an empty `previous` asserting the top-N by score is selected regardless of
margin; an incumbent absent from `ranked` asserting it does not appear in the result and blocks
nobody.

### R4.6 — The previous focus is a parameter, never package-level mutable state

**MUST**: every selection and hysteresis function in `internal/core/focus` takes the previous
focus as an explicit `Selection` parameter. No package-level variable, no `sync.Map`, no init-time
state holds a focus between calls (R4.2 makes this structural: the package declares no
package-level `var`).

**MUST NOT**: this change implement the process-lifetime holder that remembers the previous focus
across separate calls to a running service. Per owner ruling round 2 #5 ("the previous focus is
remembered in process, not in a state row"), *who* holds that value between calls is a
runtime-orchestration decision. `internal/core/focus` is pure and stateless; the holder belongs to
`internal/brain`, which does not exist in `m2a`'s scope. Stated explicitly so a reader does not
assume `m2a` ships the in-process memory itself, only the pure function it is built on.

**Verified by**: L2 — R4.2's check 3 (no package-level `var` in `internal/core/focus`) is the
mechanical proof. `golangci-lint` alone would not catch this, since `sync` is unrestricted stdlib;
the conformance check is what makes it a property rather than a review habit.

### R4.7 — Task focus and load focus each keep an independent incumbent set

**MUST**: hysteresis for the task focus is evaluated against the previous task-focus `Selection`
only; hysteresis for the load focus against the previous load-focus `Selection` only. The two
focuses never share an incumbent set, even though `AdjacencyStrengths` (R3.7) may be computed over
their union.

**Verified by**: L1 — a test where a unit displaces an incumbent in the load focus, asserting the
task focus's incumbents are unaffected by that same call sequence.

### R4.8 — If `Select` implements hysteresis as an adjusted sort, the two spellings are proven equal

**MUST**: `Displaces` is the **definition** of hysteresis — it is what I19's conformance test
names. If `Select` implements the rule differently for efficiency (`design.md` D8 proposes sorting
by `Score × (1 + margin)` for incumbents and `Score` for everyone else, then taking the top
`size`), an **L1 test MUST assert the two agree** over a boundary table including all three edge
cases from R4.3.

**MUST**: this is recorded as an accepted risk with a mechanism, not designed away — two spellings
of one rule is exactly the drift `m1a` D3 warns about.

**Verified by**: L1 — the equivalence table.

---

## 5. Calibration constants (`docs/02-cognitive-core.md` §13)

> **Reconciled (ruling 7).** The previous revision's table listed **seventeen** new constants.
> Nine of them existed **only** to support the linear weighted sum that ruling 1 removed, or the
> additive-clamp boost that ruling 2 removed, or the numeric type term that ruling 8 removed:
> `priority_weight_effective_weight`, `priority_weight_temporal_urgency`, `priority_weight_type`,
> `priority_weight_age`, `priority_weight_relation`, `priority_weight_cap`,
> `priority_type_prior_task`, `priority_type_prior_event`, `priority_type_prior_mental_load` — and
> two more from the boost, `revive_weight_floor` and `resurface_min_boost`. Eleven cut. Two were
> renamed to `design.md`'s names for the same quantity (`temporal_urgency_horizon_days` →
> `urgency_lead_days`; `revive_boost_amount`/`revive_weight_cap` → `revive_gain`/`weight_ceiling`;
> `resurface_hop_limit`/`resurface_attenuation_per_hop` → `resurface_max_hops`/
> `resurface_attenuation`). One was **added**: `focus_size`, which this document previously took as
> an unnamed caller parameter with no default anywhere — a `nooma-core` hard rule 4 violation.
> `age_horizon_days` survives ruling 9 with its value intact; `age_weight` (0.20) inherits its
> magnitude and its argument from the `novelty_weight` that ruling 9 removed from `design.md`.

### 5.1 New rows — ten

| Constant | Default | Go home | Package | PR | Chosen or derived |
|---|---|---|---|---|---|
| `focus_size` | 7 | `focus.DefaultSize` | `internal/core/focus` | 4 | chosen — a human attention bound, 7±2 |
| `urgency_lead_days` | 7 | `focus.UrgencyLeadDays` | `internal/core/focus` | 3 | chosen — a week is the horizon people plan against |
| `urgency_max` | 3.0 | `focus.UrgencyMax` | `internal/core/focus` | 3 | chosen — a due-today unit at the archive floor (0.5 × 3 = 1.5) outranks a healthy non-urgent unit at 1.0; the derivation runs backwards from the desired behaviour, so it is a choice |
| `age_weight` | 0.20 | `focus.AgeWeight` | `internal/core/focus` | 3 | chosen — at most one fifth, so age breaks close contests and never wins them (R3.5 P2: 16.7 % overturnable deficit). **Explicitly left alone by owner ruling 10**, which moved `age_horizon_days` instead precisely because this constant is the one that would have changed P1 and P2 |
| `age_horizon_days` | **15** | `focus.AgeHorizonDays` | `internal/core/focus` | 3 | **owner ruling 10's number.** It was 30 under ruling 9, and 30 was rejected once R3.5 P4 showed it left priority strictly decreasing across the whole horizon at the base λ. 15 puts the break-even λ (`age_weight/age_horizon_days` = 0.01333/day) above doc 02 §13's base 0.01/day |
| `focus_adjacency_weight` | 0.25 | `focus.AdjacencyWeight` | `internal/core/focus` | 4 | chosen — above `age_weight`, far below the urgency ceiling |
| `revive_gain` | 0.35 | `weight.ReviveGain` | `internal/core/weight` | 2 | chosen, then **pinned** by R2.4's relation |
| `weight_ceiling` | 2.0 | `weight.WeightCeiling` | `internal/core/weight` | 2 | chosen — two doublings of headroom above doc 02 §13's base weight of 1.0; **pinned** by R2.4 and R2.7 |
| `resurface_max_hops` | 2 | `weight.ResurfaceMaxHops` | `internal/core/weight` | 2 | chosen — the smallest genuinely transitive number; bounds the work at O(branching²) and the `m2c` query with it |
| `resurface_attenuation` | 0.5 | `weight.ResurfaceAttenuation` | `internal/core/weight` | 2 | chosen — distance must cost something independent of how sure the judge was; **pinned** by R2.7 |

### 5.2 Amended row — one

| Constant | Change | PR |
|---|---|---|
| `hysteresis_margin` (focus) | text amended to `hysteresis_margin (focus, relative)` (R4.3, ruling 6). Default unchanged at 0.05. Gains a Go home, `focus.DefaultHysteresisMargin` + `ResolveMargin` (R4.4, ruling 5) | 4 |

### 5.3 Rows this change does NOT touch

| Constant | Why not |
|---|---|
| `weight_threshold` (archiving) | Ruling 4 — its Go home is `m2b`'s `feat/core-consolidation-expire-archive` (proposal §5.1:348). `m2a` asserts R2.4 and R2.7 against the **migration DDL text**, not against a Go constant |
| `Event lead time` (7 days) | A separate knob from `urgency_lead_days` (R3.3) despite the identical default. Prospection's notification horizon is not the ranking's |
| `mental_load_threshold` (7) | Coincides with `focus_size` and is unrelated (R4.1). No test ties them |

### 5.4 Final count

`docs/02-cognitive-core.md` §13 currently holds **23** rows (`02:575-597`). *(`design.md` §1's
ground-truth table said 25; that was a miscount of the header and separator lines, corrected in
both documents.)* After `m2a`: **33 rows**, of which one of the original 23 — `hysteresis_margin` —
is amended in place.

Every behavioural number in this change is a named constant in exactly one place and appears in
that table (`nooma-core` hard rule 4). The quantities that are **not** knobs and correctly have no
row: the maximum context amplification 4.35 (R3.1 — the product of three knobs, asserted not
declared), the anti-starvation overturnable deficit 16.7 % (R3.5 P2 — `age_weight/(1+age_weight)`),
the anti-starvation break-even λ **0.01333/day** (R3.5 P4 — `age_weight / age_horizon_days`), the
peak-at-the-horizon threshold λ 0.01111/day (R3.5 P4 — `age_weight / (age_horizon_days ×
(1+age_weight))`), and the peak lift 1.0328× (R3.5 P5). Each is asserted by a test **computed from
its constants**, never written as a literal — which is why owner ruling 10's change of one constant
propagated to five derived figures without a single literal needing to be hunted down in code.

None of the ten new rows is marked ⚙ (learning-recalibratable) — doc 02 §9's learning module is out
of `m2a`'s scope and out of M2 entirely for these.

All ten values remain this change's proposal, not an owner-approved calibration. Rulings 8 and 9
are the two the owner has ruled on; the numeric defaults are still open to adjustment without
changing any term composition.

---

## 6. Purity and structural constraints (MUST NOT)

- **MUST NOT**: any file under `internal/core/weight/**` or `internal/core/focus/**` import
  anything beyond the standard library and `internal/core/**` — `depguard`'s `core-purity` rule
  (`.golangci.yml:47-77`) enforces this; no import of `internal/store`, `internal/providers`,
  `internal/ports`, `internal/scheduler`, or any external dependency. `math`, `time` and `sort` are
  legal; `internal/core/unit` and `internal/core/weight` (from `focus`) are legal.
- **MUST NOT**: any file under `internal/core/weight/**` or `internal/core/focus/**` call
  `time.Now`, `time.Since`, `time.Until`, `rand.*`, `uuid.*`, or `os.Getenv` — every instant is a
  `time.Time` **parameter** (R3.8), never a struct field and never a `Clock`.
- **MUST NOT**: any function in this change read the user's timezone from the OS or from any other
  source. No formula here does calendar-date arithmetic: every elapsed time is a duration ratio
  (`Sub(...).Hours() / 24`), never a day-boundary count, so no `time.Location` is needed or
  accepted. A day-boundary count would also make the decay curve a step function.
- **MUST NOT**: `internal/core/weight` import `internal/core/focus`. The arrow points one way —
  `focus → weight` (for `Effective`) — or the packages cycle. `ZoneOf` therefore takes
  `inFocus bool`, not a `focus.Selection` (R1.4).
- **MUST NOT**: any code in this change write to `decision_log` — `m2a` is pure and calls no
  repository; `decision_log` writes belong to `internal/brain` (`m2c`).
- **MUST NOT**: `unit.AllStatuses()` gain a `"focus"` member, and no line anywhere under
  `internal/` may pair the literal `"focus"` with a status comparison or assignment (I01, R4.2).
- **MUST NOT**: any selection or hysteresis function in `internal/core/focus` accept, return, or
  mutate a `unit.Status` value (R4.2).
- **MUST NOT**: `internal/core/focus` declare any package-level `var` (R4.2, R4.6).
- **MUST NOT**: this change touch `internal/ports`, `internal/store`, `internal/brain`,
  `internal/scheduler`, `cmd/nooma`, or any file under `internal/store/sqlite/migrations/`.
- **MUST NOT**: any behavioural number in this change appear as a literal outside its named
  constant (`nooma-core` hard rule 4) — every value in §5.1 is declared once. Test files may carry
  expected values as literals; production code may not.
- **MUST NOT**: `Resurface`'s traversal fail to terminate on a cyclic relation graph — R2.5's hop
  bound and `max`-over-paths semantics are the structural guarantee, not a runtime timeout.
- **MUST NOT**: this change declare `weight_threshold`'s Go home (ruling 4, R4.4).

---

## 7. Doc 02 amendments this change makes (same PRs as the code, per non-negotiable #1)

- **§2, in PR1**: `ZoneOf`'s totality — `superseded` and `incomplete` map to Cold, and temperature
  is not a function of time (R1.4). The negative-Δt clamp and the postcondition `effective_weight ≤
  weight` (R1.2).
- **§2, in PR2**: replace "writes a new boosted weight" with the asymptotic mechanism, stating
  **verbatim** that the boost is applied to the *effective* weight at `now` and not to the
  persisted weight (R2.2) — the single most consequential sentence in this change. Add that a
  direct revive at or above the ceiling still moves `last_touched_at` and why (R2.3). Replace
  "propagates a boost along the graph edges" with the hop bound, the attenuation, the
  gain-scales-the-target rule, `max`-over-paths, undirected traversal, and cycle termination
  (R2.5). Add **both halves** of the resurface write rule: it resets `last_touched_at`, *and* it
  writes only when it genuinely lifts something (R2.6) — the first half alone reads as a bug. Add
  the headline guarantee that spreading activation alone cannot hold a unit above the archive
  threshold at maximum hop distance (R2.7).
- **§3, in PR3**: rewrite line 76 from five terms to **four**, dropping `type` (R3.2, owner ruling
  8), as `priority = f(effective_weight, temporal_urgency(due_at), age, relation_to_active_focus)
  over the units the focus's type criterion already selected`. State the multiplicative envelope and
  why the shape is not a sum (R3.1). Define `age` as **anti-starvation over 15 days, older ranks
  higher** (R3.4, owner rulings 9 and 10) — doc 02 has never defined the word, and this is the
  amendment that fixes it. State in prose that anti-starvation is **bounded and transient**: a
  genuine lift peaking at about two weeks at a few percent and declining monotonically thereafter,
  re-ranking among units that still hold weight and never resurrecting units that have lost it,
  with the numbers of §9 (R3.5 P1–P6). **The amendment must not say "does not stay buried
  forever"** — that framing is exactly what ruling 10 exists to correct, and doc 02 is the one
  place where writing it down would make it permanent.
- **§3, in PR4**: define "actionable types" as `{task, event}` (R4.1, flagged as this change's
  scoping choice and an owner-review item). State that hysteresis is **relative** (R4.3). Add the
  sentence owner ruling round 2 #5 owes, with **both** of its halves: the previous focus is
  remembered in process, at the cost of one un-damped transition per restart *and* one ranking with
  no adjacency term (R3.7).
- **§13**: the ten new rows of §5.1 and the one amendment of §5.2, split across the PRs that
  introduce each constant's home.
- **`docs/06-harness.md` §4**: I24's row, in PR2 (R2.1). I19's row, in PR4 (R4.3), before its test.

`docs-sync.yml` fires on `^internal/core/`, and **every** `m2a` PR touches `internal/core/`, so
every one carries a genuine doc 02 delta — which it does, by construction. No `m2a` PR should need
the `no-spec-change` label; one that does is not implementing a behaviour doc 02 describes.

---

## 8. Test levels

**MUST**: every formula in §1–§4 (`Effective`, `ZoneOf`, `Revive`, `Resurface`, `Priority`,
`UrgencyRamp`, `AgeRamp`, `AdjacencyStrengths`, `Rank`, `Types`, `AllKinds`, `Select`, `Displaces`,
`ResolveMargin`) is **L1** — pure functions, tested next to the code in `internal/core/weight/` and
`internal/core/focus/`, no database, no network, no process. Per `nooma-testing`'s decision gate:
*when torn between L1 and L3, it is L1.*

**MUST**: exactly **five** tests in this change are **L2**, and each is L2 because it names a doc 02
invariant or pins a Go value to schema text — never merely because it is important:

| L2 test | Why L2 | PR |
|---|---|---|
| `i05_effective_weight_computed_on_read_test.go` — structural, reflects over `core/weight`'s exported surface (R1.3) | names I05 | 1 |
| `weight_constant_relations_ddl_test.go` — R2.4 and R2.7 against `0002:63`'s `DEFAULT` | pins arithmetic to schema text | 2 |
| `focus_margin_ddl_test.go` — `DefaultHysteresisMargin` against `0002:64`'s `DEFAULT` (R4.4) | pins a Go constant to schema text | 4 |
| `i19_hysteresis_margin_test.go` — new (R4.3) | names I19 | 4 |
| `i01_focus_never_persisted_test.go` — existing, gains check 3 (R4.2) | names I01 | 4 |

**MUST**: I24 is a **new invariant**, defined by this change (R2.1). Its row lands in
`docs/06-harness.md` §4 in PR2 — the definition, not yet a store-level test, since `ports.UnitRepo`
does not exist until `m2c`. `m2a`'s own proof is the type-level guarantee (`Boost` cannot omit
`LastTouchedAt`); the structural L2 test over a real repository method is `m2c`'s. Splitting a
harness row from its test across two changes is unusual and is called out here so it is not read as
an omission: the alternative — `m2c` adding the row — would mean `m2a` ships the `Boost` type with
the invariant it exists to serve undocumented for three changes.

**MUST**: each test in this change is written **before** its implementation and lands as its own
commit ahead of the implementation commit within the same PR. Because `scripts/pending-red.sh` is
retired (`714934e`), a test naming an undefined core symbol now breaks the **untagged** build —
a compile error, which is not "red for the right reason". The procedure every `m2a` PR follows:
**commit 1** is the test *plus* a stub with the final signature returning the zero value (the suite
compiles, the assertion fails); **commit 2** is the implementation.

**MUST**: the L1 table and its implementation ship in the **same PR**, always.
`scripts/core-coverage.sh` never runs in `make check` (`Makefile:36`), and `m2a` is almost entirely
`internal/core/**`, so the ≥ 90 % floor is only ever seen by `make check-all` and CI. `make
check-all` is the pre-PR command structurally, not as a reminder.

**MUST NOT**: any test in this change touch the network, a real LLM, or the real clock — every test
constructs its own `time.Time` fixtures.

---

## 9. What anti-starvation actually ships — stated narrowly on purpose

Ruling 1 (the multiplicative envelope) and ruling 9 (`age` means anti-starvation) were adjudicated
independently, and they interact. Neither original artifact could have seen it, because neither
held both. This section stated that interaction in numbers; the owner read it and pulled a lever
(**ruling 10**, `age_horizon_days` 30 → 15). What follows is what the change ships **after** that
lever, with the same numbers and the same refusal to round them in the flattering direction.

> **Reconciled (ruling 10).** This section previously concluded: *"an old ignored task does not stay
> buried forever is not what this ships"*, on the finding that at a 30-day horizon an untouched
> unit's priority decreased across the *entire* horizon at the default λ. That finding stands and is
> what motivated ruling 10. **The conclusion is now weaker but still true**, and the temptation this
> revision must resist is restating a small transient lift as the permanent floor the original
> framing promised. It is not one.

### What is now true

**There is a genuine rise, and it happens for ordinary units.** The break-even is
`age_weight / age_horizon_days` = `0.20/15` = **0.01333/day**, which sits above doc 02 §13's base
λ of 0.01/day — and `internal/core/classify/prior.go:25` assigns exactly 0.01 to every unit unless
the model overrides it, so the ordinary case is the common case. An untouched unit's priority
climbs from the moment it is captured.

**The rise peaks at two weeks, at 3.3 %, and then declines forever.** The maximum is at exactly
`t = age_horizon_days` = day 15, at `exp(-0.15) × 1.20` = **1.0328×** the unit's own day-0
priority. Past the horizon the ramp is saturated and priority is `1.20 · w · exp(-λt)` — pure decay
scaled by a constant.

### What is still not true, and must not be implied

1. **Age's entire lifetime leverage is 20 %.** With no deadline and no adjacency,
   `priority ≤ e × (1 + age_weight)` = `e × 1.20`, at any age, forever. Ruling 10 did not change
   this.
2. **Age can overturn an effective-weight deficit of at most 16.7 %** —
   `age_weight / (1 + age_weight)`, which carries **no `age_horizon_days` term at all** and is
   therefore *identical* before and after ruling 10. A unit at the archive floor (`e = 0.5`)
   reaches at most priority 0.60 at full age; a healthy unit at classify's base weight of 1.0 with
   no context beats it by 1.67×. **Ruling 10 bought earlier arrival, not more power** — which is
   precisely why the owner moved the horizon instead of raising `age_weight`.
3. **From day 30 onward the change is worth exactly nothing.** Because the bonus saturates either
   way, a 15-day horizon and the rejected 30-day horizon give **numerically identical** results at
   every instant past day 30: 0.8890 of the day-0 priority at day 30, 0.6586 at day 60. The lever
   bought a **two-week window**, not a floor. There is no floor.
4. **A unit decayed toward zero still cannot climb on age alone.** The age term multiplies `e`; it
   does not add to it. A unit that decay has taken below `weight_threshold` is on its way to
   `archived` (doc 02 §6), which is the disposition doc 02 §1 designs for it — not to the focus.
   Giving age genuine rescuing power requires making it **additive**, which is the shape ruling 1
   rejected for letting a forgotten item outrank a live one by roughly 4× on context alone. There
   is no third option that keeps both properties.

**So: what ships is a two-week grace window in which a newly captured, untouched unit gets a few
percent of help holding its place, followed by ordinary decay.** That is a real and defensible
behaviour, and it is what doc 02 §3's amendment should say. It is *not* "an old ignored task does
not stay buried forever", and §7 forbids that phrasing from entering doc 02 specifically because
doc 02 is where a wrong framing becomes permanent.

### One dependency worth watching

The break-even is 0.01333/day and the base λ is 0.01/day — a margin of **33 %**. Doc 02 §2 says
type "orients the direction" of λ, with **`task` → high λ**, and `prior.go:9-19` deliberately
declines to encode per-type numbers because doc 02 names none and the model is meant to decide.
Today that means every unit gets 0.01 and the rise is universal. **But the moment the model starts
personalizing λ upward for tasks — which doc 02 §2 explicitly directs it to — any task above
0.01333/day loses the rise entirely**, and tasks are the population anti-starvation is nominally
for. At λ = 0.02 the priority declines from day 0 with no rise at all.

This is not a defect in `m2a` and nothing here should be changed for it. It is a **calibration
dependency between two milestones** that should be watched when M5's learning module starts tuning
λ, and the levers remain what they were: shorten `age_horizon_days` further (raises the break-even
proportionally, costs nothing structurally) or raise `age_weight` (raises the break-even *and* P1
and P2, which is why it was not the lever this time).

---

## 10. Reconciliation — every ruling from #650 plus ruling 10, and where each landed

Rulings 1–9 are `sdd/m2a-weight-focus/adjudication` (#650). **Ruling 10 is not in #650**: it is a
follow-up owner decision taken after this reconciliation delivered R3.5's boundary numbers, and it
is recorded here in the same table because a reader tracing `age_horizon_days` needs both entries
in one place.

| # | Ruling | Applied where in this document |
|---|---|---|
| 1 | Priority is `design.md`'s **multiplicative envelope**; the linear weighted sum is defectively ordered | §3 preamble note; **R3.1** rewritten from a weighted sum; `priority ∈ [0,1]` claim withdrawn; §5's `priority_weight_*` rows cut |
| 2 | Boost is **one asymptotic mechanism at two strengths**; **and** at `e ≥ WeightCeiling` a direct revive must still move `last_touched_at` | §2 preamble note; **R2.1**–**R2.3** rewritten; `Revive`'s signature loses its `bool`; **R2.5**–**R2.7** replace the additive resurface; `revive_boost_amount`/`revive_weight_floor`/`revive_weight_cap`/`resurface_min_boost` cut |
| 3 | Adopt `design.md`'s **negative-Δt clamp** | **R1.2**, new; the same rule reapplied at **R3.4** |
| 4 | **`weight_threshold` goes back to `m2b`** | Scope boundary; **R2.4** and **R2.7** re-pointed at the migration DDL text; **R4.4** last MUST NOT; §5.3 |
| 5 | Adopt `ResolveMargin` + the DDL-pinning L2 test for `hysteresis_margin` | **R4.4**, new; §8's L2 table |
| 6 | Hysteresis is **relative**; §13's row gains "(relative)" | **R4.3** rewritten from an absolute band; argument order changed to `(challenger, incumbent, margin)`; §5.2 |
| 7 | Cut the constants that only supported the linear sum; **add `focus_size`** (7) | §5 preamble note; §5.1's ten rows; **R4.1** gains `DefaultSize` |
| 8 | **OWNER** — `type` leaves the priority arithmetic; doc 02 §3:76 goes from five terms to four; ties broken by `DueAt` | **R3.2**, replacing the `typeTerm` requirement; **R3.6** level 2; §5's three `priority_type_prior_*` rows cut; §7's §3 amendment |
| 9 | **OWNER** — `age` means **anti-starvation**, 0 → 1 over `age_horizon_days`, older ranks higher; `NoveltyRamp` rejected | **R3.4** (this document's reading was already the ruled one, now stated against `design.md`'s rejected opposite); `age_horizon_days` retained as the constant; `age_weight` inherits `novelty_weight`'s magnitude |
| — | **Flagged interaction**: rulings 1 + 9 mean a unit decayed toward zero cannot climb on age alone | **R3.5** (asserted properties P1–P6) and **§9** (the plain-language limitation, for owner objection). *This is the entry that produced ruling 10* |
| 10 | **OWNER, taken in response to R3.5** — `age_horizon_days` **30 → 15**; `age_weight` stays 0.20; nothing else about ruling 9 changes. 30 was rejected because at the base λ = 0.01/day it left priority strictly decreasing across the whole horizon (`exp(-0.3) × 1.20 = 0.8890 < 1`), making the promise it was chosen under false as specified | **R3.4**'s second note and its default; **R3.5 P4 rewritten** (the assertion's sign flips) and **P5**/**P6** added; **R3.5's P4/P5 test fixture changes shape** — it now asserts a rise to a maximum at the horizon, the opposite of what it asserted before; §5.1's two age rows; §5.4's derived-figure list; §7's §3 amendment, which now **forbids** the "does not stay buried forever" phrasing from entering doc 02; **§9 rewritten** end to end |

Two divergences the adjudication did not rule on, because they are naming and totality conflicts
rather than formula conflicts, resolved here in `design.md`'s favour and recorded so they are not
mistaken for silent drift:

| # | Divergence | Resolution |
|---|---|---|
| C-a | This document had `ThermalZone`, partial, with `superseded`/`incomplete` deliberately untested; `design.md` D2 had `ZoneOf`, total, both mapping to Cold, tested over the full matrix | `design.md`'s. A deliberately untested arm is an uncovered statement against a ≥ 90 % floor, and a partial function whose contract says "do not call it this way" is a rule somebody remembers. **R1.4** |
| C-b | Identifier drift throughout: `ThermalZone`/`ZoneOf`, `ActionableTypes()`/`Types(Kind)`, `Displaces(incumbent, challenger, …)`/`Displaces(challenger, incumbent, …)`, `resurface_hop_limit`/`resurface_max_hops`, `temporal_urgency_horizon_days`/`urgency_lead_days` | `design.md`'s names throughout, since `design.md` owns the signatures. Every identifier in this document is now the one `design.md` declares |
| C-d | **R2.3 states, as a MUST, that `Revive`'s signature is `func Revive(c Current, now time.Time) Boost` — "no `bool`, no `*Boost`, no 'maybe'".** The shipped signature is `(Boost, bool)` | **The code's, and this row is the record.** Ruling 2 removed an earlier `bool` while reasoning about the **ceiling edge**, where a direct use always writes and the `bool` genuinely had no false case. Judgment Day round 1 on PR #137 found a case that ruling never considered: a **non-finite** computed weight. `Revive` is the write path — `Boost` is the only shape the package lets a caller persist — so a `NaN` reaching it becomes durable in a column where nothing is ever deleted, and every later `Effective` on that unit returns `NaN` forever. Refusal was chosen over coercion because coercing to 0 would drive the unit under `weight_threshold` and **archive** it on the strength of a corrupt read. The `bool` is `false` for exactly the non-finite case and nothing else — two independent fuzzers (2,000,000 and 200,000 samples) found zero false refusals. **R2.3's text is left as written**, annotated in place, per this repository's rule that a planning artifact is a historical record |
| C-c | **R1.2 below states `Effective(w, λ, lt, now) ≤ w` over the raw `w`.** The shipped code guarantees `≤ max(w, 0)` over *sanitized* inputs, because it also clamps a negative `weight` and a negative `decayRate` to zero. Neither this document nor `design.md` D1 ever considered a negative `w` or a negative `λ` — both reason only about Δt — so the clamp is an extension invented during implementation, not a reading of R1.2 | **The code's, and this row is the record.** Two blind reviewers demonstrated R1.2's literal claim false against shipped M1 code: `weight` and `decay_rate` come from LLM JSON through `classify`'s `assignFloat`, which validates only that the value is a number, and `0001_core_tables.sql:11-12` declares both columns `REAL NOT NULL` with no `CHECK`. `Effective(1.0, -0.01, …) = 2.718` and `Effective(-1.0, 0.01, …) = -0.368`, each greater than its own `w`. `docs/02-cognitive-core.md` §2 carries the corrected guarantee, qualified to **finite** inputs. **R1.2's text is left as written**, per this repository's rule that a planning artifact is a historical record and is annotated rather than rewritten to match what shipped — this row is the annotation |
| C-e | **R3.1 below states `priority ≥ e` for every input.** Judgment Day round 1 on this PR (`feat/core-focus-priority`, both blind judges) found two ways it did not hold as shipped: `adjacency`, an ordinary unclamped `float64`, produced `priority < e` for `adjacency < 0` — `Priority(Candidate{Weight: 1.0, DecayRate: 0, LastTouchedAt: now, CreatedAt: now}, -1.0, now) = 0.75` against `e = 1.0`; and `e` itself is `weight.Effective`'s own output, which C-c above already documents as unqualified only over **finite** `weight`/`decayRate` — `Priority` inherits that boundary, it does not add a new one | Two different resolutions, not one. The `adjacency` gap is **fixed in code**, not merely annotated: `Priority` now clamps `adjacency` to `[0,1]` at its own entry point, the same entry-point-clamp discipline `weight.Effective` and `spread.go`'s `clampStrength` already follow — every finite `adjacency` now satisfies the MUST, closing that half of the gap entirely. The finite-`weight`/`decayRate` half is not a new gap: it is C-c's own carve-out, propagated through `weight.Effective` into `e`, and is corrected the same way C-c was — `docs/02-cognitive-core.md` §3 now states `priority ≥ effective_weight` over finite `weight`/`decay_rate`, and `internal/core/focus.Priority`'s own doc comment states the same restriction. **R3.1's text is left as written**, per this repository's rule that a planning artifact is a historical record and is annotated rather than rewritten to match what shipped — this row is the annotation |

---

## 11. Open items this change deliberately leaves to later changes

- **Exact numeric calibration of the ten §5.1 constants** — proposed so the conformance tests have
  concrete numbers to assert. The owner may adjust any value without changing any term
  composition; §9 names the two levers that change anti-starvation's strength without changing its
  shape.
- **Whether `list` or `procedural` should ever join `Types(KindTask)`** — R4.1 scopes it to
  `{task, event}` and flags it as an owner-review item; widening it is a future decision.
- **Whether a numeric `type` term ever returns to `priority`** — owner ruling 8 removed it, and
  reinstating it is **additive**: a new term, its own §13 rows, its own review. Not a rewrite.
- **Whether λ changes on use.** Spaced repetition says a thing used repeatedly should be forgotten
  more slowly; doc 02 §2 assigns λ at classify and says nothing about revising it. `m2a` leaves λ
  untouched (R2.2). Changing it is a doc 02 §2 amendment with its own §13 consequences and belongs
  with the learning module (doc 02 §9, M5).
- **The in-process holder of the previous focus between calls** — R4.6 ships the pure function
  only; the holder is `m2c`/`m2d`'s (`internal/brain`).
- **I05's structural (store-level) half, and I24's structural (port-level) half** — both `m2c`'s,
  once `ports.UnitRepo` gains a weight-write method taking a `weight.Boost`.
- **`Resurface`'s trigger, and the 2-hop neighbourhood loader.** *What* calls `Resurface` and
  *when* is a `brain` concern; and `m2c` inherits the obligation to load exactly the closure
  `Neighbourhood` describes — a repository shape no port has today (`design.md` §8 R6).
- **`weight_threshold`'s Go home and its `ResolveThreshold`** — `m2b`'s
  `feat/core-consolidation-expire-archive` (ruling 4).
