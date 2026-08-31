# Design — M2 Phase A: weight and focus

Technical design for `m2a-weight-focus`, the first of the four chained changes
[`m2-sleep-weight/proposal.md`](../m2-sleep-weight/proposal.md) §5 splits M2 into. Scope is that
document's **m2a block only** — the four PRs `feat/core-weight-decay`, `feat/core-weight-boost`,
`feat/core-focus-priority`, `feat/core-focus-selection`. `m2b` (consolidation core), `m2c`
(runtime) and `m2d` (scheduler and demo) are out. *(Those four are the plan of record's slices;
§8.1 re-derives their budgets after the reconciliation and splits three of them, landing `m2a` at
seven PRs. `sdd-tasks` plans against §8.1.)*

`m2a` ships two packages that are `doc.go`-only today and touches nothing else: `internal/core/weight`
and `internal/core/focus`. Zero ports, zero store, zero brain, zero I/O.

**This design is mostly not about code.** Owner ruling round 2 #1 made M2 a behaviour-*defining*
milestone, and `m2a` owns three of its six undefined formulas: **priority**, the **revive boost**,
and **resurface's hop/attenuation rule**. Doc 02 gestures at all three and defines none. §3 of this
document decides them, names every constant they introduce, states what each does at its extremes,
and gives the §13 calibration rows the implementing PRs will add. Where a number is chosen rather
than derived, the row says so — that distinction is load-bearing, because a design that presents a
judgement call as a derivation is harder to overturn later than one that flags it.

It does not restate requirements — that is `spec.md`, written in parallel. It does not edit
`docs/`; it *describes* the doc 02 amendments each PR will make.

> **Status: reconciled.** This document and `spec.md` were written **concurrently**, never saw each
> other, and disagreed on four substantive points. A fresh-context adversarial reviewer adjudicated
> all of them and the owner ruled on two; the outcome is `sdd/m2a-weight-focus/adjudication`
> (**#650**), which is binding. This revision applies all nine rulings.
>
> Five of them went this document's way (the multiplicative envelope, the single asymptotic boost,
> the negative-Δt clamp, `ResolveMargin`, and `focus_size`). **Four went against it**, and none of
> them is quietly rewritten: ruling 9 **rejects `NoveltyRamp` outright** and replaces it with the
> anti-starvation reading this document argued against at length (§3.1's `age`); ruling 2 **fixes
> an edge this document got wrong** (§3.2's `e ≥ WeightCeiling` branch wrote nothing at all, not
> even `last_touched_at`); ruling 4 **reverses D4's scope pull** of `weight_threshold`; and ruling 8
> notes that this document's rejected-alternatives table for `type` attacked a nine-constant
> strawman `spec.md` never proposed. Each of those carries an inline **Reconciled (ruling N)** note
> recording what this document used to say. §10 lists every ruling and where it landed.
>
> **One thing neither document could have seen**, because neither held both rulings: under ruling
> 1's multiplicative envelope, ruling 9's age term *multiplies* the intrinsic weight rather than
> adding to it, so a unit decayed toward zero cannot climb on age alone. §3.1's *Bounded
> anti-starvation* subsection works the boundary out with numbers and §8 R13 states the limitation
> for owner objection.
>
> **That analysis then produced a tenth ruling.** The owner read the numbers and moved
> `age_horizon_days` from 30 to 15 (**ruling 10**), because at 30 the break-even λ sat *below* doc
> 02 §13's base and the term produced no rise at all for an ordinary unit. `age_weight` stays 0.20.
> What ships now is a genuine but **transient** lift peaking at +3.3 % at two weeks — not the
> permanent floor the original framing promised, and both documents are written to keep it from
> being read as one.
>
> Where this document and `spec.md` state the same fact, they now state it in the same words and
> with the same identifiers. This document owns the signatures and the arguments; `spec.md` owns
> the testable assertions over them.

---

## 1. Ground truth this design was verified against

Every row was read at the named file and line in this session.

| Claim | How it was verified |
|---|---|
| `internal/core/weight` and `internal/core/focus` each contain exactly one file, a `doc.go` with a package comment and no declarations | `internal/core/weight/doc.go` (4 lines), `internal/core/focus/doc.go` (4 lines) |
| doc 02 §2's decay formula is `effective_weight = weight * exp(-decay_rate * Δt)`, Δt since `last_touched_at`, **in days** | `docs/02-cognitive-core.md:40` |
| doc 02 §2 says revive "writes a new boosted weight and resets `last_touched_at`" — and states no amount, no cap, no base | `docs/02-cognitive-core.md:47` |
| doc 02 §2 says resurface "propagates a boost along the graph edges, proportional to each relation's `strength`" — and states no hop limit, no attenuation, no cycle rule | `docs/02-cognitive-core.md:48-49` |
| doc 02 §3's priority is `f(effective_weight, temporal_urgency(due_at), type, age, relation_to_active_focus)` — five terms, no weighting, no shape for `temporal_urgency`, no type ordering | `docs/02-cognitive-core.md:76` |
| doc 02 §3 says the two focuses are "two queries with different criteria over `units`", and a third focus is "another query, not another schema" | `docs/02-cognitive-core.md:82-84` |
| doc 02 §3's hysteresis wording is "a challenger must beat the incumbent by more than `hysteresis_margin` (default 0.05)" — it does not say absolute or relative | `docs/02-cognitive-core.md:85-87` |
| §13 already carries a *ratio*-shaped margin and labels it as one in its own row | `docs/02-cognitive-core.md:597` — `correction_referent_margin (ratio of the top two fused scores)` |
| §13 has **no** row for the focus size N, for any urgency knob, for an age knob, for an adjacency knob, or for any boost or propagation knob | `docs/02-cognitive-core.md:575-597` — **23** rows, none of them. *(This row previously said "25 rows" at `:573-598`, which counted the table header and its separator. Corrected during reconciliation; the count is load-bearing for §3's final tally.)* |
| §13's thermal-zone vocabulary defines Hot and Warm *in terms of the focus* | `docs/02-cognitive-core.md:61-67` |
| `config.weight_threshold` is `REAL NOT NULL DEFAULT 0.5`; `config.hysteresis_margin` is `REAL NOT NULL DEFAULT 0.05` | `internal/store/sqlite/migrations/0002_learning_and_search.sql:63`, `:64` |
| The `config` singleton row is created by nothing — no migration contains `INSERT INTO config` | grep over `internal/store/sqlite/migrations/`; confirms proposal R1 |
| The fallback-to-Go-constants pattern is already shipped and already pinned to a column `DEFAULT` by an L2 test that reads the SQL off disk | `internal/core/relation/thresholds.go:14-38`, `test/conformance/relation_thresholds_ddl_test.go`, helper `migrationSQLText` at `test/conformance/i13_learning_signal_test.go:24` |
| The house precedent **refuses** to invent a per-type constant table where doc 02 names no per-type numbers | `internal/core/classify/prior.go:9-19` — "There are exactly two numbers here, not eighteen" |
| The house precedent for an unscaled multi-term score is one named constant per term, starting at a neutral value because no calibration data exists | `internal/core/recall/fuse.go:16-25` — `WeightVector = WeightLexical = 1.0` |
| The house precedent for a total order under `-shuffle=on` is an explicit three-level tie-break | `internal/core/recall/fuse.go:66-97` |
| I01's tree scan flags any Go line containing **both** the literal `"focus"` (double-quoted) and the substring `Status` | `test/conformance/i01_focus_never_persisted_test.go:93-95` |
| I01's vocabulary check reads `unit.AllStatuses()` and has passed vacuously since M0 | same file, `:54-69`; no focus has ever existed |
| `unit.Type` has exactly nine members, `mental_load` among them | `internal/core/unit/type.go:17-27` |
| `unit.Unit` carries `Weight`, `WeightDecayRate`, `LastTouchedAt`, `CreatedAt`, `DueAt *time.Time` | `internal/core/unit/unit.go:18-33` |
| `ports.UnitRepo` has seven methods, none of which writes `weight` or `last_touched_at` | `internal/ports/unitrepo.go:30-73` |
| `depguard`'s `core-purity` allows `internal/core/**` only `$gostd` and `internal/core` — so `math` is legal and `internal/ports` is not | `.golangci.yml:47-77` |
| `forbidigo` bans `time.Now` / `time.Since` / `time.Until` / `rand.*` / `uuid.*` / `os.Getenv` **by call pattern**, scoped to `internal/core/` — a `time.Time` value or field is legal | `.golangci.yml:96-110`, `:117-119` |
| `docs/06-harness.md` §4's invariant table has rows for I01, I05 and I19 and **no row for I24** | `docs/06-harness.md:173`, `:177`, `:191`; the table ends at I23 (`:195`) |
| The core coverage floor measures only test binaries under `internal/core/...`, and `make check` does not run it | recorded in `m1a-substrate/design.md` §1 from `scripts/core-coverage.sh:56` and `Makefile:36` vs `:39` |
| `scripts/pending-red.sh` is retired (commit `714934e`), so a conformance test naming an undefined symbol now breaks the **untagged** build | proposal §1 and §6; consequence derived in D11 below |

---

## 2. What `m2a` decides, in one paragraph

`core/weight` owns the Ebbinghaus curve, the thermal zones, and **one** boost mechanism used at two
strengths: a direct revive, and a resurface that is the same mechanism attenuated by graph distance.
`core/focus` owns priority, the two focuses, and hysteresis. Both are pure functions over data; the
current instant arrives as a named `now time.Time` parameter on every function that needs it and on
no function that does not. Neither package returns a value that a repository could persist as a
whole unit, which is how I05 becomes a property of the API rather than a rule somebody remembers.

---

## 3. The three formulas

Each subsection gives the formula, its constants with defaults, its behaviour at the boundaries, and
the §13 rows the implementing PR adds. Each also separates **what is derived** from **what is
chosen**. Nothing here is calibrated: there is no usage data, and pretending otherwise would be the
worse error.

### 3.1 Priority — F1

#### The shape

```
e = weight.Effective(c.Weight, c.DecayRate, c.LastTouchedAt, now)
u = UrgencyRamp(c.DueAt, now)          // [0,1], exactly 0 when DueAt is nil
g = AgeRamp(c.CreatedAt, now)          // [0,1], anti-starvation — rises with age
a = adjacency[c.ID]                    // [0,1], 0 when absent

priority = e
         × (1 + (UrgencyMax-1)·u)                // deadline: multiplicative
         × (1 + AgeWeight·g + AdjacencyWeight·a) // nudges: additive, bounded
```

> **Reconciled (ruling 1).** The shape stands, and `spec.md`'s competing linear weighted sum is
> withdrawn. The adjudication's numeric argument is worth keeping in view because it is the reason
> this document won the point: under the sum, an item at `effective_weight = 0` that is overdue,
> old and adjacent scored `0.30 + 0.10 + 0.10 + 0.15 = 0.65`, while a healthy item at
> `effective_weight = 1.0` with no context scored `0.35 × 0.5 = 0.175` — **the forgotten item beat
> the live one by roughly 4× on context alone.** The `n` term is renamed `g` and inverted per
> ruling 9; see *`age`* below.

**Why not a flat weighted sum of five normalized terms.** A sum makes the terms commensurable, and
they are not. `effective_weight` is the intrinsic quantity the whole of §2 exists to maintain; the
other terms are *contextual modulators of the moment* (§3's own words: "priority is contextual to
the moment"). In a sum, a unit whose effective weight has decayed to near zero can be lifted to the
top of the ranking by context alone — a deadline on something the brain has already forgotten
outranks the thing the user actually cares about. Multiplying by the intrinsic term makes context
*amplify* memory rather than *substitute* for it.

**Why urgency multiplies and the other two add.** A deadline is a hard external constraint that
should be allowed to dominate the ranking; an anti-starvation nudge and graph adjacency are nudges
that should never dominate. Multiplication gives urgency unbounded relative leverage over the other
terms (up to `UrgencyMax`); the additive block caps the nudges' *total* contribution at
`1 + AgeWeight + AdjacencyWeight`, no matter how many of them fire. This is a **choice**, and it is
the one structural decision in F1 that is worth an owner's attention: it says a due date is
categorically different from the other two modulators.

**Two properties this shape has by construction**, both testable and both worth stating because they
are what makes the ranking explicable in the glass box (§11):

- **`priority ≥ effective_weight`, always.** Every factor is ≥ 1. Context can promote a unit and can
  never demote one. Demotion is what decay is for; a modifier that could demote would make the
  ranking depend on the *absence* of a signal, which is the hardest kind of ordering to explain to a
  user asking "why is this not in my focus".
- **Monotone in `effective_weight` for fixed context.** Two units in identical context rank by
  weight. The formula never inverts §2.

A third property, added during reconciliation because ruling 1's envelope makes it exact and
because the anti-starvation analysis below needs it: **`priority` is homogeneous of degree 1 in
`e`.** Scaling every candidate's effective weight by the same positive factor leaves the ranking
unchanged. That is what makes the two focuses' scores comparable (D7) and it is the arithmetic
reason the hysteresis margin has to be *relative* (D8) — a scale-free score cannot be damped by an
absolute band.

**Maximum context amplification** is `UrgencyMax × (1 + AgeWeight + AdjacencyWeight)` = 3 × 1.45
= **4.35×**. That number is not a knob; it is the product of the knobs, and it is stated so the
dynamic range of the ranking is known rather than discovered. It is unchanged by ruling 9, since
`AgeWeight` inherits `NoveltyWeight`'s magnitude.

#### `temporal_urgency` — a linear ramp inside a lead window, clamped past the deadline

```
d = due_at.Sub(now).Hours() / 24            // days until due; negative when overdue
UrgencyRamp = clamp((UrgencyLeadDays - d) / UrgencyLeadDays, 0, 1)
```

| Situation | Ramp | Factor |
|---|---|---|
| `DueAt` is nil | **0, by definition** — not the `d → ∞` limit | 1.0 (exactly neutral) |
| Due in ≥ `UrgencyLeadDays` (7 d) | 0 | 1.0 |
| Due in 3.5 d | 0.5 | 2.0 |
| Due exactly now (`d = 0`) | 1 | `UrgencyMax` = 3.0 |
| **Overdue by any amount** | clamped at 1 | `UrgencyMax` = 3.0 — and no more |

The overdue clamp is the boundary that matters. Without it a task overdue by three years dominates
the focus permanently and the focus stops being a view of the present. With it, "overdue" is a
single state rather than a growing one, and what removes an overdue task from the focus is decay or
the user, not arithmetic.

`DueAt == nil` mapping to exactly 0 rather than to a limit is the second boundary: units with no
deadline are the majority, and the formula must reduce to `e × (1 + nudges)` for them with no
floating-point residue.

**Chosen, not derived:** linearity. An exponential or hyperbolic ramp would encode a curvature we
have no evidence for; a linear ramp has exactly two knobs (a horizon and a ceiling), both of which a
user could be shown and asked about. This follows `fuse.go`'s precedent of starting at the simplest
form when no calibration data exists.

**Chosen, not derived:** `UrgencyLeadDays = 7`. §13 already carries `Event lead time | 7 days`, and
this is deliberately the same number for the same human reason (a week is the horizon people plan
against) — but it is a **separate §13 row**, because one is prospection's notification horizon and
this is the ranking's, and collapsing two knobs into one because they happen to start equal is how a
calibration table becomes un-tunable.

**Chosen, not derived:** `UrgencyMax = 3.0`. The constraint that shaped it: a task due today must
outrank a non-urgent task of comparable weight, but must not outrank the entire pool. At 3.0, a
due-today unit outranks any non-urgent unit of less than 3× its effective weight. Since `archive`
holds the pool at `effective_weight ≥ weight_threshold` (0.5) as of the last consolidation, and
classify's base weight is 1.0, a due-today unit at the archive floor (0.5 × 3 = 1.5) still outranks
a healthy non-urgent unit at 1.0. That is the behaviour we want and it is *why* the value is 3 and
not 1.5 — but the derivation runs backwards from the desired behaviour, so it is a choice.

#### `age` — anti-starvation: older ranks higher

> **Reconciled (ruling 9 — OWNER, and this document was overruled).** This subsection previously
> specified a **`NoveltyRamp`** falling `1 → 0` over three days — *newer ⇒ a small, fast-fading
> bonus* — and its rejected-alternatives table dismissed the anti-starvation reading in one line:
> *"Rejected — this is what a deadline is for, and applied to everything it resurrects exactly the
> stale items decay is designed to sink."* `spec.md` R3.5 had independently specified the
> **opposite sign** for the same undefined doc 02 word: a term rising `0 → 1` over 30 days. Neither
> document knew the other existed. The owner ruled for **anti-starvation**. `NoveltyRamp`,
> `novelty_weight` and `novelty_window_days` are removed; `AgeRamp`, `age_weight` and
> `age_horizon_days` replace them.
>
> One line of the rejected argument survives as a **true and now-load-bearing observation**: an age
> term genuinely can resurrect what decay sank — *if it is additive*. Under ruling 1's
> multiplicative envelope it cannot, and *Bounded anti-starvation* below is the arithmetic of
> exactly how far it can reach. The old objection did not disappear; it was answered by the shape.
>
> One line of it was simply **wrong** and is retracted: "this is what a deadline is for". A deadline
> is what `UrgencyRamp` is for, and it fires only for units that *have* a `due_at`. Doc 02 §3 lists
> `temporal_urgency(due_at)` and `age` as **separate** terms, so reading `age` as a second deadline
> mechanism left the fourth term with no distinct job at all.

> **Reconciled (ruling 10 — OWNER, taken *because of* the boundary analysis below).**
> `AgeHorizonDays` was **30** when ruling 9 was taken; it is now **15**. `AgeWeight` stays 0.20 and
> nothing else about ruling 9 changes.
>
> The 30-day value was rejected on this document's own numbers. Ruling 9 was taken on the promise
> that "an old, ignored task does not stay buried forever", and *Bounded anti-starvation* below
> proved that false at `AgeHorizonDays = 30`: at doc 02 §13's base λ = 0.01/day the break-even sat
> at `0.20/30` = 0.00667/day, **below** the default, so an untouched unit's priority decreased
> across the entire horizon (`exp(-0.3) × 1.20 = 0.8890 < 1`). At 15 the break-even is
> `0.20/15` = **0.01333/day**, above the default, and the rise is real.
>
> **This is the first lever the analysis named, pulled.** It is also the reason the analysis was
> asked for in numbers rather than prose: the mis-framing was invisible until someone multiplied
> `exp(-λt)` by the ramp. The subsection below is rewritten to state what now ships — and, more
> importantly, what still does not.

`age` is the term doc 02 leaves most open, and reading it wrong duplicates decay under a second
name. Δt since `last_touched_at` already drives the whole of §2; if `age` also means "time since
something", the formula risks counting the same signal twice.

The disambiguation is that `last_touched_at` is *reset* by use and `created_at` never is, so their
difference is exactly "has this been revisited since capture". The term therefore reads
**`created_at`, never `last_touched_at`** — that is what keeps it from being decay under a second
name, and it is the requirement `spec.md` R3.4 states as a MUST.

```
ageDays = now.Sub(created_at).Hours() / 24
AgeRamp = clamp(ageDays / AgeHorizonDays, 0, 1)
```

| Situation | Ramp | Contribution |
|---|---|---|
| Captured this instant | 0 | +0.00 → factor 1.00 (exactly neutral) |
| Captured 7.5 days ago (half the horizon) | 0.5 | +0.10 |
| Captured ≥ 15 days ago (the horizon) | 1 | `+AgeWeight` = +0.20 → factor 1.20, and no more, ever |
| `created_at` **after** `now` (clock skew, backdated import) | clamped at **0** | +0.00 — a unit that does not yet exist has waited no time |

The negative clamp is the same rule as D1's negative-Δt clamp, applied a second time. `core`
receives instants it cannot vouch for, so **every elapsed-time computation in `m2a` saturates
rather than inverting.** One rule, two places, stated once here so it is not read as two
coincidences.

**Chosen, not derived: `AgeHorizonDays = 15`.** Owner ruling 10's number, replacing ruling 9's 30.
Not chosen for its own sake — chosen as the value that puts the break-even λ
(`AgeWeight / AgeHorizonDays`) above doc 02 §13's base 0.01/day, which is the property that makes
the term do anything at all for an ordinary unit. The next subsection derives that.

**Chosen, not derived:** `AgeWeight = 0.20`. It inherits `NoveltyWeight`'s magnitude, and it
inherits its argument unchanged, because the argument was about *leverage* and not about
direction: at most one fifth, so age **breaks close contests and never wins them**. The next
subsection makes "close" an exact number. **Ruling 10 deliberately left this constant alone**: it is
the one that sets P1's ceiling and P2's overturnable deficit, so moving it would have changed how
much power age has, and the owner wanted only to change *when* it arrives.

#### Bounded anti-starvation — the interaction rulings 1 and 9 create

> **Neither original artifact analysed this**, because neither held both rulings. The adjudication
> flags it as a required output of the reconciliation pass: *"state the resulting behaviour at the
> boundary, and surface it for owner objection rather than burying it."* This subsection is that.
> `spec.md` R3.5 turns each property below into an asserted test; §8 R13 states the limitation in
> plain language.

Ruling 9 gives the age term a rising sign. Ruling 1 makes it a **multiplier on `e`** rather than an
addend beside it. Those combine into a hard ceiling on how much starvation the term can undo, and
the ceiling is computable from the constants:

**P1 — the leverage is 20 %, for life.** With no deadline and no adjacency,
`priority ≤ e × (1 + AgeWeight)` = `e × 1.20`. The ramp saturates at `AgeHorizonDays` and the
factor saturates with it. There is no age at which the term is worth more than a fifth.

**P2 — the overturnable deficit is 16.7 %.** For two units in identical context differing only in
age, the older outranks the younger iff `e_old × (1 + AgeWeight·Δg) > e_new`, i.e. iff
`e_old / e_new > 1 / (1 + AgeWeight·Δg)`. At the extreme (`Δg = 1`) that is `1/1.20 = 0.833`, so
age rescues a unit carrying an effective-weight deficit of at most
`AgeWeight / (1 + AgeWeight)` = **16.7 %** — and not one point more.

**P3 — a unit at the archive floor cannot climb out.** The live pool's lowest effective weight is
`weight_threshold` = 0.5 as of the last consolidation. At full age and nothing else, such a unit
reaches priority `0.5 × 1.20 = 0.60`. A perfectly ordinary healthy unit at classify's base weight
of 1.0, brand new, with no deadline and no adjacency, scores **1.0** — and beats it by **1.67×**.
Anti-starvation re-ranks among units that still hold weight; it does not rescue units that have
lost it.

**P4 — there is a genuine rise, and at `AgeHorizonDays = 15` it reaches ordinary units.** For an
untouched unit, `priority(t) = w · exp(-λt) · (1 + AgeWeight · min(t/AgeHorizonDays, 1))`. Writing
`c = AgeWeight / AgeHorizonDays`, the derivative on `[0, AgeHorizonDays]` is
`exp(-λt)·[c − λ − λct]`, which gives **two** thresholds rather than the one the previous revision
of this subsection stated:

| Threshold | Closed form | At the defaults | Meaning |
|---|---|---|---|
| Rising at `t = 0` | `λ < AgeWeight / AgeHorizonDays` | **0.01333/day** | the term produces a rise at all |
| Peak lands **at** the horizon | `λ ≤ AgeWeight / (AgeHorizonDays · (1 + AgeWeight))` | **0.01111/day** | the peak is at `t = AgeHorizonDays`; between the two thresholds it peaks earlier and lower |

The interior stationary point is `t* = (c − λ)/(λc)`; at the base λ = 0.01 that is **25 days**,
which is past the horizon, so priority increases across all of `[0, 15]` and the maximum is at the
horizon itself.

**Doc 02 §13's base λ = 0.01/day satisfies both**, and this is not hypothetical:
`internal/core/classify/prior.go:25` sets `PriorDecayRate = 0.01` — pinned to migration 0001's
column `DEFAULT` by an existing L2 test — and encodes **no per-type table**, so 0.01 is the λ every
unit actually receives unless the model overrides it.

| λ | at day 15 | at day 30 | at day 60 | Shape |
|---|---|---|---|---|
| 0.001/day (doc 02 §2's "very low λ": `emotional`, a central goal) | **1.1821** | 1.1645 | 1.1301 | rises to the horizon, then decays very slowly |
| **0.01/day — §13's base, and `prior.go`'s actual value** | **1.0328** | 0.8890 | 0.6586 | **rises to a peak at the horizon, then declines** |
| 0.01111/day (peak-at-horizon threshold) | 1.0158 | 0.8598 | 0.6161 | peak exactly at the horizon, barely above 1 |
| 0.01333/day (rising-at-origin threshold) | 0.9825 | 0.8044 | 0.5392 | flat at the origin, declining thereafter |
| 0.02/day (doc 02 §2: `task` → *high* λ) | 0.8890 | 0.6586 | 0.3614 | **declining throughout — no rise at all** |

Read out: **at the default decay rate an untouched unit's priority genuinely rises, peaks at day 15
at 1.0328× its own day-0 value, and declines from there.** That is a real behaviour and it is what
ruling 10 bought.

**P5 — the rise is transient and small, and saying otherwise is the failure this ruling exists to
correct.** +3.3 %, for about two weeks. Past the horizon the ramp is saturated and priority is
`1.20 · w · exp(-λt)` — pure decay scaled by a constant, declining monotonically forever.

**P6 — the horizon bought a window, not a floor, and bought no power at all.** Three things ruling
10 did **not** change, each checkable from the closed forms:

- **P1 is untouched.** Leverage is still `1 + AgeWeight` = ×1.20.
- **P2 is untouched.** `AgeWeight / (1 + AgeWeight)` carries **no `AgeHorizonDays` term**, so the
  overturnable deficit is **16.7 % before and after**. This is exactly why the owner moved the
  horizon rather than raising `AgeWeight`, which would have moved P1 and P2 together.
- **Past day 30 the two horizons are numerically identical** — 0.8890 at day 30, 0.6586 at day 60 —
  because the bonus saturates at 1.20 either way. The 30-day horizon's failure was *when* it
  arrived, not *where* it ended, and shortening it fixed only the first.

**The levers that remain**, so a further objection can be made with numbers: shortening
`AgeHorizonDays` again raises the break-even λ proportionally and costs nothing structurally;
raising `AgeWeight` raises the break-even λ **and** P1 and P2 together, which is a different
decision; and an additive floor term outside the envelope would give age genuine rescuing power
while reintroducing precisely the defect ruling 1 removed — named, and not recommended.

**One calibration dependency to watch, stated because the margin is thin.** The break-even is
0.01333/day and the base is 0.01/day — **33 % of headroom**. Doc 02 §2 says type "orients the
direction" of λ with **`task` → high λ**, and `prior.go:9-19` deliberately declines to encode
per-type numbers because doc 02 names none and the model is meant to decide. Today that means every
unit gets 0.01 and the rise is universal. **The moment the model begins personalizing λ upward for
tasks — which doc 02 §2 directs it to — any task above 0.01333/day loses the rise entirely**, and
tasks are the population anti-starvation is nominally for (the 0.02 row above). Nothing in `m2a`
should change for this; it is a cross-milestone calibration dependency for M5's learning module to
watch, recorded in §8 R13 rather than acted on here.

The doc 02 §3 amendment states P1–P6's *meaning* in prose, not only the formula: **a two-week grace
window of a few percent, over a re-ranking among the units the brain still holds.** It does **not**
say "an old ignored task does not stay buried forever" — that is the framing ruling 10 exists to
correct, and doc 02 is the one place where writing it down would make it permanent.

#### `type` — it enters priority as the focus predicate, not as a number

> **Reconciled (ruling 8 — OWNER ruled for removal, with a correction on the table).** The outcome
> stands: `type` leaves the priority arithmetic and becomes a focus-membership predicate only, and
> doc 02 §3:76 goes from five terms to four. But the adjudication records a defect in the argument
> below that must not be quietly polished away: **this document's rejected-alternatives table
> attacked a nine-constant strawman.** `spec.md` actually proposed **three** per-type priors
> (`priority_type_prior_task` 1.0, `priority_type_prior_event` 0.85,
> `priority_type_prior_mental_load` 1.0), not nine — and within the task focus, `type` was **not**
> redundant with membership filtering, because filtering to `{task, event}` does not separate a
> `task` from an `event`. The double-counting argument below is therefore weaker than it reads: it
> is sound for `mental_load` (which membership already isolates) and unsound for the `task`/`event`
> pair. **The owner chose removal with that correction on the table**, on the remaining grounds:
> one mechanism in one place, and a type-independent `priority` whose scores are comparable across
> both focuses. The three-constant option is the one a reinstatement would revive, not the
> nine-constant one.

This is the term `m2a` proposes to **remove from the arithmetic**, and it needs the strongest
argument of the five because it changes the stated signature of `f`.

Doc 02 §3 already gives type a job two sentences below the formula: the two focuses are "two queries
with different criteria over `units`", and the criterion is type — the task focus takes actionable
types, the load focus takes `mental_load`. If type is *also* a numeric term inside `priority`, it is
counted twice: once to decide which contest a unit is in, and again to decide where it places in
that contest.

| Option | Verdict |
|---|---|
| A per-type numeric table (nine constants) | Rejected. It invents nine calibration numbers §13 never names, in the one place doc 02 says the *model* decides the value: §2 already says type "orients the direction" of the initial `weight` and λ assignment. `classify/prior.go:9-19` refused exactly this and its argument applies verbatim |
| One `TypeBonusStep` plus a ranked list of types | Rejected. Fewer constants, but the *ordering* is still nine invented facts wearing one constant's clothing |
| **Type is the focus-membership predicate and contributes nothing further to `priority`** — chosen | One mechanism, one place. `priority` becomes type-independent, which also means the two focuses' scores are directly comparable, which M4's "today" view will want |

Consequence to state plainly: **within the task focus, a `task` and an `event` of equal effective
weight, equal urgency, equal age and equal adjacency tie**, and the tie-break is `DueAt` then id
(D6) — which is the resolution ruling 8 names. If the owner wants an event to outrank a task at
equal weight, that is a numeric type term and it comes back — but it comes back as an explicit
calibration decision with its own §13 rows, not as an unexamined term in a formula. Reinstatement
is **additive**: a new term, three constants, its own review. Not a rewrite.

The doc 02 §3 amendment rewrites line 76 as:

```
priority = f(effective_weight, temporal_urgency(due_at), age, relation_to_active_focus)
           over the units the focus's type criterion already selected
```

**This is an owner-review item.** It is the only place in `m2a` where the design removes something
doc 02 states rather than filling in something doc 02 omits.

#### `relation_to_active_focus` — max over edges to the *previous* focus, and the circularity it resolves

Priority depends on the focus; the focus is computed from priority. Left alone that is a fixpoint,
and a fixpoint iteration inside a ranking is both expensive and unstable (a unit's priority would
depend on whether it is winning).

It is resolved by the same value hysteresis already needs: **adjacency is measured against the
previous focus, not the one being computed.** One parameter serves both mechanisms, one pass, no
iteration, and the two time-dependencies of the focus (its own history) are consolidated into one
input rather than two.

```
adjacency[v] = max over edges e joining v to any member of previous.Members of strength(e)
             = 0 when previous is empty, or v touches no member
```

**Max, not sum.** A sum lets a hub unit weakly connected to five focus members outrank a unit
strongly connected to one, which measures graph topology rather than relevance; it also needs
normalization (by what — the focus size?) to stay bounded. `max` is bounded in `[0,1]` by
construction, needs no normalization, and is stable under adding a redundant weak edge. The same
rule is used for multi-path aggregation in resurface (§3.3), so the codebase has **one** rule for
combining graph evidence.

Contribution is `AdjacencyWeight × adjacency`, **chosen** at 0.25 — slightly above `AgeWeight`,
because "this is about the thing you are already working on" is a stronger signal than "this has
been waiting", and well below the urgency ceiling, because it must never override a deadline. *(The
comparison previously read "a stronger signal than 'this is new'"; the ordering it justifies is
unchanged under ruling 9, only the thing it is being compared against.)*

Boundary: **on the first ranking after a process restart the previous focus is empty**, so adjacency
is 0 for every unit and the term vanishes entirely. This is the accepted cost of ruling round 2 #4
(previous focus lives in process), and it is now *two* effects rather than one — no hysteresis and
no adjacency on the first computation — which is worth recording because the proposal priced only
the first.

#### §13 rows F1 adds

| Knob | Default | Status |
|---|---|---|
| `focus_size` (top-N per focus) | 7 | new row, chosen (D7) |
| `urgency_lead_days` (priority) | 7 | new row, chosen |
| `urgency_max` (priority factor at or past `due_at`) | 3.0 | new row, chosen |
| `age_weight` (priority) | 0.20 | new row, chosen — **replaces `novelty_weight` per ruling 9**, same magnitude, opposite sign. Deliberately *not* moved by ruling 10 |
| `age_horizon_days` (priority) | **15** | new row, **owner ruling 10's number** — was 30 under ruling 9, which replaced `novelty_window_days` (3). 15 is the value that puts the break-even λ above §13's base 0.01/day |
| `focus_adjacency_weight` (priority) | 0.25 | new row, chosen |
| `hysteresis_margin` (focus) | 0.05 | **existing row, amended** — gains "(relative)" per D8 and ruling 6 |

> **Annotated, not rewritten (archive-time correction — see §10's C-c).** Task 3a.8
> (`openspec/changes/m2a-weight-focus/tasks.md`) found that this table — cited elsewhere in this
> chain's task list as "design §5.1" — had at one point carried a PR attribution for
> `focus_adjacency_weight` that pointed at PR 4, disagreeing with §5's package-layout tree and
> §8.1's PR split, both of which place `AdjacencyWeight` in PR 3 / 3a (`priority.go`, where
> `Priority` consumes it directly). The package-layout/§8.1 reading is what shipped. This document
> is historical record as of PR 3a's merge (`openspec/README.md`'s freeze rule), so the correction
> is recorded here rather than silently applied.

### 3.2 The revive boost — F2

#### The shape: asymptotic approach to a ceiling, from the *decayed* value

```
e  = Effective(w, λ, lastTouchedAt, now)
w' = e + ReviveGain × max(0, WeightCeiling - e)
lastTouchedAt' = now                             // unconditionally
```

> **Reconciled (ruling 2 — the mechanism stands, the edge was wrong).** The asymptotic form is
> adopted over `spec.md`'s `clamp(e + revive_boost_amount, floor, cap)`, on an argument `spec.md`
> could not answer: under an additive clamp, two distinct hot units at 1.5 and 1.8 both land on
> exactly 2.0 after a +0.5 boost, so **a revive destroys the ordering among the units the user
> touches most** — manufacturing the jitter hysteresis exists to suppress.
>
> But this document's `e ≥ WeightCeiling` branch wrote **"no write at all"**, not even
> `last_touched_at`, and the adjudication overturns it: *"a direct use must still move the clock."*
> The `max(0, …)` above is that fix, and it costs a **signature change** — D3's
> `Revive(c, now) (Boost, bool)` becomes `Revive(c, now) Boost`. See *At or above the ceiling*
> below for why the write is not the no-op §11 forbids.

Three decisions, and the first is the most consequential.

**The boost is applied to the *effective* weight at `now`, not to the persisted `weight`.** If it
were applied to the persisted value, reviving a unit that had decayed for ninety days would restore
its full undecayed weight plus a bonus: decay would be freely reversible, `weight` would become a
monotone ratchet, and §2's entire model would be decorative. Boosting from the decayed value means a
long-neglected thing comes back *partly*, and repeated use is what restores it — which is the
spacing effect the Ebbinghaus curve was borrowed from in the first place. This is the single most
important sentence in F2 and it goes into doc 02 §2 verbatim.

**The form is asymptotic, not additive-with-a-clamp and not multiplicative.**

| Form | Verdict |
|---|---|
| `min(e + B, Cap)` | Rejected. Bounded only by an external clamp, and the clamp is a discontinuity: every already-hot unit lands on exactly the same value, destroying the ordering among the things the user uses most |
| `min(e × M, Cap)` | Rejected. Same clamp, plus it cannot lift a unit whose effective weight has reached 0 — the multiplicative form has an absorbing state at exactly the value cold units approach |
| **`e + g·(Cap - e)`** — chosen | Bounded **by construction**, no clamp, no discontinuity. The increment shrinks as the unit gets hotter, which is the diminishing return reinforcement should have; and it is strictly increasing in `e`, so revive preserves the ordering it acts on |

**The third decision: a direct revive always writes.** See *At or above the ceiling* below.

Constants: `ReviveGain = 0.35`, `WeightCeiling = 2.0`.

#### Boundary behaviour

| Situation | Result |
|---|---|
| `e = 0` (fully decayed / cold) | `w' = 0.35 × 2.0 = 0.70` |
| `e = 1.0` (classify's base weight) | `w' = 1.0 + 0.35 × 1.0 = 1.35` |
| Repeated revives at the same instant | Strictly increasing, converging on 2.0, **never reaching and never exceeding it** |
| `e ≥ WeightCeiling` (classify assigned a weight above the ceiling) | **Writes `(e, now)`.** The `max(0, …)` term is zero, so the weight is not raised — a boost never lowers one either — and `last_touched_at` moves. See below |
| λ | **Unchanged.** Doc 02 §2 assigns λ at classify and personalizes it from the self-model; nothing says use makes a thing decay slower. Lowering λ on repeated use is a real idea (spaced repetition) and is recorded in §9 as deliberately not-now |

#### At or above the ceiling: why the write is not the no-op §11 forbids

This document's previous position was that writing `(e, now)` at `e ≥ WeightCeiling` "would change
no effective weight at any future instant — it would only materialize decay", and would therefore
owe a `decision_log` row for a decision that decided nothing. **The arithmetic half of that is
exactly right and is worth keeping**, because it is what makes the corrected write safe:

> Since `e = w · exp(-λ·(now − lt))`, the pairs `(w, lt)` and `(e, now)` denote the **same curve**.
> `Effective` returns an identical value at every future instant either way. The write is
> **effective-weight-neutral by construction**, and knowingly so.

The conclusion drawn from it was wrong, on two counts.

**First, `last_touched_at` is not only a decay anchor — it is the vault's record of direct use, and
it is read as one.** Doc 02 §2 distinguishes "direct use" from "related signal" by which of the two
mechanisms fires. §3.1's `age` term exists *because* `last_touched_at` is reset by use while
`created_at` never is; that disambiguation is the whole reason the fourth term is not a second
decay. A revive that declines to move the clock would break that meaning for **exactly the hottest
units in the vault** — the ones above the ceiling, i.e. the ones classify or the self-model marked
as mattering most. Every future "not touched in N days" question would misread them.

**Second, §11's rule is "a decision with no material effect writes nothing", and the decision here
has an effect.** The decision being recorded is *this was directly used*, and recording it **is**
the effect. §11 exists to stop the log filling with non-events; a direct use is an event. The
resurface branch (§3.3) is where §11 genuinely bites, and the two are now deliberately asymmetric:

> **A direct use always records itself. An indirect propagation records itself only when it
> genuinely lifts something.**

That asymmetry is not a wart — it is the sharpest statement of doc 02 §2's own "direct use" vs
"related signal" distinction that the code can make, and it strengthens §3.3's answer to the "does
resurface make a unit look directly used" worry rather than weakening it. Both sentences go into
the doc 02 §2 amendment.

An L1 test makes the neutrality claim testable rather than assertable: `Effective` over the
returned pair equals `Effective` over the input pair at an arbitrary later instant.

#### The constant relation that stops the numbers from being arbitrary

```
ReviveGain × WeightCeiling  >  weight_threshold's DDL default        0.35 × 2.0 = 0.70  >  0.5
```

Read out: **one direct revive is always enough to lift a fully-decayed unit back above the archive
threshold.** That is doc 02 §2's own "cold→warm/hot by a strong resurface" made arithmetic. Two
otherwise-free constants are pinned to a third that already exists in the schema, and a test asserts
the inequality — so a later recalibration of either cannot silently break the promise that a cold
thing can be brought back by using it.

The inequality is asserted as an inequality, not an equality: `weight_threshold` is marked ⚙
recalibratable per user in §13, so a user who raises it to 0.8 breaks the *relation* without
breaking the *code*. The test therefore asserts it over the **defaults** and says so in its own doc
comment.

> **Reconciled (ruling 4).** The relation survives; **what it is asserted against changes.** This
> document previously asserted it at **L1** against a Go constant `weight.DefaultWeightThreshold`
> that D4 declared in `m2a`. Ruling 4 sends `weight_threshold`'s Go home back to `m2b`'s
> `feat/core-consolidation-expire-archive`, where proposal `§5.1:348` assigns it, and notes that
> D4's pull was made unilaterally, called "deliberate", and did not flag that it was overriding an
> owner-reviewed chained-PR assignment. So the assertion moves to **L2**, against the `DEFAULT`
> literal parsed out of `0002_learning_and_search.sql:63` via the existing `migrationSQLText`
> helper (`test/conformance/i13_learning_signal_test.go:24`). `m2a` keeps the promise and declares
> no constant for it. §3.3's two-hop relation moves the same way, for the same reason.

#### §13 rows F2 adds

| Knob | Default | Status |
|---|---|---|
| `revive_gain` | 0.35 | new row, chosen |
| `weight_ceiling` | 2.0 | new row, chosen (two doublings of headroom above §13's base weight of 1.0) |

`weight_threshold` is **not** an `m2a` row and gets no Go home here (ruling 4). It is `m2b`'s.

### 3.3 Resurface — F3

#### The shape: the same boost, at an attenuated target

Resurface is **not a second formula.** It is F2 with the ceiling scaled down by how far the
activation travelled:

```
gain(v)   = max over paths p from origin to v, |p| ≤ ResurfaceMaxHops, of
              ( Π strength(e) for e in p ) × ResurfaceAttenuation^|p|

target(v) = gain(v) × WeightCeiling
e_v       = Effective(v.Weight, v.DecayRate, v.LastTouchedAt, now)

w'_v = e_v + ReviveGain × (target(v) - e_v)   when e_v < target(v)   → write (w'_v, now)
     = (no write)                             when e_v ≥ target(v)
```

**Why the gain scales the *target* and not the *step*.** This was the design's hardest choice and
the alternative is a real bug. If the gain scaled the step — `e + ReviveGain·gain·(Ceiling - e)` —
then a unit that is merely *adjacent* to something used daily converges on the full ceiling: each
day it takes a fraction of the remaining gap, and one day of decay at λ = 0.01 removes about 1% of
it. The neighbourhood of anything hot becomes permanently hot, decay never bites, and §2's model is
defeated by the graph. Scaling the target instead caps *where propagation can hold a unit*, which is
the property that makes spreading activation safe.

**The same `max`-over-paths rule as adjacency (§3.1).** A sum over paths makes a unit's boost depend
on how many redundant edges the judge happened to create — topology noise, and unbounded. `max` is
bounded by `[0,1]` and stable under adding a weak redundant path. One rule, used twice.

**Traversal is undirected.** Doc 02 §4 states that a relation's direction is "what the judge said,
not a canonical form", and that two units related in both directions hold two rows. So an edge must
conduct activation regardless of which way it was stored — otherwise propagation would depend on
which of two units happened to be captured second, which is not a property of the user's memory.
Where two units are joined by several edges (different relation types), the **strongest** is used,
by the same `max` rule.

#### Boundary behaviour

| Situation | Result |
|---|---|
| 1 hop, `strength = 1.0` | `gain = 0.5`, `target = 1.0` — propagation can hold a direct neighbour at classify's base weight and no higher |
| 2 hops, `strength = 1.0` each | `gain = 0.25`, `target = 0.5` — exactly the archive threshold (asserted as `≤` against the DDL default, per ruling 4) |
| 1 hop, `strength = 0.1` (§4's "a passing mention") | `gain = 0.05`, `target = 0.10` — below the archive threshold; a passing mention cannot keep anything alive |
| 3+ hops | Unreachable. `ResurfaceMaxHops = 2` |
| The origin itself | **Never a recipient.** It already received its direct revive; a 2-cycle back to it would double-count |
| A cycle in the graph | Terminates by the hop bound alone. Gain is strictly decreasing along a path (attenuation < 1, strength ≤ 1) and depth is capped, so a cycle can only produce a strictly worse path that `max` discards |
| `e_v ≥ target(v)` | **No write.** No weight write, no `last_touched_at` reset, no `decision_log` row — a decision with no effect writes nothing (§11) |

The second row is the behavioural headline and it deserves its own sentence in doc 02 §2:
**spreading activation alone can never lift a unit above the archive threshold at the maximum hop
distance.** Only direct use, or a strong immediate neighbourhood, keeps something out of the cold.
That is the guarantee that makes it safe to run resurface on every capture.

#### Does the propagated boost reset `last_touched_at`?

The brief's expectation is that it must not. **It must**, and the reason is mechanical rather than a
matter of taste — but the intuition behind the expectation is right and is satisfied a different
way.

`weight` is *defined* as "the value at `last_touched_at`" (§2: "Persisted: `weight` (value at the
last event)"). The on-read formula `weight × exp(-λ·Δt)` reads them as a pair. Write a boosted
`weight` while leaving `last_touched_at` alone and the very next read re-applies the entire old Δt
to the new value: the boost is eaten instantly, and worse, the pair now encodes a fiction — a value
that was never true at the timestamp it is stamped with. **This is the mechanical reason invariant
I24 exists**, and `m2a` is the change that discovers it, so `m2a` is the change that adds I24's row
to `docs/06-harness.md` §4 (D10).

The legitimate worry underneath the expectation is different: *resurface must not make a unit look
directly used.* Two mechanisms answer it, and neither is the timestamp:

1. **The target cap.** A resurfaced unit converges on `gain × WeightCeiling`, not on
   `WeightCeiling`. Its decay clock restarting is harmless because the level it restarts from is
   bounded by distance.
2. **The no-op branch.** A unit already warmer than what propagation could give it is **not written
   at all**, so the majority of neighbours of a hot unit — the ones that are already warm — never
   have their clock touched. Propagation writes only where it genuinely lifts something.

So: **a propagated boost resets `last_touched_at`, and it only ever writes when it actually raises
the weight.** Both halves go into the doc 02 §2 amendment, because the first half alone reads as the
bug the brief was worried about.

Ruling 2's correction to §3.2 sharpens this rather than muddying it. Revive now always writes;
resurface still writes only when it lifts. The asymmetry is the point:

> **A direct use always records itself. An indirect propagation records itself only when it
> genuinely lifts something.**

That is doc 02 §2's own "direct use" vs "related signal" distinction, expressed as a difference in
write behaviour rather than only as a difference in magnitude — and it is a *second* mechanism, on
top of the target cap, keeping a resurfaced unit from looking directly used.

#### §13 rows F3 adds

| Knob | Default | Status |
|---|---|---|
| `resurface_max_hops` | 2 | new row, chosen |
| `resurface_attenuation` (per hop) | 0.5 | new row, chosen |

**Chosen, not derived: `ResurfaceMaxHops = 2`.** One hop is "neighbours" and does not earn the name
spreading activation; three hops in a personal graph reaches most of the vault (a hub at strength
0.5 over three hops touches nearly everything), which makes the boost meaningless and makes the pass
cost grow as the cube of the branching factor. Two is the smallest number that is genuinely
transitive, and it bounds the work at O(branching²).

**Chosen, not derived: `ResurfaceAttenuation = 0.5`.** Edge strength already attenuates, but only by
*confidence*; without a separate per-hop factor a chain of strength-1.0 edges would propagate the
full boost to the hop limit. 0.5 makes distance cost something independent of how sure the judge
was, and at the default `WeightCeiling` it lands the two-hop target exactly on the archive threshold
(0.25 × 2.0 = 0.5) — a coincidence at the defaults, stated as such, and asserted as an **inequality**
(`Attenuation^MaxHops × Ceiling ≤ weight_threshold`'s DDL default) rather than an equality, because
`weight_threshold` is ⚙ per-user. Per ruling 4 the assertion reads the migration text, not a Go
constant `m2a` declares.

---

## 4. Decision record

### D1 — `Effective` takes the four scalars of doc 02 §2, returns a `float64`, and clamps negative Δt

```go
func Effective(weight, decayRate float64, lastTouchedAt, now time.Time) float64
```

**Scalars, not a `unit.Unit`.** The signature is doc 02 §2's formula term for term, so the code and
the governing document line up symbol by symbol; and it is callable for a *hypothetical* weight,
which `Revive` needs internally and which a "what would this be worth on Friday" test needs. One
spelling only — no `EffectiveOf(u unit.Unit, now)` beside it, because two exported spellings of one
fact are two things that can drift (`m1a` D3's own argument against a `CanTransitionTo` twin).

**Returns a `float64` and nothing else.** This is the load-bearing half of I05: there is no function
anywhere in `core/weight` that returns a `unit.Unit`, so a read path has no value it *could* hand to
a repository. See D9.

**Δt is clamped at 0.** `now` before `lastTouchedAt` happens — clock skew across a restart, a
backdated import, a test with a fake clock wound backwards. Unclamped, `exp(-λ·negative)` is
*greater than 1* and the effective weight exceeds the stored weight, which would let a read produce
a value the schema never contained. Clamped, the worst a bad timestamp does is present the stored
weight undecayed. `Effective` is therefore ≤ `weight` for all inputs, which is a testable
postcondition rather than a comment.

> **Reconciled (ruling 3).** Adopted as written. `spec.md` never constrained `now < lastTouchedAt`
> at all, and the adjudication calls the gap "a real correctness gap"; `spec.md` R1.2 is now a
> requirement of its own with the postcondition attached. The same saturate-rather-than-invert rule
> is applied a second time by §3.1's `AgeRamp`, and the two are stated as one rule in two places
> rather than as two coincidences.

Δt is in **days**, per §2's own annotation, computed as `now.Sub(lastTouchedAt).Hours() / 24` — not
`.Days()`, which does not exist on `time.Duration`, and not a day-boundary count, which would make
the curve a step function and make the result depend on a timezone the core is forbidden to know.

`math.Exp` is standard library and therefore inside `depguard`'s `core-purity` allow-list
(`.golangci.yml:55-57`).

### D2 — Thermal zones take no clock, and take focus membership as a `bool`

```go
type Zone int
const (ZoneCold Zone = iota; ZoneWarm; ZoneHot)
func AllZones() []Zone
func (z Zone) String() string
func ZoneOf(status unit.Status, inFocus bool) Zone
```

Doc 02 §2's table determines the zone from `status` and focus membership only. The parenthetical on
Cold — "its effective weight crossed the threshold during a consolidation" — is *causal history*,
not a determination: by the time a unit is `archived` the crossing already happened, and re-deriving
it on read would give two answers to one question. **`ZoneOf` therefore takes no `now`**, which is
the tidiest possible statement of a fact worth stating: temperature is not a function of time, it is
a function of two decisions already made.

`inFocus bool`, not a `focus.Selection`: `core/weight` must not import `core/focus`, or the
dependency arrow reverses (`core/focus` imports `core/weight` for `Effective`, D5) and the packages
cycle. A `bool` is also the honest signature — `ZoneOf` needs one bit, and taking a whole selection
to extract one bit invites the caller to think the function knows something about focus semantics.

Totality: `archived` maps to `ZoneCold` regardless of `inFocus`, because a focus is computed over the
pool and `archived + inFocus` is unreachable — but the function is total anyway rather than
panicking, since an unreachable arm is an uncovered statement by construction and the ≥90% floor
notices (`m1a` D9's rule 2).

`superseded` and `incomplete` also map to `ZoneCold`. Doc 02's table names only three statuses;
mapping the other two to Cold rather than adding a fourth zone is a **choice**, argued as: the zone
vocabulary is about attention, and neither status is a candidate for attention. It is recorded in
the §2 amendment.

### D3 — One boost mechanism, two entry points, and a plan the caller persists

```go
type Current struct {
    UnitID        string
    Weight        float64
    DecayRate     float64
    LastTouchedAt time.Time
}

type Boost struct {
    UnitID        string
    Weight        float64
    LastTouchedAt time.Time
}

type Edge struct {
    From, To string
    Strength float64
}

type Neighbourhood struct {
    Origin string
    States []Current  // every unit within ResurfaceMaxHops, origin included
    Edges  []Edge     // every edge among them, as stored (direction ignored — §3.3)
}

func Revive(c Current, now time.Time) Boost
func Resurface(n Neighbourhood, now time.Time) []Boost
```

**Both return a plan, never a mutation.** `core/weight` decides what the new pair should be; `brain`
performs the write (the `nooma-core` decision-gate table's first two rows). Nothing in `core/weight`
takes a repository, a context, or a clock.

**`Boost` carries both fields and there is no way to build one with only a weight.** That is I24
made a type rather than a rule: `m2c`'s new `UnitRepo` method takes a `weight.Boost` (or its three
fields), so a `SetWeight` that leaves `last_touched_at` alone is not expressible at the port. `m2a`
does not add the port — it fixes the shape the port must have, and D10 lands I24's harness row so
`m2c`'s test has something to name.

**`Revive` returns a bare `Boost`.** A direct use is always an event to record, so there is no
"maybe" to express.

> **Reconciled (ruling 2).** This paragraph previously read: *"`Revive` returns `(Boost, bool)`, the
> bool meaning 'there is a write to do'. False when `e ≥ WeightCeiling`."* The ruling requires that
> a direct use move `last_touched_at` even at or above the ceiling, so the bool has no false case
> left and carrying it would be a lie in the type. `Resurface` keeps its conditional behaviour, but
> expresses it as a **shorter slice** rather than a flag — a unit it does not lift simply is not in
> the output (§3.3). The two mechanisms now differ in their return shapes exactly as much as they
> differ in their write semantics, which is the right amount.

**`Resurface` returns a slice sorted by `UnitID`**, containing only the units it actually raises. The
sort is not cosmetic: the traversal is over slices, but any implementation will use a visited map,
and the suite runs `-shuffle=on` with `-race` (`Makefile:48`). A deterministic output order is also
what lets `m2c` write `decision_log` rows in a reproducible order for the demo (proposal §2's exit
criterion).

**`Neighbourhood` is loaded by `brain`, not walked by `core`.** `core/weight` cannot reach a
repository, so the caller loads a bounded neighbourhood — everything within `ResurfaceMaxHops` — and
hands it in. That makes the hop limit do double duty: it bounds the propagation *and* it bounds the
query. `m2c` inherits the obligation to load exactly that closure; the design states it here so it is
not discovered as a missing repository method during apply.

### D4 — `hysteresis_margin` gets its Go home in `m2a`; `weight_threshold` does **not**

> **Reconciled (ruling 4 — this decision is reversed in half).** D4 previously gave **both**
> `config` columns their Go home in `m2a`, declaring `weight.DefaultWeightThreshold` and
> `weight.ResolveThreshold` alongside the focus pair, and argued the pull was "deliberate". The
> adjudication reverses that half on a fact D4 did not engage with: `openspec/changes/m2-sleep-weight/proposal.md:348`
> assigns `weight_threshold` to `m2b`'s `feat/core-consolidation-expire-archive`, in an
> owner-reviewed chained-PR table, and **D4 overrode it without flagging that it was overriding
> anything** — including two PR line budgets neither estimate redid (§8 R10 now does).
>
> The `hysteresis_margin` half stands and is *strengthened*: ruling 5 makes it the pattern
> `spec.md` must adopt, since `spec.md` R4.3 had specified only "a single named constant with
> default 0.05" and no resolution path at all.

```go
// internal/core/focus
const DefaultHysteresisMargin = 0.05
func ResolveMargin(configured *float64) float64
```

`hysteresis_margin` is a `config` column (`0002:64`) whose singleton row **has never existed in any
vault** (proposal R1). Owner ruling round 2 #2 already chose the answer for the milestone:
`ConfigRepo` returns named Go constants when the row is absent, pinned to the SQL `DEFAULT`s by an
L2 test. That is `relation.Resolve`'s shape verbatim (`internal/core/relation/thresholds.go:26-38`),
including the `nil` sentinel meaning "no row". `m2c` supplies the `*float64`; `m2a` supplies the
meaning of `nil`.

The L2 test pinning `DefaultHysteresisMargin` to `0002:64` lands in `m2a`'s **PR 4**, beside
`hysteresis.go`, using the existing `migrationSQLText` helper
(`test/conformance/i13_learning_signal_test.go:24`) — the same mechanism
`relation_thresholds_ddl_test.go` already uses. *(It previously lived in PR 1 because it was
paired with the weight half; with that half gone it belongs where its constant is declared.)*

**`weight_threshold` is `m2b`'s.** `weight.DefaultWeightThreshold` and `weight.ResolveThreshold`
are **not** declared by this change. What `m2a` keeps is the two *arithmetic promises* that
reference it — §3.2's `ReviveGain × WeightCeiling > 0.5` and §3.3's
`Attenuation^MaxHops × Ceiling ≤ 0.5` — and it keeps them by asserting against the **migration DDL
text** rather than against a Go constant:

```
test/conformance/weight_constant_relations_ddl_test.go   (L2, PR 2)
  parse DEFAULT from 0002_learning_and_search.sql:63 via migrationSQLText
  assert weight.ReviveGain * weight.WeightCeiling                              > that default
  assert weight.ResurfaceAttenuation^weight.ResurfaceMaxHops * WeightCeiling  <= that default
```

This is arguably a *better* pin than the original: it ties `m2a`'s constants to the schema itself
rather than to another Go constant that would in turn need pinning to the schema. The cost is that
the relation is proven at L2 rather than L1 — justified, since the test's whole content is a
statement about a SQL file (`nooma-testing`'s decision gate: it names no invariant, but it pins a
value to schema text, which is the same reason `relation_thresholds_ddl_test.go` is L2).

### D5 — `core/focus` imports `core/weight`; the arrow points one way and the curve has one home

`Priority` needs `effective_weight`, and there must be exactly one implementation of doc 02 §2's
curve. So `core/focus` imports `core/weight`, `core/weight` imports `core/unit` (for `unit.Status` in
`ZoneOf`), and `core/focus` imports `core/unit` (for `unit.Type` in the focus criterion). No cycle.
`core → core` is inside `depguard`'s allow-list (`.golangci.yml:57`).

`focus` also reuses `weight.Edge` for adjacency rather than declaring a second edge type. One shape
for a graph edge across the core, because two would drift the moment `strength`'s meaning is
refined.

### D6 — `Rank` produces a total order with an explicit three-level tie-break

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

func UrgencyRamp(dueAt *time.Time, now time.Time) float64
func AgeRamp(createdAt, now time.Time) float64
func Priority(c Candidate, adjacency float64, now time.Time) float64
func Rank(cs []Candidate, adjacency map[string]float64, now time.Time) []Ranked
```

*(`NoveltyRamp` was this signature's third ramp until ruling 9 replaced it with `AgeRamp` — same
shape and same return contract, opposite sign and a 30-day rather than a 3-day scale.)*

**The ramps return raw values in `[0,1]` and `Priority` applies every weight.** So `UrgencyMax`,
`AgeWeight` and `AdjacencyWeight` appear in exactly one function, which is harness §7's "one place"
rule applied to a multi-term formula. It also makes the boundary tests trivial: a ramp's table is
over `[0,1]` and needs no arithmetic to read.

**Tie-break, in order** (mirroring `recall.FuseScored`'s three levels, `fuse.go:66-97`, and for the
same stated reason — `-shuffle=on` and exact float ties in symmetric cases):

1. higher `Score` first;
2. earlier `DueAt` first, with a non-nil `DueAt` always before a nil one;
3. lexicographic by `ID`.

Level 2 is where the type term went (§3.1): with `priority` type-independent, a `task` and an
`event` at equal score are separated by which is due sooner, which is the answer a user would give.

**`adjacency` is a `map[string]float64` that may be `nil`.** A nil or absent entry is 0, so the term
vanishes. This is what lets `m2a` ship a formula whose fifth term has no producer in M2 (proposal
§4.3): the caller that has no relation graph loaded passes `nil` and gets a well-defined ranking, and
M4's today view fills it in without a signature change.

### D7 — Two focuses, one ranking, one size; `Kind` is the type criterion

```go
type Kind string
const (
    KindTask Kind = "task"
    KindLoad Kind = "load"
)
func AllKinds() []Kind
func Types(k Kind) []unit.Type      // fresh slice, never an exported var
const DefaultSize = 7

type Selection struct {
    Kind    Kind
    Members []string   // unit ids, in rank order
}

func Select(k Kind, ranked []Ranked, previous Selection, margin float64, size int) Selection
```

`Types(KindLoad)` is `{mental_load}` — doc 02 §3 says so outright. `Types(KindTask)` is
**`{task, event}`**, and that is a **choice**:

| Option | Verdict |
|---|---|
| `{task}` only | The narrowest reading of "actionable". Rejected: the task focus answers "what should I be doing", and a meeting in two hours is the strongest possible answer to that question. Excluding events forces a third focus for them on day one |
| **`{task, event}`** — chosen | Both are things the day is organized around, and both carry a `due_at`/`event_at` the urgency ramp can read |
| `{task, event, list}` | Rejected: a `list` is a container, and putting a container in a focus of atoms means the focus sometimes holds a thing you cannot do |

`knowledge`, `procedural`, `emotional`, `structured_ref` and `insight` are in neither focus. Doc 02
§3's "a third focus = another query, not another schema" is the escape hatch if that turns out
wrong, and it costs a `Kind` constant and a `Types` arm.

**`DefaultSize = 7`, one constant for both focuses.** Chosen, not derived: a focus is a human
attention bound and 7±2 is the least-invented number available for one. One constant rather than two
follows `fuse.go`'s `WeightVector = WeightLexical = 1.0` precedent — split it when data says the two
focuses want different sizes, not before. It happens to equal §13's `mental_load_threshold`, and
that is a **coincidence, not a relation**: that knob counts live `mental_load` units, not focus
members, and no test ties them.

**`Members` is `[]string` — ids, not units.** A `[]unit.Unit` would be a persistable shape and would
put I01 one careless repository call away; a list of ids is not a thing anything would try to store
as a status. See D9.

`Kind`'s values are `"task"` and `"load"`. **They are deliberately not `"focus"`**, and more
generally: no file under `internal/core/focus` contains the double-quoted literal `"focus"`
anywhere. I01's tree scan flags any Go line carrying both `"focus"` and `Status`
(`i01_focus_never_persisted_test.go:93-95`), and this package is the one place in the tree where
both are natural. The rule is absolute rather than conditional because a conditional rule ("only
when `Status` is on the same line") is one refactor away from tripping.

### D8 — Hysteresis is relative, time-independent, and implemented as one adjusted sort

```go
func Displaces(challenger, incumbent, margin float64) bool   // challenger > incumbent*(1+margin)
```

**Relative, not absolute.** Doc 02 §3's wording is ambiguous and §13's default is 0.05, which reads
absolute — but `priority` has no fixed scale. It is `effective_weight` (whatever classify assigned,
personalized by the self-model) times up to 4.35. An absolute 0.05 is a 5% band at priority 1.0 and
a 1.25% band at priority 4.0, so the damping would be weakest exactly where the contested values are
largest. A ratio is scale-free, which is the only way a single global default can mean the same
thing for every user and every unit. §13 already carries a ratio-shaped margin and labels it in its
own row (`correction_referent_margin (ratio of the top two fused scores)`, `02:597`), so this is a
pattern rather than an invention — and the §13 row is amended to `hysteresis_margin (focus,
relative)` so the reading is not left to be inferred.

> **Reconciled (ruling 6).** Adopted, and the adjudication upgrades the argument: relative
> hysteresis is **not an independent preference, it is entailed by ruling 1.** Once `priority` is
> the unbounded multiplicative envelope of §3.1, an absolute 0.05 band *means a different thing* at
> priority 1.0 than at 4.0, so the relative form is the only one a single global default can carry.
> `spec.md` R4.3 specified the absolute form (`challenger > incumbent + margin`) and is corrected.
> **The argument order is this document's** — `Displaces(challenger, incumbent, margin)`, since
> `spec.md` had the two operands the other way round and one spelling has to win.

**This is an owner-review item**: it changes how doc 02 §3 line 85 is read, though not its number.

**Strictly greater: equality does not displace.** The incumbent wins ties, which is the entire point
of hysteresis. Boundary table: `challenger == incumbent` → no; `challenger == incumbent*(1+margin)`
→ no; `challenger == incumbent*(1+margin) + ε` → yes.

**No clock.** `Displaces` compares two scores; `Select` compares scores that `Rank` already produced
with `now`. So the brief's "pure function of (candidates, previous, margin, now)" is satisfied with
`now` consumed exactly one layer up — and the split is worth naming: **hysteresis is
time-independent**, which is why it can be tested without a fake clock at all.

**Implementation: one adjusted sort, proven equivalent to the predicate.** Instead of a swap loop,
`Select` sorts by `Score × (1 + margin)` for incumbents and `Score` for everyone else, then takes
the top `size`. The equivalence is exact:

- an incumbent `i` holds its slot against a non-incumbent `c` iff `c.Score ≤ i.Score×(1+margin)`,
  which is `!Displaces(c, i, margin)`;
- incumbent-vs-incumbent comparisons scale both sides by the same factor, so their relative order is
  untouched;
- non-incumbent-vs-non-incumbent is untouched.

Two spellings of one rule is exactly the drift `m1a` D3 warns about, so the design accepts it only
with a mechanism: `Displaces` stays the **definition** (it is what I19's conformance test names), and
an L1 test asserts the two agree over a table of incumbent/challenger pairs including all three
boundary cases. Recorded as a risk (§8 R4) rather than pretended away.

**An incumbent that is no longer in `ranked`** — archived since, or now the wrong type for this
`Kind` — is simply absent and is dropped with no contest. Stated because "the incumbent always
wins ties" invites the opposite reading.

**Empty `previous`** (first computation after a restart) reduces `Select` to a plain top-N. Together
with §3.1's adjacency term also vanishing, the first ranking after a restart differs from the second
in *two* ways, not one. That is ruling round 2 #4's accepted cost, priced correctly here.

### D9 — I05 and I01 are made properties of the API, because neither package persists anything

Two invariants have to be proven about packages that write nothing. Asserting "it does not persist"
about code with no repository is vacuous. The design instead makes the persistence **inexpressible**,
and the conformance tests assert the inexpressibility:

**I05 — decay is computed on read, never written per read.**

- No exported function in `core/weight` returns a `unit.Unit`. `Effective` returns a `float64`. A
  read path therefore has no unit-shaped value it could hand to `UnitRepo`, and "accidentally
  persist a decayed weight" has no syntax.
- The only persistable value the package produces is a `Boost`, and `Boost` values come from exactly
  two constructors — `Revive` and `Resurface` — which are the two discrete events doc 02 §2 names.
  There is no third producer.
- `m2a`'s L2 test is therefore **structural**: reflect over `core/weight`'s exported surface and fail
  if any function's result type is `unit.Unit`, `*unit.Unit`, `[]unit.Unit`, or if any producer of
  `Boost` exists beyond the two named. Its doc comment states honestly that this is I05's *pure*
  half: the structural half ("no read path in the store writes `weight`") needs the store layer and
  is `m2c`'s, scoped to read paths so it cannot forbid `reweight`'s bulk materialization, which doc
  02 §2 and §6.6 explicitly permit (proposal R13).

**I01 — `status='focus'` is never persisted.** The existing test
(`i01_focus_never_persisted_test.go`) has passed vacuously since M0 because no focus existed. `m2a`
is the first change that gives it a real corpus, and it does **not** rewrite it — it puts it under
load and adds a third check beside it:

1. *Unchanged, still passing*: `focus` is not a member of `unit.AllStatuses()`.
2. *Unchanged, now non-trivial*: the tree scan over `internal/` and `cmd/` now scans a package
   literally named `focus`. D7's absolute rule (no `"focus"` literal in the package) keeps it green
   **for the right reason** rather than by luck.
3. *New in `m2a`*: a structural check that `core/focus` **cannot express a persisted focus** — no
   exported function returns or embeds a `unit.Status`; `Selection.Members` is `[]string`; and the
   package declares **no package-level `var`**, so it holds no state between calls and a focus is
   recomputed from data every time. That last clause is the real proof: a package with no mutable
   state has no place to keep a focus, and a package that returns ids has nothing to write.

That is the answer to "how do you prove a package that persists nothing never persists": you prove
it cannot say the sentence.

### D10 — I24 gets its `docs/06-harness.md` §4 row in `m2a`, before any test names it

`docs/06-harness.md` §4's table ends at I23 (`:195`). The `nooma-testing` skill's execution step 2 is
explicit: *for a new invariant, add it to the §4 table with its doc 02 section reference, and only
then write the test.*

`m2a` is where I24 is *discovered* (§3.3's argument about the `(weight, last_touched_at)` pair), so
`m2a` is where the row lands:

| # | Invariant | Doc 02 |
|---|---|---|
| I24 | A weight write moves `weight` and `last_touched_at` together; neither is written alone | §2 |

`m2a` adds the row and the type that makes it expressible (`weight.Boost`, D3). `m2c` adds the port
method and the structural test. Splitting a row from its test across two changes is unusual and is
called out so it is not read as an omission: the alternative — `m2c` adding the row — would mean
`m2a` ships the `Boost` type with the invariant it exists to serve undocumented for three changes.

### D11 — How test-first is honoured now that `pending-red` is retired

`scripts/pending-red.sh` is gone (`714934e`). Its practical effect on `m2a` is specific and worth
stating, because "write the test first" now has a failure mode it did not have in M1: **a
conformance test naming an undefined core symbol breaks the untagged build**, so `make check` goes
red with a compile error rather than a test failure. A compile error is not "watched failing red for
the right reason" — it proves the symbol is absent, not that the assertion is meaningful.

The concrete procedure every `m2a` PR follows, and which `sdd-tasks` must encode as ordered task
items rather than as one "implement X with tests" item:

1. **Commit 1** — the test *and* a stub with the final signature whose body returns the zero value.
   The suite compiles; the assertion fails. That is red for the right reason, and the PR's own `git
   log` shows it.
2. **Commit 2** — the implementation. Green.
3. The L1 table and the implementation ship in the same PR, always: `scripts/core-coverage.sh` never
   runs in `make check` (`Makefile:36`), and `m2a` is almost entirely `internal/core/**`, so the ≥90%
   floor is only ever seen by `make check-all` and CI.

Three standing mechanisms back this up and none of them is a gate: `sdd-tasks` orders the test task
strictly before the implementation task; `sdd-verify` reads the PR's `git log` and reports an
inversion as CRITICAL; and `test/conformance/core_exported_decls_have_tests_test.go` — which *is* in
`make check` — makes "shipped with no test at all" impossible without saying anything about
ordering. Stated so it is known that ordering is discipline plus review here, not harness.

---

## 5. Package layout and dependency map

```
internal/core/weight/            PR 1, PR 2 — pure, stdlib + internal/core/unit only
  ├── doc.go                     rewritten: names the three formulas and their §13 rows
  ├── decay.go        PR 1       Effective  (no threshold constant — m2b's, ruling 4)
  ├── zone.go         PR 1       Zone, AllZones, String, ZoneOf
  ├── boost.go        PR 2       Current, Boost, Edge, Neighbourhood,
  │                              ReviveGain, WeightCeiling,
  │                              ResurfaceMaxHops, ResurfaceAttenuation,
  │                              Revive, Resurface
  ├── spread.go       PR 2       unexported bounded BFS producing gain per unit id
  └── *_test.go       PR 1, 2    L1 tables — most of m2a's coverage numerator
      imports: math, time, sort, github.com/rengo/nooma/internal/core/unit

internal/core/focus/             PR 3, PR 4 — pure, stdlib + core/unit + core/weight
  ├── doc.go                     rewritten; never writes the literal "focus" (D7)
  ├── priority.go     PR 3       Candidate, UrgencyLeadDays, UrgencyMax,
  │                              AgeWeight, AgeHorizonDays, AdjacencyWeight,
  │                              UrgencyRamp, AgeRamp, Priority
  ├── rank.go         PR 3       Ranked, Rank (the three-level tie-break)
  ├── adjacency.go    PR 4       AdjacencyStrengths(previous, edges) map[string]float64
  ├── hysteresis.go   PR 4       DefaultHysteresisMargin, ResolveMargin, Displaces
  ├── select.go       PR 4       Kind, KindTask, KindLoad, AllKinds, Types,
  │                              DefaultSize, Selection, Select
  └── *_test.go       PR 3, 4
      imports: sort, time, core/unit, core/weight

test/conformance/                PR 1..4 — L2, untagged, five files
  ├── i05_effective_weight_computed_on_read_test.go     PR 1  (pure half, structural)
  ├── weight_constant_relations_ddl_test.go             PR 2  (D4: §3.2 and §3.3 vs 0002:63)
  ├── focus_margin_ddl_test.go                          PR 4  (D4: margin vs 0002:64)
  ├── i01_focus_never_persisted_test.go                 PR 4  (existing; gains check 3)
  └── i19_hysteresis_margin_test.go                     PR 4

docs/02-cognitive-core.md        §2 amended in PR 1 and PR 2; §3 in PR 3 and PR 4;
                                 §13 gains 10 rows and amends 1 (23 rows → 33)
docs/06-harness.md               §4 gains the I24 row (PR 2) and the I19 row (PR 4)
```

*(Ruling 4 removed `DefaultWeightThreshold`/`ResolveThreshold` from `decay.go` and split D4's
single `weight_focus_defaults_ddl_test.go` into two files that now sit in the PRs where their
subjects are declared.)*

Dependency-rule check. `internal/core/weight` imports `math`, `time`, `sort` and
`internal/core/unit` — all inside `core-purity`'s allow-list (`.golangci.yml:55-57`). No file in
either package calls `time.Now`, `time.Since`, `time.Until`, `rand.*`, `uuid.*` or `os.Getenv`, and
`forbidigo` bans those **by call pattern**, so `time.Time` values and fields are legal
(`.golangci.yml:96-110`). Neither package imports `internal/ports`, `internal/store`,
`internal/scheduler` or anything external. No timezone is read: every time-dependent function takes
`now time.Time` and computes elapsed days as a duration ratio, never as a calendar-day count, so no
`time.Location` is needed or accepted.

`docs/06-harness.md` §1's tree already lists `weight/` and `focus/`, so `m2a` adds no directory and
needs no preflight tree PR. `docs-sync.yml` fires on `^internal/core/`, and **every** `m2a` PR
touches `internal/core/`, so every one of them carries a genuine doc 02 delta — which they do, by
construction: PR 1 and 2 amend §2, PR 3 and 4 amend §3, and all four touch §13. No `m2a` PR should
need the `no-spec-change` label; one that does is not implementing a behaviour doc 02 describes.

---

## 6. How `now` travels

Every time-dependent decision takes `now` as a **named parameter**, never as a struct field and
never through a `Clock`. The rule is worth stating as a rule: `grep -rn 'now time.Time'
internal/core/weight internal/core/focus` enumerates every time-dependent decision `m2a` ships,
exhaustively. A `now` hidden inside an input struct would defeat that, which is why `Candidate`
carries `LastTouchedAt`, `CreatedAt` and `DueAt` — data about the unit — and never the instant the
decision is made.

```
brain (m2c, not in this change)                 core/focus (m2a)          core/weight (m2a)
  now := clock.Now()   ── once per operation ──┐
                                               ├─→ AdjacencyStrengths(prev, edges)
                                               │        └─ map[string]float64
                                               ├─→ Rank(cands, adj, now) ─→ Priority(c, a, now)
                                               │                                 └─→ Effective(w, λ, lt, now)
                                               ├─→ Select(kind, ranked, prev, margin, size)
                                               │        └─ no clock at all (D8)
                                               ├─────────────────────────────→ Revive(cur, now)
                                               └─────────────────────────────→ Resurface(nbh, now)
                                                                                   └─ []Boost → UnitRepo (m2c)
prev : focus.Selection, held in process (ruling round 2 #4) — a parameter here, never a lookup
```

Two properties read off this diagram:

- **The instant enters at the top, once, and travels as a value.** Nothing below `brain` can obtain a
  different one, and `core` cannot obtain one at all. This is `docs/06-harness.md` §2's rule, and in
  `m2a` it is enforced by lint rather than by review (`forbidigo`, scoped to `internal/core/`).
- **The previous focus is a parameter and serves two mechanisms.** Hysteresis (D8) and the adjacency
  term (§3.1) read the same value, which is why the circularity in §3's formula does not need a
  fixpoint. `core/focus` is a pure function of `(candidates, previous, margin, now)` exactly as the
  brief requires, and the "in process, no state row" storage answer is entirely `m2c`'s problem
  because `core` never asks where the value came from.

---

## 7. Test matrix

| What | Level | Where | PR |
|---|---|---|---|
| `Effective`: Δt = 0 returns `weight`; monotone decreasing in Δt; λ = 0 is constant; result ≤ `weight` for all inputs; **negative Δt clamped** (result never exceeds `weight`) | L1 | `internal/core/weight/` | 1 |
| `ZoneOf` over the full 4 statuses × 2 focus-membership matrix, driven by `unit.AllStatuses()` and asserting its own completeness (`m1a` D9's rule 3) | L1 | `internal/core/weight/` | 1 |
| **I05 (pure half)** — no exported function in `core/weight` returns a `unit.Unit`; `Boost` has exactly two producers | L2, structural | `test/conformance/` | 1 |
| `Revive`: strictly increasing under repetition; converges on `WeightCeiling` and never reaches it; never lowers a weight; leaves λ untouched; **always returns a `Boost` with `LastTouchedAt == now`, including at `e ≥ Ceiling`** | L1 | `internal/core/weight/` | 2 |
| **`Revive` at `e ≥ WeightCeiling` is effective-weight-neutral**: `Effective` over the returned pair equals `Effective` over the input pair at an arbitrary later instant, and the returned weight is exactly `e` — neither raised nor lowered | L1 | `internal/core/weight/` | 2 |
| The two constant relations against `weight_threshold`'s DDL default (`0002:63`): `ReviveGain × WeightCeiling > 0.5` and `Attenuation^MaxHops × Ceiling ≤ 0.5` — asserted at the **defaults**, as inequalities, with the ⚙ caveat in the doc comment (D4, ruling 4) | L2, reads SQL off disk via `migrationSQLText` | `test/conformance/` | 2 |
| `Resurface`: origin never a recipient; a cyclic graph terminates; multi-path takes `max` not sum; direction ignored; multi-edge takes the strongest; output sorted by `UnitID` | L1 | `internal/core/weight/` | 2 |
| `Resurface` boundary: 1-hop target at `strength = 1.0` is exactly `WeightCeiling / 2`; `e ≥ target` produces **no `Boost` at all** (a shorter slice, not a zero-delta entry) and does not touch `last_touched_at`. *(The 2-hop-vs-`weight_threshold` half of this row moved to the L2 DDL file under ruling 4.)* | L1 | `internal/core/weight/` | 2 |
| `UrgencyRamp`: nil `DueAt` is exactly 0; `d ≥ lead` is 0; `d = 0` is 1; overdue clamps at 1 and does not grow | L1 | `internal/core/focus/` | 3 |
| `AgeRamp`: age 0 is 0; age = half the horizon is 0.5; age ≥ horizon is 1 and does not grow; `created_at` after `now` clamps at **0**. Every fixture is a multiple of `AgeHorizonDays`, never a literal day count — which is why ruling 10's 30 → 15 needs no edit here | L1 | `internal/core/focus/` | 3 |
| `Priority`: `priority ≥ effective_weight` for every input; monotone in effective weight at fixed context; **homogeneous of degree 1 in `e`** (scaling every weight by 0.5 leaves `Rank`'s order identical); maximum amplification is `UrgencyMax × (1 + AgeWeight + AdjacencyWeight)` = 4.35 | L1 | `internal/core/focus/` | 3 |
| `Priority` is **type-independent** (ruling 8): a `task` and an `event` identical in every other field score exactly equal | L1 | `internal/core/focus/` | 3 |
| **Bounded anti-starvation** (§3.1, `spec.md` R3.5) — P1: with no deadline and no adjacency, `priority ≤ e × (1 + AgeWeight)`. P2: the crossover ratio table over `Δg ∈ {0, 0.5, 1}`, pinning the 0.833 boundary from both sides. P3: `e = 0.5` at full age ranks below `e = 1.0` at zero age | L1 | `internal/core/focus/` | 3 |
| **P4/P5 — the priority-over-time walk. CHANGED BY RULING 10**: an untouched unit at λ = `classify.PriorDecayRate` walked at `t ∈ {0, ½·horizon, horizon, 2·horizon, 4·horizon}` **rises to its maximum at exactly `t = horizon`** and declines strictly thereafter. Both closed forms pinned from both sides: λ = 0.0111 (peak still at the horizon) and λ = 0.0134 (decreasing from `t = 0`). *This assertion's sign is the opposite of the one the test carried before ruling 10* | L1 | `internal/core/focus/` | 3 |
| **P6** — day-30 and day-60 priorities are independent of `AgeHorizonDays`: computed at two horizon constants and asserted equal, proving the lever bought a window and not a floor | L1 | `internal/core/focus/` | 3 |
| `Rank`: total order; the three-level tie-break, including non-nil `DueAt` before nil; deterministic under `-shuffle=on`; `nil` adjacency map behaves as all-zero | L1 | `internal/core/focus/` | 3 |
| `Types(KindTask)`/`Types(KindLoad)` are fresh slices; `AllKinds()` is exhaustive over the `Kind` constants | L1 | `internal/core/focus/` | 4 |
| `AdjacencyStrengths`: `max` over edges to previous members; direction ignored; empty previous gives an empty map | L1 | `internal/core/focus/` | 4 |
| `Displaces` boundary table: equal → no; exactly `×(1+margin)` → no; `+ε` → yes | L1 | `internal/core/focus/` | 4 |
| `ResolveMargin(nil)` is `DefaultHysteresisMargin`; a non-nil pointer passes through (D4, ruling 5) | L1 | `internal/core/focus/` | 4 |
| `focus.DefaultHysteresisMargin` equals migration `0002:64`'s column `DEFAULT` (D4, ruling 5) | L2, reads SQL off disk via `migrationSQLText` | `test/conformance/` | 4 |
| `Select`: incumbent retained on a tie; incumbent absent from `ranked` dropped with no contest; empty `previous` reduces to top-N; **the adjusted sort agrees with `Displaces` over a boundary table** (D8's equivalence, §8 R4) | L1 | `internal/core/focus/` | 4 |
| **I19** — a challenger must beat the incumbent by more than `hysteresis_margin`, named in the test identifier | L2 | `test/conformance/i19_...` | 4 |
| **I01** — existing test, now non-vacuous; plus the new structural check that `core/focus` returns no `unit.Status`, holds no package-level `var`, and returns ids not units | L2 | `test/conformance/i01_...` | 4 |

No test in `m2a` opens a database, reaches a network, or reads a real clock — every one of them is a
pure function over literal inputs, which is also why every one is L1 unless it names a doc 02
invariant (`nooma-testing`'s decision gate: *when torn between L1 and L3, it is L1*). The **five**
L2 tests exist because they name invariants (I01, I05, I19) or pin a value to schema text (D4's two
DDL files); none of them contributes to the ≥90% core floor, since `scripts/core-coverage.sh:56`
measures only test binaries under `internal/core/...`. *(There were four before ruling 4 split D4's
single defaults file in two and moved one of the halves' subject out of the change.)*

---

## 8. Risks this design accepts

| # | Risk | Position |
|---|---|---|
| 1 | **Ten new calibration constants, none of them calibrated.** There is no usage data, and every default in §3 is a judgement | Accepted and labelled. Every row in §3's three tables says "chosen" or "derived", and the two constant *relations* (§3.2, §3.3) tie four otherwise-free numbers to `weight_threshold`, which the schema already fixes. The alternative — shipping fewer knobs by burying literals — is what harness §7's "one place" rule forbids. **Post-reconciliation the count is unchanged at ten**, which is a coincidence worth stating rather than a design goal: ruling 7 cut eleven constants that only existed to support `spec.md`'s linear sum and additive-clamp boost, ruling 9 renamed two of this document's, and ruling 4 removed `weight_threshold`'s Go home. §13 goes from **23 rows to 33**, with one of the original 23 amended in place |
| 2 | **Removing `type` from the priority arithmetic changes doc 02 §3's stated signature.** Every other formula decision fills a gap; this one deletes a term | Owner-review item, flagged in §3.1 with the two rejected alternatives and their costs. The consequence is concrete and reversible: a `task` and an `event` at equal score tie, broken by `DueAt`. Reinstating a numeric type term later is additive, not a rewrite |
| 3 | **Relative hysteresis is a reading of doc 02 §3, not a quotation of it** | Owner-review item (D8). The argument is that `priority` has no fixed scale, so an absolute margin cannot have one global default; §13's `correction_referent_margin` is the in-repo precedent for a ratio margin that labels itself. The §13 row gains "(relative)" so the reading is recorded, not inferred |
| 4 | **`Select`'s adjusted sort and `Displaces` are two spellings of one rule** and can drift | Accepted with a mechanism, not pretended away: `Displaces` is the definition and I19's test target; an L1 test asserts the sort agrees with it over a boundary table including all three edge cases. A swap loop calling `Displaces` directly was the alternative and is slower and harder to read for the same behaviour |
| 5 | **`Priority`'s adjacency term has no producer in M2**, so its arm is exercised only by tests | Accepted; it is proposal §4.3's own argument in miniature. The mitigation is in the signature: `adjacency` is a `map` that may be `nil`, so the no-graph caller is a first-class case rather than a degraded one, and M4 fills it without changing anything |
| 6 | **`Resurface` obliges `m2c` to load a bounded 2-hop neighbourhood** — a repository shape no port has today, and one the proposal's R2 does not price | Named here so it is designed in `m2c` rather than discovered. The hop limit does double duty (it bounds the propagation and the query), and `Neighbourhood` is the exact value the loader must produce |
| 7 | **Resurface resets `last_touched_at`**, which the brief expected it not to | Argued in §3.3: `weight` is *defined* as the value at `last_touched_at`, so writing one without the other encodes a fiction — that is I24's mechanical origin. The legitimate worry is answered by the target cap and the no-op branch instead, and both halves go into doc 02 §2 so the next reader does not re-litigate it |
| 8 | **I01's tree scan is a substring heuristic** and `core/focus` is the package most likely to trip it | D7 makes the mitigation absolute rather than conditional: the literal `"focus"` appears nowhere in the package, in code or comments. A conditional rule would be one refactor from failing |
| 9 | **The ≥90% core floor bites hardest here** — `m2a` is almost entirely `internal/core/**`, and `make check` never runs `scripts/core-coverage.sh` | Every PR ships its L1 table in the same commit as its code (D11 step 3), and `make check-all` is the pre-PR command structurally, not as a reminder. The functions are total over small enumerable domains — three ramps on `[0,1]`, a 4×2 zone matrix, a three-case boundary table — which is what makes exhaustive tables possible rather than aspirational |
| 10 | **THREE of `m2a`'s four PRs exceed the 400-line ceiling, not two, and `m2a` is realistically seven PRs rather than four.** See §8.1 for the re-derivation the adjudication requires | Split lines drawn in advance in §8.1. `sdd-tasks` plans against those, not against proposal §5.1's row |
| 11 | *(Retired by ruling 4.)* This row previously read: *"`m2a` declares `weight.DefaultWeightThreshold` for consumers that do not exist yet (`archive` is `m2b`)"*, accepted on the grounds that §3.2's constant relation made it load-bearing | The risk is gone because the declaration is gone (D4). What replaced it is smaller and is **R14** below: the relation is now asserted against migration text, so `m2a` depends on a *parse* of a SQL file rather than on a Go constant |
| 12 | **The first ranking after a restart differs from the second in two ways**, not one: no hysteresis *and* no adjacency | Named in §3.1 and D8. It is ruling round 2 #4's accepted cost, repriced; the doc 02 §3 sentence that ruling owes gains the second half |
| 13 | **Anti-starvation is a two-week grace window of a few percent, not a floor** — and the phrase it was chosen under promised a floor. Under ruling 1's envelope, ruling 9's age term multiplies `e` instead of adding to it. **Ruling 10 fixed the half of this that was fixable** (`AgeHorizonDays` 30 → 15, break-even λ 0.00667 → 0.01333, above the base, so the rise is now real: peak 1.0328× at day 15). **The half that is structural remains**: leverage capped at ×1.20 for life, an overturnable deficit of 16.7 % that ruling 10 did not move at all, a unit at the archive floor topping out at 0.60 against a healthy unit's 1.0, and identical values to the rejected horizon from day 30 on | **Accepted on the merits and deliberately surfaced, twice** (§3.1's *Bounded anti-starvation*, `spec.md` §9). The surfacing worked — it produced ruling 10 — and the residue is genuinely structural: an item below `weight_threshold` is designed to be **archived** (doc 02 §1, §6), and the only shape that would rescue it is an **additive** term, which is exactly what ruling 1 rejected. **The standing owner-review item is now narrower**: whether a +3.3 % two-week window is the intended product behaviour, given that P2's 16.7 % is unchanged. Levers and their exact effects are in §3.1 |
| 16 | **The break-even λ has only 33 % of headroom over the base, and doc 02 §2 directs the model to raise λ for exactly the type anti-starvation is for.** Break-even 0.01333/day vs `classify.PriorDecayRate` 0.01/day; doc 02 §2 says `task` → *high* λ; at λ = 0.02 the rise vanishes entirely | Named, not acted on. `prior.go:9-19` currently encodes **no per-type λ table** (deliberately — doc 02 names no per-type numbers), so today every unit gets 0.01 and the rise is universal. This becomes live only when the model or M5's learning module starts personalizing λ upward. Recorded here so that work has the number in front of it rather than rediscovering it; the fix if it lands is another turn of `AgeHorizonDays`, which costs nothing structurally |
| 14 | **Two of `m2a`'s arithmetic promises are now asserted against a parse of a SQL file** rather than against a Go constant (D4, ruling 4) | Accepted, and arguably an improvement: the pin is to the schema itself rather than to another Go constant that would in turn need pinning. The mechanism is not new — `relation_thresholds_ddl_test.go` and `i13_learning_signal_test.go`'s `migrationSQLText` already do exactly this, and `m2a` reuses the helper rather than writing a parser. The residual risk is that a `DEFAULT` reformatted in a future migration breaks the parse; that fails loudly at L2, which is the correct failure |
| 15 | **`spec.md` and this document disagreed on four substantive points and were reconciled by a third pass**, not by either author | Named so it is not forgotten when the next change is planned. The adjudication's own conclusion: parallelising spec and design was right for `effective_weight`, the focuses and hysteresis, and wrong for the three formulas `m2a` owns — that was not independent work, and both agents invented the same thing twice. **Parallelise phases that share inputs, not phases that must both invent the same thing.** The cost was one extra pass; the benefit was three conflicts nobody had listed (`age`'s opposite signs, the `weight_threshold` scope pull, the negative-Δt gap) |

### 8.1 PR line budgets, re-derived — R10

The adjudication requires this because ruling 4 changed the scope after both estimates were
written, and because the original R10 named two over-ceiling PRs without redoing the arithmetic.

**Where the guessing is.** Every number below is a **guess**, and the honest disclosure is that
they are guesses of the same kind that were wrong before: proposal §5.1 estimated 250/300/350/350,
and the proposal itself records M1's measured overrun at **1.3×–4.3×** of estimate. The figures
below run 1.4×–1.8× proposal §5.1's, which sits at the *low end* of M1's own measured band — so if
they are wrong, the likely direction is **still too low**. What is not a guess: which PRs cross the
400-line ceiling, since three of them cross it by margins wider than the estimation error.

| PR | Go | L1 | L2 | docs | total | vs proposal §5.1 |
|---|---|---|---|---|---|---|
| 1 `feat/core-weight-decay` | ~105 (`decay.go` 35 + `zone.go` 55 + `doc.go` 15) | ~160 (`Effective` table, clamp, monotonicity, ≤ w property; the full `AllStatuses` × 2 zone matrix) | ~70 (I05 structural) | ~15 (§2) | **~350** | 250 → under the ceiling, but not by much |
| 2 `feat/core-weight-boost` | ~200 (`boost.go` 120 + `spread.go` 80) | ~260 (revive table + neutrality; six resurface graph fixtures) | ~80 (the two DDL-pinned relations) | ~38 (§2 ×2, §13 ×4, harness I24) | **~580** | 300 → **over** |
| 3 `feat/core-focus-priority` | ~195 (`priority.go` 110 + `rank.go` 70 + `doc.go` 15) | ~355 (two ramp tables; the `Priority` property set **including P1–P6's anti-starvation bounds**; the tie-break and shuffle determinism) | — | ~52 (§3 rewrite + the age reading + the anti-starvation passage; §13 ×5) | **~605** | 350 → **over** |
| 4 `feat/core-focus-selection` | ~195 (`adjacency.go` 45 + `hysteresis.go` 40 + `select.go` 110) | ~250 | ~180 (I19 60 + I01's third check 70 + the margin DDL pin 50) | ~17 | **~640** | 350 → **over** |

**Net effect of ruling 4 on the budgets, stated separately** so it is visible that the reversal did
*not* rescue the estimates: it removed ~55 lines from PR 1 (`DefaultWeightThreshold`,
`ResolveThreshold`, their L1 test, and half of the combined DDL file) and **added** ~15 to PR 2 (a
new conformance file needs its own header and helper wiring) and ~50 to PR 4 (the margin DDL pin
moves there from PR 1). `m2a` got ~10 lines lighter overall and no PR crossed back under the
ceiling.

**Split lines, drawn in advance.** Seven PRs, not four:

| Slice | Contents | est. |
|---|---|---|
| **1** | `decay.go`, `zone.go`, I05's structural test, §2's decay/zone amendment | ~350 |
| **2a** | `Current`, `Boost`, `Revive`, the revive L1 tables incl. the neutrality assertion, §2's revive paragraph, `revive_gain`/`weight_ceiling` §13 rows, **harness §4's I24 row** (it travels with the PR introducing `Boost`) | ~280 |
| **2b** | `Edge`, `Neighbourhood`, `Resurface`, `spread.go`, the graph fixtures, §2's resurface paragraph, `resurface_*` §13 rows, `weight_constant_relations_ddl_test.go` | ~300 |
| **3a** | `Candidate`, the five priority constants, `UrgencyRamp`, `AgeRamp`, `Priority`, the property set incl. P1–P6, §3's formula rewrite + age + anti-starvation passage, the five §13 rows | ~405 |
| **3b** | `Ranked`, `Rank`, the three-level tie-break, determinism under `-shuffle=on` | ~200 |
| **4a** | `Kind`/`Types`/`AllKinds`/`Selection`/`Select`, `hysteresis.go` with `Displaces` + `ResolveMargin`, I19, §3's hysteresis amendment, §13's amended row | ~400 |
| **4b** | `AdjacencyStrengths`, the I01 strengthening, `focus_margin_ddl_test.go` | ~250 |

**3a/3b is a new split** that R10 did not have; it is caused by the anti-starvation property set and
the longer doc 02 §3 amendment that ruling 9 and the flagged interaction produce. **4a sits exactly
on the ceiling at ~400** — if it runs over, the first thing to move to 4b is
`focus_margin_ddl_test.go`, which is already assigned there.

**Ruling 10's effect on these budgets: none material, and here is why rather than an assertion.**
The ruling changes one constant's value. It does not add a function, a type, a file, a constant, or
a §13 row. Three things do change and all three are net-neutral in size:

- **The P4/P5 test fixture changes shape, not length.** It walked four instants asserting a
  monotone decrease; it now walks five asserting a rise to a maximum at the horizon and a decline
  after. Call it **+5 lines**, plus **~15** for the second break-even case (λ = 0.0111 / 0.0134)
  that the two closed forms now justify pinning.
- **P6 is a new assertion** (day-30/day-60 independence from `AgeHorizonDays`): **~15 lines**.
- **The doc 02 §3 amendment prose changes wording, not volume** — "anti-starvation over 30 days"
  becomes "over 15 days", and one paragraph is rewritten from "does not reverse the descent" to "a
  transient lift peaking at two weeks". **±0.**

Net: **PR 3a moves from ~370 to ~405**, which is the one figure worth flagging — it crosses the
400-line ceiling by about 5 lines, i.e. **inside the estimation error and not a real crossing**. If
it does land over, the split is trivial and already implied: `AgeRamp` plus the P1–P6 property set
can travel separately from `UrgencyRamp` and `Priority`'s envelope. No other slice moves, and
`m2a`'s total goes from ~2,150 to ~2,185 — inside the noise of every other figure in this table.

The reason the blast radius is this small is `nooma-core` hard rule 4 doing its job: every derived
figure — the break-even λ, the peak lift, the overturnable deficit — is **computed from the
constants in the test**, never written as a literal, so changing 30 to 15 propagated to five derived
numbers without a single literal needing to be found and edited. That is the rule paying for itself
in a way worth recording, since its usual justification is stated as discipline rather than as
measured benefit.

Total: **~2,200 lines across 7 PRs** (the four unsplit rows sum to ~2,175; the seven slices to
~2,185 — the difference is rounding, not a hidden item), against proposal §5.1's ~1,250 across 4.
That is a **1.75× overrun** on the plan of record and it should be reported as such rather than
absorbed silently.

---

## 9. What this design does not decide

- **`weight_threshold`'s Go home and its `ResolveThreshold`.** Ruling 4 returns them to `m2b`'s
  `feat/core-consolidation-expire-archive` (proposal §5.1:348), where `archive` — the only consumer
  — lives. `m2a` keeps the two arithmetic promises that reference the value and asserts them
  against the migration DDL text instead (D4).
- **`strengthen`, `reweight`, and the incomplete-resolution predicate** — the other three of ruling
  round 2 #1's six formulas. They are `m2b`'s, and `reweight` in particular will consume
  `weight.Effective` and `weight.Boost` unchanged.
- **Whether λ changes on use.** Spaced repetition says a thing used repeatedly should be forgotten
  more slowly; doc 02 §2 assigns λ at classify and says nothing about revising it. `m2a` leaves λ
  untouched and records the idea here rather than acting on it — changing λ is a doc 02 §2 amendment
  with its own §13 consequences, and it belongs with the learning module (§9, M5) that would tune it.
- **The `UnitRepo` weight-write method and the `ConfigRepo`.** `m2a` fixes the *shape* both must have
  — a `Boost` at the write, a `*float64` sentinel at the read — and `m2c` declares them.
  Proposal R2's batch-versus-single tension for `reweight`'s bulk materialization is untouched here.
- **Where the previous focus is held between calls.** Ruling round 2 #4 answered it (in process, no
  state row); `m2a` only requires that it arrives as a parameter, so the storage decision is entirely
  `m2c`/`m2d`'s and could be revisited without touching `core`.
- **Any surface that displays a focus.** Proposal §4.3 is explicit that `core/focus` ships with no
  consumer in M2; the today view is M4.
- **A third focus.** Doc 02 §3 says it costs a query, and D7 makes it cost a `Kind` constant and a
  `Types` arm. Not now.
- **Bulk decay materialization.** Doc 02 §2 and §6.6 permit it; it is `reweight`'s (`m2b`), and I05's
  structural half (`m2c`) must be scoped to read paths so it does not forbid what doc 02 allows
  (proposal R13).

---

## 10. Reconciliation — every ruling from #650 plus ruling 10, and where each landed

`sdd-spec` and `sdd-design` ran concurrently over `m2a-weight-focus`, never saw each other, and
produced two artifacts that disagreed on four substantive points. A fresh-context adversarial
reviewer adjudicated every conflict and the owner ruled on the two that were theirs. The record is
`sdd/m2a-weight-focus/adjudication` (**#650**), which is binding on both documents.

**Ruling 10 is not in #650.** It is a follow-up owner decision taken *after* this reconciliation
delivered the boundary numbers #650 asked for, and it is listed in the same table because a reader
tracing `age_horizon_days` needs both entries in one place. It is also the clearest evidence that
the flagged-interaction requirement was worth having: the analysis was demanded in numbers rather
than prose, and the numbers overturned a value the owner had already ruled on.

Nothing overruled below has been silently rewritten into looking right. This project's rule —
recorded in `openspec/README.md` and in this chain's own C10/C21.1 history — is that **evidence is
kept and the status rewritten**. For a live, pre-merge planning artifact that means the text is
corrected *and* an inline **Reconciled (ruling N)** note records what it used to say, so a reader
six months out can see that a real disagreement happened and how it was settled.

| # | Ruling | Who won | Applied where in this document |
|---|---|---|---|
| 1 | Priority is the **multiplicative envelope**; `spec.md`'s linear weighted sum is defectively ordered (a forgotten item beat a live one by ~4× on context alone) | this document | §3.1 shape note; the homogeneity property added; `spec.md` R3.1 rewritten |
| 2 | Boost is **one asymptotic mechanism at two strengths** (`spec.md`'s additive clamp collapsed distinct hot units to an identical 2.0) — **but this document's `e ≥ WeightCeiling` edge is wrong**: a direct use must still move `last_touched_at` | split | §3.2's formula gains `max(0, …)` and writes unconditionally; new *At or above the ceiling* subsection; **D3's `Revive` signature loses its `bool`**; §3.3 gains the direct/indirect write asymmetry; §7 gains a neutrality test row |
| 3 | Adopt this document's **negative-Δt clamp** — `spec.md` never constrained `now < lastTouchedAt` | this document | D1's note; the same rule reapplied at §3.1's `AgeRamp` and stated as one rule in two places |
| 4 | **`weight_threshold` goes back to `m2b`** — D4's pull overrode an owner-reviewed chained-PR assignment (proposal §5.1:348) without flagging it, and changed two PR budgets neither estimate redid | `spec.md` / the proposal | **D4 reversed in half**; §3.2 and §3.3's constant relations re-pointed at the migration DDL text; §5's layout and test list updated; R11 retired and replaced by R14; §8.1 redoes the budgets; §9 records the handoff |
| 5 | `spec.md` adopts this document's **`ResolveMargin` + DDL-pinning L2 test** for `hysteresis_margin` | this document | D4's surviving half, strengthened; the pin moves from PR 1 to PR 4 |
| 6 | Hysteresis is **relative** — entailed by ruling 1, not an independent preference; §13's row gains "(relative)" | this document | D8's note; `Displaces`'s argument order is this document's |
| 7 | Cut the ~9 constants that only supported the linear sum; **add `focus_size` (7)**, which `spec.md` took as an unnamed caller parameter with no default anywhere | both | §3.1's §13 table; D7's `DefaultSize` stands; eleven of `spec.md`'s seventeen cut, net ten new rows |
| 8 | **OWNER** — `type` leaves the priority arithmetic; doc 02 §3:76 goes from five terms to four; ties broken by `DueAt`. *And*: this document's rejected-alternatives table attacked a nine-constant strawman; `spec.md` proposed three, and `type` was **not** redundant with membership filtering for the `task`/`event` pair | this document's conclusion, on corrected grounds | §3.1's `type` subsection gains the correction verbatim; the "equal novelty" phrasing updated to "equal age"; reinstatement priced as additive |
| 9 | **OWNER** — `age` means **ANTI-STARVATION**: 0 → 1 over `age_horizon_days`, older ranks higher. **`NoveltyRamp` is rejected**. *(The horizon was 30 when this ruling was taken; ruling 10 below moved it to 15 without disturbing anything else here.)* | `spec.md` | §3.1's `age` subsection **rewritten**, with the rejected argument's one surviving true observation and its one retracted claim both recorded; `novelty_weight`/`novelty_window_days` replaced by `age_weight`/`age_horizon_days`; D6, §5, §7 renamed throughout |
| — | **Flagged interaction** neither artifact examined: rulings 1 + 9 mean a unit decayed toward zero cannot climb on age alone | neither | §3.1's **Bounded anti-starvation** subsection (P1–P6 with numbers), §7's test rows, **§8 R13**, and `spec.md` R3.5 + §9. **This entry is what produced ruling 10** |
| 10 | **OWNER, taken in response to the analysis above** — `age_horizon_days` **30 → 15**; `age_weight` stays 0.20; ruling 9 is otherwise untouched. 30 was rejected because this document's own numbers showed it left the break-even λ at 0.00667/day, *below* §13's base 0.01, so priority decreased across the whole horizon (`exp(-0.3) × 1.20 = 0.8890 < 1`) and the promise it was chosen under was false as specified | the analysis | §3.1's `age` subsection gains a second reconciliation note and the new default; the ramp table's day counts; **P4 rewritten** (two closed forms rather than one, and the sign flips), **P5** and **P6** added; §3.1's §13 rows; §7's **P4/P5 test row, whose assertion now runs the opposite way**, plus a new P6 row; **§8 R13 rewritten** and **R16 added** (the 33 % λ headroom); §8.1's ruling-10 budget paragraph; `spec.md` R3.4, R3.5, §5.1, §5.4, §7 and §9 |

Two divergences the adjudication did not rule on, because they are naming and totality conflicts
rather than formula conflicts. Both are resolved in **this document's** favour, since this document
owns the signatures, and both are recorded so they are not mistaken for silent drift:

| # | Divergence | Resolution |
|---|---|---|
| C-a | `spec.md` had `ThermalZone`, **partial**, with `superseded`/`incomplete` deliberately excluded from every test on the grounds that doc 02 is silent; D2 had `ZoneOf`, **total**, both mapping to `ZoneCold`, tested over the full `AllStatuses` × 2 matrix | D2's. A deliberately untested arm is an uncovered statement against a ≥ 90 % floor (`nooma-testing` hard rule 5), and a partial function whose contract says "do not call it this way" is a rule somebody remembers rather than a property the type system holds. The Cold mapping is a **choice** and is recorded in the §2 amendment |
| C-b | Identifier drift throughout: `ThermalZone`/`ZoneOf`, `ActionableTypes()`/`Types(Kind)`, `Displaces(incumbent, challenger, …)`/`Displaces(challenger, incumbent, …)`, `resurface_hop_limit`/`resurface_max_hops`, `temporal_urgency_horizon_days`/`urgency_lead_days`, `revive_boost_amount`/`revive_gain` | This document's names throughout. `spec.md` now states every requirement against the identifiers declared here, so `sdd-tasks` reads one vocabulary rather than two |
| C-c | **§3.1's "§13 rows F1 adds" table — cited by this chain's `tasks.md` (2a.5, 2b.5, 3a.8) as "design §5.1" — carried a PR attribution for `focus_adjacency_weight` naming PR 4, disagreeing with §5's package-layout tree (`priority.go PR 3 … AdjacencyWeight`) and §8.1's PR split (`3a … the five priority constants … the five §13 rows`).** Found by task 3a.8 during PR 3a's own implementation, and confirmed still present at archive time (`sdd-verify`, `sdd/m2a-weight-focus/verify-report`) | **§5's package-layout / §8.1's reading, and this is the record.** `AdjacencyWeight` is declared where `Priority` needs it — in `priority.go`, PR 3 / 3a — not in `adjacency.go` (PR 4 / 4b), which declares the function `AdjacencyStrengths` instead. The two are easy to conflate by name and sit a few lines apart in §5's tree; the shipped code follows the package-layout/§8.1 reading, confirmed directly against `internal/core/focus/priority.go`. Cosmetic — no shipped behaviour depends on the stale cell — and left as historical record per this project's annotation convention rather than rewritten. See §3.1's own inline note for the same annotation at the table itself |

**Purity re-verified after every edit above.** Neither package imports anything outside `$gostd` +
`internal/core/**`; `math`, `time` and `sort` are the only stdlib imports and all three are inside
`depguard`'s `core-purity` allow-list (`.golangci.yml:55-57`). No file calls `time.Now`,
`time.Since`, `time.Until`, `rand.*`, `uuid.*` or `os.Getenv` — `forbidigo` bans them by call
pattern, so the `time.Time` values on `Current`, `Boost` and `Candidate` are legal
(`.golangci.yml:96-119`). No timezone is read or accepted: every elapsed time in the reconciled
formulas — `Effective`'s Δt, `UrgencyRamp`'s `d`, `AgeRamp`'s `ageDays` — is a duration ratio
(`Sub(...).Hours() / 24`), never a calendar-day count, so no `time.Location` is needed. The changes
this pass made did not introduce a single new time-dependent function: `AgeRamp` replaces
`NoveltyRamp` at the same call site with the same `(createdAt, now)` signature.
